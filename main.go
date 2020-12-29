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

package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	operatorutilsv1alpha1 "github.com/redhat-cop/operator-utils/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/controllers"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/lockedresourcecontroller"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(operatorutilsv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                     scheme,
		MetricsBindAddress:         metricsAddr,
		Port:                       9443,
		LeaderElection:             enableLeaderElection,
		LeaderElectionID:           "a0942526.example.io",
		LeaderElectionResourceLock: "configmaps",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.MyCRDReconciler{
		ReconcilerBase: util.NewFromManager(mgr, mgr.GetEventRecorderFor("MyCRD_controller")),
		Log:            ctrl.Log.WithName("controllers").WithName("MyCRD"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MyCRD")
		os.Exit(1)
	}
	if err = (&controllers.EnforcingCRDReconciler{
		EnforcingReconciler: lockedresourcecontroller.NewFromManager(mgr, mgr.GetEventRecorderFor("EnforcingCRD_controller"), true),
		Log:                 ctrl.Log.WithName("controllers").WithName("EnforcingCRD"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "EnforcingCRD")
		os.Exit(1)
	}
	if err = (&controllers.EnforcingPatchReconciler{
		EnforcingReconciler: lockedresourcecontroller.NewFromManager(mgr, mgr.GetEventRecorderFor("EnforcingPatch_controller"), true),
		Log:                 ctrl.Log.WithName("controllers").WithName("EnforcingPatch"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "EnforcingPatch")
		os.Exit(1)
	}
	if err = (&controllers.TemplatedEnforcingCRDReconciler{
		EnforcingReconciler: lockedresourcecontroller.NewFromManager(mgr, mgr.GetEventRecorderFor("TemplatedEnforcingCRD_controller"), true),
		Log:                 ctrl.Log.WithName("controllers").WithName("TemplatedEnforcingCRD"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TemplatedEnforcingCRD")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
