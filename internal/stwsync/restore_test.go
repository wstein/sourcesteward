// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func restore(t *testing.T, w *workspace, runID string) Result {
	t.Helper()
	return Restore(context.Background(), RestoreOptions{Path: w.ws, RunID: runID})
}

// editRecord is shared with retention_test.go.

// movedWorkspace advances main of the root and runs a sync; it returns the run ID and the old and new tips.
func movedWorkspace(t *testing.T) (w *workspace, id, old, now string) {
	t.Helper()
	w = newWorkspace(t)
	old = w.oid(t, ".", "main")
	w.advance(t, ".", "main")
	r := run(t, w, Options{})
	if r.RunID == nil {
		t.Fatal("no run id")
	}
	return w, *r.RunID, old, w.oid(t, ".", "main")
}

func TestRestoreMovesTheBranchBackAndIsIdempotent(t *testing.T) {
	w, id, old, now := movedWorkspace(t)
	r := restore(t, w, "")
	if r.Error != nil || r.Mode != "restore" || r.RunID == nil || *r.RunID != id || r.ExitCode() != 0 {
		t.Fatalf("%+v", r)
	}
	root := repoByName(t, r)["."]
	if root.Result != ResultRestored || *root.OldOID != now || *root.NewOID != old || w.oid(t, ".", "main") != old || w.oid(t, ".", "HEAD") != old {
		t.Fatalf("%+v", root)
	}
	if runGit(t, w.ws, "status", "--porcelain", "--untracked-files=no") != "" {
		t.Fatal("worktree not at the old tip")
	}
	again := repoByName(t, restore(t, w, ""))["."]
	if again.Result != ResultAlreadyRestored || w.oid(t, ".", "main") != old {
		t.Fatalf("%+v", again)
	}
}

func TestRestoreLeavesDirtyMovedAndBusyBranchesAlone(t *testing.T) {
	cases := map[string]func(t *testing.T, w *workspace){
		"dirty": func(t *testing.T, w *workspace) { write(t, filepath.Join(w.ws, "ws.txt"), "local edit\n") },
		"moved": func(t *testing.T, w *workspace) { commit(t, w.ws, "later.txt", "x\n") },
		"busy": func(t *testing.T, w *workspace) {
			write(t, filepath.Join(w.ws, ".git", "MERGE_HEAD"), w.oid(t, ".", "HEAD")+"\n")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			w, _, _, _ := movedWorkspace(t)
			setup(t, w)
			before := trees(t, []string{w.ws})
			tip := w.oid(t, ".", "main")
			r := restore(t, w, "")
			rp := repoByName(t, r)["."]
			if rp.Result != ResultChangedSince || r.ExitCode() != 1 || w.oid(t, ".", "main") != tip || trees(t, []string{w.ws})[w.ws] != before[w.ws] {
				t.Fatalf("%+v", rp)
			}
		})
	}
}

func TestRestoreBranchNotCheckedOutIsUpdatedAndOneHeldElsewhereIsNot(t *testing.T) {
	w, _, old, now := movedWorkspace(t)
	runGit(t, w.ws, "checkout", "-q", "-b", "other")
	r := repoByName(t, restore(t, w, ""))["."]
	if r.Result != ResultRestored || w.oid(t, ".", "main") != old || w.oid(t, ".", "HEAD") != w.oid(t, ".", "other") {
		t.Fatalf("%+v", r)
	}
	w, _, _, now = movedWorkspace(t)
	runGit(t, w.ws, "checkout", "-q", "-b", "other")
	runGit(t, w.ws, "worktree", "add", "-q", filepath.Join(w.root, "held"), "main")
	r = repoByName(t, restore(t, w, ""))["."]
	if r.Result != ResultChangedSince || w.oid(t, ".", "main") != now {
		t.Fatalf("%+v", r)
	}
}

