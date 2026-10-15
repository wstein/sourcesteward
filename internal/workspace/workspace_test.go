// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"github.com/wstein/sourcesteward/internal/inspection"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeAdapter struct {
	snap      Snapshot
	creates   int
	fail      bool
	foreign   bool
	onObserve func()
	onCreate  func(MutationPlan)
}

func (f *fakeAdapter) Observe(context.Context, string, string) (Snapshot, error) {
	if f.onObserve != nil {
		f.onObserve()
	}
	return f.snap, nil
}
func (f *fakeAdapter) Create(_ context.Context, p MutationPlan) (NativeWorkspace, error) {
	f.creates++
	if f.onCreate != nil {
		f.onCreate(p)
	}
	n := NativeWorkspace{Identity: f.snap.Identity, Complete: true, SourceUnchanged: true, Phase: "materialized", Effects: []string{"registered destination"}, Limitations: []string{}}
	n.Identity.WorkspaceRoot = p.Destination
	n.Identity.GitDir = filepath.Join(f.snap.Identity.RepositoryDir, "worktrees", p.TaskID)
	if f.fail {
		n.Complete = false
		n.Phase = "registered"
		return n, errors.New("interrupted native create")
	}
	return n, nil
}
func (f *fakeAdapter) Verify(_ context.Context, p MutationPlan) (NativeWorkspace, error) {
	hook := f.onCreate
	f.onCreate = nil
	n, _ := f.Create(context.Background(), p)
	f.onCreate = hook
	f.creates--
	n.Complete = true
	if f.foreign {
		n.Identity.RepositoryDir = "/foreign"
	}
	return n, nil
}
func fixtureCoordinator(t *testing.T) (coordinator, *fakeAdapter, PlanOptions) {
	base := privateDir(t)
	source := filepath.Join(base, "source")
	os.Mkdir(source, 0700)
	meta := filepath.Join(source, ".git")
	os.Mkdir(meta, 0700)
	state := filepath.Join(base, "state")
	os.Mkdir(state, 0700)
	f := &fakeAdapter{snap: Snapshot{Identity: NativeIdentity{Backend: "git", RepositoryDir: meta, WorkspaceRoot: source, GitDir: meta, CommitID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ObjectFormat: "sha1"}, BaseSelector: "HEAD", BaseCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ReferenceDigest: strings.Repeat("1", 64), RegistrationDigest: strings.Repeat("2", 64), SafetyDigest: strings.Repeat("3", 64)}}
	c := coordinator{inspect: func(context.Context, string) inspection.Result {
		return inspection.Result{Status: "ok", Backend: "git"}
	}, adapter: func(string) (nativeAdapter, error) { return f, nil }, inventory: func(context.Context, string, inspection.InventoryOptions) inspection.InventoryResult {
		return inspection.InventoryResult{Status: "ok"}
	}}
	return c, f, PlanOptions{Path: source, Base: "HEAD", Destination: filepath.Join(base, "destination"), Task: "task-1", Owner: "agent-1", StateDir: state}
}
func TestCoordinatorAuthorizationStaleAndCompletedRetry(t *testing.T) {
	c, f, o := fixtureCoordinator(t)
	p := c.plan(context.Background(), o)
	if p.Status != "ok" || p.Plan == nil {
		t.Fatalf("plan %#v", p)
	}
	opts := ApplyOptions{StateDir: o.StateDir, PlanID: p.Plan.PlanID}
	if r := c.apply(context.Background(), opts); r.Status != "refused" || f.creates != 0 {
		t.Fatal("unauthorized native action")
	}
	opts.Authorize = true
	before := f.snap
	f.snap.BaseCommit = "changed"
	if r := c.apply(context.Background(), opts); r.Status != "refused" || f.creates != 0 {
		t.Fatalf("stale applied %#v", r)
	}
	f.snap = before
	r := c.apply(context.Background(), opts)
	if r.Status != "ok" || r.Task == nil || f.creates != 1 {
		t.Fatalf("apply %#v creates%d", r, f.creates)
	}
	f.snap.Identity.CommitID = strings.Repeat("b", 40)
	r = c.apply(context.Background(), opts)
	if r.Status != "ok" || f.creates != 1 {
		t.Fatalf("retry %#v", r)
	}
	f.foreign = true
	r = c.apply(context.Background(), opts)
	if r.Status != "refused" {
		t.Fatalf("foreign retry %#v", r)
	}
}
func TestCoordinatorIncompleteNeverAdoptsOrReexecutes(t *testing.T) {
	c, f, o := fixtureCoordinator(t)
	p := c.plan(context.Background(), o)
	f.fail = true
	r := c.apply(context.Background(), ApplyOptions{o.StateDir, p.Plan.PlanID, true})
	if r.Status != "partial" || f.creates != 1 {
		t.Fatalf("partial %#v", r)
	}
	f.fail = false
	r = c.apply(context.Background(), ApplyOptions{o.StateDir, p.Plan.PlanID, true})
	if r.Status != "partial" || f.creates != 1 {
		t.Fatalf("incomplete retried %#v", r)
	}
	if _, e := os.Stat(o.Destination); e != nil {
		t.Fatal("removed incomplete destination")
	}
}
func TestCoordinatorHandoffRetirementAndTaskCollision(t *testing.T) {
	c, _, o := fixtureCoordinator(t)
	p := c.plan(context.Background(), o)
	if second := c.plan(context.Background(), o); second.Status != "refused" {
		t.Fatal("task reservation collision")
	}
	r := c.apply(context.Background(), ApplyOptions{o.StateDir, p.Plan.PlanID, true})
	if r.Status != "ok" {
		t.Fatalf("apply %#v", r)
	}
	for _, retire := range []bool{false, true} {
		r = c.handoff(context.Background(), Options{o.Destination, o.StateDir}, retire)
		if r.Status != "ok" || r.Task == nil {
			t.Fatalf("handoff %#v", r)
		}
		if retire && r.Task.State != "retained" {
			t.Fatal("retirement did not retain")
		}
	}
}

