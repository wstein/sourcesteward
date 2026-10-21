// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"github.com/wstein/sourcesteward/internal/hosting"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Spec 031 T012, T017: the proof classes in the plan, their order, selection,
// budgets and the apply re-evaluation, through the real coordinators.

// classFix builds one repository with a branch per class, all selected.
func classFix(t *testing.T) *proofFix {
	f := newProofFix(t)
	m0 := f.rev("HEAD")
	f.git("branch", "ancestor", m0)
	f.branch("rebased", m0)
	r1 := f.commit("r1", map[string]string{"r1.txt": "r1\n"})
	r2 := f.commit("r2", map[string]string{"r2.txt": "r2\n"})
	f.checkout(f.main)
	f.branch("squashed", m0)
	f.commit("s1", map[string]string{"s1.txt": "s1\n"})
	f.commit("s2", map[string]string{"s2.txt": "s2\n"})
	f.checkout(f.main)
	f.branch("wip", m0)
	f.commit("wip", map[string]string{"w.txt": "wip\n"})
	f.git("revert", "--no-edit", "HEAD")
	f.checkout(f.main)
	f.branch("fresh", m0)
	f.commit("fresh", map[string]string{"fresh.txt": "fresh\n"})
	f.checkout(f.main)
	f.branch("ws", m0)
	f.commit("ws", map[string]string{"w.sh": "  echo hi\n"})
	f.checkout(f.main)
	f.commit("unrelated", map[string]string{"u.txt": "u\n"})
	f.pick(r1)
	f.pick(r2)
	f.commit("squash landed", map[string]string{"s1.txt": "s1\n", "s2.txt": "s2\n"})
	f.commit("ws landed", map[string]string{"w.sh": "echo hi\n"})
	return f
}

func (f *proofFix) planWith(c tidyV3RefsCoordinator, o V3PlanOptions) V3Result {
	f.t.Helper()
	o.Path, o.Base, o.StateDir = f.dir, "refs/heads/"+f.main, privateDir(f.t)
	if o.ProtectWorktrees == nil {
		o.ProtectWorktrees = []string{}
	}
	return c.plan(context.Background(), o)
}
func (f *proofFix) plan(o V3PlanOptions) V3Result {
	return f.planWith(defaultTidyV3RefsCoordinator(), o)
}

func candidateByName(t *testing.T, p *V3Plan, name string) V3Candidate {
	t.Helper()
	for _, c := range p.Candidates {
		if c.Name == "refs/heads/"+name {
			return c
		}
	}
	t.Fatalf("no candidate %s in %+v", name, p.Candidates)
	return V3Candidate{}
}

