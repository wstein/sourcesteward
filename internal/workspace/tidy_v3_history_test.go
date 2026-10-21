// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

// Spec 031 T050: the history view and restore --list, over journals written by
// real tidy v3 applies.

func historyOf(t *testing.T, o V3HistoryOptions) V3HistoryResult {
	t.Helper()
	return HistoryTidyV3(context.Background(), o)
}

func TestHistoryListsJournalsNewestFirstWithCounts(t *testing.T) {
	f := newHistoryFix(t, []string{"a1", "a2"}, []string{"b1"}, []string{"c1", "c2", "c3"})
	before := f.snapshot()
	r := historyOf(t, f.historyOptions())
	if r.Status != "ok" || r.Command != "tidy-history" || r.SchemaVersion != 1 || r.Total != 3 || r.ScanTruncated || len(r.Truncated) != 0 {
		t.Fatalf("%+v", r)
	}
	if got, want := historyIDs(r), []string{f.plans[2].PlanID, f.plans[1].PlanID, f.plans[0].PlanID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
	for i, want := range []struct{ deleted, restorable int }{{3, 3}, {1, 1}, {2, 2}} {
		e := r.Entries[i]
		stamp := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).Add(time.Duration(2-i) * time.Hour)
		if e.Mode != "local-refs" || e.Deleted != want.deleted || e.Retained != 0 || e.Restorable != want.restorable || e.RecoveryRefs != "present" || e.JournalStatus != "complete" || e.DecisionKinds != (V3HistoryDecisions{}) || e.Time != stamp.Format(time.RFC3339Nano) {
			t.Errorf("entry %d: %+v", i, e)
		}
	}
	if after := f.snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatal("history changed the repository or the store")
	}
}

func TestHistoryCountsFollowTheRepository(t *testing.T) {
	f := newHistoryFix(t, []string{"a1", "a2", "a3"})
	id := f.plans[0].PlanID
	f.git("update-ref", "refs/heads/a1", f.tips["a1"])               // restored by hand: no longer restorable
	f.git("update-ref", "refs/heads/a2", f.git("rev-parse", "HEAD")) // name taken
	f.git("update-ref", "-d", v3RecoveryFor(f.plans[0], actionOf(t, f.plans[0], "a3"), f.tips["a3"]).Name)
	e := historyEntry(t, historyOf(t, f.historyOptions()), id)
	if e.Restorable != 0 || e.RecoveryRefs != "partial" {
		t.Fatalf("%+v", e)
	}
	f.git("update-ref", "-d", "refs/heads/a1")
	if e := historyEntry(t, historyOf(t, f.historyOptions()), id); e.Restorable != 1 {
		t.Fatalf("one branch is restorable again: %+v", e)
	}
	for _, name := range []string{"a1", "a2"} {
		f.git("update-ref", "-d", v3RecoveryFor(f.plans[0], actionOf(t, f.plans[0], name), f.tips[name]).Name)
	}
	if e := historyEntry(t, historyOf(t, f.historyOptions()), id); e.Restorable != 0 || e.RecoveryRefs != "absent" {
		t.Fatalf("%+v", e)
	}
}

func actionOf(t *testing.T, p V3Plan, name string) string {
	t.Helper()
	for _, c := range p.Candidates {
		if c.Name == "refs/heads/"+name {
			return c.ActionID
		}
	}
	t.Fatalf("no candidate %s", name)
	return ""
}

func TestHistoryCountsTheKindOfEveryDecision(t *testing.T) {
	f := classFix(t)
	r := f.decide(t, func(tip func(string) string, base string) []map[string]any {
		human := validDecision("refs/heads/fresh", tip("fresh"), base)
		ai := validDecision("refs/heads/ws", tip("ws"), base)
		ai["decision"], ai["kind"], ai["reason"] = "drop", "ai", "abandoned"
		return []map[string]any{human, ai}
	}, V3PlanOptions{})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("%+v", r.Diagnostics)
	}
	p := *r.Plan
	if a := defaultTidyV3RefsApplyCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}}); a.Status != "ok" {
		t.Fatalf("%+v", a.Diagnostics)
	}
	e := historyEntry(t, historyOf(t, V3HistoryOptions{Path: f.dir, StateDir: p.StateDir}), p.PlanID)
	if e.DecisionKinds != (V3HistoryDecisions{Human: 1, AI: 1}) || e.Deleted < 2 || e.Restorable != e.Deleted {
		t.Fatalf("%+v", e)
	}
}

