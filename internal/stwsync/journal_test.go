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

func readJournalFiles(t *testing.T, repo string) []journalRecord {
	t.Helper()
	dir := filepath.Join(repo, ".git", "stw", "sync")
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
			t.Fatalf("%s: %v", e.Name(), err)
		}
		out = append(out, rec)
	}
	return out
}

// TestJournalIsWrittenBeforeEachRefChangeAndCompletedAfter snapshots the journal and the recovery ref
// from a reference-transaction hook at the moment Git prepares the branch change.
func TestJournalIsWrittenBeforeEachRefChangeAndCompletedAfter(t *testing.T) {
	f := newFixture(t)
	f.branchOnRemote(t, "side") // not checked out anywhere
	f.advanceRemote(t, "main")  // checked out in repo
	f.advanceRemote(t, "side")
	oldMain, oldSide := f.oid(t, "main"), f.oid(t, "side")
	snap := filepath.Join(f.root, "snapshots")
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	hook := "#!/bin/sh\n[ \"$1\" = prepared ] || exit 0\nwhile read old new ref; do\n" +
		"  case \"$ref\" in refs/heads/*) cat \"$(git rev-parse --git-common-dir)\"/stw/sync/*.json > '" + snap + "'/\"$(basename \"$ref\")\";\n" +
		"  git for-each-ref refs/stw/recovery/sync > '" + snap + "'/\"$(basename \"$ref\")\".refs;; esac\ndone\n"
	write(t, filepath.Join(f.repo, ".git", "hooks", "reference-transaction"), hook)
	if err := os.Chmod(filepath.Join(f.repo, ".git", "hooks", "reference-transaction"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := Run(context.Background(), Options{Path: f.repo})
	if r.Error != nil || r.Mode != "sync" || r.RunID == nil {
		t.Fatalf("%+v", r)
	}
	for branch, old := range map[string]string{"main": oldMain, "side": oldSide} {
		raw, err := os.ReadFile(filepath.Join(snap, branch))
		if err != nil {
			t.Fatalf("%s: no journal at the moment of the change: %v", branch, err)
		}
		var rec journalRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			t.Fatal(err)
		}
		var entry *journalEntry
		for i := range rec.Entries {
			if rec.Entries[i].Ref == "refs/heads/"+branch {
				entry = &rec.Entries[i]
			}
		}
		if rec.Type != "sync" || rec.RunID != *r.RunID || entry == nil || entry.State != "prepared" || entry.OldOID != old || entry.PlannedNewOID == "" {
			t.Fatalf("%s: record before the change: %+v", branch, rec)
		}
		refs, _ := os.ReadFile(filepath.Join(snap, branch+".refs"))
		if !strings.Contains(string(refs), old) || !strings.Contains(string(refs), "refs/stw/recovery/sync/"+*r.RunID+"/") {
			t.Fatalf("%s: recovery ref missing before the change: %q", branch, refs)
		}
	}
	recs := readJournalFiles(t, f.repo)
	if len(recs) != 1 || len(recs[0].Entries) != 2 {
		t.Fatalf("%+v", recs)
	}
	for _, e := range recs[0].Entries {
		if e.State != "completed" || e.NewOID != f.oid(t, e.Ref) {
			t.Errorf("not completed: %+v", e)
		}
		if got := f.oid(t, e.RecoveryRef); got != e.OldOID {
			t.Errorf("recovery ref %s -> %s, want %s", e.RecoveryRef, got, e.OldOID)
		}
	}
	main := recs[0].Entries[0]
	if main.Ref != "refs/heads/main" || main.OrigHead != oldMain {
		t.Errorf("a checked-out branch records ORIG_HEAD: %+v", main)
	}
}

func TestJournalIsNotWrittenForPlansOrUnchangedRuns(t *testing.T) {
	f := newFixture(t)
	Plan(context.Background(), Options{Path: f.repo})
	Run(context.Background(), Options{Path: f.repo}) // nothing to change
	if _, err := os.Stat(filepath.Join(f.repo, ".git", "stw")); !os.IsNotExist(err) {
		t.Fatalf("a journal directory exists without a ref change: %v", err)
	}
	if out := runGit(t, f.repo, "for-each-ref", "refs/stw"); out != "" {
		t.Fatalf("recovery refs without a change: %s", out)
	}
}

func TestFailedJournalWriteStopsTheChange(t *testing.T) {
	f := newFixture(t)
	f.advanceRemote(t, "main")
	old := f.oid(t, "main")
	write(t, filepath.Join(f.repo, ".git", "stw"), "a file where the journal directory belongs\n")
	r := Run(context.Background(), Options{Path: f.repo})
	it := itemsByBranch(t, r)["main"]
	if it.Result != ResultFailed || f.oid(t, "main") != old || it.Reason == nil || !strings.HasPrefix(*it.Reason, "journal-failed") {
		t.Fatalf("%+v", it)
	}
}
