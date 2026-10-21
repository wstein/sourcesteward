// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readJournals(t *testing.T, gitDir string) []journalRecord {
	t.Helper()
	dir := filepath.Join(gitDir, "stw", "sync")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []journalRecord
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var rec journalRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			t.Fatal(err)
		}
		out = append(out, rec)
	}
	return out
}

// The journal and the recovery ref exist, with the branch still at its old tip, at the moment Git
// prepares the ref change, and the record is completed afterwards.
func TestJournalIsWrittenBeforeTheRefChangeAndCompletedAfter(t *testing.T) {
	w := newWorkspace(t)
	w.advance(t, ".", "main")
	old := w.oid(t, ".", "main")
	snap := filepath.Join(w.root, "snapshot")
	hookScript(t, filepath.Join(w.ws, ".git"), "reference-transaction", "[ \"$1\" = prepared ] || exit 0\nwhile read o n ref; do case \"$ref\" in refs/heads/main) cat \"$(git rev-parse --git-common-dir)\"/stw/sync/*.json > '"+snap+"'; git for-each-ref refs/stw/recovery/sync > '"+snap+".refs';; esac; done\n")
	r := run(t, w, Options{})
	if r.RunID == nil {
		t.Fatal("a run that changed a branch has a run ID")
	}
	raw, err := os.ReadFile(snap)
	if err != nil {
		t.Fatalf("no journal when the ref changed: %v", err)
	}
	var before journalRecord
	if err := json.Unmarshal(raw, &before); err != nil || before.Type != "sync" || before.RunID != *r.RunID || len(before.Entries) != 1 || before.Entries[0].State != statePrepared || before.Entries[0].OldOID != old || before.Entries[0].PlannedNewOID == "" {
		t.Fatalf("%v %+v", err, before)
	}
	refs, _ := os.ReadFile(snap + ".refs")
	if !strings.Contains(string(refs), old) || !strings.Contains(string(refs), "refs/stw/recovery/sync/"+*r.RunID+"/1") {
		t.Fatalf("recovery ref: %q", refs)
	}
	rec := readJournals(t, filepath.Join(w.ws, ".git"))[0]
	e := rec.Entries[0]
	if e.State != stateCompleted || e.NewOID != w.oid(t, ".", "main") || e.OrigHead != old || runGit(t, w.ws, "rev-parse", e.RecoveryRef) != old {
		t.Fatalf("%+v", e)
	}
}

func TestNoJournalForPlansOrUnchangedRuns(t *testing.T) {
	w := newWorkspace(t)
	run(t, w, Options{Plan: true})
	run(t, w, Options{})
	w.advance(t, ".", "main")
	run(t, w, Options{Plan: true})
	if _, err := os.Stat(filepath.Join(w.ws, ".git", "stw")); !os.IsNotExist(err) {
		t.Fatal("journal written without a branch change")
	}
	if runGit(t, w.ws, "for-each-ref", "refs/stw") != "" {
		t.Fatal("recovery ref without a branch change")
	}
}

func TestFailedJournalWriteStopsTheChange(t *testing.T) {
	w := newWorkspace(t)
	w.advance(t, ".", "main")
	old := w.oid(t, ".", "HEAD")
	write(t, filepath.Join(w.ws, ".git", "stw"), "a file where the journal directory belongs\n")
	g := repoByName(t, run(t, w, Options{}))["."]
	if g.Result != ResultFailed || w.oid(t, ".", "HEAD") != old || !strings.HasPrefix(*g.Reason, "journal-failed") {
		t.Fatalf("%+v", g)
	}
}

// A lost race (HEAD moved after the plan) is failed, never repaired, and the other repositories proceed.
func TestLostRaceIsFailedAndNeverRepaired(t *testing.T) {
	w := newWorkspace(t)
	w.advance(t, ".", "main")
	w.advance(t, "a", "main")
	applyHook = func(dir string) {
		if dir == w.ws {
			commit(t, w.ws, "racer.txt", "x\n")
		}
	}
	t.Cleanup(func() { applyHook = nil })
	racer := ""
	got := repoByName(t, run(t, w, Options{}))
	if got["."].Result != ResultFailed || *got["."].Reason != "head-moved" {
		t.Fatalf("%+v", got["."])
	}
	racer = runGit(t, w.ws, "log", "-1", "--format=%s")
	if !strings.Contains(racer, "racer") || runGit(t, w.ws, "rev-list", "--count", "origin/main..HEAD") != "1" {
		t.Error("the racing commit was repaired away")
	}
	if got["a"].Result != ResultFastForwarded {
		t.Errorf("a: %+v", got["a"])
	}
	if j := readJournals(t, filepath.Join(w.ws, ".git"))[0]; j.Entries[0].State != stateFailed {
		t.Errorf("%+v", j)
	}
}

func TestOrigHeadAndPlannedTipAreCheckedAfterTheMerge(t *testing.T) {
	for name, hook := range map[string]string{
		"orig-head": "git update-ref ORIG_HEAD \"$(git rev-parse HEAD)\"\n",
		"moved-tip": "git update-ref refs/heads/main \"$(git rev-parse HEAD~1)\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorkspace(t)
			w.advance(t, ".", "main")
			w.advance(t, ".", "main")
			hookScript(t, filepath.Join(w.ws, ".git"), "post-merge", hook)
			g := repoByName(t, run(t, w, Options{}))["."]
			if g.Result != ResultFailed || *g.Reason != "raced" {
				t.Fatalf("%+v %v", g, deref(g.Reason))
			}
		})
	}
}

