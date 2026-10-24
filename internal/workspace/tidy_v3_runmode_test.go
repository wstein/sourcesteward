// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Spec 041 T013b and T019b: the mode evidence of a run is recorded in the apply journal and shown
// by history.

func runModeFixture() *V3RunMode {
	return &V3RunMode{Mode: "agent", StdinTTY: false, StdoutTTY: true, Markers: []string{"CLAUDECODE"}, Mine: true, ByOwner: []string{"codex"}, Caller: "claude", CallerSource: "env"}
}

func applyRefsWithRunMode(t *testing.T, mode *V3RunMode) (*restoreFix, V3Plan) {
	t.Helper()
	f := newHistoryFix(t) // state directory and repository only
	f.git("checkout", "-q", "--detach", f.main)
	oid := f.commit("work", map[string]string{"w.txt": "w\n"})
	f.git("update-ref", "refs/heads/w", oid)
	f.git("checkout", "-q", f.main)
	f.git("merge", "-q", "--no-ff", "-m", "integrate", oid)
	planned := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: f.dir, Base: "refs/heads/" + f.main, StateDir: f.state, Refs: []string{"refs/heads/w"}, ProtectWorktrees: []string{}})
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatalf("plan: %+v", planned.Diagnostics)
	}
	a := defaultTidyV3RefsApplyCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: f.state, PlanID: planned.Plan.PlanID, Authorize: true, ProtectWorktrees: []string{}, RunMode: mode})
	if a.Status != "ok" {
		t.Fatalf("apply: %s %+v", a.Status, a.Diagnostics)
	}
	return f, *planned.Plan
}

func journalRaw(t *testing.T, f *restoreFix, p V3Plan) map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.state, "tidy-v3-operations", p.PlanID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestApplyRecordsTheRunModeInTheJournalAndHistoryShowsIt(t *testing.T) {
	want := runModeFixture()
	f, p := applyRefsWithRunMode(t, want)
	raw, ok := journalRaw(t, f, p)["run_mode"]
	if !ok {
		t.Fatal("the journal must record the run mode")
	}
	var got V3RunMode
	if err := json.Unmarshal(raw, &got); err != nil || !reflect.DeepEqual(&got, want) {
		t.Fatalf("journal run_mode %s (%v)", raw, err)
	}
	e := historyEntry(t, historyOf(t, f.historyOptions()), p.PlanID)
	if e.RunMode == nil || !reflect.DeepEqual(e.RunMode, want) || e.JournalStatus != "complete" {
		t.Fatalf("history entry: %+v", e)
	}
}

func TestApplyWithoutARunModeKeepsTheJournalAndHistoryUnchanged(t *testing.T) {
	f, p := applyRefsWithRunMode(t, nil)
	if _, ok := journalRaw(t, f, p)["run_mode"]; ok {
		t.Fatal("an apply without a run mode must not add the key")
	}
	e := historyEntry(t, historyOf(t, f.historyOptions()), p.PlanID)
	if e.RunMode != nil {
		t.Fatalf("%+v", e.RunMode)
	}
	b, _ := json.Marshal(e)
	if strings.Contains(string(b), "run_mode") {
		t.Fatalf("history JSON of an old journal changed: %s", b)
	}
}

func TestRunModeIsValidatedBeforeAnythingIsWritten(t *testing.T) {
	good := runModeFixture()
	cases := map[string]func(*V3RunMode){
		"unknown mode":      func(m *V3RunMode) { m.Mode = "root" },
		"nil markers":       func(m *V3RunMode) { m.Markers = nil },
		"nil by_owner":      func(m *V3RunMode) { m.ByOwner = nil },
		"too many markers":  func(m *V3RunMode) { m.Markers = make([]string, 33) },
		"long marker":       func(m *V3RunMode) { m.Markers = []string{strings.Repeat("A", 129)} },
		"invalid utf8":      func(m *V3RunMode) { m.Caller = "\xff" },
		"nul in caller":     func(m *V3RunMode) { m.Caller = "a\x00b" },
		"long caller":       func(m *V3RunMode) { m.Caller = strings.Repeat("a", 129) },
		"long source":       func(m *V3RunMode) { m.CallerSource = strings.Repeat("a", 65) },
		"owner list length": func(m *V3RunMode) { m.ByOwner = make([]string, 33) },
	}
	for name, mutate := range cases {
		m := *good
		m.Markers, m.ByOwner = append([]string{}, good.Markers...), append([]string{}, good.ByOwner...)
		mutate(&m)
		if validV3RunMode(&m) {
			t.Errorf("%s must be invalid", name)
		}
	}
	if !validV3RunMode(good) || !validV3RunMode(nil) {
		t.Error("a complete run mode and its absence are valid")
	}
	f := newHistoryFix(t)
	f.git("checkout", "-q", "--detach", f.main)
	bad := *good
	bad.Mode = "root"
	r := defaultTidyV3RefsApplyCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: f.state, PlanID: "x", Authorize: true, ProtectWorktrees: []string{}, RunMode: &bad})
	if r.Status == "ok" || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "invalid-options" {
		t.Fatalf("%+v", r)
	}
}

