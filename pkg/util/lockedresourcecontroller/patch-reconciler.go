package lockedresourcecontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	utilsapi "github.com/redhat-cop/operator-utils/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedpatch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/jsonpath"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sigs.k8s.io/yaml"
)

//LockedPatchReconciler is a reconciler that can enforce a LockedPatch
type LockedPatchReconciler struct {
	util.ReconcilerBase
	patch        lockedpatch.LockedPatch
	status       map[string][]metav1.Condition
	statusChange chan<- event.GenericEvent
	parentObject client.Object
	statusLock   sync.Mutex
	log          logr.Logger
}

//NewLockedPatchReconciler returns a new reconcile.Reconciler
func NewLockedPatchReconciler(mgr manager.Manager, patch lockedpatch.LockedPatch, statusChange chan<- event.GenericEvent, parentObject client.Object) (*LockedPatchReconciler, error) {

	// TODO create the object is it does not exists
	controllername := "patch-reconciler"

	reconciler := &LockedPatchReconciler{
		log:            ctrl.Log.WithName(controllername).WithName(apis.GetKeyShort(parentObject)).WithName(patch.GetKey()),
		ReconcilerBase: util.NewFromManager(mgr, mgr.GetEventRecorderFor(controllername+"_"+patch.GetKey())),
		patch:          patch,
		statusChange:   statusChange,
		parentObject:   parentObject,
		statusLock:     sync.Mutex{},
		status: map[string][]metav1.Condition{
			"reconciler": []metav1.Condition([]metav1.Condition{{
				Type:               "Initializing",
				LastTransitionTime: metav1.Now(),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 0,
				Reason:             "ReconcilerManagerRestarting",
			}}),
		},
	}

	controller, err := controller.New(controllername+"_"+patch.GetKey(), mgr, controller.Options{Reconciler: reconciler})
	if err != nil {
		return &LockedPatchReconciler{}, err
	}

	//create watcher for target
	obj := targetObjectRefToRuntimeType(&patch.TargetObjectRef)
	mgr.GetScheme().AddKnownTypes(schema.FromAPIVersionAndKind(patch.TargetObjectRef.APIVersion, patch.TargetObjectRef.Kind).GroupVersion(), obj)

	err = controller.Watch(&source.Kind{Type: obj}, &handler.EnqueueRequestForObject{}, &targetReferenceModifiedPredicate{
		TargetObjectReference: patch.TargetObjectRef,
		log:                   reconciler.log.WithName("target-watcher"),
		restConfig:            mgr.GetConfig(),
	})
	if err != nil {
		return &LockedPatchReconciler{}, err
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return &LockedPatchReconciler{}, err
	}
	for _, sourceRef := range patch.SourceObjectRefs {
		obj := sourceObjectRefToRuntimeType(&sourceRef)
		mgr.GetScheme().AddKnownTypes(schema.FromAPIVersionAndKind(sourceRef.APIVersion, sourceRef.Kind).GroupVersion(), obj)
		err = controller.Watch(&source.Kind{Type: obj}, &enqueueRequestForPatch{
			source:          &sourceRef,
			target:          &patch.TargetObjectRef,
			discoveryClient: discoveryClient,
			restConfig:      mgr.GetConfig(),
			log:             reconciler.log.WithName(sourceRef.APIVersion + "/" + sourceRef.Kind + "/" + sourceRef.Namespace + "/" + sourceRef.Name).WithName("source-event-handler"),
		}, &sourceReferenceModifiedPredicate{
			log:        reconciler.log.WithName(sourceRef.APIVersion + "/" + sourceRef.Kind + "/" + sourceRef.Namespace + "/" + sourceRef.Name).WithName("source-event-filter"),
			source:     &sourceRef,
			target:     &patch.TargetObjectRef,
			restConfig: mgr.GetConfig(),
		})
		if err != nil {
			return &LockedPatchReconciler{}, err
		}
	}

	return reconciler, nil
}

func sourceObjectRefToRuntimeType(objref *utilsapi.SourceObjectReference) client.Object {
	obj := &unstructured.Unstructured{}
	obj.SetKind(objref.Kind)
	obj.SetAPIVersion(objref.APIVersion)
	return obj
}

func targetObjectRefToRuntimeType(objref *utilsapi.TargetObjectReference) client.Object {
	obj := &unstructured.Unstructured{}
	obj.SetKind(objref.Kind)
	obj.SetAPIVersion(objref.APIVersion)
	return obj
}

