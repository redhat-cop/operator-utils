package lockedresourcecontroller

import (
	"errors"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedpatch"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedpatch/lockedpatchset"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedresource"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedresource/lockedresourceset"
	"github.com/redhat-cop/operator-utils/pkg/util/stoppablemanager"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/kubernetes/pkg/kubectl/cmd/util/openapi"
	"k8s.io/kubernetes/pkg/kubectl/cmd/util/openapi/validation"
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
	patches             []lockedpatch.LockedPatch
	patchReconcilers    []*LockedPatchReconciler
	config              *rest.Config
	options             manager.Options
	parent              apis.Resource
	statusChange        chan<- event.GenericEvent
}

// NewLockedResourceManager build a new LockedResourceManager
// config: the rest config client to be used by the controllers
// options: the manager options
// parent: an object to which send notification when a recocilianton cicle completes for one of the reconcilers
// statusChange: a channel through which send the notifications
func NewLockedResourceManager(config *rest.Config, options manager.Options, parent apis.Resource, statusChange chan<- event.GenericEvent) (LockedResourceManager, error) {
	//diabling metrics
	options.MetricsBindAddress = "0"
	options.LeaderElection = false

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

// GetResources returns the currently enforced resources
func (lrm *LockedResourceManager) GetResources() []lockedresource.LockedResource {
	return lrm.resources
}

//GetPatches retunrs the currently enforced patches
func (lrm *LockedResourceManager) GetPatches() []lockedpatch.LockedPatch {
	return lrm.patches
}

// SetResources set the resources to be enfroced. Can be called only when the LockedResourceManager is stopped.
func (lrm *LockedResourceManager) SetResources(resources []lockedresource.LockedResource) error {
	if lrm.stoppableManager.IsStarted() {
		return errors.New("cannot set resources while enforcing is on")
	}
	err := lrm.validateLockedResources(resources)
	if err != nil {
		log.Error(err, "unable to validate resources against running api server")
		return err
	}
	lrm.resources = resources
	return nil
}

// SetPatches set the patches to be enfroced. Can be called only when the LockedResourceManager is stopped.
func (lrm *LockedResourceManager) SetPatches(patches []lockedpatch.LockedPatch) error {
	if lrm.stoppableManager.IsStarted() {
		return errors.New("cannot set resources while enforcing is on")
	}
	lrm.patches = patches
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

	patchReconcilers := []*LockedPatchReconciler{}
	for _, patch := range lrm.patches {
		reconciler, err := NewLockedPatchReconciler(lrm.stoppableManager.Manager, patch, lrm.statusChange, lrm.parent)
		if err != nil {
			log.Error(err, "unable to create reconciler", "for locked patch", patch)
			return err
		}
		patchReconcilers = append(patchReconcilers, reconciler)
	}
	lrm.patchReconcilers = patchReconcilers

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
		if err != nil {
			log.Error(err, "unable to delete resources")
			return err
		}
	}
	return nil
}

