package lockedresource

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func FilterOutPaths(obj *unstructured.Unstructured, jsonPaths []string) (*unstructured.Unstructured, error) {
	doc, err := obj.MarshalJSON()
	if err != nil {
		innerlog.Error(err, "unable to marshall", "unstructured", obj)
		return &unstructured.Unstructured{}, err
	}

	patches, err := createPatchesFromJSONPaths(jsonPaths)
	if err != nil {
		innerlog.Error(err, "unable to create patches from", "jsonPaths", jsonPaths)
		return &unstructured.Unstructured{}, err
	}
	for _, patch := range patches {
		decodedPatch, err := jsonpatch.DecodePatch(patch)
		if err != nil {
			innerlog.Error(err, "unable to decode", "patch", string(patch))
			return &unstructured.Unstructured{}, err
		}
		doc1, err := decodedPatch.Apply(doc)
		if err != nil {
			// An excluded path that the document does not have (a missing key, or an array index past
			// the end) is not an error: there is nothing to exclude.
			if strings.Contains(err.Error(), "Unable to remove nonexistent key") ||
				strings.Contains(err.Error(), "remove operation does not apply: doc is missing path") ||
				strings.Contains(err.Error(), "Unable to access invalid index") {
				continue
			}
			innerlog.Error(err, "unable to apply", "patch", patch, "to json", string(doc))
			return &unstructured.Unstructured{}, err
		}
		doc = doc1
	}

	var result = &unstructured.Unstructured{}

	err = result.UnmarshalJSON(doc)

	if err != nil {
		innerlog.Error(err, "unable to unMarshall", "json", doc)
		return &unstructured.Unstructured{}, err
	}

	return result, nil
}

// Patch represents a patch operation
type Patch struct {
	Operation string `json:"op"`
	Path      string `json:"path"`
}

func createPatchesFromJSONPaths(jsonPaths []string) ([][]byte, error) {
	result := [][]byte{}
	for _, jsonPath := range jsonPaths {
		if err := validatePath(jsonPath); err != nil {
			return [][]byte{}, err
		}
		pointer := getMergePathFromJSONPath(jsonPath)
		patch := []Patch{
			{
				Operation: "remove",
				Path:      pointer,
			},
		}
		mpatch, err := json.Marshal(patch)
		if err != nil {
			innerlog.Error(err, "unable to marshal", "patch", patch)
			return [][]byte{}, err
		}
		result = append(result, mpatch)
	}
	return result, nil
}

var indexedSegment = regexp.MustCompile(`\[\s*(?:(-?\d+)|'([^']*)'|"([^"]*)")\s*\]`)

// A quoted key is data, whatever it contains; it is removed before the path's own syntax is checked.
var quotedSegment = regexp.MustCompile(`\[\s*(?:'[^']*'|"[^"]*")\s*\]`)

// A negative index in any spelling: "[-1]", ".rules.-1", "/rules/-1".
var negativeIndex = regexp.MustCompile(`\[\s*-\d+\s*\]|(?:^|[./])-\d+(?:[./\[]|$)`)

// validatePath reports an excluded path that cannot name a fixed location:
//   - a negative index: json-patch would count it from the end and report an out-of-range one with
//     the same text as a past-the-end index, so it would be silently ignored;
//   - a bracket that is not a complete [n], ['key'] or ["key"] expression, or one glued to text
//     (".data[0]foo" would become "/data/0foo" and match nothing);
//   - an empty segment (a trailing dot, or two dots in a row), or a path that names only the root.
//
// Measured in review before these checks: ".data[" removed nothing, silently, and ".data." removed
// all of "data" because a trailing dot was trimmed. A typo in an excluded path must be reported, not
// retargeted. A quoted key is data, whatever it contains, so ".data['-1']" is a key; a key that
// contains one kind of quote is written with the other kind. An RFC 6901 pointer ("/data/a.b") is
// passed through untouched, dots and brackets in its segments included; in that form a segment
// that is a negative number is taken for an index.
func validatePath(jsonPath string) error {
	if strings.HasPrefix(jsonPath, "/") {
		if negativeIndex.MatchString(jsonPath) {
			return fmt.Errorf("excluded path %q has a negative index; indexes count from 0", jsonPath)
		}
		return nil
	}
	path := strings.TrimPrefix(jsonPath, "$")
	if strings.TrimSpace(path) == "" || path == "." {
		return fmt.Errorf("excluded path %q names no field", jsonPath)
	}
	for _, loc := range indexedSegment.FindAllStringIndex(path, -1) {
		if end := loc[1]; end < len(path) && path[end] != '.' && path[end] != '[' {
			return fmt.Errorf("excluded path %q has a malformed bracket expression; use [n], ['key'] or [\"key\"]", jsonPath)
		}
	}
	unquoted := quotedSegment.ReplaceAllString(path, "")
	if negativeIndex.MatchString(unquoted) {
		return fmt.Errorf("excluded path %q has a negative index; indexes count from 0", jsonPath)
	}
	stripped := indexedSegment.ReplaceAllString(unquoted, "")
	if strings.ContainsAny(stripped, "[]") {
		return fmt.Errorf("excluded path %q has a malformed bracket expression; use [n], ['key'] or [\"key\"]", jsonPath)
	}
	// The root dot is not a segment: ".['root']" strips to "." and is well formed.
	stripped = strings.TrimPrefix(stripped, ".")
	if strings.HasSuffix(stripped, ".") || strings.Contains(stripped, "..") {
		return fmt.Errorf("excluded path %q has an empty segment", jsonPath)
	}
	return nil
}

