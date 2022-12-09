package discoveryclient

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// GetDiscoveryClient returns a discovery client for the current reconciler
// needs context with restConfig
func GetDiscoveryClient(context context.Context) (*discovery.DiscoveryClient, error) {
	restConfig := context.Value("restConfig").(*rest.Config)
	return discovery.NewDiscoveryClientForConfig(restConfig)
}

// IsAPIResourceAvailable checks of a give GroupVersionKind is available in the running apiserver
// needs context with restConfig and log
func IsGVKDefined(context context.Context, GVK schema.GroupVersionKind) (bool, error) {
	_, found, err := GetAPIResourceForGVK(context, GVK)
	return found, err
}

func GetAPIResourceForGVK(context context.Context, GVK schema.GroupVersionKind) (apiresource *v1.APIResource, found bool, err error) {
	log := log.FromContext(context)
	discoveryClient, err := GetDiscoveryClient(context)
	if err != nil {
		log.Error(err, "Unable to get discovery client")
		return nil, false, err
	}
	// Query for known OpenShift API resource to verify it is available
	apiResources, err := discoveryClient.ServerResourcesForGroupVersion(GVK.GroupVersion().String())
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		log.Error(err, "Unable to retrive resources for", "GVK", GVK)
		return nil, false, err
	}
	for i := range apiResources.APIResources {
		if apiResources.APIResources[i].Kind == GVK.Kind {
			return &apiResources.APIResources[i], true, nil
		}
	}
	return nil, false, nil
}

// IsGVKNamespaced checks whether the passed GVK os namespaced
// needs context with restConfig and log
func IsGVKNamespaced(context context.Context, GVK schema.GroupVersionKind) (bool, error) {
	resource, found, err := GetAPIResourceForGVK(context, GVK)
	if err != nil || !found {
		return found, err
	}
	return resource.Namespaced, nil
}

// IsUnstructuredDefined checks whether the content of a unstructured is defined in the current cluster
// needs context with restConfig and log
func IsUnstructuredDefined(context context.Context, obj *unstructured.Unstructured) (bool, error) {
	return IsGVKDefined(context, obj.GroupVersionKind())
}

// IsUnstructuredDefined checks whether the content of a unstructured is defined in the current cluster
// needs context with restConfig and log
func IsUnstructuredNamespaced(context context.Context, obj *unstructured.Unstructured) (bool, error) {
	return IsGVKNamespaced(context, obj.GroupVersionKind())
}