// A hook that makes git exit non-zero fails only that repository.
func TestHookFailureFailsOnlyThatRepository(t *testing.T) {
	w := newWorkspace(t)
	w.advance(t, "a", "main")
	w.advance(t, "b", "main")
	oldA := w.oid(t, "a", "HEAD")
	hookScript(t, filepath.Join(w.ws, ".git", "modules", "a"), "reference-transaction", "[ \"$1\" = prepared ] || exit 0\nwhile read o n ref; do [ \"$ref\" = refs/heads/main ] && exit 1; done\nexit 0\n")
	r := run(t, w, Options{})
	got := repoByName(t, r)
	if got["a"].Result != ResultFailed || w.oid(t, "a", "HEAD") != oldA || got["b"].Result != ResultFastForwarded || r.ExitCode() != 1 {
		t.Fatalf("%+v %+v", got["a"], got["b"])
	}
}

// Decision 8: rebase.updateRefs=true must not move another branch that stands on the rebased one.
func TestRebaseUpdateRefsConfigDoesNotMoveStackedBranches(t *testing.T) {
	w := newWorkspace(t)
	w.advance(t, ".", "main")
	runGit(t, w.ws, "config", "pull.rebase", "true")
	runGit(t, w.ws, "config", "rebase.updateRefs", "true")
	commit(t, w.ws, "local.txt", "l\n")
	runGit(t, w.ws, "branch", "stacked")
	commit(t, w.ws, "local2.txt", "l2\n")
	stacked := w.oid(t, ".", "stacked")
	got := repoByName(t, run(t, w, Options{}))
	if got["."].Result != ResultRebased {
		t.Fatalf("%+v %v", got["."], deref(got["."].Reason))
	}
	if w.oid(t, ".", "stacked") != stacked {
		t.Error("rebase.updateRefs moved another branch")
	}
}

func TestSubmoduleRecurseDoesNotMoveSubmoduleHeads(t *testing.T) {
	w := newWorkspace(t)
	adv := filepath.Join(w.root, "bump")
	runGit(t, w.root, "clone", "-q", w.bare["ws"], adv)
	runGit(t, adv, "submodule", "update", "--init", "-q", "b")
	commit(t, filepath.Join(adv, "b"), "b-new.txt", "n\n")
	runGit(t, filepath.Join(adv, "b"), "push", "-q", "origin", "HEAD:refs/heads/side")
	runGit(t, adv, "add", "b")
	runGit(t, adv, "commit", "-q", "-m", "bump b")
	runGit(t, adv, "push", "-q", "origin", "main")
	runGit(t, filepath.Join(w.ws, "b"), "fetch", "-q", "origin")
	head := w.oid(t, "b", "HEAD")
	runGit(t, w.ws, "config", "submodule.recurse", "true")
	g := repoByName(t, run(t, w, Options{}))
	if g["."].Result != ResultFastForwarded || w.oid(t, "b", "HEAD") != head || g["b"].Result != ResultUpToDate {
		t.Fatalf("%+v; sub HEAD %s want %s", g["."], w.oid(t, "b", "HEAD"), head)
	}
}

func TestNoMergeCommitAndNoLostCommitInAnyRepository(t *testing.T) {
	for _, o := range []Options{{}, {Rebase: On}, {Rebase: On, Autostash: On}, {Rebase: Off}} {
		w := newWorkspace(t)
		for _, n := range []string{".", "a", "b"} {
			commit(t, w.dir(n), n+"-local.txt", "l\n")
			w.advance(t, n, "main")
		}
		tips := map[string]string{}
		for _, n := range []string{".", "a", "b"} {
			tips[n] = w.oid(t, n, "HEAD")
		}
		run(t, w, o)
		for n, tip := range tips {
			if c := runGit(t, w.dir(n), "rev-list", "--merges", "--count", "HEAD"); c != "0" {
				t.Errorf("%+v %s: merge commit", o, n)
			}
			if o.Rebase == On && !fileExists(filepath.Join(w.dir(n), n+"-local.txt")) {
				t.Errorf("%s lost its local commit", n)
			}
			if o.Rebase != On && w.oid(t, n, "HEAD") != tip {
				t.Errorf("%+v %s: a diverged branch changed without --rebase", o, n)
			}
		}
	}
}

func TestSyncIgnoresInheritedRepositoryVariables(t *testing.T) {
	w := newWorkspace(t)
	decoy := t.TempDir()
	runGit(t, decoy, "init", "-q", "-b", "main", ".")
	t.Setenv("GIT_DIR", filepath.Join(decoy, ".git"))
	t.Setenv("GIT_WORK_TREE", decoy)
	if r := Sync(context.Background(), Options{Path: w.ws, Plan: true}); r.Error != nil || len(r.Repos) != 6 {
		t.Fatalf("%+v", r)
	}
}
