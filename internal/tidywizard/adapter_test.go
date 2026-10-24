// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func found(task, owner, dest string) derive.TaskLookup {
	return derive.TaskLookup{Result: derive.Result{Derived: derive.Derived{Name: "owner:" + task, Value: owner, Source: "task-record"}}, Record: workspace.TaskRecord{TaskID: task, Owner: owner, Destination: dest}}
}

func lookupFailure(code string) derive.TaskLookup {
	return derive.TaskLookup{Result: derive.Result{Ambiguity: &derive.Ambiguity{Code: code}}}
}

// testAdapter returns an adapter over one repository whose observation is obs.
func testAdapter(obs workspace.WorktreeObservation, records map[string]derive.TaskLookup) (Adapter, *workspace.ObserveOptions) {
	var seen workspace.ObserveOptions
	a := NewAdapter([]RepoSpec{{Name: "r", Path: "/ws/r", Base: "refs/heads/main"}}, nil)
	a.Observe = func(_ context.Context, o workspace.ObserveOptions) (workspace.WorktreeObservation, error) {
		seen = o
		return obs, nil
	}
	a.Records = func(task string) derive.TaskLookup {
		if r, ok := records[task]; ok {
			return r
		}
		return lookupFailure("registration-record-missing")
	}
	return a, &seen
}

func evid(path, head string) workspace.WorktreeEvidence {
	return workspace.WorktreeEvidence{Path: path, HeadOID: "a1", HeadRef: head, Proof: "ancestor", AuthorHint: "Ana", LastActivity: old, HasOwnCommit: true}
}

func TestAdapterMapsWorktreeFacts(t *testing.T) {
	linked := evid("/ws/wt/a", "refs/heads/feat/a")
	linked.Locked, linked.LockReason = true, "stw:a"
	linked.Dirty, linked.IgnoredExtras, linked.Ahead = true, true, 3
	primary := evid("/ws/r", "refs/heads/main")
	primary.Primary, primary.Proof, primary.LastActivity = true, "", time.Time{}
	detached := evid("/ws/wt/d", "")
	foreign := evid("/ws/wt/f", "refs/heads/f")
	foreign.Locked, foreign.LockReason = true, "claimed by another tool"
	gone := evid("/ws/wt/g", "refs/heads/g")
	gone.DirMissing, gone.Prunable, gone.Proof = true, true, ""
	gone.Locked, gone.LockReason = true, "stw:g"
	odd := evid("/ws/wt/o", "refs/heads/o")
	odd.Locked, odd.LockReason = true, "stw:" // not the exact stw:<task> form
	a, seen := testAdapter(workspace.WorktreeObservation{Worktrees: []workspace.WorktreeEvidence{linked, primary, detached, foreign, gone, odd}}, map[string]derive.TaskLookup{
		"a": found("a", "werner", "/ws/wt/a"),
		"g": found("g", "werner", "/ws/wt/g"),
	})
	f, err := a.Facts(context.Background(), "r")
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "r" || f.Skip != "" || seen.Path != "/ws/r" || seen.Base != "refs/heads/main" || seen.Online || seen.Remote != "" {
		t.Fatalf("%+v %+v", f, *seen)
	}
	by := map[string]Worktree{}
	for _, w := range f.Worktrees {
		by[w.Path] = w
	}
	want := Worktree{Path: "/ws/wt/a", Branch: "feat/a", Lock: LockStw, Task: "a", Record: RecordFound, TaskOwner: "werner", Dirty: true, IgnoredExtras: true, Proof: ProofAncestor, Ahead: 3, AuthorHint: "Ana", LastActivity: old, HasOwnCommit: true}
	if got := by["/ws/wt/a"]; !reflect.DeepEqual(got, want) {
		t.Errorf("linked:\n got  %+v\n want %+v", got, want)
	}
	if got := by["/ws/r"]; !got.Primary || got.Incomplete != "" || got.Branch != "main" || got.Lock != LockNone || got.Record != RecordNone {
		t.Errorf("primary: %+v", got)
	}
	if got := by["/ws/wt/d"]; got.Branch != "" {
		t.Errorf("detached: %+v", got)
	}
	if got := by["/ws/wt/f"]; got.Lock != LockForeign || got.Task != "" || got.Record != RecordNone {
		t.Errorf("foreign lock: %+v", got)
	}
	if got := by["/ws/wt/g"]; got.Incomplete != "" || !got.DirMissing || !got.Prunable || got.Record != RecordFound || got.Lock != LockStw {
		t.Errorf("stale registration: %+v", got)
	}
	if got := by["/ws/wt/o"]; got.Lock != LockForeign {
		t.Errorf("a lock that is not the exact stw:<task> form is foreign: %+v", got)
	}
}

