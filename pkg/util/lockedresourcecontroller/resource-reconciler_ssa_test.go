package lockedresourcecontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/redhat-cop/operator-utils/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/structured-merge-diff/v4/fieldpath"
)

// The reconciler's contract is the API server's server-side-apply behaviour, so these tests run
// against a real one (envtest). `make test` sets KUBEBUILDER_ASSETS; without it they are skipped
// with a message, never silently.
var testCfg *rest.Config

func TestMain(m *testing.M) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		fmt.Println("KUBEBUILDER_ASSETS is not set: the envtest-backed reconciler tests are skipped (run through `make test`)")
		os.Exit(m.Run())
	}
	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		fmt.Println("envtest failed to start:", err)
		os.Exit(1)
	}
	testCfg = cfg
	code := m.Run()
	_ = env.Stop()
	os.Exit(code)
}

func needEnvtest(t *testing.T) {
	t.Helper()
	if testCfg == nil {
		t.Skip("KUBEBUILDER_ASSETS is not set")
	}
}

func configMap(name string, data map[string]string, labels map[string]string) *unstructured.Unstructured {
	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]interface{}{"name": name, "namespace": "default"},
	}
	if labels != nil {
		obj["metadata"].(map[string]interface{})["labels"] = toInterface(labels)
	}
	if data != nil {
		obj["data"] = toInterface(data)
	}
	return &unstructured.Unstructured{Object: obj}
}

func toInterface(m map[string]string) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func newReconciler(t *testing.T, resource *unstructured.Unstructured, excluded []string) *LockedResourceReconciler {
	t.Helper()
	c, err := client.New(testCfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatal(err)
	}
	return &LockedResourceReconciler{
		Resource:       *resource,
		ExcludePaths:   excluded,
		ReconcilerBase: util.NewReconcilerBase(c, scheme.Scheme, testCfg, nil, c),
		log:            logr.Discard(),
	}
}

func reconcileOnce(t *testing.T, r *LockedResourceReconciler) {
	t.Helper()
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: r.Resource.GetName(), Namespace: r.Resource.GetNamespace()}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

