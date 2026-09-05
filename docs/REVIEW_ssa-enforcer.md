# Review — the server-side-apply enforcer (branch feat/ssa-enforcer)

Adversarial second-opinion passes, 2026-09-05, on `LockedResourceReconciler` enforcing with server-side apply
(commit 1bab849 on top of upstream master 09e27b5, then the fixes below). Reviewers: Codex (gpt-5.6-sol, xhigh)
and Cursor (Grok 4.6 high fast, ask mode). Neither could run a process or create a file this run, so every verdict
is traced from source; every runtime claim was measured here against envtest (a real API server) and, for the
operator, on a live cluster. Briefs and raw outputs: session scratchpad `adv/review_brief_ssa*.md`,
`adv/review_{codex,cursor}_ssa*.txt`.

## Contract under review

Every reconcile GETs the live object. Absent: the whole rendered object is applied under `FieldManager` with force
(excluded paths included: "set once"). Present: the reconciler releases its ownership of every excluded path
(and, if the consumer named legacy managers, folds their client-side entries into `FieldManager` with client-go's
csaupgrade) with one JSON patch sent only when something changes, then applies the rendered object minus the
excluded paths. Consequences: drift on an owned field restored; a field the template stops rendering removed; an
excluded field left as it is, owned by nobody; other managers' fields untouched; a no-op apply writes nothing.

## First pass (1bab849)

| Claim | Cursor | Codex | Decision |
|---|---|---|---|
| C1 release-then-apply safe under concurrency | CONFIRMED | CONFIRMED | — (the patch replaces resourceVersion: a concurrent write conflicts; force apply only takes fields it sends) |
| C2 excluded-path release is complete | REFUTED | REFUTED | **Accepted, two defects**: a quoted numeric key (`.data['0']`) was taken for a list index and released all of `data` (fix: `FieldPath` returns an Index flag per segment; measured with a ConfigMap key "0"); a child of an atomic map (a Pod's `nodeSelector.zone`) cannot be released alone, the map stayed owned and the apply deleted the child (Codex; fix: each exclusion widens to the unit the server tracks when nothing is owned at or below it but a strict ancestor is, and that unit is left out of the apply; measured with a Pod) |
| C3 creation "sets once", no window | CONFIRMED | REFUTED | **Accepted in Codex's form**: `.rules[0]` released the whole list and then applied the list without element 0, deleting it (as the merge patch did). With widening the list is the unit: released and not applied; both elements stay (measured with a Role). The cost, stated in the code: a non-excluded sibling inside the unit is no longer enforced after creation |
| C4 legacy fold cannot remove another actor's field | REFUTED | REFUTED | **Accepted as a policy, not as code the reviewers proposed**: a whole entry of the legacy name is folded, so a same-named foreign actor's field would go. Cursor's "fold only what the template renders" and Codex's "remove the fold" both give up removing stale fields on legacy objects, the failure #194 reported. Decision: the fold is opt-in (`LegacyFieldManagers` empty by default; the operator names "manager" because it knows its history), the residual risk is written next to the variable |
| C5 lists are atomic; drift restored, dropped element removed | PLAUSIBLE | REFUTED (unmeasured) | **Measured** with a Role: drift inside the list restored, a dropped element removed |
| C6 status/conditions unchanged; "equal" log exact | REFUTED | REFUTED | **Accepted**: conditions unchanged; the "equal" verdict compared the apply's result with the first GET, so a release patch followed by a no-op apply logged "NOT equal". `releaseOwnership` returns the object the apply races. Test with a capturing log sink |
| C7 dependencies clean | PLAUSIBLE | PLAUSIBLE | `go mod tidy` clean: jsondiff gone, structured-merge-diff v4.2.3 direct, one copy |
| C8 tests not vacuous | CONFIRMED | PLAUSIBLE | — |

**Found here, fixed:** a locked resource's identity was its object alone, so editing a template's excludedPaths on
a CR changed nothing until the operator restarted (measured on a cluster: `.metadata` removed from a
NamespaceConfig, a tampered label not restored). `GetKey` now carries the sorted excluded paths; a changed key
restarts the manager without deleting anything (the deletion set keys on kind, namespace and name).

**Rejected:** Codex's second GET after the release plus a resourceVersion precondition on the apply (the release
returns the patched object; server-side apply cannot delete another manager's field whatever interleaves, and a
precondition only adds conflicts under churn).

## Measured on a live cluster (operator build against this library)

