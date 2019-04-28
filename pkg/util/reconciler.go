package util

import (
	"context"
	"fmt"
	"text/template"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ReconcilerBase struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

func NewReconcilerBase(client client.Client, scheme *runtime.Scheme) ReconcilerBase {
	return ReconcilerBase{
		client: client,
		scheme: scheme,
	}
}

func (r *ReconcilerBase) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

func (r *ReconcilerBase) GetClient() client.Client {
	return r.client
}

func (r *ReconcilerBase) GetScheme() *runtime.Scheme {
	return r.scheme
}

// CreateOrUpdateResource creates a resource if it doesn't exist, and updates (overwrites it), if it exist
// if owner is not nil, the owner field os set
// if namespace is not "", the namespace field of the object is overwritten with the passed value
func (r *ReconcilerBase) CreateOrUpdateResource(owner metav1.Object, namespace string, obj metav1.Object) error {
	runtimeObj, ok := (obj).(runtime.Object)
	if !ok {
		return fmt.Errorf("is not a %T a runtime.Object", obj)
	}

	if owner != nil {
		_ = controllerutil.SetControllerReference(owner, obj, r.GetScheme())
	}
	if namespace != "" {
		obj.SetNamespace(namespace)
	}

	obj2 := unstructured.Unstructured{}
	obj2.SetKind(runtimeObj.GetObjectKind().GroupVersionKind().Kind)
	if runtimeObj.GetObjectKind().GroupVersionKind().Group != "" {
		obj2.SetAPIVersion(runtimeObj.GetObjectKind().GroupVersionKind().Group + "/" + runtimeObj.GetObjectKind().GroupVersionKind().Version)
	} else {
		obj2.SetAPIVersion(runtimeObj.GetObjectKind().GroupVersionKind().Version)
	}

	err := r.GetClient().Get(context.TODO(), types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}, &obj2)

	if apierrors.IsNotFound(err) {
		err = r.GetClient().Create(context.TODO(), runtimeObj)
		if err != nil {
			log.Error(err, "unable to create object", "object", runtimeObj)
		}
		return err
	}
	if err == nil {
		obj.SetResourceVersion(obj2.GetResourceVersion())
		err = r.GetClient().Update(context.TODO(), runtimeObj)
		if err != nil {
			log.Error(err, "unable to update object", "object", runtimeObj)
		}
		return err

	}
	log.Error(err, "unable to lookup object", "object", runtimeObj)
	return err
}

// CreateOrUpdateResources operates as CreateOrUpdate, but on an array of resources
func (r *ReconcilerBase) CreateOrUpdateResources(owner metav1.Object, namespace string, objs []metav1.Object) error {
	for _, obj := range objs {
		err := r.CreateOrUpdateResource(owner, namespace, obj)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteResource deletes an existing resource. It doesn't fail if the resource does not exist
func (r *ReconcilerBase) DeleteResource(obj metav1.Object) error {
	runtimeObj, ok := (obj).(runtime.Object)
	if !ok {
		return fmt.Errorf("is not a %T a runtime.Object", obj)
	}

	err := r.GetClient().Delete(context.TODO(), runtimeObj, nil)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "unable to delete object ", "object", runtimeObj)
		return err
	}
	return nil
}

// DeleteResources operates like DeleteResources, but on an arrays of resources
func (r *ReconcilerBase) DeleteResources(objs []metav1.Object) error {
	for _, obj := range objs {
		err := r.DeleteResource(obj)
		if err != nil {
			return err
		}
	}
	return nil
}

// CreateResourceIfNotExists create a resource if it doesn't already exists. If the resource exists it is left untouched and the functin does not fails
// if owner is not nil, the owner field os set
// if namespace is not "", the namespace field of the object is overwritten with the passed value
func (r *ReconcilerBase) CreateResourceIfNotExists(owner metav1.Object, namespace string, obj metav1.Object) error {
	runtimeObj, ok := (obj).(runtime.Object)
	if !ok {
		return fmt.Errorf("is not a %T a runtime.Object", obj)
	}

	if owner != nil {
		_ = controllerutil.SetControllerReference(owner, obj, r.GetScheme())
	}
	if namespace != "" {
		obj.SetNamespace(namespace)
	}

	err := r.GetClient().Create(context.TODO(), runtimeObj)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		log.Error(err, "unable to create object ", "object", runtimeObj)
		return err
	}
	return nil
}

// CreateResourcesIfNotExist operates as CreateResourceIfNotExists, but on an array of resources
func (r *ReconcilerBase) CreateResourcesIfNotExist(owner metav1.Object, namespace string, objs []metav1.Object) error {
	for _, obj := range objs {
		err := r.CreateResourceIfNotExists(owner, namespace, obj)
		if err != nil {
			return err
		}
	}
	return nil
}

// CreateOrUpdateTemplatedResources processes an initialized template expecting an array of objects as a result and the processes them with the CreateOrUpdate function
func (r *ReconcilerBase) CreateOrUpdateTemplatedResources(owner metav1.Object, namespace string, data interface{}, template *template.Template) error {
	objs, err := ProcessTemplateArray(data, template)
	if err != nil {
		log.Error(err, "error creating manifest from template")
		return err
	}
	for _, obj := range *objs {
		err = r.CreateOrUpdateResource(owner, namespace, &obj)
		if err != nil {
			return err
		}
	}
	return nil
}

// CreateIfNotExistTemplatedResources processes an initialized template expecting an array of objects as a result and then processes them with the CreateResourceIfNotExists function
func (r *ReconcilerBase) CreateIfNotExistTemplatedResources(owner metav1.Object, namespace string, data interface{}, template *template.Template) error {
	objs, err := ProcessTemplateArray(data, template)
	if err != nil {
		log.Error(err, "error creating manifest from template")
		return err
	}
	for _, obj := range *objs {
		err = r.CreateResourceIfNotExists(owner, namespace, &obj)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteTemplatedResources processes an initialized template expecting an array of objects as a result and then processes them with the Delete function
func (r *ReconcilerBase) DeleteTemplatedResources(data interface{}, template *template.Template) error {
	objs, err := ProcessTemplateArray(data, template)
	if err != nil {
		log.Error(err, "error creating manifest from template")
		return err
	}
	for _, obj := range *objs {
		err = r.DeleteResource(&obj)
		if err != nil {
			return err
		}
	}
	return nil
}
