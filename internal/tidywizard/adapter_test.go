// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
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
	// The paths of this observation do not exist: the physical admission checks are not run.
	a.DestinationAdmission, a.ParentAdmission, a.Registrations = nil, nil, nil
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
	want := Worktree{Path: "/ws/wt/a", Branch: "feat/a", Tip: "a1", Lock: LockStw, Task: "a", Record: RecordFound, TaskOwner: "werner", Dirty: true, IgnoredExtras: true, Proof: ProofAncestor, Ahead: 3, AuthorHint: "Ana", LastActivity: old, HasOwnCommit: true}
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
			{Name: "refs/heads/x", Proof: "patch-id", Ahead: 2, AuthorHint: "Ana", Reason: "workspace-used"},
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
	if b := br["x"]; b.Proof != ProofPatchID || b.Ahead != 2 || b.AuthorHint != "Ana" || b.Incomplete != "" || b.Reason != "workspace-used" {
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

// The empty working copy of a jj workspace on an integrated parent is a proof of a workspace only.
func TestAdapterAcceptsTheEmptyWorkingCopyProofForWorktreesOnly(t *testing.T) {
	w := evid("/ws/wt/jj", "")
	w.Proof = "empty-working-copy-integrated-parent"
	obs := workspace.WorktreeObservation{
		Worktrees: []workspace.WorktreeEvidence{w},
		Branches:  []workspace.BranchEvidence{{Name: "b", Proof: "empty-working-copy-integrated-parent"}},
	}
	a, _ := testAdapter(obs, nil)
	f, err := a.Facts(context.Background(), "r")
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Worktrees[0]; got.Incomplete != "" || got.Proof != ProofEmptyWorkingCopy {
		t.Errorf("worktree: %+v", got)
	}
	if got := f.Branches[0]; got.Incomplete != "unknown-proof" {
		t.Errorf("branch: %+v", got)
	}
	// Without a lock or a record the workspace is unregistered; its owner is only a commit author,
	// which the legacy completion cannot declare, so it is kept (Spec 044 D2).
	res := Classify(context.Background(), a, []string{"r"}, cfg())
	for _, c := range res.Candidates {
		if c.Kind == KindWorktree && (c.Verdict != VerdictRetain || c.Reason != "legacy-owner-unknown" || c.Scope == "own") {
			t.Errorf("%+v", c)
		}
	}
}

// The whole chain over a real jj repository: only a bookmark with no keep reason is safe.
func TestAdapterOverARealJJRepository(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		if os.Getenv("STW_REQUIRE_JJ") == "1" {
			t.Fatal(err)
		}
		t.Skip("jj unavailable")
	}
	jj := func(dir string, args ...string) string {
		t.Helper()
		c := exec.Command("jj", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "JJ_CONFIG=", "JJ_USER=Test", "JJ_EMAIL=test@example.invalid")
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("jj %v: %s %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	last := func(s string) string { f := strings.Fields(s); return f[len(f)-1] }
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	jj(root, "git", "init", "--no-colocate", repo)
	if err := os.WriteFile(filepath.Join(repo, "file"), []byte("stored\n"), 0600); err != nil {
		t.Fatal(err)
	}
	jj(repo, "commit", "-m", "base")
	jj(repo, "bookmark", "create", "main", "-r", "@-")
	if err := os.WriteFile(filepath.Join(repo, "feature"), []byte("work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	jj(repo, "commit", "-m", "feature work")
	jj(repo, "bookmark", "create", "feature", "-r", "@-")
	store := filepath.Join(repo, ".jj", "repo", "store", "git")
	featureID := last(jj(repo, "log", "-r", "feature", "--no-graph", "-T", "commit_id"))
	for _, args := range [][]string{{"remote", "add", "origin", "https://example.invalid/r.git"}, {"update-ref", "refs/remotes/origin/pushed", featureID}} {
		if out, err := exec.Command("git", append([]string{"--git-dir", store}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s %v", out, err)
		}
	}
	old := last(jj(repo, "log", "-r", "main", "--no-graph", "-T", "commit_id"))
	jj(repo, "new", "main", "-m", "next")
	jj(repo, "bookmark", "move", "main", "--to", "@")
	jj(repo, "bookmark", "create", "merged", "-r", old)
	jj(repo, "bookmark", "create", "pushed", "-r", "feature")
	jj(repo, "bookmark", "create", "stw-recovery-x", "-r", "feature")
	jj(repo, "bookmark", "create", "trunk", "-r", "main")
	jj(repo, "git", "import")
	a := NewAdapter([]RepoSpec{{Name: "r", Path: repo, Base: "main"}}, nil)
	res := Classify(context.Background(), a, []string{"r"}, Config{Caller: "werner", CWD: "/elsewhere", Now: time.Now()})
	if res.Repositories[0].State != "analysed" {
		if strings.Contains(res.Repositories[0].Reason, "bubblewrap") && os.Getenv("STW_REQUIRE_JJ") != "1" {
			t.Skip("native jj sandbox unavailable")
		}
		t.Fatalf("%+v", res.Repositories)
	}
	got := map[string]Candidate{}
	for _, c := range res.Candidates {
		got[c.ID] = c
	}
	if c := got["branch:r:merged"]; c.Verdict != VerdictSafe || !reflect.DeepEqual(c.Effects, []string{"delete-branch:merged"}) {
		t.Errorf("merged: %+v", c)
	}
	for name, reason := range map[string]string{"pushed": "remote-counterpart", "stw-recovery-x": "recovery-reference", "trunk": "selected-base"} {
		if c := got["branch:r:"+name]; c.Verdict != VerdictRetain || c.Reason != reason || len(c.Effects) != 0 {
			t.Errorf("%s: %+v", name, c)
		}
	}
	if c := got["branch:r:feature"]; c.Verdict != VerdictRetain || c.Reason != "unmerged" {
		t.Errorf("feature: %+v", c)
	}
	if _, ok := got["branch:r:main"]; ok {
		t.Error("the base bookmark must not be a candidate")
	}
	if c := got["worktree:r:"+repo]; c.Reason != "primary-checkout" {
		t.Errorf("primary: %+v", c)
	}
}

// Spec 041 T019c: the tracking reason the adapter passes on becomes the reason code of a retained
// reference, so it is a closed list; anything else is replaced by the fixed evidence-incomplete
// retention with a fixed evidence text, never echoed.
func TestAdapterAllowlistsTheTrackingReason(t *testing.T) {
	long := strings.Repeat("x", 4096)
	for _, reason := range []string{"remote-source-present", "symbolic-reference", "selected-base", "workspace-used"} {
		obs := workspace.WorktreeObservation{TrackingRefs: []workspace.TrackingRefEvidence{{Name: "refs/remotes/origin/a", Proof: "none", Reason: reason}}}
		a, _ := testAdapter(obs, nil)
		res := Classify(context.Background(), a, []string{"r"}, cfg())
		if c := res.Candidates[0]; c.Verdict != VerdictRetain || c.Reason != reason {
			t.Errorf("%s: %+v", reason, c)
		}
	}
	for name, reason := range map[string]string{
		"unknown": "made-up-reason", "control bytes": "workspace-used\x1b[31m\n", "long": long,
		"case": "Workspace-Used", "prefix": "workspace-used-x", "space": " selected-base", "invalid utf-8": "workspace-used\xff\xfe",
	} {
		obs := workspace.WorktreeObservation{TrackingRefs: []workspace.TrackingRefEvidence{{Name: "refs/remotes/origin/a", Proof: "none", Reason: reason}}}
		a, _ := testAdapter(obs, nil)
		res := Classify(context.Background(), a, []string{"r"}, cfg())
		c := res.Candidates[0]
		if c.Verdict != VerdictRetain || c.Reason != "evidence-incomplete" || len(c.Effects) != 0 || c.Evidence != "tracking-reason-unrecognized" {
			t.Errorf("%s: %+v", name, c)
		}
	}
	// A proven absence stays deletable and its own reason is allowed.
	obs := workspace.WorktreeObservation{TrackingRefs: []workspace.TrackingRefEvidence{{Name: "refs/remotes/origin/a", Proof: "github-branch-absent", Reason: "github-branch-absent"}}}
	a, _ := testAdapter(obs, nil)
	if c := Classify(context.Background(), a, []string{"r"}, cfg()).Candidates[0]; c.Verdict != VerdictSafe {
		t.Errorf("absence: %+v", c)
	}
}

// Spec 041 T011a5: a jj workspace (no stw lock) is joined to its creation record by backend,
// destination and repository directory, through derive only.
func jjObservation(paths ...string) workspace.WorktreeObservation {
	obs := workspace.WorktreeObservation{Backend: "jj", Base: "main", RepositoryDir: "/ws/r/.jj/repo"}
	obs.Worktrees = append(obs.Worktrees, workspace.WorktreeEvidence{Path: "/ws/r", Primary: true})
	for _, p := range paths {
		e := evid(p, "")
		e.Proof = "empty-working-copy-integrated-parent"
		obs.Worktrees = append(obs.Worktrees, e)
	}
	return obs
}

type destCall struct{ backend, dest, repoDir string }

// fakeJJ is a JJRecords whose lookups and skips the test decides, counting the scans.
type fakeJJ struct {
	lookup  func(destCall) derive.TaskLookup
	records []string
	stores  int
	calls   *[]destCall
}

func (f fakeJJ) Lookup(dest, repoDir string) derive.TaskLookup {
	c := destCall{"jj", dest, repoDir}
	*f.calls = append(*f.calls, c)
	return f.lookup(c)
}
func (f fakeJJ) Skips() ([]string, int) { return f.records, f.stores }

func jjAdapter(obs workspace.WorktreeObservation, lookup func(destCall) derive.TaskLookup) (Adapter, *[]destCall) {
	a, scans := jjAdapterSkips(obs, lookup, nil, 0)
	return a, scans
}

// jjAdapterSkips also reports the unreadable records and stores; the second result counts lookups.
func jjAdapterSkips(obs workspace.WorktreeObservation, lookup func(destCall) derive.TaskLookup, records []string, stores int) (Adapter, *[]destCall) {
	var calls []destCall
	a, _ := testAdapter(obs, nil)
	a.JJRecords = func() JJRecords { return fakeJJ{lookup: lookup, records: records, stores: stores, calls: &calls} }
	return a, &calls
}

func foundFor(task, owner string) derive.TaskLookup {
	return derive.TaskLookup{
		Result: derive.Result{Derived: derive.Derived{Name: "owner:" + task, Value: owner, Source: "task-record"}},
		Record: workspace.TaskRecord{TaskID: task, Owner: owner},
	}
}
func TestAdapterJoinsAJJWorkspaceToItsRecordByDestination(t *testing.T) {
	a, calls := jjAdapter(jjObservation("/ws/wt/a", "/ws/wt/b"), func(c destCall) derive.TaskLookup {
		if c.dest == "/ws/wt/a" {
			return foundFor("ta", "werner")
		}
		return lookupFailure("registration-record-missing")
	})
	f, err := a.Facts(context.Background(), "r")
	if err != nil {
		t.Fatal(err)
	}
	want := []destCall{{"jj", "/ws/wt/a", "/ws/r/.jj/repo"}, {"jj", "/ws/wt/b", "/ws/r/.jj/repo"}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("the primary workspace is never looked up, the others by destination and repository: %v", *calls)
	}
	byPath := map[string]Worktree{}
	for _, w := range f.Worktrees {
		byPath[w.Path] = w
	}
	if w := byPath["/ws/wt/a"]; !w.JJOwned || w.Task != "ta" || w.TaskOwner != "werner" || w.Lock != LockNone {
		t.Errorf("owned: %+v", w)
	}
	if w := byPath["/ws/wt/b"]; w.JJOwned || w.TaskOwner != "" || w.Incomplete != "" {
		t.Errorf("without a record: %+v", w)
	}
	if w := byPath["/ws/r"]; w.JJOwned {
		t.Errorf("primary: %+v", w)
	}
	res := Classify(context.Background(), a, []string{"r"}, cfg())
	for _, c := range res.Candidates {
		if c.Path == "/ws/wt/a" && (c.Verdict != VerdictRetain || c.Reason != "jj-retirement-unsupported" || c.Scope != owner.ScopeOwn || len(c.Effects) != 0) {
			t.Errorf("owned workspace: %+v", c)
		}
	}
}

func TestAdapterNeverLooksAGitWorktreeUpByDestination(t *testing.T) {
	obs := workspace.WorktreeObservation{Backend: "git", RepositoryDir: "/ws/r/.git", Worktrees: []workspace.WorktreeEvidence{evid("/ws/wt/a", "refs/heads/x")}}
	a, calls := jjAdapter(obs, func(destCall) derive.TaskLookup { return foundFor("t", "werner") })
	f, _ := a.Facts(context.Background(), "r")
	if len(*calls) != 0 || f.Worktrees[0].JJOwned {
		t.Fatalf("%v %+v", *calls, f.Worktrees[0])
	}
}

func TestAdapterRetainsAJJWorkspaceWithAmbiguousRecordsAndReportsSkippedOnes(t *testing.T) {
	a, _ := jjAdapterSkips(jjObservation("/ws/wt/a"), func(destCall) derive.TaskLookup {
		return lookupFailure("registration-record-ambiguous")
	}, []string{"x\x1b[31m", "y"}, 1)
	f, _ := a.Facts(context.Background(), "r")
	var w Worktree
	for _, x := range f.Worktrees {
		if x.Path == "/ws/wt/a" {
			w = x
		}
	}
	if w.JJOwned || w.Incomplete != "registration-record-ambiguous" {
		t.Errorf("ambiguous records must retain: %+v", w)
	}
	// The notice is a fixed text with honest counts: unreadable records and, apart from them,
	// stores that could not be listed at all. No record id is echoed.
	want := "creation-records-unreadable: 2 records, 1 store that could not be listed"
	n := 0
	for _, x := range f.Notices {
		if strings.Contains(x, "creation-records-unreadable") {
			n++
			if x != want {
				t.Errorf("notice %q, want %q", x, want)
			}
		}
	}
	if n != 1 {
		t.Errorf("want one notice, got %d: %v", n, f.Notices)
	}
	// Reported once for the repository, not once per workspace; nothing skipped, no notice.
	a2, _ := jjAdapterSkips(jjObservation("/ws/wt/a", "/ws/wt/b"), func(destCall) derive.TaskLookup { return lookupFailure("registration-record-missing") }, []string{"y"}, 0)
	f2, _ := a2.Facts(context.Background(), "r")
	n = 0
	for _, x := range f2.Notices {
		if x == "creation-records-unreadable: 1 record, 0 stores that could not be listed" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("want one notice, got %d: %v", n, f2.Notices)
	}
	a4, _ := jjAdapterSkips(jjObservation("/ws/wt/a"), func(destCall) derive.TaskLookup { return lookupFailure("registration-record-missing") }, nil, 2)
	f4, _ := a4.Facts(context.Background(), "r")
	if !slices.Contains(f4.Notices, "creation-records-unreadable: 0 records, 2 stores that could not be listed") {
		t.Errorf("a store that cannot be listed is reported on its own: %v", f4.Notices)
	}
	a3, _ := jjAdapter(jjObservation("/ws/wt/a"), func(destCall) derive.TaskLookup { return lookupFailure("registration-record-missing") })
	for _, x := range func() []string { f, _ := a3.Facts(context.Background(), "r"); return f.Notices }() {
		if strings.Contains(x, "creation-records") {
			t.Errorf("a notice without anything skipped: %q", x)
		}
	}
}

// Whatever the lookup returns, only a found record with a valid task id and the task-record source
// owns a workspace, and a locked workspace is never looked up.
func TestAdapterJJOwnershipFailsClosed(t *testing.T) {
	for name, l := range map[string]derive.TaskLookup{
		"task id with a path":  foundFor("../x", "werner"),
		"empty task id":        foundFor("", "werner"),
		"no source":            {Record: workspace.TaskRecord{TaskID: "t", Owner: "werner"}},
		"source of a guess":    {Result: derive.Result{Derived: derive.Derived{Source: "os-user"}}, Record: workspace.TaskRecord{TaskID: "t", Owner: "werner"}},
		"missing with a hit":   {Result: derive.Result{Ambiguity: &derive.Ambiguity{Code: "registration-record-missing"}}, Record: workspace.TaskRecord{TaskID: "t", Owner: "werner"}},
		"ambiguous with a hit": {Result: derive.Result{Ambiguity: &derive.Ambiguity{Code: "registration-record-ambiguous"}}, Record: workspace.TaskRecord{TaskID: "t", Owner: "werner"}},
	} {
		a, _ := jjAdapter(jjObservation("/ws/wt/a"), func(destCall) derive.TaskLookup { return l })
		f, _ := a.Facts(context.Background(), "r")
		for _, w := range f.Worktrees {
			if w.JJOwned {
				t.Errorf("%s owns: %+v", name, w)
			}
		}
	}
	obs := jjObservation("/ws/wt/a")
	obs.Worktrees[1].Locked, obs.Worktrees[1].LockReason = true, "something"
	a, calls := jjAdapter(obs, func(destCall) derive.TaskLookup { return foundFor("t", "werner") })
	f, _ := a.Facts(context.Background(), "r")
	if len(*calls) != 0 || f.Worktrees[1].JJOwned {
		t.Errorf("a locked workspace was joined: %v %+v", *calls, f.Worktrees[1])
	}
}

// Spec 041 T011a5 review: the records are read once per run, however many workspaces and
// repositories are analysed, and a Git repository never reads them.
func TestAdapterReadsTheJJRecordsOncePerRun(t *testing.T) {
	scans := 0
	a, calls := jjAdapter(jjObservation("/ws/wt/a", "/ws/wt/b", "/ws/wt/c"), func(destCall) derive.TaskLookup { return lookupFailure("registration-record-missing") })
	inner := a.JJRecords
	a.JJRecords = func() JJRecords { scans++; return inner() }
	for i := 0; i < 3; i++ {
		if _, err := a.Facts(context.Background(), "r"); err != nil {
			t.Fatal(err)
		}
	}
	if scans != 1 || len(*calls) != 9 {
		t.Fatalf("scans %d (want 1), lookups %d (want 9: 3 workspaces x 3 runs)", scans, len(*calls))
	}
	git := workspace.WorktreeObservation{Backend: "git", Worktrees: []workspace.WorktreeEvidence{evid("/ws/wt/a", "refs/heads/x")}}
	ga, _ := jjAdapter(git, func(destCall) derive.TaskLookup { return foundFor("t", "werner") })
	gscans := 0
	ginner := ga.JJRecords
	ga.JJRecords = func() JJRecords { gscans++; return ginner() }
	_, _ = ga.Facts(context.Background(), "r")
	if gscans != 0 {
		t.Errorf("a Git repository scanned the records %d times", gscans)
	}
}

// An Adapter built as a struct literal, without NewAdapter, is safe: a jj repository does not
// panic, and the records are read once per Facts call (the run-wide sharing needs NewAdapter).
func TestAdapterStructLiteralIsSafeForJJ(t *testing.T) {
	scans := 0
	var calls []destCall
	a := Adapter{
		Repos: map[string]RepoSpec{"r": {Name: "r", Path: "/ws/r", Base: "main"}},
		Observe: func(context.Context, workspace.ObserveOptions) (workspace.WorktreeObservation, error) {
			return jjObservation("/ws/wt/a", "/ws/wt/b", "/ws/wt/c"), nil
		},
		JJRecords: func() JJRecords {
			scans++
			return fakeJJ{lookup: func(c destCall) derive.TaskLookup {
				if c.dest == "/ws/wt/a" {
					return foundFor("ta", "werner")
				}
				return lookupFailure("registration-record-missing")
			}, calls: &calls}
		},
	}
	f, err := a.Facts(context.Background(), "r")
	if err != nil {
		t.Fatal(err)
	}
	if scans != 1 || len(calls) != 3 {
		t.Fatalf("scans %d (want 1 per call), lookups %d", scans, len(calls))
	}
	owned := 0
	for _, w := range f.Worktrees {
		if w.JJOwned {
			owned++
		}
	}
	if owned != 1 {
		t.Errorf("owned %d", owned)
	}
	// Without a record source a jj repository is analysed as before, nothing owned and no panic.
	b := Adapter{Repos: a.Repos, Observe: a.Observe}
	f, err = b.Facts(context.Background(), "r")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range f.Worktrees {
		if w.JJOwned {
			t.Errorf("owned without a record source: %+v", w)
		}
	}
}

// A repository the preparation could not read is an error row with its reason, never skipped or analysed.
func TestAdapterReportsARepositoryErrorOfThePreparation(t *testing.T) {
	a := NewAdapter([]RepoSpec{{Name: "r", Path: "/ws/r", Error: "the references cannot be read: boom"}}, nil)
	observed := false
	a.Observe = func(context.Context, workspace.ObserveOptions) (workspace.WorktreeObservation, error) {
		observed = true
		return workspace.WorktreeObservation{}, nil
	}
	res := Classify(context.Background(), a, []string{"r"}, cfg())
	if observed || len(res.Repositories) != 1 || res.Repositories[0].State != "error" || !strings.Contains(res.Repositories[0].Reason, "boom") {
		t.Errorf("%+v observed=%v", res.Repositories, observed)
	}
}

// Spec 044 D13: when the online read is only the implicit default of the human wizard and the
// tracking evidence of a repository failed as a whole, the repository is judged offline: its
// tracking rows are dropped like an offline run, the fall back is recorded for the one notice
// line, and nothing is an error or an incomplete row. An explicit --online keeps today's rows.
func TestAdapterJudgesAFailedImplicitOnlineRepositoryOffline(t *testing.T) {
	failed := workspace.WorktreeObservation{TrackingRefs: []workspace.TrackingRefEvidence{
		{Name: "refs/remotes/origin/a", Incomplete: "hosting-evidence-unavailable"},
		{Name: "refs/remotes/origin/b", Incomplete: "hosting-evidence-unavailable"},
	}, Branches: []workspace.BranchEvidence{{Name: "refs/heads/topic", OID: "a1", Proof: "ancestor"}}}
	for name, tc := range map[string]struct {
		spec     RepoSpec
		obs      workspace.WorktreeObservation
		wantRefs int
		fallback string
	}{
		"implicit failure":     {RepoSpec{Online: true, Remote: "origin", OnlineDefault: true}, failed, 0, "error"},
		"explicit failure":     {RepoSpec{Online: true, Remote: "origin"}, failed, 2, ""},
		"unsupported":          {RepoSpec{Online: true, Remote: "origin", OnlineDefault: true}, withReason(failed, "hosting-proof-unsupported"), 0, "remote-unsupported"},
		"limit":                {RepoSpec{Online: true, Remote: "origin", OnlineDefault: true}, withReason(failed, "hosting-evidence-limit"), 0, "member-budget"},
		"deadline":             {RepoSpec{Online: true, Remote: "origin", OnlineDefault: true}, withReason(failed, "deadline-exceeded"), 0, "timeout"},
		"implicit success":     {RepoSpec{Online: true, Remote: "origin", OnlineDefault: true}, workspace.WorktreeObservation{TrackingRefs: []workspace.TrackingRefEvidence{{Name: "refs/remotes/origin/a", Proof: "none", Reason: "remote-source-present"}}}, 1, ""},
		"implicit mixed":       {RepoSpec{Online: true, Remote: "origin", OnlineDefault: true}, workspace.WorktreeObservation{TrackingRefs: []workspace.TrackingRefEvidence{{Name: "refs/remotes/origin/a", Proof: "none"}, {Name: "refs/remotes/origin/b", Incomplete: "observation-failed"}}}, 2, ""},
		"implicit no tracking": {RepoSpec{Online: true, Remote: "origin", OnlineDefault: true}, workspace.WorktreeObservation{}, 0, ""},
	} {
		a, _ := testAdapter(tc.obs, nil)
		tc.spec.Name, tc.spec.Path, tc.spec.Base = "r", "/ws/r", "refs/heads/main"
		a.Repos["r"] = tc.spec
		f, err := a.Facts(context.Background(), "r")
		if err != nil || len(f.TrackingRefs) != tc.wantRefs || f.OnlineFallback != tc.fallback {
			t.Errorf("%s: %+v %v", name, f, err)
		}
		if tc.fallback != "" && !slices.Contains(f.Notices, "tracking-requires-online") {
			t.Errorf("%s: the offline notice is missing: %v", name, f.Notices)
		}
		res := Classify(context.Background(), a, []string{"r"}, cfg())
		if len(res.Repositories) != 1 || res.Repositories[0].State != "analysed" || res.Repositories[0].OnlineFallback != tc.fallback {
			t.Errorf("%s: %+v", name, res.Repositories)
		}
	}
}

func withReason(o workspace.WorktreeObservation, reason string) workspace.WorktreeObservation {
	refs := []workspace.TrackingRefEvidence{}
	for _, r := range o.TrackingRefs {
		r.Incomplete = reason
		refs = append(refs, r)
	}
	o.TrackingRefs = refs
	return o
}
