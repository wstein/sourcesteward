// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"reflect"
	"strings"
	"time"
)

// Apply of a restore (Spec 031 FR-044, FR-045, contracts/restore.md). The only
// effect is the creation of one local branch per restorable entry by
// compare-and-swap against the zero object ID. Before each creation a
// write-ahead intent record is durable in the restore namespace of the private
// store, after it an outcome record follows. Nothing is rewritten, no
// recovery ref, worktree, remote, configuration or index is touched.

const (
	restoresKind       = "tidy-v3-restores"
	restoreRecordLimit = 64

	restoreIntent  = "intent"
	restoreOutcome = "outcome"

	outcomeCreated          = "created"
	outcomeCreatedRecovered = "created-recovered"
	outcomeAlreadyPresent   = "already-present"
	outcomeNameTaken        = "name-taken"
	outcomeSymbolic         = "symbolic-reference"
	outcomeRecoveryMissing  = "recovery-missing"
	outcomeJournalIncomp    = "journal-incomplete"
	outcomeError            = "error"
	outcomeNotAttempted     = "not-attempted"
)

// restoreRecord is one append-only record of the restore journal.
type restoreRecord struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	PlanID        string `json:"plan_id"`
	ActionID      string `json:"action_id"`
	Ref           string `json:"ref"`
	OID           string `json:"oid"`
	Outcome       string `json:"outcome,omitempty"`
	By            string `json:"by"`
	At            string `json:"at"`
}

// V3RestoreRecordRef names the records an apply wrote for one entry.
type V3RestoreRecordRef struct {
	Intent  string `json:"intent,omitempty"`
	Outcome string `json:"outcome,omitempty"`
}

var restoreOutcomes = []string{outcomeCreated, outcomeCreatedRecovered, outcomeNameTaken, outcomeSymbolic, outcomeRecoveryMissing, outcomeError}

// restoreRecordID is the record identity of the seq-th record of an action. The
// plan and action identities (up to 128 bytes each) are hashed into it so that
// the file name stays within one identity.
func restoreRecordID(planID, actionID string, seq int) string {
	sum := sha256.Sum256([]byte(planID + "\x00" + actionID))
	return fmt.Sprintf("%s-%04d", hex.EncodeToString(sum[:16]), seq)
}

func validRestoreHexOID(s string) bool {
	return (len(s) == 40 || len(s) == 64) && validAlphabet(s, len(s), "0123456789abcdef")
}

func validRestoreRecord(r restoreRecord, planID, actionID string) bool {
	if r.SchemaVersion != 1 || r.PlanID != planID || r.ActionID != actionID || !validRestoreRefName(r.Ref) || !validRestoreHexOID(r.OID) || r.By == "" || !validText(r.By) || r.At == "" || !validText(r.At) {
		return false
	}
	switch r.Kind {
	case restoreIntent:
		return r.Outcome == ""
	case restoreOutcome:
		for _, o := range restoreOutcomes {
			if o == r.Outcome {
				return true
			}
		}
	}
	return false
}

// readRestoreRecords returns the records of one action in order. A store that
// has no restore namespace has no records.
func readRestoreRecords(s *store, planID, actionID string) ([]restoreRecord, error) {
	records := []restoreRecord{}
	if s.dirs[restoresKind] == nil {
		return records, nil
	}
	for seq := 1; ; seq++ {
		if seq > restoreRecordLimit {
			return nil, failure("record-limit", "The restore journal of the action exceeds its record bound.")
		}
		var rec restoreRecord
		err := s.read(restoresKind, restoreRecordID(planID, actionID, seq), &rec)
		if os.IsNotExist(err) {
			return records, nil
		}
		if err != nil {
			return nil, err
		}
		if !validRestoreRecord(rec, planID, actionID) {
			return nil, failure("invalid-state", "A restore journal record is malformed.")
		}
		records = append(records, rec)
	}
}

// pendingIntent is the last record when it is an intent without an outcome.
func pendingIntent(records []restoreRecord) (restoreRecord, bool) {
	if n := len(records); n > 0 && records[n-1].Kind == restoreIntent {
		return records[n-1], true
	}
	return restoreRecord{}, false
}

// openRestoreStore opens the private store for the restore apply. With write it
// creates the restore namespace; without it, the namespace is used only when it
// exists.
func openRestoreStore(stateDir string, write bool) (*store, error) {
	kinds := []string{"tidy-v3-plans", v3OperationsKind}
	if _, err := os.Lstat(stateDir + "/" + restoresKind); write || err == nil {
		kinds = append(kinds, restoresKind)
	}
	return openStoreKinds(stateDir, write, kinds)
}

func restoreWho() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return fmt.Sprintf("uid-%d", os.Getuid())
}

