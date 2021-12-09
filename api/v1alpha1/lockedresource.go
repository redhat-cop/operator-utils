package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// LockedResource represents a resource to be enforced in a LockedResourceController and can be used in a API specification
// +k8s:openapi-gen=true
type LockedResource struct {

	// Object is a yaml representation of an API resource
	// +kubebuilder:validation:Required
	Object runtime.RawExtension `json:"object"`

	// ExludedPaths are a set of json paths that need not be considered by the LockedResourceReconciler
	// +kubebuilder:validation:Optional
	// +listType=set
	ExcludedPaths []string `json:"excludedPaths,omitempty"`
}

// LockedResourceTemplate represents a resource template in go language to be enforced in a LockedResourceController and can be used in a API specification
// +k8s:openapi-gen=true
type LockedResourceTemplate struct {

	// ObjectTemplate is a goland template. Whne processed, it must resolve to a yaml representation of an API resource
	// +kubebuilder:validation:Required
	ObjectTemplate string `json:"objectTemplate"`

	// ExludedPaths are a set of json paths that need not be considered by the LockedResourceReconciler
	// +kubebuilder:validation:Optional
	// +listType=set
	ExcludedPaths []string `json:"excludedPaths,omitempty"`
}
