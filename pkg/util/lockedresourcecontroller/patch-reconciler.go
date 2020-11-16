package lockedresourcecontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	astatus "github.com/operator-framework/operator-sdk/pkg/ansible/controller/status"
	"github.com/operator-framework/operator-sdk/pkg/status"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedpatch"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/jsonpath"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sigs.k8s.io/yaml"
)

var controllername = "controller_patchlocker"

var log = logf.Log.WithName(controllername)

//LockedPatchReconciler is a reconciler that can enforce a LockedPatch
type LockedPatchReconciler struct {
	util.ReconcilerBase
	patch        lockedpatch.LockedPatch
	status       status.Conditions
	statusChange chan<- event.GenericEvent
	parentObject metav1.Object
	statusLock   sync.Mutex
}

//NewLockedPatchReconciler returns a new reconcile.Reconciler
func NewLockedPatchReconciler(mgr manager.Manager, patch lockedpatch.LockedPatch, statusChange chan<- event.GenericEvent, parentObject metav1.Object) (*LockedPatchReconciler, error) {

	// TODO create the object is it does not exists

	reconciler := &LockedPatchReconciler{
		ReconcilerBase: util.NewReconcilerBase(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetEventRecorderFor(controllername+"_"+patch.GetKey())),
		patch:          patch,
		statusChange:   statusChange,
		parentObject:   parentObject,
		statusLock:     sync.Mutex{},
		status: status.Conditions([]status.Condition{{
			Type:               status.ConditionType("Initializing"),
			LastTransitionTime: metav1.Now(),
			Status:             corev1.ConditionTrue,
		}}),
	}

	controller, err := controller.New(controllername+"_"+patch.GetKey(), mgr, controller.Options{Reconciler: reconciler})
	if err != nil {
		return &LockedPatchReconciler{}, err
	}

	//create watcher for target
	gvk := getGVKfromReference(&patch.TargetObjectRef)
	groupVersion := schema.GroupVersion{Group: gvk.Group, Version: gvk.Version}
	obj := objectRefToRuntimeType(&patch.TargetObjectRef)
	mgr.GetScheme().AddKnownTypes(groupVersion, obj)

	err = controller.Watch(&source.Kind{Type: obj}, &enqueueRequestForPatch{
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      patch.TargetObjectRef.Name,
				Namespace: patch.TargetObjectRef.Namespace,
			},
		},
	}, &referenceModifiedPredicate{
		ObjectReference: patch.TargetObjectRef,
		Client:          reconciler.GetClient(),
	})
	if err != nil {
		return &LockedPatchReconciler{}, err
	}

	for _, sourceRef := range patch.SourceObjectRefs {
		gvk := getGVKfromReference(&patch.TargetObjectRef)
		groupVersion := schema.GroupVersion{Group: gvk.Group, Version: gvk.Version}
		obj := objectRefToRuntimeType(&patch.TargetObjectRef)
		mgr.GetScheme().AddKnownTypes(groupVersion, obj)
		err = controller.Watch(&source.Kind{Type: obj}, &enqueueRequestForPatch{
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      sourceRef.Name,
					Namespace: sourceRef.Namespace,
				},
			},
		}, &referenceModifiedPredicate{
			ObjectReference: sourceRef,
			Client:          reconciler.GetClient(),
		})
		if err != nil {
			return &LockedPatchReconciler{}, err
		}
	}

	return reconciler, nil
}

// func getKeyFromPatch(patch lockedpatch.LockedPatch) string {
// 	return patch.TargetObjectRef.String()
// }

func getGVKfromReference(objref *corev1.ObjectReference) schema.GroupVersionKind {
	return schema.FromAPIVersionAndKind(objref.APIVersion, objref.Kind)
}

func objectRefToRuntimeType(objref *corev1.ObjectReference) runtime.Object {
	obj := &unstructured.Unstructured{}
	obj.SetKind(objref.Kind)
	obj.SetAPIVersion(objref.APIVersion)
	obj.SetNamespace(objref.Namespace)
	obj.SetName(objref.Name)
	return obj
}

type enqueueRequestForPatch struct {
	reconcile.Request
}

var enqueueLog = logf.Log.WithName("eventhandler").WithName("EnqueueRequestForObject")

func (e *enqueueRequestForPatch) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	q.Add(e.Request)
}

// Update implements EventHandler
func (e *enqueueRequestForPatch) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	q.Add(e.Request)
}

// Delete implements EventHandler
func (e *enqueueRequestForPatch) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	q.Add(e.Request)
}

// Generic implements EventHandler
func (e *enqueueRequestForPatch) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	q.Add(e.Request)
}

type referenceModifiedPredicate struct {
	corev1.ObjectReference
	predicate.Funcs
	client.Client
}