func TestHistoryListsUnreadableAndForeignModeJournals(t *testing.T) {
	t.Run("unreadable", func(t *testing.T) {
		f := newHistoryFix(t, []string{"a1"})
		for _, name := range []string{"damaged", "orphan"} {
			if err := os.WriteFile(filepath.Join(f.state, "tidy-v3-operations", name+".json"), []byte(`{"schema_version":3,`), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		r := historyOf(t, f.historyOptions())
		if r.Status != "ok" || r.Total != 3 {
			t.Fatalf("%+v", r)
		}
		for _, id := range []string{"damaged", "orphan"} {
			if e := historyEntry(t, r, id); e.JournalStatus != "unreadable" || e.Restorable != 0 || e.Mode != "unknown" || e.Time == "" {
				t.Errorf("%s: %+v", id, e)
			}
		}
		if e := historyEntry(t, r, f.plans[0].PlanID); e.JournalStatus != "complete" || e.Restorable != 1 {
			t.Errorf("the readable journal was affected: %+v", e)
		}
	})
	t.Run("tracking-refs", func(t *testing.T) {
		f := fixtureTidyV3TrackingApply(t, 1)
		if r := f.coordinator.apply(context.Background(), trackingApplyOptions(f.plan)); r.Status != "ok" {
			t.Fatalf("%+v", r.Diagnostics)
		}
		r := historyOf(t, V3HistoryOptions{Path: f.dir, StateDir: f.plan.StateDir})
		e := historyEntry(t, r, f.plan.PlanID)
		if e.Mode != "tracking-refs" || e.Restorable != 0 || e.Deleted != 1 || e.JournalStatus != "complete" || e.RecoveryRefs != "present" {
			t.Fatalf("%+v", e)
		}
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
		e := historyEntry(t, historyOf(t, V3HistoryOptions{Path: o.Path, StateDir: p.StateDir}), p.PlanID)
		if e.Mode != "owned-retirement" && e.Mode != "legacy-retirement" || e.Restorable != 0 || e.JournalStatus != "complete" {
			t.Fatalf("%+v", e)
		}
	})
}

func TestHistoryLimitKeepsTheNewestAndSaysSo(t *testing.T) {
	f := newHistoryFix(t, []string{"a1"}, []string{"b1"}, []string{"c1"})
	o := f.historyOptions()
	o.Limit = 2
	r := historyOf(t, o)
	if got, want := historyIDs(r), []string{f.plans[2].PlanID, f.plans[1].PlanID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
	if r.Total != 3 || r.ScanTruncated || !reflect.DeepEqual(r.Truncated, []V3HistoryTruncation{{Unit: "journals", Shown: 2, Total: 3}}) {
		t.Fatalf("%+v", r)
	}
	for _, bad := range []int{-1, 1025} {
		o.Limit = bad
		if r := historyOf(t, o); r.Status != "refused" || r.Diagnostics[0].Code != "invalid-options" {
			t.Errorf("limit %d: %+v", bad, r)
		}
	}
	o.Limit = 1024
	if r := historyOf(t, o); r.Status != "ok" {
		t.Errorf("the ceiling is allowed: %+v", r)
	}
}

func TestHistoryScansAtMostFourThousandNinetySixRecords(t *testing.T) {
	f := newHistoryFix(t, []string{"a1"})
	dir := filepath.Join(f.state, "tidy-v3-operations")
	for i := 0; i < 4100; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("junk%04d.json", i)), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := historyOf(t, f.historyOptions())
	if r.Status != "ok" || r.Total != 4096 || !r.ScanTruncated || len(r.Entries) != 256 {
		t.Fatalf("total=%d truncated=%v entries=%d", r.Total, r.ScanTruncated, len(r.Entries))
	}
	if !reflect.DeepEqual(r.Truncated, []V3HistoryTruncation{{Unit: "journals", Shown: 256, Total: 4096}}) {
		t.Fatalf("%+v", r.Truncated)
	}
}

func TestHistoryChecksObjectsInOneBatchAndMarksWhatExceedsABound(t *testing.T) {
	f := newHistoryFix(t, []string{"a1", "a2"}, []string{"b1", "b2"})
	count := func(c tidyV3HistoryCoordinator) (r V3HistoryResult, batches int) {
		c.observeArgs = func(args []string) {
			if len(args) > 0 && args[0] == "cat-file" {
				batches++
			}
		}
		return c.history(context.Background(), f.historyOptions()), batches
	}
	r, batches := count(defaultTidyV3HistoryCoordinator())
	if batches != 1 || r.Entries[0].Restorable != 2 || r.Entries[1].Restorable != 2 {
		t.Fatalf("batches=%d %+v", batches, r.Entries)
	}
	small := defaultTidyV3HistoryCoordinator()
	small.objectLines = 3 // the newer journal needs 2, the older one would make 4
	r, batches = count(small)
	newer, older := r.Entries[0], r.Entries[1]
	if batches != 1 || newer.Restorable != 2 || newer.JournalStatus != "complete" || older.JournalStatus != "incomplete" || older.Restorable != 0 {
		t.Fatalf("batches=%d %+v", batches, r.Entries)
	}
	if !reflect.DeepEqual(r.Truncated, []V3HistoryTruncation{{Unit: "objects", Shown: 2, Total: 4}}) {
		t.Fatalf("%+v", r.Truncated)
	}
}

func TestHistoryBudgetMarksTheRemainingJournalsIncomplete(t *testing.T) {
	f := newHistoryFix(t, []string{"a1"}, []string{"b1"}, []string{"c1"})
	c := defaultTidyV3HistoryCoordinator()
	checked := 0
	c.expired = func() bool { checked++; return checked > 1 }
	r := c.history(context.Background(), f.historyOptions())
	if r.Status != "ok" || len(r.Entries) != 3 {
		t.Fatalf("%+v", r)
	}
	if r.Entries[0].JournalStatus != "complete" || r.Entries[1].JournalStatus != "incomplete" || r.Entries[2].JournalStatus != "incomplete" || r.Entries[1].Restorable != 0 {
		t.Fatalf("%+v", r.Entries)
	}
	if !reflect.DeepEqual(r.Truncated, []V3HistoryTruncation{{Unit: "checked-journals", Shown: 1, Total: 3}}) {
		t.Fatalf("%+v", r.Truncated)
	}
}

func TestRestoreListIsTheSameReaderAsHistory(t *testing.T) {
	f := newHistoryFix(t, []string{"a1"}, []string{"b1"})
	o := f.historyOptions()
	history := historyOf(t, o)
	o.List = true
	list := historyOf(t, o)
	if list.Command != "tidy-restore" || history.Command != "tidy-history" {
		t.Fatalf("%s %s", list.Command, history.Command)
	}
	list.Command = history.Command
	if !reflect.DeepEqual(list, history) {
		t.Fatalf("\n%+v\n%+v", list, history)
	}
}

func TestHistoryJSONHasTheContractShape(t *testing.T) {
	f := newHistoryFix(t, []string{"a1"})
	b, err := json.Marshal(historyOf(t, f.historyOptions()))
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
	if got := keys(doc); !reflect.DeepEqual(got, []string{"command", "diagnostics", "entries", "scan_truncated", "schema_version", "status", "total", "truncated"}) {
		t.Errorf("top-level keys %v", got)
	}
	entry := doc["entries"].([]any)[0].(map[string]any)
	if got := keys(entry); !reflect.DeepEqual(got, []string{"decision_kinds", "deleted", "journal_status", "mode", "plan_id", "recovery_refs", "restorable", "retained", "time"}) {
		t.Errorf("entry keys %v", got)
	}
	if got := keys(entry["decision_kinds"].(map[string]any)); !reflect.DeepEqual(got, []string{"ai", "human"}) {
		t.Errorf("decision kind keys %v", got)
	}
}

func TestHistoryRefusesAStateDirectoryThatOverlapsTheRepository(t *testing.T) {
	f := newHistoryFix(t, []string{"a1"})
	o := f.historyOptions()
	o.StateDir = filepath.Join(f.dir, ".git")
	if r := historyOf(t, o); r.Status == "ok" {
		t.Fatalf("%+v", r)
	}
}