func TestPlanRecordsEveryForgeFreeClassAndReviewReasons(t *testing.T) {
	f := classFix(t)
	r := f.plan(V3PlanOptions{})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("plan failed: %+v", r.Diagnostics)
	}
	p := r.Plan
	if p.ProofPolicy == nil || !reflect.DeepEqual(*p.ProofPolicy, defaultIntegrationPolicy()) {
		t.Fatalf("default policy %+v", p.ProofPolicy)
	}
	want := map[string][3]string{ // decision, reason, proof
		"ancestor": {"delete", "native-ancestry", "native-ancestry"},
		"rebased":  {"delete", "patch-id", "patch-id"},
		"squashed": {"delete", "merge-noop", "merge-noop"},
		"wip":      {"delete", "no-net-change", "no-net-change"},
		"fresh":    {"retain", "needs-review", "none"},
		"ws":       {"retain", "needs-review", "none"},
	}
	for name, w := range want {
		c := candidateByName(t, p, name)
		if got := [3]string{c.Decision, c.Reason, c.Proof}; got != w {
			t.Errorf("%s: %v, want %v", name, got, w)
		}
	}
	rebased := candidateByName(t, p, "rebased").Evidence.Integration
	if rebased.Strength != "history-equivalent" || rebased.Matched != 2 || rebased.Disposition != "proven" || len(rebased.Reasons) != 0 {
		t.Errorf("rebased evidence %+v", rebased)
	}
	if e := candidateByName(t, p, "squashed").Evidence.Integration; e.Strength != "content-present" || e.Class != "merge-noop" {
		t.Errorf("squashed evidence %+v", e)
	}
	if e := candidateByName(t, p, "wip").Evidence.Integration; e.Strength != "nothing-unmerged" || e.Class != "no-net-change" {
		t.Errorf("wip evidence %+v", e)
	}
	fresh := candidateByName(t, p, "fresh").Evidence.Integration
	if fresh.Disposition != "needs-review" || !reflect.DeepEqual(fresh.Reasons, []string{"would-add-content"}) {
		t.Errorf("fresh evidence %+v", fresh)
	}
	ws := candidateByName(t, p, "ws").Evidence.Integration
	if !reflect.DeepEqual(ws.Reasons, []string{"whitespace-differs", "conflict"}) || !reflect.DeepEqual(ws.Flags, []string{"whitespace-differs"}) {
		t.Errorf("ws evidence %+v", ws)
	}
	// Strengths never call no-net-change integrated.
	if strings.Contains(candidateByName(t, p, "wip").Reason, "integrated") {
		t.Error("no-net-change labelled integrated")
	}
	// The accepting policy deletes the whitespace-only difference and says so.
	a := f.plan(V3PlanOptions{AcceptWhitespaceDifferences: true})
	if a.Status != "ok" {
		t.Fatalf("accepting plan failed: %+v", a.Diagnostics)
	}
	c := candidateByName(t, a.Plan, "ws")
	if c.Decision != "delete" || c.Proof != "patch-id" || !reflect.DeepEqual(c.Evidence.Integration.Flags, []string{"accepted-whitespace-differences"}) {
		t.Errorf("accepted ws %+v", c)
	}
	if a.Plan.Digest == p.Digest || !a.Plan.ProofPolicy.AcceptWhitespaceDifferences {
		t.Error("plan identity does not bind the accepting flag")
	}
}

func TestPlanProofSelectionAndOrder(t *testing.T) {
	f := classFix(t)
	var mu sync.Mutex
	var trace []string
	c := defaultTidyV3RefsCoordinator()
	c.integration.trace = func(class string) { mu.Lock(); trace = append(trace, class); mu.Unlock() }
	only := func(oid string) []string { // the classes tried for one tip, in order
		mu.Lock()
		defer mu.Unlock()
		out := trace
		trace = nil
		_ = oid
		return out
	}
	r := f.planWith(c, V3PlanOptions{Refs: []string{"refs/heads/fresh"}})
	if r.Status != "ok" {
		t.Fatalf("plan failed: %+v", r.Diagnostics)
	}
	if got := only(""); !reflect.DeepEqual(got, []string{"ancestor", "no-net-change", "patch-id", "merge-noop"}) {
		t.Fatalf("order %v", got)
	}
	// Classes that are not selected are neither run nor reported.
	r = f.planWith(c, V3PlanOptions{Refs: []string{"refs/heads/fresh"}, IntegrationProofs: []string{"patch-id"}})
	if r.Status != "ok" {
		t.Fatalf("narrowed plan failed: %+v", r.Diagnostics)
	}
	if got := only(""); !reflect.DeepEqual(got, []string{"patch-id"}) {
		t.Fatalf("narrowed run %v", got)
	}
	cand := candidateByName(t, r.Plan, "fresh")
	if cand.Decision != "retain" || cand.Reason != "integration-not-proven" || cand.Evidence != nil {
		t.Fatalf("a branch no selected class proves or flags stays integration-not-proven: %+v", cand)
	}
	if !reflect.DeepEqual(r.Plan.ProofPolicy.Kinds, []string{"patch-id"}) {
		t.Fatalf("kinds %v", r.Plan.ProofPolicy.Kinds)
	}
	// Kinds are canonicalized: the order given does not change the identity.
	a := f.plan(V3PlanOptions{Refs: []string{"refs/heads/rebased"}, IntegrationProofs: []string{"no-net-change", "ancestor"}})
	if a.Status != "ok" || !reflect.DeepEqual(a.Plan.ProofPolicy.Kinds, []string{"ancestor", "no-net-change"}) {
		t.Fatalf("canonical kinds: %+v %+v", a.Diagnostics, a.Plan)
	}
	// Ancestor is not selected: an ancestor tip is not proven by ancestry.
	n := f.plan(V3PlanOptions{Refs: []string{"refs/heads/ancestor"}, IntegrationProofs: []string{"patch-id"}})
	if c := candidateByName(t, n.Plan, "ancestor"); c.Decision != "retain" || c.Proof != "none" {
		t.Fatalf("ancestor ran although not selected: %+v", c)
	}
	for _, bad := range [][]string{{"cherry-pick"}, {"patch-id", "patch-id"}, {}, {""}} {
		if b := f.plan(V3PlanOptions{IntegrationProofs: bad}); b.Status == "ok" || len(b.Diagnostics) == 0 || b.Diagnostics[0].Code != "invalid-options" {
			if len(bad) != 0 { // an empty list means the default
				t.Errorf("kinds %q admitted: %+v", bad, b)
			}
		}
	}
}

