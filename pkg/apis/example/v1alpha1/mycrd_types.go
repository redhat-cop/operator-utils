package v1alpha1

import (
	"github.com/redhat-cop/operator-utils/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// MyCRDSpec defines the desired state of MyCRD
// +k8s:openapi-gen=true
type MyCRDSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html
	Initialized bool `json:"initialized,omitempty"`
	Valid       bool `json:"valid,omitempty"`
	Error       bool `json:"error,omitempty"`
}

// MyCRDStatus defines the observed state of MyCRD
// +k8s:openapi-gen=true
type MyCRDStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html

	util.ReconcileStatus `json:",inline"`
}

func (m MyCRD) GetReconcileStatus() util.ReconcileStatus {
	return m.Status.ReconcileStatus
}

func (m MyCRD) SetReconcileStatus(reconcileStatus util.ReconcileStatus) {
	m.Status.ReconcileStatus = reconcileStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MyCRD is the Schema for the mycrds API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type MyCRD struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MyCRDSpec   `json:"spec,omitempty"`
	Status MyCRDStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MyCRDList contains a list of MyCRD
type MyCRDList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MyCRD `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MyCRD{}, &MyCRDList{})
}