func TestAdapterJoinsTheCreationRecord(t *testing.T) {
	for name, tc := range map[string]struct {
		lookup derive.TaskLookup
		path   string
		record Record
		owner  string
	}{
		"matching record":                         {found("a", "werner", "/ws/wt/a"), "/ws/wt/a", RecordFound, "werner"},
		"record of another path":                  {found("a", "werner", "/elsewhere"), "/ws/wt/a", RecordMismatch, ""},
		"record of another task":                  {derive.TaskLookup{Result: derive.Result{Derived: derive.Derived{Value: "werner", Source: "task-record"}}, Record: workspace.TaskRecord{TaskID: "b", Owner: "werner", Destination: "/ws/wt/a"}}, "/ws/wt/a", RecordMismatch, ""},
		"task-record source without a record":     {derive.TaskLookup{Result: derive.Result{Derived: derive.Derived{Value: "werner", Source: "task-record"}}}, "/ws/wt/a", RecordMissing, ""},
		"no ambiguity and no record":              {derive.TaskLookup{}, "/ws/wt/a", RecordMissing, ""},
		"a record without the task-record source": {derive.TaskLookup{Result: derive.Result{Derived: derive.Derived{Value: "werner"}}, Record: workspace.TaskRecord{TaskID: "a", Owner: "werner", Destination: "/ws/wt/a"}}, "/ws/wt/a", RecordMissing, ""},
		"missing":         {lookupFailure("registration-record-missing"), "/ws/wt/a", RecordMissing, ""},
		"ambiguous":       {lookupFailure("registration-record-ambiguous"), "/ws/wt/a", RecordAmbiguous, ""},
		"unknown failure": {lookupFailure("something-else"), "/ws/wt/a", RecordMissing, ""},
	} {
		w := evid(tc.path, "refs/heads/feat/a")
		w.Locked, w.LockReason = true, "stw:a"
		a, _ := testAdapter(workspace.WorktreeObservation{Worktrees: []workspace.WorktreeEvidence{w}}, map[string]derive.TaskLookup{"a": tc.lookup})
		f, err := a.Facts(context.Background(), "r")
		if err != nil || len(f.Worktrees) != 1 {
			t.Fatalf("%s: %v %+v", name, err, f)
		}
		if g := f.Worktrees[0]; g.Record != tc.record || g.TaskOwner != tc.owner {
			t.Errorf("%s: record %v owner %q", name, g.Record, g.TaskOwner)
		}
	}
}

