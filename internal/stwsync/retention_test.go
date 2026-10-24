// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
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

// fakeRun writes a completed record dated by its ID, with one recovery ref at the current main tip.
func fakeRun(t *testing.T, w *workspace, id string) {
	t.Helper()
	tip := runGit(t, w.ws, "rev-parse", "HEAD")
	ref := "refs/stw/recovery/sync/" + id + "/1"
	runGit(t, w.ws, "update-ref", ref, tip)
	rec := journalRecord{Type: "sync", SchemaVersion: 1, RunID: id, Entries: []journalEntry{{N: 1, Ref: "refs/heads/main", OldOID: tip, NewOID: tip, RecoveryRef: ref, State: stateCompleted}}}
	raw, _ := json.Marshal(rec)
	write(t, filepath.Join(journalDir(w), id+".json"), string(raw))
}

func fakeRuns(t *testing.T, w *workspace, n int, year string) (ids []string) {
	t.Helper()
	if err := os.MkdirAll(journalDir(w), 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		id := year + "0101T0000" + itoa(10+i) + "Z-0000000" + itoa(i%10)
		fakeRun(t, w, id)
		ids = append(ids, id)
	}
	return ids
}

func hasRecord(w *workspace, id string) bool {
	_, err := os.Stat(filepath.Join(journalDir(w), id+".json"))
	return err == nil
}

// F1: the current run is never retained away, even when ten completed records carry later dates.
func TestRetentionNeverDeletesTheCurrentRun(t *testing.T) {
	w := newWorkspace(t)
	fakeRuns(t, w, 10, "2099")
	w.advance(t, ".", "main")
	r := run(t, w, Options{})
	if r.RunID == nil || !hasRecord(w, *r.RunID) || runGit(t, w.ws, "for-each-ref", "refs/stw/recovery/sync/"+*r.RunID) == "" {
		t.Fatalf("the current run lost its record or recovery ref: %+v", r.RunID)
	}
	if n := len(journalFiles(t, w)); n != 10 {
		t.Fatalf("records: %d", n)
	}
}

// F2: a symbolic ref in the recovery namespace is kept, never followed, and its target survives. The target
// sits exactly at the journaled old OID, so only the guards (not a moved branch) protect it.
func TestRetentionNeverFollowsASymbolicRecoveryRef(t *testing.T) {
	w := newWorkspace(t)
	ids := fakeRuns(t, w, 1, "2001")
	ref := "refs/stw/recovery/sync/" + ids[0] + "/1"
	old := runGit(t, w.ws, "rev-parse", ref)
	runGit(t, w.ws, "update-ref", "refs/heads/victim", old)
	runGit(t, w.ws, "update-ref", "-d", ref)
	runGit(t, w.ws, "symbolic-ref", ref, "refs/heads/victim")
	syncRuns(t, w, 10)
	if runGit(t, w.ws, "rev-parse", "--verify", "refs/heads/victim") != old || runGit(t, w.ws, "symbolic-ref", ref) != "refs/heads/victim" || !hasRecord(w, ids[0]) {
		t.Fatal("the symbolic recovery ref was followed or removed")
	}
}

// N1: a dangling symbolic ref is not listed by for-each-ref but exists; it is kept with its record.
func TestRetentionKeepsADanglingSymbolicRecoveryRef(t *testing.T) {
	w := newWorkspace(t)
	ids := fakeRuns(t, w, 1, "2001")
	ref := "refs/stw/recovery/sync/" + ids[0] + "/1"
	runGit(t, w.ws, "update-ref", "-d", ref)
	runGit(t, w.ws, "symbolic-ref", ref, "refs/heads/does-not-exist")
	syncRuns(t, w, 10)
	if runGit(t, w.ws, "symbolic-ref", ref) != "refs/heads/does-not-exist" || !hasRecord(w, ids[0]) {
		t.Fatal("a dangling symbolic recovery ref was treated as gone")
	}
}

// F3: a record copied under an old file name must not be treated as that old run: it describes a recent run
// whose recovery ref has to survive.
func TestRetentionIgnoresARecentRecordUnderAnOldName(t *testing.T) {
	w := newWorkspace(t)
	ids := syncRuns(t, w, 10)
	recent := ids[len(ids)-1]
	raw, err := os.ReadFile(filepath.Join(journalDir(w), recent+".json"))
	if err != nil {
		t.Fatal(err)
	}
	old := "20010101T000000Z-00000000"
	write(t, filepath.Join(journalDir(w), old+".json"), string(raw))
	syncRuns(t, w, 1)
	if runGit(t, w.ws, "for-each-ref", "refs/stw/recovery/sync/"+recent) == "" {
		t.Fatal("the recent run's recovery ref was deleted through a record under an old name")
	}
	if !hasRecord(w, old) {
		t.Fatal("the mismatching record was removed")
	}
}

// F2: only the exact name refs/stw/recovery/sync/RUN_ID/N of the entry itself is deleted.
func TestRetentionDeletesOnlyTheExactEntryRefName(t *testing.T) {
	w := newWorkspace(t)
	ids := fakeRuns(t, w, 1, "2001")
	editRecord(t, w, w.ws, ids[0], func(r *journalRecord) {
		r.Entries[0].RecoveryRef = "refs/stw/recovery/sync/" + ids[0] + "/other/7"
		runGit(t, w.ws, "update-ref", r.Entries[0].RecoveryRef, r.Entries[0].OldOID)
	})
	syncRuns(t, w, 10)
	if !hasRecord(w, ids[0]) || runGit(t, w.ws, "for-each-ref", "refs/stw/recovery/sync/"+ids[0]+"/other") == "" {
		t.Fatal("a ref outside the exact entry name was deleted")
	}
}

