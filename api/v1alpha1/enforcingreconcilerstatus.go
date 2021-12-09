package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +patchMergeKey=type
// +patchStrategy=merge
// +listType=map
// +listMapKey=type
type Conditions []metav1.Condition

// EnforcingReconcileStatus represents the status of the last reconcile cycle. It's used to communicate success or failure and the error message
type EnforcingReconcileStatus struct {

	// ReconcileStatus this is the general status of the main reconciler
	// +kubebuilder:validation:Optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	//LockedResourceStatuses contains the reconcile status for each of the managed resources
	// +kubebuilder:validation:Optional
	LockedResourceStatuses map[string]Conditions `json:"lockedResourceStatuses,omitempty"`

	//LockedResourceStatuses contains the reconcile status for each of the managed resources
	// +kubebuilder:validation:Optional
	LockedPatchStatuses map[string]map[string]Conditions `json:"lockedPatchStatuses,omitempty"`
}

// EnforcingReconcileStatusAware is an interfce that must be implemented by a CRD type that has been enabled with ReconcileStatus, it can then benefit of a series of utility methods.
// +kubebuilder:object:generate:=false
type EnforcingReconcileStatusAware interface {
	GetEnforcingReconcileStatus() EnforcingReconcileStatus
	SetEnforcingReconcileStatus(enforcingReconcileStatus EnforcingReconcileStatus)
}