func clientset(t *testing.T) *kubernetes.Clientset {
	t.Helper()
	cs, err := kubernetes.NewForConfig(testCfg)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func live(t *testing.T, name string) *corev1.ConfigMap {
	t.Helper()
	cm, err := clientset(t).CoreV1().ConfigMaps("default").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return cm
}

func ownedBy(t *testing.T, cm *corev1.ConfigMap, manager string, op metav1.ManagedFieldsOperationType) *fieldpath.Set {
	t.Helper()
	return ownedIn(t, cm.ManagedFields, manager, op)
}

func ownedIn(t *testing.T, entries []metav1.ManagedFieldsEntry, manager string, op metav1.ManagedFieldsOperationType) *fieldpath.Set {
	t.Helper()
	for _, e := range entries {
		if e.Manager == manager && e.Operation == op && e.FieldsV1 != nil {
			set := fieldpath.NewSet()
			if err := set.FromJSON(bytes.NewReader(e.FieldsV1.Raw)); err != nil {
				t.Fatal(err)
			}
			return set
		}
	}
	return nil
}

func owns(set *fieldpath.Set, parts ...interface{}) bool {
	return set != nil && set.Has(fieldpath.MakePathOrDie(parts...))
}

// Another actor writing through server-side apply under its own manager name.
func applyAs(t *testing.T, manager string, obj *unstructured.Unstructured) {
	t.Helper()
	data, err := json.Marshal(obj.Object)
	if err != nil {
		t.Fatal(err)
	}
	force := true
	_, err = clientset(t).CoreV1().ConfigMaps("default").Patch(context.Background(), obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{FieldManager: manager, Force: &force})
	if err != nil {
		t.Fatal(err)
	}
}

// An actor writing the old way: a client-side update under its own manager name.
func updateAs(t *testing.T, manager string, mutate func(*corev1.ConfigMap)) func(name string) {
	return func(name string) {
		t.Helper()
		cs := clientset(t)
		cm, err := cs.CoreV1().ConfigMaps("default").Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		mutate(cm)
		if _, err := cs.CoreV1().ConfigMaps("default").Update(context.Background(), cm, metav1.UpdateOptions{FieldManager: manager}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSSA_CreatesThenRestoresDrift(t *testing.T) {
	needEnvtest(t)
	r := newReconciler(t, configMap("ssa-drift", map[string]string{"a": "1"}, nil), nil)
	reconcileOnce(t, r)
	cm := live(t, "ssa-drift")
	if cm.Data["a"] != "1" {
		t.Fatalf("created object must carry the template's data, got %v", cm.Data)
	}
	if !owns(ownedBy(t, cm, FieldManager, metav1.ManagedFieldsOperationApply), "data", "a") {
		t.Fatalf("the reconciler must own data.a after creating, managedFields=%v", cm.ManagedFields)
	}
	updateAs(t, "someone", func(cm *corev1.ConfigMap) { cm.Data["a"] = "changed" })("ssa-drift")
	reconcileOnce(t, r)
	if got := live(t, "ssa-drift").Data["a"]; got != "1" {
		t.Fatalf("drift on an owned field must be restored, got %q", got)
	}
}

// The #194 case: the template stops rendering a field. Same manager, so the server removes it.
func TestSSA_FieldDroppedFromTemplateIsRemoved(t *testing.T) {
	needEnvtest(t)
	r := newReconciler(t, configMap("ssa-dropped", map[string]string{"a": "1", "b": "1"}, nil), nil)
	reconcileOnce(t, r)
	r.Resource = *configMap("ssa-dropped", map[string]string{"a": "1"}, nil)
	reconcileOnce(t, r)
	cm := live(t, "ssa-dropped")
	if _, still := cm.Data["b"]; still {
		t.Fatalf("a field the template no longer renders must be removed, got %v", cm.Data)
	}
	if cm.Data["a"] != "1" {
		t.Fatalf("the rendered field must remain, got %v", cm.Data)
	}
}

// An excluded path is set when the object is created and never enforced again: the reconciler
// releases its ownership before the next apply, so the server keeps whatever is there.
func TestSSA_ExcludedPathIsSetOnceThenLeftAlone(t *testing.T) {
	needEnvtest(t)
	r := newReconciler(t, configMap("ssa-excluded", map[string]string{"a": "1", "b": "from-template"}, nil), []string{".data.b"})
	reconcileOnce(t, r)
	if got := live(t, "ssa-excluded").Data["b"]; got != "from-template" {
		t.Fatalf("an excluded field is still set at creation, got %q", got)
	}
	updateAs(t, "someone", func(cm *corev1.ConfigMap) { cm.Data["b"] = "theirs"; cm.Data["a"] = "drift" })("ssa-excluded")
	reconcileOnce(t, r)
	cm := live(t, "ssa-excluded")
	if cm.Data["b"] != "theirs" {
		t.Fatalf("an excluded field must be left as another actor set it, got %q", cm.Data["b"])
	}
	if cm.Data["a"] != "1" {
		t.Fatalf("a non-excluded sibling must still be enforced, got %q", cm.Data["a"])
	}
	if owns(ownedBy(t, cm, FieldManager, metav1.ManagedFieldsOperationApply), "data", "b") {
		t.Fatalf("the reconciler must not own the excluded field, managedFields=%v", cm.ManagedFields)
	}
	// and later reconciles, with nothing left to release, neither delete it nor stop enforcing its
	// sibling (measured in review: a widening read off live ownership released all of data here)
	updateAs(t, "someone", func(cm *corev1.ConfigMap) { cm.Data["a"] = "drift-again" })("ssa-excluded")
	reconcileOnce(t, r)
	reconcileOnce(t, r)
	cm = live(t, "ssa-excluded")
	if cm.Data["b"] != "theirs" || cm.Data["a"] != "1" {
		t.Fatalf("an excluded field survives every reconcile and its sibling stays enforced, got %v", cm.Data)
	}
	if !owns(ownedBy(t, cm, FieldManager, metav1.ManagedFieldsOperationApply), "data", "a") {
		t.Fatalf("the reconciler must keep owning data.a, managedFields=%v", cm.ManagedFields)
	}
}

// The operator's default exclusion. Labels from the template are set at creation, and later left
// to whoever changes them; a label another actor adds is never touched.
func TestSSA_ExcludedMetadataKeepsLabelsFromCreation(t *testing.T) {
	needEnvtest(t)
	r := newReconciler(t, configMap("ssa-metadata", map[string]string{"a": "1"}, map[string]string{"owner": "template"}), []string{".metadata"})
	reconcileOnce(t, r)
	if got := live(t, "ssa-metadata").Labels["owner"]; got != "template" {
		t.Fatalf("labels are still set at creation with .metadata excluded, got %v", live(t, "ssa-metadata").Labels)
	}
	updateAs(t, "someone", func(cm *corev1.ConfigMap) { cm.Labels["owner"] = "changed"; cm.Labels["extra"] = "x" })("ssa-metadata")
	reconcileOnce(t, r)
	reconcileOnce(t, r)
	cm := live(t, "ssa-metadata")
	if cm.Labels["owner"] != "changed" || cm.Labels["extra"] != "x" {
		t.Fatalf("with .metadata excluded, labels must be left alone after creation, got %v", cm.Labels)
	}
	if owns(ownedBy(t, cm, FieldManager, metav1.ManagedFieldsOperationApply), "metadata", "labels") {
		t.Fatalf("the reconciler must not own metadata.labels, managedFields=%v", cm.ManagedFields)
	}
}

// An object created by an earlier version of this library through a client-side update: the
// stale field it owns must go once the template no longer renders it, after the entry is folded.
func TestSSA_LegacyClientSideEntryIsFolded(t *testing.T) {
	needEnvtest(t)
	saved := LegacyFieldManagers
	LegacyFieldManagers = []string{"legacy-manager"}
	t.Cleanup(func() { LegacyFieldManagers = saved })

	cs := clientset(t)
	if _, err := cs.CoreV1().ConfigMaps("default").Create(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "ssa-legacy", Namespace: "default", Labels: map[string]string{"owner": "legacy"}},
		Data:       map[string]string{"a": "1", "stale": "1"},
	}, metav1.CreateOptions{FieldManager: "legacy-manager"}); err != nil {
		t.Fatal(err)
	}
	before := live(t, "ssa-legacy")
	if ownedBy(t, before, "legacy-manager", metav1.ManagedFieldsOperationUpdate) == nil {
		t.Fatalf("precondition: the legacy Update entry must exist, managedFields=%v", before.ManagedFields)
	}

	r := newReconciler(t, configMap("ssa-legacy", map[string]string{"a": "1"}, nil), []string{".metadata"})
	reconcileOnce(t, r)
	cm := live(t, "ssa-legacy")
	if _, still := cm.Data["stale"]; still {
		t.Fatalf("a stale field owned by the folded legacy entry must be removed, got %v", cm.Data)
	}
	if cm.Labels["owner"] != "legacy" {
		t.Fatalf("labels under the excluded .metadata must survive the fold, got %v", cm.Labels)
	}
	if ownedBy(t, cm, "legacy-manager", metav1.ManagedFieldsOperationUpdate) != nil {
		t.Fatalf("the legacy entry must be gone, managedFields=%v", cm.ManagedFields)
	}
	set := ownedBy(t, cm, FieldManager, metav1.ManagedFieldsOperationApply)
	if !owns(set, "data", "a") || owns(set, "metadata", "labels") {
		t.Fatalf("after the fold the reconciler owns the rendered data and not the excluded metadata, got %v", set)
	}
	// the fold happens once: the next reconcile has nothing to release and nothing to apply
	reconcileOnce(t, r)
	if got := live(t, "ssa-legacy").ResourceVersion; got != cm.ResourceVersion {
		t.Fatalf("the second reconcile after a fold must write nothing: resourceVersion %s -> %s", cm.ResourceVersion, got)
	}
}

func TestSSA_NoOpApplyKeepsResourceVersion(t *testing.T) {
	needEnvtest(t)
	r := newReconciler(t, configMap("ssa-noop", map[string]string{"a": "1"}, map[string]string{"l": "v"}), []string{".metadata"})
	reconcileOnce(t, r)
	reconcileOnce(t, r) // releases ownership of the labels taken at creation
	rv := live(t, "ssa-noop").ResourceVersion
	reconcileOnce(t, r)
	reconcileOnce(t, r)
	if got := live(t, "ssa-noop").ResourceVersion; got != rv {
		t.Fatalf("a reconcile that changes nothing must not write: resourceVersion %s -> %s", rv, got)
	}
}

// A field another manager applied, which the template never renders, is not this reconciler's.
func TestSSA_OtherManagersFieldIsLeft(t *testing.T) {
	needEnvtest(t)
	r := newReconciler(t, configMap("ssa-other", map[string]string{"a": "1"}, nil), nil)
	reconcileOnce(t, r)
	applyAs(t, "other-controller", configMap("ssa-other", map[string]string{"theirs": "x"}, nil))
	reconcileOnce(t, r)
	cm := live(t, "ssa-other")
	if cm.Data["theirs"] != "x" || cm.Data["a"] != "1" {
		t.Fatalf("another manager's field must be left and ours enforced, got %v", cm.Data)
	}
}

// Pure: the trimming of a managedFields set by excluded prefixes.
func TestWithoutExcluded(t *testing.T) {
	set := fieldpath.NewSet()
	raw := `{"f:data":{".":{},"f:a":{},"f:b":{}},"f:metadata":{"f:labels":{".":{},"f:x":{}},"f:annotations":{".":{},"f:y":{}}},"f:rules":{}}`
	if err := set.FromJSON(bytes.NewReader([]byte(raw))); err != nil {
		t.Fatal(err)
	}
	kept := withoutExcluded(set, [][]string{{"data", "b"}, {"metadata", "labels"}, {"rules"}})
	want := map[string]bool{".data": true, ".data.a": true, ".metadata.annotations": true, ".metadata.annotations.y": true}
	got := map[string]bool{}
	kept.Iterate(func(p fieldpath.Path) { got[p.String()] = true })
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kept %v, want %v", got, want)
	}
}

// Pure: an index in an excluded path releases the list it indexes, whole.
func TestExcludedFieldPaths_IndexStopsAtTheList(t *testing.T) {
	r := &LockedResourceReconciler{ExcludePaths: []string{".rules[0].verbs", ".metadata.labels", "$.data['a.b']", "unrooted.path", ".spec.containers[2]"}}
	got, err := r.excludedFieldPaths()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"rules"}, {"metadata", "labels"}, {"data", "a.b"}, {"spec", "containers"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, err := (&LockedResourceReconciler{ExcludePaths: []string{".rules[-1]"}}).excludedFieldPaths(); err == nil {
		t.Fatal("a malformed excluded path must be reported")
	}
}

// A quoted key that looks like a number is a key, not an index: excluding it must release exactly
// that key. Measured in review: it used to release all of data.
func TestExcludedFieldPaths_NumericQuotedKeyIsNotAnIndex(t *testing.T) {
	r := &LockedResourceReconciler{ExcludePaths: []string{".data['0']", `.data["1"]`, ".rules[0]", ".rules.0.verbs"}}
	got, err := r.excludedFieldPaths()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"data", "0"}, {"data", "1"}, {"rules"}, {"rules"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSSA_NumericQuotedDataKeyIsSetOnce(t *testing.T) {
	needEnvtest(t)
	r := newReconciler(t, configMap("ssa-numkey", map[string]string{"0": "from-template", "a": "1"}, nil), []string{".data['0']"})
	reconcileOnce(t, r)
	updateAs(t, "someone", func(cm *corev1.ConfigMap) { cm.Data["0"] = "theirs"; cm.Data["a"] = "drift" })("ssa-numkey")
	reconcileOnce(t, r)
	cm := live(t, "ssa-numkey")
	if cm.Data["0"] != "theirs" || cm.Data["a"] != "1" {
		t.Fatalf("the quoted numeric key is set once and its sibling enforced, got %v", cm.Data)
	}
	set := ownedBy(t, cm, FieldManager, metav1.ManagedFieldsOperationApply)
	if owns(set, "data", "0") || !owns(set, "data", "a") {
		t.Fatalf("the reconciler must own data.a and not data.0, managedFields=%v", cm.ManagedFields)
	}
}

// A sink that keeps the messages, to check what the reconciler says about a reconcile.
type memLog struct{ lines []string }

func (m *memLog) Init(logr.RuntimeInfo)                    {}
func (m *memLog) Enabled(int) bool                         { return true }
func (m *memLog) Info(_ int, msg string, _ ...interface{}) { m.lines = append(m.lines, msg) }
func (m *memLog) Error(error, string, ...interface{})      {}
func (m *memLog) WithValues(...interface{}) logr.LogSink   { return m }
func (m *memLog) WithName(string) logr.LogSink             { return m }

// The "equal" verdict compares the apply's result with the object the apply raced, which is the
// one the ownership release returned; measured in review: comparing with the first GET called a
// release-then-no-op-apply "NOT equal".
func TestSSA_EqualLogOnlyWhenApplyDoesNotWrite(t *testing.T) {
	needEnvtest(t)
	sink := &memLog{}
	r := newReconciler(t, configMap("ssa-eqlog", map[string]string{"a": "1"}, map[string]string{"l": "v"}), []string{".metadata"})
	r.log = logr.New(sink)
	reconcileOnce(t, r) // creates
	reconcileOnce(t, r) // releases the labels, then applies without them: nothing to write
	var sawNotEqual, sawEqual bool
	for _, line := range sink.lines {
		switch line {
		case "determined that resources are NOT equal; applied":
			sawNotEqual = true
		case "determined that resources are equal":
			sawEqual = true
		}
	}
	if sawNotEqual || !sawEqual {
		t.Fatalf("a release followed by a no-op apply is 'equal'; lines=%v", sink.lines)
	}
}

func role(name string, rules []interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "Role",
		"metadata":   map[string]interface{}{"name": name, "namespace": "default"},
		"rules":      rules,
	}}
}

