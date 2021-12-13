package lockedpatch

import (
	"text/template"

	"github.com/go-logr/logr"
	utilsapi "github.com/redhat-cop/operator-utils/v2/api/v1alpha1"
	utilstemplate "github.com/redhat-cop/operator-utils/v2/pkg/util/templates"
	"github.com/scylladb/go-set/strset"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("lockedpatch")

//LockedPatch represents a patch that needs to be enforced.
type LockedPatch struct {
	Name             string                           `json:"name,omitempty"`
	SourceObjectRefs []utilsapi.SourceObjectReference `json:"sourceObjectRefs,omitempty"`
	TargetObjectRef  utilsapi.TargetObjectReference   `json:"targetObjectRef,omitempty"`
	PatchType        types.PatchType                  `json:"patchType,omitempty"`
	PatchTemplate    string                           `json:"patchTemplate,omitempty"`
	Template         template.Template                `json:"-"`
}

//GetKey returns a not so unique key for a patch
func (lp *LockedPatch) GetKey() string {
	return lp.Name
}

//GetLockedPatchMap returns a map and a slice of LockedPatch, useful for set based operations. Needed for internal implementation.
func GetLockedPatchMap(lockedPatches []LockedPatch) (map[string]LockedPatch, []string) {
	lockedPatchMap := map[string]LockedPatch{}
	lockedPatcheIDs := []string{}
	for _, lockedPatch := range lockedPatches {
		lockedPatchMap[lockedPatch.Name] = lockedPatch
		lockedPatcheIDs = append(lockedPatcheIDs, lockedPatch.Name)
	}
	return lockedPatchMap, lockedPatcheIDs
}

func GetLockedPatchesFromLockedPatcheSet(lockedPatchSet *strset.Set, lockedPatchMap map[string]LockedPatch) []LockedPatch {
	lockedPatches := []LockedPatch{}
	for _, lockedPatchID := range lockedPatchSet.List() {
		lockedPatches = append(lockedPatches, lockedPatchMap[lockedPatchID])
	}
	return lockedPatches
}

//GetLockedPatches returns a slice of LockedPatches from a slice of apis.Patches
func GetLockedPatches(patches map[string]utilsapi.Patch, config *rest.Config, logger logr.Logger) ([]LockedPatch, error) {
	lockedPatches := []LockedPatch{}
	for key, patch := range patches {
		template, err := template.New(patch.PatchTemplate).Funcs(utilstemplate.AdvancedTemplateFuncMap(config, logger)).Parse(patch.PatchTemplate)
		if err != nil {
			log.Error(err, "unable to parse ", "template", patch.PatchTemplate)
			return []LockedPatch{}, err
		}
		lockedPatches = append(lockedPatches, LockedPatch{
			SourceObjectRefs: patch.SourceObjectRefs,
			PatchTemplate:    patch.PatchTemplate,
			PatchType:        patch.PatchType,
			TargetObjectRef:  patch.TargetObjectRef,
			Template:         *template,
			Name:             key,
		})
	}
	return lockedPatches, nil
}