func TestPlansDiscloseBackendSpecificEffects(t *testing.T) {
	c, _, o := fixtureCoordinator(t)
	r := c.plan(context.Background(), o)
	if r.Status != "ok" {
		t.Fatal(r.Error())
	}
	all := strings.Join(r.Plan.Effects, " ")
	for _, term := range []string{"detached", "worktrees", "locked"} {
		if !strings.Contains(all, term) {
			t.Fatalf("missing native effect %q: %s", term, all)
		}
	}
}
func TestIncompleteEffectsAreNotClaimedComplete(t *testing.T) {
	c, f, o := fixtureCoordinator(t)
	p := c.plan(context.Background(), o)
	f.fail = true
	r := c.apply(context.Background(), ApplyOptions{o.StateDir, p.Plan.PlanID, true})
	for _, a := range r.Actions {
		if a.Name == "native-effect" && a.Status == "completed" {
			t.Fatal("uncertain native effect claimed complete")
		}
	}
}
func TestInitialFinalizeRequiresExactNativeIdentity(t *testing.T) {
	c, f, o := fixtureCoordinator(t)
	p := c.plan(context.Background(), o)
	s, e := openStore(o.StateDir, true)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	id, e := reserveDestination(*p.Plan)
	if e != nil {
		t.Fatal(e)
	}
	effective := *p.Plan
	effective.DestinationIdentity = &id
	native, _ := f.Create(context.Background(), effective)
	j := journal{SchemaVersion: 1, PlanID: p.Plan.PlanID, Phase: "native-created", Native: &native, DestinationIdentity: &id, Actions: []ActionOutcome{}, Diagnostics: []Diagnostic{}}
	f.snap.Identity.CommitID = "concurrent commit after create"
	r := c.finalize(context.Background(), s, effective, j, f, newResult("workspace-apply"))
	if r.Status != "partial" {
		t.Fatalf("accepted concurrent native identity: %#v", r)
	}
}
func TestHandoffRejectsIncompleteJournalEvidence(t *testing.T) {
	c, _, o := fixtureCoordinator(t)
	p := c.plan(context.Background(), o)
	r := c.apply(context.Background(), ApplyOptions{o.StateDir, p.Plan.PlanID, true})
	if r.Status != "ok" {
		t.Fatal(r.Error())
	}
	s, e := openStore(o.StateDir, true)
	if e != nil {
		t.Fatal(e)
	}
	var j journal
	if e = s.read("operations", p.Plan.PlanID, &j); e != nil {
		t.Fatal(e)
	}
	j.Native.Complete = false
	if e = s.write("operations", p.Plan.PlanID, j, false); e != nil {
		t.Fatal(e)
	}
	s.Close()
	r = c.handoff(context.Background(), Options{o.Destination, o.StateDir}, false)
	if r.Status != "refused" {
		t.Fatal("accepted noncomplete native ownership")
	}
}

func TestTaskOwnershipRejectsForgedRecoveryAndCompletion(t *testing.T) {
	c, _, o := fixtureCoordinator(t)
	p := c.plan(context.Background(), o)
	r := c.apply(context.Background(), ApplyOptions{o.StateDir, p.Plan.PlanID, true})
	if r.Status != "ok" {
		t.Fatal(r.Error())
	}
	task := *r.Task
	task.RecoveryPointer = "/foreign"
	if validTask(task, *p.Plan) {
		t.Fatal("accepted foreign recovery record")
	}
	task = *r.Task
	task.State = "incomplete"
	if validTask(task, *p.Plan) {
		t.Fatal("accepted noncomplete stored task")
	}
}

