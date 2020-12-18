/*


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

// TemplatedEnforcingCRDSpec defines the desired state of TemplatedEnforcingCRD
type TemplatedEnforcingCRDSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	// +kubebuilder:validation:Optional
	// +listType=atomic
	Templates []apis.LockedResourceTemplate `json:"templates,omitempty"`
}

// TemplatedEnforcingCRDStatus defines the observed state of TemplatedEnforcingCRD
type TemplatedEnforcingCRDStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	// +kubebuilder:validation:Optional
	apis.EnforcingReconcileStatus `json:",inline,omitempty"`
}

func (m *TemplatedEnforcingCRD) GetEnforcingReconcileStatus() apis.EnforcingReconcileStatus {
	return m.Status.EnforcingReconcileStatus
}

func (m *TemplatedEnforcingCRD) SetEnforcingReconcileStatus(reconcileStatus apis.EnforcingReconcileStatus) {
	m.Status.EnforcingReconcileStatus = reconcileStatus
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// TemplatedEnforcingCRD is the Schema for the templatedenforcingcrds API
type TemplatedEnforcingCRD struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TemplatedEnforcingCRDSpec   `json:"spec,omitempty"`
	Status TemplatedEnforcingCRDStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TemplatedEnforcingCRDList contains a list of TemplatedEnforcingCRD
type TemplatedEnforcingCRDList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TemplatedEnforcingCRD `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TemplatedEnforcingCRD{}, &TemplatedEnforcingCRDList{})
}
