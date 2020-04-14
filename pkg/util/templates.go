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
