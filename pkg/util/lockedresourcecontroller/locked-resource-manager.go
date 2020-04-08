package lockedresourcecontroller

import (
	"context"
	"errors"

	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedresource"
	"github.com/redhat-cop/operator-utils/pkg/util/stoppablemanager"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// LockedResourceManager is a manager designed to manage a set of LockedResourceReconciler.
// Each reconciler can handle a LockedResource.
// LockedResourceManager is designed to be sued within an operator to enforce a set of resources.
// It has methods to start and stop the enforcing and to detect whether a set of resources is equal to the currently enforce set.
type LockedResourceManager struct {
	stoppableManager    stoppablemanager.StoppableManager
	resources           []lockedresource.LockedResource
	resourceReconcilers []*LockedResourceReconciler
	config              *rest.Config
	options             manager.Options
	parent              metav1.Object
	statusChange        chan<- event.GenericEvent
}

// NewLockedResourceManager build a new LockedResourceManager
// config: the rest config client to be used by the controllers
// options: the manager options
// parent: an object to which send notification when a recocilianton cicle completes for one of the reconcilers
// statusChange: a channel through which send the notifications
func NewLockedResourceManager(config *rest.Config, options manager.Options, parent metav1.Object, statusChange chan<- event.GenericEvent) (LockedResourceManager, error) {
	stoppableManager, err := stoppablemanager.NewStoppableManager(config, options)
	if err != nil {
		log.Error(err, "unable to create stoppable manager")
		return LockedResourceManager{}, err
	}
	lockedResourceManager := LockedResourceManager{
		stoppableManager: stoppableManager,
		config:           config,
		options:          options,
		parent:           parent,
		statusChange:     statusChange,
	}
	return lockedResourceManager, nil
}

// GetResource returns the currently enforced resources
func (lrm *LockedResourceManager) GetResources() []lockedresource.LockedResource {
	return lrm.resources
}

// SetResources set the resources to be enfroced. Can be called only when the LockedResourceManager is stopped.
func (lrm *LockedResourceManager) SetResources(resources []lockedresource.LockedResource) error {
	if lrm.stoppableManager.IsStarted() {
		return errors.New("cannot set resources while enforcing is on")
	}
	lrm.resources = resources
	return nil
}

// IsStarted returns whether the LockedResourceManager is started
func (lrm *LockedResourceManager) IsStarted() bool {
	return lrm.stoppableManager.IsStarted()
}

// Start starts the LockedResourceManager
func (lrm *LockedResourceManager) Start() error {
	if lrm.stoppableManager.IsStarted() {
		return nil
	}
	resourceReconcilers := []*LockedResourceReconciler{}
	for _, resource := range lrm.resources {
		reconciler, err := NewLockedObjectReconciler(lrm.stoppableManager.Manager, resource.Unstructured, resource.ExcludedPaths, lrm.statusChange, lrm.parent)
		if err != nil {
			log.Error(err, "unable to create reconciler", "for locked resource", resource)
			return err
		}
		resourceReconcilers = append(resourceReconcilers, reconciler)
	}
	lrm.resourceReconcilers = resourceReconcilers
	lrm.stoppableManager.Start()
	return nil
}

// Stop stops the LockedResourceManager.
// deleteResource controls whether the managed resources should be deleted or left in place
// notice that lrm will always succed at stoppping the manager, but it might fail at deleting resources
func (lrm *LockedResourceManager) Stop(deleteResources bool) error {
	lrm.stoppableManager.Stop()
	if deleteResources {
		err := lrm.deleteResources()
		log.Error(err, "unable to delete resources")
		return err
	}
	return nil
}

// Restart restarts the manager with a different set of resources
// if deleteResources is set, resources that were enforced are deleted.
func (lrm *LockedResourceManager) Restart(resources []lockedresource.LockedResource, deleteResources bool) error {
	if lrm.IsStarted() {
		lrm.Stop(deleteResources)
	}
	lrm.SetResources(resources)
	stoppableManager, err := stoppablemanager.NewStoppableManager(lrm.config, manager.Options{
		MetricsBindAddress: "0",
		LeaderElection:     false,
	})
	if err != nil {
		log.Error(err, "unable to create stoppable manager")
		return err
	}
	lrm.stoppableManager = stoppableManager
	return lrm.Start()
}

// IsSameResources checks whether the currently enforced resources are the same as the ones passed as parameters
// same is true is current resources are the same as the resources passed as a parameter
// leftDifference containes the resources that are in the current reosurces but not in passed in the parameter
// intersection contains resources that are both in the current resources and the parameter
// rightDifference containes the resources that are in the parameter but not in the current resources
func (lrm *LockedResourceManager) IsSameResources(resources []lockedresource.LockedResource) (same bool, leftDifference []lockedresource.LockedResource, intersection []lockedresource.LockedResource, rightDifference []lockedresource.LockedResource) {
	currentResources := lockedresource.New(lrm.GetResources()...)
	newResources := lockedresource.New(resources...)
	leftDifference = lockedresource.Difference(currentResources, newResources).List()
	intersection = lockedresource.Intersection(currentResources, newResources).List()
	rightDifference = lockedresource.Difference(newResources, currentResources).List()
	same = currentResources.IsEqual(newResources)
	return same, leftDifference, intersection, rightDifference
}

func (lrm *LockedResourceManager) deleteResources() error {
	for _, resource := range lrm.GetResources() {
		err := lrm.stoppableManager.Manager.GetClient().Delete(context.TODO(), &resource.Unstructured, &client.DeleteOptions{})
		if err != nil {
			log.Error(err, "unable to delete", "resource", resource.Unstructured)
			return err
		}
	}
	return nil
}

//GetResourceReconcilers return the currently active resource reconcilers
func (lrm *LockedResourceManager) GetResourceReconcilers() []*LockedResourceReconciler {
	if lrm.IsStarted() {
		return lrm.resourceReconcilers
	}
	return []*LockedResourceReconciler{}
}