func TestPlanProtectedRefsRetainRegardlessOfProof(t *testing.T) {
	f := classFix(t)
	linked := filepath.Join(gitTempDir(t), "linked")
	f.git("worktree", "add", "-q", linked, "rebased")
	r := f.plan(V3PlanOptions{Refs: []string{"refs/heads/rebased", "refs/heads/" + f.main}})
	if r.Status != "ok" {
		t.Fatalf("plan failed: %+v", r.Diagnostics)
	}
	if c := candidateByName(t, r.Plan, "rebased"); c.Decision != "retain" || c.Reason != "workspace-used" || c.Evidence != nil {
		t.Errorf("workspace-used ref: %+v", c)
	}
	if c := candidateByName(t, r.Plan, f.main); c.Decision != "retain" || c.Reason != "selected-base" {
		t.Errorf("selected base: %+v", c)
	}
}

// Hard stops: the plan succeeds and retains every proof-eligible ref.
func TestPlanHardStopsRetainEveryTip(t *testing.T) {
	cases := map[string]struct{ file, reason string }{
		"grafts":     {".git/info/grafts", "history-override"},
		"shallow":    {".git/shallow", "history-override"},
		"alternates": {".git/objects/info/alternates", "unsupported-metadata"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := classFix(t)
			content := f.rev("HEAD") + "\n"
			if name == "alternates" {
				content = filepath.Join(f.dir, ".git", "objects") + "\n"
			}
			f.write(c.file, content)
			r := f.plan(V3PlanOptions{})
			if r.Status != "ok" || r.Plan == nil {
				t.Fatalf("plan refused: %+v", r.Diagnostics)
			}
			for _, cand := range r.Plan.Candidates {
				if cand.Name == "refs/heads/"+f.main {
					continue
				}
				if cand.Decision != "retain" || cand.Reason != c.reason || cand.Evidence == nil || cand.Evidence.Integration.Disposition != "retained" {
					t.Errorf("%s: %+v", cand.Name, cand)
				}
			}
		})
	}
}

// fakeClock advances only when told.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	calls int
	jump  func(calls int) time.Duration // extra time to add before the call
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.jump != nil {
		c.now = c.now.Add(c.jump(c.calls))
	}
	return c.now
}

