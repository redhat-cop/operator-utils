package util

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReconcileStatus represent the status of the last reconcile cycle. It's used to communicate success or failer and the error message
// +k8s:openapi-gen=true
type ReconcileStatus struct {

	// +kubebuilder:validation:Enum=Success,Failure
	Status     string      `json:"status,omitempty"`
	LastUpdate metav1.Time `json:"lastUpdate,omitempty"`
	Reason     string      `json:"reason,omitempty"`
}

// ReconcileStatusAware represnt a CRD type that has been enabled with ReconcileStatus, it can then benefit of a series of utility methods.
type ReconcileStatusAware interface {
	GetReconcileStatus() ReconcileStatus
	SetReconcileStatus(reconcileStatus ReconcileStatus)
}