type enqueueRequestForPatch struct {
	source          *utilsapi.SourceObjectReference
	target          *utilsapi.TargetObjectReference
	discoveryClient *discovery.DiscoveryClient
	restConfig      *rest.Config
	log             logr.Logger
}

func (e *enqueueRequestForPatch) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	//to see if this event is relevant and we have to do the following:
	// 1. see if the target is single or multiple
	// 2. if single just see if it matches, the pass the event.
	// 3. if multiple see which macth and then pass the event
	e.log.V(1).Info("enqueue create", "for", evt.Object)
	ctx := context.TODO()
	ctx = context.WithValue(ctx, "restConfig", e.restConfig)
	ctx = log.IntoContext(ctx, e.log)
	multiple, _, err := e.target.IsSelectingMultipleInstances(ctx)
	if err != nil {
		e.log.Error(err, "Unable to determine if target resolves to multiple instance", "target", e.target)
		return
	}
	if !multiple {
		obj, err := e.target.GetReferencedObject(ctx)
		if err != nil {
			e.log.Error(err, "Unable to get referenced object", "target", e.target)
			return
		}
		sourceName, sourceNamespace, err := e.source.GetNameAndNamespace(ctx, obj)
		if err != nil {
			e.log.Error(err, "Unable to process name and namespace templates", "source", e.source, "param", obj)
			return
		}
		if sourceName == evt.Object.GetName() && sourceNamespace == evt.Object.GetNamespace() {
			e.log.V(1).Info("enqueing", "request", reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      e.target.Name,
					Namespace: e.target.Namespace,
				},
			})
			q.Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      e.target.Name,
					Namespace: e.target.Namespace,
				},
			})
		}
		return
	}

	if multiple {
		objs, err := e.target.GetReferencedObjects(ctx)
		if err != nil {
			e.log.Error(err, "Unable to get referenced objects", "target", e.target)
			return
		}
		for i := range objs {
			sourceName, sourceNamespace, err := e.source.GetNameAndNamespace(ctx, &objs[i])
			if err != nil {
				e.log.Error(err, "Unable to process name and namespace templates", "source", e.source, "param", objs[i])
				return
			}
			if sourceName == evt.Object.GetName() && sourceNamespace == evt.Object.GetNamespace() {
				e.log.V(1).Info("enqueing", "request", reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      objs[i].GetName(),
						Namespace: objs[i].GetNamespace(),
					},
				})
				q.Add(reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      objs[i].GetName(),
						Namespace: objs[i].GetNamespace(),
					},
				})
			}
		}
	}

}

// Update implements EventHandler
func (e *enqueueRequestForPatch) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	//to see if this event is relevant and we have to do the following:
	// 1. see if the target is single or multiple
	// 2. if single just see if it matches, the pass the event.
	// 3. if multiple see which macth and then pass the event
	// TODO  this could be optmized to see if the change affected the needed jsonpath
	e.log.V(1).Info("enqueue update", "for", evt.ObjectNew)
	ctx := context.TODO()
	ctx = context.WithValue(ctx, "discoveryClient", e.discoveryClient)
	ctx = context.WithValue(ctx, "restConfig", e.restConfig)
	ctx = log.IntoContext(ctx, e.log)
	multiple, _, err := e.target.IsSelectingMultipleInstances(ctx)
	if err != nil {
		e.log.Error(err, "Unable to determine if target resolves to multiple instance", "target", e.target)
		return
	}

	if !multiple {
		obj, err := e.target.GetReferencedObject(ctx)
		if err != nil {
			e.log.Error(err, "Unable to get referenced object", "target", e.target)
			return
		}
		sourceName, sourceNamespace, err := e.source.GetNameAndNamespace(ctx, obj)
		if err != nil {
			e.log.Error(err, "Unable to process name and namespace templates", "source", e.source, "param", obj)
			return
		}
		if sourceName == evt.ObjectNew.GetName() && sourceNamespace == evt.ObjectNew.GetNamespace() {
			q.Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      e.target.Name,
					Namespace: e.target.Namespace,
				},
			})
		}
		return
	}

	if multiple {
		objs, err := e.target.GetReferencedObjects(ctx)
		if err != nil {
			e.log.Error(err, "Unable to get referenced objects", "target", e.target)
			return
		}
		for i := range objs {
			sourceName, sourceNamespace, err := e.source.GetNameAndNamespace(ctx, &objs[i])
			if err != nil {
				e.log.Error(err, "Unable to process name and namespace templates", "source", e.source, "param", objs[i])
				return
			}
			if sourceName == evt.ObjectNew.GetName() && sourceNamespace == evt.ObjectNew.GetNamespace() {
				q.Add(reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      objs[i].GetName(),
						Namespace: objs[i].GetNamespace(),
					},
				})
			}
		}
	}
}