// The per-tip budget (20 s) and the forge-free phase budget (60 s) are measured
// by the injected clock; exhaustion marks the remaining tips and the plan stays
// valid.
func TestPlanProofBudgetExhaustionKeepsThePlanValid(t *testing.T) {
	f := classFix(t)
	start := time.Unix(1_800_000_000, 0)
	// Probe: how many clock reads does one full tip evaluation take?
	probe := &fakeClock{now: start}
	c := defaultTidyV3RefsCoordinator()
	c.integration.now = probe.Now
	if r := f.planWith(c, V3PlanOptions{Refs: []string{"refs/heads/fresh"}}); r.Status != "ok" {
		t.Fatalf("probe plan failed: %+v", r.Diagnostics)
	}
	perTip := probe.calls
	if perTip < 3 {
		t.Fatalf("a tip reads the clock %d times", perTip)
	}
	// Phase budget: time jumps past 60 s once the first tip is done; every later
	// distinct tip is proof-budget-exhausted, the first is evaluated.
	phase := &fakeClock{now: start, jump: func(calls int) time.Duration {
		if calls == perTip+1 {
			return 61 * time.Second
		}
		return 0
	}}
	c.integration.now = phase.Now
	r := f.planWith(c, V3PlanOptions{Refs: []string{"refs/heads/fresh", "refs/heads/rebased", "refs/heads/squashed", "refs/heads/ws"}})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("exhausted plan refused: %+v", r.Diagnostics)
	}
	exhausted := 0
	for _, cand := range r.Plan.Candidates {
		if cand.Evidence != nil && cand.Evidence.Integration != nil && reflect.DeepEqual(cand.Evidence.Integration.Reasons, []string{"proof-budget-exhausted"}) {
			exhausted++
			if cand.Decision != "retain" || cand.Reason != "needs-review" {
				t.Errorf("exhausted tip deletable: %+v", cand)
			}
		}
	}
	if exhausted != 3 {
		t.Fatalf("%d exhausted tips, want 3 (the first tip is evaluated): %+v", exhausted, r.Plan.Candidates)
	}
	if first := candidateByName(t, r.Plan, "fresh"); first.Evidence == nil || first.Evidence.Integration.Reasons[0] != "would-add-content" {
		t.Errorf("the first tip was not evaluated: %+v", first)
	}
	// Per-tip budget: a tip that spends 20 s stops; the next tip starts afresh.
	tip := &fakeClock{now: start, jump: func(calls int) time.Duration {
		if calls == 4 {
			return 21 * time.Second
		}
		return 0
	}}
	c.integration.now = tip.Now
	r = f.planWith(c, V3PlanOptions{Refs: []string{"refs/heads/fresh", "refs/heads/squashed"}})
	if r.Status != "ok" {
		t.Fatalf("plan failed: %+v", r.Diagnostics)
	}
	if first := candidateByName(t, r.Plan, "fresh"); first.Evidence == nil || !strings.Contains(strings.Join(first.Evidence.Integration.Reasons, ","), "proof-budget-exhausted") {
		t.Errorf("per-tip budget not applied: %+v", first)
	}
	if second := candidateByName(t, r.Plan, "squashed"); second.Proof != "merge-noop" {
		t.Errorf("the next tip did not start afresh: %+v", second)
	}
}

// A command that exceeds its time limit is proof-limit for its class.
func TestPlanCommandTimeoutIsProofLimit(t *testing.T) {
	f := classFix(t)
	c := defaultTidyV3RefsCoordinator()
	c.integration.commandTimeout = time.Nanosecond
	r := f.planWith(c, V3PlanOptions{Refs: []string{"refs/heads/fresh"}})
	if r.Status != "ok" {
		t.Fatalf("plan failed: %+v", r.Diagnostics)
	}
	if e := candidateByName(t, r.Plan, "fresh").Evidence; e == nil || !reflect.DeepEqual(e.Integration.Reasons, []string{"proof-limit"}) {
		t.Fatalf("timeout evidence %+v", e)
	}
}

