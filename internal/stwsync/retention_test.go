// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func journalDir(w *workspace) string { return filepath.Join(w.ws, ".git", "stw", "sync") }

func journalFiles(t *testing.T, w *workspace) []string {
	t.Helper()
	entries, err := os.ReadDir(journalDir(w))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func recoveryRefs(t *testing.T, w *workspace) []string {
	t.Helper()
	out := runGit(t, w.ws, "for-each-ref", "--format=%(refname)", "refs/stw/recovery/sync")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// syncRuns makes n real runs that each move main of the root repository.
func syncRuns(t *testing.T, w *workspace, n int) (ids []string) {
	t.Helper()
	for i := 0; i < n; i++ {
		w.advance(t, ".", "main")
		r := run(t, w, Options{})
		if r.RunID == nil {
			t.Fatalf("run %d changed nothing", i)
		}
		ids = append(ids, *r.RunID)
	}
	return ids
}

// Only the ten most recent completed runs keep their record and recovery refs; everything else stays.
func TestRetentionKeepsTheTenNewestCompletedRuns(t *testing.T) {
	w := newWorkspace(t)
	runGit(t, w.ws, "update-ref", "refs/stw/recovery/other/1", w.oid(t, ".", "HEAD"))
	ids := syncRuns(t, w, 12)
	files := journalFiles(t, w)
	if len(files) != 10 {
		t.Fatalf("records: %v", files)
	}
	for _, id := range ids[:2] {
		for _, f := range files {
			if f == id+".json" {
				t.Fatalf("old run %s kept", id)
			}
		}
		if out := runGit(t, w.ws, "for-each-ref", "refs/stw/recovery/sync/"+id); out != "" {
			t.Fatalf("recovery ref of %s kept: %s", id, out)
		}
	}
	for _, id := range ids[2:] {
		if _, err := os.Stat(filepath.Join(journalDir(w), id+".json")); err != nil {
			t.Fatalf("recent run %s lost: %v", id, err)
		}
		if runGit(t, w.ws, "for-each-ref", "refs/stw/recovery/sync/"+id) == "" {
			t.Fatalf("recovery ref of %s lost", id)
		}
	}
	if got := len(recoveryRefs(t, w)); got != 10 {
		t.Fatalf("recovery refs: %d", got)
	}
	if runGit(t, w.ws, "rev-parse", "--verify", "refs/stw/recovery/other/1") == "" {
		t.Fatal("a ref outside the sync namespace was removed")
	}
	if runGit(t, w.ws, "rev-parse", "--verify", "refs/heads/main") != w.oid(t, ".", "refs/remotes/origin/main") {
		t.Fatal("main is not at the upstream tip")
	}
}

// A plan, an unchanged run and a run with at most ten records remove nothing.
func TestRetentionDoesNotRunForPlansOrShortHistories(t *testing.T) {
	w := newWorkspace(t)
	syncRuns(t, w, 10)
	w.advance(t, ".", "main")
	run(t, w, Options{Plan: true})
	if n := len(journalFiles(t, w)); n != 10 {
		t.Fatalf("records after a plan: %d", n)
	}
}

// A run whose record is still prepared is kept until a restore has dealt with it, however old.
func TestRetentionKeepsRunsWithAnIncompleteRecord(t *testing.T) {
	w := newWorkspace(t)
	ids := syncRuns(t, w, 1)
	path := filepath.Join(journalDir(w), ids[0]+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), `"state":"completed"`, `"state":"prepared"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	syncRuns(t, w, 12)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("incomplete run removed: %v", err)
	}
	if runGit(t, w.ws, "for-each-ref", "refs/stw/recovery/sync/"+ids[0]) == "" {
		t.Fatal("recovery ref of the incomplete run removed")
	}
	if n := len(journalFiles(t, w)); n != 11 {
		t.Fatalf("records: %d, want the incomplete run and ten completed", n)
	}
}

// The record is data: a recovery ref outside the run's own namespace, or one that no longer points at the
// journaled old OID, is never deleted, and then the record stays so that nothing is lost silently.
func TestRetentionDeletesOnlyTheRunsOwnUnmovedRecoveryRefs(t *testing.T) {
	w := newWorkspace(t)
	ids := syncRuns(t, w, 2)
	keep := w.oid(t, ".", "refs/heads/main")
	for i, id := range ids {
		path := filepath.Join(journalDir(w), id+".json")
		raw, _ := os.ReadFile(path)
		if i == 0 { // tampered: names a branch that sits exactly at the journaled old OID
			runGit(t, w.ws, "update-ref", "refs/heads/keepme", runGit(t, w.ws, "rev-parse", "refs/stw/recovery/sync/"+id+"/1"))
			raw = []byte(strings.Replace(string(raw), "refs/stw/recovery/sync/"+id+"/1", "refs/heads/keepme", 1))
		} else { // moved: the ref points elsewhere now
			runGit(t, w.ws, "update-ref", "refs/stw/recovery/sync/"+id+"/1", keep)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	syncRuns(t, w, 10)
	if runGit(t, w.ws, "rev-parse", "--verify", "refs/heads/keepme") == "" {
		t.Fatal("branch deleted")
	}
	if runGit(t, w.ws, "rev-parse", "--verify", "refs/stw/recovery/sync/"+ids[1]+"/1") != keep {
		t.Fatal("a moved recovery ref was deleted")
	}
	for _, id := range ids {
		if _, err := os.Stat(filepath.Join(journalDir(w), id+".json")); err != nil {
			t.Fatalf("record %s removed although a ref stayed: %v", id, err)
		}
	}
}

// Foreign and unreadable files in the journal directory are left alone and not counted.
func TestRetentionIgnoresForeignFiles(t *testing.T) {
	w := newWorkspace(t)
	syncRuns(t, w, 1)
	write(t, filepath.Join(journalDir(w), "notes.json"), "{not json\n")
	write(t, filepath.Join(journalDir(w), "README"), "x\n")
	syncRuns(t, w, 11)
	for _, f := range []string{"notes.json", "README"} {
		if _, err := os.Stat(filepath.Join(journalDir(w), f)); err != nil {
			t.Fatalf("%s removed", f)
		}
	}
	if n := len(journalFiles(t, w)); n != 12 {
		t.Fatalf("files: %d", n)
	}
}
