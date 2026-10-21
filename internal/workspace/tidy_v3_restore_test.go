// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Spec 031 T046: the restore plan on repositories whose branches were deleted
// by a real tidy v3 apply.

func restorePlan(t *testing.T, o V3RestoreOptions) V3RestoreResult {
	t.Helper()
	return RestoreTidyV3(context.Background(), o)
}

func TestRestorePlanReportsEveryDeletedBranchAsRestorable(t *testing.T) {
	f := newRestoreFix(t, "alpha", "feature/nested", "beta")
	before := f.snapshot()
	r := restorePlan(t, f.options())
	if r.Status != "ok" || r.Command != "tidy-restore" || r.Mode != "plan" || r.PlanID != f.stored.PlanID || r.SchemaVersion != 1 {
		t.Fatalf("wrong plan envelope: %+v", r)
	}
	eligible := v3Eligible(f.stored)
	if len(r.Entries) != 3 || len(eligible) != 3 {
		t.Fatalf("entries: %+v", r.Entries)
	}
	for i, e := range r.Entries {
		c := eligible[i]
		if e.ActionID != c.ActionID || e.Ref != c.Name || e.OID != c.OID || e.RecoveryRef != v3RecoveryFor(f.stored, c.ActionID, c.OID).Name || e.State != "restorable" || e.Reason != nil || e.Class != c.Proof || e.Flags == nil || e.Decision != nil {
			t.Errorf("entry %d: %+v (candidate %+v)", i, e, c)
		}
		if got := f.tips[strings.TrimPrefix(c.Name, "refs/heads/")]; got != e.OID {
			t.Errorf("entry %d tip %s, deleted tip %s", i, e.OID, got)
		}
	}
	if r.Counts != (V3RestoreCounts{Restorable: 3}) {
		t.Errorf("counts: %+v", r.Counts)
	}
	if !reflect.DeepEqual(r.Limits, []string{"tip-and-history-only", "no-config", "no-reflog", "no-worktrees"}) {
		t.Errorf("limits: %v", r.Limits)
	}
	if after := f.snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatal("a restore plan changed the repository or the private store")
	}
}

func TestRestorePlanJSONHasTheContractShape(t *testing.T) {
	f := newRestoreFix(t, "alpha")
	b, err := json.Marshal(restorePlan(t, f.options()))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	keys := func(m map[string]any) []string {
		out := []string{}
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	if got := keys(doc); !reflect.DeepEqual(got, []string{"command", "counts", "diagnostics", "entries", "limits", "mode", "plan_id", "schema_version", "status"}) {
		t.Errorf("top-level keys %v", got)
	}
	entry := doc["entries"].([]any)[0].(map[string]any)
	if got := keys(entry); !reflect.DeepEqual(got, []string{"action_id", "class", "decision", "flags", "oid", "reason", "recovery_ref", "ref", "state"}) {
		t.Errorf("entry keys %v", got)
	}
	if v, ok := entry["reason"]; !ok || v != nil {
		t.Errorf("reason must be an explicit null, got %v", v)
	}
	if v, ok := entry["decision"]; !ok || v != nil {
		t.Errorf("decision must be an explicit null, got %v", v)
	}
	if got := keys(doc["counts"].(map[string]any)); !reflect.DeepEqual(got, []string{"already-present", "journal-incomplete", "name-taken", "recovery-missing", "restorable"}) {
		t.Errorf("count keys %v", got)
	}
}

func TestRestorePlanNeverOverwritesAndAsNamesAnotherBranch(t *testing.T) {
	f := newRestoreFix(t, "alpha", "beta", "gamma")
	head := f.git("rev-parse", "HEAD")
	f.git("update-ref", "refs/heads/alpha", head)          // other OID
	f.git("update-ref", "refs/heads/beta", f.tips["beta"]) // same OID
	before := f.snapshot()
	r := restorePlan(t, f.options())
	if a := entryFor(t, r, "alpha"); a.State != "name-taken" || reasonOf(a) != "" {
		t.Errorf("alpha: %+v", a)
	}
	if b := entryFor(t, r, "beta"); b.State != "already-present" {
		t.Errorf("beta: %+v", b)
	}
	if g := entryFor(t, r, "gamma"); g.State != "restorable" {
		t.Errorf("gamma: %+v", g)
	}
	if r.Counts != (V3RestoreCounts{Restorable: 1, AlreadyPresent: 1, NameTaken: 1}) {
		t.Errorf("counts %+v", r.Counts)
	}
	o := f.options()
	o.Actions, o.As = []string{f.action("alpha")}, "refs/heads/alpha-again"
	as := restorePlan(t, o)
	if as.Status != "ok" || len(as.Entries) != 1 || as.Entries[0].Ref != "refs/heads/alpha-again" || as.Entries[0].State != "restorable" || as.Entries[0].OID != f.tips["alpha"] {
		t.Errorf("--as: %+v", as)
	}
	o.As = "refs/heads/beta"
	if taken := restorePlan(t, o); taken.Entries[0].State != "name-taken" {
		t.Errorf("--as onto an existing branch at another OID: %+v", taken.Entries)
	}
	if after := f.snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatal("planning changed the repository or the store")
	}
}

