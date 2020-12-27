package lockedresourcecontroller

import (
	"context"
	"reflect"
	"sync"

	"encoding/json"

	"github.com/go-logr/logr"

	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedresource"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// LockedResourceReconciler is a reconciler that will lock down a resource to prevent changes from external events.
// This reconciler can be configured to ignore a set of json path. Changed occurring on the ignored path will be ignored, and therefore allowed by the reconciler
type LockedResourceReconciler struct {
	Resource     unstructured.Unstructured
	ExcludePaths []string
	util.ReconcilerBase
	status         []metav1.Condition
	statusChange   chan<- event.GenericEvent
	statusLock     sync.Mutex
	parentObject   client.Object
	creationFailed chan event.GenericEvent
	log            logr.Logger
}

// NewLockedObjectReconciler returns a new reconcile.Reconciler
func NewLockedObjectReconciler(mgr manager.Manager, object unstructured.Unstructured, excludePaths []string, statusChange chan<- event.GenericEvent, parentObject client.Object) (*LockedResourceReconciler, error) {

	controllername := "resource-reconciler"

	reconciler := &LockedResourceReconciler{
		log:            ctrl.Log.WithName(controllername).WithName(apis.GetKeyShort(parentObject)).WithName(apis.GetKeyLong(&object)),
		ReconcilerBase: util.NewReconcilerBase(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetEventRecorderFor(controllername+"_"+apis.GetKeyLong(&object))),
		Resource:       object,
		ExcludePaths:   excludePaths,
		statusChange:   statusChange,
		parentObject:   parentObject,
		statusLock:     sync.Mutex{},
		creationFailed: make(chan event.GenericEvent),
		status: []metav1.Condition([]metav1.Condition{{
			Type:               "Initializing",
			LastTransitionTime: metav1.Now(),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: object.GetGeneration(),
			Reason:             "ReconcilerManagerRestarting",
		}}),
	}

	err := reconciler.CreateOrUpdateResource(context.TODO(), nil, "", object.DeepCopy())
	if err != nil {
		reconciler.log.Error(err, "unable to create or update", "resource", object)
		reconciler.manageErrorNoInstance(err)
		go func() {
			reconciler.creationFailed <- event.GenericEvent{
				Object: &object,
			}
		}()
	}

	controller, err := controller.New("controller_locked_object_"+apis.GetKeyLong(&object), mgr, controller.Options{Reconciler: reconciler})
	if err != nil {
		reconciler.log.Error(err, "unable to create new controller", "with reconciler", reconciler)
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
		reconciler.log.Error(err, "unable to create new watch", "with source", object)
		return &LockedResourceReconciler{}, err
	}

	err = controller.Watch(
		&source.Channel{Source: reconciler.creationFailed},
		&handler.EnqueueRequestForObject{},
	)
	if err != nil {
		return &LockedResourceReconciler{}, err
	}

	return reconciler, nil
}

// Reconcile contains the reconcile logic for LockedResourceReconciler
func (lor *LockedResourceReconciler) Reconcile(context context.Context, request reconcile.Request) (reconcile.Result, error) {
	lor.log.Info("reconcile called for", "object", apis.GetKeyLong(&lor.Resource), "request", request)
	//err := lor.CreateOrUpdateResource(nil, "", &lor.Object)

	// Fetch the  instance
	//instance := &unstructured.Unstructured{}
	client, err := lor.GetDynamicClientOnUnstructured(lor.Resource)
	if err != nil {
		lor.log.Error(err, "unable to get dynamicClient", "on object", lor.Resource)
		return lor.manageErrorNoInstance(err)
	}
	instance, err := client.Get(context, lor.Resource.GetName(), v1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// if not found we have to recreate it.
			err = lor.CreateOrUpdateResource(context, nil, "", lor.Resource.DeepCopy())
			if err != nil {
				lor.log.Error(err, "unable to create or update", "object", lor.Resource)
				return lor.manageErrorNoInstance(err)
			}
			return lor.manageSuccessNoInstance()
		}
		// Error reading the object - requeue the request.
		lor.log.Error(err, "unable to lookup", "object", lor.Resource)
		return lor.manageError(instance, err)
	}
	lor.log.V(1).Info("determining if resources are equal", "desired", lor.Resource, "current", instance)
	equal, err := lor.isEqual(instance)
	if err != nil {
		lor.log.Error(err, "unable to determine if", "object", lor.Resource, "is equal to object", instance)
		return lor.manageError(instance, err)
	}
	if !equal {
		lor.log.V(1).Info("determined that resources are NOT equal")
		patch, err := lockedresource.FilterOutPaths(&lor.Resource, lor.ExcludePaths)
		if err != nil {
			lor.log.Error(err, "unable to filter out ", "excluded paths", lor.ExcludePaths, "from object", lor.Resource)
			return lor.manageError(instance, err)
		}
		patchBytes, err := json.Marshal(patch)
		if err != nil {
			lor.log.Error(err, "unable to marshall ", "object", patch)
			return lor.manageError(instance, err)
		}
		lor.log.V(1).Info("executing", "patch", string(patchBytes), "on object", instance)
		_, err = client.Patch(context, instance.GetName(), types.MergePatchType, patchBytes, metav1.PatchOptions{})
		if err != nil {
			lor.log.Error(err, "unable to patch ", "object", instance, "with patch", string(patchBytes))
			return lor.manageError(instance, err)
		}
		return lor.manageSuccess(instance)
	}
	lor.log.V(1).Info("determined that resources are equal")
	return lor.manageSuccess(instance)
}