func TestRestoreIncompleteRecords(t *testing.T) {
	// a fast-forward record (planned new OID): at the new tip it is restored, at the old tip already restored
	w, id, old, _ := movedWorkspace(t)
	editRecord(t, w, w.ws, id, func(r *journalRecord) { r.Entries[0].State, r.Entries[0].NewOID = statePrepared, "" })
	if got := repoByName(t, restore(t, w, ""))["."]; got.Result != ResultRestored || w.oid(t, ".", "main") != old {
		t.Fatalf("%+v", got)
	}
	if got := repoByName(t, restore(t, w, ""))["."]; got.Result != ResultAlreadyRestored {
		t.Fatalf("%+v", got)
	}
	// the dealt-with record is no longer incomplete
	editRecord(t, w, w.ws, id, func(r *journalRecord) {
		if r.Entries[0].State == statePrepared {
			t.Fatal("record still prepared after a restore")
		}
	})
	// anywhere else is changed-since
	w, id, _, _ = movedWorkspace(t)
	editRecord(t, w, w.ws, id, func(r *journalRecord) { r.Entries[0].State, r.Entries[0].NewOID = statePrepared, "" })
	commit(t, w.ws, "later.txt", "x\n")
	tip := w.oid(t, ".", "main")
	if got := repoByName(t, restore(t, w, ""))["."]; got.Result != ResultChangedSince || w.oid(t, ".", "main") != tip {
		t.Fatalf("%+v", got)
	}
	// a record without a planned new OID (rebase, merge) is journal-incomplete and untouched
	w, id, _, now := movedWorkspace(t)
	editRecord(t, w, w.ws, id, func(r *journalRecord) {
		r.Entries[0].State, r.Entries[0].NewOID, r.Entries[0].PlannedNewOID = statePrepared, "", ""
	})
	r := restore(t, w, "")
	if got := repoByName(t, r)["."]; got.Result != ResultJournalIncomplete || r.ExitCode() != 1 || w.oid(t, ".", "main") != now {
		t.Fatalf("%+v", got)
	}
}

func TestRestoreRunSelectionAndRefusals(t *testing.T) {
	w, first, old, _ := movedWorkspace(t)
	time.Sleep(1100 * time.Millisecond) // second-resolution run IDs on this branch
	w.advance(t, ".", "main")
	second := *run(t, w, Options{}).RunID
	if r := restore(t, w, ""); *r.RunID != second {
		t.Fatalf("default is the latest run: %v", *r.RunID)
	}
	if r := restore(t, w, first); *r.RunID != first || repoByName(t, r)["."].Result != ResultRestored || w.oid(t, ".", "main") != old {
		t.Fatalf("%+v", r)
	}
	for _, bad := range []string{"../x", "nope", "20200101T000000Z-00000000"} {
		if r := restore(t, w, bad); r.Error == nil || len(r.Repos) != 0 {
			t.Fatalf("%q: %+v", bad, r)
		}
	}
	empty := newWorkspace(t)
	if r := restore(t, empty, ""); r.Error == nil || r.Error.Code != "sync-restore-no-run" {
		t.Fatalf("%+v", r)
	}
}

// The journal record is data: a ref that is not a branch, a bad OID or a foreign type is failed and nothing moves.
func TestRestoreRefusesTamperedRecords(t *testing.T) {
	for name, fn := range map[string]func(*journalEntry){
		"tag ref":    func(e *journalEntry) { e.Ref = "refs/tags/v1" },
		"option ref": func(e *journalEntry) { e.Ref = "refs/heads/--hard" },
		"bad old":    func(e *journalEntry) { e.OldOID = "HEAD~5" },
		"bad new":    func(e *journalEntry) { e.NewOID = "--force" },
	} {
		t.Run(name, func(t *testing.T) {
			w, id, _, now := movedWorkspace(t)
			editRecord(t, w, w.ws, id, func(r *journalRecord) { fn(&r.Entries[0]) })
			r := restore(t, w, "")
			if got := repoByName(t, r)["."]; got.Result != ResultFailed || r.ExitCode() != 1 || w.oid(t, ".", "main") != now {
				t.Fatalf("%+v", got)
			}
		})
	}
}

// A run that moved the root and a submodule restores both; the gitlink and other submodules are not touched.
func TestRestoreCoversSubmodulesOfTheWorkspace(t *testing.T) {
	w := newWorkspace(t)
	oldRoot, oldA := w.oid(t, ".", "main"), w.oid(t, "a", "main")
	w.advance(t, ".", "main")
	w.advance(t, "a", "main")
	id := *run(t, w, Options{}).RunID
	r := restore(t, w, id)
	by := repoByName(t, r)
	if by["."].Result != ResultRestored || by["a"].Result != ResultRestored || w.oid(t, ".", "main") != oldRoot || w.oid(t, "a", "main") != oldA {
		t.Fatalf("%+v", r.Repos)
	}
}

// Races between the checks and the change: a branch that moves is changed-since, and a worktree that
// becomes dirty keeps its edit (reset --keep keeps a change in a file the restore does not touch; nothing is discarded).
func TestRestoreRacesNeverLoseWork(t *testing.T) {
	defer func() { restoreHook = nil }()
	w, _, _, _ := movedWorkspace(t)
	restoreHook = func(dir string) { write(t, filepath.Join(dir, "ws.txt"), "edited after the check\n") }
	r := repoByName(t, restore(t, w, ""))["."]
	raw, _ := os.ReadFile(filepath.Join(w.ws, "ws.txt"))
	if string(raw) != "edited after the check\n" || r.Result != ResultRestored {
		t.Fatalf("%+v %q", r, raw)
	}
	w, _, _, _ = movedWorkspace(t)
	runGit(t, w.ws, "checkout", "-q", "-b", "other")
	commit(t, w.ws, "racer.txt", "r\n")
	var racer string
	restoreHook = func(dir string) {
		racer = w.oid(t, ".", "other")
		runGit(t, dir, "update-ref", "refs/heads/main", racer)
	}
	r = repoByName(t, restore(t, w, ""))["."]
	if r.Result != ResultChangedSince || w.oid(t, ".", "main") != racer {
		t.Fatalf("%+v", r)
	}
}

