// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The owner's own repositories carry about a hundred linked worktrees and two
// hundred branches. This acceptance test retires 60 integrated worktrees one
// plan and apply at a time, deletes 120 integrated branches (the 60 that were
// checked out in the retired worktrees and 60 more) in ONE reviewed plan, and
// restores them from the history with equal object ids. No manual
// `git worktree remove` or `git branch -D` is involved.
func TestTidyV3ScaleSixtyWorktreesAndOneHundredTwentyBranches(t *testing.T) {
	if testing.Short() {
		t.Skip("long: 60 worktrees and 120 branches")
	}
	const n = 60
	f := &restoreFix{proofFix: newProofFix(t), tips: map[string]string{}}
	tips := map[string]string{}
	integrate := func(name string) {
		f.git("checkout", "-q", "--detach", f.main)
		oid := f.commit("work on "+name, map[string]string{"work-" + name + ".txt": name + "\n"})
		f.git("update-ref", "refs/heads/"+name, oid)
		f.git("checkout", "-q", f.main)
		f.git("merge", "-q", "--no-ff", "-m", "integrate "+name, oid)
		tips[name] = oid
	}
	dirs := make([]string, n)
	root := gitTempDir(t)
	for i := 0; i < n; i++ {
		integrate(fmt.Sprintf("wt-%02d", i))
		integrate(fmt.Sprintf("br-%02d", i))
	}
	for i := 0; i < n; i++ {
		dirs[i] = filepath.Join(root, fmt.Sprintf("wt-%02d", i))
		f.git("worktree", "add", "-q", dirs[i], fmt.Sprintf("wt-%02d", i))
	}
	state := privateDir(t)
	c := defaultTidyV3RetirementCoordinator()
	for i := 0; i < n; i++ {
		others := []string{}
		for j := i + 1; j < n; j++ {
			others = append(others, dirs[j])
		}
		o := V3PlanOptions{Path: f.dir, Base: "refs/heads/" + f.main, StateDir: state, CompletedWorktree: dirs[i], Owner: "operator", RemoveRegistration: true, RemoveFiles: dirs[i], ProtectWorktrees: others}
		planned := c.plan(context.Background(), o)
		if planned.Status != "ok" || planned.Plan == nil || !v3RetirementEligible(*planned.Plan.Retirement) {
			t.Fatalf("retirement plan %d: %s %+v", i, planned.Status, planned.Diagnostics)
		}
		applied := c.apply(context.Background(), V3ApplyOptions{StateDir: state, PlanID: planned.Plan.PlanID, Authorize: true, CompletedWorktree: dirs[i], Owner: "operator", ProtectWorktrees: others})
		if applied.Status != "ok" {
			t.Fatalf("retirement apply %d: %s %+v", i, applied.Status, applied.Diagnostics)
		}
		if _, err := os.Lstat(dirs[i]); !os.IsNotExist(err) {
			t.Fatalf("worktree %d remains", i)
		}
	}
	if got := strings.Count(f.git("worktree", "list", "--porcelain"), "worktree "); got != 1 {
		t.Fatalf("%d registrations remain", got)
	}
	refs := []string{}
	for name := range tips {
		refs = append(refs, "refs/heads/"+name)
	}
	sort.Strings(refs)
	r := f.plan(V3PlanOptions{Refs: refs})
	if r.Status != "ok" || r.Plan == nil || len(r.Plan.Candidates) != 2*n {
		t.Fatalf("branch plan: %s %+v", r.Status, r.Diagnostics)
	}
	for _, cand := range r.Plan.Candidates {
		if cand.Decision != "delete" {
			t.Fatalf("not deletable: %+v", cand)
		}
	}
	f.stored, f.state = *r.Plan, r.Plan.StateDir
	a := defaultTidyV3RefsApplyCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: f.state, PlanID: f.stored.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if a.Status != "ok" {
		t.Fatalf("branch apply: %s %+v", a.Status, a.Diagnostics)
	}
	if out := f.git("for-each-ref", "--format=%(refname)", "refs/heads/wt-*", "refs/heads/br-*"); out != "" {
		t.Fatalf("branches remain: %s", out)
	}
	restored := defaultTidyV3RestoreCoordinator().restore(context.Background(), f.applyOptions())
	if restored.Status != "ok" || len(restored.Entries) != 2*n {
		t.Fatalf("restore: %s %+v", restored.Status, restored.Diagnostics)
	}
	for name, want := range tips {
		if got := f.git("rev-parse", "refs/heads/"+name); got != want {
			t.Fatalf("%s restored at %s, deleted at %s", name, got, want)
		}
	}
}

// 120 integrated branches (STW_SCALE_BRANCHES=200 for the owner's size) are one plan, one apply and one restore with
// equal object ids: no count in the plan, the journal or the native facts may
// make the owner's real repository unusable.
func TestTidyV3ManyBranchesPlanApplyRestore(t *testing.T) {
	if testing.Short() {
		t.Skip("long: 200 branches")
	}
	count := 120
	if v, err := strconv.Atoi(os.Getenv("STW_SCALE_BRANCHES")); err == nil && v > 0 {
		count = v
	}
	names := make([]string, count)
	for i := range names {
		names[i] = fmt.Sprintf("topic/b-%03d", i)
	}
	f := &restoreFix{proofFix: newProofFix(t), tips: map[string]string{}}
	refs := make([]string, 0, len(names))
	for _, name := range names {
		f.git("checkout", "-q", "--detach", f.main)
		oid := f.commit("work on "+name, map[string]string{"work-" + strings.ReplaceAll(name, "/", "-") + ".txt": name + "\n"})
		f.git("update-ref", "refs/heads/"+name, oid)
		f.git("checkout", "-q", f.main)
		f.git("merge", "-q", "--no-ff", "-m", "integrate "+name, oid)
		f.tips[name] = oid
		refs = append(refs, "refs/heads/"+name)
	}
	started := time.Now()
	r := f.plan(V3PlanOptions{Refs: refs})
	if r.Status != "ok" || r.Plan == nil || len(r.Plan.Candidates) != len(names) {
		t.Fatalf("plan: %s %+v", r.Status, r.Diagnostics)
	}
	t.Logf("plan of %d branches took %s", len(names), time.Since(started))
	f.stored, f.state = *r.Plan, r.Plan.StateDir
	started = time.Now()
	a := defaultTidyV3RefsApplyCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: f.state, PlanID: f.stored.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if a.Status != "ok" {
		t.Fatalf("apply: %s %+v", a.Status, a.Diagnostics)
	}
	t.Logf("apply of %d branches took %s", len(names), time.Since(started))
	started = time.Now()
	defer func() { t.Logf("restore took %s", time.Since(started)) }()
	if out := f.git("for-each-ref", "--format=%(refname)", "refs/heads/topic/"); out != "" {
		t.Fatalf("branches remain: %d", strings.Count(out, "\n")+1)
	}
	restored := defaultTidyV3RestoreCoordinator().restore(context.Background(), f.applyOptions())
	if restored.Status != "ok" || len(restored.Entries) != len(names) {
		t.Fatalf("restore: %s %+v", restored.Status, restored.Diagnostics)
	}
	for _, name := range names {
		if got := f.git("rev-parse", "refs/heads/"+name); got != f.tips[name] {
			t.Fatalf("%s restored at %s, deleted at %s", name, got, f.tips[name])
		}
	}
}
