package lockedresourcecontroller

import (
	"context"
	"errors"

	astatus "github.com/operator-framework/operator-sdk/pkg/ansible/controller/status"
	"github.com/operator-framework/operator-sdk/pkg/status"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedresource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// EnforcingReconciler is a reconciler designed to as a base type to extend for those operators that compute a set of resources that then need to be kept in place (i.e. enforced)
// the enforcing piece is taken care for, an implementor would just neeed to take care of the logic that computes the resorces to be enforced.
type EnforcingReconciler struct {
	util.ReconcilerBase
	lockedResourceManagers map[string]*LockedResourceManager
	statusChange           chan event.GenericEvent
}

//NewEnforcingReconciler creates a new EnforcingReconciler
func NewEnforcingReconciler(client client.Client, scheme *runtime.Scheme, restConfig *rest.Config, recorder record.EventRecorder) EnforcingReconciler {
	return EnforcingReconciler{
		ReconcilerBase:         util.NewReconcilerBase(client, scheme, restConfig, recorder),
		lockedResourceManagers: map[string]*LockedResourceManager{},
		statusChange:           make(chan event.GenericEvent),
	}
}

//GetStatusChangeChannel returns the channel thoughr which status change events can be received
func (er *EnforcingReconciler) GetStatusChangeChannel() <-chan event.GenericEvent {
	return er.statusChange
}

func (er *EnforcingReconciler) getLockedResourceManager(instance metav1.Object) (*LockedResourceManager, error) {
	lockedResourceManager, ok := er.lockedResourceManagers[apis.GetKeyShort(instance)]
	if !ok {
		lockedResourceManager, err := NewLockedResourceManager(er.GetRestConfig(), manager.Options{}, instance, er.statusChange)
		if err != nil {
			log.Error(err, "unable to create LockedResourceManager")
			return &LockedResourceManager{}, err
		}
		er.lockedResourceManagers[apis.GetKeyShort(instance)] = &lockedResourceManager
		return &lockedResourceManager, nil
	}
	return lockedResourceManager, nil
}

// UpdateLockedResources will do the following:
// 1. initialize or retrieve the LockedResourceManager related to the passed parent resource
// 2. compare the currently enfrced resources with the one passed as parameters and then
//    a. return immediately if they are the same
//    b. restart the LockedResourceManager if they don't match
func (er *EnforcingReconciler) UpdateLockedResources(instance metav1.Object, lockedResources []lockedresource.LockedResource) error {
	lockedResourceManager, err := er.getLockedResourceManager(instance)
	if err != nil {
		log.Error(err, "unable to get LockedResourceManager")
		return err
	}
	same, leftDifference, _, _ := lockedResourceManager.IsSameResources(lockedResources)
	if !same {
		lockedResourceManager.Restart(lockedResources, false)
		err := er.DeleteUnstructuredResources(lockedresource.AsListOfUnstructured(leftDifference))
		if err != nil {
			log.Error(err, "unable to delete unmanaged", "resources", leftDifference)
			return err
		}
	}
	return nil
}

//ManageError manage error sets an error status in the CR and fires an event, finally it returns the error so the operator can re-attempt
func (er *EnforcingReconciler) ManageError(instance metav1.Object, issue error) (reconcile.Result, error) {
	runtimeObj, ok := (instance).(runtime.Object)
	if !ok {
		log.Error(errors.New("not a runtime.Object"), "passed object was not a runtime.Object", "object", instance)
		return reconcile.Result{}, nil
	}
	er.GetRecorder().Event(runtimeObj, "Warning", "ProcessingError", issue.Error())
	if enforcingReconcileStatusAware, updateStatus := (instance).(apis.EnforcingReconcileStatusAware); updateStatus {
		condition := status.Condition{
			Type:               "ReconcileError",
			LastTransitionTime: metav1.Now(),
			Message:            issue.Error(),
			Reason:             astatus.FailedReason,
			Status:             corev1.ConditionTrue,
		}
		status := apis.EnforcingReconcileStatus{
			Conditions:             status.NewConditions(condition),
			LockedResourceStatuses: er.GetLockedResourceStatuses(instance),
		}
		enforcingReconcileStatusAware.SetEnforcingReconcileStatus(status)
		err := er.GetClient().Status().Update(context.Background(), runtimeObj)
		if err != nil {
			log.Error(err, "unable to update status for", "object", runtimeObj)
			return reconcile.Result{}, err
		}
	} else {
		log.V(1).Info("object is not RecocileStatusAware, not setting status")
	}
	return reconcile.Result{}, issue
}

// ManageSuccess will update the status of the CR and return a successful reconcile result
func (er *EnforcingReconciler) ManageSuccess(instance metav1.Object) (reconcile.Result, error) {
	runtimeObj, ok := (instance).(runtime.Object)
	if !ok {
		err := errors.New("not a runtime.Object")
		log.Error(err, "passed object was not a runtime.Object", "object", instance)
		return reconcile.Result{}, err
	}
	if enforcingReconcileStatusAware, updateStatus := (instance).(apis.EnforcingReconcileStatusAware); updateStatus {
		condition := status.Condition{
			Type:               "ReconcileSuccess",
			LastTransitionTime: metav1.Now(),
			Message:            astatus.SuccessfulMessage,
			Reason:             astatus.SuccessfulReason,
			Status:             corev1.ConditionTrue,
		}
		status := apis.EnforcingReconcileStatus{
			Conditions:             status.NewConditions(condition),
			LockedResourceStatuses: er.GetLockedResourceStatuses(instance),
		}
		enforcingReconcileStatusAware.SetEnforcingReconcileStatus(status)
		err := er.GetClient().Status().Update(context.Background(), runtimeObj)
		if err != nil {
			log.Error(err, "unable to update status for", "object", runtimeObj)
			return reconcile.Result{}, err
		}
	} else {
		log.V(1).Info("object is not RecocileStatusAware, not setting status")
	}
	return reconcile.Result{}, nil
}

// GetLockedResourceStatuses returns the status for all LockedResources
func (er *EnforcingReconciler) GetLockedResourceStatuses(instance metav1.Object) map[string]status.Conditions {
	lockedResourceManager, err := er.getLockedResourceManager(instance)
	if err != nil {
		log.Error(err, "unable to get locked resource manager for", "parent", instance)
		return map[string]status.Conditions{}
	}
	lockedResourceReconcileStatuses := map[string]status.Conditions{}
	for _, lockedResourceReconciler := range lockedResourceManager.GetResourceReconcilers() {
		lockedResourceReconcileStatuses[apis.GetKeyLong(&lockedResourceReconciler.Resource)] = lockedResourceReconciler.GetStatus()
	}
	return lockedResourceReconcileStatuses
}

// Terminate will stop the execution for the current istance. It will also optionally delete the locked resources.
func (er *EnforcingReconciler) Terminate(instance metav1.Object, deleteResources bool) error {
	lockedResourceManager, err := er.getLockedResourceManager(instance)
	if err != nil {
		log.V(1).Info("unable to get locked resource manager for", "parent", instance)
		return err
	}
	err = lockedResourceManager.Stop(deleteResources)
	if err != nil {
		log.V(1).Info("unable to stop ", "lockedResourceManager", lockedResourceManager)
		return err
	}
	return nil
}
