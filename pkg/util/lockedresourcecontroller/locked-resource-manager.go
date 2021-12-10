package lockedresourcecontroller

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/go-logr/logr"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
	"github.com/redhat-cop/operator-utils/pkg/util/discoveryclient"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedpatch"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedresource"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedresource/lockedresourceset"
	"github.com/redhat-cop/operator-utils/pkg/util/stoppablemanager"
	"github.com/redhat-cop/operator-utils/pkg/util/templates"
	"github.com/scylladb/go-set/strset"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/util/openapi"
	"k8s.io/kubectl/pkg/util/openapi/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// LockedResourceManager is a manager designed to manage a set of LockedResourceReconciler.
// Each reconciler can handle a LockedResource.
// LockedResourceManager is designed to be sued within an operator to enforce a set of resources.
// It has methods to start and stop the enforcing and to detect whether a set of resources is equal to the currently enforce set.
type LockedResourceManager struct {
	stoppableManager    *stoppablemanager.StoppableManager
	resources           []lockedresource.LockedResource
	resourceReconcilers []*LockedResourceReconciler
	patches             []lockedpatch.LockedPatch
	patchReconcilers    []*LockedPatchReconciler
	config              *rest.Config
	options             manager.Options
	parent              client.Object
	statusChange        chan<- event.GenericEvent
	clusterWatchers     bool
	log                 logr.Logger
}

// NewLockedResourceManager build a new LockedResourceManager
// config: the rest config client to be used by the controllers
// options: the manager options
// parent: an object to which send notification when a recocilianton cicle completes for one of the reconcilers
// statusChange: a channel through which send the notifications
func NewLockedResourceManager(config *rest.Config, options manager.Options, parent client.Object, statusChange chan<- event.GenericEvent, clusterWatchers bool) (LockedResourceManager, error) {
	lockedResourceManager := LockedResourceManager{
		config:          config,
		options:         options,
		parent:          parent,
		statusChange:    statusChange,
		clusterWatchers: clusterWatchers,
		log:             ctrl.Log.WithName("locker-resource-manager").WithName(apis.GetKeyShort(parent)),
	}
	return lockedResourceManager, nil
}

// GetResources returns the currently enforced resources
func (lrm *LockedResourceManager) GetResources() []lockedresource.LockedResource {
	return lrm.resources
}

//GetPatches returns the currently enforced patches
func (lrm *LockedResourceManager) GetPatches() []lockedpatch.LockedPatch {
	return lrm.patches
}

// SetResources set the resources to be enforced. Can be called only when the LockedResourceManager is stopped.
func (lrm *LockedResourceManager) SetResources(resources []lockedresource.LockedResource) error {
	if lrm.stoppableManager != nil && lrm.stoppableManager.IsStarted() {
		return errors.New("cannot set resources while enforcing is on")
	}
	err := lrm.validateLockedResources(resources)
	if err != nil {
		lrm.log.Error(err, "unable to validate resources against running api server")
		return err
	}
	lrm.resources = resources
	return nil
}

// SetPatches set the patches to be enforced. Can be called only when the LockedResourceManager is stopped.
func (lrm *LockedResourceManager) SetPatches(patches []lockedpatch.LockedPatch) error {
	if lrm.stoppableManager != nil && lrm.stoppableManager.IsStarted() {
		return errors.New("cannot set resources while enforcing is on")
	}
	// verifyPatchID Uniqueness
	lockedPatchMap := map[string]lockedpatch.LockedPatch{}
	for _, lockedPatch := range patches {
		if lockedPatch.Name == "" {
			return errors.New("lockedPatch.ID must be initialized")
		}
		if _, ok := lockedPatchMap[lockedPatch.Name]; ok {
			return errors.New("Duplicate patch id: " + lockedPatch.Name)
		}
		lockedPatchMap[lockedPatch.Name] = lockedPatch
	}
	err := lrm.validateLockedPatches(patches)
	if err != nil {
		lrm.log.Error(err, "unable to validate patches against running api server")
		return err
	}
	lrm.patches = patches
	return nil
}

