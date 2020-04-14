package lockedresourcecontroller

import (
	"context"
	"reflect"

	"encoding/json"

	astatus "github.com/operator-framework/operator-sdk/pkg/ansible/controller/status"
	"github.com/operator-framework/operator-sdk/pkg/status"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("lockedresourcecontroller")

// LockedResourceReconciler is a reconciler that will lock down a resource based preventi changes from external events.
// This reconciler cna be configured to ignore a set og json path. Changed occuring on the ignored path will be ignored, and therefore allowed by the reconciler
type LockedResourceReconciler struct {
	Resource     unstructured.Unstructured
	ExcludePaths []string
	util.ReconcilerBase
	status       status.Conditions
	statusChange chan<- event.GenericEvent
	parentObject metav1.Object
}

// NewLockedObjectReconciler returns a new reconcile.Reconciler
func NewLockedObjectReconciler(mgr manager.Manager, object unstructured.Unstructured, excludePaths []string, statusChange chan<- event.GenericEvent, parentObject metav1.Object) (*LockedResourceReconciler, error) {

	reconciler := &LockedResourceReconciler{
		ReconcilerBase: util.NewReconcilerBase(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetEventRecorderFor("controller_locked_object_"+apis.GetKeyLong(&object))),
		Resource:       object,
		ExcludePaths:   excludePaths,
		statusChange:   statusChange,
		parentObject:   parentObject,
	}

	err := reconciler.CreateOrUpdateResource(nil, "", object.DeepCopy())
	if err != nil {
		log.Error(err, "unable to create or update", "resource", object)
		return &LockedResourceReconciler{}, err
	}

	controller, err := controller.New("controller_locked_object_"+apis.GetKeyLong(&object), mgr, controller.Options{Reconciler: reconciler})
	if err != nil {
		log.Error(err, "unable to create new controller", "with reconciler", reconciler)
		return &LockedResourceReconciler{}, err
	}

	gvk := object.GetObjectKind().GroupVersionKind()
	groupVersion := schema.GroupVersion{Group: gvk.Group, Version: gvk.Version}

	mgr.GetScheme().AddKnownTypes(groupVersion, &object)

	err = controller.Watch(&source.Kind{Type: &object}, &handler.EnqueueRequestForObject{}, &resourceModifiedPredicate{
		name:      object.GetName(),
		namespace: object.GetNamespace(),
		lrr:       reconciler,
	})
	if err != nil {
		log.Error(err, "unable to create new watch", "with source", object)
		return &LockedResourceReconciler{}, err
	}

	return reconciler, nil
}

// Reconcile contains the reconcile logic for LockedResourceReconciler
func (lor *LockedResourceReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log.Info("reconcile called for", "object", apis.GetKeyLong(&lor.Resource), "request", request)
	//err := lor.CreateOrUpdateResource(nil, "", &lor.Object)

	// Fetch the  instance
	//instance := &unstructured.Unstructured{}
	client, err := lor.GetDynamicClientOnUnstructured(lor.Resource)
	if err != nil {
		log.Error(err, "unable to get dynamicClient", "on object", lor.Resource)
		return lor.manageError(err)
	}
	instance, err := client.Get(lor.Resource.GetName(), v1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// if not found we have to recreate it.
			err = lor.CreateOrUpdateResource(nil, "", lor.Resource.DeepCopy())
			if err != nil {
				log.Error(err, "unable to create or update", "object", lor.Resource)
				lor.manageError(err)
			}
			return lor.manageSuccess()
		}
		// Error reading the object - requeue the request.
		log.Error(err, "unable to lookup", "object", lor.Resource)
		return lor.manageError(err)
	}
	log.Info("determining if resources are equal", "desired", lor.Resource, "current", instance)
	equal, err := lor.isEqual(instance)
	if err != nil {
		log.Error(err, "unable to determine if", "object", lor.Resource, "is equal to object", instance)
		return lor.manageError(err)
	}
	if !equal {
		log.Info("determined that resources are NOT equal")
		patch, err := filterOutPaths(&lor.Resource, lor.ExcludePaths)
		if err != nil {
			log.Error(err, "unable to filter out ", "excluded paths", lor.ExcludePaths, "from object", lor.Resource)
			return lor.manageError(err)
		}
		patchBytes, err := json.Marshal(patch)
		if err != nil {
			log.Error(err, "unable to marshall ", "object", patch)
			return lor.manageError(err)
		}
		log.Info("executing", "patch", string(patchBytes), "on object", instance)
		_, err = client.Patch(instance.GetName(), types.MergePatchType, patchBytes, metav1.PatchOptions{})
		if err != nil {
			log.Error(err, "unable to patch ", "object", instance, "with patch", string(patchBytes))
			return lor.manageError(err)
		}
		return lor.manageSuccess()
	}
	log.Info("determined that resources are equal")
	return lor.manageSuccess()
}

