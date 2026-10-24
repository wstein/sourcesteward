// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Journal states of one branch ref change.
const (
	statePrepared  = "prepared"
	stateCompleted = "completed"
	stateFailed    = "failed"
)

// journalEntry is the record of one branch ref change. It is written (state prepared, with its
// recovery ref already created) before the change and rewritten after it.
type journalEntry struct {
	N             int    `json:"n"`
	Ref           string `json:"ref"`
	Worktree      string `json:"worktree"`
	OldOID        string `json:"old_oid"`
	PlannedNewOID string `json:"planned_new_oid"`
	NewOID        string `json:"new_oid"`
	OrigHead      string `json:"orig_head"`
	StashOID      string `json:"stash_oid"`
	RecoveryRef   string `json:"recovery_ref"`
	State         string `json:"state"`
	Reason        string `json:"reason"`
}

// journalRecord is the `sync` record type: one file per run in <git-common-dir>/stw/sync. It follows the
// Spec 029/031 conventions (recovery refs under refs/stw/recovery, durable atomic rewrites) but is keyed
// by run ID and needs no plan, state directory or sandboxed reader.
type journalRecord struct {
	Type          string         `json:"type"`
	SchemaVersion int            `json:"schema_version"`
	RunID         string         `json:"run_id"`
	Remote        string         `json:"remote"`
	Started       string         `json:"started"`
	Entries       []journalEntry `json:"entries"`
}

type journal struct {
	e    env
	path string // empty until the first entry: a run without a ref change leaves no file
	dir  string
	rec  journalRecord
}

func newRunID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	// Microseconds keep the run IDs of two runs in the same second in order: retention sorts by run ID.
	return fmt.Sprintf("%s%06dZ-%s", now.Format("20060102T150405"), now.Nanosecond()/1000, hex.EncodeToString(b)), nil
}

// changedOK reports whether this run completed at least one journaled branch change here.
func (j *journal) changedOK() bool {
	for _, en := range j.rec.Entries {
		if en.State == stateCompleted {
			return true
		}
	}
	return false
}

// retainedRuns is how many completed runs of one repository keep their record and recovery refs.
const retainedRuns = 10

var runIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}([0-9]{6})?Z-[0-9a-f]{8}$`)

// retain is the last step of a real run: it removes the record and the recovery refs of the runs older
// than the retainedRuns most recent completed ones. A run with an incomplete (prepared) record is kept
// until a restore has dealt with it. A recovery ref is deleted only when the record names it inside the
// run's own namespace and it still points at the journaled old OID; if one stays, so does its record.
// Files that are not run records are ignored. Best effort: a failure leaves the older runs in place.
func (j *journal) retain(ctx context.Context) {
	files, err := os.ReadDir(j.dir)
	if err != nil {
		return
	}
	type run struct {
		id  string
		rec journalRecord
	}
	var completed []run
	for _, f := range files {
		id, ok := strings.CutSuffix(f.Name(), ".json")
		if !ok || !runIDPattern.MatchString(id) || f.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(j.dir, f.Name()))
		if err != nil {
			continue
		}
		var rec journalRecord
		if json.Unmarshal(raw, &rec) != nil || rec.Type != "sync" || rec.RunID != id {
			continue
		}
		done := true
		for _, en := range rec.Entries {
			if en.State == statePrepared {
				done = false
			}
		}
		if done {
			completed = append(completed, run{id, rec})
		}
	}
	// The current run is protected first, whatever the clock-based IDs say, and counts as one of the retained runs.
	others := completed[:0]
	for _, r := range completed {
		if r.id != j.rec.RunID {
			others = append(others, r)
		}
	}
	completed = others
	sort.Slice(completed, func(a, b int) bool { return completed[a].id < completed[b].id })
	for len(completed) > retainedRuns-1 {
		old := completed[0]
		completed = completed[1:]
		if j.dropRecoveryRefs(ctx, old.rec) {
			os.Remove(filepath.Join(j.dir, old.id+".json"))
		}
	}
}

// dropRecoveryRefs deletes the recovery refs of one run and reports whether none is left. The record is
// data: only the exact name refs/stw/recovery/sync/RUN_ID/N of the entry itself, a valid old OID as the
// expected value and a non-symbolic ref qualify. Only a verified-missing ref counts as gone; a ref that
// cannot be read keeps the record.
func (j *journal) dropRecoveryRefs(ctx context.Context, rec journalRecord) bool {
	clean := true
	for _, en := range rec.Entries {
		if en.N < 1 || en.RecoveryRef != fmt.Sprintf("refs/stw/recovery/sync/%s/%d", rec.RunID, en.N) || !oidPattern.MatchString(en.OldOID) {
			clean = false // not this entry's own ref: never touched, and the record stays
			continue
		}
		// a symbolic ref is detected first: a dangling one is not listed by for-each-ref but exists
		if sym, err := j.e.g(ctx, "symbolic-ref", "-q", en.RecoveryRef); err != nil || sym.Code != 1 {
			clean = false // symbolic (or unreadable): never followed, never counted as gone
			continue
		}
		list, err := j.e.g(ctx, "for-each-ref", "--format=%(refname)", en.RecoveryRef)
		if err != nil || list.Code != 0 {
			clean = false
			continue
		}
		if strings.TrimSpace(list.Out) == "" {
			continue // verified missing
		}
		if out, err := j.e.g(ctx, "update-ref", "--no-deref", "-d", en.RecoveryRef, en.OldOID); err != nil || out.Code != 0 {
			clean = false
		}
	}
	return clean
}

func newJournal(ctx context.Context, e env, id, remote string) (*journal, error) {
	out, err := e.g(ctx, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || out.Code != 0 || strings.TrimSpace(out.Out) == "" {
		return nil, fmt.Errorf("cannot locate the Git directory")
	}
	dir := filepath.Join(strings.TrimSpace(out.Out), "stw", "sync")
	return &journal{e: e, dir: dir, path: filepath.Join(dir, id+".json"),
		rec: journalRecord{Type: "sync", SchemaVersion: 1, RunID: id, Remote: remote, Started: time.Now().UTC().Format(time.RFC3339), Entries: []journalEntry{}}}, nil
}

func (j *journal) runID() string { return j.rec.RunID }

// prepare creates the recovery ref (old OID, create-only) and durably records the planned change.
func (j *journal) prepare(ctx context.Context, ref, worktree, oldOID, newOID string) (int, error) {
	n := len(j.rec.Entries) + 1
	recovery := fmt.Sprintf("refs/stw/recovery/sync/%s/%d", j.rec.RunID, n)
	if out, err := j.e.g(ctx, "update-ref", "--create-reflog", recovery, oldOID, strings.Repeat("0", len(oldOID))); err != nil || out.Code != 0 {
		return 0, fmt.Errorf("recovery ref: %s", oneLine(out.Diagnostic))
	}
	j.rec.Entries = append(j.rec.Entries, journalEntry{N: n, Ref: ref, Worktree: worktree, OldOID: oldOID, PlannedNewOID: newOID, RecoveryRef: recovery, State: statePrepared})
	if err := j.write(); err != nil {
		j.rec.Entries = j.rec.Entries[:n-1]
		j.e.g(ctx, "update-ref", "--no-deref", "-d", recovery, oldOID)
		return 0, err
	}
	return n - 1, nil
}

// stash records the autostash OID of entry i before the branch change.
func (j *journal) stash(i int, oid string) error {
	j.rec.Entries[i].StashOID = oid
	return j.write()
}

// finish records the outcome of entry i.
func (j *journal) finish(i int, state, newOID, origHead, reason string) error {
	en := &j.rec.Entries[i]
	en.State, en.NewOID, en.OrigHead, en.Reason = state, newOID, origHead, reason
	return j.write()
}

func (j *journal) write() error {
	if err := os.MkdirAll(j.dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(j.rec)
	if err != nil {
		return err
	}
	tmp := j.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err = os.Rename(tmp, j.path); err != nil {
		return err
	}
	if d, derr := os.Open(j.dir); derr == nil {
		d.Sync()
		d.Close()
	}
	return nil
}