func TestRestorePlanSymbolicTargetIsNameTakenEvenAtTheSameOID(t *testing.T) {
	f := newRestoreFix(t, "alpha", "beta", "gamma")
	f.git("symbolic-ref", "refs/heads/alpha", "refs/heads/"+f.main)
	// gamma points at a branch holding gamma's own tip: dereferencing would
	// report it as present.
	f.git("update-ref", "refs/heads/holder", f.tips["gamma"])
	f.git("symbolic-ref", "refs/heads/gamma", "refs/heads/holder")
	r := restorePlan(t, f.options())
	for _, name := range []string{"alpha", "gamma"} {
		if e := entryFor(t, r, name); e.State != "name-taken" || reasonOf(e) != "symbolic-reference" {
			t.Errorf("%s: %+v", name, e)
		}
	}
	if e := entryFor(t, r, "beta"); e.State != "restorable" {
		t.Errorf("beta: %+v", e)
	}
}

func TestRestorePlanNamesWhyTheRecoveryIsMissing(t *testing.T) {
	f := newRestoreFix(t, "absent", "retargeted", "symbolic", "pruned", "intact")
	head := f.git("rev-parse", "HEAD")
	f.git("update-ref", "-d", f.recoveryRef("absent"))
	f.git("update-ref", f.recoveryRef("retargeted"), head)
	f.git("update-ref", "-d", f.recoveryRef("symbolic"))
	f.git("update-ref", "refs/heads/holds-symbolic-tip", f.tips["symbolic"])
	f.git("symbolic-ref", f.recoveryRef("symbolic"), "refs/heads/holds-symbolic-tip")
	prunedTip := f.tips["pruned"]
	if err := os.Remove(filepath.Join(f.dir, ".git", "objects", prunedTip[:2], prunedTip[2:])); err != nil {
		t.Fatal(err)
	}
	before := f.snapshot()
	r := restorePlan(t, f.options())
	want := map[string]string{"absent": "recovery-ref-absent", "retargeted": "recovery-ref-retargeted", "symbolic": "recovery-ref-symbolic", "pruned": "object-unreadable"}
	for name, reason := range want {
		if e := entryFor(t, r, name); e.State != "recovery-missing" || reasonOf(e) != reason {
			t.Errorf("%s: %+v", name, e)
		}
	}
	if e := entryFor(t, r, "intact"); e.State != "restorable" {
		t.Errorf("intact: %+v", e)
	}
	if r.Counts != (V3RestoreCounts{Restorable: 1, RecoveryMissing: 4}) {
		t.Errorf("counts %+v", r.Counts)
	}
	if after := f.snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatal("planning wrote while the recovery was missing")
	}
}

func TestRestorePlanOfARecloneFindsNoRecoveryRefs(t *testing.T) {
	f := newRestoreFix(t, "alpha", "beta")
	clone := filepath.Join(gitTempDir(t), "clone")
	f.git("clone", "-q", "--no-local", f.dir, clone)
	o := f.options()
	o.Path = clone
	r := restorePlan(t, o)
	if r.Status != "ok" || r.Counts != (V3RestoreCounts{RecoveryMissing: 2}) {
		t.Fatalf("%+v", r)
	}
	for _, e := range r.Entries {
		if e.State != "recovery-missing" || reasonOf(e) != "recovery-ref-absent" {
			t.Errorf("%+v", e)
		}
	}
}

