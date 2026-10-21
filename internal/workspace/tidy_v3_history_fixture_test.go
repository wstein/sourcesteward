// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newHistoryFix applies one real tidy v3 plan per group of branches into one
// shared private store. The journals get fixed modification times, one hour
// apart in the order of the groups, so that "newest first" is deterministic.
func newHistoryFix(t *testing.T, groups ...[]string) *restoreFix {
	t.Helper()
	f := &restoreFix{proofFix: newProofFix(t), tips: map[string]string{}}
	f.state = privateDir(t)
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i, names := range groups {
		refs := []string{}
		for _, name := range names {
			f.git("checkout", "-q", "--detach", f.main)
			oid := f.commit("work on "+name, map[string]string{"work-" + strings.ReplaceAll(name, "/", "-") + ".txt": name + "\n"})
			f.git("update-ref", "refs/heads/"+name, oid)
			f.git("checkout", "-q", f.main)
			f.git("merge", "-q", "--no-ff", "-m", "integrate "+name, oid)
			f.tips[name] = oid
			refs = append(refs, "refs/heads/"+name)
		}
		planned := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: f.dir, Base: "refs/heads/" + f.main, StateDir: f.state, Refs: refs, ProtectWorktrees: []string{}})
		if planned.Status != "ok" || planned.Plan == nil {
			t.Fatalf("fixture plan: %+v", planned.Diagnostics)
		}
		f.stored = *planned.Plan
		a := defaultTidyV3RefsApplyCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: f.state, PlanID: f.stored.PlanID, Authorize: true, ProtectWorktrees: []string{}})
		if a.Status != "ok" {
			t.Fatalf("fixture apply: %s %+v", a.Status, a.Diagnostics)
		}
		f.plans = append(f.plans, f.stored)
		stamp := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(f.journalPathOf(f.stored), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *restoreFix) journalPathOf(p V3Plan) string {
	return filepath.Join(f.state, "tidy-v3-operations", p.PlanID+".json")
}

func (f *restoreFix) historyOptions() V3HistoryOptions {
	return V3HistoryOptions{Path: f.dir, StateDir: f.state}
}

func historyEntry(t *testing.T, r V3HistoryResult, planID string) V3HistoryEntry {
	t.Helper()
	for _, e := range r.Entries {
		if e.PlanID == planID {
			return e
		}
	}
	t.Fatalf("no history entry for %s in %+v", planID, r.Entries)
	return V3HistoryEntry{}
}

func historyIDs(r V3HistoryResult) []string {
	ids := []string{}
	for _, e := range r.Entries {
		ids = append(ids, e.PlanID)
	}
	return ids
}
