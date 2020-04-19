package v1alpha1

import (
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// EnforcingCRDSpec defines the desired state of EnforcingCRD
type EnforcingCRDSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	// Resources is a list of resource manifests that should be locked into the specified configuration
	// +kubebuilder:validation:Optional
	// +listType=atomic
	Resources []apis.LockedResource `json:"resources,omitempty"`
}

// EnforcingCRDStatus defines the observed state of EnforcingCRD
type EnforcingCRDStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	// +kubebuilder:validation:Optional
	apis.EnforcingReconcileStatus `json:",inline,omitempty"`
}

func (m *EnforcingCRD) GetEnforcingReconcileStatus() apis.EnforcingReconcileStatus {
	return m.Status.EnforcingReconcileStatus
}

func (m *EnforcingCRD) SetEnforcingReconcileStatus(reconcileStatus apis.EnforcingReconcileStatus) {
	m.Status.EnforcingReconcileStatus = reconcileStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// EnforcingCRD is the Schema for the enforcingcrds API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=enforcingcrds,scope=Namespaced
type EnforcingCRD struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EnforcingCRDSpec   `json:"spec,omitempty"`
	Status EnforcingCRDStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// EnforcingCRDList contains a list of EnforcingCRD
type EnforcingCRDList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EnforcingCRD `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EnforcingCRD{}, &EnforcingCRDList{})
}
