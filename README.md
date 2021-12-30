# Operator Utility Library

![build status](https://github.com/redhat-cop/operator-utils/workflows/push/badge.svg)
[![GoDoc reference](https://img.shields.io/badge/godoc-reference-blue.svg)](https://pkg.go.dev/github.com/redhat-cop/operator-utils)
[![Go Report Card](https://goreportcard.com/badge/github.com/redhat-cop/operator-utils)](https://goreportcard.com/report/github.com/redhat-cop/operator-utils)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/redhat-cop/operator-utils)

This library layers on top of the Operator SDK and with the objective of helping writing better and more consistent operators.

*NOTICE* versions of this library up to `v0.3.7` are compatible with [operator-sdk](https://github.com/operator-framework/operator-sdk) `0.x`, starting from version v0.4.0 this library will be compatible only with [operator-sdk](https://github.com/operator-framework/operator-sdk) 1.x.

## Scope of this library

This library covers three main areas:

1. [Utility Methods](#Utility-Methods) Utility methods that are callable by any operator.
2. [Idempotent methods](#Idempotent-Methods-to-Manipulate-Resources) to manipulate resources and arrays of resources
3. [Basic operator lifecycle](#Basic-Operator-Lifecycle-Management) needs (validation, initialization, status and error management, finalization)
4. [Enforcing resources operator support](#Enforcing-Resource-Operator-Support). For those operators which calculate a set of resources that need to exist and then enforce them, generalized support for the enforcing phase is provided.

## Utility Methods

Prior to version v1.3.x the general philosophy of this library was that new operator would inherit from `ReconcilerBase` and in doing so they would have access to a bunch of utility methods.
With release v1.3.0 a new approach is available. Utility methods are callable by any operator having to inherit. This makes it easier to use this library and does not conflict with autogenerate code from `kube-builder` and `operator-sdk`.
Most of the Utility methods receive a context.Context parameter. Normally this context must be initialized with a `logr.Logger` and a `rest.Config`. Some utility methods may require more, see each individual documentation.

Utility methods are currently organized in the following folders:

1. crud: idempotent create/update/delete functions.
2. discoveryclient: methods related to the discovery client, typically used to load `apiResource` objects.
3. dynamicclient: methods related to building client based on object whose type is not known at compile time.
4. templates: utility methods for dealing with templates whose output is an object or a list of objects.

## Idempotent Methods to Manipulate Resources

The following idempotent methods are provided (and their corresponding array version):

1. createIfNotExists
2. createOrUpdate
3. deleteIfExists

Also there are utility methods to manage finalizers, test ownership and process templates of resources.

## Basic Operator Lifecycle Management

---

Note

This part of the library is largely deprecated. For initialization and defaulting a MutatingWebHook should be used. For validation a Validating WebHook should be used.
The part regarding the finalization is still relevant.

---

To get started with this library do the following:

Change your reconciler initialization as exemplified below to add a set of utility methods to it

```go
import "github.com/redhat-cop/operator-utils/pkg/util"

...
type MyReconciler struct {
  util.ReconcilerBase
  Log logr.Logger
  ... other optional fields ...
}
```

in main.go change like this

```go
  if err = (&controllers.MyReconciler{
    ReconcilerBase: util.NewReconcilerBase(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetEventRecorderFor("My_controller"), mgr.GetAPIReader()),
    Log:            ctrl.Log.WithName("controllers").WithName("My"),
  }).SetupWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create controller", "controller", "My")
    os.Exit(1)
  }
```

Also make sure to create the manager with `configmap` as the lease option for leader election:

```go
  mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
    Scheme:                     scheme,
    MetricsBindAddress:         metricsAddr,
    Port:                       9443,
    LeaderElection:             enableLeaderElection,
    LeaderElectionID:           "dcb036b8.redhat.io",
    LeaderElectionResourceLock: "configmaps",
  })
```  

If you want status management, add this to your CRD:

```go
  // +patchMergeKey=type
  // +patchStrategy=merge
  // +listType=map
  // +listMapKey=type
  Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

func (m *MyCRD) GetConditions() []metav1.Condition {
  return m.Status.Conditions
}

func (m *MyCRD) SetConditions(conditions []metav1.Condition) {
  m.Status.Conditions = conditions
}
```

At this point your controller is able to leverage the utility methods of this library:

1. [managing CR validation](#managing-cr-validation)
2. [managing CR initialization](#managing-cr-initialization)
3. [managing status and error conditions](#managing-status-and-error-conditions)
4. [managing CR finalization](#managing-cr-finalization)
5. high-level object manipulation functions such as:
   - createOrUpdate, createIfNotExists, deleteIfExists
   - same functions on an array of objects
   - go template processing of objects

A full example is provided [here](./controllers/mycrd_controller.go)

### Managing CR validation

To enable CR validation add this to your controller:

```go
if ok, err := r.IsValid(instance); !ok {
 return r.ManageError(ctx, instance, err)
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
  return r.ManageError(ctx, instance, err)
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
return r.ManageSuccess(ctx, instance)
```

To update the status with failure, record an event and return from the reconciliation cycle, code the following:

```go
return r.ManageError(ctx, instance, err)
```

notice that this function will reschedule a reconciliation cycle with increasingly longer wait time up to six hours.

There are also variants of these calls to allow for requeuing after a given delay.
Requeuing is handy when reconciliation depends on a cluster-external state which is not observable from within the api-server.

```go
return r.ManageErrorWithRequeue(ctx, instance, err, 3*time.Second)
```

```go
return r.ManageSuccessWithRequeue(ctx, instance, 3*time.Second)
```

or simply using the convenience function:

```go
return r.ManageOutcomeWithRequeue(ctx, instance, err, 3*time.Second)
```

which will delegate to the error or success variant depending on `err` being `nil` or not.

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
  return r.ManageError(ctx, instance, err)
 }
 util.RemoveFinalizer(instance, controllerName)
 err = r.GetClient().Update(context.TODO(), instance)
 if err != nil {
  log.Error(err, "unable to update instance", "instance", instance)
  return r.ManageError(ctx, instance, err)
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

## Support for operators that need to enforce a set of resources to a defined state

Many operators have the following logic:

1. Phase 1: based on the CR and potentially additional status, a set of resources that need to exist is calculated.
2. Phase 2: These resources are then created or updated against the master API.
3. Phase 3: A well written operator also ensures that these resources stay in place and are not accidentally or maliciously changed by third parties.

These phases are of increasing difficulty to implement. It's also true that phase 2 and 3 can be generalized.

Operator-utils offers some scaffolding to assist in writing these kinds of operators.

Similarly to the `BaseReconciler` class, we have a base type to extend called: `EnforcingReconciler`. This class extends from `BaseReconciler`, so you have all the same facilities as above.

When initializing the EnforcingReconciler, one must chose whether watchers will be created at the cluster level or at the namespace level.

- if cluster level is chosen a watch per CR and type defined in it will be created. This will require the operator to have cluster level access.

- if namespace level watchers is chosen a watch per CR, type and namespace will be created. This will minimize the needed permissions, but depending on what the operator needs to do may open a very high number of connections to the API server.

The body of the reconciler function will look something like this:

```golang
validation...
initialization...
(optional) finalization...
Phase1 ... calculate a set of resources to be enforced -> LockedResources

  err = r.UpdateLockedResources(context,instance, lockedResources, ...)
  if err != nil {
    log.Error(err, "unable to update locked resources")
    return r.ManageError(ctx, instance, err)
 }

  return r.ManageSuccess(ctx, instance)
```

this is all you have to do for basic functionality. For more details see the [example](pkg/controller/apis/enforcingcrd/enforcingcrd_controller.go)
the EnforcingReconciler will do the following:

1. restore the resources to the desired stated if the are changed. Notice that you can exclude paths from being considered when deciding whether to restore a resource. As set of JSON Path can be passed together with the LockedResource. It is recommended to set these paths:
    1. `.metadata`
    2. `.status`

2. restore resources when they are deleted.

The `UpdateLockedResources` will validate the input as follows:

1. the passed resource must be defined in the current apiserver
2. the passed resource must be syntactically compliant with the OpenAPI definition of the resource defined in the server.
3. if the passed resource is namespaced, the namespace field must be initialized.

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

Convenience methods are also available for when resources are templated. See the [templatedenforcingcrd](./pkgcontroller/templatedenforcingcrd/templatedenforcingcrd_controller.go) controller as an example.

## Support for operators that need to enforce a set of patches

For similar reasons stated in the previous paragraphs, operators might need to enforce patches.
A patch modifies an object created by another entity. Because in this case the CR does not own the to-be-modified object a patch must be enforced against changes made on it.
One must be careful not to create circular situations where an operator deletes the patch and this operator recreates the patch.
In some situations, a patch must be parametric on some state of the cluster. For this reason, it's possible to monitor source objects that will be used as parameters to calculate the patch.

A patch is defined as follows:

```golang
type LockedPatch struct { 
  Name             string                           `json:"name,omitempty"`
  SourceObjectRefs []utilsapi.SourceObjectReference `json:"sourceObjectRefs,omitempty"`
  TargetObjectRef  utilsapi.TargetObjectReference   `json:"targetObjectRef,omitempty"`
  PatchType        types.PatchType                  `json:"patchType,omitempty"`
  PatchTemplate    string                           `json:"patchTemplate,omitempty"`
  Template         template.Template                `json:"-"`
}
```

the targetObjectRef and sourceObjectRefs are watched for changes by the reconciler.

targetObjectRef can select multiple objects, this is the logic

| Namespaced Type | Namespace | Name | Selection type |
| --- | --- | --- | --- |
| yes | null | null | multiple selection across namespaces |
| yes | null | not null | multiple selection across namespaces where the name corresponds to the passed name |
| yes | not null | null | multiple selection within a namespace |
| yes | not null | not nul | single selection |
| no | N/A | null | multiple selection  |
| no | N/A | not null | single selection |

Selection can be further narrowed down by filtering by labels and/or annotations. The patch will be applied to all of the selected instances.

Name and Namespace of sourceRefObjects are interpreted as golang templates with the current target instance and the only parameter. This allows to select different source object for each target object.

The relevant part of the operator code would look like this:

```golang
validation...
initialization...
Phase1 ... calculate a set of patches to be enforced -> LockedPatches

  err = r.UpdateLockedResources(context, instance, ..., lockedPatches...)
  if err != nil {
    log.Error(err, "unable to update locked resources")
    return r.ManageError(ctx, instance, err)
 }

  return r.ManageSuccess(ctx, instance)
```

The `UpdateLockedResources` will validate the input as follows:

1. the passed patch target/source `ObjectRef` resource must be defined in the current apiserver
2. if the passed patch target/source `ObjectRef` resources are namespaced the corresponding namespace field must be initialized.
3. the ID must have a not null and unique value in the array of the passed patches.

Patches cannot be undone so there is no need to manage a finalizer.

[Here](./pkg/controller/enforcingpatch/enforcingpatch_controller.go) you can find an example of how to implement an operator with this the ability to enforce patches.

## Support for operators that need dynamic creation of locked resources using templates

Operators may also need to leverage locked resources created dynamically through templates. This can be done using [go templates](https://golang.org/pkg/text/template/) and leveraging the `GetLockedResourcesFromTemplates` function.

```golang
lockedResources, err := r.GetLockedResourcesFromTemplates(templates..., params...)
if err != nil {
  log.Error(err, "unable to process templates with param")
  return err
}
```

The `GetLockedResourcesFromTemplates` will validate the input as follows:

1. check that the passed template is valid
2. format the template using the properties of the passed object in the params parameter
3. create an array of `LockedResource` objects based on parsed template

The example below shows how templating can be used to reference the name of the resource passed as the parameter and use it as a property in the creation of the `LockedResource`.

```golang
objectTemplate: |
  apiVersion: v1
  kind: Namespace
  metadata:
    name: {{ .Name }}
```

This functionality can leverage advanced features of go templating, such as loops, to generate more than one object following a set pattern. The below example will create an array of namespace `LockedResources` using the title of any key where the associated value matches the text *devteam* in the key/value pair of the `Labels` property of the resource passed in the params parameter.

```golang
objectTemplate: |
  {{range $key, $value := $.Labels}}
    {{if eq $value "devteam"}}
      - apiVersion: v1
        kind: Namespace
        metadata:
          name: {{ $key }}
    {{end}}
  {{end}}
```

## Support for operators that need advanced templating functionality

Operators may need to utilize advanced templating functions not found in the base go templating library. This advanced template functionality matches the same available in the popular k8s management tool [Helm](https://helm.sh/). `LockedPatch` templates uses this functionality by default. To utilize these features when using `LockedResources` the following function is required,

```golang
lockedResources, err := r.GetLockedResourcesFromTemplatesWithRestConfig(templates..., rest.Config..., params...)
if err != nil {
  log.Error(err, "unable to process templates with param")
  return err
}
```  

## Deployment

### Deploying with Helm

Here are the instructions to install the latest release with Helm.

```shell
oc new-project operator-utils
helm repo add operator-utils https://redhat-cop.github.io/operator-utils
helm repo update
helm install operator-utils operator-utils/operator-utils
```

This can later be updated with the following commands:

```shell
helm repo update
helm upgrade operator-utils operator-utils/operator-utils
```

## Development

## Running the operator locally

```shell
make install
oc new-project operator-utils-operator-local
kustomize build ./config/local-development | oc apply -f - -n operator-utils-operator-local
export token=$(oc serviceaccounts get-token 'operator-utils-operator-controller-manager' -n operator-utils-operator-local)
oc login --token ${token}
make run ENABLE_WEBHOOKS=false
```

### testing

Patches

```shell
oc new-project patch-test
oc create sa test -n patch-test
oc adm policy add-cluster-role-to-user cluster-admin -z default -n patch-test
oc apply -f ./test/enforcing-patch.yaml -n patch-test
oc apply -f ./test/enforcing-patch-multiple.yaml -n patch-test
oc apply -f ./test/enforcing-patch-multiple-cluster-level.yaml -n patch-test
```

## Building/Pushing the operator image

```shell
export repo=raffaelespazzoli #replace with yours
docker login quay.io/$repo
make docker-build IMG=quay.io/$repo/operator-utils:latest
make docker-push IMG=quay.io/$repo/operator-utils:latest
```

## Deploy to OLM via bundle

```shell
make manifests
make bundle IMG=quay.io/$repo/operator-utils:latest
operator-sdk bundle validate ./bundle --select-optional name=operatorhub
make bundle-build BUNDLE_IMG=quay.io/$repo/operator-utils-bundle:latest
docker push quay.io/$repo/operator-utils-bundle:latest
operator-sdk bundle validate quay.io/$repo/operator-utils-bundle:latest --select-optional name=operatorhub
oc new-project operator-utils
oc label namespace operator-utils openshift.io/cluster-monitoring="true"
operator-sdk cleanup operator-utils -n operator-utils
operator-sdk run bundle --install-mode AllNamespaces -n operator-utils quay.io/$repo/operator-utils-bundle:latest
```

## Releasing

```shell
git tag -a "<tagname>" -m "<commit message>"
git push upstream <tagname>
```

If you need to remove a release:

```shell
git tag -d <tagname>
git push upstream --delete <tagname>
```

If you need to "move" a release to the current main

```shell
git tag -f <tagname>
git push upstream -f <tagname>
```

### Cleaning up

```shell
operator-sdk cleanup operator-utils -n operator-utils
oc delete operatorgroup operator-sdk-og
oc delete catalogsource operator-utils-catalog
```
