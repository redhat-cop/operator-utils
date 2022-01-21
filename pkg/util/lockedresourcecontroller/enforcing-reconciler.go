package lockedresourcecontroller

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	"github.com/redhat-cop/operator-utils/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedpatch"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedresource"
	"github.com/scylladb/go-set/strset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// EnforcingReconciler is a reconciler designed to as a base type to extend for those operators that compute a set of resources that then need to be kept in place (i.e. enforced)
// the enforcing piece is taken care for, an implementor would just need to take care of the logic that computes the resources to be enforced.
type EnforcingReconciler struct {
	util.ReconcilerBase
	lockedResourceManagers      map[string]*LockedResourceManager
	statusChange                chan event.GenericEvent
	lockedResourceManagersMutex sync.Mutex
	clusterWatchers             bool
	log                         logr.Logger
	returnOnlyFailingStatuses   bool
}

//NewEnforcingReconciler creates a new EnforcingReconciler
// clusterWatcher determines whether the created watchers should be at the cluster level or namespace level.
// this affects the kind of permissions needed to run the controller
// also creating multiple namespace level permissions can create performance issue as one watch per object type per namespace is opened to the API server, if in doubt pass true here.
func NewEnforcingReconciler(client client.Client, scheme *runtime.Scheme, restConfig *rest.Config, apireader client.Reader, recorder record.EventRecorder, clusterWatchers bool, returnOnlyFailingStatuses bool) EnforcingReconciler {
	return EnforcingReconciler{
		ReconcilerBase:              util.NewReconcilerBase(client, scheme, restConfig, recorder, apireader),
		lockedResourceManagers:      map[string]*LockedResourceManager{},
		statusChange:                make(chan event.GenericEvent),
		lockedResourceManagersMutex: sync.Mutex{},
		clusterWatchers:             clusterWatchers,
		log:                         ctrl.Log.WithName("enforcing-reconciler"),
		returnOnlyFailingStatuses:   returnOnlyFailingStatuses,
	}
}

func NewFromManager(mgr manager.Manager, recorderName string, clusterWatchers bool, returnOnlyFailingStatuses bool) EnforcingReconciler {
	return NewEnforcingReconciler(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetAPIReader(), mgr.GetEventRecorderFor(recorderName), clusterWatchers, returnOnlyFailingStatuses)
}

//GetStatusChangeChannel returns the channel through which status change events can be received
func (er *EnforcingReconciler) GetStatusChangeChannel() <-chan event.GenericEvent {
	return er.statusChange
}

func (er *EnforcingReconciler) removeLockedResourceManager(instance client.Object) {
	er.lockedResourceManagersMutex.Lock()
	defer er.lockedResourceManagersMutex.Unlock()
	delete(er.lockedResourceManagers, apis.GetKeyShort(instance))
}

func (er *EnforcingReconciler) getLockedResourceManager(instance client.Object) (*LockedResourceManager, error) {
	er.lockedResourceManagersMutex.Lock()
	defer er.lockedResourceManagersMutex.Unlock()
	lockedResourceManager, ok := er.lockedResourceManagers[apis.GetKeyShort(instance)]
	if !ok {
		lockedResourceManager, err := NewLockedResourceManager(er.GetRestConfig(), manager.Options{}, instance, er.statusChange, er.clusterWatchers)
		if err != nil {
			er.log.Error(err, "unable to create LockedResourceManager")
			return &LockedResourceManager{}, err
		}
		er.lockedResourceManagers[apis.GetKeyShort(instance)] = &lockedResourceManager
		return &lockedResourceManager, nil
	}
	return lockedResourceManager, nil
}

// UpdateLockedResources will do the following:
// 1. initialize or retrieve the LockedResourceManager related to the passed parent resource
// 2. compare the currently enforced resources with the one passed as parameters and then
//    a. return immediately if they are the same
//    b. restart the LockedResourceManager if they don't match
func (er *EnforcingReconciler) UpdateLockedResources(context context.Context, instance client.Object, lockedResources []lockedresource.LockedResource, lockedPatches []lockedpatch.LockedPatch) error {
	return er.UpdateLockedResourcesWithRestConfig(context, instance, lockedResources, lockedPatches, er.GetRestConfig())
}