// F3: an empty or malformed old OID keeps the ref and the record.
func TestRetentionRefusesAnInvalidOldOID(t *testing.T) {
	for _, bad := range []string{"", "HEAD", "zz"} {
		w := newWorkspace(t)
		ids := fakeRuns(t, w, 1, "2001")
		editRecord(t, w, w.ws, ids[0], func(r *journalRecord) { r.Entries[0].OldOID = bad })
		syncRuns(t, w, 10)
		if !hasRecord(w, ids[0]) || runGit(t, w.ws, "for-each-ref", "refs/stw/recovery/sync/"+ids[0]) == "" {
			t.Fatalf("old OID %q: ref or record deleted", bad)
		}
	}
}

// F3: files whose type, run ID or name do not match are neither counted nor deleted.
func TestRetentionIgnoresRecordsThatDoNotMatchTheirName(t *testing.T) {
	w := newWorkspace(t)
	ids := fakeRuns(t, w, 3, "2001")
	editRecord(t, w, w.ws, ids[0], func(r *journalRecord) { r.Type = "other" })
	editRecord(t, w, w.ws, ids[1], func(r *journalRecord) { r.RunID = ids[2] })
	odd := "2001-odd-name"
	raw, _ := os.ReadFile(filepath.Join(journalDir(w), ids[2]+".json"))
	write(t, filepath.Join(journalDir(w), odd+".json"), string(raw))
	syncRuns(t, w, 10)
	for _, f := range []string{ids[0], ids[1], odd} {
		if !hasRecord(w, f) {
			t.Fatalf("%s deleted", f)
		}
	}
	if hasRecord(w, ids[2]) {
		t.Fatal("the one valid old record was not retained away")
	}
}

// F3: a ref that cannot be read (cancelled context) is not "already gone": the record stays.
func TestRetentionKeepsTheRecordWhenTheRefCannotBeRead(t *testing.T) {
	w := newWorkspace(t)
	ids := fakeRuns(t, w, 1, "2001")
	e := env{bin: mustGit(t), dir: w.ws}
	j, err := newJournal(context.Background(), e, "20990101T000000Z-ffffffff", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec, _ := loadForTest(t, w, ids[0])
	if j.dropRecoveryRefs(ctx, rec) {
		t.Fatal("an unreadable ref counted as gone")
	}
	if runGit(t, w.ws, "for-each-ref", "refs/stw/recovery/sync/"+ids[0]) == "" {
		t.Fatal("ref deleted")
	}
	// a verified-missing ref does count as gone
	runGit(t, w.ws, "update-ref", "-d", "refs/stw/recovery/sync/"+ids[0]+"/1")
	if !j.dropRecoveryRefs(context.Background(), rec) {
		t.Fatal("a missing ref should count as gone")
	}
}

func mustGit(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func loadForTest(t *testing.T, w *workspace, id string) (journalRecord, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(journalDir(w), id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec journalRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	return rec, true
}

// F4: an unchanged run, and a run whose change failed, retain nothing away.
func TestRetentionOnlyRunsAfterASuccessfulJournaledChange(t *testing.T) {
	w := newWorkspace(t)
	ids := fakeRuns(t, w, 12, "2001")
	run(t, w, Options{}) // nothing to change: no journal, no retention
	if n := len(journalFiles(t, w)); n != 12 {
		t.Fatalf("an unchanged run removed records: %d", n)
	}
	w.advance(t, ".", "main")
	applyHook = func(dir string) {
		if dir == w.ws {
			commit(t, w.ws, "racer.txt", "x\n")
		}
	}
	defer func() { applyHook = nil }()
	r := run(t, w, Options{})
	if repoByName(t, r)["."].Result != ResultFailed {
		t.Fatalf("expected a failed change: %+v", repoByName(t, r)["."])
	}
	for _, id := range ids {
		if !hasRecord(w, id) {
			t.Fatalf("a failed change retained %s away", id)
		}
	}
}

func editRecord(t *testing.T, w *workspace, dir, id string, fn func(*journalRecord)) {
	t.Helper()
	path := filepath.Join(dir, ".git", "stw", "sync", id+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec journalRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	fn(&rec)
	raw, _ = json.Marshal(rec)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Retention orders an old-format run ID and a microsecond ID of the same second by time, not as raw text
// ('Z' sorts after the digits): at the boundary the genuinely older run is the one pruned.
func TestRetentionOrdersMixedRunIDFormatsByTime(t *testing.T) {
	w := newWorkspace(t)
	fakeRuns(t, w, 8, "2099")
	older, newer := "20260101T120000Z-00000000", "20260101T120000500000Z-00000000"
	fakeRun(t, w, older)
	fakeRun(t, w, newer)
	w.advance(t, ".", "main")
	run(t, w, Options{})
	if hasRecord(w, older) || !hasRecord(w, newer) {
		t.Fatalf("older kept: %v, newer kept: %v", hasRecord(w, older), hasRecord(w, newer))
	}
	if runGit(t, w.ws, "for-each-ref", "refs/stw/recovery/sync/"+newer) == "" {
		t.Fatal("the newer run lost its recovery ref")
	}
}
