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
	"bytes"
	"encoding/json"

	"text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/kubernetes/pkg/kubectl/cmd/util/openapi/validation"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/yaml"
)

var log = logf.Log.WithName("util")

// ProcessTemplate processes an initialized Go template with a set of data. It expects one API object to be defined in the template
func ProcessTemplate(data interface{}, template *template.Template) (*unstructured.Unstructured, error) {
	obj := unstructured.Unstructured{}
	var b bytes.Buffer
	err := template.Execute(&b, data)
	if err != nil {
		log.Error(err, "Error executing template", "template", template)
		return &obj, err
	}
	bb, err := yaml.YAMLToJSON(b.Bytes())
	if err != nil {
		log.Error(err, "Error trasnfoming yaml to json", "manifest", string(b.Bytes()))
		return &obj, err
	}

	err = json.Unmarshal(bb, &obj)
	if err != nil {
		log.Error(err, "Error unmarshalling json manifest", "manifest", string(bb))
		return &obj, err
	}

	return &obj, err
}

// ProcessTemplateArray processes an initialized Go template with a set of data. It expects an arrays of API objects to be defined in the template. Dishomogeneus types are supported
func ProcessTemplateArray(data interface{}, template *template.Template) ([]unstructured.Unstructured, error) {
	obj := []unstructured.Unstructured{}
	var b bytes.Buffer
	err := template.Execute(&b, data)
	if err != nil {
		log.Error(err, "Error executing template", "template", template)
		return []unstructured.Unstructured{}, err
	}
	bb, err := yaml.YAMLToJSON(b.Bytes())
	if err != nil {
		log.Error(err, "Error trasnfoming yaml to json", "manifest", string(b.Bytes()))
		return []unstructured.Unstructured{}, err
	}

	err = json.Unmarshal(bb, &obj)
	if err != nil {
		log.Error(err, "Error unmarshalling json manifest", "manifest", string(bb))
		return []unstructured.Unstructured{}, err
	}

	return obj, err
}

// ValidateUnstructured validates the content of an unstructred against an openapi schema.
// the schema is intented to be retrieved from a running instance of kubernetes, but other usages are possible.
func ValidateUnstructured(obj *unstructured.Unstructured, validationSchema *validation.SchemaValidation) error {
	bb, err := obj.MarshalJSON()
	if err != nil {
		log.Error(err, "unable to unmarshall", "unstrcutured", obj)
		return err
	}
	err = validationSchema.ValidateBytes(bb)
	if err != nil {
		log.Error(err, "unable to valudate", "json doc", string(bb), "against schemas", validationSchema)
		return err
	}
	return nil
}

//IsUnstructuredDefined checks whether the content of a unstructured is defined against the passed DiscoveryClient
func IsUnstructuredDefined(obj *unstructured.Unstructured, discoveryClient *discovery.DiscoveryClient) (bool, error) {
	gvk := obj.GroupVersionKind()
	return IsGVKDefined(gvk, discoveryClient)
}

func IsGVKDefined(gvk schema.GroupVersionKind, discoveryClient *discovery.DiscoveryClient) (bool, error) {
	resources, err := discoveryClient.ServerResourcesForGroupVersion(gvk.GroupVersion().String())
	if err != nil {
		log.Error(err, "unable to find resources for", "gvk", gvk)
		return false, err
	}
	for _, resource := range resources.APIResources {
		if resource.Kind == gvk.Kind {
			return true, nil
		}
	}
	return false, nil
}