// UpdateLockedResourcesWithRestConfig will do the following:
// 1. initialize or retrieve the LockedResourceManager related to the passed parent resource
// 2. compare the currently enforced resources with the one passed as parameters and then
//    a. return immediately if they are the same
//    b. restart the LockedResourceManager if they don't match
// this variant allows passing a rest config
func (er *EnforcingReconciler) UpdateLockedResourcesWithRestConfig(context context.Context, instance client.Object, lockedResources []lockedresource.LockedResource, lockedPatches []lockedpatch.LockedPatch, config *rest.Config) error {
	lockedResourceManager, err := er.getLockedResourceManager(instance)
	if err != nil {
		er.log.Error(err, "unable to get LockedResourceManager")
		return err
	}
	sameResources, leftDifference, _, _ := lockedResourceManager.IsSameResources(lockedResources)
	//the resource in the leftDifference are not necessarily to be deleted, we need to check if the resource has simply been updated maintinign the sam type/namespace/value.
	toBeDeleted := getToBeDeletdResources(lockedResources, leftDifference)
	samePatches, _, _, _ := lockedResourceManager.IsSamePatches(lockedPatches)
	if !sameResources || !samePatches {
		err = er.DeleteUnstructuredResources(context, lockedresource.AsListOfUnstructured(toBeDeleted))
		if err != nil {
			er.log.Error(err, "unable to delete unmanaged", "resources", leftDifference)
			return err
		}
		err := lockedResourceManager.Restart(context, lockedResources, lockedPatches, false, config)
		if err != nil {
			er.log.Error(err, "unable to restart", "manager", lockedResourceManager)
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
		modifiedResourcesSet.Add(apis.GetKeyLong(&lockerResource))
		modifiedResourceMap[apis.GetKeyLong(&lockerResource)] = lockerResource
	}
	toBeDeletedKeys := strset.Difference(modifiedResourcesSet, neededResourceSet).List()
	for _, resourceKey := range toBeDeletedKeys {
		toBeDeleted = append(toBeDeleted, modifiedResourceMap[resourceKey])
	}
	return toBeDeleted
}

//ManageError manage error sets an error status in the CR and fires an event, finally it returns the error so the operator can re-attempt
func (er *EnforcingReconciler) ManageError(context context.Context, instance client.Object, issue error) (reconcile.Result, error) {
	er.GetRecorder().Event(instance, "Warning", "ProcessingError", issue.Error())
	if enforcingReconcileStatusAware, updateStatus := (instance).(v1alpha1.EnforcingReconcileStatusAware); updateStatus {
		condition := metav1.Condition{
			Type:               apis.ReconcileError,
			LastTransitionTime: metav1.Now(),
			Message:            issue.Error(),
			ObservedGeneration: instance.GetGeneration(),
			Reason:             apis.ReconcileErrorReason,
			Status:             metav1.ConditionTrue,
		}
		status := v1alpha1.EnforcingReconcileStatus{
			Conditions:             apis.AddOrReplaceCondition(condition, enforcingReconcileStatusAware.GetEnforcingReconcileStatus().Conditions),
			LockedResourceStatuses: er.GetLockedResourceStatuses(instance),
			LockedPatchStatuses:    er.GetLockedPatchStatuses(instance),
		}
		enforcingReconcileStatusAware.SetEnforcingReconcileStatus(status)
		err := er.GetClient().Status().Update(context, instance)
		if err != nil {
			if errors.IsResourceExpired(err) {
				er.log.Info("unable to update status for", "object version", instance.GetResourceVersion(), "resource version expired, will trigger another reconcile cycle", "")
			} else {
				er.log.Error(err, "unable to update status for", "object", instance)
			}
			return reconcile.Result{}, err
		}
	} else {
		er.log.V(1).Info("object is not ReconcileStatusAware, not setting status")
	}
	return reconcile.Result{}, issue
}

// ManageSuccess will update the status of the CR and return a successful reconcile result
func (er *EnforcingReconciler) ManageSuccess(context context.Context, instance client.Object) (reconcile.Result, error) {
	if enforcingReconcileStatusAware, updateStatus := (instance).(v1alpha1.EnforcingReconcileStatusAware); updateStatus {
		condition := metav1.Condition{
			Type:               apis.ReconcileSuccess,
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: instance.GetGeneration(),
			Reason:             apis.ReconcileSuccessReason,
			Status:             metav1.ConditionTrue,
		}
		status := v1alpha1.EnforcingReconcileStatus{
			Conditions:             apis.AddOrReplaceCondition(condition, enforcingReconcileStatusAware.GetEnforcingReconcileStatus().Conditions),
			LockedResourceStatuses: er.GetLockedResourceStatuses(instance),
			LockedPatchStatuses:    er.GetLockedPatchStatuses(instance),
		}
		enforcingReconcileStatusAware.SetEnforcingReconcileStatus(status)
		err := er.GetClient().Status().Update(context, instance)
		if err != nil {
			if errors.IsResourceExpired(err) {
				er.log.Info("unable to update status for", "object version", instance.GetResourceVersion(), "resource version expired, will trigger another reconcile cycle", "")
			} else {
				er.log.Error(err, "unable to update status for", "object", instance)
			}
			return reconcile.Result{}, err
		}
	} else {
		er.log.V(1).Info("object is not ReconcileStatusAware, not setting status")
	}
	return reconcile.Result{}, nil
}

// GetLockedResourceStatuses returns the status for all LockedResources
func (er *EnforcingReconciler) GetLockedResourceStatuses(instance client.Object) map[string]v1alpha1.Conditions {
	lockedResourceManager, err := er.getLockedResourceManager(instance)
	if err != nil {
		er.log.Error(err, "unable to get locked resource manager for", "parent", instance)
		return map[string]v1alpha1.Conditions{}
	}
	lockedResourceReconcileStatuses := map[string]v1alpha1.Conditions{}
	for _, lockedResourceReconciler := range lockedResourceManager.GetResourceReconcilers() {
		status := lockedResourceReconciler.GetStatus()
		if er.returnOnlyFailingStatuses {
			if lastCondition, ok := apis.GetLastCondition(status); ok && apis.IsErrorCondition(lastCondition) {
				lockedResourceReconcileStatuses[apis.GetKeyLong(&lockedResourceReconciler.Resource)] = status
			}
		} else {
			lockedResourceReconcileStatuses[apis.GetKeyLong(&lockedResourceReconciler.Resource)] = status
		}
	}
	return lockedResourceReconcileStatuses
}

// GetLockedPatchStatuses returns the status for all LockedPatches
func (er *EnforcingReconciler) GetLockedPatchStatuses(instance client.Object) map[string]v1alpha1.ConditionMap {
	lockedResourceManager, err := er.getLockedResourceManager(instance)
	if err != nil {
		er.log.Error(err, "unable to get locked resource manager for", "parent", instance)
		return nil
	}
	lockedPatchReconcileStatuses := map[string]v1alpha1.ConditionMap{}
	for _, lockedPatchReconciler := range lockedResourceManager.GetPatchReconcilers() {
		status := lockedPatchReconciler.GetStatus()
		for key, conditions := range status {
			if _, ok := lockedPatchReconcileStatuses[lockedPatchReconciler.GetKey()]; !ok {
				lockedPatchReconcileStatuses[lockedPatchReconciler.GetKey()] = map[string]v1alpha1.Conditions{}
			}
			if er.returnOnlyFailingStatuses {
				if lastCondition, ok := apis.GetLastCondition(status[key]); ok && apis.IsErrorCondition(lastCondition) {

					lockedPatchReconcileStatuses[lockedPatchReconciler.GetKey()][key] = conditions
				}
			} else {
				lockedPatchReconcileStatuses[lockedPatchReconciler.GetKey()][key] = conditions
			}
		}
	}
	return lockedPatchReconcileStatuses
}

// Terminate will stop the execution for the current instance. It will also optionally delete the locked resources.
func (er *EnforcingReconciler) Terminate(instance client.Object, deleteResources bool) error {
	defer er.removeLockedResourceManager(instance)
	lockedResourceManager, err := er.getLockedResourceManager(instance)
	if err != nil {
		er.log.Error(err, "unable to get locked resource manager for", "parent", instance)
		return err
	}
	if lockedResourceManager.IsStarted() {
		err = lockedResourceManager.Stop(deleteResources)
		if err != nil {
			er.log.Error(err, "unable to stop ", "lockedResourceManager", lockedResourceManager)
			return err
		}
	}
	return nil
}