// F1: a branch an operation works on is never moved, in the main or in a linked worktree, although HEAD
// is detached there; the user's next `git rebase --continue` must still work.
func TestRestoreRefusesABranchInTheMiddleOfAnOperation(t *testing.T) {
	cases := map[string]func(t *testing.T, w *workspace){
		"rebase in the main worktree": func(t *testing.T, w *workspace) {
			runGit(t, w.ws, "checkout", "-q", "--detach")
			os.MkdirAll(filepath.Join(w.ws, ".git", "rebase-merge"), 0o755)
			write(t, filepath.Join(w.ws, ".git", "rebase-merge", "head-name"), "refs/heads/main\n")
		},
		"rebase in a linked worktree": func(t *testing.T, w *workspace) {
			runGit(t, w.ws, "checkout", "-q", "-b", "other")
			lw := filepath.Join(w.root, "lw")
			runGit(t, w.ws, "worktree", "add", "-q", "--detach", lw)
			dir := filepath.Join(runGit(t, lw, "rev-parse", "--absolute-git-dir"), "rebase-apply")
			os.MkdirAll(dir, 0o755)
			write(t, filepath.Join(dir, "head-name"), "refs/heads/main\n")
		},
		"bisect in a linked worktree": func(t *testing.T, w *workspace) {
			runGit(t, w.ws, "checkout", "-q", "-b", "other")
			lw := filepath.Join(w.root, "lw")
			runGit(t, w.ws, "worktree", "add", "-q", "--detach", lw)
			gd := runGit(t, lw, "rev-parse", "--absolute-git-dir")
			write(t, filepath.Join(gd, "BISECT_LOG"), "git bisect start\n")
			write(t, filepath.Join(gd, "BISECT_START"), "main\n")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			w, _, _, now := movedWorkspace(t)
			setup(t, w)
			r := restore(t, w, "")
			if got := repoByName(t, r)["."]; got.Result != ResultChangedSince || r.ExitCode() != 1 || w.oid(t, ".", "main") != now {
				t.Fatalf("%+v", got)
			}
		})
	}
}

// F2: a failed or conflict entry is classified by where the branch really is.
func TestRestoreClassifiesFailedEntriesByTheBranchPosition(t *testing.T) {
	for _, state := range []string{stateFailed, "conflict"} {
		// the branch moved to the planned new tip (a 'raced' failure after the move): restored
		w, id, old, _ := movedWorkspace(t)
		editRecord(t, w, w.ws, id, func(r *journalRecord) { r.Entries[0].State, r.Entries[0].NewOID = state, "" })
		if got := repoByName(t, restore(t, w, ""))["."]; got.Result != ResultRestored || w.oid(t, ".", "main") != old {
			t.Fatalf("%s at planned tip: %+v", state, got)
		}
		// at the old tip: verified already restored
		if got := repoByName(t, restore(t, w, ""))["."]; got.Result != ResultAlreadyRestored {
			t.Fatalf("%s at old tip: %+v", state, got)
		}
		// anywhere else: changed-since, untouched
		w, id, _, _ = movedWorkspace(t)
		editRecord(t, w, w.ws, id, func(r *journalRecord) { r.Entries[0].State, r.Entries[0].NewOID = state, "" })
		commit(t, w.ws, "later.txt", "x\n")
		tip := w.oid(t, ".", "main")
		if got := repoByName(t, restore(t, w, ""))["."]; got.Result != ResultChangedSince || w.oid(t, ".", "main") != tip {
			t.Fatalf("%s elsewhere: %+v", state, got)
		}
	}
}

// F3: the default run is the highest run ID, also after a restore rewrote an older record.
func TestRestoreDefaultRunIsOrderedByRunIDNotByModificationTime(t *testing.T) {
	w, first, _, _ := movedWorkspace(t)
	editRecord(t, w, w.ws, first, func(r *journalRecord) { r.Entries[0].State, r.Entries[0].NewOID = statePrepared, "" })
	time.Sleep(1100 * time.Millisecond) // second-resolution run IDs on this branch: the later run needs a later second
	w.advance(t, ".", "main")
	second := *run(t, w, Options{}).RunID
	if r := restore(t, w, first); *r.RunID != first {
		t.Fatalf("%v", *r.RunID) // rewrites the record of the older run
	}
	if r := restore(t, w, ""); *r.RunID != second {
		t.Fatalf("the default run is %s, want the highest run ID %s", *r.RunID, second)
	}
}

