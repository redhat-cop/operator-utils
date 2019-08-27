/*
Copyright 2019 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
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
	Initialized bool `json:"initialized"`
	Valid       bool `json:"valid"`
	Error       bool `json:"error"`
}

// MyCRDStatus defines the observed state of MyCRD
// +k8s:openapi-gen=true
type MyCRDStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html

	apis.ReconcileStatus `json:",inline"`
}

/* // ReconcileStatus represent the status of the last reconcile cycle. It's used to communicate success or failer and the error message
// +k8s:openapi-gen=true
type ReconcileStatus struct {

	// +kubebuilder:validation:Enum=Success,Failure
	Status     string      `json:"status,omitempty"`
	LastUpdate metav1.Time `json:"lastUpdate,omitempty"`
	Reason     string      `json:"reason,omitempty"`
} */

func (m *MyCRD) GetReconcileStatus() apis.ReconcileStatus {
	return m.Status.ReconcileStatus
}

func (m *MyCRD) SetReconcileStatus(reconcileStatus apis.ReconcileStatus) {
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
