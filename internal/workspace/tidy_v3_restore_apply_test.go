// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// Spec 031 T048: the restore apply creates local branches by compare-and-swap
// and records an intent before and an outcome after each creation.

func applyRestore(t *testing.T, c tidyV3RestoreCoordinator, o V3RestoreOptions) V3RestoreResult {
	t.Helper()
	return c.restore(context.Background(), o)
}

func outcomeFor(t *testing.T, r V3RestoreResult, name string) string {
	t.Helper()
	return entryFor(t, r, name).Outcome
}

func TestRestoreApplyCreatesTheBranchesAndTouchesNothingElse(t *testing.T) {
	f := newRestoreFix(t, "alpha", "feature/nested", "beta")
	refsBefore := f.git("for-each-ref", "--format=%(refname) %(objectname) %(symref)")
	configBefore := f.snapshot()["config"]
	worktreeBefore := f.worktreeBytes()
	indexBefore := treeBytes(t, filepath.Join(f.dir, ".git"))["index"]
	journalBefore := treeBytes(t, filepath.Join(f.state, "tidy-v3-operations"))
	plansBefore := treeBytes(t, filepath.Join(f.state, "tidy-v3-plans"))
	logsBefore := treeBytes(t, filepath.Join(f.dir, ".git", "logs"))
	r := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	if r.Status != "ok" || r.Mode != "apply" || len(r.Entries) != 3 {
		t.Fatalf("%+v", r)
	}
	for _, name := range []string{"alpha", "feature/nested", "beta"} {
		e := entryFor(t, r, name)
		if e.Outcome != "created" || e.RestoreJournal == nil || e.RestoreJournal.Intent == "" || e.RestoreJournal.Outcome == "" {
			t.Errorf("%s: %+v", name, e)
		}
		if got := f.git("rev-parse", "refs/heads/"+name); got != f.tips[name] {
			t.Errorf("%s restored at %s, deleted at %s", name, got, f.tips[name])
		}
		wantMessage := "stw restore " + f.stored.PlanID + " " + f.action(name)
		if log := f.reflog(name); strings.Count(log, "\n") != 1 || !strings.HasSuffix(strings.TrimSpace(log), "\t"+wantMessage) {
			t.Errorf("%s reflog %q, want message %q", name, log, wantMessage)
		}
	}
	// Only the new branches and their reflogs differ.
	var added []string
	for _, name := range []string{"alpha", "feature/nested", "beta"} {
		added = append(added, "refs/heads/"+name+" "+f.tips[name]+" ")
	}
	lines := append(strings.Split(refsBefore, "\n"), added...)
	sort.Strings(lines)
	want := strings.Join(lines, "\n")
	got := strings.Split(f.git("for-each-ref", "--format=%(refname) %(objectname) %(symref)"), "\n")
	sort.Strings(got)
	if strings.TrimSpace(strings.Join(got, "\n")) != strings.TrimSpace(want) {
		t.Errorf("refs\n%s\nwant\n%s", strings.Join(got, "\n"), want)
	}
	if f.snapshot()["config"] != configBefore || !reflect.DeepEqual(worktreeBefore, f.worktreeBytes()) || treeBytes(t, filepath.Join(f.dir, ".git"))["index"] != indexBefore {
		t.Error("configuration, worktree or index changed")
	}
	if !reflect.DeepEqual(journalBefore, treeBytes(t, filepath.Join(f.state, "tidy-v3-operations"))) || !reflect.DeepEqual(plansBefore, treeBytes(t, filepath.Join(f.state, "tidy-v3-plans"))) {
		t.Error("the V3Journal or the plan was modified")
	}
	logs := treeBytes(t, filepath.Join(f.dir, ".git", "logs"))
	for path, content := range logsBefore {
		if logs[path] != content && !strings.HasPrefix(path, filepath.Join("refs", "heads")) {
			t.Errorf("reflog %s changed", path)
		}
	}
	for _, name := range []string{"alpha", "feature/nested", "beta"} {
		if f.git("rev-parse", f.recoveryRef(name)) != f.tips[name] {
			t.Errorf("the recovery ref of %s was not kept", name)
		}
	}
}