func TestAdapterMapsIncompleteAndUnknownProofs(t *testing.T) {
	bad := evid("/ws/wt/b", "refs/heads/b")
	bad.Incomplete = "deadline-exceeded"
	weird := evid("/ws/wt/w", "refs/heads/w")
	weird.Proof = "invented"
	blank := evid("/ws/wt/e", "refs/heads/e")
	blank.Proof = ""
	obs := workspace.WorktreeObservation{
		Worktrees: []workspace.WorktreeEvidence{bad, weird, blank},
		Branches: []workspace.BranchEvidence{
			{Name: "refs/heads/main", IsBase: true, Proof: "ancestor"},
			{Name: "refs/heads/x", Proof: "patch-id", Ahead: 2, AuthorHint: "Ana"},
			{Name: "refs/heads/y", Incomplete: "branch-limit"},
			{Name: "refs/heads/z", Proof: "invented"},
		},
		TrackingRefs: []workspace.TrackingRefEvidence{
			{Name: "refs/remotes/origin/gone", Proof: "github-branch-absent"},
			{Name: "refs/remotes/origin/live", Proof: "none"},
			{Name: "refs/remotes/origin/bad", Incomplete: "hosting-evidence-unavailable"},
			{Name: "refs/remotes/origin/odd", Proof: "invented"},
		},
	}
	a, _ := testAdapter(obs, nil)
	f, err := a.Facts(context.Background(), "r")
	if err != nil {
		t.Fatal(err)
	}
	wt := map[string]Worktree{}
	for _, w := range f.Worktrees {
		wt[w.Path] = w
	}
	if wt["/ws/wt/b"].Incomplete != "deadline-exceeded" {
		t.Errorf("incomplete lost: %+v", wt["/ws/wt/b"])
	}
	for _, p := range []string{"/ws/wt/w", "/ws/wt/e"} {
		if wt[p].Incomplete != "unknown-proof" {
			t.Errorf("%s: an unknown or missing proof must be Incomplete: %+v", p, wt[p])
		}
	}
	br := map[string]Branch{}
	for _, b := range f.Branches {
		br[b.Name] = b
	}
	if b := br["main"]; !b.IsBase || b.Proof != ProofAncestor {
		t.Errorf("main: %+v", b)
	}
	if b := br["x"]; b.Proof != ProofPatchID || b.Ahead != 2 || b.AuthorHint != "Ana" || b.Incomplete != "" {
		t.Errorf("x: %+v", b)
	}
	if b := br["y"]; b.Incomplete != "branch-limit" {
		t.Errorf("y: %+v", b)
	}
	if b := br["z"]; b.Incomplete != "unknown-proof" {
		t.Errorf("z: %+v", b)
	}
	tr := map[string]TrackingRef{}
	for _, r := range f.TrackingRefs {
		tr[r.Name] = r
	}
	if r := tr["refs/remotes/origin/gone"]; r.Proof != "github-branch-absent" || r.Incomplete != "" {
		t.Errorf("gone: %+v", r)
	}
	if r := tr["refs/remotes/origin/live"]; r.Proof != ProofNone || r.Incomplete != "" {
		t.Errorf("live: %+v", r)
	}
	if r := tr["refs/remotes/origin/bad"]; r.Incomplete != "hosting-evidence-unavailable" {
		t.Errorf("bad: %+v", r)
	}
	if r := tr["refs/remotes/origin/odd"]; r.Incomplete != "unknown-proof" {
		t.Errorf("odd: %+v", r)
	}
}

func TestAdapterPassesOnlineAndRemote(t *testing.T) {
	a, seen := testAdapter(workspace.WorktreeObservation{}, nil)
	a.Repos["r"] = RepoSpec{Name: "r", Path: "/ws/r", Base: "refs/remotes/origin/main", Online: true, Remote: "origin"}
	if _, err := a.Facts(context.Background(), "r"); err != nil {
		t.Fatal(err)
	}
	if !seen.Online || seen.Remote != "origin" || seen.Base != "refs/remotes/origin/main" {
		t.Fatalf("%+v", *seen)
	}
}

func TestAdapterSkipsAndErrors(t *testing.T) {
	a, _ := testAdapter(workspace.WorktreeObservation{}, nil)
	called := false
	a.Observe = func(context.Context, workspace.ObserveOptions) (workspace.WorktreeObservation, error) {
		called = true
		return workspace.WorktreeObservation{}, errors.New("boom")
	}
	a.Repos["s"] = RepoSpec{Name: "s", Skip: "base-ambiguous"}
	f, err := a.Facts(context.Background(), "s")
	if err != nil || f.Skip != "base-ambiguous" || called || f.Name != "s" {
		t.Fatalf("skip: %v %+v called=%v", err, f, called)
	}
	if _, err = a.Facts(context.Background(), "r"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("an observation error must be returned: %v", err)
	}
	if _, err = a.Facts(context.Background(), "unknown"); err == nil {
		t.Fatal("an unknown repository name was accepted")
	}
}

