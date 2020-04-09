package apis

// EnforcingReconcileStatus represent the status of the last reconcile cycle. It's used to communicate success or failer and the error message
// +k8s:openapi-gen=true
type EnforcingReconcileStatus struct {

	// ReconcileStatus this is the general status of the main reconciler
	ReconcileStatus `json:"inline"`

	//LockedResourceStatuses containes the reconcile status for each of the managed resoureces
	LockedResourceStatuses map[string]ReconcileStatus `json:"lockedResourceStatuses,omitempty"`
}

// EnforcingReconcileStatusAware represnt a CRD type that has been enabled with ReconcileStatus, it can then benefit of a series of utility methods.
type EnforcingReconcileStatusAware interface {
	GetEnforcingReconcileStatus() EnforcingReconcileStatus
	SetEnforcingReconcileStatus(enforcingReconcileStatus EnforcingReconcileStatus)
}
