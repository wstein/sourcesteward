// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
