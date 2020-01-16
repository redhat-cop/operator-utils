package mycrd

import (
	"context"
	"errors"

	examplev1alpha1 "github.com/redhat-cop/operator-utils/pkg/apis/example/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const controllerName = "controller_mycrd"

var log = logf.Log.WithName(controllerName)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new MyCRD Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileMyCRD{
		ReconcilerBase: util.NewReconcilerBase(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetEventRecorderFor(controllerName)),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("mycrd-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource MyCRD
	err = c.Watch(&source.Kind{Type: &examplev1alpha1.MyCRD{}}, &handler.EnqueueRequestForObject{}, util.ResourceGenerationOrFinalizerChangedPredicate{})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileMyCRD{}

// ReconcileMyCRD reconciles a MyCRD object
type ReconcileMyCRD struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	util.ReconcilerBase
}

// Reconcile reads that state of the cluster for a MyCRD object and makes changes based on the state read
// and what is in the MyCRD.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileMyCRD) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling MyCRD")

	// Fetch the MyCRD instance
	instance := &examplev1alpha1.MyCRD{}
	err := r.GetClient().Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if ok, err := r.IsValid(instance); !ok {
		return r.ManageError(instance, err)
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

	err = r.manageOperatorLogic(instance)
	if err != nil {
		return r.ManageError(instance, err)
	}
	return r.ManageSuccess(instance)
}

func (r *ReconcileMyCRD) IsInitialized(obj metav1.Object) bool {
	mycrd, ok := obj.(*examplev1alpha1.MyCRD)
	if !ok {
		return false
	}
	if mycrd.Spec.Initialized {
		return true
	}
	util.AddFinalizer(mycrd, controllerName)
	mycrd.Spec.Initialized = true
	return false

}

func (r *ReconcileMyCRD) IsValid(obj metav1.Object) (bool, error) {
	mycrd, ok := obj.(*examplev1alpha1.MyCRD)
	if !ok {
		return false, errors.New("not a mycrd object")
	}
	if mycrd.Spec.Valid {
		return true, nil
	}
	return false, errors.New("not valid becuase blah blah")
}

func (r *ReconcileMyCRD) manageCleanUpLogic(mycrd *examplev1alpha1.MyCRD) error {
	return nil
}

func (r *ReconcileMyCRD) manageOperatorLogic(mycrd *examplev1alpha1.MyCRD) error {
	if mycrd.Spec.Error {
		return errors.New("error because blah blah")
	}
	return nil
}