// createBranch is the compare-and-swap of FR-044: the reference is created only
// if it does not exist, never followed through a symbolic reference.
func (n restoreNative) createBranch(ctx context.Context, planID, actionID, ref, oid string) (int, error) {
	ro, err := gitTidyWriteProtection(n.id, []string{ref})
	if err != nil {
		return -1, err
	}
	args := []string{"update-ref", "--no-deref", "-m", "stw restore " + planID + " " + actionID, "--", ref, oid, strings.Repeat("0", len(oid))}
	if n.observe != nil {
		n.observe(args)
	}
	_, code, err := runSandboxed(ctx, n.bin, args, n.id.WorkspaceRoot, []string{n.id.RepositoryDir}, ro)
	if err != nil {
		return -1, gitExecutionError(err)
	}
	return code, nil
}

// isSymbolic reads one reference without following it; a dangling symbolic
// reference counts too.
func (n restoreNative) isSymbolic(ctx context.Context, ref string) (bool, error) {
	_, code, err := n.run(ctx, []string{"symbolic-ref", "-q", "--", ref})
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

// outcomeOf maps an evaluated state to the outcome of an entry that has no
// effect.
func outcomeOf(e V3RestoreEntry) string {
	switch {
	case e.State == restoreNameTaken && reasonOf(e) == "symbolic-reference":
		return outcomeSymbolic
	case e.State == restoreNameTaken:
		return outcomeNameTaken
	case e.State == restoreRecoveryMissing:
		return outcomeRecoveryMissing
	case e.State == restoreJournalIncomplete:
		return outcomeJournalIncomp
	}
	return outcomeAlreadyPresent
}

// restoreApplyStatus is the status rule of the contract.
func restoreApplyStatus(items []restoreItem) string {
	created, failed, errored := 0, 0, false
	for _, item := range items {
		switch item.entry.Outcome {
		case outcomeCreated, outcomeCreatedRecovered:
			created++
		case outcomeAlreadyPresent:
		default:
			failed++
			errored = errored || item.entry.Outcome == outcomeError || item.entry.Outcome == outcomeNotAttempted
		}
	}
	switch {
	case failed == 0:
		return "ok"
	case created > 0:
		return "partial"
	case errored:
		return "error"
	}
	return "refused"
}

// restoreEffectGuard refuses the effect where repository hooks or executable
// configuration would run with a reference update: the strict admission of
// Spec 029 apply, without the planning neutralization.
func restoreEffectGuard(ctx context.Context, n restoreNative) error {
	_, err := gitSafety(withoutPlanningSession(ctx), n.bin, n.id, n.id.CommitID)
	return err
}

func (c tidyV3RestoreCoordinator) record(s *store, rec restoreRecord, seq int) (string, error) {
	id := restoreRecordID(rec.PlanID, rec.ActionID, seq)
	write := c.writeRecord
	if write == nil {
		write = func(s *store, id string, rec restoreRecord) error { return s.write(restoresKind, id, rec, true) }
	}
	return id, write(s, id, rec)
}

func (c tidyV3RestoreCoordinator) stamp(kind string, e V3RestoreEntry, planID, outcome string) restoreRecord {
	who := restoreWho
	if c.who != nil {
		who = c.who
	}
	return restoreRecord{SchemaVersion: 1, Kind: kind, PlanID: planID, ActionID: e.ActionID, Ref: e.Ref, OID: e.OID, Outcome: outcome, By: who(), At: c.now().UTC().Format(time.RFC3339Nano)}
}

// applyRestore carries out the effects computed from the fresh plan evaluation.
func (c tidyV3RestoreCoordinator) applyRestore(ctx context.Context, n restoreNative, o V3RestoreOptions, view v3JournalView, items []restoreItem, r V3RestoreResult) V3RestoreResult {
	finish := func(r V3RestoreResult) V3RestoreResult {
		r.Entries, r.Counts = r.Entries[:0], V3RestoreCounts{}
		for _, item := range items {
			r.Entries = append(r.Entries, item.entry)
			r.Counts.add(item.entry.State)
		}
		r.Status = restoreApplyStatus(items)
		return r
	}
	for i := range items {
		items[i].entry.Outcome = outcomeOf(items[i].entry)
	}
	pending := map[string]bool{}
	if ro, err := openRestoreStore(o.StateDir, false); err != nil {
		return failRestore(r, err)
	} else {
		for _, item := range items {
			records, err := readRestoreRecords(ro, o.PlanID, item.entry.ActionID)
			if err != nil {
				ro.Close()
				return failRestore(r, err)
			}
			if p, ok := pendingIntent(records); ok && p.Ref == item.entry.Ref && p.OID == item.entry.OID {
				pending[item.entry.ActionID] = true
			}
		}
		ro.Close()
	}
	work, restorable := false, false
	for _, item := range items {
		restorable = restorable || item.entry.State == restoreRestorable
		work = work || item.entry.State == restoreRestorable || item.entry.State == restoreAlreadyPresent && pending[item.entry.ActionID]
	}
	if !work {
		return finish(r)
	}
	if restorable {
		if err := restoreEffectGuard(ctx, n); err != nil {
			return failRestore(r, err)
		}
	}
	if c.afterEvaluate != nil {
		c.afterEvaluate()
	}
	s, err := openRestoreStore(o.StateDir, true)
	if err != nil {
		return failRestore(r, err)
	}
	defer s.Close()
	// The journal is read again under the writer lock: the effects follow the
	// journal that was evaluated, never a changed one.
	locked := &v3Store{store: s, protected: []string{}}
	again, err := locked.readJournalView(o.PlanID)
	if err != nil || !reflect.DeepEqual(again.Journal, view.Journal) || !reflect.DeepEqual(again.Plan, view.Plan) {
		return failRestore(r, failure("changed-state", "The stored journal changed while the restore was prepared."))
	}
	stopped := false
	for i := range items {
		e := &items[i].entry
		if stopped {
			e.Outcome = outcomeNotAttempted
			continue
		}
		switch e.State {
		case restoreAlreadyPresent:
			if !pending[e.ActionID] {
				continue
			}
			records, err := readRestoreRecords(s, o.PlanID, e.ActionID)
			if err != nil {
				e.Outcome, stopped = outcomeError, true
				r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "restore-journal-unreadable", Message: "The restore journal of an action could not be read."})
				continue
			}
			id, err := c.record(s, c.stamp(restoreOutcome, *e, o.PlanID, outcomeCreatedRecovered), len(records)+1)
			if err != nil {
				e.Outcome, stopped = outcomeError, true
				r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "restore-journal-unwritten", Message: "The outcome of an interrupted restore could not be recorded."})
				continue
			}
			e.Outcome, e.RestoreJournal = outcomeCreatedRecovered, &V3RestoreRecordRef{Outcome: id}
		case restoreRestorable:
			if c.restoreOne(ctx, n, s, o, e, &r) {
				stopped = true
			}
		}
	}
	return finish(r)
}