func (lor *LockedResourceReconciler) isEqual(instance *unstructured.Unstructured) (bool, error) {
	left, err := filterOutPaths(&lor.Resource, lor.ExcludePaths)
	log.Info("resource", "desired", left)
	if err != nil {
		return false, err
	}
	right, err := filterOutPaths(instance, lor.ExcludePaths)
	if err != nil {
		return false, err
	}
	log.Info("resource", "current", right)
	return reflect.DeepEqual(left, right), nil
}

// func getKeyFromObject(object *unstructured.Unstructured) string {
// 	return object.GroupVersionKind().String() + "/" + object.GetNamespace() + "/" + object.GetName()
// }

type resourceModifiedPredicate struct {
	name      string
	namespace string
	lrr       *LockedResourceReconciler
	predicate.Funcs
}

// Update implements default UpdateEvent filter for validating resource version change
func (p *resourceModifiedPredicate) Update(e event.UpdateEvent) bool {
	if e.MetaNew.GetNamespace() == p.namespace && e.MetaNew.GetName() == p.name {
		return true
	}
	return false
}

func (p *resourceModifiedPredicate) Create(e event.CreateEvent) bool {
	if e.Meta.GetNamespace() == p.namespace && e.Meta.GetName() == p.name {
		return true
	}
	return false
}

func (p *resourceModifiedPredicate) Delete(e event.DeleteEvent) bool {
	if e.Meta.GetNamespace() == p.namespace && e.Meta.GetName() == p.name {
		// we return true only if the enclosing namespace is not also being deleted
		if e.Meta.GetNamespace() != "" {
			namespace := corev1.Namespace{}
			err := p.lrr.GetClient().Get(context.TODO(), types.NamespacedName{Name: e.Meta.GetNamespace()}, &namespace)
			if err != nil {
				log.Error(err, "unable to retrieve ", "namespace", "e.Meta.GetNamespace()")
				return false
			}
			if util.IsBeingDeleted(&namespace) {
				return false
			}
		}
		return true
	}
	return false
}

func (lor *LockedResourceReconciler) manageError(err error) (reconcile.Result, error) {
	condition := status.Condition{
		Type:               "ReconcileError",
		LastTransitionTime: metav1.Now(),
		Message:            err.Error(),
		Reason:             astatus.FailedReason,
		Status:             corev1.ConditionTrue,
	}
	lor.setStatus(status.NewConditions(condition))
	return reconcile.Result{}, err
}

func (lor *LockedResourceReconciler) manageSuccess() (reconcile.Result, error) {
	condition := status.Condition{
		Type:               "ReconcileSuccess",
		LastTransitionTime: metav1.Now(),
		Message:            astatus.SuccessfulMessage,
		Reason:             astatus.SuccessfulReason,
		Status:             corev1.ConditionTrue,
	}
	lor.setStatus(status.NewConditions(condition))
	return reconcile.Result{}, nil
}

func (lor *LockedResourceReconciler) setStatus(status status.Conditions) {
	lor.status = status
	if lor.statusChange != nil {
		lor.statusChange <- event.GenericEvent{
			Meta: lor.parentObject,
		}
	}
}

// GetStatus returns the latest reconcile status
func (lor *LockedResourceReconciler) GetStatus() status.Conditions {
	return lor.status
}
