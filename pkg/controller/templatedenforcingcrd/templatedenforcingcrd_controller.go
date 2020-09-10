package templatedenforcingcrd

import (
	"context"
	errs "errors"

	examplev1alpha1 "github.com/redhat-cop/operator-utils/pkg/apis/example/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedpatch"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedresource"
	"github.com/scylladb/go-set/strset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var controllerName = "templatedenforcingcrd_controller"
var log = logf.Log.WithName(controllerName)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new TemplatedEnforcingCRD Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileTemplatedEnforcingCRD{
		EnforcingReconciler: lockedresourcecontroller.NewEnforcingReconciler(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetEventRecorderFor(controllerName), true),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New(controllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource TemplatedEnforcingCRD
	err = c.Watch(&source.Kind{Type: &examplev1alpha1.TemplatedEnforcingCRD{
		TypeMeta: metav1.TypeMeta{
			Kind: "TemplatedEnforcingCRD",
		}}}, &handler.EnqueueRequestForObject{}, util.ResourceGenerationOrFinalizerChangedPredicate{})
	if err != nil {
		return err
	}

	//if interested in updates from the managed resources
	// watch for changes in status in the locked resources
	reconcileTemplatedEnforcingCRD, ok := r.(*ReconcileTemplatedEnforcingCRD)
	if !ok {
		err := errs.New("unable to convert reconciler to ReconcileEnforcingCRD")
		log.Error(err, "unable to convert to ReconcileEnforcingCRD", "reconciler", r)
		return err
	}
	err = c.Watch(
		&source.Channel{Source: reconcileTemplatedEnforcingCRD.GetStatusChangeChannel()},
		&handler.EnqueueRequestForObject{},
	)
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileTemplatedEnforcingCRD implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileTemplatedEnforcingCRD{}

// ReconcileTemplatedEnforcingCRD reconciles a TemplatedEnforcingCRD object
type ReconcileTemplatedEnforcingCRD struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	lockedresourcecontroller.EnforcingReconciler
}

// Reconcile reads that state of the cluster for a TemplatedEnforcingCRD object and makes changes based on the state read
// and what is in the TemplatedEnforcingCRD.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileTemplatedEnforcingCRD) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling TemplatedEnforcingCRD")

	// Fetch the TemplatedEnforcingCRD instance
	instance := &examplev1alpha1.TemplatedEnforcingCRD{}
	err := r.GetClient().Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if ok := r.IsInitialized(instance); !ok {
		err := r.GetClient().Update(context.TODO(), instance)
		if err != nil {
			log.Error(err, "unable to update instance", "instance", instance)
			return r.ManageError(instance, err)
		}
		return reconcile.Result{}, nil
	}

	if util.IsBeingDeleted(instance) {
		if !util.HasFinalizer(instance, controllerName) {
			return reconcile.Result{}, nil
		}
		err := r.manageCleanUpLogic(instance)
		if err != nil {
			log.Error(err, "unable to delete instance", "instance", instance)
			return r.ManageError(instance, err)
		}
		util.RemoveFinalizer(instance, controllerName)
		err = r.GetClient().Update(context.TODO(), instance)
		if err != nil {
			log.Error(err, "unable to update instance", "instance", instance)
			return r.ManageError(instance, err)
		}
		return reconcile.Result{}, nil
	}

	lockedResources, err := lockedresource.GetLockedResourcesFromTemplatesWithRestConfig(instance.Spec.Templates, r.GetRestConfig(), instance)
	if err != nil {
		log.Error(err, "unable to get locked resources")
		return r.ManageError(instance, err)
	}
	err = r.UpdateLockedResources(instance, lockedResources, []lockedpatch.LockedPatch{})
	if err != nil {
		log.Error(err, "unable to update locked resources")
		return r.ManageError(instance, err)
	}

	return r.ManageSuccess(instance)
}

// IsInitialized can be used to check if isntance is correctlty initialuzed.
// returns false it it's not.
func (r *ReconcileTemplatedEnforcingCRD) IsInitialized(instance *examplev1alpha1.TemplatedEnforcingCRD) bool {
	needsUpdate := true
	for i := range instance.Spec.Templates {
		currentSet := strset.New(instance.Spec.Templates[i].ExcludedPaths...)
		if !currentSet.IsEqual(strset.Union(lockedresource.DefaultExcludedPathsSet, currentSet)) {
			instance.Spec.Templates[i].ExcludedPaths = strset.Union(lockedresource.DefaultExcludedPathsSet, currentSet).List()
			needsUpdate = false
		}
	}
	if len(instance.Spec.Templates) > 0 && !util.HasFinalizer(instance, controllerName) {
		util.AddFinalizer(instance, controllerName)
		needsUpdate = false
	}
	if len(instance.Spec.Templates) == 0 && util.HasFinalizer(instance, controllerName) {
		util.RemoveFinalizer(instance, controllerName)
		needsUpdate = false
	}
	return needsUpdate
}

func (r *ReconcileTemplatedEnforcingCRD) manageCleanUpLogic(instance *examplev1alpha1.TemplatedEnforcingCRD) error {
	err := r.Terminate(instance, true)
	if err != nil {
		log.Error(err, "unable to terminate enforcing reconciler for", "instance", instance)
		return err
	}
	return nil
}