var predicateLog = logf.Log.WithName("predicate").WithName("ReferenceModifiedPredicate")

// Update implements default UpdateEvent filter for validating resource version change
func (p *referenceModifiedPredicate) Update(e event.UpdateEvent) bool {
	if e.MetaNew.GetName() == p.ObjectReference.Name && e.MetaNew.GetNamespace() == p.ObjectReference.Namespace {
		if compareObjectsWithoutIgnoredFields(p.Client, e.MetaNew, p.ObjectReference) {
			return false
		}
		return true
	}
	return false
}

func (p *referenceModifiedPredicate) Create(e event.CreateEvent) bool {
	if e.Meta.GetName() == p.ObjectReference.Name && e.Meta.GetNamespace() == p.ObjectReference.Namespace {
		return true
	}
	return false
}

func (p *referenceModifiedPredicate) Delete(e event.DeleteEvent) bool {
	// we ignore Delete events because if we loosing references there is no point in trying to recompute the patch
	return false
}

func (p *referenceModifiedPredicate) Generic(e event.GenericEvent) bool {
	// we ignore Generic events
	return false
}

// we ignore the fields of resourceVersion and managedFields
func compareObjectsWithoutIgnoredFields(client client.Client, changedObjSrc metav1.Object, patchObjSrc corev1.ObjectReference) bool {
	changeObj := changedObjSrc
	patchObj := &unstructured.Unstructured{}
	client.Get(context.TODO(), types.NamespacedName{Name: patchObjSrc.Name, Namespace: patchObjSrc.Namespace}, patchObj)
	changeObj.SetManagedFields(nil)
	changeObj.SetResourceVersion("")
	patchObj.SetManagedFields(nil)
	patchObj.SetResourceVersion("")
	changeObjJSON, _ := json.Marshal(changeObj)
	patchObjJSON, _ := json.Marshal(patchObj)

	return (string(changeObjJSON) == string(patchObjJSON))
}

//Reconcile method
func (lpr *LockedPatchReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	//gather all needed the objects
	targetObj, err := lpr.getReferecedObject(&lpr.patch.TargetObjectRef)
	if err != nil {
		log.Error(err, "unable to retrieve", "target", lpr.patch.TargetObjectRef)
		return lpr.manageError(err)
	}
	sourceMaps := []interface{}{}
	for _, objref := range lpr.patch.SourceObjectRefs {
		sourceObj, err := lpr.getReferecedObject(&objref)
		if err != nil {
			log.Error(err, "unable to retrieve", "source", sourceObj)
			return lpr.manageError(err)
		}
		sourceMap, err := getSubMapFromObject(sourceObj, objref.FieldPath)
		if err != nil {
			log.Error(err, "unable to retrieve", "field", objref.FieldPath, "from object", sourceObj)
			return lpr.manageError(err)
		}
		sourceMaps = append(sourceMaps, sourceMap)
	}

	//compute the template
	var b bytes.Buffer
	err = lpr.patch.Template.Execute(&b, sourceMaps)
	if err != nil {
		log.Error(err, "unable to process ", "template ", lpr.patch.Template, "parameters", sourceMaps)
		return lpr.manageError(err)
	}
	//log.Info("processed", "template", b.String())
	// convert the patch to from yaml to json
	bb, err := yaml.YAMLToJSON(b.Bytes())

	if err != nil {
		log.Error(err, "unable to convert to json", "processed template", b.String())
		return lpr.manageError(err)
	}
	//log.Info("json", "patch", string(bb))
	//apply the patch

	patch := client.ConstantPatch(lpr.patch.PatchType, bb)

	err = lpr.GetClient().Patch(context.TODO(), targetObj, patch)

	if err != nil {
		log.Error(err, "unable to apply ", "patch", patch, "on target", targetObj)
		return lpr.manageError(err)
	}

	return lpr.manageSuccess()
}

//GetKey return the patch no so unique identifier
func (lpr *LockedPatchReconciler) GetKey() string {
	return lpr.patch.GetKey()
}

func (lpr *LockedPatchReconciler) getReferecedObject(objref *corev1.ObjectReference) (*unstructured.Unstructured, error) {
	var ri dynamic.ResourceInterface
	res, err := lpr.getAPIReourceForGVK(schema.FromAPIVersionAndKind(objref.APIVersion, objref.Kind))
	if err != nil {
		log.Error(err, "unable to get resourceAPI ", "objectref", objref)
		return &unstructured.Unstructured{}, err
	}
	nri, err := lpr.GetDynamicClientOnAPIResource(res)
	if err != nil {
		log.Error(err, "unable to get dynamicClient on ", "resourceAPI", res)
		return &unstructured.Unstructured{}, err
	}
	if res.Namespaced {
		ri = nri.Namespace(objref.Namespace)
	} else {
		ri = nri
	}
	obj, err := ri.Get(context.TODO(), objref.Name, metav1.GetOptions{})
	if err != nil {
		log.Error(err, "unable to get referenced ", "object", objref)
		return &unstructured.Unstructured{}, err
	}
	return obj, nil
}