// The journal of a stopped apply lists only the entries with a confirmed
// recovery as restorable; the rest are never replayed.
func TestRestorePlanOfAnIncompleteJournalListsTheRestAsIncomplete(t *testing.T) {
	f := newPartialRestoreFix(t, "first", "second", "third")
	before := f.snapshot()
	r := restorePlan(t, f.options())
	if r.Status != "ok" || len(r.Entries) != 3 {
		t.Fatalf("%+v", r)
	}
	if e := entryFor(t, r, "first"); e.State != "restorable" {
		t.Errorf("first: %+v", e)
	}
	if e := entryFor(t, r, "second"); e.State != "already-present" {
		t.Errorf("second (recovery confirmed, branch still there): %+v", e)
	}
	if e := entryFor(t, r, "third"); e.State != "journal-incomplete" || reasonOf(e) != "no-confirmed-recovery" {
		t.Errorf("third: %+v", e)
	}
	if after := f.snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatal("planning replayed or rewrote an incomplete journal")
	}
}

func TestRestorePlanRefusesJournalsOfOtherModes(t *testing.T) {
	t.Run("tracking-refs", func(t *testing.T) {
		f := fixtureTidyV3TrackingApply(t, 1)
		if r := f.coordinator.apply(context.Background(), trackingApplyOptions(f.plan)); r.Status != "ok" {
			t.Fatalf("%+v", r.Diagnostics)
		}
		requireUnsupportedMode(t, f.dir, f.plan)
	})
	t.Run("retirement", func(t *testing.T) {
		o, _ := tidyV3LegacyFixture(t, false)
		o.StateDir = privateDir(t)
		c := defaultTidyV3RetirementCoordinator()
		planned := c.plan(context.Background(), o)
		if planned.Status != "ok" || planned.Plan == nil {
			t.Fatalf("%+v", planned.Diagnostics)
		}
		p := *planned.Plan
		if r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees}); r.Status != "ok" {
			t.Fatalf("%+v", r.Diagnostics)
		}
		requireUnsupportedMode(t, o.Path, p)
	})
}

func requireUnsupportedMode(t *testing.T, repo string, p V3Plan) {
	t.Helper()
	before := treeBytes(t, p.StateDir)
	r := restorePlan(t, V3RestoreOptions{Path: repo, StateDir: p.StateDir, PlanID: p.PlanID})
	if r.Status != "refused" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "unsupported-mode" || len(r.Entries) != 0 {
		t.Fatalf("%s journal: %+v", p.Mode, r)
	}
	if !reflect.DeepEqual(before, treeBytes(t, p.StateDir)) {
		t.Fatal("the refusal wrote to the store")
	}
}