func TestMalformedSemanticPlansNeverEscapeIntoResults(t *testing.T) {
	for _, change := range []func(*MutationPlan){func(p *MutationPlan) { p.Expected.BaseCommit = "x" }, func(p *MutationPlan) { p.Expected.Identity.CommitID = "x" }, func(p *MutationPlan) { p.Expected.SafetyDigest = "x" }, func(p *MutationPlan) { p.Effects = []string{strings.Repeat("x", 4097)} }} {
		c, _, o := fixtureCoordinator(t)
		r := c.plan(context.Background(), o)
		p := *r.Plan
		change(&p)
		p.Digest = planDigest(p)
		s, e := openStore(o.StateDir, true)
		if e != nil {
			t.Fatal(e)
		}
		if e = s.write("plans", p.PlanID, p, false); e != nil {
			t.Fatal(e)
		}
		s.Close()
		r = c.apply(context.Background(), ApplyOptions{o.StateDir, p.PlanID, true})
		if r.Status != "error" || r.Plan != nil || r.Task != nil {
			t.Fatalf("malformed stored plan escaped result: %#v", r)
		}
	}
}
func TestMalformedJournalActionsNeverEscapeIntoResults(t *testing.T) {
	c, _, o := fixtureCoordinator(t)
	r := c.plan(context.Background(), o)
	p := *r.Plan
	s, e := openStore(o.StateDir, true)
	if e != nil {
		t.Fatal(e)
	}
	j := journal{SchemaVersion: 1, PlanID: p.PlanID, Phase: "incomplete", Actions: []ActionOutcome{{"x", "incomplete", strings.Repeat("x", 4097)}}, Diagnostics: []Diagnostic{}}
	if e = s.write("operations", p.PlanID, j, true); e != nil {
		t.Fatal(e)
	}
	s.Close()
	r = c.apply(context.Background(), ApplyOptions{o.StateDir, p.PlanID, true})
	if r.Status != "error" || len(r.Actions) != 0 {
		t.Fatalf("malformed journal action escaped: %#v", r)
	}
}

func TestJournalPersistenceFailurePreventsNativeAction(t *testing.T) {
	c, f, o := fixtureCoordinator(t)
	r := c.plan(context.Background(), o)
	ops := filepath.Join(o.StateDir, "operations")
	f.onObserve = func() {
		if e := os.Chmod(ops, 0755); e != nil {
			t.Fatal(e)
		}
	}
	defer os.Chmod(ops, 0700)
	r = c.apply(context.Background(), ApplyOptions{o.StateDir, r.Plan.PlanID, true})
	if r.Status != "error" || f.creates != 0 {
		t.Fatalf("native executed without secure durable journal: %#v", r)
	}
	if _, e := os.Stat(o.Destination); !os.IsNotExist(e) {
		t.Fatal("destination reserved before journal persistence")
	}
}
func TestTaskStoreFailureAfterNativePreservesPreparedRecovery(t *testing.T) {
	c, f, o := fixtureCoordinator(t)
	r := c.plan(context.Background(), o)
	id := r.Plan.PlanID
	tasks := filepath.Join(o.StateDir, "tasks")
	f.onCreate = func(MutationPlan) {
		if e := os.Chmod(tasks, 0755); e != nil {
			t.Fatal(e)
		}
	}
	r = c.apply(context.Background(), ApplyOptions{o.StateDir, id, true})
	if r.Status != "partial" || f.creates != 1 {
		t.Fatalf("lost partial outcome: %#v", r)
	}
	known := false
	for _, a := range r.Actions {
		if a.Name == "native-effect" {
			known = true
		}
	}
	if !known {
		t.Fatal("known native effect omitted")
	}
	if _, e := os.Stat(o.Destination); e != nil {
		t.Fatal("incomplete destination lost")
	}
	if e := os.Chmod(tasks, 0700); e != nil {
		t.Fatal(e)
	}
	f.onCreate = nil
	s, e := openStore(o.StateDir, false)
	if e != nil {
		t.Fatal(e)
	}
	var j journal
	if e = s.read("operations", id, &j); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if j.Phase != "prepared" {
		t.Fatalf("earlier durable recovery lost: %s", j.Phase)
	}
	r = c.apply(context.Background(), ApplyOptions{o.StateDir, id, true})
	if r.Status != "partial" || f.creates != 1 {
		t.Fatal("unproven recovery recreated/adopted native destination")
	}
}
func TestCancellationAfterNativeRetainsPartialEffects(t *testing.T) {
	c, f, o := fixtureCoordinator(t)
	r := c.plan(context.Background(), o)
	id := r.Plan.PlanID
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.onCreate = func(p MutationPlan) {
		if p.DestinationIdentity == nil {
			t.Fatal("native started before reservation journal")
		}
		cancel()
	}
	r = c.apply(ctx, ApplyOptions{o.StateDir, id, true})
	if r.Status != "partial" || f.creates != 1 {
		t.Fatalf("cancellation lost completed or partial effects: %#v", r)
	}
	if len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "canceled" {
		t.Fatalf("cancellation not explicit: %#v", r.Diagnostics)
	}
	if _, e := os.Stat(o.Destination); e != nil {
		t.Fatal("cancellation removed destination")
	}
	f.onCreate = nil
	r = c.apply(context.Background(), ApplyOptions{o.StateDir, id, true})
	if r.Status != "partial" || f.creates != 1 {
		t.Fatal("canceled native action was repeated")
	}
}
