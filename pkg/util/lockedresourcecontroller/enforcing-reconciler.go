package lockedresourcecontroller

import (
	"context"
	"sync"

	astatus "github.com/operator-framework/operator-sdk/pkg/ansible/controller/status"
	"github.com/operator-framework/operator-sdk/pkg/status"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedpatch"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedresource"
	"github.com/scylladb/go-set/strset"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubernetes/staging/src/k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// EnforcingReconciler is a reconciler designed to as a base type to extend for those operators that compute a set of resources that then need to be kept in place (i.e. enforced)
// the enforcing piece is taken care for, an implementor would just neeed to take care of the logic that computes the resorces to be enforced.
type EnforcingReconciler struct {
	util.ReconcilerBase
	lockedResourceManagers      map[string]*LockedResourceManager
	statusChange                chan event.GenericEvent
	lockedResourceManagersMutex sync.Mutex
	clusterWatchers             bool
}

//NewEnforcingReconciler creates a new EnforcingReconciler
// clusterWatcher detemines whether the created watchers should be at the cluster level or namespace level.
// this affects the kind of permissions needed to run the controlelr
// also creating multiple namespace level permissions can create performance issue as one watch per object type per namespace is opened to the API server, if in doubt pass true here.
func NewEnforcingReconciler(client client.Client, scheme *runtime.Scheme, restConfig *rest.Config, recorder record.EventRecorder, clusterWatchers bool) EnforcingReconciler {
	return EnforcingReconciler{
		ReconcilerBase:              util.NewReconcilerBase(client, scheme, restConfig, recorder),
		lockedResourceManagers:      map[string]*LockedResourceManager{},
		statusChange:                make(chan event.GenericEvent),
		lockedResourceManagersMutex: sync.Mutex{},
		clusterWatchers:             clusterWatchers,
	}
}

//GetStatusChangeChannel returns the channel thoughr which status change events can be received
func (er *EnforcingReconciler) GetStatusChangeChannel() <-chan event.GenericEvent {
	return er.statusChange
}

func (er *EnforcingReconciler) removeLockedResourceManager(instance apis.Resource) {
	er.lockedResourceManagersMutex.Lock()
	defer er.lockedResourceManagersMutex.Unlock()
	delete(er.lockedResourceManagers, apis.GetKeyShort(instance))
}

func (er *EnforcingReconciler) getLockedResourceManager(instance apis.Resource) (*LockedResourceManager, error) {
	er.lockedResourceManagersMutex.Lock()
	defer er.lockedResourceManagersMutex.Unlock()
	lockedResourceManager, ok := er.lockedResourceManagers[apis.GetKeyShort(instance)]
	if !ok {
		lockedResourceManager, err := NewLockedResourceManager(er.GetRestConfig(), manager.Options{}, instance, er.statusChange, er.clusterWatchers)
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
func (er *EnforcingReconciler) UpdateLockedResources(instance apis.Resource, lockedResources []lockedresource.LockedResource, lockedPatches []lockedpatch.LockedPatch) error {
	return er.UpdateLockedResourcesWithRestConfig(instance, lockedResources, lockedPatches, er.GetRestConfig())
}

// UpdateLockedResourcesWithRestConfig will do the following:
// 1. initialize or retrieve the LockedResourceManager related to the passed parent resource
// 2. compare the currently enfrced resources with the one passed as parameters and then
//    a. return immediately if they are the same
//    b. restart the LockedResourceManager if they don't match
// this varian allow passing a rest config
func (er *EnforcingReconciler) UpdateLockedResourcesWithRestConfig(instance apis.Resource, lockedResources []lockedresource.LockedResource, lockedPatches []lockedpatch.LockedPatch, config *rest.Config) error {
	lockedResourceManager, err := er.getLockedResourceManager(instance)
	if err != nil {
		log.Error(err, "unable to get LockedResourceManager")
		return err
	}
	sameResources, leftDifference, _, _ := lockedResourceManager.IsSameResources(lockedResources)
	//the resource in the leftDifference are not necessarily to be deleted, we need to check if the resource has simply been updated maintinign the sam type/namespace/value.
	toBeDeleted := getToBeDeletdResources(lockedResources, leftDifference)
	log.V(1).Info("Is Same Resources", "", sameResources)
	samePatches, _, _, _ := lockedResourceManager.IsSamePatches(lockedPatches)
	log.V(1).Info("Is Same Patches", "", samePatches)
	if !sameResources || !samePatches {
		err = er.DeleteUnstructuredResources(lockedresource.AsListOfUnstructured(toBeDeleted))
		if err != nil {
			log.Error(err, "unable to delete unmanaged", "resources", leftDifference)
			return err
		}
		err := lockedResourceManager.Restart(lockedResources, lockedPatches, false, config)
		if err != nil {
			log.Error(err, "unable to restart", "manager", lockedResourceManager)
			return err
		}
	}
	return nil
}

func getToBeDeletdResources(neededResources []lockedresource.LockedResource, modifiedResources []lockedresource.LockedResource) []lockedresource.LockedResource {
	neededResourceSet := strset.New()
	modifiedResourcesSet := strset.New()
	modifiedResourceMap := map[string]lockedresource.LockedResource{}
	toBeDeleted := []lockedresource.LockedResource{}
	for _, lockerResource := range neededResources {
		neededResourceSet.Add(apis.GetKeyLong(&lockerResource))
	}
	for _, lockerResource := range modifiedResources {
		neededResourceSet.Add(apis.GetKeyLong(&lockerResource))
		modifiedResourceMap[apis.GetKeyLong(&lockerResource)] = lockerResource
	}
	toBeDeletedKeys := strset.Difference(modifiedResourcesSet, neededResourceSet).List()
	for _, resourceKey := range toBeDeletedKeys {
		toBeDeleted = append(toBeDeleted, modifiedResourceMap[resourceKey])
	}
	return toBeDeleted
}

//ManageError manage error sets an error status in the CR and fires an event, finally it returns the error so the operator can re-attempt
func (er *EnforcingReconciler) ManageError(instance apis.Resource, issue error) (reconcile.Result, error) {
	er.GetRecorder().Event(instance, "Warning", "ProcessingError", issue.Error())
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
		log.V(1).Info("about to modify state for", "instance version", instance.GetResourceVersion())
		err := er.GetClient().Status().Update(context.Background(), instance)
		if err != nil {
			if errors.IsResourceExpired(err) {
				log.Info("unable to update status for", "object version", instance.GetResourceVersion(), "resource version expired, will trigger another reconcile cycle", "")
			} else {
				log.Error(err, "unable to update status for", "object", instance)
			}
			return reconcile.Result{}, err
		}
	} else {
		log.V(1).Info("object is not RecocileStatusAware, not setting status")
	}
	return reconcile.Result{}, issue
}

// ManageSuccess will update the status of the CR and return a successful reconcile result
func (er *EnforcingReconciler) ManageSuccess(instance apis.Resource) (reconcile.Result, error) {
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
			LockedPatchStatuses:    er.GetLockedPatchStatuses(instance),
		}
		enforcingReconcileStatusAware.SetEnforcingReconcileStatus(status)
		log.V(1).Info("about to modify state for", "instance version", instance.GetResourceVersion())
		err := er.GetClient().Status().Update(context.Background(), instance)
		if err != nil {
			if errors.IsResourceExpired(err) {
				log.Info("unable to update status for", "object version", instance.GetResourceVersion(), "resource version expired, will trigger another reconcile cycle", "")
			} else {
				log.Error(err, "unable to update status for", "object", instance)
			}
			return reconcile.Result{}, err
		}
	} else {
		log.V(1).Info("object is not RecocileStatusAware, not setting status")
	}
	return reconcile.Result{}, nil
}