func TestRestorePlanRefusesUnknownPlanActionAndDamagedJournal(t *testing.T) {
	f := newRestoreFix(t, "alpha")
	code := func(r V3RestoreResult) string {
		if r.Status != "refused" || len(r.Diagnostics) != 1 || len(r.Entries) != 0 {
			t.Fatalf("not a single refusal: %+v", r)
		}
		return r.Diagnostics[0].Code
	}
	o := f.options()
	o.PlanID = "no-such-plan"
	if got := code(restorePlan(t, o)); got != "plan-not-found" {
		t.Errorf("unknown plan: %s", got)
	}
	o = f.options()
	o.Actions = []string{"no-such-action"}
	if got := code(restorePlan(t, o)); got != "action-not-found" {
		t.Errorf("unknown action: %s", got)
	}
	if err := os.WriteFile(f.journalPath(), []byte(`{"schema_version":3,`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := code(restorePlan(t, f.options())); got != "journal-unreadable" {
		t.Errorf("damaged journal: %s", got)
	}
}

func TestRestorePlanScopesToTheNamedActionsInJournalOrder(t *testing.T) {
	f := newRestoreFix(t, "alpha", "beta", "gamma")
	o := f.options()
	o.Actions = []string{f.action("gamma"), f.action("alpha")}
	r := restorePlan(t, o)
	if len(r.Entries) != 2 || r.Entries[0].Ref != "refs/heads/alpha" || r.Entries[1].Ref != "refs/heads/gamma" {
		t.Fatalf("%+v", r.Entries)
	}
	if r.Counts != (V3RestoreCounts{Restorable: 2}) {
		t.Errorf("counts cover only the scope: %+v", r.Counts)
	}
}

func TestRestorePlanWithNothingRestorableIsStillOk(t *testing.T) {
	f := newRestoreFix(t, "alpha")
	f.git("update-ref", "refs/heads/alpha", f.tips["alpha"])
	r := restorePlan(t, f.options())
	if r.Status != "ok" || r.Counts != (V3RestoreCounts{AlreadyPresent: 1}) {
		t.Fatalf("%+v", r)
	}
}

// A name from a journal is data: one that is not a valid branch ref name is
// refused for its entry and never reaches a Git argument.
func TestRestorePlanRefusesHostileJournalNamesWithoutPassingThemToGit(t *testing.T) {
	f := newRestoreFix(t, "-dash", "fine")
	var argv []string
	c := defaultTidyV3RestoreCoordinator()
	c.observeArgs = func(args []string) { argv = append(argv, args...) }
	r := c.restore(context.Background(), f.options())
	if e := entryFor(t, r, "-dash"); e.State != "journal-incomplete" || reasonOf(e) != "invalid-ref-name" {
		t.Errorf("-dash: %+v", e)
	}
	if e := entryFor(t, r, "fine"); e.State != "restorable" {
		t.Errorf("fine: %+v", e)
	}
	if len(argv) == 0 {
		t.Fatal("the recorder saw no Git argument")
	}
	for _, a := range argv {
		if strings.Contains(a, "-dash") {
			t.Errorf("the hostile name reached Git: %q", a)
		}
	}
}

func TestRestoreRefNameValidation(t *testing.T) {
	valid := []string{"refs/heads/main", "refs/heads/feature/x", "refs/heads/a.b", "refs/heads/ünï", "refs/heads/" + strings.Repeat("a", 100)}
	invalid := map[string]string{
		"empty": "", "no prefix": "main", "tag": "refs/tags/x", "only the prefix": "refs/heads/", "leading dash": "refs/heads/-x",
		"double dot": "refs/heads/a..b", "control": "refs/heads/a\x01b", "delete": "refs/heads/a\x7fb", "newline": "refs/heads/a\nb",
		"space": "refs/heads/a b", "tilde": "refs/heads/a~1", "caret": "refs/heads/a^", "colon": "refs/heads/a:b", "question": "refs/heads/a?", "star": "refs/heads/a*", "bracket": "refs/heads/a[", "backslash": `refs/heads/a\b`,
		"at brace": "refs/heads/a@{1}", "lock": "refs/heads/a.lock", "lock component": "refs/heads/a.lock/b", "trailing slash": "refs/heads/a/", "double slash": "refs/heads/a//b",
		"trailing dot": "refs/heads/a.", "dot component": "refs/heads/.hidden", "nul": "refs/heads/a\x00b", "too long": "refs/heads/" + strings.Repeat("a", 4096), "invalid utf8": "refs/heads/\xff",
	}
	for _, name := range valid {
		if !validRestoreRefName(name) {
			t.Errorf("rejected %q", name)
		}
	}
	for why, name := range invalid {
		if validRestoreRefName(name) {
			t.Errorf("accepted (%s) %q", why, name)
		}
	}
}

func TestRestoreOptionsAreCheckedBeforeAnyNativeWork(t *testing.T) {
	good := V3RestoreOptions{Path: "/repo", StateDir: "/state", PlanID: "plan", Actions: []string{"a"}}
	cases := map[string]func(*V3RestoreOptions){
		"duplicate action":            func(o *V3RestoreOptions) { o.Actions = []string{"a", "a"} },
		"as without an action":        func(o *V3RestoreOptions) { o.Actions, o.As = nil, "refs/heads/x" },
		"as with two actions":         func(o *V3RestoreOptions) { o.Actions, o.As = []string{"a", "b"}, "refs/heads/x" },
		"as outside heads":            func(o *V3RestoreOptions) { o.As = "refs/tags/x" },
		"as not a ref name":           func(o *V3RestoreOptions) { o.As = "refs/heads/a..b" },
		"as with a leading dash":      func(o *V3RestoreOptions) { o.As = "refs/heads/-x" },
		"authorize without apply":     func(o *V3RestoreOptions) { o.Authorize = true },
		"malformed plan":              func(o *V3RestoreOptions) { o.PlanID = "a b" },
		"malformed action":            func(o *V3RestoreOptions) { o.Actions = []string{"a/b"} },
		"relative state directory":    func(o *V3RestoreOptions) { o.StateDir = "state" },
		"empty plan":                  func(o *V3RestoreOptions) { o.PlanID = "" },
		"apply without authorization": func(o *V3RestoreOptions) { o.Apply = true },
	}
	if err := validateV3RestoreOptions(good); err != nil {
		t.Fatalf("good options refused: %v", err)
	}
	for name, mutate := range cases {
		o := good
		mutate(&o)
		if err := validateV3RestoreOptions(o); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if r := RestoreTidyV3(context.Background(), V3RestoreOptions{Path: "/nonexistent", StateDir: "/state", PlanID: "p", Actions: []string{"a", "a"}}); r.Status != "refused" || r.Diagnostics[0].Code != "invalid-options" {
		t.Errorf("%+v", r)
	}
}
