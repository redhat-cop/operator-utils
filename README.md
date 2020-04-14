# Operator Utility Library

[![Build Status](https://travis-ci.org/redhat-cop/operator-utils.svg?branch=master)](https://travis-ci.org/redhat-cop/operator-utils) [![Docker Repository on Quay](https://quay.io/repository/redhat-cop/operator-utils/status "Docker Repository on Quay")](https://quay.io/repository/redhat-cop/operator-utils)

This library layers on top of the Operator SDK and with the objective of helping writing better and more consistent operators.

## Scope of this library

This library covers three main areas:

1. [Idempotent methods](#Idempotent-Methods-to-Manipulate-Resources) to manipulate resources and array of resources
2. [Basic operator lifecycle](#Basic-Operator-Lifecycle-Management) needs (validation, initialization, status and error management, finalization)
3. [Enforcing resources operator support](#Enforcing-Resource-Operator-Support). For those operators which calculate a set of resources that need to exist and then enforce them, generalized support for the enforcing phase is provided.

## Idempotent Methods to Manipulate Resources

The following idempotent methods are provided (and their corresponding array version):

1. createIfNotExists
2. createOrUpdate
3. deleteIfExits

Also there are utility methods to manage finalizers, test ownership and process templates of resources.

## Basic Operator Lifecycle Management

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
import "github.com/operator-framework/operator-sdk/pkg/status"

...


// +k8s:openapi-gen=true
type MyCRDStatus struct {
 Conditions status.Conditions `json:"conditions"`
}

...

func (m *MyCRD) GetReconcileStatus() status.Conditions {
  return m.Status.Conditions
}

func (m *MyCRD) SetReconcileStatus(reconcileStatus status.Conditions) {
  m.Status.Conditions = reconcileStatus
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

### Managing CR validation

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

### Managing CR Initialization

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

### Managing Status and Error Conditions

To update the status with success and return from the reconciliation cycle, code the following:

```go
return r.ManageSuccess(instance)
```

To update the status with failure, record and event and return from the reconciliation cycle, code the following:

```go
return r.ManageError(instance, err)
```

notice that this function will reschedule a reconciliation cycle with increasingly longer wait time up to six hours.

### Managing CR Finalization

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

## Enforcing Resource Operator Support

Many operator have the following logic:

1. Phase 1: based on the CR and potentially additional status as set of resources that need to exist is calculated.
2. Phase 2: These resources are then created or updated against the master API.
3. Phase 3: A well written also ensures that these resources stay in place and are not accidentally or maliciously changed by third parties.

These phases are of increasing difficulty to implement. It's also true that phase 2 and 3 can be generalized.

Operator-utils offers some scaffolding to writing these kinds of operators.

Similarly to the `BaseReconciler` class, we have a base type to extend called: `EnforcingReconciler`. This class extends from `BaseReconciler`, so you have all the same facilities as above.

The body of the reconciler function will look something like this:

```golang
validation...
initialization...
(optional) finalization...
Phase1 ... calculate a set of resources to be enforced -> LockedResources

  err = r.UpdateLockedResources(instance, lockedResources)
  if err != nil {
    log.Error(err, "unable to update locked resources")
    return r.ManageError(instance, err)
 }

  return r.ManageSuccess(instance)
```

this is all you have to do for basic functionality. For mode details see the [example](pkg/controller/apis/enforcingcrd/enforcingcrd_controller.go)
the EnforcingReconciler will do the following:

1. restore the resources to the desired stated if the are changed. Notice that you can exclude paths from being considered when deciding whether to restore a resource. As set oj JSON Path can be passed together with the LockedResource. It is recommended to set these paths:
    1. `.metadata`
    2. `.status`

2. restore resources when they are deleted.

The finalization method will look like this:

```golang
func (r *ReconcileEnforcingCRD) manageCleanUpLogic(instance *examplev1alpha1.EnforcingCRD) error {
  err := r.Terminate(instance, true)
  if err != nil {
    log.Error(err, "unable to terminate enforcing reconciler for", "instance", instance)
    return err
  }
  ... additional finalization logic ...
  return nil
}
```

## Local Development

Execute the following steps to develop the functionality locally. It is recommended that development be done using a cluster with `cluster-admin` permissions.

```shell
go mod download
```

optionally:

```shell
go mod vendor
```

Using the [operator-sdk](https://github.com/operator-framework/operator-sdk), run the operator locally:

```shell
oc apply -f deploy/crds
OPERATOR_NAME='example-operator' operator-sdk --verbose run --local --namespace ""
```

## Testing

### Enforcing CRD testing

```shell
oc new-project test-enforcingcrd
oc apply -f test/enforcing_cr.yaml -n test-enforcingcrd
```

## License

This project is licensed under the [Apache License, Version 2.0](https://www.apache.org/licenses/LICENSE-2.0).

## Release Process

To release execute the following:

```shell
git tag -a "<version>" -m "release <version>"
git push upstream <version>
```

use this version format: vM.m.z
