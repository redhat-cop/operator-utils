package lockedresource

import (
	"encoding/json"
	"text/template"

	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

var log = logf.Log.WithName("lockedresource")

// LockedResource represents a resource to be locked down by a LockedResourceReconciler within a LockedResourceManager
type LockedResource struct {
	// unstructured.Unstructured is the resource to be locked
	unstructured.Unstructured
	// ExcludedPaths are the jsonPaths to be excluded when consider whether the resource has changed
	ExcludedPaths []string
}

// AsListOfUnstructured given a list of LockedResource, returns a list of unstructured.Unstructured
func AsListOfUnstructured(lockedResources []LockedResource) []unstructured.Unstructured {
	unstructuredList := []unstructured.Unstructured{}
	for _, lockedResource := range lockedResources {
		unstructuredList = append(unstructuredList, lockedResource.Unstructured)
	}
	return unstructuredList
}

// GetKey returns a unique key from a locked reosurce, the key is the concatenation of apiVersion/kind/namespace/name
func (lr *LockedResource) GetKey() string {
	return apis.GetKeyLong(&lr.Unstructured)
}

// GetLockedResources turns an array of Resources as read from an API into an array of LockedResources, usable by the LockedResourceManager
func GetLockedResources(resources []apis.Resource) ([]LockedResource, error) {
	lockedResources := []LockedResource{}
	for _, resource := range resources {
		bb, err := yaml.YAMLToJSON(resource.Object.Raw)
		if err != nil {
			log.Error(err, "Error transforming yaml to json", "raw", resource.Object.Raw)
			return []LockedResource{}, err
		}
		obj := &unstructured.Unstructured{}
		err = json.Unmarshal(bb, obj)
		if err != nil {
			log.Error(err, "Error unmarshalling json manifest", "manifest", string(bb))
			return []LockedResource{}, err
		}
		lockedResources = append(lockedResources, LockedResource{
			Unstructured:  *obj,
			ExcludedPaths: resource.ExcludedPaths,
		})
	}
	return lockedResources, nil
}

var templates = map[string]*template.Template{}

// GetLockedResourcesFromTemplate turns an array of ResourceTemplates as read from an API into an array of LockedResources using a params to process the templates
func GetLockedResourcesFromTemplate(resources []apis.ResourceTemplate, params interface{}) ([]LockedResource, error) {
	lockedResources := []LockedResource{}
	for _, resource := range resources {
		template, err := getTemplate(&resource)
		if err != nil {
			log.Error(err, "unable to retrieve template for", "resource", resource)
			return []LockedResource{}, nil
		}
		obj, err := util.ProcessTemplate(params, template)
		if err != nil {
			log.Error(err, "unable to process template for", "resource", resource, "params", params)
			return []LockedResource{}, nil
		}
		lockedResources = append(lockedResources, LockedResource{
			Unstructured:  *obj,
			ExcludedPaths: resource.ExcludedPaths,
		})
	}
	return lockedResources, nil
}

func getTemplate(resource *apis.ResourceTemplate) (*template.Template, error) {
	template, ok := templates[resource.ObjectTemplate]
	var err error
	if !ok {
		template, err = template.New(resource.ObjectTemplate).Parse(resource.ObjectTemplate)
		if err != nil {
			log.Error(err, "unable to parse", "template", resource.ObjectTemplate)
			return nil, nil
		}
		templates[resource.ObjectTemplate] = template
	}
	return template, nil
}
