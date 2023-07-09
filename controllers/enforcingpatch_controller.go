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
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/redhat-cop/operator-utils/api/v1alpha1"
	operatorutilsv1alpha1 "github.com/redhat-cop/operator-utils/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedpatch"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller/lockedresource"
)

// EnforcingPatchReconciler reconciles a EnforcingPatch object
type EnforcingPatchReconciler struct {
	lockedresourcecontroller.EnforcingReconciler
	Log logr.Logger
}

// +kubebuilder:rbac:groups=operator-utils.example.io,resources=enforcingpatches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator-utils.example.io,resources=enforcingpatches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=*,resources=*,verbs=*

func (r *EnforcingPatchReconciler) Reconcile(context context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("enforcingpatch", req.NamespacedName)

	// Fetch the EnforcingPatch instance
	instance := &v1alpha1.EnforcingPatch{}
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

	lockedPatches, err := lockedpatch.GetLockedPatches(instance.Spec.Patches, r.GetRestConfig(), log)
	if err != nil {
		log.Error(err, "unable to get locked patches")
		return r.ManageError(context, instance, err)
	}
	err = r.UpdateLockedResources(context, instance, []lockedresource.LockedResource{}, lockedPatches)
	if err != nil {
		log.Error(err, "unable to update locked pacthes")
		return r.ManageError(context, instance, err)
	}

	return r.ManageSuccess(context, instance)
}

// IsInitialized can be used to check if instance is correctly initialized.
// returns false it isn't.
func (r *EnforcingPatchReconciler) IsInitialized(instance *v1alpha1.EnforcingPatch) bool {
	needsUpdate := true
	for i, patch := range instance.Spec.Patches {

		if patch.PatchType == "" {
			patch.PatchType = "application/strategic-merge-patch+json"
			instance.Spec.Patches[i] = patch
			needsUpdate = false
		}
	}
	return needsUpdate
}
func (r *EnforcingPatchReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorutilsv1alpha1.EnforcingPatch{}).
		WatchesRawSource(&source.Channel{Source: r.GetStatusChangeChannel()}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
