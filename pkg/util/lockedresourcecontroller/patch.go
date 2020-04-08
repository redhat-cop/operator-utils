package lockedresourcecontroller

import (
	"encoding/json"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func filterOutPaths(obj *unstructured.Unstructured, jsonPaths []string) (*unstructured.Unstructured, error) {
	doc, err := obj.MarshalJSON()
	if err != nil {
		log.Error(err, "unable to marshall", "unstructured", obj)
		return &unstructured.Unstructured{}, err
	}

	patches, err := createPatchesFromJsonPaths(jsonPaths)
	if err != nil {
		log.Error(err, "unable to create patches from", "jsonPaths", jsonPaths)
		return &unstructured.Unstructured{}, err
	}
	for _, patch := range patches {
		decodedPatch, err := jsonpatch.DecodePatch(patch)
		if err != nil {
			log.Error(err, "unable to decode", "patch", string(patch))
			return &unstructured.Unstructured{}, err
		}
		doc1, err := decodedPatch.Apply(doc)
		if err != nil {
			if strings.Contains(err.Error(), "Unable to remove nonexistent key") {
				continue
			}
			log.Error(err, "unable to apply", "patch", patch, "to json", string(doc))
			return &unstructured.Unstructured{}, err
		}
		doc = doc1
	}

	var result = &unstructured.Unstructured{}

	err = result.UnmarshalJSON(doc)

	if err != nil {
		log.Error(err, "unable to unMarshall", "json", doc)
		return &unstructured.Unstructured{}, err
	}

	return result, nil
}

type Patch struct {
	Operation string `json:"op"`
	Path      string `json:"path"`
}

func createPatchesFromJsonPaths(jsonPaths []string) ([][]byte, error) {
	result := [][]byte{}
	for _, jsonPath := range jsonPaths {
		patch := []Patch{
			Patch{
				Operation: "remove",
				Path:      getMergePathFromJsonPath(jsonPath),
			},
		}
		mpatch, err := json.Marshal(patch)
		if err != nil {
			log.Error(err, "unable to marshal", "patch", patch)
			return [][]byte{}, err
		}
		result = append(result, mpatch)
	}
	return result, nil
}

func getMergePathFromJsonPath(jsonPath string) string {
	//remove "$" if present
	if strings.HasPrefix(jsonPath, "$") {
		jsonPath = jsonPath[1:]
	}
	// convert "[" and "]" to "."
	if strings.HasSuffix(jsonPath, "]") {
		jsonPath = jsonPath[:len(jsonPath)-2]
	}
	jsonPath = strings.ReplaceAll(jsonPath, "[", ".")
	jsonPath = strings.ReplaceAll(jsonPath, "]", ".")
	// convert "." to "/"
	jsonPath = strings.ReplaceAll(jsonPath, ".", "/")
	return jsonPath
}