// The whole chain: observation, adapter and classifier. An observation that finished produces
// the verdicts of the behaviour table; one that did not is never selectable.
func TestAdapterThroughTheClassifier(t *testing.T) {
	clean := evid("/ws/wt/clean", "refs/heads/feat/clean")
	clean.Locked, clean.LockReason = true, "stw:clean"
	unfinished := evid("/ws/wt/unfinished", "refs/heads/feat/unfinished")
	unfinished.Locked, unfinished.LockReason = true, "stw:unfinished"
	unfinished.Incomplete = "registration-mismatch"
	dirty := evid("/ws/wt/dirty", "refs/heads/feat/dirty")
	dirty.Locked, dirty.LockReason, dirty.Dirty = true, "stw:dirty", true
	obs := workspace.WorktreeObservation{
		Worktrees: []workspace.WorktreeEvidence{clean, unfinished, dirty},
		Branches: []workspace.BranchEvidence{
			{Name: "refs/heads/feat/clean", Proof: "ancestor"},
			{Name: "refs/heads/feat/unfinished", Proof: "ancestor"},
			{Name: "refs/heads/feat/dirty", Proof: "ancestor"},
		},
	}
	a, _ := testAdapter(obs, map[string]derive.TaskLookup{
		"clean":      found("clean", "werner", "/ws/wt/clean"),
		"unfinished": found("unfinished", "werner", "/ws/wt/unfinished"),
		"dirty":      found("dirty", "werner", "/ws/wt/dirty"),
	})
	res := Classify(context.Background(), a, []string{"r"}, cfg())
	got := map[string]Candidate{}
	for _, c := range res.Candidates {
		got[c.ID] = c
	}
	c := got["worktree:r:/ws/wt/clean"]
	if c.Verdict != VerdictSafe || !reflect.DeepEqual(c.Effects, []string{"remove-registration", "remove-files", "delete-branch:feat/clean"}) || c.Scope != "own" {
		t.Errorf("clean: %+v", c)
	}
	if c := got["worktree:r:/ws/wt/unfinished"]; c.Verdict != VerdictRetain || c.Reason != "evidence-incomplete" || len(c.Effects) != 0 {
		t.Errorf("unfinished: %+v", c)
	}
	if c := got["worktree:r:/ws/wt/dirty"]; c.Verdict != VerdictRetain || c.Reason != "dirty" {
		t.Errorf("dirty: %+v", c)
	}
	// The branch of the unfinished worktree is listed and retained, not deleted with it.
	if c := got["branch:r:feat/unfinished"]; c.Verdict != VerdictRetain || c.Reason != "branch-attached-elsewhere" {
		t.Errorf("unfinished branch: %+v", c)
	}
}

