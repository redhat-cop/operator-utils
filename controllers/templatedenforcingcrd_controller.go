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

	"github.com/redhat-cop/operator-utils/v2/api/v1alpha1"
	operatorutilsv1alpha1 "github.com/redhat-cop/operator-utils/v2/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/v2/pkg/util"
	"github.com/redhat-cop/operator-utils/v2/pkg/util/lockedresourcecontroller"
	"github.com/redhat-cop/operator-utils/v2/pkg/util/lockedresourcecontroller/lockedpatch"
	"github.com/redhat-cop/operator-utils/v2/pkg/util/lockedresourcecontroller/lockedresource"
)

// TemplatedEnforcingCRDReconciler reconciles a TemplatedEnforcingCRD object
type TemplatedEnforcingCRDReconciler struct {
	lockedresourcecontroller.EnforcingReconciler
	Log logr.Logger
}

// +kubebuilder:rbac:groups=operator-utils.example.io,resources=templatedenforcingcrds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator-utils.example.io,resources=templatedenforcingcrds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=*,resources=*,verbs=*

func (r *TemplatedEnforcingCRDReconciler) Reconcile(context context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("templatedenforcingcrd", req.NamespacedName)

	// Fetch the TemplatedEnforcingCRD instance
	instance := &v1alpha1.TemplatedEnforcingCRD{}
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

	lockedResources, err := lockedresource.GetLockedResourcesFromTemplatesWithRestConfig(instance.Spec.Templates, r.GetRestConfig(), instance)
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

// IsInitialized can be used to check if instance is correctly initialized.
// returns false it isn't.
func (r *TemplatedEnforcingCRDReconciler) IsInitialized(instance *v1alpha1.TemplatedEnforcingCRD) bool {
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

func (r *TemplatedEnforcingCRDReconciler) manageCleanUpLogic(instance *v1alpha1.TemplatedEnforcingCRD) error {
	err := r.Terminate(instance, true)
	if err != nil {
		r.Log.Error(err, "unable to terminate enforcing reconciler for", "instance", instance)
		return err
	}
	return nil
}

func (r *TemplatedEnforcingCRDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorutilsv1alpha1.TemplatedEnforcingCRD{}).
		Watches(&source.Channel{Source: r.GetStatusChangeChannel()}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
