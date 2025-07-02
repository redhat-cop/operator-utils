package openapi

import "k8s.io/kubectl/pkg/util/openapi"

type OpenAPIResourcesGetter struct {
	Resources openapi.Resources
}

func (o OpenAPIResourcesGetter) OpenAPISchema() (openapi.Resources, error) {
	return o.Resources, nil
}
