package lockedpatch

import (
	"text/template"

	"github.com/prometheus/common/log"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
	"github.com/scylladb/go-set/strset"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

//LockedPatch represents a patch that needs to be enforced.
type LockedPatch struct {
	ID               string
	SourceObjectRefs []corev1.ObjectReference
	TargetObjectRef  corev1.ObjectReference
	PatchType        types.PatchType
	PatchTemplate    string
	Template         template.Template
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
func GetLockedPatches(patches []apis.Patch) ([]LockedPatch, error) {
	lockedPatches := []LockedPatch{}
	for _, patch := range patches {
		template, err := template.New(patch.PatchTemplate).Funcs(util.CustomFuncMap()).Parse(patch.PatchTemplate)
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
