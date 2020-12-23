package lockedpatch

import (
	"text/template"

	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
	"github.com/scylladb/go-set/strset"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("lockedpatch")

//LockedPatch represents a patch that needs to be enforced.
type LockedPatch struct {
	ID               string                   `json:"id,omitempty"`
	SourceObjectRefs []corev1.ObjectReference `json:"sourceObjectRefs,omitempty"`
	TargetObjectRef  corev1.ObjectReference   `json:"targetObjectRef,omitempty"`
	PatchType        types.PatchType          `json:"patchType,omitempty"`
	PatchTemplate    string                   `json:"patchTemplate,omitempty"`
	Template         template.Template        `json:"-"`
}

//GetKey returns a not so unique key for a patch
func (lp *LockedPatch) GetKey() string {
	return lp.ID
}

//GetLockedPatchMap returns a map and a slice of LockedPatch, useful for set based operations. Needed for internal implementation.
func GetLockedPatchMap(lockedPatches []LockedPatch) (map[string]LockedPatch, []string) {
	lockedPatchMap := map[string]LockedPatch{}
	lockedPatcheIDs := []string{}
	for _, lockedPatch := range lockedPatches {
		lockedPatchMap[lockedPatch.ID] = lockedPatch
		lockedPatcheIDs = append(lockedPatcheIDs, lockedPatch.ID)
	}
	return lockedPatchMap, lockedPatcheIDs
}

func GetLockedPatchedFromLockedPatchesSet(lockedPatchSet *strset.Set, lockedPatchMap map[string]LockedPatch) []LockedPatch {
	lockedPatches := []LockedPatch{}
	for _, lockedPatchID := range lockedPatchSet.List() {
		lockedPatches = append(lockedPatches, lockedPatchMap[lockedPatchID])
	}
	return lockedPatches
}

//GetLockedPatches retunrs a slice of LockedPatches from a slicd of apis.Patches
func GetLockedPatches(patches []apis.Patch, config *rest.Config) ([]LockedPatch, error) {
	lockedPatches := []LockedPatch{}
	for _, patch := range patches {
		template, err := template.New(patch.PatchTemplate).Funcs(util.AdvancedTemplateFuncMap(config)).Parse(patch.PatchTemplate)
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
			ID:               patch.ID,
		})
	}
	return lockedPatches, nil
}
