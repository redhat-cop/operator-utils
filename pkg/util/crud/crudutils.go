package crud

import (
	"context"
	"text/template"

	"github.com/redhat-cop/operator-utils/v2/pkg/util/templates"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// CreateOrUpdateResource creates a resource if it doesn't exist, and updates (overwrites it), if it exist
// if owner is not nil, the owner field os set
// if namespace is not "", the namespace field of the object is overwritten with the passed value
// requires a context with log and client
func CreateOrUpdateResource(context context.Context, owner client.Object, namespace string, obj client.Object) error {
	log := log.FromContext(context)
	client := context.Value("client").(client.Client)
	if owner != nil {
		_ = controllerutil.SetControllerReference(owner, obj, client.Scheme())
	}
	if namespace != "" {
		obj.SetNamespace(namespace)
	}

	obj2 := &unstructured.Unstructured{}
	obj2.SetGroupVersionKind(obj.GetObjectKind().GroupVersionKind())

	err := client.Get(context, types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}, obj2)

	if apierrors.IsNotFound(err) {
		err = client.Create(context, obj)
		if err != nil {
			log.Error(err, "unable to create object", "object", obj)
			return err
		}
		return nil
	}
	if err == nil {
		obj.SetResourceVersion(obj2.GetResourceVersion())
		err = client.Update(context, obj)
		if err != nil {
			log.Error(err, "unable to update object", "object", obj)
			return err
		}
		return nil

	}
	log.Error(err, "unable to lookup object", "object", obj)
	return err
}

// CreateOrUpdateResources operates as CreateOrUpdate, but on an array of resources
// requires a context with log and client
func CreateOrUpdateResources(context context.Context, owner client.Object, namespace string, objs []client.Object) error {
	for _, obj := range objs {
		err := CreateOrUpdateResource(context, owner, namespace, obj)
		if err != nil {
			return err
		}
	}
	return nil
}

// CreateOrUpdateUnstructuredResources operates as CreateOrUpdate, but on an array of unstructured.Unstructured
// requires a context with log and client
func CreateOrUpdateUnstructuredResources(context context.Context, owner client.Object, namespace string, objs []unstructured.Unstructured) error {
	for _, obj := range objs {
		err := CreateOrUpdateResource(context, owner, namespace, &obj)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteResourceIfExists deletes an existing resource. It doesn't fail if the resource does not exist
// requires a context with log and client
func DeleteResourceIfExists(context context.Context, obj client.Object) error {
	log := log.FromContext(context)
	client := context.Value("client").(client.Client)
	err := client.Delete(context, obj)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "unable to delete object ", "object", obj)
		return err
	}
	return nil
}

// DeleteResourcesIfExist operates like DeleteResources, but on an arrays of resources
// requires a context with log and client
func DeleteResourcesIfExist(context context.Context, objs []client.Object) error {
	for _, obj := range objs {
		err := DeleteResourceIfExists(context, obj)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteUnstructuredResources operates like DeleteResources, but on an arrays of unstructured.Unstructured
// requires a context with log and client
func DeleteUnstructuredResources(context context.Context, objs []unstructured.Unstructured) error {
	for _, obj := range objs {
		err := DeleteResourceIfExists(context, &obj)
		if err != nil {
			return err
		}
	}
	return nil
}

// CreateResourceIfNotExists create a resource if it doesn't already exists. If the resource exists it is left untouched and the functin does not fails
// if owner is not nil, the owner field os set
// if namespace is not "", the namespace field of the object is overwritten with the passed value
// requires a context with log and client
func CreateResourceIfNotExists(context context.Context, owner client.Object, namespace string, obj client.Object) error {
	log := log.FromContext(context)
	client := context.Value("client").(client.Client)
	if owner != nil {
		_ = controllerutil.SetControllerReference(owner, obj, client.Scheme())
	}
	if namespace != "" {
		obj.SetNamespace(namespace)
	}

	err := client.Create(context, obj)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		log.Error(err, "unable to create object ", "object", obj)
		return err
	}
	return nil
}

// CreateResourcesIfNotExist operates as CreateResourceIfNotExists, but on an array of resources
// requires a context with log and client
func CreateResourcesIfNotExist(context context.Context, owner client.Object, namespace string, objs []client.Object) error {
	for _, obj := range objs {
		err := CreateResourceIfNotExists(context, owner, namespace, obj)
		if err != nil {
			return err
		}
	}
	return nil
}

// CreateUnstructuredResourcesIfNotExist operates as CreateResourceIfNotExists, but on an array of unstructured.Unstructured
// requires a context with log and client
func CreateUnstructuredResourcesIfNotExist(context context.Context, owner client.Object, namespace string, objs []unstructured.Unstructured) error {
	for _, obj := range objs {
		err := CreateResourceIfNotExists(context, owner, namespace, &obj)
		if err != nil {
			return err
		}
	}
	return nil
}

// CreateOrUpdateTemplatedResources processes an initialized template expecting an array of objects as a result and the processes them with the CreateOrUpdate function
// requires a context with log and client
func CreateOrUpdateTemplatedResources(context context.Context, owner client.Object, namespace string, data interface{}, template *template.Template) error {
	log := log.FromContext(context)
	objs, err := templates.ProcessTemplateArray(context, data, template)
	if err != nil {
		log.Error(err, "error creating manifest from template")
		return err
	}
	for _, obj := range objs {
		err = CreateOrUpdateResource(context, owner, namespace, &obj)
		if err != nil {
			return err
		}
	}
	return nil
}

// CreateIfNotExistTemplatedResources processes an initialized template expecting an array of objects as a result and then processes them with the CreateResourceIfNotExists function
// requires a context with log and client
func CreateIfNotExistTemplatedResources(context context.Context, owner client.Object, namespace string, data interface{}, template *template.Template) error {
	log := log.FromContext(context)
	objs, err := templates.ProcessTemplateArray(context, data, template)
	if err != nil {
		log.Error(err, "error creating manifest from template")
		return err
	}
	for _, obj := range objs {
		err = CreateResourceIfNotExists(context, owner, namespace, &obj)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteTemplatedResources processes an initialized template expecting an array of objects as a result and then processes them with the Delete function
// requires a context with log and client
func DeleteTemplatedResources(context context.Context, data interface{}, template *template.Template) error {
	log := log.FromContext(context)
	objs, err := templates.ProcessTemplateArray(context, data, template)
	if err != nil {
		log.Error(err, "error creating manifest from template")
		return err
	}
	for _, obj := range objs {
		err = DeleteResourceIfExists(context, &obj)
		if err != nil {
			return err
		}
	}
	return nil
}