func (lor *LockedResourceReconciler) isEqual(instance *unstructured.Unstructured) (bool, error) {
	left, err := lockedresource.FilterOutPaths(&lor.Resource, lor.ExcludePaths)
	lor.log.V(1).Info("resource", "desired", left)
	if err != nil {
		return false, err
	}
	right, err := lockedresource.FilterOutPaths(instance, lor.ExcludePaths)
	if err != nil {
		return false, err
	}
	lor.log.V(1).Info("resource", "current", right)
	return reflect.DeepEqual(left, right), nil
}

type resourceModifiedPredicate struct {
	name      string
	namespace string
	lrr       *LockedResourceReconciler
	predicate.Funcs
}

// Update implements default UpdateEvent filter for validating resource version change
func (p *resourceModifiedPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectNew.GetNamespace() == p.namespace && e.ObjectNew.GetName() == p.name {
		return true
	}
	return false
}

func (p *resourceModifiedPredicate) Create(e event.CreateEvent) bool {
	if e.Object.GetNamespace() == p.namespace && e.Object.GetName() == p.name {
		return true
	}
	return false
}

func (p *resourceModifiedPredicate) Delete(e event.DeleteEvent) bool {
	if e.Object.GetNamespace() == p.namespace && e.Object.GetName() == p.name {
		// we return true only if the enclosing namespace is not also being deleted
		if e.Object.GetNamespace() != "" {
			namespace := corev1.Namespace{}
			err := p.lrr.GetClient().Get(context.TODO(), types.NamespacedName{Name: e.Object.GetNamespace()}, &namespace)
			if err != nil {
				p.lrr.log.Error(err, "unable to retrieve ", "namespace", "e.Meta.GetNamespace()")
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

func (lor *LockedResourceReconciler) manageError(instance *unstructured.Unstructured, err error) (reconcile.Result, error) {
	condition := metav1.Condition{
		Type:               apis.ReconcileError,
		LastTransitionTime: metav1.Now(),
		Message:            err.Error(),
		Reason:             apis.ReconcileErrorReason,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: instance.GetGeneration(),
	}
	lor.setStatus(apis.AddOrReplaceCondition(condition, lor.GetStatus()))
	return reconcile.Result{}, err
}

func (lor *LockedResourceReconciler) manageErrorNoInstance(err error) (reconcile.Result, error) {
	condition := metav1.Condition{
		Type:               apis.ReconcileError,
		LastTransitionTime: metav1.Now(),
		Message:            err.Error(),
		Reason:             apis.ReconcileErrorReason,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 0,
	}
	lor.setStatus(apis.AddOrReplaceCondition(condition, lor.GetStatus()))
	return reconcile.Result{}, err
}

func (lor *LockedResourceReconciler) manageSuccess(instance *unstructured.Unstructured) (reconcile.Result, error) {
	condition := metav1.Condition{
		Type:               apis.ReconcileSuccess,
		LastTransitionTime: metav1.Now(),
		Reason:             apis.ReconcileSuccessReason,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: instance.GetGeneration(),
	}
	lor.setStatus(apis.AddOrReplaceCondition(condition, lor.GetStatus()))
	return reconcile.Result{}, nil
}

func (lor *LockedResourceReconciler) manageSuccessNoInstance() (reconcile.Result, error) {
	condition := metav1.Condition{
		Type:               apis.ReconcileSuccess,
		LastTransitionTime: metav1.Now(),
		Reason:             apis.ReconcileSuccessReason,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 0,
	}
	lor.setStatus(apis.AddOrReplaceCondition(condition, lor.GetStatus()))
	return reconcile.Result{}, nil
}

func (lor *LockedResourceReconciler) setStatus(status []metav1.Condition) {
	lor.statusLock.Lock()
	defer lor.statusLock.Unlock()
	lor.status = status
	if lor.statusChange != nil {
		lor.statusChange <- event.GenericEvent{
			Object: lor.parentObject,
		}
	}
}

// GetStatus returns the latest reconcile status
func (lor *LockedResourceReconciler) GetStatus() []metav1.Condition {
	lor.statusLock.Lock()
	defer lor.statusLock.Unlock()
	status := lor.status
	return status
}