Every object the operator had created carried one entry, manager `manager`, operation Update. After one pass
with `LegacyFieldManagers = ["manager"]`: that entry gone, `lockedresourcecontroller`/Apply owning `roleRef` and
`subjects` only, all 10 labels and the annotations kept (owned by nobody), a hand-added subject removed within one
reconcile, a hand-added label kept with its own manager, a forced reconcile afterwards changing no
resourceVersion, 40 one-time ownership patches, every CR ReconcileSuccess. A local build named otherwise did not
fold (the user-agent-derived default), which is why the operator names the manager explicitly and the library's
default is empty. With `.metadata` removed from a NamespaceConfig's excludedPaths (after the `GetKey` fix): the
reconciler took ownership of the rendered labels and annotations, a tampered rendered label was restored, a
foreign label left alone.

## Second pass (f86b9a8)

Cursor and Codex again traced without a process; every runtime claim was measured here against envtest and on
the live cluster with the operator build (commit 0a6d81b carries the fixes).

| Claim | Cursor | Codex | Decision |
|---|---|---|---|
| C1 widening is correct and minimal | REFUTED | PLAUSIBLE (same reasoning) | **Accepted, the most important finding of the pass, both reviewers**: the widening read live ownership as if it were schema. After a granular exclusion was released, its parent (a member because of the `.` marker) looked like an atomic ancestor and the next reconcile widened to the whole map: a ConfigMap's `data` stopped being enforced. And after an atomic map was released, nothing recorded that it was atomic, so a later reconcile, or a restarted operator, would send the map without its excluded child and the server would delete the child. Fix: the unit of each excluded path is learned from a dry-run apply of the whole rendered object (the managedFields the server would record, schema-shaped: a granular map lists its keys, an atomic map or list is one childless member), the deepest owned ancestor when it has no children, else the path itself; computed once per reconciler. Measured: the granular sibling stays enforced through further reconciles; the atomic map survives a fresh reconciler with no memory. An index into a map-keyed list (Deployment containers) still releases the whole list, because the merge key is schema the reconciler does not have; documented and measured (nothing deleted, fields outside the list stay owned). Cursor's index-to-key resolution rejected: it needs the merge key |
| C2 `desired` round-trips every pointer | REFUTED | CONFIRMED | **Accepted (Cursor)**: a ConfigMap key named `-1` was rejected on the way back through a pointer as a negative index. The reduced object is now built by deleting units by name; no pointer round trip exists. Measured with a key `-1` set once and its sibling enforced |
| C3 creation sets once; no window | CONFIRMED (race) | REFUTED (third reconcile) | **Accepted in Codex's form**: same root cause as C1, same fix; the documented cost (a non-excluded sibling inside a released unit is no longer enforced) is stated on `ownershipUnits` |
| C4 `GetKey` restarts without deleting | CONFIRMED | CONFIRMED | — (deletion keys on group, kind, namespace, name; the restart passes `deleteResources=false`) |
| C5 the fold is opt-in and happens once | CONFIRMED | PLAUSIBLE | **Measured**: the second reconcile after a fold writes nothing (resourceVersion unchanged); test added |
| C6 `FieldPath` grammar | CONFIRMED | CONFIRMED | `/data/0` in pointer form is an index and releases all of `data`: use the dotted or quoted form for a numeric key; documented |

**Volunteered by Codex, recorded, no change:** `StoppableManager.Stop` cancels and returns without waiting for
in-flight reconciles, so a restart may briefly run the old and new reconcilers together under one manager name.
Upstream behaviour, unchanged by this branch; the two converge on the same object.

**Live, with the operator build against 0a6d81b:** with `.metadata` excluded (the chart's CRs today) a hand-added
subject is removed and a tampered label stays, set once; with `.metadata` removed from a NamespaceConfig a
tampered rendered label is restored and a foreign label kept; the dry-run apply passes the cluster's admission
webhooks; every CR ReconcileSuccess.

## Third pass (0a6d81b), Cursor

Every claim confirmed from source: the dry-run apply runs the same field-manager pipeline with persistence skipped;
the unit rule yields the path for granular maps, the atomic parent for atomic maps and structs, the list for an
index, and never widens past a childless member; the cache is populated only after the object exists and the
enforcer restarts the reconciler on any key change; `RemoveNestedField` expresses every unit this code emits; a
failing dry run leaves the cache empty and the reconcile retries. Two of the five review-driven tests were noted
as not failing on the previous head (the map-keyed list and the fold-once tests): true, they document behaviour
rather than fix it; kept as documentation. One operational fact recorded in the code: an admission webhook that
declares side effects rejects dry-run requests, and then the reconcile fails visibly and retries.

A first-principles reviewer (Fable 5.1) was also asked to break the contract with experiments; it was cut off by
the session's rate limit before reporting.
