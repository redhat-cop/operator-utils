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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// EnforcingPatchSpec defines the desired state of EnforcingPatch
type EnforcingPatchSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html

	// Patches is a list of pacthes that should be encforced at runtime.
	// +kubebuilder:validation:Optional
	Patches map[string]Patch `json:"patches,omitempty"`
}

// EnforcingPatchStatus defines the observed state of EnforcingPatch
type EnforcingPatchStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	EnforcingReconcileStatus `json:",inline,omitempty"`
}

func (m *EnforcingPatch) GetEnforcingReconcileStatus() EnforcingReconcileStatus {
	return m.Status.EnforcingReconcileStatus
}

func (m *EnforcingPatch) SetEnforcingReconcileStatus(reconcileStatus EnforcingReconcileStatus) {
	m.Status.EnforcingReconcileStatus = reconcileStatus
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// EnforcingPatch is the Schema for the enforcingpatches API
type EnforcingPatch struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EnforcingPatchSpec   `json:"spec,omitempty"`
	Status EnforcingPatchStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// EnforcingPatchList contains a list of EnforcingPatch
type EnforcingPatchList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EnforcingPatch `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EnforcingPatch{}, &EnforcingPatchList{})
}