// The same chain over a real repository and the real observers.
func TestAdapterOverARealRepository(t *testing.T) {
	git := func(dir string, args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "init.defaultBranch=main"}, args...)...)
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	canon := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	repo := canon(t.TempDir())
	git(repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "file"), []byte("stored\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(repo, "add", ".")
	git(repo, "commit", "-qm", "base")
	git(repo, "branch", "-M", "main")
	add := func(name string) string {
		p := filepath.Join(canon(t.TempDir()), name)
		git(repo, "worktree", "add", "-q", "-b", name, p, "main")
		return p
	}
	done, work := add("done"), add("work")
	git(repo, "worktree", "lock", "--reason", "stw:done", done)
	if err := os.WriteFile(filepath.Join(work, "scratch"), []byte("untracked\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := NewAdapter([]RepoSpec{{Name: "r", Path: repo, Base: "refs/heads/main"}}, nil)
	a.Records = func(task string) derive.TaskLookup { return found(task, "werner", done) }
	res := Classify(context.Background(), a, []string{"r"}, Config{Caller: "werner", CWD: "/elsewhere", Now: time.Now().Add(72 * time.Hour)})
	if res.Repositories[0].State != "analysed" {
		if strings.Contains(res.Repositories[0].Reason, "sandbox") {
			t.Skip("native sandbox unavailable")
		}
		t.Fatalf("%+v", res.Repositories)
	}
	got := map[string]Candidate{}
	for _, c := range res.Candidates {
		got[c.ID] = c
	}
	if c := got["worktree:r:"+done]; c.Verdict != VerdictSafe || c.Scope != "own" || !reflect.DeepEqual(c.Effects, []string{"remove-registration", "remove-files", "delete-branch:done"}) {
		t.Errorf("done: %+v", c)
	}
	if c := got["worktree:r:"+work]; c.Verdict != VerdictRetain || c.Reason != "ignored-extras" {
		t.Errorf("work: %+v", c)
	}
	if c := got["worktree:r:"+repo]; c.Verdict != VerdictRetain || c.Reason != "primary-checkout" {
		t.Errorf("primary: %+v", c)
	}
}

// Each kind has its own proofs: a word of another kind is not a proof.
func TestAdapterProofsAreKindSpecific(t *testing.T) {
	wt := evid("/ws/wt/w", "refs/heads/w")
	wt.Proof = "github-branch-absent"
	obs := workspace.WorktreeObservation{
		Worktrees: []workspace.WorktreeEvidence{wt},
		Branches:  []workspace.BranchEvidence{{Name: "refs/heads/b", Proof: "github-branch-absent"}},
		TrackingRefs: []workspace.TrackingRefEvidence{
			{Name: "refs/remotes/origin/t", Proof: "ancestor"},
			{Name: "refs/remotes/origin/u", Proof: "empty-working-copy-integrated-parent"},
		},
	}
	a, _ := testAdapter(obs, nil)
	f, err := a.Facts(context.Background(), "r")
	if err != nil {
		t.Fatal(err)
	}
	if f.Worktrees[0].Incomplete != "unknown-proof" || f.Branches[0].Incomplete != "unknown-proof" {
		t.Errorf("worktree/branch: %+v %+v", f.Worktrees[0], f.Branches[0])
	}
	for _, r := range f.TrackingRefs {
		if r.Incomplete != "unknown-proof" {
			t.Errorf("tracking ref: %+v", r)
		}
	}
	res := Classify(context.Background(), a, []string{"r"}, cfg())
	for _, c := range res.Candidates {
		if c.Verdict != VerdictRetain || len(c.Effects) != 0 {
			t.Errorf("a wrong-kind proof produced %+v", c)
		}
	}
}

func TestAdapterCarriesTheRetainReasonOfATrackingReference(t *testing.T) {
	obs := workspace.WorktreeObservation{TrackingRefs: []workspace.TrackingRefEvidence{
		{Name: "refs/remotes/origin/live", Proof: "none", Reason: "remote-source-present"},
		{Name: "refs/remotes/origin/used", Proof: "none", Reason: "workspace-used"},
		{Name: "refs/remotes/origin/codex/x", Proof: "github-branch-absent", Reason: "github-branch-absent"},
	}}
	a, _ := testAdapter(obs, nil)
	res := Classify(context.Background(), a, []string{"r"}, cfg())
	got := map[string]Candidate{}
	for _, c := range res.Candidates {
		got[c.ID] = c
	}
	if c := got["tracking-ref:r:refs/remotes/origin/live"]; c.Verdict != VerdictRetain || c.Reason != "remote-source-present" || len(c.Effects) != 0 {
		t.Errorf("live: %+v", c)
	}
	if c := got["tracking-ref:r:refs/remotes/origin/used"]; c.Verdict != VerdictRetain || c.Reason != "workspace-used" {
		t.Errorf("used: %+v", c)
	}
	c := got["tracking-ref:r:refs/remotes/origin/codex/x"]
	if c.Verdict != VerdictSafe || c.Owner != "codex" || c.OwnerSource != "path-convention" || c.Scope != "other" || len(c.Effects) != 1 {
		t.Errorf("an owner convention must match the branch part of the tracking name: %+v", c)
	}
}

func TestAdapterCarriesNoticesToTheRepositoryResult(t *testing.T) {
	obs := workspace.WorktreeObservation{TrackingSkipped: "tracking-requires-online", Limitations: []string{"Evidence is sampled."}}
	a, _ := testAdapter(obs, nil)
	res := Classify(context.Background(), a, []string{"r"}, cfg())
	if got := res.Repositories[0].Notices; !reflect.DeepEqual(got, []string{"tracking-requires-online", "Evidence is sampled."}) {
		t.Fatalf("notices: %v", got)
	}
	// Notices are bounded.
	obs.Limitations = []string{strings.Repeat("x", 2000)}
	for i := 0; i < 20; i++ {
		obs.Limitations = append(obs.Limitations, "n")
	}
	a, _ = testAdapter(obs, nil)
	got := Classify(context.Background(), a, []string{"r"}, cfg()).Repositories[0].Notices
	if len(got) != MaxNotices || len(got[1]) > MaxEvidenceBytes {
		t.Fatalf("notices not bounded: %d, %d bytes", len(got), len(got[1]))
	}
}
