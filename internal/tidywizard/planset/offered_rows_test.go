// SPDX-License-Identifier: EUPL-1.2

package planset

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// The rule under test: an offered row is a plannable row. A row the analysis offers (verdict
// safe or decision) must build through the planner on its own; every row that cannot is kept
// with a reason. The fixtures are real repositories, one per condition the dogfood run of
// Spec 044 T016 found (D1 to D3), and a mixed one.

// offeredFixture is one repository with its state directory and its classification.
type offeredFixture struct {
	t           *testing.T
	root, repo  string
	state       string
	git         func(dir string, args ...string) string
	cleanup     []string
	staleLocked string
}

func newOfferedFixture(t *testing.T) *offeredFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	root := t.TempDir()
	f := &offeredFixture{t: t, root: root, repo: filepath.Join(root, "repo"), state: filepath.Join(root, "state")}
	for _, d := range []struct {
		path string
		mode os.FileMode
	}{{f.state, 0700}, {f.repo, 0755}, {filepath.Join(root, "wts"), 0755}} {
		if err := os.Mkdir(d.path, d.mode); err != nil {
			t.Fatal(err)
		}
	}
	f.git = func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	f.git(f.repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(f.repo, "file"), []byte("content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f.git(f.repo, "add", "file")
	f.git(f.repo, "commit", "-q", "-m", "base")
	t.Cleanup(func() {
		for _, p := range f.cleanup {
			f.git(f.repo, "worktree", "remove", "--force", p)
			_ = os.RemoveAll(p)
		}
	})
	return f
}

// mergedBranch makes a branch whose commit is merged into main (integrated, so deletable).
func (f *offeredFixture) mergedBranch(name string) {
	f.t.Helper()
	f.git(f.repo, "branch", name, "main")
	f.git(f.repo, "switch", "-q", name)
	f.git(f.repo, "commit", "-q", "--allow-empty", "-m", "work "+name)
	f.git(f.repo, "switch", "-q", "main")
	f.git(f.repo, "merge", "-q", "--no-ff", "-m", "merge "+name, name)
}

// plainWorktree adds an unregistered worktree (no stw lock) on a merged branch of its own.
func (f *offeredFixture) plainWorktree(path, branch string) {
	f.t.Helper()
	f.mergedBranch(branch)
	f.git(f.repo, "worktree", "add", "-q", path, branch)
	f.cleanup = append(f.cleanup, path)
}

// staleStwWorktree creates a worktree with stw (it carries the lock stw:<task>), checks the
// branch out in it and removes its directory: the registration stays, locked and missing.
func (f *offeredFixture) staleStwWorktree(task, branch string) {
	f.t.Helper()
	f.mergedBranch(branch)
	dest := filepath.Join(f.root, "wts", task)
	planned := workspace.PlanCreate(context.Background(), workspace.PlanOptions{Path: f.repo, Base: "refs/heads/main", Destination: dest, Task: task, Owner: "claude", StateDir: f.state, CallerDir: f.repo})
	if planned.Plan == nil {
		f.t.Fatalf("create plan: %+v", planned)
	}
	if r := workspace.Apply(context.Background(), workspace.ApplyOptions{StateDir: f.state, PlanID: planned.Plan.PlanID, Authorize: true}); r.Status != "ok" {
		f.t.Fatalf("create apply: %+v", r)
	}
	f.git(dest, "switch", "-q", branch)
	if err := os.RemoveAll(dest); err != nil {
		f.t.Fatal(err)
	}
	f.staleLocked = dest
}

// staleUnlockedWorktree leaves a prunable registration (not locked) that holds the branch.
func (f *offeredFixture) staleUnlockedWorktree(name, branch string) {
	f.t.Helper()
	f.mergedBranch(branch)
	dest := filepath.Join(f.root, "wts", name)
	f.git(f.repo, "worktree", "add", "-q", dest, branch)
	if err := os.RemoveAll(dest); err != nil {
		f.t.Fatal(err)
	}
}

// controlWorktree adds a worktree that is plannable: its directory is below a directory of the
// user whose name starts with whr (owner codex, scope other, so choosable in human mode).
func (f *offeredFixture) controlWorktree(branch string) string {
	f.t.Helper()
	dir, err := os.MkdirTemp("/tmp", "whr-control-")
	if err != nil {
		f.t.Skipf("cannot make a directory under /tmp: %v", err)
	}
	f.t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "wt")
	f.plainWorktree(path, branch)
	return path
}

func (f *offeredFixture) planner() V3Planner {
	return NewV3Planner([]RepoSpec{{Name: "r", Path: f.repo, Base: "refs/heads/main"}}, []derive.Store{{Path: f.state}})
}

