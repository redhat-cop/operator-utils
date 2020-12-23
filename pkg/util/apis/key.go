package apis

import (
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("util.api")

// GetKeyLong return a unique key for a given object in the pattern of <kind>/<apiversion>/<namespace>/<name>
// namespace can be null
func GetKeyLong(obj metav1.Object) string {
	robj, ok := obj.(runtime.Object)
	if !ok {
		err := errors.New("unable to conver meta.Object to runtime.Object")
		log.Error(err, "unable to conver meta.Object to runtime.Object", "object", obj)
		panic(err)
	}
	return robj.GetObjectKind().GroupVersionKind().GroupVersion().String() + "/" + robj.GetObjectKind().GroupVersionKind().Kind + "/" + obj.GetNamespace() + "/" + obj.GetName()
}

// GetKeyShort return a unique key for a given object in the pattern of <apiversion>/<namespace>/<name>
// namespace can be null
func GetKeyShort(obj metav1.Object) string {
	return obj.GetNamespace() + "/" + obj.GetName()
}
