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

	"github.com/go-logr/logr"
	"github.com/scylladb/go-set/strset"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/redhat-cop/operator-utils/api/v1alpha1"
	operatorutilsv1alpha1 "github.com/redhat-cop/operator-utils/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedpatch"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedresource"
)

// EnforcingCRDReconciler reconciles a EnforcingCRD object
type EnforcingCRDReconciler struct {
	lockedresourcecontroller.EnforcingReconciler
	Log logr.Logger
}

// +kubebuilder:rbac:groups=operator-utils.example.io,resources=enforcingcrds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator-utils.example.io,resources=enforcingcrds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=*,resources=*,verbs=*

func (r *EnforcingCRDReconciler) Reconcile(context context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("enforcingcrd", req.NamespacedName)

	// Fetch the EnforcingCRD instance
	instance := &v1alpha1.EnforcingCRD{}
	err := r.GetClient().Get(context, req.NamespacedName, instance)
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
	log.V(1).Info("about to reconcile", "instance version", instance.GetResourceVersion())
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

	lockedResources, err := lockedresource.GetLockedResources(instance.Spec.Resources)
	if err != nil {
		log.Error(err, "unable to get locked resources")
		return r.ManageError(context, instance, err)
	}
	err = r.UpdateLockedResources(context, instance, lockedResources, []lockedpatch.LockedPatch{})
	if err != nil {
		log.Error(err, "unable to update locked resources")
		return r.ManageError(context, instance, err)
	}

	return r.ManageSuccess(context, instance)
}

func (r *EnforcingCRDReconciler) manageCleanUpLogic(instance *v1alpha1.EnforcingCRD) error {
	err := r.Terminate(instance, true)
	if err != nil {
		r.Log.Error(err, "unable to terminate enforcing reconciler for", "instance", instance)
		return err
	}
	return nil
}

// IsInitialized can be used to check if instance is correctly initialized.
// returns false it isn't.
func (r *EnforcingCRDReconciler) IsInitialized(instance *v1alpha1.EnforcingCRD) bool {
	needsUpdate := false
	for i := range instance.Spec.Resources {
		currentSet := strset.New(instance.Spec.Resources[i].ExcludedPaths...)
		if !currentSet.IsEqual(strset.Union(lockedresource.DefaultExcludedPathsSet, currentSet)) {
			instance.Spec.Resources[i].ExcludedPaths = strset.Union(lockedresource.DefaultExcludedPathsSet, currentSet).List()
			needsUpdate = true
		}
	}
	if len(instance.Spec.Resources) > 0 && !util.HasFinalizer(instance, controllerName) {
		util.AddFinalizer(instance, controllerName)
		needsUpdate = true
	}
	if len(instance.Spec.Resources) == 0 && util.HasFinalizer(instance, controllerName) {
		util.RemoveFinalizer(instance, controllerName)
		needsUpdate = true
	}
	return !needsUpdate
}

func (r *EnforcingCRDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorutilsv1alpha1.EnforcingCRD{}).
		Watches(&source.Channel{Source: r.GetStatusChangeChannel()}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