// Delete implements EventHandler
func (e *enqueueRequestForPatch) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
}

// Generic implements EventHandler
func (e *enqueueRequestForPatch) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
}

type sourceReferenceModifiedPredicate struct {
	source     *utilsapi.SourceObjectReference
	target     *utilsapi.TargetObjectReference
	restConfig *rest.Config
	log        logr.Logger
}

// Update implements default UpdateEvent filter for validating resource version change
func (p *sourceReferenceModifiedPredicate) Update(e event.UpdateEvent) bool {
	//TODO can be optimized by calculating whether we are selecting multiple objects
	p.log.V(1).Info("filter update", "for", e.ObjectNew)
	ctx := context.TODO()
	ctrl.LoggerInto(ctx, p.log)
	return p.isRelevant(e.ObjectNew) && !compareSourceObjects(ctx, p.source, e.ObjectNew, e.ObjectOld)
}

func (p *sourceReferenceModifiedPredicate) Create(e event.CreateEvent) bool {
	//TODO can be optimized by calculating whether we are selecting multiple objects
	p.log.V(1).Info("filter create", "for", e.Object)
	return p.isRelevant(e.Object)
}

func (p *sourceReferenceModifiedPredicate) isRelevant(obj client.Object) bool {
	// we need to aggressively filter events.
	// if name and namespaces are not templates, we can check the object
	if !strings.Contains(p.source.Name, "{{") && !strings.Contains(p.source.Namespace, "{{") {
		return obj.GetName() == p.source.Name && obj.GetNamespace() == p.source.Namespace
	}
	// if target is not selecting multiple instances then we can resolve the templates and test the object
	ctx := context.TODO()
	ctrl.LoggerInto(ctx, p.log)
	ctx = context.WithValue(ctx, "restConfig", p.restConfig)
	multiple, _, err := p.target.IsSelectingMultipleInstances(ctx)
	if err != nil {
		p.log.Error(err, "unable to determine if target object selects multiple instances")
		return false
	}
	if !multiple {
		tobj, err := p.target.GetReferencedObject(ctx)
		if err != nil {
			p.log.Error(err, "unable to get target referenced obect")
			return false
		}
		name, namespace, err := p.source.GetNameAndNamespace(ctx, tobj)
		if err != nil {
			p.log.Error(err, "unable to get source name and namespace from target")
			return false
		}
		return name == obj.GetName() && namespace == obj.GetNamespace()
	}
	return true
}

func (p *sourceReferenceModifiedPredicate) Delete(e event.DeleteEvent) bool {
	// we ignore Delete events because if we loosed references there is no point in trying to recompute the patch
	return false
}

func (p *sourceReferenceModifiedPredicate) Generic(e event.GenericEvent) bool {
	// we ignore Generic events
	return false
}

type targetReferenceModifiedPredicate struct {
	utilsapi.TargetObjectReference
	restConfig *rest.Config
	log        logr.Logger
}

// Update implements default UpdateEvent filter for validating resource version change
func (p *targetReferenceModifiedPredicate) Update(e event.UpdateEvent) bool {
	p.log.V(1).Info("filter update", "for", e.ObjectNew)
	ctx := context.TODO()
	ctrl.LoggerInto(ctx, p.log)
	ctx = context.WithValue(ctx, "restConfig", p.restConfig)
	selected, err := p.TargetObjectReference.Selects(ctx, e.ObjectNew)
	if err != nil {
		p.log.Error(err, "unable to determine if current object is selected", "object", e.ObjectNew, "target", p.TargetObjectReference)
		return false
	}
	p.log.V(1).Info("", "selected", selected)
	if selected {
		return !compareObjectsWithoutIgnoredFields(e.ObjectNew, e.ObjectOld)
	}
	return false
}