// IsStarted returns whether the LockedResourceManager is started
func (lrm *LockedResourceManager) IsStarted() bool {
	return lrm.stoppableManager != nil && lrm.stoppableManager.IsStarted()
}

// Start starts the LockedResourceManager
func (lrm *LockedResourceManager) Start(config *rest.Config) error {
	if lrm.stoppableManager != nil && lrm.stoppableManager.IsStarted() {
		return nil
	}

	//diabling metrics
	options := lrm.options
	options.MetricsBindAddress = "0"
	options.LeaderElection = false

	if !lrm.clusterWatchers {
		namespaces := lrm.scanNamespaces()
		lrm.log.V(1).Info("starting multicache with the following ", "namespaces", namespaces)
		options.NewCache = cache.MultiNamespacedCacheBuilder(namespaces)
	}

	stoppableManager, err := stoppablemanager.NewStoppableManager(config, options)
	lrm.stoppableManager = &stoppableManager

	if err != nil {
		lrm.log.Error(err, "unable to create stoppable manager")
		return err
	}

	resourceReconcilers := []*LockedResourceReconciler{}
	for _, resource := range lrm.resources {
		reconciler, err := NewLockedObjectReconciler(lrm.stoppableManager.Manager, resource.Unstructured, resource.ExcludedPaths, lrm.statusChange, lrm.parent)
		if err != nil {
			lrm.log.Error(err, "unable to create reconciler", "for locked resource", resource)
			return err
		}
		resourceReconcilers = append(resourceReconcilers, reconciler)
	}
	lrm.resourceReconcilers = resourceReconcilers

	patchReconcilers := []*LockedPatchReconciler{}
	for _, patch := range lrm.patches {
		reconciler, err := NewLockedPatchReconciler(lrm.stoppableManager.Manager, patch, lrm.statusChange, lrm.parent)
		if err != nil {
			lrm.log.Error(err, "unable to create reconciler", "for locked patch", patch)
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
// notice that lrm will always succeed at stopping the manager, but it might fail at deleting resources
func (lrm *LockedResourceManager) Stop(deleteResources bool) error {
	lrm.stoppableManager.Stop()
	if deleteResources {
		err := lrm.deleteResources(context.TODO())
		if err != nil {
			lrm.log.Error(err, "unable to delete resources")
			return err
		}
	}
	return nil
}

func (lrm *LockedResourceManager) scanNamespaces() []string {
	namespaceSet := strset.New()
	for _, resource := range lrm.GetResources() {
		if resource.GetNamespace() != "" {
			namespaceSet.Add(resource.GetNamespace())
		}
	}
	for _, patch := range lrm.GetPatches() {
		if patch.TargetObjectRef.Namespace != "" {
			namespaceSet.Add(patch.TargetObjectRef.Namespace)
		}
		for _, sourceObj := range patch.SourceObjectRefs {
			if sourceObj.Namespace != "" {
				namespaceSet.Add(sourceObj.Namespace)
			}
		}
	}
	//in case no namesopace is added it means that all of the objects are cluster scoped, then we need to add an emptu string to activate the cache.
	if len(lrm.GetResources())+len(lrm.GetPatches()) > 0 && len(namespaceSet.List()) == 0 {
		return []string{""}
	}
	return namespaceSet.List()
}

// Restart restarts the manager with a different set of resources
// if deleteResources is set, resources that were enforced are deleted.
func (lrm *LockedResourceManager) Restart(resources []lockedresource.LockedResource, patches []lockedpatch.LockedPatch, deleteResources bool, config *rest.Config) error {
	if lrm.IsStarted() {
		err := lrm.Stop(deleteResources)
		if err != nil {
			lrm.log.Error(err, "unable to stop", "deleteResources", deleteResources)
			return err
		}
	}
	err := lrm.SetResources(resources)
	if err != nil {
		lrm.log.Error(err, "unable to set", "resources", resources)
		return err
	}
	err = lrm.SetPatches(patches)
	if err != nil {
		lrm.log.Error(err, "unable to set", "patches", patches)
		return err
	}
	return lrm.Start(config)
}

// IsSameResources checks whether the currently enforced resources are the same as the ones passed as parameters
// same is true is current resources are the same as the resources passed as a parameter
// leftDifference contains the resources that are in the current resources but not in passed in the parameter
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
// intersection contains patches that are both in the current patches and the parameter, the patch definition may not be the same, the definitions of those in the parameter are returned
// rightDifference contains the patches that are in the parameter but not in the current patches
func (lrm *LockedResourceManager) IsSamePatches(patches []lockedpatch.LockedPatch) (same bool, leftDifference []lockedpatch.LockedPatch, intersection []lockedpatch.LockedPatch, rightDifference []lockedpatch.LockedPatch) {
	currentPatchMap, currentPatches := lockedpatch.GetLockedPatchMap(lrm.GetPatches())
	newPatchMap, newPatches := lockedpatch.GetLockedPatchMap(patches)
	currentPatchSet := strset.New(currentPatches...)
	newPatchSet := strset.New(newPatches...)
	leftDifference = lockedpatch.GetLockedPatchesFromLockedPatcheSet(strset.Difference(currentPatchSet, newPatchSet), currentPatchMap)
	intersection = lockedpatch.GetLockedPatchesFromLockedPatcheSet(strset.Intersection(currentPatchSet, newPatchSet), newPatchMap)
	rightDifference = lockedpatch.GetLockedPatchesFromLockedPatcheSet(strset.Difference(newPatchSet, currentPatchSet), newPatchMap)
	same = currentPatchSet.IsEqual(newPatchSet)
	//we also need to check intersection to see if there are differences in the pacth definition
	for _, patchID := range strset.Intersection(currentPatchSet, newPatchSet).List() {
		currentPatch, err := json.Marshal(currentPatchMap[patchID])
		if err != nil {
			lrm.log.Error(err, "unable to Marshall", "currentPatch", currentPatchMap[patchID])
			return false, leftDifference, intersection, rightDifference
		}
		newPatch, err := json.Marshal(newPatchMap[patchID])
		if err != nil {
			lrm.log.Error(err, "unable to Marshall", "newPatch", newPatchMap[patchID])
			return false, leftDifference, intersection, rightDifference
		}
		if string(currentPatch) != string(newPatch) {
			same = false
			break
		}
	}
	return same, leftDifference, intersection, rightDifference
}

func (lrm *LockedResourceManager) deleteResources(context context.Context) error {
	reconcilerBase := util.NewFromManager(lrm.stoppableManager.Manager, lrm.stoppableManager.GetEventRecorderFor("resource-deleter"))
	for _, resource := range lrm.GetResources() {
		gvk := resource.Unstructured.GetObjectKind().GroupVersionKind()
		groupVersion := schema.GroupVersion{Group: gvk.Group, Version: gvk.Version}
		lrm.stoppableManager.GetScheme().AddKnownTypes(groupVersion, &resource.Unstructured)
		err := reconcilerBase.DeleteResourceIfExists(context, &resource.Unstructured)
		if err != nil {
			lrm.log.Error(err, "unable to delete", "resource", resource.Unstructured)
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
	ctx := context.TODO()
	ctx = context.WithValue(ctx, "restConfig", lrm.config)
	ctx = log.IntoContext(ctx, lrm.log)
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(lrm.config)
	if err != nil {
		lrm.log.Error(err, "unable to create discovery client")
		return err
	}
	//lockedResourceAPIResource.
	// validate the unstructured object is conformant to the openapi
	doc, err := discoveryClient.OpenAPISchema()
	if err != nil {
		lrm.log.Error(err, "unable to get openapi schema")
		return err
	}
	resources, err := openapi.NewOpenAPIData(doc)
	if err != nil {
		lrm.log.Error(err, "unable to get resources from openapi doc")
		return err
	}
	schemaValidation := validation.NewSchemaValidation(resources)
	result := &multierror.Error{}
	for _, lockedResource := range lockedResources {
		defined, err := discoveryclient.IsUnstructuredDefined(ctx, &lockedResource.Unstructured)
		if err != nil {
			lrm.log.Error(err, "unable to validate", "unstructured", lockedResource.Unstructured)
			result = multierror.Append(result, err)
			continue
		}
		if !defined {
			result = multierror.Append(result, errors.New("resource type:"+lockedResource.Unstructured.GroupVersionKind().String()+"not defined"))
			continue
		}
		err = templates.ValidateUnstructured(ctx, &lockedResource.Unstructured, schemaValidation)
		if err != nil {
			lrm.log.Error(err, "unable to validate", "unstructured", lockedResource.Unstructured)
			result = multierror.Append(result, err)
			continue
		}
		namespaced, err := discoveryclient.IsUnstructuredNamespaced(ctx, &lockedResource.Unstructured)
		if err != nil {
			lrm.log.Error(err, "unable to determine if namespaced", "unstructured", lockedResource.Unstructured)
			result = multierror.Append(result, err)
			continue
		}
		if namespaced && lockedResource.Unstructured.GetNamespace() == "" {
			err := errors.New("namespaced resources must specify a namespace")
			lrm.log.Error(err, "unable to validate", "unstructured", lockedResource.Unstructured)
			result = multierror.Append(result, err)
			continue
		}
	}
	if result.ErrorOrNil() != nil {
		lrm.log.Error(result, "encountered errors during resources validation")
		return result
	}
	return nil
}

//GetPatchReconcilers return the currently active patch reconcilers
func (lrm *LockedResourceManager) GetPatchReconcilers() []*LockedPatchReconciler {
	if lrm.IsStarted() {
		return lrm.patchReconcilers
	}
	return []*LockedPatchReconciler{}
}

func (lrm *LockedResourceManager) validateLockedPatches(patches []lockedpatch.LockedPatch) error {
	ctx := context.TODO()
	ctx = context.WithValue(ctx, "restConfig", lrm.config)
	ctx = log.IntoContext(ctx, lrm.log)
	result := &multierror.Error{}
	for _, lockedPatch := range patches {
		GVKs := []schema.GroupVersionKind{}
		for i := range lockedPatch.SourceObjectRefs {
			GVKs = append(GVKs, schema.FromAPIVersionAndKind(lockedPatch.SourceObjectRefs[i].APIVersion, lockedPatch.SourceObjectRefs[i].Kind))
		}
		GVKs = append(GVKs, schema.FromAPIVersionAndKind(lockedPatch.TargetObjectRef.APIVersion, lockedPatch.TargetObjectRef.Kind))
		for i := range GVKs {
			defined, err := discoveryclient.IsGVKDefined(ctx, GVKs[i])
			if err != nil {
				lrm.log.Error(err, "undefined resource in this cluster", "gvk", GVKs[i])
				result = multierror.Append(result, err)
				continue
			}
			if !defined {
				result = multierror.Append(result, errors.New("resource type:"+GVKs[i].String()+"not defined"))
				continue
			}
			// if resource.Namespaced && objref.Namespace == "" {
			// 	err := errors.New("namespace must be specified for namespaced resources")
			// 	lrm.log.Error(err, "unable to validate", "objectref", objref)
			// 	result = multierror.Append(result, err)
			// 	continue
			// }
		}
	}
	if result.ErrorOrNil() != nil {
		lrm.log.Error(result, "encountered errors during patch validation")
		return result
	}
	return nil
}
