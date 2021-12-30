package v1alpha1

import (
	"bytes"
	"context"
	"errors"
	"text/template"

	"github.com/redhat-cop/operator-utils/pkg/util/discoveryclient"
	"github.com/redhat-cop/operator-utils/pkg/util/dynamicclient"
	utiltemplates "github.com/redhat-cop/operator-utils/pkg/util/templates"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Patch describes a patch to be enforced at runtime
// +k8s:openapi-gen=true
type PatchSpec struct {
	//Name represents a unique name for this patch, it has no particular effect, except for internal bookeeping

	// SourceObjectRefs is an arrays of refereces to source objects that will be used as input for the template processing. These refernces must resolve to single instance. The resolution rule is as follows (+ present, - absent):
	// the King and APIVersion field are mandatory
	// +Namespace +Name: resolves to object <Namespace>/<Name>
	// +Namespace -Name: results in an error
	// -Namespace +Name: resolves to cluster-level object <Name>. If Kind is namespaced, this results in an error.
	// -Namespace -Name: results in an error
	// Name manespaces Namespace are evaluated as golang templates with the input of the template being the target object. When selecting multiple target, this allows for having specific source objects for each target.
	// ResourceVersion and UID are always ignored
	// If FieldPath is specified, the restuned object is calculated from the path, so for example if FieldPath=.spec, the only the spec portion of the object is returned.
	// The target object is always added as element zero of the array of the SourceObjectRefs
	// +kubebuilder:validation:Optional
	// +listType=atomic
	SourceObjectRefs []SourceObjectReference `json:"sourceObjectRefs,omitempty"`

	// TargetObjectRef is a reference to the object to which the pacth should be applied.
	// the King and APIVersion field are mandatory
	// the Name and Namespace field have the following meaning (+ present, - absent)
	// +Namespace +Name: apply the patch to the object: <Namespace>/<Name>
	// +Namespace -Name: apply the patch to all of the objects in <Namespace>
	// -Namespace +Name: apply the patch to the cluster-level object <Name>. If Kind is namespaced, this results in an error.
	// -Namespace -Name: if the kind is namespaced apply the patch to all of the objects in all of the namespaces. If the kind is not namespaced, apply the patch to all of the cluster level objects.
	// The lable selector can be used to further filter the selected objects.
	// +kubebuilder:validation:Required
	TargetObjectRef TargetObjectReference `json:"targetObjectRef,omitempty"`

	// PatchType is the type of patch to be applied, one of "application/json-patch+json"'"application/merge-patch+json","application/strategic-merge-patch+json","application/apply-patch+yaml"
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum="application/json-patch+json";"application/merge-patch+json";"application/strategic-merge-patch+json";"application/apply-patch+yaml"
	// default:="application/strategic-merge-patch+json"
	PatchType types.PatchType `json:"patchType,omitempty"`

	// PatchTemplate is a go template that will be resolved using the SourceObjectRefs as parameters. The result must be a valid patch based on the pacth type and the target object.
	// +kubebuilder:validation:Required
	PatchTemplate string `json:"patchTemplate,omitempty"`
}

type TargetObjectReference struct {
	// API version of the referent.
	// +kubebuilder:validation:Required
	APIVersion string `json:"apiVersion,omitempty" protobuf:"bytes,5,opt,name=apiVersion"`

	// Kind of the referent.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +kubebuilder:validation:Required
	Kind string `json:"kind,omitempty" protobuf:"bytes,1,opt,name=kind"`

	// Namespace of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,2,opt,name=namespace"`

	// Name of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	// +kubebuilder:validation:Optional
	Name string `json:"name,omitempty" protobuf:"bytes,3,opt,name=name"`

	// LabelSelector selects objects by label
	// +kubebuilder:validation:Optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// AnnotationSelector selects objects by label
	AnnotationSelector *metav1.LabelSelector `json:"annotationSelector,omitempty"`

	//apiResource caches apiResource for this targetReference
	apiResource *metav1.APIResource `json:"-"`
}

func (t *TargetObjectReference) getAPIReourceForGVK(context context.Context) (*metav1.APIResource, bool, error) {
	if t.apiResource != nil {
		return t.apiResource, true, nil
	}
	apiresource, found, err := discoveryclient.GetAPIResourceForGVK(context, schema.FromAPIVersionAndKind(t.APIVersion, t.Kind))
	if err != nil && found {
		t.apiResource = apiresource
	}
	return apiresource, found, err
}

func (t *TargetObjectReference) getDynamicClient(context context.Context) (dynamic.ResourceInterface, error) {
	log := log.FromContext(context)
	_, namespacedSelection, err := t.IsSelectingMultipleInstances(context)
	if err != nil {
		log.Error(err, "unable to determine if the target reference is selecting multiple instance", "targetReference", t)
		return nil, err
	}
	var ri dynamic.ResourceInterface
	nri, namespaced, err := dynamicclient.GetDynamicClientForGVK(context, schema.FromAPIVersionAndKind(t.APIVersion, t.Kind))
	if err != nil {
		log.Error(err, "unable to get dynamicClient on ", "gvk", schema.FromAPIVersionAndKind(t.APIVersion, t.Kind))
		return nil, err
	}
	if namespaced && namespacedSelection {
		ri = nri.Namespace(t.Namespace)
	} else {
		ri = nri
	}
	return ri, nil
}

func (t *TargetObjectReference) GetReferencedObjectWithName(context context.Context, namespacedName types.NamespacedName) (*unstructured.Unstructured, error) {
	log := log.FromContext(context)
	multiple, _, err := t.IsSelectingMultipleInstances(context)
	if err != nil {
		log.Error(err, "unable to determine if the target reference is selecting multiple instance", "targetReference", t)
		return nil, err
	}
	if !multiple {
		return t.GetReferencedObject(context)
	}
	targetCopy := t.DeepCopy()
	targetCopy.Name = namespacedName.Name
	namespaced, err := t.IsNamespaced(context)
	if err != nil {
		log.Error(err, "unable to determine if the target reference is namespaced", "targetReference", t)
		return nil, err
	}
	if namespaced {
		targetCopy.Namespace = namespacedName.Namespace
	}
	client, err := targetCopy.getDynamicClient(context)
	if err != nil {
		log.Error(err, "unable to get dynamic client with", "targetReference", targetCopy)
		return nil, err
	}
	obj, err := client.Get(context, targetCopy.Name, metav1.GetOptions{})
	if err != nil {
		log.Error(err, "unable to get referenced object", "targetReference", targetCopy)
		return nil, err
	}
	return obj, nil
}

func (t *TargetObjectReference) GetReferencedObject(context context.Context) (*unstructured.Unstructured, error) {
	log := log.FromContext(context)
	multiple, _, err := t.IsSelectingMultipleInstances(context)
	if err != nil {
		log.Error(err, "unable to determine if the target reference is selecting multiple instance", "targetReference", t)
		return nil, err
	}
	if multiple {
		return nil, errors.New("cannot call this method on a target that selects multiple instances")
	}
	dclient, err := t.getDynamicClient(context)
	if err != nil {
		log.Error(err, "unable to get dynamic client on", "targetReference", t)
		return nil, err
	}
	obj, err := dclient.Get(context, t.Name, metav1.GetOptions{})
	if err != nil {
		log.Error(err, "unable to get referenced ", "object", t)
		return nil, err
	}
	return obj, nil
}

func (t *TargetObjectReference) GetReferencedObjects(context context.Context) ([]unstructured.Unstructured, error) {
	log := log.FromContext(context)
	multiple, _, err := t.IsSelectingMultipleInstances(context)
	if err != nil {
		log.Error(err, "unable to determine if the target reference is selecting multiple instance", "targetReference", t)
		return nil, err
	}
	if !multiple {
		return nil, errors.New("cannot call this method on a target that does not select multiple instances")
	}
	dclient, err := t.getDynamicClient(context)
	if err != nil {
		log.Error(err, "unable to get dynamic client on", "targetReference", t)
		return nil, err
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(t.LabelSelector)
	if err != nil {
		log.Error(err, "unable to process ", "labelSelector", t.LabelSelector)
		return nil, err
	}
	objList, err := dclient.List(context, metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
	if err != nil {
		log.Error(err, "unable to list referenced ", "objects", t)
		return nil, err
	}
	var annotatonSelector labels.Selector
	if t.AnnotationSelector != nil {
		annotatonSelector, err = metav1.LabelSelectorAsSelector(t.AnnotationSelector)
		if err != nil {
			return nil, err
		}
	} else {
		annotatonSelector = labels.Everything()
	}
	//filter by annotation
	annotationFilteredList := []unstructured.Unstructured{}
	for i := range objList.Items {
		if annotatonSelector.Matches(labels.Set(objList.Items[i].GetAnnotations())) {
			annotationFilteredList = append(annotationFilteredList, objList.Items[i])
		}
	}
	//filter by name
	if t.Name != "" {
		filteredList := []unstructured.Unstructured{}
		for i := range annotationFilteredList {
			if t.Name == annotationFilteredList[i].GetName() {
				filteredList = append(filteredList, annotationFilteredList[i])
			}
		}
		return filteredList, nil
	}
	return objList.Items, nil
}

func (t *TargetObjectReference) IsNamespaced(context context.Context) (bool, error) {
	apiresource, found, err := t.getAPIReourceForGVK(context)
	if err != nil {
		return false, err
	}
	if !found {
		return false, errors.New("resource not found" + schema.FromAPIVersionAndKind(t.APIVersion, t.Kind).String())
	}
	return apiresource.Namespaced, nil
}

//IsSelectingMultipleInstances is a helper function to determine whether this targetObjectReference selects one or multiple instance.
func (t *TargetObjectReference) IsSelectingMultipleInstances(context context.Context) (multiple bool, namespacedSelection bool, err error) {
	log := log.FromContext(context)
	namespaced, err := t.IsNamespaced(context)
	if err != nil {
		log.Error(err, "Unable to determine if targetObjectReference is namespaced", "TargetObjectReference", t)
		return false, false, err
	}
	if namespaced {
		if t.Namespace == "" {
			return true, false, nil
		} else {
			if t.Name == "" {
				return true, true, nil
			} else {
				return false, true, nil
			}
		}
	} else {
		return t.Name == "", false, nil
	}
}

//Selects returns whether the passed object is selected by the current target reference
// requires context with log and restConfig
func (t *TargetObjectReference) Selects(context context.Context, obj client.Object) (bool, error) {
	log := log.FromContext(context)
	if apiversion, kind := obj.GetObjectKind().GroupVersionKind().ToAPIVersionAndKind(); t.Kind != kind || t.APIVersion != apiversion {
		return false, nil
	}
	var labelSelector labels.Selector
	var annotatonSelector labels.Selector
	var err error
	if t.LabelSelector != nil {
		labelSelector, err = metav1.LabelSelectorAsSelector(t.LabelSelector)
		if err != nil {
			return false, err
		}
	} else {
		labelSelector = labels.Everything()
	}
	if t.AnnotationSelector != nil {
		annotatonSelector, err = metav1.LabelSelectorAsSelector(t.AnnotationSelector)
		if err != nil {
			return false, err
		}
	} else {
		annotatonSelector = labels.Everything()
	}
	namespaced, err := discoveryclient.IsGVKNamespaced(context, obj.GetObjectKind().GroupVersionKind())
	if err != nil {
		log.Error(err, "Unable to determine if GVK is namespaced", "GVK", obj.GetObjectKind().GroupVersionKind())
		return false, err
	}
	if namespaced {
		if t.Namespace != "" {
			//we are selecting within a namespace
			if t.Namespace != obj.GetNamespace() {
				return false, nil
			}
		}
		if t.Name != "" {
			// we are matching on name
			return t.Name == obj.GetName(), nil
		} else {
			// we select via selectors
			return labelSelector.Matches(labels.Set(obj.GetLabels())) && annotatonSelector.Matches(labels.Set(obj.GetAnnotations())), nil
		}
	} else {
		//cluster object, we ignore namespace
		if t.Name != "" {
			// we select via name
			return t.Name == obj.GetName(), nil

		} else {
			// we select via selectors
			return labelSelector.Matches(labels.Set(obj.GetLabels())) && annotatonSelector.Matches(labels.Set(obj.GetAnnotations())), nil
		}
	}
}

//GetNameAndNamespace processes the templates for Name and Namespace of the sourceObjectReference
// requires context with log and restConfig
func (s *SourceObjectReference) GetNameAndNamespace(context context.Context, target *unstructured.Unstructured) (name string, namespace string, err error) {
	log := log.FromContext(context)
	name, err = processTemplate(context, s.Name, target.UnstructuredContent())
	if err != nil {
		log.Error(err, "unable to process template for", "name", s.Name)
		return "", "", err
	}
	namespace, err = processTemplate(context, s.Namespace, target.UnstructuredContent())
	if err != nil {
		log.Error(err, "unable to process template for", "namespace", s.Name)
		return "", "", err
	}
	return
}

func (t *SourceObjectReference) getAPIReourceForGVK(context context.Context) (*metav1.APIResource, bool, error) {
	if t.apiResource != nil {
		return t.apiResource, true, nil
	}
	apiresource, found, err := discoveryclient.GetAPIResourceForGVK(context, schema.FromAPIVersionAndKind(t.APIVersion, t.Kind))
	if err != nil && found {
		t.apiResource = apiresource
	}
	return apiresource, found, err
}

func (t *SourceObjectReference) getDynamicClient(context context.Context) (dynamic.ResourceInterface, error) {
	log := log.FromContext(context)

	var ri dynamic.ResourceInterface
	nri, namespaced, err := dynamicclient.GetDynamicClientForGVK(context, schema.FromAPIVersionAndKind(t.APIVersion, t.Kind))
	if err != nil {
		log.Error(err, "unable to get dynamicClient on ", "gvk", schema.FromAPIVersionAndKind(t.APIVersion, t.Kind))
		return nil, err
	}
	if namespaced {
		ri = nri.Namespace(t.Namespace)
	} else {
		ri = nri
	}
	return ri, nil
}

func (s *SourceObjectReference) GetReferencedObject(context context.Context, target *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	log := log.FromContext(context)
	name, namespace, err := s.GetNameAndNamespace(context, target)
	if err != nil {
		log.Error(err, "unable to get name and namespaces on ", "SourceObjectReference", s, "with target", target)
		return nil, err
	}
	sourceCopy := s.DeepCopy()
	sourceCopy.Name = name
	sourceCopy.Namespace = namespace
	client, err := sourceCopy.getDynamicClient(context)
	if err != nil {
		log.Error(err, "unable to get dynamic client for ", "source", sourceCopy)
		return nil, err
	}
	obj, err := client.Get(context, name, metav1.GetOptions{})
	if err != nil {
		log.Error(err, "unable to get referenced object ", "sourceCopy", sourceCopy)
		return nil, err
	}
	return obj, nil
}

func processTemplate(context context.Context, templateString string, param interface{}) (string, error) {
	log := log.FromContext(context)
	restConfig := context.Value("restConfig").(*rest.Config)
	template, err := template.New(templateString).Funcs(utiltemplates.AdvancedTemplateFuncMap(restConfig, log)).Parse(templateString)
	if err != nil {
		log.Error(err, "unable to parse", "template", templateString)
		return "", err
	}
	var b bytes.Buffer
	err = template.Execute(&b, param)
	if err != nil {
		log.Error(err, "unable to process", "template", templateString, "with param", param)
		return "", err
	}
	return b.String(), nil
}

type SourceObjectReference struct {
	// API version of the referent.
	// +kubebuilder:validation:Required
	APIVersion string `json:"apiVersion,omitempty" protobuf:"bytes,5,opt,name=apiVersion"`

	// Kind of the referent.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +kubebuilder:validation:Required
	Kind string `json:"kind,omitempty" protobuf:"bytes,1,opt,name=kind"`

	// Namespace of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,2,opt,name=namespace"`

	// Name of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	// +kubebuilder:validation:Optional
	Name string `json:"name,omitempty" protobuf:"bytes,3,opt,name=name"`

	// If referring to a piece of an object instead of an entire object, this string
	// should contain a valid JSON/Go field access statement, such as desiredState.manifest.containers[2].
	// For example, if the object reference is to a container within a pod, this would take on a value like:
	// "spec.containers{name}" (where "name" refers to the name of the container that triggered
	// the event) or if no container name is specified "spec.containers[2]" (container with
	// index 2 in this pod). This syntax is chosen only to have some well-defined way of
	// referencing a part of an object.
	// +kubebuilder:validation:Optional
	FieldPath string `json:"fieldPath,omitempty" protobuf:"bytes,7,opt,name=fieldPath"`

	//apiResource caches apiResource for this targetReference
	apiResource *metav1.APIResource `json:"-"`
}
