package util

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IsBeingDeleted returns whether this object has been requested to be deleted
func IsBeingDeleted(obj *metav1.Object) bool {
	return !(*obj).GetDeletionTimestamp().IsZero()
}

// HasFinalizaer returns whether this object has the passed finalizer
func HasFinalizaer(obj *metav1.Object, finalizer string) bool {
	for _, fin := range (*obj).GetFinalizers() {
		if fin == finalizer {
			return true
		}
	}
	return false
}

// AddFinalizaer adds the passed finalizer this object
func AddFinalizaer(obj *metav1.Object, finalizer string) {
	if !HasFinalizaer(obj, finalizer) {
		(*obj).SetFinalizers(append((*obj).GetFinalizers(), finalizer))
	}
}

// RemoveFinalizer removes the passed finalizer from object
func RemoveFinalizer(obj *metav1.Object, finalizer string) {
	for i, fin := range (*obj).GetFinalizers() {
		if fin == finalizer {
			finalizers := (*obj).GetFinalizers()
			finalizers[i] = finalizers[len(finalizers)-1]
			(*obj).SetFinalizers(finalizers[:len(finalizers)-1])
			return
		}
	}
}
