package lockedresource

import (
	"encoding/json"
	"text/template"

	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
	"github.com/scylladb/go-set/strset"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

var log = ctrl.Log.WithName("lockedresource")

// LockedResource represents a resource to be locked down by a LockedResourceReconciler within a LockedResourceManager
type LockedResource struct {
	// unstructured.Unstructured is the resource to be locked
	unstructured.Unstructured `json:"usntructured,omitempty"`
	// ExcludedPaths are the jsonPaths to be excluded when consider whether the resource has changed
	ExcludedPaths []string `json:"excludedPaths,omitempty"`
}

// AsListOfUnstructured given a list of LockedResource, returns a list of unstructured.Unstructured
func AsListOfUnstructured(lockedResources []LockedResource) []unstructured.Unstructured {
	unstructuredList := []unstructured.Unstructured{}
	for _, lockedResource := range lockedResources {
		unstructuredList = append(unstructuredList, lockedResource.Unstructured)
	}
	return unstructuredList
}

// GetKey returns the marshalled resource
func (lr *LockedResource) GetKey() string {
	bb, err := lr.Unstructured.MarshalJSON()
	if err != nil {
		log.Error(err, "unable to marshall", "unstructured", lr.Unstructured)
		panic(err)
	}
	return string(bb)
}

// GetLockedResources turns an array of Resources as read from an API into an array of LockedResources, usable by the LockedResourceManager
func GetLockedResources(resources []apis.LockedResource) ([]LockedResource, error) {
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

// GetLockedResourcesFromTemplates Keep backwards compatability with existing consumers
func GetLockedResourcesFromTemplates(resources []apis.LockedResourceTemplate, params interface{}) ([]LockedResource, error) {

	return GetLockedResourcesFromTemplatesWithRestConfig(resources, nil, params)
}

// GetLockedResourcesFromTemplatesWithRestConfig turns an array of ResourceTemplates as read from an API into an array of LockedResources using a params to process the templates
func GetLockedResourcesFromTemplatesWithRestConfig(resources []apis.LockedResourceTemplate, config *rest.Config, params interface{}) ([]LockedResource, error) {
	lockedResources := []LockedResource{}
	for _, resource := range resources {
		template, err := getTemplate(&resource, config)
		if err != nil {
			log.Error(err, "unable to retrieve template for", "resource", resource)
			return []LockedResource{}, nil
		}
		objs, err := util.ProcessTemplateArray(params, template)
		if err != nil {
			log.Error(err, "unable to process template for", "resource", resource, "params", params)
			return []LockedResource{}, nil
		}
		for _, obj := range objs {
			lockedResources = append(lockedResources, LockedResource{
				Unstructured:  obj,
				ExcludedPaths: resource.ExcludedPaths,
			})
		}
	}
	return lockedResources, nil
}

func getTemplate(resource *apis.LockedResourceTemplate, config *rest.Config) (*template.Template, error) {
	tmpl, ok := templates[resource.ObjectTemplate]
	var err error
	if !ok {
		tmpl, err = template.New(resource.ObjectTemplate).Funcs(util.AdvancedTemplateFuncMap(config)).Parse(resource.ObjectTemplate)
		if err != nil {
			log.Error(err, "unable to parse", "template", resource.ObjectTemplate)
			return nil, err
		}
		log.V(1).Info("", "template", tmpl)
		templates[resource.ObjectTemplate] = tmpl
	}
	return tmpl, nil
}

//DefaultExcludedPaths represents paths that are exlcuded by default in all resources
var DefaultExcludedPaths = []string{".metadata", ".status", ".spec.replicas"}

//DefaultExcludedPathsSet represents paths that are exlcuded by default in all resources
var DefaultExcludedPathsSet = strset.New(DefaultExcludedPaths...)

//GetResources returs an arrays of apis.Resources from an arya of LockedResources, useful for mass operations on the LockedResources
func GetResources(lockedResources []LockedResource) []client.Object {
	resources := []client.Object{}
	for _, lockedResource := range lockedResources {
		resources = append(resources, &lockedResource.Unstructured)
	}
	return resources
}
