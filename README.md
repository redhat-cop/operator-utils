# Operator Utility Library

This library layers on top of the Operator SDK and with the objective of helping writing better and more consistent operators.

To get started with this library do the following:

Change your reconciler initialization as exemplified below to add a set of utility methods to it

```go
import "github.com/redhat-cop/operator-utils/pkg/util"

...
type MyReconciler struct {
  util.ReconcilerBase
  ... other optional fields ...
}

...

func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileMyCRD{
		ReconcilerBase: util.NewReconcilerBase(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetRecorder(controllerName)),
	}
}
```

If you want status management, add this to your CRD:

```go
import "github.com/redhat-cop/operator-utils/pkg/util/apis"

...


// +k8s:openapi-gen=true
type MyCRDStatus struct {
	apis.ReconcileStatus `json:",inline"`
}

...

func (m *MyCRD) GetReconcileStatus() apis.ReconcileStatus {
	return m.Status.ReconcileStatus
}

func (m *MyCRD) SetReconcileStatus(reconcileStatus apis.ReconcileStatus) {
	m.Status.ReconcileStatus = reconcileStatus
}

```

At this point your controller is able to reuse leverage the utility methods of this library:

1. [managing CR validation](#managing-cr-validation)
2. [managing CR initialization](#managing-cr-initialization)
3. [managing status and error conditions](#managing-status-and-error-conditions)
4. [managing CR finalization](#managing-cr-finalization)
5. high-level object manipulation functions such as:
   - createOrUpdate, createIfNotExists, DeleteIfExists
   - same functions on an array of objects
   - go template processing of objects

A full example is provided [here](./pkg/controller/mycrd/mycrd_controller.go)

## Managing CR validation

To enable CR validation add this to your controller:

```go
if ok, err := r.IsValid(instance); !ok {
	return r.ManageError(instance, err)
}
```

The implement the following function:

```go
func (r *ReconcileMyCRD) IsValid(obj metav1.Object) (bool, error) {
	mycrd, ok := obj.(*examplev1alpha1.MyCRD)
	...
}
```

## Managing CR Initialization

To enable CR initialization, add this to your controller:

```go
if ok := r.IsInitialized(instance); !ok {
	err := r.GetClient().Update(context.TODO(), instance)
	if err != nil {
		log.Error(err, "unable to update instance", "instance", instance)
		return r.ManageError(instance, err)
	}
	return reconcile.Result{}, nil
}
```

Then implement the following function:

```go
func (r *ReconcileMyCRD) IsInitialized(obj metav1.Object) bool {
	mycrd, ok := obj.(*examplev1alpha1.MyCRD)
}
```

## Managing Status and Error Conditions

To update the status with success and return from the reconciliation cycle, code the following:

```go
return r.ManageSuccess(instance)
```

To update the status with failure, record and event and return from the reconciliation cycle, code the following:

```go
return r.ManageError(instance, err)
```

notice that this function will reschedule a reconciliation cycle with increasingly longer wait time up to six hours.

## Managing CR Finalization

to enable CR finalization add this to your controller:

```go
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
```

Then implement this method:

```go
func (r *ReconcileMyCRD) manageCleanUpLogic(mycrd *examplev1alpha1.MyCRD) error {
  ...
}
```