func rule(resource, verb string) map[string]interface{} {
	return map[string]interface{}{"apiGroups": []interface{}{""}, "resources": []interface{}{resource}, "verbs": []interface{}{verb}}
}

// RBAC rules are an atomic list under server-side apply: the reconciler restores the whole list on
// any drift and removes a dropped element. An exclusion INSIDE the list (".rules[0]") cannot be
// honoured element by element, so it excludes the list.
func TestSSA_RulesAreAtomic(t *testing.T) {
	needEnvtest(t)
	cs := clientset(t)
	r := newReconciler(t, role("ssa-role", []interface{}{rule("pods", "get"), rule("configmaps", "list")}), nil)
	reconcileOnce(t, r)
	got, err := cs.RbacV1().Roles("default").Get(context.Background(), "ssa-role", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got.Rules[1].Verbs = []string{"delete"}
	if _, err := cs.RbacV1().Roles("default").Update(context.Background(), got, metav1.UpdateOptions{FieldManager: "someone"}); err != nil {
		t.Fatal(err)
	}
	reconcileOnce(t, r)
	got, _ = cs.RbacV1().Roles("default").Get(context.Background(), "ssa-role", metav1.GetOptions{})
	if len(got.Rules) != 2 || got.Rules[1].Verbs[0] != "list" {
		t.Fatalf("drift inside the list must be restored, got %v", got.Rules)
	}
	r.Resource = *role("ssa-role", []interface{}{rule("pods", "get")})
	reconcileOnce(t, r)
	got, _ = cs.RbacV1().Roles("default").Get(context.Background(), "ssa-role", metav1.GetOptions{})
	if len(got.Rules) != 1 || got.Rules[0].Resources[0] != "pods" {
		t.Fatalf("a dropped element must be removed, got %v", got.Rules)
	}

	// An exclusion inside the atomic list excludes the list: set at creation, then released whole
	// and never applied again, so both elements stay and nobody owns them.
	partial := newReconciler(t, role("ssa-role-partial", []interface{}{rule("pods", "get"), rule("configmaps", "list")}), []string{".rules[0]"})
	reconcileOnce(t, partial)
	reconcileOnce(t, partial)
	got, _ = cs.RbacV1().Roles("default").Get(context.Background(), "ssa-role-partial", metav1.GetOptions{})
	if len(got.Rules) != 2 {
		t.Fatalf("an exclusion inside an atomic list must leave the whole list alone, got %v", got.Rules)
	}
	for _, e := range got.ManagedFields {
		if e.Manager == FieldManager && e.FieldsV1 != nil && strings.Contains(string(e.FieldsV1.Raw), "f:rules") {
			t.Fatalf("the reconciler must not own the released list, managedFields=%v", got.ManagedFields)
		}
	}
}

// An exclusion inside an atomic map: the server tracks nodeSelector as one unit, so the child
// cannot be released alone. Measured in review before the widening: the reconciler kept owning
// the map and the apply without the child deleted it. Now the map is the unit: set once, left alone.
func TestSSA_ExcludedChildOfAtomicMapIsNotDeleted(t *testing.T) {
	needEnvtest(t)
	pod := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]interface{}{"name": "ssa-atomic-map", "namespace": "default"},
		"spec": map[string]interface{}{
			"nodeSelector": map[string]interface{}{"disk": "ssd", "zone": "set-once"},
			"containers":   []interface{}{map[string]interface{}{"name": "c", "image": "example.invalid/image"}},
		},
	}}
	r := newReconciler(t, pod, []string{".spec.nodeSelector.zone"})
	reconcileOnce(t, r)
	reconcileOnce(t, r)
	// a restarted operator: a fresh reconciler with no memory, on an object whose map nobody owns
	// any more; it must learn the unit again rather than send the map without its excluded child
	fresh := newReconciler(t, pod, []string{".spec.nodeSelector.zone"})
	reconcileOnce(t, fresh)
	reconcileOnce(t, fresh)
	got, err := clientset(t).CoreV1().Pods("default").Get(context.Background(), "ssa-atomic-map", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Spec.NodeSelector["zone"] != "set-once" || got.Spec.NodeSelector["disk"] != "ssd" {
		t.Fatalf("the excluded child of an atomic map must survive, got %v", got.Spec.NodeSelector)
	}
	for _, e := range got.ManagedFields {
		if e.Manager == FieldManager && e.FieldsV1 != nil && strings.Contains(string(e.FieldsV1.Raw), "f:nodeSelector") {
			t.Fatalf("the reconciler must release the whole atomic map, managedFields=%v", got.ManagedFields)
		}
	}
}