// getMergePathFromJSONPath turns a dotted JSON path (".spec.replicas", ".rules[0]", "$.data['a.b']")
// into the RFC 6901 JSON pointer a remove operation needs ("/spec/replicas", "/rules/0", "/data/a.b").
// The previous conversion dropped the last two characters of any path ending in "]", so ".rules[0]"
// became "/rules/" (an error on every reconcile) and ".rules[10]" became "/rules/1" (the wrong
// element removed silently).
func getMergePathFromJSONPath(jsonPath string) string {
	// Only a path that names the root (".x" or "$.x") gets the root slash. Before the index fix an
	// unrooted "spec.replicas" produced "spec/replicas", which json-patch treated as a no-op; keeping
	// that (rather than silently turning it into "/spec/replicas") preserves what existing callers
	// observed. The rooted spelling is the documented one.
	if strings.HasPrefix(jsonPath, "/") {
		// Already a pointer: converting its dots would retarget "/data/a.b" to "/data/a/b".
		return jsonPath
	}
	rooted := strings.HasPrefix(jsonPath, "$") || strings.HasPrefix(jsonPath, ".")
	jsonPath = strings.TrimPrefix(jsonPath, "$")
	// [n] and ['key'] / ["key"] become dotted segments; a bracketed key may itself contain dots or
	// slashes, so it is escaped per RFC 6901 before the dots are turned into separators.
	jsonPath = indexedSegment.ReplaceAllStringFunc(jsonPath, func(m string) string {
		sub := indexedSegment.FindStringSubmatch(m)
		seg := sub[1]
		if seg == "" {
			seg = sub[2]
			if seg == "" {
				seg = sub[3]
			}
			seg = strings.NewReplacer("~", "~0", "/", "~1", ".", "\x00").Replace(seg)
		}
		return "." + seg
	})
	// ".['root']" is now "..root"; without this trim it would become "//root" and match nothing.
	jsonPath = strings.TrimPrefix(jsonPath, ".")
	pointer := strings.ReplaceAll(jsonPath, ".", "/")
	if rooted && !strings.HasPrefix(pointer, "/") {
		pointer = "/" + pointer
	}
	// Restore the dots that belonged to bracketed keys.
	return strings.ReplaceAll(pointer, "\x00", ".")
}

// NormalizeJSONPaths rewrites excluded paths into the dotted form the null-field logic compares
// against: no leading "$", indexes and bracketed keys as ".segment", so ".rules[0]" and "$.rules.0"
// are the same path.
func NormalizeJSONPaths(jsonPaths []string) []string {
	out := make([]string, 0, len(jsonPaths))
	for _, jp := range jsonPaths {
		out = append(out, strings.ReplaceAll(getMergePathFromJSONPath(jp), "/", "."))
	}
	return out
}

// FieldSegment is one step of an excluded path for a caller that searches a managedFields set:
// the key, unescaped, and whether the path wrote it as a list index. A quoted key is never an
// index whatever it contains (".data['0']" names the key "0"); "[0]", a bare ".0" and a numeric
// segment of an RFC 6901 pointer are.
type FieldSegment struct {
	Name  string
	Index bool
}

// FieldPath returns an excluded path as its segments. An unrooted path, a no-op for FilterOutPaths,
// is a no-op here too (nil); a malformed path is reported as it is there.
func FieldPath(jsonPath string) ([]FieldSegment, error) {
	if err := validatePath(jsonPath); err != nil {
		return nil, err
	}
	if strings.HasPrefix(jsonPath, "/") {
		if jsonPath == "/" {
			return nil, nil
		}
		var out []FieldSegment
		for _, raw := range strings.Split(jsonPath[1:], "/") {
			name := strings.NewReplacer("~1", "/", "~0", "~").Replace(raw)
			out = append(out, FieldSegment{Name: name, Index: isNumeric(name)})
		}
		return out, nil
	}
	if !strings.HasPrefix(jsonPath, "$") && !strings.HasPrefix(jsonPath, ".") {
		return nil, nil
	}
	rest := strings.TrimPrefix(jsonPath, "$")
	var out []FieldSegment
	for rest != "" {
		switch {
		case strings.HasPrefix(rest, "["):
			m := indexedSegment.FindStringSubmatchIndex(rest)
			if m == nil || m[0] != 0 {
				return nil, fmt.Errorf("excluded path %q has a malformed bracket expression", jsonPath)
			}
			switch {
			case m[2] >= 0:
				out = append(out, FieldSegment{Name: rest[m[2]:m[3]], Index: true})
			case m[4] >= 0:
				out = append(out, FieldSegment{Name: rest[m[4]:m[5]]})
			default:
				out = append(out, FieldSegment{Name: rest[m[6]:m[7]]})
			}
			rest = rest[m[1]:]
		case strings.HasPrefix(rest, "."):
			rest = rest[1:]
			end := strings.IndexAny(rest, ".[")
			if end < 0 {
				end = len(rest)
			}
			if name := rest[:end]; name != "" {
				out = append(out, FieldSegment{Name: name, Index: isNumeric(name)})
			}
			rest = rest[end:]
		default:
			return nil, fmt.Errorf("excluded path %q: unexpected text %q", jsonPath, rest)
		}
	}
	return out, nil
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