func TestPlanBudgetTotalsAreAmended(t *testing.T) {
	if got := v3ProofPlanTimeout(V3PlanOptions{}); got != 120*time.Second {
		t.Errorf("offline plan budget %s, want 60 s + 60 s", got)
	}
	if got := v3ProofPlanTimeout(V3PlanOptions{Online: true}); got != 150*time.Second {
		t.Errorf("online plan budget %s, want 60 s + 60 s + 30 s", got)
	}
}

// Apply re-evaluates the recorded class with fresh state and retains on change.
func TestApplyReevaluatesRecordedClassAndRetainsOnChange(t *testing.T) {
	for name, kinds := range map[string][]string{"patch-id": {"patch-id"}, "merge-noop": {"merge-noop"}, "no-net-change": {"no-net-change"}} {
		t.Run(name, func(t *testing.T) {
			f := classFix(t)
			branch := map[string]string{"patch-id": "rebased", "merge-noop": "squashed", "no-net-change": "wip"}[name]
			plan := f.plan(V3PlanOptions{Refs: []string{"refs/heads/" + branch}, IntegrationProofs: kinds})
			if plan.Status != "ok" || candidateByName(t, plan.Plan, branch).Decision != "delete" {
				t.Fatalf("plan: %+v %+v", plan.Diagnostics, plan.Plan)
			}
			apply := func() V3Result {
				return defaultTidyV3RefsApplyCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: plan.Plan.StateDir, PlanID: plan.Plan.PlanID, Authorize: true, ProtectWorktrees: []string{}})
			}
			if name != "no-net-change" {
				// An attribute source that appears after planning is not bound by
				// the safety digest; the re-evaluation must catch it and retain.
				f.write(".git/info/attributes", "*.txt -diff\n")
				a := apply()
				if a.Status == "ok" || len(a.Diagnostics) == 0 || a.Diagnostics[0].Code != "evidence-changed" {
					t.Fatalf("apply did not retain on change: %+v", a)
				}
				if out := f.git("for-each-ref", "refs/heads/"+branch, "refs/stw/recovery"); strings.Contains(out, "recovery") || !strings.Contains(out, branch) {
					t.Fatalf("a retained apply changed refs: %q", out)
				}
				os.Remove(filepath.Join(f.dir, ".git", "info", "attributes"))
			}
			a := apply()
			if a.Status != "ok" {
				t.Fatalf("apply failed: %+v", a.Diagnostics)
			}
			if err := exec.Command("git", "-C", f.dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run(); err == nil {
				t.Fatal("the proven branch still exists")
			}
			target := candidateByName(t, plan.Plan, branch)
			if got := f.git("rev-parse", v3RecoveryFor(*plan.Plan, target.ActionID, target.OID).Name); got != target.OID {
				t.Fatalf("recovery at %s, want %s", got, target.OID)
			}
		})
	}
}