// With no legacy manager named (the default), a client-side entry is left alone: the reconciler
// takes only what it applies, and a field it does not render stays with its writer.
func TestSSA_NoFoldByDefault(t *testing.T) {
	needEnvtest(t)
	if len(LegacyFieldManagers) != 0 {
		t.Fatalf("the library must fold nothing unless the consumer names a manager, got %v", LegacyFieldManagers)
	}
	cs := clientset(t)
	if _, err := cs.CoreV1().ConfigMaps("default").Create(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "ssa-nofold", Namespace: "default"},
		Data:       map[string]string{"enforced": "one", "foreign": "must-survive"},
	}, metav1.CreateOptions{FieldManager: "manager"}); err != nil {
		t.Fatal(err)
	}
	r := newReconciler(t, configMap("ssa-nofold", map[string]string{"enforced": "two"}, nil), nil)
	reconcileOnce(t, r)
	cm := live(t, "ssa-nofold")
	if cm.Data["foreign"] != "must-survive" || cm.Data["enforced"] != "two" {
		t.Fatalf("without a fold the foreign field survives and the rendered one is enforced, got %v", cm.Data)
	}
	if ownedBy(t, cm, "manager", metav1.ManagedFieldsOperationUpdate) == nil {
		t.Fatalf("the legacy entry must remain, managedFields=%v", cm.ManagedFields)
	}
}