func TestRestoreApplyRecordsExactlyIntentAndOutcomeAppendOnly(t *testing.T) {
	f := newRestoreFix(t, "alpha", "beta")
	c := defaultTidyV3RestoreCoordinator()
	o := f.applyOptions()
	o.Actions = []string{f.action("alpha")}
	first := applyRestore(t, c, o)
	afterFirst := f.restoreRecords()
	if first.Status != "ok" || len(afterFirst) != 2 {
		t.Fatalf("%+v %v", first, sortedKeys(afterFirst))
	}
	o.Actions = []string{f.action("beta")}
	second := applyRestore(t, c, o)
	if second.Status != "ok" {
		t.Fatalf("%+v", second)
	}
	records := f.restoreRecords()
	if len(records) != 4 {
		t.Fatalf("records: %v", sortedKeys(records))
	}
	for name, content := range afterFirst {
		if records[name] != content {
			t.Errorf("record %s was rewritten", name)
		}
	}
	fields := func(raw string) map[string]any {
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	keys := func(m map[string]any) []string {
		out := []string{}
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	intents, outcomes := 0, 0
	for name, raw := range records {
		m := fields(raw)
		switch m["kind"] {
		case "intent":
			intents++
			if got := keys(m); !reflect.DeepEqual(got, []string{"action_id", "at", "by", "kind", "oid", "plan_id", "ref", "schema_version"}) {
				t.Errorf("intent %s fields %v", name, got)
			}
		case "outcome":
			outcomes++
			if got := keys(m); !reflect.DeepEqual(got, []string{"action_id", "at", "by", "kind", "oid", "outcome", "plan_id", "ref", "schema_version"}) || m["outcome"] != "created" {
				t.Errorf("outcome %s fields %v %v", name, got, m["outcome"])
			}
		default:
			t.Errorf("record %s kind %v", name, m["kind"])
		}
		if m["plan_id"] != f.stored.PlanID || m["by"] == "" {
			t.Errorf("record %s: %v", name, m)
		}
		if _, err := time.Parse(time.RFC3339Nano, m["at"].(string)); err != nil {
			t.Errorf("record %s time: %v", name, err)
		}
	}
	if intents != 2 || outcomes != 2 {
		t.Errorf("intents %d outcomes %d", intents, outcomes)
	}
}

func TestRestoreApplyRerunIsIdempotent(t *testing.T) {
	f := newRestoreFix(t, "alpha", "beta")
	c := defaultTidyV3RestoreCoordinator()
	if r := applyRestore(t, c, f.applyOptions()); r.Status != "ok" {
		t.Fatalf("%+v", r)
	}
	before, snapshot := f.restoreRecords(), f.snapshot()
	r := applyRestore(t, c, f.applyOptions())
	if r.Status != "ok" || outcomeFor(t, r, "alpha") != "already-present" || outcomeFor(t, r, "beta") != "already-present" {
		t.Fatalf("%+v", r)
	}
	if !reflect.DeepEqual(before, f.restoreRecords()) || !reflect.DeepEqual(snapshot, f.snapshot()) {
		t.Fatal("the rerun wrote a record or changed the repository")
	}
}

func TestRestoreApplyNeverOverwritesAnExistingBranch(t *testing.T) {
	f := newRestoreFix(t, "taken", "free")
	head := f.git("rev-parse", "HEAD")
	f.git("update-ref", "refs/heads/taken", head)
	r := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	if r.Status != "partial" || outcomeFor(t, r, "taken") != "name-taken" || outcomeFor(t, r, "free") != "created" {
		t.Fatalf("%+v", r)
	}
	if got := f.git("rev-parse", "refs/heads/taken"); got != head {
		t.Fatalf("an existing branch was overwritten: %s", got)
	}
	if len(f.restoreRecords()) != 2 {
		t.Errorf("an entry without an effect wrote a record: %v", sortedKeys(f.restoreRecords()))
	}
}

func TestRestoreApplyLostRaceFailsClosedAsNameTaken(t *testing.T) {
	f := newRestoreFix(t, "alpha")
	head := f.git("rev-parse", "HEAD")
	c := defaultTidyV3RestoreCoordinator()
	c.beforeCAS = func(V3RestoreEntry) { f.git("update-ref", "refs/heads/alpha", head) }
	r := applyRestore(t, c, f.applyOptions())
	if r.Status != "refused" || outcomeFor(t, r, "alpha") != "name-taken" {
		t.Fatalf("%+v", r)
	}
	if got := f.git("rev-parse", "refs/heads/alpha"); got != head {
		t.Fatalf("the racing branch was overwritten: %s", got)
	}
	records := f.restoreRecords()
	if len(records) != 2 {
		t.Fatalf("intent and outcome expected: %v", sortedKeys(records))
	}
	found := false
	for _, raw := range records {
		found = found || strings.Contains(raw, `"outcome":"name-taken"`)
	}
	if !found {
		t.Error("the lost race was not recorded as name-taken")
	}
}

func TestRestoreApplySymbolicTargetCreatedBeforeTheEffectIsNotFollowed(t *testing.T) {
	f := newRestoreFix(t, "alpha")
	c := defaultTidyV3RestoreCoordinator()
	c.beforeCAS = func(V3RestoreEntry) { f.git("symbolic-ref", "refs/heads/alpha", "refs/heads/elsewhere") }
	r := applyRestore(t, c, f.applyOptions())
	if r.Status != "refused" || outcomeFor(t, r, "alpha") != "symbolic-reference" {
		t.Fatalf("%+v", r)
	}
	if out, err := f.tryGit("rev-parse", "--verify", "-q", "refs/heads/elsewhere"); err == nil {
		t.Fatalf("the symbolic reference was followed and created %s", out)
	}
	if got := f.git("symbolic-ref", "refs/heads/alpha"); got != "refs/heads/elsewhere" {
		t.Fatalf("the symbolic reference changed: %s", got)
	}
}

func TestRestoreApplyReevaluatesWhatChangedSincePlanning(t *testing.T) {
	t.Run("a branch created after the plan", func(t *testing.T) {
		f := newRestoreFix(t, "alpha")
		head := f.git("rev-parse", "HEAD")
		plan := RestoreTidyV3(context.Background(), f.options())
		if plan.Counts.Restorable != 1 {
			t.Fatalf("%+v", plan)
		}
		f.git("update-ref", "refs/heads/alpha", head)
		r := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
		if r.Status != "refused" || outcomeFor(t, r, "alpha") != "name-taken" || len(f.restoreRecords()) != 0 {
			t.Fatalf("%+v records=%v", r, sortedKeys(f.restoreRecords()))
		}
	})
	for name, target := range map[string]string{"a symbolic reference created after the plan": "main", "a dangling symbolic reference created after the plan": "elsewhere"} {
		t.Run(name, func(t *testing.T) {
			f := newRestoreFix(t, "alpha")
			f.git("symbolic-ref", "refs/heads/alpha", "refs/heads/"+strings.ReplaceAll(target, "main", f.main))
			r := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
			if r.Status != "refused" || outcomeFor(t, r, "alpha") != "symbolic-reference" || len(f.restoreRecords()) != 0 {
				t.Fatalf("%+v", r)
			}
			if _, err := f.tryGit("rev-parse", "--verify", "-q", "refs/heads/elsewhere"); err == nil {
				t.Fatal("the symbolic reference was followed")
			}
		})
	}
	t.Run("a recovery ref retargeted after the entries were evaluated", func(t *testing.T) {
		f := newRestoreFix(t, "alpha", "beta")
		head := f.git("rev-parse", "HEAD")
		c := defaultTidyV3RestoreCoordinator()
		c.afterEvaluate = func() { f.git("update-ref", f.recoveryRef("alpha"), head) }
		r := applyRestore(t, c, f.applyOptions())
		a := entryFor(t, r, "alpha")
		if r.Status != "partial" || a.Outcome != "recovery-missing" || a.State != "recovery-missing" || reasonOf(a) != "recovery-ref-retargeted" || outcomeFor(t, r, "beta") != "created" {
			t.Fatalf("%+v", r)
		}
		if out, err := f.tryGit("rev-parse", "--verify", "-q", "refs/heads/alpha"); err == nil {
			t.Fatalf("restored from a retargeted recovery ref: %s", out)
		}
		if len(f.restoreRecords()) != 2 {
			t.Errorf("records: %v", sortedKeys(f.restoreRecords()))
		}
	})
}

func TestRestoreApplyNeverRestoresFromARecoveryRefThatDisagreesWithTheJournal(t *testing.T) {
	f := newRestoreFix(t, "alpha")
	f.git("update-ref", f.recoveryRef("alpha"), f.git("rev-parse", "HEAD"))
	r := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	if r.Status != "refused" || outcomeFor(t, r, "alpha") != "recovery-missing" {
		t.Fatalf("%+v", r)
	}
	if _, err := f.tryGit("rev-parse", "--verify", "-q", "refs/heads/alpha"); err == nil {
		t.Fatal("a branch was created from the retargeted recovery ref")
	}
}

func TestRestoreApplyCrashBetweenIntentAndOutcome(t *testing.T) {
	crash := errors.New("simulated crash")
	t.Run("before the effect: the rerun retries", func(t *testing.T) {
		f := newRestoreFix(t, "alpha")
		c := defaultTidyV3RestoreCoordinator()
		c.afterIntent = func(V3RestoreEntry) error { return crash }
		r := applyRestore(t, c, f.applyOptions())
		if r.Status != "error" || len(f.restoreRecords()) != 1 {
			t.Fatalf("%+v %v", r, sortedKeys(f.restoreRecords()))
		}
		if _, err := f.tryGit("rev-parse", "--verify", "-q", "refs/heads/alpha"); err == nil {
			t.Fatal("the crashed run created the branch")
		}
		again := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
		if again.Status != "ok" || outcomeFor(t, again, "alpha") != "created" || f.git("rev-parse", "refs/heads/alpha") != f.tips["alpha"] {
			t.Fatalf("%+v", again)
		}
		if len(f.restoreRecords()) != 3 {
			t.Errorf("a retry records a new intent and its outcome: %v", sortedKeys(f.restoreRecords()))
		}
	})
	t.Run("after the effect: the rerun appends created-recovered once", func(t *testing.T) {
		f := newRestoreFix(t, "alpha")
		c := defaultTidyV3RestoreCoordinator()
		c.afterCAS = func(V3RestoreEntry) error { return crash }
		if r := applyRestore(t, c, f.applyOptions()); r.Status == "ok" {
			t.Fatalf("%+v", r)
		}
		first := f.restoreRecords()
		if len(first) != 1 || f.git("rev-parse", "refs/heads/alpha") != f.tips["alpha"] {
			t.Fatalf("records %v", sortedKeys(first))
		}
		again := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
		if again.Status != "ok" || outcomeFor(t, again, "alpha") != "created-recovered" {
			t.Fatalf("%+v", again)
		}
		records := f.restoreRecords()
		if len(records) != 2 {
			t.Fatalf("%v", sortedKeys(records))
		}
		for name, content := range first {
			if records[name] != content {
				t.Errorf("record %s was rewritten", name)
			}
		}
		third := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
		if outcomeFor(t, third, "alpha") != "already-present" || len(f.restoreRecords()) != 2 {
			t.Fatalf("a completed recovery was recorded again: %+v", third)
		}
	})
	t.Run("another actor created the branch at the same OID after the crash", func(t *testing.T) {
		f := newRestoreFix(t, "alpha")
		c := defaultTidyV3RestoreCoordinator()
		c.afterIntent = func(V3RestoreEntry) error { return crash }
		applyRestore(t, c, f.applyOptions())
		f.git("update-ref", "refs/heads/alpha", f.tips["alpha"])
		again := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
		if again.Status != "ok" || outcomeFor(t, again, "alpha") != "created-recovered" {
			t.Fatalf("%+v", again)
		}
	})
	t.Run("another actor created the branch at another OID after the crash", func(t *testing.T) {
		f := newRestoreFix(t, "alpha")
		head := f.git("rev-parse", "HEAD")
		c := defaultTidyV3RestoreCoordinator()
		c.afterIntent = func(V3RestoreEntry) error { return crash }
		applyRestore(t, c, f.applyOptions())
		f.git("update-ref", "refs/heads/alpha", head)
		before := f.restoreRecords()
		again := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
		if again.Status != "refused" || outcomeFor(t, again, "alpha") != "name-taken" || f.git("rev-parse", "refs/heads/alpha") != head || !reflect.DeepEqual(before, f.restoreRecords()) {
			t.Fatalf("%+v", again)
		}
	})
}

func TestRestoreApplyFailedIntentWriteTouchesNoReference(t *testing.T) {
	f := newRestoreFix(t, "alpha")
	c := defaultTidyV3RestoreCoordinator()
	c.writeRecord = func(*store, string, restoreRecord) error { return errors.New("disk full") }
	r := applyRestore(t, c, f.applyOptions())
	a := entryFor(t, r, "alpha")
	if r.Status != "error" || a.Outcome != "error" || a.RestoreJournal != nil {
		t.Fatalf("%+v", r)
	}
	if _, err := f.tryGit("rev-parse", "--verify", "-q", "refs/heads/alpha"); err == nil {
		t.Fatal("a reference was created although its intent was not durable")
	}
}

func TestRestoreApplyWithoutAuthorizationRefusesBeforeAnyEffect(t *testing.T) {
	f := newRestoreFix(t, "alpha")
	before := f.snapshot()
	o := f.options()
	o.Apply = true
	r := applyRestore(t, defaultTidyV3RestoreCoordinator(), o)
	if r.Status != "refused" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "authorization-required" {
		t.Fatalf("%+v", r)
	}
	if !reflect.DeepEqual(before, f.snapshot()) {
		t.Fatal("an unauthorized apply changed state")
	}
	if _, err := os.Lstat(filepath.Join(f.state, "tidy-v3-restores")); !os.IsNotExist(err) {
		t.Fatal("an unauthorized apply created the restore namespace")
	}
}

func TestRestoreApplyRefusesWhereAReferenceTransactionHookWouldRun(t *testing.T) {
	f := newRestoreFix(t, "alpha")
	hook := filepath.Join(f.dir, ".git", "hooks", "reference-transaction")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	if r.Status != "refused" || len(r.Diagnostics) != 1 || !strings.Contains(r.Diagnostics[0].Message, "default-hooks") {
		t.Fatalf("%+v", r)
	}
	if _, err := f.tryGit("rev-parse", "--verify", "-q", "refs/heads/alpha"); err == nil || len(f.restoreRecords()) != 0 {
		t.Fatal("the refused apply had an effect")
	}
}

func TestRestoreApplyAsCreatesTheEntryUnderAnotherName(t *testing.T) {
	f := newRestoreFix(t, "alpha")
	f.git("update-ref", "refs/heads/alpha", f.git("rev-parse", "HEAD"))
	o := f.applyOptions()
	o.Actions, o.As = []string{f.action("alpha")}, "refs/heads/alpha-back"
	r := applyRestore(t, defaultTidyV3RestoreCoordinator(), o)
	if r.Status != "ok" || r.Entries[0].Ref != "refs/heads/alpha-back" || f.git("rev-parse", "refs/heads/alpha-back") != f.tips["alpha"] {
		t.Fatalf("%+v", r)
	}
	for _, raw := range f.restoreRecords() {
		if !strings.Contains(raw, `"ref":"refs/heads/alpha-back"`) {
			t.Errorf("record names another ref: %s", raw)
		}
	}
	if f.git("rev-parse", "refs/heads/alpha") == f.tips["alpha"] {
		t.Error("the existing alpha was touched")
	}
}

func TestRestoreApplyStatusRule(t *testing.T) {
	item := func(outcome string) restoreItem { return restoreItem{entry: V3RestoreEntry{Outcome: outcome}} }
	cases := []struct {
		name     string
		outcomes []string
		want     string
	}{
		{"nothing to do", nil, "ok"},
		{"all created", []string{"created", "created-recovered"}, "ok"},
		{"present and created", []string{"already-present", "created"}, "ok"},
		{"only present", []string{"already-present"}, "ok"},
		{"created and taken", []string{"created", "name-taken"}, "partial"},
		{"created and missing", []string{"created", "recovery-missing"}, "partial"},
		{"created and an error", []string{"created", "error", "not-attempted"}, "partial"},
		{"only taken", []string{"name-taken"}, "refused"},
		{"present and taken", []string{"already-present", "name-taken"}, "refused"},
		{"incomplete", []string{"journal-incomplete"}, "refused"},
		{"symbolic", []string{"symbolic-reference"}, "refused"},
		{"only an error", []string{"error"}, "error"},
	}
	for _, c := range cases {
		items := []restoreItem{}
		for _, o := range c.outcomes {
			items = append(items, item(o))
		}
		if got := restoreApplyStatus(items); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestRestoreApplyRefusesAChangedJournal(t *testing.T) {
	f := newRestoreFix(t, "alpha")
	c := defaultTidyV3RestoreCoordinator()
	c.afterEvaluate = func() {
		b, err := os.ReadFile(f.journalPath())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.journalPath(), []byte(strings.Replace(string(b), `"phase":"complete"`, `"phase":"incomplete"`, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := applyRestore(t, c, f.applyOptions())
	if r.Status == "ok" {
		t.Fatalf("%+v", r)
	}
	if _, err := f.tryGit("rev-parse", "--verify", "-q", "refs/heads/alpha"); err == nil {
		t.Fatal("restored from a journal that changed")
	}
}

func TestRestoreApplyWritesNoRecordForAnEntryThatIsAlreadyPresent(t *testing.T) {
	f := newRestoreFix(t, "present", "missing")
	f.git("update-ref", "refs/heads/present", f.tips["present"])
	r := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	if r.Status != "ok" || outcomeFor(t, r, "present") != "already-present" || outcomeFor(t, r, "missing") != "created" {
		t.Fatalf("%+v", r)
	}
	if records := f.restoreRecords(); len(records) != 2 {
		t.Fatalf("only the created branch has records: %v", sortedKeys(records))
	}
}

// With nothing restorable the plan is still ok; apply is refused when an entry
// is not already present, and writes neither a record nor the namespace.
func TestRestoreWithNothingRestorableWritesNothing(t *testing.T) {
	f := newRestoreFix(t, "taken", "present")
	f.git("update-ref", "refs/heads/taken", f.git("rev-parse", "HEAD"))
	f.git("update-ref", "refs/heads/present", f.tips["present"])
	if plan := RestoreTidyV3(context.Background(), f.options()); plan.Status != "ok" || plan.Counts.Restorable != 0 {
		t.Fatalf("%+v", plan)
	}
	before := f.snapshot()
	r := applyRestore(t, defaultTidyV3RestoreCoordinator(), f.applyOptions())
	if r.Status != "refused" || outcomeFor(t, r, "taken") != "name-taken" || outcomeFor(t, r, "present") != "already-present" {
		t.Fatalf("%+v", r)
	}
	if !reflect.DeepEqual(before, f.snapshot()) {
		t.Fatal("an apply without an effect changed state")
	}
	if _, err := os.Lstat(filepath.Join(f.state, "tidy-v3-restores")); !os.IsNotExist(err) {
		t.Fatal("the restore namespace was created without an effect")
	}
	onlyPresent := f.options()
	onlyPresent.Actions, onlyPresent.Apply, onlyPresent.Authorize = []string{f.action("present")}, true, true
	if ok := applyRestore(t, defaultTidyV3RestoreCoordinator(), onlyPresent); ok.Status != "ok" {
		t.Fatalf("%+v", ok)
	}
}
