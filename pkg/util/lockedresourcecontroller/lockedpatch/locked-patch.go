package lockedpatch

import (
	"text/template"

	"github.com/prometheus/common/log"
	"github.com/redhat-cop/operator-utils/pkg/util/apis"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

//LockedPatch represents a patch that needs to be enforced.
type LockedPatch struct {
	SourceObjectRefs []corev1.ObjectReference
	TargetObjectRef  corev1.ObjectReference
	PatchType        types.PatchType
	PatchTemplate    string
	Template         template.Template
}

//HashableLockedPatch a represenation of a LockedPatch that can be sued as a hash key, useful for set-based operations
type HashableLockedPatch struct {
	MarshalledSourceObjectRefs string
	TargetObjectRef            corev1.ObjectReference
	PatchType                  types.PatchType
	PatchTemplate              string
	Template                   template.Template
}

//GetKey returns a not so unique key for a patch
func (lp *LockedPatch) GetKey() string {
	return lp.TargetObjectRef.String()
}

func (lp *LockedPatch) getHashableLockedPatch() (HashableLockedPatch, error) {
	bb := []byte{}
	for _, objectReference := range lp.SourceObjectRefs {
		b, err := objectReference.Marshal()
		if err != nil {
			log.Error(err, "unable to marshall", "objectreference", objectReference)
			return HashableLockedPatch{}, err
		}
		bb = append(bb, b...)
	}
	return HashableLockedPatch{
		MarshalledSourceObjectRefs: string(bb),
		TargetObjectRef:            lp.TargetObjectRef,
		PatchType:                  lp.PatchType,
		PatchTemplate:              lp.PatchTemplate,
		Template:                   lp.Template,
	}, nil
}

//GetHashableLockedPatchMap returns a map and a slice of HashableLockedPatch, useful for set based operations. Needed for internal implementation.
func GetHashableLockedPatchMap(lockedPatches []LockedPatch) (map[HashableLockedPatch]LockedPatch, []HashableLockedPatch, error) {
	hashableLockedPatchMap := map[HashableLockedPatch]LockedPatch{}
	hashableLockedPatches := []HashableLockedPatch{}
	for _, lockedPatch := range lockedPatches {
		hashableLockedPatch, err := lockedPatch.getHashableLockedPatch()
		if err != nil {
			log.Error(err, "unable to get HAshableLockedPathc from", "LockedPatch", lockedPatch)
			return map[HashableLockedPatch]LockedPatch{}, []HashableLockedPatch{}, err
		}
		hashableLockedPatchMap[hashableLockedPatch] = lockedPatch
		hashableLockedPatches = append(hashableLockedPatches, hashableLockedPatch)
	}
	return hashableLockedPatchMap, hashableLockedPatches, nil
}

//GetLockedPatches retunrs a slice of LockedPatches from a slicd of apis.Patches
func GetLockedPatches(patches []apis.Patch) ([]LockedPatch, error) {
	lockedPatches := []LockedPatch{}
	for _, patch := range patches {
		template, err := template.New(patch.PatchTemplate).Parse(patch.PatchTemplate)
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
		})
	}
	return lockedPatches, nil
}