// A ConfigMap key that looks like a negative index: excluding it must round-trip (measured in
// review: the reduced object was built through a pointer that rejected "/data/-1").
func TestSSA_NegativeLookingKeyIsSetOnce(t *testing.T) {
	needEnvtest(t)
	r := newReconciler(t, configMap("ssa-negkey", map[string]string{"-1": "from-template", "a": "1"}, nil), []string{".data['-1']"})
	reconcileOnce(t, r)
	updateAs(t, "someone", func(cm *corev1.ConfigMap) { cm.Data["-1"] = "theirs"; cm.Data["a"] = "drift" })("ssa-negkey")
	reconcileOnce(t, r)
	cm := live(t, "ssa-negkey")
	if cm.Data["-1"] != "theirs" || cm.Data["a"] != "1" {
		t.Fatalf("the key is set once and its sibling enforced, got %v", cm.Data)
	}
}

// An index into a map-keyed list (Deployment containers) releases the whole list: the unit the
// path names cannot be expressed without the list's merge key, so the list is set once. Documented.
func TestSSA_IndexIntoMapKeyedListReleasesTheList(t *testing.T) {
	needEnvtest(t)
	container := func(name, image string) map[string]interface{} {
		return map[string]interface{}{"name": name, "image": image}
	}
	dep := func(images ...string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata":   map[string]interface{}{"name": "ssa-maplist", "namespace": "default"},
			"spec": map[string]interface{}{
				"replicas": int64(1),
				"selector": map[string]interface{}{"matchLabels": map[string]interface{}{"app": "x"}},
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "x"}},
					"spec":     map[string]interface{}{"containers": []interface{}{container("keep", images[0]), container("free", images[1])}},
				},
			},
		}}
	}
	r := newReconciler(t, dep("example.invalid/keep:1", "example.invalid/free:1"), []string{".spec.template.spec.containers[1].image"})
	reconcileOnce(t, r)
	reconcileOnce(t, r)
	got, err := clientset(t).AppsV1().Deployments("default").Get(context.Background(), "ssa-maplist", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Spec.Template.Spec.Containers) != 2 {
		t.Fatalf("the list is set once, nothing deleted, got %v", got.Spec.Template.Spec.Containers)
	}
	for _, e := range got.ManagedFields {
		if e.Manager == FieldManager && e.FieldsV1 != nil && strings.Contains(string(e.FieldsV1.Raw), "f:containers") {
			t.Fatalf("the whole list must be released, managedFields=%v", got.ManagedFields)
		}
	}
	if !owns(ownedIn(t, got.ManagedFields, FieldManager, metav1.ManagedFieldsOperationApply), "spec", "replicas") {
		t.Fatalf("fields outside the list stay owned, managedFields=%v", got.ManagedFields)
	}
}