func getSubMapFromObject(obj *unstructured.Unstructured, fieldPath string) (interface{}, error) {
	if fieldPath == "" {
		return obj.UnstructuredContent(), nil
	}

	jp := jsonpath.New("fieldPath:" + fieldPath)
	err := jp.Parse("{" + fieldPath + "}")
	if err != nil {
		log.Error(err, "unable to parse ", "fieldPath", fieldPath)
		return nil, err
	}

	values, err := jp.FindResults(obj.UnstructuredContent())
	if err != nil {
		log.Error(err, "unable to apply ", "jsonpath", jp, " to obj ", obj.UnstructuredContent())
		return nil, err
	}

	if len(values) > 0 && len(values[0]) > 0 {
		return values[0][0].Interface(), nil
	}

	return nil, errors.New("jsonpath returned empty result")
}

func (lpr *LockedPatchReconciler) getAPIReourceForGVK(gvk schema.GroupVersionKind) (metav1.APIResource, error) {
	res := metav1.APIResource{}
	discoveryClient, err := lpr.GetDiscoveryClient()
	if err != nil {
		log.Error(err, "unable to create discovery client")
		return res, err
	}
	resList, err := discoveryClient.ServerResourcesForGroupVersion(gvk.GroupVersion().String())
	if err != nil {
		log.Error(err, "unable to retrieve resouce list for:", "groupversion", gvk.GroupVersion().String())
		return res, err
	}
	for _, resource := range resList.APIResources {
		if resource.Kind == gvk.Kind && !strings.Contains(resource.Name, "/") {
			res = resource
			res.Namespaced = resource.Namespaced
			res.Group = gvk.Group
			res.Version = gvk.Version
			break
		}
	}
	return res, nil
}

var jsonRegexp = regexp.MustCompile(`^\{\.?([^{}]+)\}$|^\.?([^{}]+)$`)

// relaxedJSONPathExpression attempts to be flexible with JSONPath expressions, it accepts:
//   * metadata.name (no leading '.' or curly braces '{...}'
//   * {metadata.name} (no leading '.')
//   * .metadata.name (no curly braces '{...}')
//   * {.metadata.name} (complete expression)
// And transforms them all into a valid jsonpath expression:
//   {.metadata.name}
func relaxedJSONPathExpression(pathExpression string) (string, error) {
	if len(pathExpression) == 0 {
		return pathExpression, nil
	}
	submatches := jsonRegexp.FindStringSubmatch(pathExpression)
	if submatches == nil {
		return "", fmt.Errorf("unexpected path string, expected a 'name1.name2' or '.name1.name2' or '{name1.name2}' or '{.name1.name2}'")
	}
	if len(submatches) != 3 {
		return "", fmt.Errorf("unexpected submatch list: %v", submatches)
	}
	var fieldSpec string
	if len(submatches[1]) != 0 {
		fieldSpec = submatches[1]
	} else {
		fieldSpec = submatches[2]
	}
	return fmt.Sprintf("{.%s}", fieldSpec), nil
}

func (lpr *LockedPatchReconciler) manageError(err error) (reconcile.Result, error) {
	condition := status.Condition{
		Type:               "ReconcileError",
		LastTransitionTime: metav1.Now(),
		Message:            err.Error(),
		Reason:             astatus.FailedReason,
		Status:             corev1.ConditionTrue,
	}
	lpr.setStatus(status.NewConditions(condition))
	return reconcile.Result{}, err
}

func (lpr *LockedPatchReconciler) manageSuccess() (reconcile.Result, error) {
	condition := status.Condition{
		Type:               "ReconcileSuccess",
		LastTransitionTime: metav1.Now(),
		Message:            astatus.SuccessfulMessage,
		Reason:             astatus.SuccessfulReason,
		Status:             corev1.ConditionTrue,
	}
	lpr.setStatus(status.NewConditions(condition))
	return reconcile.Result{}, nil
}

func (lpr *LockedPatchReconciler) setStatus(status status.Conditions) {
	lpr.statusLock.Lock()
	defer lpr.statusLock.Unlock()
	lpr.status = status
	if lpr.statusChange != nil {
		lpr.statusChange <- event.GenericEvent{
			Meta: lpr.parentObject,
		}
	}
}

//GetStatus returns the status for this reconciler
func (lpr *LockedPatchReconciler) GetStatus() status.Conditions {
	lpr.statusLock.Lock()
	defer lpr.statusLock.Unlock()
	return lpr.status
}