// classify analyses the repository as the wizard does, a month from now so nothing is recent.
func (f *offeredFixture) classify() tidywizard.Result {
	f.t.Helper()
	stores := []derive.Store{{Path: f.state}}
	adapter := tidywizard.NewAdapter([]tidywizard.RepoSpec{{Name: "r", Path: f.repo, Base: "refs/heads/main"}}, stores)
	return tidywizard.Classify(context.Background(), adapter, []string{"r"}, tidywizard.Config{Caller: "claude", CWD: f.root, Now: time.Now().Add(30 * 24 * time.Hour)})
}

// typedTTY is a terminal that answers the typed confirmation by reading the word the prompt
// names, as a person would.
type typedTTY struct {
	out bytes.Buffer
	in  *strings.Reader
}

var typedWord = regexp.MustCompile(`Type "([^"]*)" to confirm`)

func (tt *typedTTY) Read(p []byte) (int, error) {
	if tt.in == nil {
		m := typedWord.FindSubmatch(tt.out.Bytes())
		if m == nil {
			return 0, io.EOF
		}
		tt.in = strings.NewReader(string(m[1]) + "\n")
	}
	return tt.in.Read(p)
}
func (tt *typedTTY) Write(p []byte) (int, error) { return tt.out.Write(p) }
func (tt *typedTTY) Close() error                { return nil }

func typingOpener() owner.Opener {
	return func() (io.ReadWriteCloser, error) { return &typedTTY{}, nil }
}

// offered returns the rows the analysis offers: a verdict safe or decision is the superset of
// every row the wizard numbers.
func offered(res tidywizard.Result) []tidywizard.Candidate {
	var out []tidywizard.Candidate
	for _, c := range res.Candidates {
		if c.Verdict == tidywizard.VerdictSafe || c.Verdict == tidywizard.VerdictDecision {
			out = append(out, c)
		}
	}
	return out
}

// requireOfferedRowsBuild is the property: each offered row builds alone. It returns the number
// of rows that were offered.
func requireOfferedRowsBuild(t *testing.T, f *offeredFixture, res tidywizard.Result) int {
	t.Helper()
	rows := offered(res)
	for _, c := range rows {
		confirmed := map[string]bool{}
		if c.Verdict == tidywizard.VerdictDecision {
			confirmed[c.ID] = true
		}
		_, _, err := Build(context.Background(), Request{Candidates: res.Candidates, Chosen: []string{c.ID}, Mode: human, DecisionConfirmed: confirmed, Confirm: typingOpener()}, f.planner())
		if err != nil {
			t.Errorf("the analysis offers %s (%s, owner %q, scope %s) but it does not build: %v", c.ID, c.Verdict, c.Owner, c.Scope, err)
		}
	}
	return len(rows)
}

