// SPDX-License-Identifier: EUPL-1.2

package planset

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

type realChain struct {
	repo, state string
	wts         []string
	set         Set
	git         func(dir string, args ...string) string
	req         ApplyRequest
}

// newRealChain creates n owned worktrees of one repository (one parent directory holds them
// all), gives each a merged branch of its own, and stores one plan set that retires all of them
// and, with branches, deletes their branches too.
func newRealChain(t *testing.T, n int, branches bool) *realChain {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	root := t.TempDir()
	f := &realChain{repo: filepath.Join(root, "repo"), state: filepath.Join(root, "state")}
	parent := filepath.Join(root, "parent")
	for _, d := range []struct {
		path string
		mode os.FileMode
	}{{f.state, 0700}, {f.repo, 0755}, {parent, 0755}} {
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
	for i := 1; i <= n; i++ {
		wt := filepath.Join(parent, fmt.Sprintf("wt%d", i))
		planned := workspace.PlanCreate(context.Background(), workspace.PlanOptions{Path: f.repo, Base: "refs/heads/main", Destination: wt, Task: fmt.Sprintf("task%d", i), Owner: "claude", StateDir: f.state, CallerDir: f.repo})
		if planned.Plan == nil {
			t.Fatalf("create plan: %+v", planned)
		}
		if r := workspace.Apply(context.Background(), workspace.ApplyOptions{StateDir: f.state, PlanID: planned.Plan.PlanID, Authorize: true}); r.Status != "ok" {
			t.Fatalf("create apply: %+v", r)
		}
		f.git(wt, "switch", "-q", "-c", fmt.Sprintf("feat%d", i))
		f.git(wt, "commit", "-q", "--allow-empty", "-m", fmt.Sprintf("work %d", i))
		f.git(f.repo, "merge", "-q", "--no-ff", "-m", fmt.Sprintf("merge %d", i), fmt.Sprintf("feat%d", i))
		f.wts = append(f.wts, wt)
	}
	planner := NewV3Planner([]RepoSpec{{Name: "r", Path: f.repo, Base: "refs/heads/main"}}, []derive.Store{{Path: f.state}})
	cands, chosen := []tidywizard.Candidate{}, []string{}
	for i, wt := range f.wts {
		c := cand(tidywizard.KindWorktree, "r", fmt.Sprintf("wt%d", i+1), tidywizard.VerdictSafe, "claude", owner.SourceTaskRecord, owner.ScopeOwn)
		c.Path, c.ID, c.Task, c.Branch = wt, "worktree:r:"+wt, fmt.Sprintf("task%d", i+1), fmt.Sprintf("feat%d", i+1)
		c.Effects = []string{"remove-registration", "remove-files"}
		if branches {
			c.Effects = append(c.Effects, "delete-branch:"+c.Branch)
		}
		cands, chosen = append(cands, c), append(chosen, c.ID)
	}
	set, err := Build(context.Background(), Request{Candidates: cands, Chosen: chosen, Mode: human}, planner)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := Store(f.state, set); err != nil {
		t.Fatal(err)
	}
	f.set = set
	f.req = ApplyRequest{StateDirs: []string{f.state}, PlanID: set.PlanID, Mode: human, Authorize: true, Applier: V3Applier{ApplyFunc: workspace.ApplyTidyV3}}
	return f
}

func (f *realChain) requireRetired(t *testing.T, branches bool) {
	t.Helper()
	for i, wt := range f.wts {
		if _, err := os.Stat(wt); !os.IsNotExist(err) {
			t.Errorf("wt%d must be gone", i+1)
		}
		if _, err := os.Stat(filepath.Join(f.repo, ".git", "worktrees", fmt.Sprintf("wt%d", i+1))); !os.IsNotExist(err) {
			t.Errorf("the registration of wt%d must be gone", i+1)
		}
		has := strings.Contains(f.git(f.repo, "branch", "--list", fmt.Sprintf("feat%d", i+1)), "feat")
		if has == branches {
			t.Errorf("branch feat%d present=%t, want %t", i+1, has, !branches)
		}
	}
	want := len(f.wts)
	if branches {
		want *= 2
	}
	if got := len(strings.Fields(f.git(f.repo, "for-each-ref", "--format=%(refname)", "refs/stw/recovery/"))); got != want {
		t.Errorf("%d recovery references, want %d", got, want)
	}
	if got := strings.Count(f.git(f.repo, "worktree", "list", "--porcelain"), "worktree "); got != 1 {
		t.Errorf("only the primary worktree must remain, got %d", got)
	}
}

// Issue 285 end to end: three owned worktrees of one repository in one plan set apply in one run.
func TestApplyRetiresEveryWorktreeOfOneRepositoryInOneRun(t *testing.T) {
	for _, branches := range []bool{false, true} {
		t.Run(fmt.Sprintf("branches=%t", branches), func(t *testing.T) {
			f := newRealChain(t, 3, branches)
			res := Apply(context.Background(), f.req)
			want := 3
			if branches {
				want = 6
			}
			if res.Status != "ok" || len(res.Outcomes) != want {
				t.Fatalf("%+v", res)
			}
			for _, o := range res.Outcomes {
				if o.Status != StatusApplied {
					t.Errorf("%+v", o)
				}
			}
			f.requireRetired(t, branches)
			// The completed set replays as a verified no-op.
			again := Apply(context.Background(), f.req)
			if again.Status != "ok" {
				t.Fatalf("replay: %+v", again)
			}
			f.requireRetired(t, branches)
		})
	}
}

// A change the journals do not explain still turns the later rows stale.
func TestApplyOfASetWithAnUnrelatedChangeStillTurnsLaterRowsStale(t *testing.T) {
	f := newRealChain(t, 3, false)
	// A branch created after planning changes the reference digest of every stored baseline.
	f.git(f.repo, "branch", "unrelated", "HEAD")
	res := Apply(context.Background(), f.req)
	if res.Status != "failed" {
		t.Fatalf("%+v", res)
	}
	for _, o := range res.Outcomes {
		if o.Status != StatusStale || o.Code != "evidence-changed" {
			t.Errorf("%+v", o)
		}
	}
	for i, wt := range f.wts {
		if _, err := os.Stat(wt); err != nil {
			t.Errorf("wt%d was touched: %v", i+1, err)
		}
	}
}