func (p *targetReferenceModifiedPredicate) Create(e event.CreateEvent) bool {
	p.log.V(1).Info("filter create", "for", e.Object)
	ctx := context.TODO()
	ctrl.LoggerInto(ctx, p.log)
	ctx = context.WithValue(ctx, "restConfig", p.restConfig)
	selected, err := p.TargetObjectReference.Selects(ctx, e.Object)
	if err != nil {
		p.log.Error(err, "unable to determine if current object is selected", "object", e.Object, "target", p.TargetObjectReference)
		return false
	}
	return selected
}

func (p *targetReferenceModifiedPredicate) Delete(e event.DeleteEvent) bool {
	// we ignore Delete events because if we loosed references there is no point in trying to recompute the patch
	return false
}

func (p *targetReferenceModifiedPredicate) Generic(e event.GenericEvent) bool {
	// we ignore Generic events
	return false
}

// we ignore the fields of resourceVersion and managedFields
func compareObjectsWithoutIgnoredFields(changedObjSrc runtime.Object, originalObjSrc runtime.Object) bool {
	changedObj := changedObjSrc.DeepCopyObject().(*unstructured.Unstructured)
	originalObj := originalObjSrc.DeepCopyObject().(*unstructured.Unstructured)

	changedObj.SetManagedFields(nil)
	changedObj.SetResourceVersion("")
	originalObj.SetManagedFields(nil)
	originalObj.SetResourceVersion("")

	changedObjJSON, _ := json.Marshal(changedObj)
	originalObjJSON, _ := json.Marshal(originalObj)

	return (string(changedObjJSON) == string(originalObjJSON))
}

func compareSourceObjects(ctx context.Context, sourceObjectReference *utilsapi.SourceObjectReference, changedObjSrc runtime.Object, originalObjSrc runtime.Object) bool {
	if sourceObjectReference.FieldPath != "" {
		mlog := log.FromContext(ctx)
		changedUnstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(changedObjSrc)
		if err != nil {
			mlog.Error(err, "unable to convert runtime object to unstructured", "runtime object", changedObjSrc)
			return false
		}
		originalUnstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(originalObjSrc)
		if err != nil {
			mlog.Error(err, "unable to convert runtime object to unstructured", "runtime object", originalObjSrc)
			return false
		}
		changedObjSubMap, err := getSubMapFromObject(ctx, &unstructured.Unstructured{Object: changedUnstructuredObj}, sourceObjectReference.FieldPath)
		if err != nil {
			mlog.Error(err, "unable to convert get submap from unstructured", "fieldPath", sourceObjectReference.FieldPath, "unstructured", changedUnstructuredObj)
			return false
		}
		originalObjSubMap, err := getSubMapFromObject(ctx, &unstructured.Unstructured{Object: originalUnstructuredObj}, sourceObjectReference.FieldPath)
		if err != nil {
			mlog.Error(err, "unable to convert get submap from unstructured", "fieldPath", sourceObjectReference.FieldPath, "unstructured", originalUnstructuredObj)
			return false
		}
		return !reflect.DeepEqual(changedObjSubMap, originalObjSubMap)
	} else {
		return compareObjectsWithoutIgnoredFields(changedObjSrc, originalObjSrc)
	}
}

//Reconcile method
func (lpr *LockedPatchReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	//gather all needed the objects
	lpr.log.V(1).Info("reconcile", "for", request)
	ctx = context.WithValue(ctx, "restConfig", lpr.GetRestConfig())
	ctx = log.IntoContext(ctx, lpr.log)
	targetObj, err := lpr.patch.TargetObjectRef.GetReferencedObjectWithName(ctx, request.NamespacedName)
	if err != nil {
		lpr.log.Error(err, "unable to retrieve", "target", lpr.patch.TargetObjectRef)
		return lpr.manageErrorNoTarget(err)
	}
	// the first object is always the target object
	sourceMaps := []interface{}{targetObj.UnstructuredContent()}
	for i := range lpr.patch.SourceObjectRefs {
		sourceObj, err := lpr.patch.SourceObjectRefs[i].GetReferencedObject(ctx, targetObj)
		if err != nil {
			lpr.log.Error(err, "unable to retrieve", "sourceObjectRef", lpr.patch.SourceObjectRefs[i])
			return lpr.manageError(targetObj, err)
		}
		sourceMap, err := getSubMapFromObject(ctx, sourceObj, lpr.patch.SourceObjectRefs[i].FieldPath)
		if err != nil {
			lpr.log.Error(err, "unable to retrieve", "field", lpr.patch.SourceObjectRefs[i].FieldPath, "from object", sourceObj)
			return lpr.manageError(targetObj, err)
		}
		sourceMaps = append(sourceMaps, sourceMap)
	}

	//compute the template
	var b bytes.Buffer
	err = lpr.patch.Template.Execute(&b, sourceMaps)
	if err != nil {
		lpr.log.Error(err, "unable to process ", "template ", lpr.patch.Template, "parameters", sourceMaps)
		return lpr.manageError(targetObj, err)
	}

	bb, err := yaml.YAMLToJSON(b.Bytes())

	if err != nil {
		lpr.log.Error(err, "unable to convert to json", "processed template", b.String())
		return lpr.manageError(targetObj, err)
	}

	patch := client.RawPatch(lpr.patch.PatchType, bb)

	err = lpr.GetClient().Patch(ctx, targetObj, patch)

	if err != nil {
		lpr.log.Error(err, "unable to apply ", "patch", patch, "on target", targetObj)
		return lpr.manageError(targetObj, err)
	}

	return lpr.manageSuccess(targetObj)
}

