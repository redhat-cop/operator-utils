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

package util

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IsBeingDeleted returns whether this object has been requested to be deleted
func IsBeingDeleted(obj metav1.Object) bool {
	return !obj.GetDeletionTimestamp().IsZero()
}

// HasFinalizer returns whether this object has the passed finalizer
func HasFinalizer(obj metav1.Object, finalizer string) bool {
	for _, fin := range obj.GetFinalizers() {
		if fin == finalizer {
			return true
		}
	}
	return false
}

// AddFinalizer adds the passed finalizer this object
func AddFinalizer(obj metav1.Object, finalizer string) {
	if !HasFinalizer(obj, finalizer) {
		obj.SetFinalizers(append(obj.GetFinalizers(), finalizer))
	}
}

// RemoveFinalizer removes the passed finalizer from object
func RemoveFinalizer(obj metav1.Object, finalizer string) {
	for i, fin := range obj.GetFinalizers() {
		if fin == finalizer {
			finalizers := obj.GetFinalizers()
			finalizers[i] = finalizers[len(finalizers)-1]
			obj.SetFinalizers(finalizers[:len(finalizers)-1])
			return
		}
	}
}