func TestJournalRunModeNeverChangesAfterItWasPrepared(t *testing.T) {
	f, p := applyRefsWithRunMode(t, runModeFixture())
	var j V3Journal
	b, err := os.ReadFile(filepath.Join(f.state, "tidy-v3-operations", p.PlanID+".json"))
	if err != nil || json.Unmarshal(b, &j) != nil {
		t.Fatal(err)
	}
	after := j
	other := *runModeFixture()
	other.Mode = "human"
	after.RunMode = &other
	if validV3Forward(j, after, p) {
		t.Error("a transition that changes the run mode must be refused")
	}
	if validateV3Journal(j, p) != nil {
		t.Error("the stored journal is valid")
	}
	bad := j
	badMode := *runModeFixture()
	badMode.Mode = "root"
	bad.RunMode = &badMode
	if validateV3Journal(bad, p) == nil {
		t.Error("a journal with an invalid run mode must be refused")
	}
	// An initial journal prepared with a run mode must be accepted, and one that differs only in
	// the run mode of the stored record must not be confused with another plan's.
	fresh := newV3JournalWithMode(p, j.InitialNative, runModeFixture())
	if fresh.RunMode == nil || !reflect.DeepEqual(fresh.RunMode, runModeFixture()) {
		t.Fatalf("%+v", fresh.RunMode)
	}
}

func TestRetirementApplyRecordsTheRunModeAndTheLegCarriesIt(t *testing.T) {
	w := ownedPositionFixture(t)
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), w.Options)
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatalf("plan: %+v", planned.Diagnostics)
	}
	leg, ok := TidyV3BranchLeg(*planned.Plan)
	if !ok {
		t.Fatal("leg expected")
	}
	mode := runModeFixture()
	o := ownedApplyOptions(w.Options, *planned.Plan)
	o.RunMode = mode
	retirement := c.apply(context.Background(), o)
	if retirement.Status != "ok" {
		t.Fatalf("retirement: %+v", retirement.Diagnostics)
	}
	readMode := func(planID string) *V3RunMode {
		b, err := os.ReadFile(filepath.Join(w.Options.StateDir, "tidy-v3-operations", planID+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var j V3Journal
		if err := json.Unmarshal(b, &j); err != nil {
			t.Fatal(err)
		}
		return j.RunMode
	}
	if got := readMode(planned.Plan.PlanID); !reflect.DeepEqual(got, mode) {
		t.Fatalf("retirement journal run_mode %+v", got)
	}
	leg.RunMode = mode
	r := ApplyTidyV3BranchLeg(context.Background(), leg, retirement)
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("leg: %+v", r.Diagnostics)
	}
	if got := readMode(r.Plan.PlanID); !reflect.DeepEqual(got, mode) {
		t.Fatalf("the branch deletion's journal must carry the run mode: %+v", got)
	}
}

func TestRetirementApplyRefusesAnInvalidRunModeFirst(t *testing.T) {
	bad := *runModeFixture()
	bad.Mode = "root"
	r := defaultTidyV3RetirementCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: t.TempDir(), PlanID: "x", Authorize: true, ProtectWorktrees: []string{}, RunMode: &bad})
	if r.Status == "ok" || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "invalid-options" {
		t.Fatalf("%+v", r)
	}
}

func TestValidateV3RunModeNamesTheFieldThatIsRefused(t *testing.T) {
	good := runModeFixture()
	if err := ValidateV3RunMode(good); err != nil {
		t.Fatal(err)
	}
	if err := ValidateV3RunMode(nil); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		mutate func(*V3RunMode)
		field  string
	}{
		"mode":                {func(m *V3RunMode) { m.Mode = "root" }, "mode"},
		"markers":             {func(m *V3RunMode) { m.Markers = make([]string, 33) }, "markers"},
		"marker length":       {func(m *V3RunMode) { m.Markers = []string{strings.Repeat("A", 129)} }, "markers"},
		"by_owner entry":      {func(m *V3RunMode) { m.ByOwner = []string{strings.Repeat("o", 129)} }, "by_owner"},
		"by_owner":            {func(m *V3RunMode) { m.ByOwner = make([]string, 33) }, "by_owner"},
		"caller too long":     {func(m *V3RunMode) { m.Caller = strings.Repeat("o", 129) }, "caller"},
		"caller invalid utf8": {func(m *V3RunMode) { m.Caller = "\xff" }, "caller"},
		"caller source":       {func(m *V3RunMode) { m.CallerSource = strings.Repeat("s", 65) }, "caller_source"},
	}
	for name, tc := range cases {
		m := *good
		m.Markers, m.ByOwner = append([]string{}, good.Markers...), append([]string{}, good.ByOwner...)
		tc.mutate(&m)
		err := ValidateV3RunMode(&m)
		if err == nil || !strings.Contains(err.Error(), tc.field) {
			t.Errorf("%s: %v must name %q", name, err, tc.field)
		}
		if validV3RunMode(&m) {
			t.Errorf("%s: validV3RunMode must agree", name)
		}
	}
}
