package lockedresource

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"text/template"

	"github.com/go-logr/logr"
	utilsapi "github.com/redhat-cop/operator-utils/api/v1alpha1"
	utilstemplates "github.com/redhat-cop/operator-utils/pkg/util/templates"
	"github.com/scylladb/go-set/strset"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

var innerlog = ctrl.Log.WithName("lockedresource")

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

// GetKey identifies the resource for set comparisons: the marshalled object AND its excluded paths.
// The enforcer restarts a reconciler only for a resource whose key changed; with the object alone
// as the key, editing a template's excludedPaths on the CR changed nothing until the operator
// restarted (measured: a reconciler kept excluding `.metadata` after the CR stopped listing it).
func (lr *LockedResource) GetKey() string {
	bb, err := lr.Unstructured.MarshalJSON()
	if err != nil {
		innerlog.Error(err, "unable to marshall", "unstructured", lr.Unstructured)
		panic(err)
	}
	paths := append([]string(nil), lr.ExcludedPaths...)
	sort.Strings(paths)
	return string(bb) + "\x00" + strings.Join(paths, "\x00")
}

// GetLockedResources turns an array of Resources as read from an API into an array of LockedResources, usable by the LockedResourceManager
func GetLockedResources(resources []utilsapi.LockedResource) ([]LockedResource, error) {
	lockedResources := []LockedResource{}
	for _, resource := range resources {
		bb, err := yaml.YAMLToJSON(resource.Object.Raw)
		if err != nil {
			innerlog.Error(err, "Error transforming yaml to json", "raw", resource.Object.Raw)
			return []LockedResource{}, err
		}
		obj := &unstructured.Unstructured{}
		err = json.Unmarshal(bb, obj)
		if err != nil {
			innerlog.Error(err, "Error unmarshalling json manifest", "manifest", string(bb))
			return []LockedResource{}, err
		}
		lockedResources = append(lockedResources, LockedResource{
			Unstructured:  *obj,
			ExcludedPaths: resource.ExcludedPaths,
		})
	}
	return lockedResources, nil
}

// templates caches parsed templates by text. It is read and written from every controller that
// renders through this package, concurrently, so it must be a sync.Map: the plain map it replaced
// was a data race and, under load at operator start, a fatal "concurrent map writes". A cached
// entry is a parsed base that is never executed: the FuncMap (lookup in particular) closes over a
// rest config, so getTemplate clones the base and rebinds the caller's functions on every call.
// Keying the cache by config material was tried and shown incomplete in review: Password, TLS
// settings, impersonation groups, exec and auth providers, proxy and transport all reach lookup
// and cannot all be fingerprinted, and a pointer key grew one entry per copied config.
var templates sync.Map // map[string]*template.Template

// GetLockedResourcesFromTemplates Keep backwards compatability with existing consumers
func GetLockedResourcesFromTemplates(resources []utilsapi.LockedResourceTemplate, params interface{}) ([]LockedResource, error) {

	return GetLockedResourcesFromTemplatesWithRestConfig(resources, nil, params)
}

// GetLockedResourcesFromTemplatesWithRestConfig turns an array of ResourceTemplates as read from an API into an array of LockedResources using a params to process the templates
func GetLockedResourcesFromTemplatesWithRestConfig(resources []utilsapi.LockedResourceTemplate, config *rest.Config, params interface{}) ([]LockedResource, error) {
	lockedResources := []LockedResource{}
	ctx := context.TODO()
	ctx = context.WithValue(ctx, "restConfig", config)
	ctx = log.IntoContext(ctx, innerlog)
	for _, resource := range resources {
		template, err := getTemplate(&resource, config, innerlog)
		if err != nil {
			innerlog.Error(err, "unable to retrieve template for", "resource", resource)
			return []LockedResource{}, nil
		}
		objs, err := utilstemplates.ProcessTemplateArray(ctx, params, template)
		if err != nil {
			innerlog.Error(err, "unable to process template for", "resource", resource, "params", params)
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

func getTemplate(resource *utilsapi.LockedResourceTemplate, config *rest.Config, logger logr.Logger) (*template.Template, error) {
	text := resource.ObjectTemplate
	base, found := templates.Load(text)
	if !found {
		parsed, err := template.New(text).Funcs(utilstemplates.AdvancedTemplateFuncMap(config, logger)).Parse(text)
		if err != nil {
			innerlog.Error(err, "unable to parse", "template", text)
			return nil, err
		}
		// Concurrent first users may each parse the text; exactly one base is kept.
		base, _ = templates.LoadOrStore(text, parsed)
	}
	// text/template's Clone copies the function maps, so Funcs on the clone leaves the base alone;
	// Funcs is documented as legal after parsing, to replace a function before execution.
	bound, err := base.(*template.Template).Clone()
	if err != nil {
		innerlog.Error(err, "unable to clone", "template", text)
		return nil, err
	}
	return bound.Funcs(utilstemplates.AdvancedTemplateFuncMap(config, logger)), nil
}

// DefaultExcludedPaths represents paths that are exlcuded by default in all resources
var DefaultExcludedPaths = []string{".metadata", ".status", ".spec.replicas"}

// DefaultExcludedPathsSet represents paths that are exlcuded by default in all resources
var DefaultExcludedPathsSet = strset.New(DefaultExcludedPaths...)

// GetResources returs an arrays of apis.Resources from an arya of LockedResources, useful for mass operations on the LockedResources
func GetResources(lockedResources []LockedResource) []client.Object {
	resources := []client.Object{}
	for _, lockedResource := range lockedResources {
		resources = append(resources, &lockedResource.Unstructured)
	}
	return resources
}