//GetKey return the patch no so unique identifier
func (lpr *LockedPatchReconciler) GetKey() string {
	return lpr.patch.GetKey()
}

func getSubMapFromObject(ctx context.Context, obj *unstructured.Unstructured, fieldPath string) (interface{}, error) {
	mlog := log.FromContext(ctx)
	if fieldPath == "" {
		return obj.UnstructuredContent(), nil
	}

	jp := jsonpath.New("fieldPath:" + fieldPath)
	err := jp.Parse("{" + fieldPath + "}")
	if err != nil {
		mlog.Error(err, "unable to parse ", "fieldPath", fieldPath)
		return nil, err
	}

	values, err := jp.FindResults(obj.UnstructuredContent())
	if err != nil {
		mlog.Error(err, "unable to apply ", "jsonpath", jp, " to obj ", obj.UnstructuredContent())
		return nil, err
	}

	if len(values) > 0 && len(values[0]) > 0 {
		return values[0][0].Interface(), nil
	}

	return nil, errors.New("jsonpath returned empty result")
}

func (lpr *LockedPatchReconciler) manageError(target client.Object, err error) (reconcile.Result, error) {
	condition := metav1.Condition{
		Type:               apis.ReconcileError,
		LastTransitionTime: metav1.Now(),
		Message:            err.Error(),
		Reason:             apis.ReconcileErrorReason,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: target.GetGeneration(),
	}
	lpr.setStatus(apis.GetKeyShort(target), apis.AddOrReplaceCondition(condition, lpr.GetStatus()[apis.GetKeyShort(target)]))
	return reconcile.Result{}, err
}

func (lpr *LockedPatchReconciler) manageErrorNoTarget(err error) (reconcile.Result, error) {
	condition := metav1.Condition{
		Type:               apis.ReconcileError,
		LastTransitionTime: metav1.Now(),
		Message:            err.Error(),
		Reason:             apis.ReconcileErrorReason,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 0,
	}
	lpr.setStatus("reconciler", apis.AddOrReplaceCondition(condition, lpr.GetStatus()["reconciler"]))
	return reconcile.Result{}, err
}

func (lpr *LockedPatchReconciler) manageSuccess(target client.Object) (reconcile.Result, error) {
	condition := metav1.Condition{
		Type:               apis.ReconcileSuccess,
		LastTransitionTime: metav1.Now(),
		Reason:             apis.ReconcileSuccessReason,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: target.GetGeneration(),
	}
	lpr.setStatus(apis.GetKeyShort(target), apis.AddOrReplaceCondition(condition, lpr.GetStatus()[apis.GetKeyShort(target)]))
	return reconcile.Result{}, nil
}

func (lpr *LockedPatchReconciler) setStatus(key string, conditions []metav1.Condition) {
	lpr.statusLock.Lock()
	defer lpr.statusLock.Unlock()
	lpr.status[key] = conditions
	if lpr.statusChange != nil {
		lpr.statusChange <- event.GenericEvent{
			Object: lpr.parentObject,
		}
	}
}

//GetStatus returns the status for this reconciler
func (lpr *LockedPatchReconciler) GetStatus() map[string][]metav1.Condition {
	lpr.statusLock.Lock()
	defer lpr.statusLock.Unlock()
	return lpr.status
}