// restoreOne re-evaluates one entry immediately before its compare-and-swap and
// creates the branch. It reports whether the run has to stop.
func (c tidyV3RestoreCoordinator) restoreOne(ctx context.Context, n restoreNative, s *store, o V3RestoreOptions, e *V3RestoreEntry, r *V3RestoreResult) (stop bool) {
	fail := func(code, message string) bool {
		e.Outcome = outcomeError
		r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: code, Message: message})
		return true
	}
	if err := classifyRestoreEntries(ctx, n, []*V3RestoreEntry{e}); err != nil {
		return fail("restore-reevaluation-failed", "The entry could not be re-evaluated before its effect.")
	}
	if e.State == restoreRestorable {
		// A dangling symbolic reference is invisible to the batched read.
		symbolic, err := n.isSymbolic(ctx, e.Ref)
		if err != nil {
			return fail("restore-reevaluation-failed", "The entry could not be re-evaluated before its effect.")
		}
		if symbolic {
			e.State, e.Reason = restoreNameTaken, reasonPointer("symbolic-reference")
		}
	}
	if e.State != restoreRestorable {
		e.Outcome = outcomeOf(*e)
		return false
	}
	records, err := readRestoreRecords(s, o.PlanID, e.ActionID)
	if err != nil {
		return fail("restore-journal-unreadable", "The restore journal of the action could not be read.")
	}
	ref := &V3RestoreRecordRef{}
	intent, err := c.record(s, c.stamp(restoreIntent, *e, o.PlanID, ""), len(records)+1)
	if err != nil {
		return fail("restore-journal-unwritten", "The intent could not be recorded; no reference was touched.")
	}
	ref.Intent = intent
	e.RestoreJournal = ref
	if c.afterIntent != nil {
		if err := c.afterIntent(*e); err != nil {
			return fail("interrupted", "The restore was interrupted after its intent.")
		}
	}
	if c.beforeCAS != nil {
		c.beforeCAS(*e)
	}
	// Some Git versions replace a dangling symbolic reference when the old value
	// is the zero object ID, so the symbol is looked for right before the swap.
	symbolicNow, err := n.isSymbolic(ctx, e.Ref)
	if err != nil {
		return fail("restore-failed", "The reference could not be created.")
	}
	code := 1
	if !symbolicNow {
		code, err = n.createBranch(ctx, o.PlanID, e.ActionID, e.Ref, e.OID)
		if err != nil {
			return fail("restore-failed", "The reference could not be created.")
		}
	}
	outcome := outcomeCreated
	if code != 0 {
		symbolic, err := n.isSymbolic(ctx, e.Ref)
		if err != nil {
			return fail("restore-failed", "The reference could not be created.")
		}
		current, err := n.readRefs(ctx, []string{e.Ref})
		switch {
		case err != nil:
			return fail("restore-failed", "The reference could not be created.")
		case symbolic:
			outcome = outcomeSymbolic
		case current[e.Ref].Present:
			outcome = outcomeNameTaken
		default:
			return fail("restore-failed", "The reference could not be created.")
		}
	}
	e.Outcome = outcome
	if c.afterCAS != nil {
		if err := c.afterCAS(*e); err != nil {
			return fail("interrupted", "The restore was interrupted before its outcome was recorded.")
		}
	}
	id, err := c.record(s, c.stamp(restoreOutcome, *e, o.PlanID, outcome), len(records)+2)
	if err != nil {
		e.Outcome = outcome
		r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "restore-journal-unwritten", Message: "The outcome could not be recorded; the reference effect is as reported."})
		return true
	}
	ref.Outcome = id
	return false
}
