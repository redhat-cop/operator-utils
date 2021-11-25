package apis

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Patch describes a patch to be enforced at runtime
// +k8s:openapi-gen=true
type Patch struct {
	//ID represents a unique Identifier for this patch
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// SourceObject refs is an arrays of refereces to source objects that will be used as input for the template processing. These refernce must resolve to single instance. The resolution rule is as follows (+ present, - absent):
	// the King and APIVersion field are mandatory
	// +Namespace +Name: resolves to object <Namespace>/<Name>
	// +Namespace -Name: results in an error
	// -Namespace +Name: resolves to cluster-level object <Name>. If Kind is namespaced, this results in an error.
	// -Namespace -Name: results in an error
	// If Name or Namespace start with "$", the rest of the string is considered a jsonpath to be applied to the source object and used to calculate the value.
	// ResourceVersion and UID are always ignored
	// If FieldPath is specified, the restuned object is calculated from the path, so for example if FieldPath=.spec, the only the spec portion of the object is returned.
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
	TargetObjectRef TargetObjectReference `json:"targetObjectRef"`

	// PatchType is the type of patch to be applied, one of "application/json-patch+json"'"application/merge-patch+json","application/strategic-merge-patch+json","application/apply-patch+yaml"
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum="application/json-patch+json";"application/merge-patch+json";"application/strategic-merge-patch+json";"application/apply-patch+yaml"
	// default:="application/strategic-merge-patch+json"
	PatchType types.PatchType `json:"patchType,omitempty"`

	// PatchTemplate is a go template that will be resolved using the SourceObjectRefs as parameters. The result must be a valid patch based on the pacth type and the target object.
	// +kubebuilder:validation:Required
	PatchTemplate string `json:"patchTemplate"`
}

type TargetObjectReference struct {
	// Kind of the referent.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +required
	Kind string `json:"kind,omitempty" protobuf:"bytes,1,opt,name=kind"`
	// Namespace of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/
	// +optional
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,2,opt,name=namespace"`
	// Name of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,3,opt,name=name"`
	// API version of the referent.
	// +required
	APIVersion string `json:"apiVersion,omitempty" protobuf:"bytes,5,opt,name=apiVersion"`
	// LabelSelector selects objects by label.
	// +kubebuilder:validation:Optional
	LabelSelector metav1.LabelSelector `json:"labelSelector,omitempty"`
}

type SourceObjectReference struct {
	// Kind of the referent.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +required
	Kind string `json:"kind,omitempty" protobuf:"bytes,1,opt,name=kind"`
	// Namespace of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/
	// +optional
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,2,opt,name=namespace"`
	// Name of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,3,opt,name=name"`
	// API version of the referent.
	// +required
	APIVersion string `json:"apiVersion,omitempty" protobuf:"bytes,5,opt,name=apiVersion"`

	// If referring to a piece of an object instead of an entire object, this string
	// should contain a valid JSON/Go field access statement, such as desiredState.manifest.containers[2].
	// For example, if the object reference is to a container within a pod, this would take on a value like:
	// "spec.containers{name}" (where "name" refers to the name of the container that triggered
	// the event) or if no container name is specified "spec.containers[2]" (container with
	// index 2 in this pod). This syntax is chosen only to have some well-defined way of
	// referencing a part of an object.
	// TODO: this design is not final and this field is subject to change in the future.
	// +optional
	FieldPath string `json:"fieldPath,omitempty" protobuf:"bytes,7,opt,name=fieldPath"`
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Patch) DeepCopyInto(out *Patch) {
	*out = *in
	if in.SourceObjectRefs != nil {
		in, out := &in.SourceObjectRefs, &out.SourceObjectRefs
		*out = make([]corev1.ObjectReference, len(*in))
		copy(*out, *in)
	}
	out.TargetObjectRef = in.TargetObjectRef
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Patch.
func (in *Patch) DeepCopy() *Patch {
	if in == nil {
		return nil
	}
	out := new(Patch)
	in.DeepCopyInto(out)
	return out
}
