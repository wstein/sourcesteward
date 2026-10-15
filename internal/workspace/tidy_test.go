// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
	"github.com/wstein/sourcesteward/internal/inspection"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fakeTidyAdapter struct {
	observation TidyObservation
	calls       int
	onObserve   func(int)
	err         error
}

func (f *fakeTidyAdapter) Observe(context.Context, string, string) (TidyObservation, error) {
	f.calls++
	if f.onObserve != nil {
		f.onObserve(f.calls)
	}
	return f.observation, f.err
}
func fixtureTidyCoordinator(t *testing.T) (tidyCoordinator, *fakeTidyAdapter, TidyPlanOptions) {
	t.Helper()
	old, native, options := fixtureCoordinator(t)
	f := &fakeTidyAdapter{observation: TidyObservation{WorkspacePathsComplete: true, WorkspacePaths: []string{options.Path}, Expected: native.snap, Candidates: []TidyCandidate{{Kind: "branch", Name: "refs/heads/topic", CommitID: native.snap.BaseCommit, Decision: "delete", Reason: "integrated", Proof: "native-ancestry", WorkspacePaths: []string{}, Limitations: []string{}}}, Limitations: []string{}}}
	return tidyCoordinator{inspect: old.inspect, adapter: func(string) (tidyAdapter, error) { return f, nil }}, f, TidyPlanOptions{Path: options.Path, Base: options.Base, StateDir: options.StateDir}
}
func TestTidyPlanStatelessAndImmutableStore(t *testing.T) {
	c, f, o := fixtureTidyCoordinator(t)
	state := o.StateDir
	o.StateDir = ""
	r := c.plan(context.Background(), o)
	if r.Status != "ok" || r.Plan == nil || r.Plan.PlanID != "" || f.calls != 2 {
		t.Fatalf("stateless %#v calls%d", r, f.calls)
	}
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 0 {
		t.Fatal("stateless created state", entries, err)
	}
	o.StateDir = state
	r = c.plan(context.Background(), o)
	if r.Status != "ok" || r.Plan == nil || r.Plan.PlanID == "" {
		t.Fatalf("persist %#v", r)
	}
	s, err := openTidyStore(state, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var saved TidyPlanRecord
	if err = s.read("tidy-plans", r.Plan.PlanID, &saved); err != nil {
		t.Fatal(err)
	}
	if err = validateTidyPlan(saved, state, saved.PlanID); err != nil {
		t.Fatal(err)
	}
	saved.Candidates[0].CommitID = strings.Repeat("b", 40)
	if err = validateTidyPlan(saved, state, saved.PlanID); err == nil {
		t.Fatal("modified evidence accepted")
	}
}
func TestTidyPlanSelectionAndRefusals(t *testing.T) {
	for _, test := range []struct {
		name   string
		refs   []string
		mutate func(*tidyCoordinator, *fakeTidyAdapter, *TidyPlanOptions)
	}{
		{name: "missing", refs: []string{"refs/heads/missing"}}, {name: "duplicate", refs: []string{"refs/heads/topic", "refs/heads/topic"}},
		{name: "ambiguous", mutate: func(c *tidyCoordinator, _ *fakeTidyAdapter, _ *TidyPlanOptions) {
			c.inspect = func(context.Context, string) inspection.Result {
				return inspection.Result{Status: "error", Backend: "ambiguous"}
			}
		}},
		{name: "stale", mutate: func(_ *tidyCoordinator, f *fakeTidyAdapter, _ *TidyPlanOptions) {
			f.onObserve = func(n int) {
				if n == 2 {
					f.observation.Expected.BaseCommit = strings.Repeat("b", 40)
				}
			}
		}},
		{name: "unproven", mutate: func(_ *tidyCoordinator, f *fakeTidyAdapter, _ *TidyPlanOptions) {
			f.observation.Candidates[0].Proof = "none"
		}},
		{name: "workspaceused", mutate: func(_ *tidyCoordinator, f *fakeTidyAdapter, o *TidyPlanOptions) {
			f.observation.Candidates[0].WorkspacePaths = []string{o.Path}
		}},
		{name: "overlap", mutate: func(_ *tidyCoordinator, _ *fakeTidyAdapter, o *TidyPlanOptions) { o.StateDir = o.Path }},
		{name: "mutationflags", mutate: func(_ *tidyCoordinator, _ *fakeTidyAdapter, o *TidyPlanOptions) { o.RemoveRegistration = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, f, o := fixtureTidyCoordinator(t)
			o.Refs = test.refs
			if test.mutate != nil {
				test.mutate(&c, f, &o)
			}
			r := c.plan(context.Background(), o)
			if r.Status == "ok" || r.Plan != nil {
				t.Fatalf("unsafe plan %#v", r)
			}
			entries, _ := os.ReadDir(o.StateDir)
			if test.name != "overlap" && len(entries) != 0 {
				t.Fatal("refusal wrote state")
			}
		})
	}
	c, _, o := fixtureTidyCoordinator(t)
	o.Refs = []string{"refs/heads/topic"}
	r := c.plan(context.Background(), o)
	if r.Status != "ok" || len(r.Plan.Candidates) != 1 {
		t.Fatalf("selection %#v", r)
	}
}
func TestTidyCancellationAndBoundsNeverPublishPlan(t *testing.T) {
	c, _, o := fixtureTidyCoordinator(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := c.plan(ctx, o)
	if r.Status == "ok" || r.Plan != nil {
		t.Fatal("canceled proof accepted")
	}
	c, f, o := fixtureTidyCoordinator(t)
	f.observation.Candidates[0].Limitations = make([]string, observationLimit)
	r = c.plan(context.Background(), o)
	if r.Status == "ok" || r.Plan != nil {
		t.Fatal("aggregate bound accepted")
	}
	r = newTidyResult("tidy-plan")
	r.Limitations = []string{strings.Repeat("\x01", recordLimit/2)}
	r = boundedTidy(r)
	b, _ := json.Marshal(r)
	if len(b) > recordLimit || r.Plan != nil || r.Status != "partial" {
		t.Fatal("escaped output not bounded")
	}
}
func TestTidyPrivateStoreCoexistsWithoutChangingLegacyRecords(t *testing.T) {
	state := filepath.Join(privateDir(t), "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	old, err := openStore(state, true)
	if err != nil {
		t.Fatal(err)
	}
	old.Close()
	s, err := openTidyStore(state, true)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	old, err = openStore(state, false)
	if err != nil {
		t.Fatal(err)
	}
	old.Close()
	if err = os.Remove(filepath.Join(state, "tidy-plans")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(state, "plans"), filepath.Join(state, "tidy-plans")); err != nil {
		t.Fatal(err)
	}
	if s, err = openTidyStore(state, true); err == nil {
		s.Close()
		t.Fatal("symlink state accepted")
	}
}

func TestTidyStateRetainsUnmatchedAndFilteredWorkspacePaths(t *testing.T) {
	for _, filtered := range []bool{false, true} {
		c, f, o := fixtureTidyCoordinator(t)
		stateParent := filepath.Dir(o.StateDir)
		other := filepath.Join(stateParent, "other-workspace")
		if err := os.Mkdir(other, 0700); err != nil {
			t.Fatal(err)
		}
		o.StateDir = filepath.Join(other, "private-state")
		if err := os.Mkdir(o.StateDir, 0700); err != nil {
			t.Fatal(err)
		}
		f.observation.WorkspacePaths = append(f.observation.WorkspacePaths, other)
		if filtered {
			o.Refs = []string{"refs/heads/topic"}
			f.observation.Candidates = append(f.observation.Candidates, TidyCandidate{Kind: "branch", Name: "refs/heads/unselected", CommitID: strings.Repeat("b", 40), Decision: "retain", Reason: "workspace-used", Proof: "none", WorkspacePaths: []string{other}, Limitations: []string{}})
		}
		r := c.plan(context.Background(), o)
		if r.Status == "ok" || r.Plan != nil {
			t.Fatalf("registered workspace state overlap accepted filtered=%v %#v", filtered, r)
		}
		entries, _ := os.ReadDir(o.StateDir)
		if len(entries) != 0 {
			t.Fatal("unsafe store wrote files")
		}
	}
}

func TestTidyUnknownWorkspaceRootsCannotPersist(t *testing.T) {
	c, f, o := fixtureTidyCoordinator(t)
	f.observation.WorkspacePathsComplete = false
	r := c.plan(context.Background(), o)
	if r.Status == "ok" || r.Plan != nil {
		t.Fatal("unknown workspace roots authorized a store write")
	}
	o.StateDir = ""
	r = c.plan(context.Background(), o)
	if r.Status != "ok" {
		t.Fatalf("bounded stateless evidence should remain usable %#v", r)
	}
}

func TestTidyRegisteredSymlinkAliasCannotHideStateOverlap(t *testing.T) {
	c, f, o := fixtureTidyCoordinator(t)
	parent := filepath.Dir(o.StateDir)
	alias := filepath.Join(parent, "registered-alias")
	if err := os.Symlink(parent, alias); err != nil {
		t.Fatal(err)
	}
	f.observation.WorkspacePaths = append(f.observation.WorkspacePaths, alias)
	r := c.plan(context.Background(), o)
	if r.Status == "ok" || r.Plan != nil {
		t.Fatal("symlink registration alias hid state-store overlap")
	}
	entries, _ := os.ReadDir(o.StateDir)
	if len(entries) != 0 {
		t.Fatal("aliased workspace refusal wrote state")
	}
	o.StateDir = ""
	r = c.plan(context.Background(), o)
	if r.Status != "ok" {
		t.Fatalf("stateless native evidence unexpectedly rejected %#v", r)
	}
}

func TestTidyNativeJJUnreferencedWorkspaceStateOverlap(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		if os.Getenv("STW_REQUIRE_JJ") == "1" {
			t.Fatal("mandatory jj executable unavailable")
		}
		t.Skip("jj unavailable")
	}
	for _, shape := range []string{"standalone", "colocated"} {
		t.Run(shape, func(t *testing.T) {
			root := gitTempDir(t)
			repo := filepath.Join(root, "repo")
			run := func(dir string, args ...string) string {
				t.Helper()
				cmd := exec.Command("jj", args...)
				cmd.Dir = dir
				cmd.Env = append(os.Environ(), "JJ_CONFIG=", "JJ_USER=Fixture", "JJ_EMAIL=fixture@example.invalid")
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("native jj fixture %v failed: %v %s", args, err, out)
				}
				return string(out)
			}
			colocate := "false"
			if shape == "colocated" {
				colocate = "true"
			}
			run(root, "--config", "git.colocate="+colocate, "git", "init", repo)
			run(repo, "bookmark", "create", "main")
			secondary := filepath.Join(root, "unreferenced")
			run(repo, "workspace", "add", "--name", "unreferenced", secondary)
			state := filepath.Join(secondary, "private-state")
			if err := os.Mkdir(state, 0700); err != nil {
				t.Fatal(err)
			}
			before := run(repo, "--ignore-working-copy", "op", "log", "--limit", "1", "--no-graph", "--template", "id")
			r := PlanTidy(context.Background(), TidyPlanOptions{Path: repo, Base: "main", StateDir: state})
			if r.Status == "ok" || r.Plan != nil || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "state-overlap" {
				t.Fatalf("unreferenced native workspace overlap not refused: %#v", r)
			}
			entries, err := os.ReadDir(state)
			if err != nil || len(entries) != 0 {
				t.Fatal("refused store wrote files", entries, err)
			}
			after := run(repo, "--ignore-working-copy", "op", "log", "--limit", "1", "--no-graph", "--template", "id")
			if before != after {
				t.Fatal("refusal changed native jj operation")
			}
		})
	}
}

func TestTidyCanonicalStatelessRootsDoNotReportUnknownDisjointness(t *testing.T) {
	c, _, o := fixtureTidyCoordinator(t)
	o.StateDir = ""
	r := c.plan(context.Background(), o)
	if r.Status != "ok" {
		t.Fatalf("stateless plan failed %#v", r)
	}
	for _, limitation := range r.Limitations {
		if strings.Contains(limitation, "missing or noncanonical") {
			t.Fatalf("canonical roots falsely reported unknown: %s", limitation)
		}
	}
}
