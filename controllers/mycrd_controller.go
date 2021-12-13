/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	"github.com/redhat-cop/operator-utils/api/v1alpha1"
	operatorutilsv1alpha1 "github.com/redhat-cop/operator-utils/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const controllerName = "MyCRD_controller"

//var log = logf.Log.WithName(controllerName)

// MyCRDReconciler reconciles a MyCRD object
type MyCRDReconciler struct {
	util.ReconcilerBase
	Log logr.Logger
}

// +kubebuilder:rbac:groups=operator-utils.example.io,resources=mycrds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator-utils.example.io,resources=mycrds/status,verbs=get;update;patch

func (r *MyCRDReconciler) Reconcile(context context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("mycrd", req.NamespacedName)

	// Fetch the MyCRD instance
	instance := &v1alpha1.MyCRD{}
	err := r.GetClient().Get(context, req.NamespacedName, instance)
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
		return r.ManageError(context, instance, err)
	}

	if ok := r.IsInitialized(instance); !ok {
		err := r.GetClient().Update(context, instance)
		if err != nil {
			log.Error(err, "unable to update instance", "instance", instance)
			return r.ManageError(context, instance, err)
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
			return r.ManageError(context, instance, err)
		}
		util.RemoveFinalizer(instance, controllerName)
		err = r.GetClient().Update(context, instance)
		if err != nil {
			log.Error(err, "unable to update instance", "instance", instance)
			return r.ManageError(context, instance, err)
		}
		return reconcile.Result{}, nil
	}

	err = r.manageOperatorLogic(instance)
	if err != nil {
		return r.ManageError(context, instance, err)
	}
	return r.ManageSuccess(context, instance)
}

func (r *MyCRDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorutilsv1alpha1.MyCRD{}).
		Complete(r)
}

func (r *MyCRDReconciler) IsInitialized(obj metav1.Object) bool {
	mycrd, ok := obj.(*v1alpha1.MyCRD)
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

func (r *MyCRDReconciler) IsValid(obj metav1.Object) (bool, error) {
	mycrd, ok := obj.(*v1alpha1.MyCRD)
	if !ok {
		return false, errors.New("not a mycrd object")
	}
	if mycrd.Spec.Valid {
		return true, nil
	}
	return false, errors.New("not valid because blah blah")
}

func (r *MyCRDReconciler) manageCleanUpLogic(mycrd *v1alpha1.MyCRD) error {
	return nil
}

func (r *MyCRDReconciler) manageOperatorLogic(mycrd *v1alpha1.MyCRD) error {
	if mycrd.Spec.Error {
		return errors.New("error because blah blah")
	}
	return nil
}