// GetLockedResourceStatuses returns the status for all LockedResources
func (er *EnforcingReconciler) GetLockedResourceStatuses(instance apis.Resource) map[string]status.Conditions {
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

// GetLockedPatchStatuses returns the status for all LockedPatches
func (er *EnforcingReconciler) GetLockedPatchStatuses(instance apis.Resource) map[string]status.Conditions {
	lockedResourceManager, err := er.getLockedResourceManager(instance)
	if err != nil {
		log.Error(err, "unable to get locked resource manager for", "parent", instance)
		return map[string]status.Conditions{}
	}
	lockedPatchReconcileStatuses := map[string]status.Conditions{}
	for _, lockedPatchReconciler := range lockedResourceManager.GetPatchReconcilers() {
		lockedPatchReconcileStatuses[lockedPatchReconciler.GetKey()] = lockedPatchReconciler.GetStatus()
	}
	return lockedPatchReconcileStatuses
}

// Terminate will stop the execution for the current istance. It will also optionally delete the locked resources.
func (er *EnforcingReconciler) Terminate(instance apis.Resource, deleteResources bool) error {
	defer er.removeLockedResourceManager(instance)
	lockedResourceManager, err := er.getLockedResourceManager(instance)
	if err != nil {
		log.V(1).Info("unable to get locked resource manager for", "parent", instance)
		return err
	}
	if lockedResourceManager.IsStarted() {
		err = lockedResourceManager.Stop(deleteResources)
		if err != nil {
			log.V(1).Info("unable to stop ", "lockedResourceManager", lockedResourceManager)
			return err
		}
	}
	return nil
}
