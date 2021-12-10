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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// IsBeingDeleted returns whether this object has been requested to be deleted
func IsBeingDeleted(obj client.Object) bool {
	return !obj.GetDeletionTimestamp().IsZero()
}

// HasFinalizer returns whether this object has the passed finalizer
// Deprecated use controllerutil.ContainsFinalizer
func HasFinalizer(obj client.Object, finalizer string) bool {
	return controllerutil.ContainsFinalizer(obj, finalizer)
}

// AddFinalizer adds the passed finalizer this object
// Deprecated use controllerutil.AddFinalizer
func AddFinalizer(obj client.Object, finalizer string) {
	controllerutil.AddFinalizer(obj, finalizer)
}

// RemoveFinalizer removes the passed finalizer from object
// Deprecated use controllerutil.RemoveFinalizer
func RemoveFinalizer(obj client.Object, finalizer string) {
	controllerutil.RemoveFinalizer(obj, finalizer)
}
