package apis

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//Resource represents a kubernetes Resource
// type Resource interface {
// 	metav1.Object
// 	runtime.Object
// }

const ReconcileError = "ReconcileError"
const ReconcileErrorReason = "LastReconcileCycleFailed"
const ReconcileSuccess = "ReconcileSuccess"
const ReconcileSuccessReason = "LastReconcileCycleSucceded"

// ConditionsAware represents a CRD type that has been enabled with metav1.Conditions, it can then benefit of a series of utility methods.
type ConditionsAware interface {
	GetConditions() []metav1.Condition
	SetConditions(conditions []metav1.Condition)
}

//SetCondition adds or replaces the passed condition in the array of condition of the ConditionAware object
func SetCondition(c metav1.Condition, csa ConditionsAware) {
	conditions := csa.GetConditions()
	conditions = AddOrReplaceCondition(c, conditions)
	csa.SetConditions(conditions)
}

//AddOrReplaceCondition adds or replaces the passed condition in the passed array of conditions
func AddOrReplaceCondition(c metav1.Condition, conditions []metav1.Condition) []metav1.Condition {
	for i, condition := range conditions {
		if c.Type == condition.Type {
			conditions[i] = c
			return conditions
		}
	}
	conditions = append(conditions, c)
	return conditions
}