// The re-evaluated verdict is compared with the recorded evidence even when the
// native snapshot is unchanged: any difference retains.
func TestApplyRetainsWhenTheReevaluatedVerdictDiffers(t *testing.T) {
	f := classFix(t)
	plan := f.plan(V3PlanOptions{Refs: []string{"refs/heads/rebased"}, IntegrationProofs: []string{"patch-id"}})
	if plan.Status != "ok" || candidateByName(t, plan.Plan, "rebased").Decision != "delete" {
		t.Fatalf("plan: %+v", plan.Diagnostics)
	}
	mutations := map[string]func(*integrationVerdict){
		"matched":        func(v *integrationVerdict) { v.Matched++ },
		"unmatched":      func(v *integrationVerdict) { v.Unmatched++ },
		"empty":          func(v *integrationVerdict) { v.Empty++ },
		"skipped merges": func(v *integrationVerdict) { v.SkippedMerges++ },
		"flags":          func(v *integrationVerdict) { v.Flags = []string{"accepted-whitespace-differences"} },
		"class":          func(v *integrationVerdict) { v.Class = "merge-noop" },
		"disposition":    func(v *integrationVerdict) { v.Disposition = "needs-review" },
	}
	apply := func(mutate func(*integrationVerdict)) V3Result {
		a := defaultTidyV3RefsApplyCoordinator()
		if mutate != nil {
			a.refs.proofRunner = func(ctx context.Context, run *integrationRun, base, tip string, policy V3ProofPolicy) (integrationVerdict, error) {
				v, err := run.evaluate(ctx, base, tip, policy)
				mutate(&v)
				return v, err
			}
		}
		return a.apply(context.Background(), V3ApplyOptions{StateDir: plan.Plan.StateDir, PlanID: plan.Plan.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	}
	for name, mutate := range mutations {
		a := apply(mutate)
		if a.Status == "ok" || len(a.Diagnostics) == 0 || a.Diagnostics[0].Code != "evidence-changed" {
			t.Errorf("%s: apply did not retain: %+v", name, a)
		}
		if out := f.git("for-each-ref", "refs/heads/rebased", "refs/stw/recovery"); strings.Contains(out, "recovery") || !strings.Contains(out, "rebased") {
			t.Fatalf("%s: a retained apply changed refs: %q", name, out)
		}
	}
	if a := apply(nil); a.Status != "ok" {
		t.Fatalf("the unchanged evidence must apply: %+v", a.Diagnostics)
	}
}

// Online evidence with the default four-class policy: a tip left for review
// by the native classes is still upgraded by a verified merged pull request,
// and an unselected ancestor class never proves a tip.
func TestPlanOnlineEvidenceUpgradesNeedsReviewAndHonoursUnselectedAncestor(t *testing.T) {
	f := newProofFix(t)
	m0 := f.rev("HEAD")
	f.git("branch", "anc", m0)
	f.branch("topic", m0)
	head := f.commit("topic", map[string]string{"f.txt": "x\n"})
	f.checkout(f.main)
	f.commit("unrelated", map[string]string{"u.txt": "u\n"})
	result := f.commit("squash landed", map[string]string{"f.txt": "x\n", "g.txt": "g\n"})
	f.commit("later edit", map[string]string{"f.txt": "x\ny\n"})
	repo := hosting.TidyRepositoryIdentity{Host: "github.com", TidyRepositoryID: hosting.TidyRepositoryID{ID: 1, NodeID: "R_fixture", FullName: "owner/repo"}, DefaultBranch: "main"}
	row := tidyV3PR(repo, head, m0, result, "main", 1)
	c, _ := fixtureTidyV3Online(t, f.dir, repo, []hosting.TidyHeadEvidence{row})

	offline := f.plan(V3PlanOptions{Refs: []string{"refs/heads/topic"}})
	if got := candidateByName(t, offline.Plan, "topic"); got.Decision != "retain" || got.Reason != "needs-review" {
		t.Fatalf("the fixture must leave topic for review offline: %+v", got)
	}
	r := f.planWith(c, V3PlanOptions{Refs: []string{"refs/heads/topic"}, Online: true, Remote: "origin"})
	if r.Status != "ok" {
		t.Fatalf("online plan: %+v", r.Diagnostics)
	}
	if got := candidateByName(t, r.Plan, "topic"); got.Decision != "delete" || got.Proof != "github-merged-head" {
		t.Fatalf("a verified merged PR must upgrade a needs-review tip: %+v", got)
	}
	// Without the ancestor class an ancestor tip is not proven by ancestry, not
	// even through the provider path.
	n := f.planWith(c, V3PlanOptions{Refs: []string{"refs/heads/anc"}, Online: true, Remote: "origin", IntegrationProofs: []string{"patch-id"}})
	if n.Status != "ok" {
		t.Fatalf("online narrowed plan: %+v", n.Diagnostics)
	}
	if got := candidateByName(t, n.Plan, "anc"); got.Decision != "retain" || got.Proof != "none" {
		t.Fatalf("ancestor ran although not selected: %+v", got)
	}
}