func TestRunIDOrderMixesBothFormats(t *testing.T) {
	ids := []string{"20261024T201051Z-00000001", "20261024T201050Z-ffffffff", "20261024T201050000001Z-00000000", "20261024T201050999999Z-00000000"}
	var latest string
	for _, id := range ids {
		if latest == "" || runIDLess(latest, id) {
			latest = id
		}
	}
	if latest != "20261024T201051Z-00000001" || !runIDLess("20261024T201050Z-ffffffff", "20261024T201050000001Z-00000000") || runIDLess("20261024T201050999999Z-00000000", "20261024T201050000001Z-00000000") {
		t.Fatal(latest)
	}
}

// F4: a symbolic ref is never followed.
func TestRestoreNeverFollowsASymbolicBranchRef(t *testing.T) {
	w, id, _, now := movedWorkspace(t)
	runGit(t, w.ws, "checkout", "-q", "-b", "other")
	runGit(t, w.ws, "symbolic-ref", "refs/heads/alias", "refs/heads/main")
	editRecord(t, w, w.ws, id, func(r *journalRecord) { r.Entries[0].Ref = "refs/heads/alias" })
	if got := repoByName(t, restore(t, w, ""))["."]; got.Result != ResultChangedSince || w.oid(t, ".", "main") != now {
		t.Fatalf("%+v", got)
	}
}

// F5: a missing old object is a failure of the record, not a branch change.
func TestRestoreWithAMissingOldObjectFails(t *testing.T) {
	w, id, _, now := movedWorkspace(t)
	editRecord(t, w, w.ws, id, func(r *journalRecord) { r.Entries[0].OldOID = "1111111111111111111111111111111111111111" })
	r := restore(t, w, "")
	if got := repoByName(t, r)["."]; got.Result != ResultFailed || r.ExitCode() != 1 || w.oid(t, ".", "main") != now {
		t.Fatalf("%+v", got)
	}
}

// F6: a bad --run is a usage error, not "no such run".
func TestRestoreRunPatternIsCheckedBeforeAnyLookup(t *testing.T) {
	w, _, _, _ := movedWorkspace(t)
	for _, bad := range []string{"../x", "nope", "20261024T201050Z-0000000g"} {
		if r := restore(t, w, bad); r.Error == nil || r.Error.Code != "sync-invalid-options" {
			t.Fatalf("%q: %+v", bad, r.Error)
		}
	}
}

// F6: the branch must have landed on the old tip after reset --keep.
func TestRestoreChecksWhereTheBranchLanded(t *testing.T) {
	defer func() { restoreAfterHook = nil }()
	w, _, _, _ := movedWorkspace(t)
	restoreAfterHook = func(dir string) { commit(t, dir, "after.txt", "x\n") }
	if got := repoByName(t, restore(t, w, ""))["."]; got.Result != ResultFailed {
		t.Fatalf("%+v", got)
	}
}

// F7: a failed or conflict entry with no planned and no new tip (a rebase) whose branch is not at the old
// tip is changed-since, never reset: the owner may have finished the rebase after a conflict stop.
func TestRestoreFailedRebaseEntryWithoutATipIsLeftAlone(t *testing.T) {
	for _, state := range []string{stateFailed, "conflict"} {
		w, id, _, now := movedWorkspace(t)
		editRecord(t, w, w.ws, id, func(r *journalRecord) {
			r.Entries[0].State, r.Entries[0].NewOID, r.Entries[0].PlannedNewOID = state, "", ""
		})
		before := trees(t, []string{w.ws})
		r := restore(t, w, "")
		if got := repoByName(t, r)["."]; got.Result != ResultChangedSince || r.ExitCode() != 1 || w.oid(t, ".", "main") != now || trees(t, []string{w.ws})[w.ws] != before[w.ws] {
			t.Fatalf("%s: %+v", state, got)
		}
	}
}

// Restore picks the latest run by time: an old-format ID and a microsecond ID of the same second sort by
// their time stamp, not as raw text ('Z' sorts after the digits).
func TestRestoreDefaultRunOrdersMixedRunIDFormatsByTime(t *testing.T) {
	w := newWorkspace(t)
	if err := os.MkdirAll(journalDir(w), 0o700); err != nil {
		t.Fatal(err)
	}
	older, newer := "20260101T120000Z-ffffffff", "20260101T120000500000Z-00000000"
	fakeRun(t, w, older)
	fakeRun(t, w, newer)
	if r := restore(t, w, ""); r.RunID == nil || *r.RunID != newer {
		t.Fatalf("%v", r.RunID)
	}
}
