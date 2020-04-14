package apis

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

//Resource represents a kubernetes Resource
type Resource interface {
	metav1.Object
	runtime.Object
}
