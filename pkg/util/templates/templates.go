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

package templates

import (
	"bytes"
	"context"
	"encoding/json"
	"text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/kubectl/pkg/util/openapi/validation"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

// ProcessTemplate processes an initialized Go template with a set of data. It expects one API object to be defined in the template
// requires a context with log
func ProcessTemplate(context context.Context, data interface{}, template *template.Template) (*unstructured.Unstructured, error) {
	log := log.FromContext(context)
	obj := unstructured.Unstructured{}
	var b bytes.Buffer
	err := template.Execute(&b, data)
	if err != nil {
		log.Error(err, "Error executing template", "template", template)
		return &obj, err
	}

	bb, err := yaml.YAMLToJSON(b.Bytes())
	if err != nil {
		log.Error(err, "Error transforming yaml to json", "manifest", b.String())
		return &obj, err
	}

	err = obj.UnmarshalJSON(bb)
	if err != nil {
		log.Error(err, "Error unmarshalling json manifest", "manifest", string(bb))
		return &obj, err
	}
	return &obj, err
}

// ProcessTemplateArray processes an initialized Go template with a set of data. It expects an arrays of API objects to be defined in the template. Dishomogeneus types are supported
// requires a context with log
func ProcessTemplateArray(context context.Context, data interface{}, template *template.Template) ([]unstructured.Unstructured, error) {
	log := log.FromContext(context)
	objs := []unstructured.Unstructured{}
	var b bytes.Buffer
	err := template.Execute(&b, data)
	if err != nil {
		log.Error(err, "Error executing template", "template", template)
		return []unstructured.Unstructured{}, err
	}
	bb, err := yaml.YAMLToJSON(b.Bytes())
	if err != nil {
		log.Error(err, "Error transforming yaml to json", "manifest", b.String())
		return []unstructured.Unstructured{}, err
	}
	if !IsJSONArray(bb) {
		obj := unstructured.Unstructured{}
		err = obj.UnmarshalJSON(bb)
		objs = append(objs, obj)
	} else {
		intfs := &[]interface{}{}
		err = json.Unmarshal(bb, &intfs)
		if err != nil {
			log.Error(err, "Error unmarshalling json manifest", "manifest", string(bb))
			return []unstructured.Unstructured{}, err
		}
		for _, intf := range *intfs {
			b, err := json.Marshal(intf)
			if err != nil {
				log.Error(err, "Error marshalling", "interface", intf)
				return []unstructured.Unstructured{}, err
			}
			obj := unstructured.Unstructured{}
			err = obj.UnmarshalJSON(b)
			if err != nil {
				log.Error(err, "Error unmarshalling", "json", string(b))
				return []unstructured.Unstructured{}, err
			}
			objs = append(objs, obj)
		}
	}

	if err != nil {
		log.Error(err, "Error unmarshalling json manifest", "manifest", string(bb))
		return []unstructured.Unstructured{}, err
	}
	return objs, err
}

// ValidateUnstructured validates the content of an unstructured against an openapi schema.
// the schema is intended to be retrieved from a running instance of kubernetes, but other usages are possible.
// requires a context with log
func ValidateUnstructured(context context.Context, obj *unstructured.Unstructured, validationSchema *validation.SchemaValidation) error {
	log := log.FromContext(context)
	bb, err := obj.MarshalJSON()
	if err != nil {
		log.Error(err, "unable to unmarshall", "unstructured", obj)
		return err
	}
	err = validationSchema.ValidateBytes(bb)
	if err != nil {
		log.Error(err, "unable to validate", "json doc", string(bb), "against schemas", validationSchema)
		return err
	}
	return nil
}

//IsJSONArray checks to see if a byte array containing JSON is an array of data
func IsJSONArray(data []byte) bool {
	firstLine := bytes.TrimLeft(data, " \t\r\n")
	return firstLine[0] == '['
}