// Restart restarts the manager with a different set of resources
// if deleteResources is set, resources that were enforced are deleted.
func (lrm *LockedResourceManager) Restart(resources []lockedresource.LockedResource, patches []lockedpatch.LockedPatch, deleteResources bool) error {
	if lrm.IsStarted() {
		lrm.Stop(deleteResources)
	}
	err := lrm.SetResources(resources)
	if err != nil {
		log.Error(err, "unable to set", "resources", resources)
		return err
	}
	lrm.SetPatches(patches)
	if err != nil {
		log.Error(err, "unable to set", "patches", patches)
		return err
	}
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
// leftDifference contains the resources that are in the current reosurces but not in passed in the parameter
// intersection contains resources that are both in the current resources and the parameter
// rightDifference contains the resources that are in the parameter but not in the current resources
func (lrm *LockedResourceManager) IsSameResources(resources []lockedresource.LockedResource) (same bool, leftDifference []lockedresource.LockedResource, intersection []lockedresource.LockedResource, rightDifference []lockedresource.LockedResource) {
	currentResources := lockedresourceset.New(lrm.GetResources()...)
	newResources := lockedresourceset.New(resources...)
	leftDifference = lockedresourceset.Difference(currentResources, newResources).List()
	intersection = lockedresourceset.Intersection(currentResources, newResources).List()
	rightDifference = lockedresourceset.Difference(newResources, currentResources).List()
	same = currentResources.IsEqual(newResources)
	return same, leftDifference, intersection, rightDifference
}

// IsSamePatches checks whether the currently enforced patches are the same as the ones passed as parameters
// same is true is current patches are the same as the patches passed as a parameter
// leftDifference contains the patches that are in the current patches but not in passed in the parameter
// intersection contains patches that are both in the current patches and the parameter
// rightDifference contains the patches that are in the parameter but not in the current patches
func (lrm *LockedResourceManager) IsSamePatches(patches []lockedpatch.LockedPatch) (same bool, leftDifference []lockedpatch.LockedPatch, intersection []lockedpatch.LockedPatch, rightDifference []lockedpatch.LockedPatch) {
	currentHashablePatchMap, currentHashablePatches, err := lockedpatch.GetHashableLockedPatchMap(lrm.GetPatches())
	if err != nil {
		log.Error(err, "unable to compute hashable patches")
		return false, []lockedpatch.LockedPatch{}, []lockedpatch.LockedPatch{}, []lockedpatch.LockedPatch{}
	}
	newHashablePatchesMap, newHashablePatches, err := lockedpatch.GetHashableLockedPatchMap(patches)
	if err != nil {
		log.Error(err, "unable to compute hashable patches")
		return false, []lockedpatch.LockedPatch{}, []lockedpatch.LockedPatch{}, []lockedpatch.LockedPatch{}
	}
	currentPatches := lockedpatchset.New(currentHashablePatches...)
	newPatches := lockedpatchset.New(newHashablePatches...)
	leftDifference = lockedpatchset.GetLockedPatchedFromLockedPatchesSet(lockedpatchset.Difference(currentPatches, newPatches), currentHashablePatchMap)
	intersection = lockedpatchset.GetLockedPatchedFromLockedPatchesSet(lockedpatchset.Intersection(currentPatches, newPatches), currentHashablePatchMap)
	rightDifference = lockedpatchset.GetLockedPatchedFromLockedPatchesSet(lockedpatchset.Difference(newPatches, currentPatches), newHashablePatchesMap)
	same = currentPatches.IsEqual(newPatches)
	return same, leftDifference, intersection, rightDifference
}

func (lrm *LockedResourceManager) deleteResources() error {
	reconcilerBase := util.NewReconcilerBase(lrm.stoppableManager.GetClient(), lrm.stoppableManager.GetScheme(), lrm.stoppableManager.GetConfig(), lrm.stoppableManager.GetEventRecorderFor("resource-deleter"))
	for _, resource := range lrm.GetResources() {
		gvk := resource.Unstructured.GetObjectKind().GroupVersionKind()
		groupVersion := schema.GroupVersion{Group: gvk.Group, Version: gvk.Version}
		lrm.stoppableManager.GetScheme().AddKnownTypes(groupVersion, &resource.Unstructured)
		err := reconcilerBase.DeleteResourceIfExists(&resource.Unstructured)
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


func (lrm *LockedResourceManager) validateLockedResources(lockedResources []lockedresource.LockedResource) error {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(lrm.config)
	if err != nil {
		log.Error(err, "unable to create discovery client")
		return err
	}
	//lockedResourceAPIResource.
	// validate the unstructured object is conformant to the openapi
	doc, err := discoveryClient.OpenAPISchema()
	if err != nil {
		log.Error(err, "unable to get openapi schema")
		return err
	}
	resources, err := openapi.NewOpenAPIData(doc)
	if err != nil {
		log.Error(err, "unable to get resouces from openapi doc")
		return err
	}
	schemaValidation := validation.NewSchemaValidation(resources)
	result := &multierror.Error{}
	for _, lockedResource := range lockedResources {
		log.V(1).Info("validating", "resource", lockedResource.Unstructured)
		err := util.IsUnstructuredDefined(&lockedResource.Unstructured, discoveryClient)
		if err != nil {
			log.Error(err, "unable to validate", "unstructured", lockedResource.Unstructured)
			multierror.Append(result, err)
			continue
		}
		err = util.ValidateUnstructured(&lockedResource.Unstructured, schemaValidation)
		if err != nil {
			log.Error(err, "unable to validate", "unstructured", lockedResource.Unstructured)
			multierror.Append(result, err)
		}
	}
	if result.ErrorOrNil() != nil {
		log.Error(result, "encountered errors during resources validation")
		return result
	}
	return nil

//GetPatchReconcilers return the currently active patch reconcilers
func (lrm *LockedResourceManager) GetPatchReconcilers() []*LockedPatchReconciler {
	if lrm.IsStarted() {
		return lrm.patchReconcilers
	}
	return []*LockedPatchReconciler{}

}
