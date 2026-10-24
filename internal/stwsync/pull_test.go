// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, w *workspace, o Options) Result {
	t.Helper()
	o.Path = w.ws
	r := Sync(context.Background(), o)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	return r
}

// TestSyncFollowsGitPull is the configuration matrix: pull.rebase (true, false, unset) with a clean or
// dirty (non-conflicting) worktree, for a fast-forward and a diverged branch. A dirty tree is only
// acceptable to git when the update does not touch its files, or with autostash.
func TestSyncFollowsGitPull(t *testing.T) {
	type tc struct {
		rebase    string // "", "true", "false"
		autostash string // rebase.autoStash
		dirty     bool
		diverged  bool
		want      string
	}
	var cases []tc
	for _, rb := range []string{"", "true", "false"} {
		for _, dirty := range []bool{false, true} {
			for _, diverged := range []bool{false, true} {
				want := ResultFastForwarded
				switch {
				case rb == "true" && dirty:
					want = ResultSkippedDirty // git pull --rebase refuses a dirty tree without autostash, even for a fast-forward
				case diverged && rb == "":
					want = ResultDiverged
				case diverged && rb == "false":
					want = ResultDiverged // owner decision 2026-10-24: pull.rebase=false never makes stw sync merge
				case diverged && rb == "true" && dirty:
					want = ResultSkippedDirty // git pull --rebase refuses a dirty tree without autostash
				case diverged && rb == "true":
					want = ResultRebased
				}
				cases = append(cases, tc{rb, "", dirty, diverged, want})
			}
		}
	}
	cases = append(cases, tc{"true", "true", true, true, ResultRebased}, tc{"true", "true", true, false, ResultFastForwarded})
	for _, c := range cases {
		name := "pull.rebase=" + c.rebase + "/autoStash=" + c.autostash + "/dirty=" + map[bool]string{true: "y", false: "n"}[c.dirty] + "/diverged=" + map[bool]string{true: "y", false: "n"}[c.diverged]
		t.Run(name, func(t *testing.T) {
			w := newWorkspace(t)
			if c.rebase != "" {
				runGit(t, w.ws, "config", "pull.rebase", c.rebase)
			}
			if c.autostash != "" {
				runGit(t, w.ws, "config", "rebase.autoStash", c.autostash)
			}
			if c.diverged {
				commit(t, w.ws, "local.txt", "local\n")
			}
			w.advance(t, ".", "main")
			w.advance(t, ".", "main")
			if c.dirty {
				write(t, filepath.Join(w.ws, "ws.txt"), "local edit\n")
				write(t, filepath.Join(w.ws, "staged.txt"), "s\n")
				runGit(t, w.ws, "add", "staged.txt")
				write(t, filepath.Join(w.ws, "staged.txt"), "s, then edited\n")
			}
			status := runGit(t, w.ws, "status", "--porcelain")
			oldHead := w.oid(t, ".", "HEAD")
			up := w.oid(t, ".", "origin/main")
			_ = up
			r := run(t, w, Options{})
			got := repoByName(t, r)["."]
			if got.Result != c.want {
				t.Fatalf("got %s (%v), want %s", got.Result, deref(got.Reason), c.want)
			}
			if n := runGit(t, w.ws, "rev-list", "--merges", "--count", "HEAD"); n != "0" {
				t.Errorf("%s merge commits, want none", n)
			}
			switch c.want {
			case ResultFastForwarded, ResultRebased:
				if w.oid(t, ".", "HEAD") == oldHead {
					t.Error("HEAD did not move")
				}
				if err := gitCmd(w.ws, "merge-base", "--is-ancestor", w.oid(t, ".", "origin/main"), "HEAD").Run(); err != nil {
					t.Error("the upstream is not contained")
				}
				if c.diverged && !fileExists(filepath.Join(w.ws, "local.txt")) {
					t.Error("a local commit was lost")
				}
				if runGit(t, w.ws, "stash", "list") != "" {
					t.Error("a stash entry is left over")
				}
				if c.dirty {
					// git's own autostash returns the changes to the worktree (it does not keep the index split)
					for f, want := range map[string]string{"ws.txt": "local edit\n", "staged.txt": "s, then edited\n"} {
						if b, _ := os.ReadFile(filepath.Join(w.ws, f)); string(b) != want {
							t.Errorf("%s lost its local change: %q", f, b)
						}
					}
				}
			default:
				if w.oid(t, ".", "HEAD") != oldHead || runGit(t, w.ws, "status", "--porcelain") != status {
					t.Error("a skipped repository changed")
				}
			}
			if (c.want == ResultDiverged) && (got.Reason == nil || !strings.Contains(*got.Reason, "needs a choice")) {
				t.Errorf("reason: %v", deref(got.Reason))
			}
			if r.ExitCode() != 0 {
				t.Errorf("exit %d", r.ExitCode())
			}
		})
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestSyncFlagsOverrideTheConfiguration(t *testing.T) {
	w := newWorkspace(t)
	commit(t, w.ws, "local.txt", "local\n")
	w.advance(t, ".", "main")
	runGit(t, w.ws, "config", "pull.rebase", "true")
	before := w.oid(t, ".", "HEAD")
	if g := repoByName(t, run(t, w, Options{Rebase: Off}))["."]; g.Result != ResultDiverged || w.oid(t, ".", "HEAD") != before {
		t.Fatalf("--ff-only: %+v", g)
	}
	runGit(t, w.ws, "config", "pull.rebase", "false")
	if g := repoByName(t, run(t, w, Options{Rebase: On}))["."]; g.Result != ResultRebased {
		t.Fatalf("--rebase: %+v", g)
	}
	// pull.ff=only: a diverged branch is an error for git pull, so it is reported, not rebased or merged
	w2 := newWorkspace(t)
	commit(t, w2.ws, "local.txt", "local\n")
	w2.advance(t, ".", "main")
	runGit(t, w2.ws, "config", "pull.ff", "only")
	if g := repoByName(t, run(t, w2, Options{}))["."]; g.Result != ResultDiverged || !strings.Contains(*g.Reason, "pull.ff=only") {
		t.Fatalf("%+v", g)
	}
	// branch.NAME.rebase wins over pull.rebase
	w3 := newWorkspace(t)
	commit(t, w3.ws, "local.txt", "local\n")
	w3.advance(t, ".", "main")
	runGit(t, w3.ws, "config", "pull.rebase", "false")
	runGit(t, w3.ws, "config", "branch.main.rebase", "true")
	if g := repoByName(t, run(t, w3, Options{}))["."]; g.Result != ResultRebased {
		t.Fatalf("%+v", g)
	}
}

func TestSyncDevcontainerConfigRebasesAndAutostashesLikePull(t *testing.T) {
	w := newWorkspace(t)
	for k, v := range map[string]string{"pull.rebase": "true", "rebase.autoStash": "true", "fetch.prune": "true"} {
		runGit(t, w.ws, "config", k, v)
		runGit(t, w.dir("a"), "config", k, v) // the real configuration is global
	}
	runGit(t, w.ws, "push", "-q", "origin", "main:gone")
	runGit(t, w.ws, "fetch", "-q", "origin")
	runGit(t, w.root, "--git-dir", w.bare["ws"], "branch", "-q", "-D", "gone")
	commit(t, w.ws, "local.txt", "local\n")
	w.advance(t, ".", "main")
	commit(t, w.dir("a"), "a-local.txt", "l\n")
	w.advance(t, "a", "main")
	write(t, filepath.Join(w.ws, "ws.txt"), "dirty\n")
	write(t, filepath.Join(w.dir("a"), "a.txt"), "dirty a\n")
	r := run(t, w, Options{})
	got := repoByName(t, r)
	if got["."].Result != ResultRebased || got["a"].Result != ResultRebased || got["."].Pruned != 1 {
		t.Fatalf("%+v %+v", got["."], got["a"])
	}
	for _, d := range []string{w.ws, w.dir("a")} {
		if !strings.Contains(runGit(t, d, "status", "--porcelain"), "M ") || runGit(t, d, "stash", "list") != "" {
			t.Errorf("%s: dirty state not restored: status %q stash %q", d, runGit(t, d, "status", "--porcelain"), runGit(t, d, "stash", "list"))
		}
	}
	if runGit(t, w.ws, "branch", "-r", "--list", "origin/gone") != "" {
		t.Error("fetch.prune was not honoured")
	}
}

func TestSyncRebaseConflictStopsLikeGit(t *testing.T) {
	w := newWorkspace(t)
	runGit(t, w.ws, "config", "pull.rebase", "true")
	commit(t, w.ws, "ws.txt", "local change\n")
	w.advanceFile(t, ".", "main", "ws.txt")
	w.advance(t, "a", "main")
	oldMain := w.oid(t, ".", "main")
	r := run(t, w, Options{})
	got := repoByName(t, r)
	g := got["."]
	if g.Result != ResultConflict || g.Detail == nil || !strings.Contains(*g.Detail, "git rebase --continue") || !strings.Contains(*g.Reason, "git rebase --continue") {
		t.Fatalf("%+v %v", g, deref(g.Detail))
	}
	if !fileExists(filepath.Join(w.ws, ".git", "rebase-merge")) && !fileExists(filepath.Join(w.ws, ".git", "rebase-apply")) {
		t.Error("the rebase must be left in progress, as git leaves it")
	}
	if w.oid(t, ".", "refs/heads/main") != oldMain {
		t.Error("the branch ref must not move before the rebase finishes")
	}
	if got["a"].Result != ResultFastForwarded {
		t.Errorf("the other repositories go on: %+v", got["a"])
	}
	if r.ExitCode() != 1 {
		t.Error("a conflict exits non-zero")
	}
	// the next run reports the unfinished rebase and touches nothing
	if g := repoByName(t, run(t, w, Options{}))["."]; g.Result != ResultSkippedBusy {
		t.Errorf("%+v", g)
	}
	runGit(t, w.ws, "rebase", "--abort")
	if w.oid(t, ".", "HEAD") != oldMain {
		t.Error("abort must return to the old tip")
	}
}

func TestSyncAutostashConflictKeepsTheStash(t *testing.T) {
	w := newWorkspace(t)
	w.advanceFile(t, ".", "main", "ws.txt")
	write(t, filepath.Join(w.ws, "ws.txt"), "local edit conflicting with the update\n")
	r := run(t, w, Options{Autostash: On})
	g := repoByName(t, r)["."]
	if g.Result != ResultAutostashConflict || g.Detail == nil || runGit(t, w.ws, "stash", "list") == "" {
		t.Fatalf("%+v", g)
	}
	if w.oid(t, ".", "HEAD") != *g.NewOID || r.ExitCode() != 1 {
		t.Error("the branch moved; the owner resolves the stash")
	}
}

func TestSyncUntrackedFileCollisionIsSkippedWithGitsReason(t *testing.T) {
	w := newWorkspace(t)
	w.advanceFile(t, ".", "main", "collide.txt")
	write(t, filepath.Join(w.ws, "collide.txt"), "mine\n")
	old := w.oid(t, ".", "HEAD")
	g := repoByName(t, run(t, w, Options{}))["."]
	if g.Result != ResultSkippedDirty || g.Reason == nil || !strings.Contains(*g.Reason, "collide.txt") || w.oid(t, ".", "HEAD") != old {
		t.Fatalf("%+v %v", g, deref(g.Reason))
	}
	if b, _ := os.ReadFile(filepath.Join(w.ws, "collide.txt")); string(b) != "mine\n" {
		t.Error("the untracked file changed")
	}
}

func TestSyncPruneFollowsFetchConfigAndTheFlags(t *testing.T) {
	for name, tc := range map[string]struct {
		config string
		prune  Tri
		want   int
	}{"flag": {"", On, 1}, "config": {"true", Unset, 1}, "none": {"", Unset, 0}, "no-prune flag": {"true", Off, 0}} {
		t.Run(name, func(t *testing.T) {
			w := newWorkspace(t)
			runGit(t, w.ws, "push", "-q", "origin", "main:gone")
			runGit(t, w.ws, "fetch", "-q", "origin")
			runGit(t, w.root, "--git-dir", w.bare["ws"], "branch", "-q", "-D", "gone")
			if tc.config != "" {
				runGit(t, w.ws, "config", "fetch.prune", tc.config)
			}
			g := repoByName(t, run(t, w, Options{Prune: tc.prune}))["."]
			if g.Pruned != tc.want {
				t.Errorf("pruned %d, want %d", g.Pruned, tc.want)
			}
			if exists := runGit(t, w.ws, "branch", "-r", "--list", "origin/gone") != ""; exists == (tc.want == 1) {
				t.Errorf("remote-tracking ref present=%v", exists)
			}
			runGit(t, w.ws, "rev-parse", "--verify", "refs/heads/main") // a local branch is never deleted
		})
	}
}

func TestSyncRemoteBranchGoneIsReportedAndKept(t *testing.T) {
	w := newWorkspace(t)
	runGit(t, w.dir("b"), "checkout", "-q", "-b", "topic")
	runGit(t, w.dir("b"), "push", "-q", "-u", "origin", "topic")
	runGit(t, w.root, "--git-dir", w.bare["b"], "branch", "-q", "-D", "topic")
	g := repoByName(t, run(t, w, Options{Prune: On}))["b"]
	if g.Result != ResultSkippedNoUpstream || !strings.Contains(*g.Reason, "no longer exists") {
		t.Fatalf("%+v", g)
	}
	runGit(t, w.dir("b"), "rev-parse", "--verify", "refs/heads/topic")
}

func TestPlanPredictsWhatARunWouldDoWithADirtyTree(t *testing.T) {
	w := newWorkspace(t)
	w.advanceFile(t, ".", "main", "ws.txt")
	w.advance(t, "a", "main")
	write(t, filepath.Join(w.ws, "ws.txt"), "conflicting local edit\n")
	write(t, filepath.Join(w.dir("a"), "a.txt"), "unrelated local edit\n")
	got := repoByName(t, run(t, w, Options{Plan: true}))
	if got["."].Result != ResultSkippedDirty || got["a"].Result != ResultFastForward {
		t.Fatalf("%+v %+v", got["."], got["a"])
	}
	if got := repoByName(t, run(t, w, Options{Plan: true, Autostash: On})); got["."].Result != ResultFastForward {
		t.Fatalf("%+v", got["."])
	}
}

// Owner decision 2026-10-24: a merge commit exists only with the explicit --merge flag; no Git
// configuration enables it.
func TestSyncNeverMergesWithoutTheMergeFlag(t *testing.T) {
	for _, cfg := range []map[string]string{
		{}, {"pull.rebase": "false"}, {"pull.ff": "false"}, {"pull.ff": "true"},
		{"pull.rebase": "false", "pull.ff": "false"}, {"branch.main.rebase": "false", "pull.rebase": "false"},
		{"merge.ff": "false"}, {"branch.main.mergeOptions": "--no-ff"},
	} {
		w := newWorkspace(t)
		for k, v := range cfg {
			runGit(t, w.ws, "config", k, v)
		}
		commit(t, w.ws, "local.txt", "l\n")
		w.advance(t, ".", "main")
		old := w.oid(t, ".", "HEAD")
		plan := repoByName(t, run(t, w, Options{Plan: true}))["."]
		g := repoByName(t, run(t, w, Options{}))["."]
		if g.Result != ResultDiverged || plan.Result != ResultDiverged || w.oid(t, ".", "HEAD") != old || runGit(t, w.ws, "rev-list", "--merges", "--count", "HEAD") != "0" {
			t.Fatalf("%v: %+v", cfg, g)
		}
		if g.Reason == nil || !strings.Contains(*g.Reason, "needs a choice") || !strings.Contains(*g.Reason, "--rebase") || !strings.Contains(*g.Reason, "--merge") {
			t.Errorf("%v: the hint must name --rebase and --merge: %v", cfg, deref(g.Reason))
		}
	}
}

// A fast-forwardable branch fast-forwards in every case, also with --merge and pull.ff=false: no merge commit.
func TestSyncFastForwardsWhenPossibleEvenWithMergeSettings(t *testing.T) {
	for _, tc := range []struct {
		cfg  map[string]string
		opts Options
	}{{nil, Options{}}, {map[string]string{"pull.ff": "false"}, Options{}}, {map[string]string{"pull.rebase": "false"}, Options{}}, {nil, Options{Merge: true}}, {map[string]string{"pull.ff": "false"}, Options{Merge: true}},
		// Git's own merge settings must not create a merge commit on the fast-forward path either (it runs --ff-only)
		{map[string]string{"merge.ff": "false"}, Options{}}, {map[string]string{"branch.main.mergeOptions": "--no-ff"}, Options{}},
		{map[string]string{"merge.ff": "false"}, Options{Merge: true}}, {map[string]string{"branch.main.mergeOptions": "--no-ff"}, Options{Merge: true}}} {
		w := newWorkspace(t)
		for k, v := range tc.cfg {
			runGit(t, w.ws, "config", k, v)
		}
		w.advance(t, ".", "main")
		plan := tc.opts
		plan.Plan = true
		if p := repoByName(t, run(t, w, plan))["."]; p.Result != ResultFastForward {
			t.Fatalf("%v %+v: plan %+v", tc.cfg, tc.opts, p)
		}
		if g := repoByName(t, run(t, w, tc.opts))["."]; g.Result != ResultFastForwarded || runGit(t, w.ws, "rev-list", "--merges", "--count", "HEAD") != "0" {
			t.Fatalf("%v %+v: %+v", tc.cfg, tc.opts, g)
		}
	}
}

func TestSyncMergeFlagMergesADivergedBranch(t *testing.T) {
	w := newWorkspace(t)
	commit(t, w.ws, "local.txt", "l\n")
	w.advance(t, ".", "main")
	old := w.oid(t, ".", "HEAD")
	if p := repoByName(t, run(t, w, Options{Merge: true, Plan: true}))["."]; p.Result != ResultMerge || w.oid(t, ".", "HEAD") != old {
		t.Fatalf("plan: %+v", p)
	}
	g := repoByName(t, run(t, w, Options{Merge: true}))["."]
	if g.Result != ResultMerged || runGit(t, w.ws, "rev-list", "--merges", "--count", "HEAD") != "1" || !fileExists(filepath.Join(w.ws, "local.txt")) {
		t.Fatalf("%+v %v", g, deref(g.Reason))
	}
	if err := gitCmd(w.ws, "merge-base", "--is-ancestor", old, "HEAD").Run(); err != nil {
		t.Error("the local commit is not contained")
	}
	// --merge wins over pull.rebase=true and pull.ff=only; the flags themselves exclude each other
	for _, cfg := range []map[string]string{{"pull.rebase": "true"}, {"pull.ff": "only"}} {
		w := newWorkspace(t)
		for k, v := range cfg {
			runGit(t, w.ws, "config", k, v)
		}
		commit(t, w.ws, "local.txt", "l\n")
		w.advance(t, ".", "main")
		if g := repoByName(t, run(t, w, Options{Merge: true}))["."]; g.Result != ResultMerged {
			t.Fatalf("%v: %+v", cfg, g)
		}
	}
	for _, o := range []Options{{Merge: true, Rebase: On}, {Merge: true, Rebase: Off}} {
		if ValidateOptions(o) == nil {
			t.Errorf("%+v accepted", o)
		}
	}
}

// The merge of --merge never opens an editor (--no-edit), even when Git is told to always edit.
func TestSyncMergeNeverOpensAnEditor(t *testing.T) {
	t.Setenv("GIT_MERGE_AUTOEDIT", "yes")
	t.Setenv("GIT_EDITOR", "false")
	w := newWorkspace(t)
	commit(t, w.ws, "local.txt", "l\n")
	w.advance(t, ".", "main")
	if g := repoByName(t, run(t, w, Options{Merge: true}))["."]; g.Result != ResultMerged {
		t.Fatalf("%+v %v", g, deref(g.Reason))
	}
}

// The merge of --merge is `git merge --ff`: Git's own settings merge.ff=only and --ff-only in
// branch.NAME.mergeOptions must not turn the explicit --merge into a refusal.
func TestSyncMergeIgnoresGitsFastForwardOnlySettings(t *testing.T) {
	for _, cfg := range []map[string]string{{"merge.ff": "only"}, {"branch.main.mergeOptions": "--ff-only"}} {
		w := newWorkspace(t)
		for k, v := range cfg {
			runGit(t, w.ws, "config", k, v)
		}
		commit(t, w.ws, "local.txt", "l\n")
		w.advance(t, ".", "main")
		if g := repoByName(t, run(t, w, Options{Merge: true}))["."]; g.Result != ResultMerged {
			t.Fatalf("%v: %+v %v", cfg, g, deref(g.Reason))
		}
	}
}

// Deliberately stricter than git pull --no-rebase (F4): --merge refuses a dirty file even when only the
// local commits touch it; the refusal is safe and documented.
func TestSyncMergeRefusesADirtyFileTheLocalCommitsTouch(t *testing.T) {
	w := newWorkspace(t)
	commit(t, w.ws, "ws.txt", "local commit\n")
	w.advance(t, ".", "main")
	write(t, filepath.Join(w.ws, "ws.txt"), "dirty edit\n")
	before := w.oid(t, ".", "HEAD")
	g := repoByName(t, run(t, w, Options{Merge: true}))["."]
	if g.Result != ResultSkippedDirty || w.oid(t, ".", "HEAD") != before {
		t.Fatalf("%+v", g)
	}
	if b, _ := os.ReadFile(filepath.Join(w.ws, "ws.txt")); string(b) != "dirty edit\n" {
		t.Error("the dirty file changed")
	}
}

func TestSyncMergeConflictStopsAsGitStops(t *testing.T) {
	w := newWorkspace(t)
	commit(t, w.ws, "ws.txt", "local\n")
	w.advanceFile(t, ".", "main", "ws.txt")
	r := run(t, w, Options{Merge: true})
	if g := repoByName(t, r)["."]; g.Result != ResultConflict || !strings.Contains(*g.Reason, "git merge --abort") || r.ExitCode() != 1 {
		t.Fatalf("%+v", g)
	}
}

func TestSyncRebaseByConfigurationOrFlag(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg  map[string]string
		opts Options
	}{"pull.rebase=true": {map[string]string{"pull.rebase": "true"}, Options{}}, "branch.rebase=true": {map[string]string{"branch.main.rebase": "true", "pull.rebase": "false"}, Options{}}, "--rebase": {map[string]string{"pull.rebase": "false"}, Options{Rebase: On}}} {
		w := newWorkspace(t)
		for k, v := range tc.cfg {
			runGit(t, w.ws, "config", k, v)
		}
		commit(t, w.ws, "local.txt", "l\n")
		w.advance(t, ".", "main")
		if g := repoByName(t, run(t, w, tc.opts))["."]; g.Result != ResultRebased || runGit(t, w.ws, "rev-list", "--merges", "--count", "HEAD") != "0" {
			t.Fatalf("%s: %+v", name, g)
		}
	}
}

func TestPlanRefusesRefspecsThatWriteLocalRefsButARunFollowsGitPull(t *testing.T) {
	w := newWorkspace(t)
	runGit(t, w.ws, "push", "-q", "origin", "main:side")
	runGit(t, w.ws, "config", "--add", "remote.origin.fetch", "+refs/heads/side:refs/heads/side")
	w.advance(t, ".", "main")
	before := runGit(t, w.ws, "for-each-ref", "refs/heads", "refs/remotes")
	g := repoByName(t, run(t, w, Options{Plan: true}))["."]
	if g.Result != ResultFailed || !strings.Contains(*g.Reason, "sync-refspec-unsafe") || runGit(t, w.ws, "for-each-ref", "refs/heads", "refs/remotes") != before {
		t.Fatalf("%+v", g)
	}
	if g := repoByName(t, run(t, w, Options{}))["."]; g.Result != ResultFastForwarded {
		t.Fatalf("a real run follows git pull: %+v %v", g, deref(g.Reason))
	}
}
