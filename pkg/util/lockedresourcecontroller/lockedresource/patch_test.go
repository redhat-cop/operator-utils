package lockedresource

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"text/template"

	utilsapi "github.com/redhat-cop/operator-utils/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
)

func TestGetMergePathFromJSONPath(t *testing.T) {
	cases := map[string]string{
		".spec.replicas":            "/spec/replicas",
		"$.spec.replicas":           "/spec/replicas",
		".metadata":                 "/metadata",
		".rules[0]":                 "/rules/0",
		".rules[10]":                "/rules/10",
		".spec.containers[2].image": "/spec/containers/2/image",
		".data['a.b']":              "/data/a.b",
		`.data["x/y"]`:              "/data/x~1y",
		".rules[0].verbs[1]":        "/rules/0/verbs/1",
	}
	for in, want := range cases {
		if got := getMergePathFromJSONPath(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeJSONPaths(t *testing.T) {
	got := NormalizeJSONPaths([]string{"$.rules[0]", ".data['a.b']", ".spec"})
	want := []string{".rules.0", ".data.a.b", ".spec"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFilterOutPaths_IndexedPathRemovesTheRightElement(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"kind": "Role",
		"rules": []interface{}{
			map[string]interface{}{"verbs": []interface{}{"get"}},
			map[string]interface{}{"verbs": []interface{}{"list", "watch"}},
		},
	}}
	out, err := FilterOutPaths(obj, []string{".rules[0]"})
	if err != nil {
		t.Fatalf("an indexed path must be a valid remove, got %v", err)
	}
	rules := out.Object["rules"].([]interface{})
	if len(rules) != 1 || !reflect.DeepEqual(rules[0].(map[string]interface{})["verbs"], []interface{}{"list", "watch"}) {
		t.Errorf("expected only the second rule to remain, got %v", rules)
	}
	out, err = FilterOutPaths(obj, []string{".rules[1].verbs[0]"})
	if err != nil {
		t.Fatal(err)
	}
	if verbs := out.Object["rules"].([]interface{})[1].(map[string]interface{})["verbs"]; !reflect.DeepEqual(verbs, []interface{}{"watch"}) {
		t.Errorf("expected the first verb of the second rule removed, got %v", verbs)
	}
	// A path that does not exist is still ignored, as before.
	if _, err := FilterOutPaths(obj, []string{".rules[5]", ".nope"}); err != nil {
		t.Errorf("missing paths must be ignored, got %v", err)
	}
}

// The parse cache is shared by every controller in the process; it must survive concurrent use and
// must not hand a template parsed with one rest config to a caller with another.
// lookupTransport answers the two requests `lookup "v1" "ConfigMap"` makes (discovery of v1, then
// the object) and labels the object with the identity of the transport that served it.
type lookupTransport struct{ source string }

func (tr lookupTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body string
	switch req.URL.Path {
	case "/api/v1":
		body = `{"apiVersion":"v1","kind":"APIResourceList","groupVersion":"v1","resources":[{"name":"configmaps","singularName":"configmap","namespaced":true,"kind":"ConfigMap","verbs":["get","list"]}]}`
	case "/api/v1/namespaces/ns/configmaps/name":
		body = fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"name","namespace":"ns","labels":{"source":%q}}}`, tr.source)
	default:
		return nil, fmt.Errorf("unexpected request %s", req.URL.Path)
	}
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// One parsed base per template text, and every caller executes with ITS config. Measured in review
// on the material-keyed cache: 32 configs that differed only in their transport shared one key, so
// whichever caller parsed first supplied everyone's lookup.
func TestGetTemplate_OneBasePerText_EachCallerBoundToItsConfig(t *testing.T) {
	templates = sync.Map{}
	const text = `{{ index (index (index (lookup "v1" "ConfigMap" "ns" "name") "metadata") "labels") "source" }}`
	const callers = 32
	type result struct {
		want, got string
		err       error
	}
	start := make(chan struct{})
	results := make(chan result, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		source := fmt.Sprintf("config-%02d", i)
		config := &rest.Config{Host: "https://same.invalid", Transport: lookupTransport{source: source}}
		wg.Add(1)
		go func(want string, config *rest.Config) {
			defer wg.Done()
			<-start
			tmpl, err := getTemplate(&utilsapi.LockedResourceTemplate{ObjectTemplate: text}, config, innerlog)
			if err != nil {
				results <- result{want: want, err: err}
				return
			}
			var out bytes.Buffer
			err = tmpl.Execute(&out, nil)
			results <- result{want: want, got: out.String(), err: err}
		}(source, config)
	}
	close(start)
	wg.Wait()
	close(results)
	for r := range results {
		if r.err != nil {
			t.Errorf("%s: %v", r.want, r.err)
			continue
		}
		if r.got != r.want {
			t.Errorf("a caller executed with another caller's config: got %q, want %q", r.got, r.want)
		}
	}
	entries := 0
	templates.Range(func(_, _ any) bool { entries++; return true })
	if entries != 1 {
		t.Errorf("expected one cached base for one text, got %d", entries)
	}
}

// Fresh config values per call (rest.CopyConfig, a literal) and a nil config all share the one base;
// each call gets its own clone, so the base is never handed out.
func TestGetTemplate_FreshConfigsShareOneBase(t *testing.T) {
	templates = sync.Map{}
	const text = "kind: ConfigMap\n"
	seen := map[*template.Template]bool{}
	for i := 0; i < 50; i++ {
		tmpl, err := getTemplate(&utilsapi.LockedResourceTemplate{ObjectTemplate: text}, &rest.Config{Host: "https://same"}, innerlog)
		if err != nil {
			t.Fatal(err)
		}
		seen[tmpl] = true
	}
	if tmpl, err := getTemplate(&utilsapi.LockedResourceTemplate{ObjectTemplate: text}, nil, innerlog); err != nil {
		t.Fatal(err)
	} else {
		seen[tmpl] = true
	}
	entries := 0
	templates.Range(func(_, v any) bool {
		entries++
		if seen[v.(*template.Template)] {
			t.Error("the cached base must never be returned to a caller")
		}
		return true
	})
	if entries != 1 {
		t.Errorf("expected one cache entry for one text, got %d", entries)
	}
	if len(seen) != 51 {
		t.Errorf("expected a private clone per call, got %d distinct templates", len(seen))
	}
}

func TestFilterOutPaths_NegativeIndexIsAnError(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{"kind": "Role", "rules": []interface{}{map[string]interface{}{"id": 0}, map[string]interface{}{"id": 1}}}}
	for _, path := range []string{".rules.-2", ".rules[-1]", ".rules.-1.id", "$.rules[ -1 ]", "/rules/-1"} {
		_, err := FilterOutPaths(obj, []string{path})
		if err == nil || !strings.Contains(err.Error(), "negative index") {
			t.Errorf("%q: a negative index is a misconfiguration and must be reported as such, got %v", path, err)
		}
	}
	// a quoted key that merely looks like a negative index is a key (measured: it was rejected before)
	keyed := &unstructured.Unstructured{Object: map[string]interface{}{"kind": "ConfigMap", "data": map[string]interface{}{"-1": "v", "x.-1": "w", "keep": "k"}}}
	for _, path := range []string{".data['-1']", `.data["-1"]`, ".data['x.-1']"} {
		out, err := FilterOutPaths(keyed, []string{path})
		if err != nil {
			t.Errorf("%q names a key, got %v", path, err)
			continue
		}
		if data := out.Object["data"].(map[string]interface{}); len(data) != 2 {
			t.Errorf("%q must remove exactly its key, got %v", path, data)
		}
	}
	// a non-negative past-the-end index stays a no-op
	if _, err := FilterOutPaths(obj, []string{".rules[5]"}); err != nil {
		t.Errorf("past-the-end index must be ignored, got %v", err)
	}
}

// An unrooted path kept its pre-fix meaning (no-op); only ".x" and "$.x" name the root.
func TestGetMergePathFromJSONPath_UnrootedStaysUnrooted(t *testing.T) {
	if got := getMergePathFromJSONPath("spec.replicas"); got != "spec/replicas" {
		t.Errorf("unrooted path must stay unrooted, got %q", got)
	}
	obj := &unstructured.Unstructured{Object: map[string]interface{}{"kind": "Deployment", "spec": map[string]interface{}{"replicas": int64(3)}}}
	out, err := FilterOutPaths(obj, []string{"spec.replicas"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Object["spec"].(map[string]interface{})["replicas"]; !ok {
		t.Error("unrooted spec.replicas must remain a no-op as before")
	}
}

// A typo in an excluded path is reported, not retargeted: before this check ".data." removed all of
// "data" and ".data[" removed nothing, both silently (measured in review).
func TestFilterOutPaths_MalformedPathIsAnError(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{"kind": "ConfigMap", "data": map[string]interface{}{"key": "value", "a.b": "dotted"}}}
	for _, path := range []string{".data.", ".data[", ".data[]", ".data..key", "$.data[key]", ".data['a.b'].", ".data[0", ".data[0]foo", ".data['foo']bar", "$.", ".", "$", "", ".data['it\\'s']"} {
		_, err := FilterOutPaths(obj, []string{path})
		if err == nil || !(strings.Contains(err.Error(), "malformed bracket") || strings.Contains(err.Error(), "empty segment") || strings.Contains(err.Error(), "names no field")) {
			t.Errorf("%q must be reported as malformed, got %v", path, err)
		}
	}
	// well-formed spellings, including a quoted key that contains a dot, still work
	for _, path := range []string{".data.key", "$.data.key", ".data['a.b']", ".data[\"a.b\"]", "/data/key"} {
		out, err := FilterOutPaths(obj, []string{path})
		if err != nil {
			t.Errorf("%q is well formed, got %v", path, err)
			continue
		}
		if data := out.Object["data"].(map[string]interface{}); len(data) != 1 {
			t.Errorf("%q must remove exactly one key, got %v", path, data)
		}
	}
}

// An RFC 6901 pointer is passed through untouched. Measured in review before this: "/data/a.b" was
// converted to "/data/a/b", the nested path, and the dotted key survived.
func TestFilterOutPaths_PointerInputIsNotConverted(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{"kind": "ConfigMap", "data": map[string]interface{}{"a.b": "dotted", "a": map[string]interface{}{"b": "nested"}, "[x]": "bracketed"}}}
	for _, path := range []string{"/data/a.b", "/data/[x]"} {
		if got := getMergePathFromJSONPath(path); got != path {
			t.Errorf("%q must pass through, got %q", path, got)
		}
	}
	out, err := FilterOutPaths(obj, []string{"/data/a.b", "/data/[x]"})
	if err != nil {
		t.Fatal(err)
	}
	data := out.Object["data"].(map[string]interface{})
	if _, ok := data["a.b"]; ok {
		t.Errorf("the dotted key must be removed, got %v", data)
	}
	if _, ok := data["[x]"]; ok {
		t.Errorf("the bracketed key must be removed, got %v", data)
	}
	if data["a"].(map[string]interface{})["b"] != "nested" {
		t.Errorf("the nested path must be untouched, got %v", data)
	}
	if _, err := FilterOutPaths(obj, []string{"/data/-1"}); err == nil || !strings.Contains(err.Error(), "negative index") {
		t.Errorf("a negative segment in pointer form is an index, got %v", err)
	}
}

// An empty quoted key names the key ""; it is well formed. At 84aa264 the trailing-dot trim turned
// it into the parent and removed all of "data" (both reviewers, second pass).
func TestFilterOutPaths_EmptyQuotedKey(t *testing.T) {
	for _, path := range []string{".data['']", `.data[""]`} {
		if got := getMergePathFromJSONPath(path); got != "/data/" {
			t.Errorf("%q must be the empty pointer token, got %q", path, got)
		}
		obj := &unstructured.Unstructured{Object: map[string]interface{}{"kind": "ConfigMap", "data": map[string]interface{}{"": "empty", "keep": "k"}}}
		out, err := FilterOutPaths(obj, []string{path})
		if err != nil {
			t.Fatal(err)
		}
		data, ok := out.Object["data"].(map[string]interface{})
		if !ok || len(data) != 1 || data["keep"] != "k" {
			t.Errorf("%q must remove only the empty key, got %v", path, out.Object["data"])
		}
	}
}

// A quoted key at the root: ".['root']" became "//root" and matched nothing (volunteered in review).
func TestFilterOutPaths_RootQuotedKey(t *testing.T) {
	for _, path := range []string{".['root']", "$['root']", `$["root"]`} {
		if got := getMergePathFromJSONPath(path); got != "/root" {
			t.Errorf("%q must be /root, got %q", path, got)
		}
	}
	obj := &unstructured.Unstructured{Object: map[string]interface{}{"kind": "ConfigMap", "root": "v", "keep": "k"}}
	out, err := FilterOutPaths(obj, []string{".['root']"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Object["root"]; ok || out.Object["keep"] != "k" {
		t.Errorf("must remove root and keep the rest, got %v", out.Object)
	}
}

// FieldPath hands the excluded path to callers that search a managedFields set: unescaped keys,
// an index only where the path wrote one, unrooted and root-only paths as no-ops, malformed paths
// reported. Measured in review before the Index flag: ".data['0']" was taken for a list index and
// released all of data.
func TestFieldPath(t *testing.T) {
	key := func(n string) FieldSegment { return FieldSegment{Name: n} }
	idx := func(n string) FieldSegment { return FieldSegment{Name: n, Index: true} }
	cases := map[string][]FieldSegment{
		".spec.replicas":        {key("spec"), key("replicas")},
		"$.rules[0].verbs":      {key("rules"), idx("0"), key("verbs")},
		".rules.0":              {key("rules"), idx("0")},
		`.data["x/y"]`:          {key("data"), key("x/y")},
		".data['a.b'].c":        {key("data"), key("a.b"), key("c")},
		".data['0']":            {key("data"), key("0")},
		`.data[""]`:             {key("data"), key("")},
		".['root']":             {key("root")},
		"/metadata/labels/a~1b": {key("metadata"), key("labels"), key("a/b")},
		"/rules/0":              {key("rules"), idx("0")},
		"spec.replicas":         nil,
	}
	for in, want := range cases {
		got, err := FieldPath(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q -> %v, want %v", in, got, want)
		}
	}
	for _, in := range []string{".rules[-1]", ".data[", "$.", ".data[0]foo"} {
		if _, err := FieldPath(in); err == nil {
			t.Errorf("%q must be reported as malformed", in)
		}
	}
}

// The excluded paths are part of a locked resource's identity, in any order.
func TestGetKey_IncludesExcludedPaths(t *testing.T) {
	obj := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]interface{}{"name": "x"}}}
	a := LockedResource{Unstructured: obj, ExcludedPaths: []string{".metadata", ".status"}}
	b := LockedResource{Unstructured: obj, ExcludedPaths: []string{".status", ".metadata"}}
	c := LockedResource{Unstructured: obj, ExcludedPaths: []string{".status"}}
	if a.GetKey() != b.GetKey() {
		t.Error("the same paths in another order are the same resource")
	}
	if a.GetKey() == c.GetKey() {
		t.Error("different excluded paths are a different locked resource: the enforcer must restart its reconciler")
	}
}