func rowOf(t *testing.T, res tidywizard.Result, id string) tidywizard.Candidate {
	t.Helper()
	for _, c := range res.Candidates {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no candidate %s in %d candidates", id, len(res.Candidates))
	return tidywizard.Candidate{}
}

// sharedParent makes a directory directly under /tmp, which another user owns (root), and
// skips the test where the user owns /tmp. The name starts with whr, which the owner
// convention labels codex (scope other), so the row is choosable in human mode.
func sharedParent(t *testing.T) string {
	t.Helper()
	var st syscall.Stat_t
	if err := syscall.Stat("/tmp", &st); err != nil || int(st.Uid) == os.Geteuid() {
		t.Skip("/tmp is owned by the current user here; the condition needs a directory of another owner")
	}
	dir, err := os.MkdirTemp("/tmp", "whr-offered-")
	if err != nil {
		t.Skipf("cannot make a directory under /tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// D1: a worktree directly under a directory that another user owns is never offered.
func TestD1WorktreeUnderAForeignOwnedParentIsKeptNotOffered(t *testing.T) {
	f := newOfferedFixture(t)
	f.mergedBranch("plain")
	shared := sharedParent(t)
	f.plainWorktree(shared, "under-tmp")
	control := f.controlWorktree("fine")
	res := f.classify()
	c := rowOf(t, res, "worktree:r:"+shared)
	if c.Verdict != tidywizard.VerdictRetain || c.Reason != "retirement-parent-foreign" {
		t.Errorf("worktree under /tmp: verdict %s reason %q, want retain retirement-parent-foreign", c.Verdict, c.Reason)
	}
	if got := rowOf(t, res, "worktree:r:"+control); got.Verdict != tidywizard.VerdictSafe {
		t.Errorf("the worktree in a directory of the user must stay offered, got %s %q", got.Verdict, got.Reason)
	}
	if n := requireOfferedRowsBuild(t, f, res); n == 0 {
		t.Error("the fixture offers nothing, so the property proves nothing")
	}
}

// D2: an unregistered worktree whose only owner is a commit author hint is kept (owner
// decision of 2026-10-28), a branch with the same hint stays offered.
func TestD2WorktreeWithAnAuthorHintOwnerIsKeptNotOffered(t *testing.T) {
	f := newOfferedFixture(t)
	hinted := filepath.Join(f.root, "wts", "hinted")
	f.plainWorktree(hinted, "hinted")
	f.mergedBranch("just-a-branch")
	f.controlWorktree("fine")
	res := f.classify()
	c := rowOf(t, res, "worktree:r:"+hinted)
	if c.OwnerSource != owner.SourceCommitAuthor {
		t.Fatalf("fixture: owner source %s, want commit-author", c.OwnerSource)
	}
	if c.Verdict != tidywizard.VerdictRetain || c.Reason != "legacy-owner-unknown" {
		t.Errorf("hint-owned worktree: verdict %s reason %q, want retain legacy-owner-unknown", c.Verdict, c.Reason)
	}
	if b := rowOf(t, res, "branch:r:just-a-branch"); b.Verdict != tidywizard.VerdictSafe {
		t.Errorf("a branch with the same hint owner is plannable and stays offered, got %s %q", b.Verdict, b.Reason)
	}
	if n := requireOfferedRowsBuild(t, f, res); n == 0 {
		t.Error("the fixture offers nothing, so the property proves nothing")
	}
}

// D3: a locked registration whose directory is gone keeps the whole repository, with one
// notice; a branch checked out in any registration, stale ones included, is attached.
func TestD3LockedMissingWorktreeKeepsTheRepositoryWithOneNotice(t *testing.T) {
	f := newOfferedFixture(t)
	f.mergedBranch("plain")
	f.staleStwWorktree("gone1", "lockme")
	control := f.controlWorktree("fine")
	res := f.classify()
	// The branch held by the stale registration is kept for its own, more specific reason.
	if c := rowOf(t, res, "branch:r:lockme"); c.Verdict != tidywizard.VerdictRetain || c.Reason != "branch-attached-elsewhere" {
		t.Errorf("branch:r:lockme: verdict %s reason %q, want retain branch-attached-elsewhere", c.Verdict, c.Reason)
	}
	for _, id := range []string{"branch:r:plain", "worktree:r:" + control} {
		c := rowOf(t, res, id)
		if c.Verdict != tidywizard.VerdictRetain || c.Reason != "unknown-workspace-path" {
			t.Errorf("%s: verdict %s reason %q, want retain unknown-workspace-path", id, c.Verdict, c.Reason)
		}
	}
	named := 0
	for _, n := range res.Repositories[0].Notices {
		if strings.Contains(n, "git worktree unlock") && strings.Contains(n, "git worktree prune") {
			named++
		}
	}
	if named != 1 {
		t.Errorf("want exactly one notice that names git worktree unlock and git worktree prune, got %d in %q", named, res.Repositories[0].Notices)
	}
	requireOfferedRowsBuild(t, f, res)
}

func TestD3BranchCheckedOutInAPrunableRegistrationIsAttachedElsewhere(t *testing.T) {
	f := newOfferedFixture(t)
	f.mergedBranch("plain")
	f.staleUnlockedWorktree("gone2", "held")
	res := f.classify()
	if c := rowOf(t, res, "branch:r:held"); c.Verdict != tidywizard.VerdictRetain || c.Reason != "branch-attached-elsewhere" {
		t.Errorf("branch held by a stale registration: verdict %s reason %q, want retain branch-attached-elsewhere", c.Verdict, c.Reason)
	}
	if c := rowOf(t, res, "branch:r:plain"); c.Verdict != tidywizard.VerdictSafe {
		t.Errorf("an unrelated branch stays offered, got %s %q", c.Verdict, c.Reason)
	}
	if n := requireOfferedRowsBuild(t, f, res); n == 0 {
		t.Error("the fixture offers nothing, so the property proves nothing")
	}
}

// The mixed repository: every condition at once, and the control rows that must stay offered
// and build.
func TestMixedConditionsEveryOfferedRowBuilds(t *testing.T) {
	f := newOfferedFixture(t)
	f.mergedBranch("plain")
	f.plainWorktree(filepath.Join(f.root, "wts", "hinted"), "hinted")
	f.staleUnlockedWorktree("gone2", "held")
	f.controlWorktree("fine")
	res := f.classify()
	rows := offered(res)
	ids := []string{}
	for _, c := range rows {
		ids = append(ids, c.ID)
	}
	for _, want := range []string{"branch:r:plain", "worktree:r:/tmp/whr-control-"} {
		if !strings.Contains(strings.Join(ids, " "), want) {
			t.Errorf("control row %s is not offered: %v", want, ids)
		}
	}
	requireOfferedRowsBuild(t, f, res)
	if testing.Short() {
		return
	}
	if sharedParentAvailable() {
		g := newOfferedFixture(t)
		g.mergedBranch("plain")
		g.plainWorktree(sharedParent(t), "under-tmp")
		g.controlWorktree("fine")
		gres := g.classify()
		requireOfferedRowsBuild(t, g, gres)
	}
}

func sharedParentAvailable() bool {
	var st syscall.Stat_t
	return syscall.Stat("/tmp", &st) == nil && int(st.Uid) != os.Geteuid()
}
