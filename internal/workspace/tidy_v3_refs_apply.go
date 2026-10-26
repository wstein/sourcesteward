// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
)

type tidyV3ApplyRefTransaction interface {
	Commit(context.Context, func(context.Context) error) (gitRefTransactionOutcome, error)
	Abort() error
	outcome() gitRefTransactionOutcome
}
type tidyV3RefsApplyCoordinator struct {
	refs      tidyV3RefsCoordinator
	openStore func(string, bool, []string, []string) (*v3Store, error)
	pin       func(context.Context, string, NativeIdentity, TidyRecovery, []string) (bool, error)
	prepare   func(context.Context, string, NativeIdentity, string, string, []string, []string) (tidyV3ApplyRefTransaction, error)
	// chain holds the journaled effects of the other plans of the plan set; nil when none.
	chain *v3Chain
}

func defaultTidyV3RefsApplyCoordinator() tidyV3RefsApplyCoordinator {
	return tidyV3RefsApplyCoordinator{refs: defaultTidyV3RefsCoordinator(), openStore: openV3Store, pin: pinTidyV3Recovery,
		prepare: func(ctx context.Context, bin string, id NativeIdentity, ref, oid string, write, ro []string) (tidyV3ApplyRefTransaction, error) {
			return prepareGitRefDeletion(ctx, bin, id, ref, oid, write, ro)
		}}
}

// v3ApplyTimeout is the apply deadline: 60 s plus 5 s per planned effect (a
// sandboxed native process costs about 1.8 s each) plus 50 ms per recorded
// manifest entry, because every apply observation re-reads each file. It has no
// ceiling: the work, not a constant, bounds the time, and the caller's own
// context (or --timeout) still ends it.
// v3ApplyBaseTimeout is the fixed part of the apply deadline; a variable only
// so tests can make the default small and prove that --timeout overrides it.
var (
	v3ApplyBaseTimeout = 60 * time.Second
	v3ApplyPerEffect   = 5 * time.Second
	v3ApplyPerEntry    = 50 * time.Millisecond
)

func v3ApplyTimeout(effects, entries int) time.Duration {
	return v3ApplyBaseTimeout + time.Duration(effects)*v3ApplyPerEffect + time.Duration(entries)*v3ApplyPerEntry
}

// v3PlanApplyTimeout derives the apply deadline from the stored plan.
func v3PlanApplyTimeout(p V3Plan) time.Duration {
	effects, entries := 0, 0
	for _, candidate := range p.Candidates {
		if candidate.Decision == "delete" {
			effects++
		}
	}
	if p.Retirement != nil {
		effects++
		entries = len(p.Retirement.Disk.Entries) + len(p.Retirement.Metadata.Entries)
	}
	return v3ApplyTimeout(effects, entries)
}

type v3TimeoutKey struct{}

// withV3Timeout carries an explicit --timeout through the operation so that it
// replaces the default deadline of every phase. Zero keeps the defaults.
func withV3Timeout(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, v3TimeoutKey{}, d)
}

// v3Timeout is the deadline of one phase: the explicit override or the default.
func v3Timeout(ctx context.Context, def time.Duration) time.Duration {
	if d, _ := ctx.Value(v3TimeoutKey{}).(time.Duration); d > 0 {
		return d
	}
	return def
}

func (c tidyV3RefsApplyCoordinator) apply(ctx context.Context, o V3ApplyOptions) V3Result {
	ctx, cancel := context.WithCancel(withTidyApplyPolicy(ctx))
	defer cancel()
	return boundV3Result(c.applyRefs(ctx, o))
}

// Bootstrap is strictly read-only: the immutable plan supplies complete original
// roots, then fresh native roots qualify the descriptor-pinned writable store.
func readTidyV3ApplyRecords(o V3ApplyOptions) (V3Plan, V3NativeState, *V3Journal, error) {
	var p V3Plan
	var native V3NativeState
	raw, err := openStoreKinds(o.StateDir, false, []string{"tidy-v3-plans", "tidy-v3-operations"})
	if err != nil {
		return p, native, nil, err
	}
	defer raw.Close()
	s := &v3Store{store: raw, protected: []string{}}
	p, native, err = s.readPlan(o.PlanID)
	if err != nil {
		return p, native, nil, err
	}
	if err = validateV3ApplyOptions(o, p); err != nil {
		return p, native, nil, err
	}
	if p.Mode != "local-refs" && p.Mode != "tracking-refs" {
		return p, native, nil, failure("unsupported-mode", "This apply coordinator supports local-reference and tracking-reference plans only.")
	}
	journal, err := s.readJournal(p)
	if errors.Is(err, os.ErrNotExist) {
		return p, native, nil, nil
	}
	if err != nil {
		return p, native, nil, err
	}
	return p, native, &journal, nil
}
func (c tidyV3RefsApplyCoordinator) applyRefs(ctx context.Context, o V3ApplyOptions) (r V3Result) {
	r = newV3Result("tidy-apply")
	if !o.Authorize {
		return failTidyV3(r, failure("authorization-required", "Apply requires explicit acknowledgment of exact stored local effects."))
	}
	if !validID(o.PlanID) || !v3CanonicalPath(o.StateDir) || !validV3RunMode(o.RunMode) {
		return failTidyV3(r, failure("invalid-options", "An exact stored plan identity, canonical private state directory and a complete run mode are required."))
	}
	if err := ctx.Err(); err != nil {
		return failTidyV3(r, err)
	}
	p, initial, prior, err := readTidyV3ApplyRecords(o)
	if err != nil {
		return failTidyV3(r, err)
	}
	if c.chain, err = loadV3Chain(o.StateDir, p, initial, o.SiblingPlans); err != nil {
		return failTidyV3(r, err)
	}
	// The stored plan is read before the deadline exists because the deadline
	// depends on its size; that read is bounded local state I/O and still ends
	// with the caller's own context.
	var stop context.CancelFunc
	ctx, stop = context.WithTimeout(ctx, v3Timeout(ctx, v3PlanApplyTimeout(p)))
	defer stop()
	r.Backend = p.Expected.Identity.Backend
	r.Limitations = append(append([]string{}, p.RecoveryLimits...), "Recovery journal: "+filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json"))
	if p.Mode == "tracking-refs" {
		r.Limitations = append(r.Limitations, tidyV3TrackingSamplingLimit)
	}
	if prior != nil && prior.Phase != "complete" {
		r = failTidyV3(r, failure("incomplete-operation", "Prepared or uncertain operations require manual inspection; automatic replay is refused."))
		r.Status = "refused"
		if len(prior.KnownEffects) > 0 {
			r.Status = "partial"
		}
		r.Actions = tidyV3JournalActions(*prior)
		r.Diagnostics = append(r.Diagnostics, prior.Diagnostics...)
		return r
	}
	expected, native := p.Expected, initial
	if prior != nil {
		expected, native = prior.CurrentExpected, prior.CurrentNative
	}
	fresh, err := c.observeCurrent(ctx, p, expected, native)
	if err != nil {
		return failTidyV3(r, err)
	}
	// A completed retry opens no writable state handles or native writers.
	s, err := c.openStore(p.StateDir, prior == nil, v3NativeRoots(fresh.Native), fresh.Native.ProtectedRoots)
	if err != nil {
		return failTidyV3(r, err)
	}
	defer s.Close()
	stored, baseline, err := s.readPlan(p.PlanID)
	if err != nil {
		return failTidyV3(r, err)
	}
	if !reflect.DeepEqual(stored, p) || !reflect.DeepEqual(baseline, initial) {
		return failTidyV3(r, failure("changed-plan", "Immutable plan or private native baseline changed before apply."))
	}
	lockedJournal, jerr := s.readJournal(p)
	if prior == nil {
		if !errors.Is(jerr, os.ErrNotExist) {
			if jerr == nil {
				jerr = failure("incomplete-operation", "Another operation already claims this immutable plan.")
			}
			return failTidyV3(r, jerr)
		}
	} else if jerr != nil || !reflect.DeepEqual(lockedJournal, *prior) {
		if jerr == nil {
			jerr = failure("changed-state", "Completed journal changed before read-only verification.")
		}
		return failTidyV3(r, jerr)
	}
	fresh, err = c.observeCurrent(ctx, p, expected, native)
	if err != nil {
		return failTidyV3(r, err)
	}
	if prior == nil && len(v3Eligible(p)) > 0 {
		if err = tidyV3ApplyEffectGuard(ctx, p); err != nil {
			return failTidyV3(r, err)
		}
	}
	var session tidyV3Hosting
	if p.Online != nil {
		session, err = c.refs.openHosting(ctx, p.SourcePath, hosting.TidySessionOptions{Online: true, Apply: true, Remote: p.Online.Remote, Repo: p.Online.Repository.Name})
		if session != nil {
			defer func() {
				b := session.Budget()
				r.Boundary.NetworkAttempted = b.NetworkAttempted
				r.Boundary.Budget.HTTPRequests = int64(b.Requests)
				r.Boundary.Budget.HTTPBytes = b.RawBytes
				session.Close()
			}()
		}
		if err != nil {
			return failTidyV3(r, tidyV3HostingError(err))
		}
		if session == nil {
			return failTidyV3(r, failure("hosting-evidence-unavailable", "A complete typed hosting session is required."))
		}
		if tidyV3Repository(session.Repository()) != p.Online.Repository {
			return failTidyV3(r, failure("evidence-changed", "Provider repository identity changed since planning."))
		}
	}
	if prior != nil {
		err = verifyV3Retry(ctx, p, *prior, func(ctx context.Context, _ V3Plan, j V3Journal) error {
			if err := c.revalidateAll(ctx, p, j.CurrentExpected, j.CurrentNative, session); err != nil {
				return err
			}
			if _, err := c.observeCurrent(ctx, p, j.CurrentExpected, j.CurrentNative); err != nil {
				return err
			}
			current, err := s.readJournal(p)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, j) {
				return failure("changed-state", "Completed journal changed during read-only verification.")
			}
			return nil
		})
		if err != nil {
			return failTidyV3(r, err)
		}
		r.Status, r.Plan, r.Boundary.Complete = "ok", &p, true
		r.Actions = tidyV3JournalActions(*prior)
		return r
	}
	if err = c.revalidateAll(ctx, p, p.Expected, initial, session); err != nil {
		return failTidyV3(r, err)
	}
	if _, err = c.observeCurrent(ctx, p, p.Expected, initial); err != nil {
		return failTidyV3(r, err)
	}
	journal := newV3JournalWithMode(p, initial, o.RunMode)
	if err = s.prepareJournal(p, journal); err != nil {
		return failTidyV3(r, err)
	}
	r.Actions = append(r.Actions, ActionOutcome{Name: "prepare-journal", Status: "complete", Detail: "Durably recorded exact recovery and local reference effects before native mutation."})
	for index, target := range v3Eligible(p) {
		if err = c.revalidateAction(ctx, p, journal.CurrentExpected, journal.CurrentNative, target, session); err != nil {
			return c.stop(s, p, journal, r, err)
		}
		next := cloneTidyV3Journal(journal)
		next.Phase = "prepared"
		next.Entries[index].RecoveryAttempted = true
		next.KnownEffects = v3JournalEffects(p, next)
		if err = advanceTidyV3Journal(s, p, &journal, next); err != nil {
			return c.stop(s, p, journal, r, err)
		}
		after, afterNative, pinErr := c.createRecovery(ctx, p, journal, target, session)
		recovery := journal.Entries[index].PlannedRecovery
		if pinErr != nil {
			r.Actions = append(r.Actions, ActionOutcome{Name: "recovery:" + recovery.Name, Status: "unknown", Detail: "Recovery creation was predeclared but not confirmed; inspect the retained journal and exact root before retry."})
			return c.stop(s, p, journal, r, pinErr)
		}
		r.Actions = append(r.Actions, ActionOutcome{Name: "recovery:" + recovery.Name, Status: "complete", Detail: "Confirmed exact retained recovery at " + target.OID + "."})
		next = cloneTidyV3Journal(journal)
		next.Entries[index].Recovery = &recovery
		next.Entries[index].Phase = "recovery-confirmed"
		next.Entries[index].Branch = c.recordBranchEvidence(ctx, p, journal.CurrentExpected.Identity, target)
		next.Entries[index].Deltas = append(next.Entries[index].Deltas, V3StateDelta{Kind: "recovery-created", Before: journal.CurrentExpected, After: after})
		next.Phase, next.CurrentExpected, next.CurrentNative = "recovery-confirmed", after, afterNative
		next.KnownEffects = v3JournalEffects(p, next)
		if err = advanceTidyV3Journal(s, p, &journal, next); err != nil {
			return c.stop(s, p, journal, r, err)
		}
		if err = c.revalidateAction(ctx, p, journal.CurrentExpected, journal.CurrentNative, target, session); err != nil {
			return c.stop(s, p, journal, r, err)
		}
		bin, e := gitExecutable()
		if e != nil {
			return c.stop(s, p, journal, r, e)
		}
		ro, e := gitTidyWriteProtection(journal.CurrentExpected.Identity, []string{target.Name})
		if e != nil {
			return c.stop(s, p, journal, r, e)
		}
		tx, prepareErr := c.prepare(ctx, bin, journal.CurrentExpected.Identity, target.Name, target.OID, []string{journal.CurrentExpected.Identity.RepositoryDir}, ro)
		if prepareErr != nil || tx == nil {
			if prepareErr == nil {
				prepareErr = failure("native-prepare-unconfirmed", "Native reference preparation returned no acknowledged session.")
			}
			if tx != nil {
				out := tx.outcome()
				abortErr := tx.Abort()
				prepareErr = errors.Join(prepareErr, abortErr)
				r.Actions = append(r.Actions, ActionOutcome{Name: "prepare-reference:" + target.ActionID, Status: "unknown", Detail: tidyV3TransactionDetail(out, "Preparation failed; COMMIT was not sent.")})
			}
			return c.stop(s, p, journal, r, errors.Join(failure("native-prepare-unconfirmed", "Reference prepare for action "+target.ActionID+" was attempted but acknowledgment, abort or process completion may be unconfirmed; possible locks remain. COMMIT was not sent. Inspect manually; no automatic replay."), prepareErr), Diagnostic{Code: "native-prepare-target:" + target.ActionID, Message: target.Name})
		}
		next = cloneTidyV3Journal(journal)
		next.Phase = "target-prepared"
		next.Entries[index].Phase = "target-prepared"
		next.Entries[index].Transaction = "prepared"
		next.KnownEffects = v3JournalEffects(p, next)
		if err = advanceTidyV3Journal(s, p, &journal, next); err != nil {
			abortErr := tx.Abort()
			return c.stop(s, p, journal, r, errors.Join(err, abortErr))
		}
		out, commitErr := tx.Commit(ctx, func(ctx context.Context) error {
			if err := c.revalidateAction(ctx, p, journal.CurrentExpected, journal.CurrentNative, target, session); err != nil {
				return err
			}
			// Persist the commit intent before A can send the destructive command.
			next := cloneTidyV3Journal(journal)
			next.Entries[index].Transaction = "commit-attempted"
			next.KnownEffects = v3JournalEffects(p, next)
			return advanceTidyV3Journal(s, p, &journal, next)
		})
		if commitErr != nil || !out.Committed {
			if commitErr == nil {
				commitErr = failure("native-commit-unconfirmed", "Native deletion was not completely acknowledged.")
			}
			if out.Prepared && !out.CommitAttempted && !out.Committed && !out.Uncertain {
				return c.confirmedAbort(s, p, journal, index, r, commitErr)
			}
			r.Actions = append(r.Actions, ActionOutcome{Name: "delete-reference:" + target.ActionID, Status: "unknown", Detail: tidyV3TransactionDetail(out, "Native deletion was not confirmed. Prior recovery evidence is recorded in the journal; inspect current target and recovery manually.")})
			return c.stop(s, p, journal, r, commitErr)
		}
		desired := v3ApplyStateDelta(p, journal.CurrentNative, "reference-deleted", target.ActionID)
		desiredSnapshot := v3SnapshotWithState(journal.CurrentExpected, desired)
		if _, err = c.observeCurrent(ctx, p, desiredSnapshot, desired); err != nil {
			return c.stop(s, p, journal, r, err)
		}
		r.Actions = append(r.Actions, ActionOutcome{Name: "delete-reference:" + target.ActionID, Status: "complete", Detail: "Confirmed exact target absence and retained recovery after native commit."})
		next = cloneTidyV3Journal(journal)
		entry := &next.Entries[index]
		entry.Phase, entry.Transaction, entry.Outcome = "action-complete", "confirmed", "complete"
		entry.Deltas = append(entry.Deltas, V3StateDelta{Kind: "reference-deleted", Before: journal.CurrentExpected, After: desiredSnapshot})
		next.Phase, next.CurrentExpected, next.CurrentNative = "action-complete", desiredSnapshot, desired
		next.KnownEffects = v3JournalEffects(p, next)
		if err = advanceTidyV3Journal(s, p, &journal, next); err != nil {
			return c.stop(s, p, journal, r, err)
		}
	}
	next := cloneTidyV3Journal(journal)
	next.Phase = "complete"
	if err = advanceTidyV3Journal(s, p, &journal, next); err != nil {
		return c.stop(s, p, journal, r, err)
	}
	r.Status, r.Plan, r.Boundary.Complete = "ok", &p, true
	return r
}
func (c tidyV3RefsApplyCoordinator) observeCurrent(ctx context.Context, p V3Plan, expected Snapshot, native V3NativeState) (tidyV3NativeObservation, error) {
	var out tidyV3NativeObservation
	if err := ctx.Err(); err != nil {
		return out, err
	}
	identity := c.refs.inspect(ctx, p.SourcePath)
	if identity.Status != "ok" || identity.Backend != "git" {
		return out, failure("unsupported-backend", "Native source ownership is no longer admitted Git; retain all remaining targets.")
	}
	out, err := c.refs.observe(ctx, p.SourcePath, p.Expected.BaseSelector)
	if err != nil {
		return out, err
	}
	expected, native = c.chain.apply(expected, native)
	if out.Expected != expected || !reflect.DeepEqual(out.Native, native) {
		return out, failure("evidence-changed", "Native source, base, references, registrations or safety differ from the exact allowed state.")
	}
	if err = qualifyTidyV3Registrations(ctx, out.Native, nil, p.StateDir); err != nil {
		return out, err
	}
	if p.Online != nil {
		raw, digest, e := qualifyTidyV3Remote(out, p.Online.Remote, p.Online.Repository.Name)
		if p.Mode == "tracking-refs" {
			var mapping tidyV3TrackingMapping
			mapping, e = qualifyTidyV3TrackingMapping(out, p.Online.Remote, p.Online.Repository.Name)
			raw, digest = mapping.FetchURL, mapping.ConfigDigest
		}
		if e != nil {
			return out, e
		}
		if raw != p.Online.RemoteURL || digest != p.Online.ConfigDigest {
			return out, failure("evidence-changed", "Selected remote or exact configuration origins/bytes changed.")
		}
	}
	return out, nil
}
func (c tidyV3RefsApplyCoordinator) revalidateAll(ctx context.Context, p V3Plan, expected Snapshot, native V3NativeState, s tidyV3Hosting) error {
	if s != nil {
		if p.Online == nil || tidyV3Repository(s.Repository()) != p.Online.Repository {
			return failure("evidence-changed", "Provider repository identity changed.")
		}
		if err := s.RevalidateRepository(ctx); err != nil {
			return tidyV3HostingError(err)
		}
	}
	for _, target := range v3Eligible(p) {
		// The proofs compare the live snapshot, which holds the sibling effects too.
		live, _ := c.chain.apply(expected, native)
		if err := c.revalidateProof(ctx, p, live, target, s); err != nil {
			return err
		}
	}
	return ctx.Err()
}
func (c tidyV3RefsApplyCoordinator) revalidateProof(ctx context.Context, p V3Plan, expected Snapshot, target V3Candidate, s tidyV3Hosting) error {
	if p.Mode == "tracking-refs" {
		return revalidateTidyV3TrackingProof(ctx, p, target, s)
	}
	if p.Online != nil && s == nil {
		return failure("hosting-evidence-unavailable", "Explicit online proof requires the original operation-scoped hosting session.")
	}
	if s != nil && target.Proof != "github-merged-head" {
		if err := s.RevalidateRepository(ctx); err != nil {
			return tidyV3HostingError(err)
		}
	}
	if reviewedProofTag(target.Proof) {
		return c.revalidateReviewedDecision(ctx, p, expected, target)
	}
	if integrationProofTag(target.Proof) {
		return c.revalidateIntegrationProof(ctx, p, expected, target)
	}
	proof := V3CommitProof{OID: target.OID, Decision: target.Decision, Reason: target.Reason, Proof: target.Proof, WorkspacePaths: target.WorkspacePaths, Evidence: target.Evidence}
	return revalidateTidyV3Commit(ctx, s, expected, proof, func(ctx context.Context, oid, base string) (bool, error) {
		return c.refs.ancestry(ctx, expected, oid, base)
	})
}
func (c tidyV3RefsApplyCoordinator) revalidateAction(ctx context.Context, p V3Plan, expected Snapshot, native V3NativeState, target V3Candidate, s tidyV3Hosting) error {
	if _, err := c.observeCurrent(ctx, p, expected, native); err != nil {
		return err
	}
	live, _ := c.chain.apply(expected, native)
	if err := c.revalidateProof(ctx, p, live, target, s); err != nil {
		return err
	}
	// Provider reads may take time: sample complete native protections afterward.
	_, err := c.observeCurrent(ctx, p, expected, native)
	return err
}

// recordBranchEvidence reads the evidence of a local branch right before its
// deletion is prepared. Other modes record nothing; a failed read records
// unavailable evidence and never stops the apply.
func (c tidyV3RefsApplyCoordinator) recordBranchEvidence(ctx context.Context, p V3Plan, id NativeIdentity, target V3Candidate) *V3BranchEvidence {
	if p.Mode != "local-refs" || !strings.HasPrefix(target.Name, restoreRefPrefix) {
		return nil
	}
	bin, err := gitExecutable()
	if err != nil {
		return &V3BranchEvidence{ConfigStatus: branchStatusUnavailable, Config: []V3BranchConfigItem{}, BranchOID: target.OID, ReflogTip: branchStatusUnavailable}
	}
	return readBranchEvidence(ctx, bin, id, target.Name, target.OID)
}
func (c tidyV3RefsApplyCoordinator) createRecovery(ctx context.Context, p V3Plan, j V3Journal, target V3Candidate, s tidyV3Hosting) (Snapshot, V3NativeState, error) {
	recovery := v3RecoveryFor(p, target.ActionID, target.OID)
	bin, err := gitExecutable()
	if err != nil {
		return Snapshot{}, V3NativeState{}, err
	}
	if _, err = gitTidyWriteProtection(j.CurrentExpected.Identity, []string{target.Name, recovery.Name}); err != nil {
		return Snapshot{}, V3NativeState{}, err
	}
	ro, err := gitTidyWriteProtection(j.CurrentExpected.Identity, []string{recovery.Name})
	if err != nil {
		return Snapshot{}, V3NativeState{}, err
	}
	if err = c.revalidateAction(ctx, p, j.CurrentExpected, j.CurrentNative, target, s); err != nil {
		return Snapshot{}, V3NativeState{}, err
	}
	attempted, err := c.pin(ctx, bin, j.CurrentExpected.Identity, recovery, ro)
	if err != nil {
		return Snapshot{}, V3NativeState{}, err
	}
	if !attempted {
		return Snapshot{}, V3NativeState{}, failure("recovery-unconfirmed", "Native recovery creation was not attempted or confirmed.")
	}
	native := v3ApplyStateDelta(p, j.CurrentNative, "recovery-created", target.ActionID)
	expected := v3SnapshotWithState(j.CurrentExpected, native)
	if _, err = c.observeCurrent(ctx, p, expected, native); err != nil {
		return Snapshot{}, V3NativeState{}, err
	}
	if err = gitTidyLiteralRecovery(ctx, bin, p.SourcePath, recovery); err != nil {
		return Snapshot{}, V3NativeState{}, err
	}
	return expected, native, nil
}
func pinTidyV3Recovery(ctx context.Context, bin string, id NativeIdentity, recovery TidyRecovery, ro []string) (bool, error) {
	if recovery.Kind != "git-recovery-ref" || !strings.HasPrefix(recovery.Name, "refs/stw/recovery/v3/") || !v3OID(recovery.CommitID, id.ObjectFormat) {
		return false, failure("invalid-recovery", "Exact plan-derived native recovery is required.")
	}
	_, code, err := runSandboxed(ctx, bin, []string{"--no-replace-objects", "update-ref", "--no-deref", recovery.Name, recovery.CommitID, strings.Repeat("0", len(recovery.CommitID))}, id.WorkspaceRoot, []string{id.RepositoryDir}, ro)
	if err != nil {
		return true, gitExecutionError(err)
	}
	if code != 0 {
		return true, failure("recovery-unconfirmed", "Native recovery creation failed; inspect the predeclared root before retry.")
	}
	return true, nil
}
func cloneTidyV3Journal(j V3Journal) V3Journal {
	out := j
	out.InitialNative = cloneV3Native(j.InitialNative)
	out.CurrentNative = cloneV3Native(j.CurrentNative)
	out.KnownEffects = append([]string{}, j.KnownEffects...)
	out.Diagnostics = append([]Diagnostic{}, j.Diagnostics...)
	out.Entries = append([]V3JournalEntry{}, j.Entries...)
	for i := range out.Entries {
		out.Entries[i].Deltas = append([]V3StateDelta{}, j.Entries[i].Deltas...)
		if j.Entries[i].Recovery != nil {
			r := *j.Entries[i].Recovery
			out.Entries[i].Recovery = &r
		}
		out.Entries[i].Branch = cloneBranchEvidence(j.Entries[i].Branch)
	}
	return out
}
func advanceTidyV3Journal(s *v3Store, p V3Plan, current *V3Journal, next V3Journal) error {
	if err := s.advanceJournal(p, *current, next); err != nil {
		return err
	}
	*current = next
	return nil
}

// A's acknowledged, reaped no-send abort is terminal. Persist it exactly once;
// retrying stop after this write would attempt to rewrite a terminal journal.
func (c tidyV3RefsApplyCoordinator) confirmedAbort(s *v3Store, p V3Plan, j V3Journal, index int, r V3Result, err error) V3Result {
	failed := failTidyV3(r, err)
	failed.Status, failed.Plan, failed.Boundary.Complete = "partial", nil, false
	action := j.Entries[index].ActionID
	diagnostic := Diagnostic{Code: "native-commit-aborted:" + action, Message: "Native transaction for " + action + " confirmed that COMMIT was not sent; abort was acknowledged and the process reaped. Prior recovery evidence is recorded in the journal. Inspect current target and recovery manually; no automatic replay."}
	next := cloneTidyV3Journal(j)
	next.Phase = "incomplete"
	next.Entries[index].Transaction = "aborted"
	next.Diagnostics = append(next.Diagnostics, diagnostic)
	next.KnownEffects = v3JournalEffects(p, next)
	failed.Diagnostics = append(failed.Diagnostics, diagnostic)
	if e := s.advanceJournal(p, j, next); e != nil {
		failed.Diagnostics = append(failed.Diagnostics, Diagnostic{Code: "journal-update-failed", Message: "Confirmed no-send abort could not be durably recorded; the last journal may retain commit intent. Inspect the exact journal, current target and current recovery manually; no automatic replay."})
	}
	failed.Actions = append(failed.Actions, ActionOutcome{Name: "delete-reference:" + action, Status: "refused", Detail: diagnostic.Message})
	return failed
}
func (c tidyV3RefsApplyCoordinator) stop(s *v3Store, p V3Plan, j V3Journal, r V3Result, err error, diagnostics ...Diagnostic) V3Result {
	failed := failTidyV3(r, err)
	failed.Diagnostics = append(failed.Diagnostics, diagnostics...)
	failed.Status = "partial"
	failed.Plan = nil
	failed.Boundary.Complete = false
	next := cloneTidyV3Journal(j)
	next.Phase = "incomplete"
	for i := range next.Entries {
		if next.Entries[i].Transaction == "commit-attempted" {
			next.Entries[i].Transaction = "unknown"
		}
	}
	next.Diagnostics = append(next.Diagnostics, failed.Diagnostics...)
	next.KnownEffects = v3JournalEffects(p, next)
	if e := s.advanceJournal(p, j, next); e != nil {
		failed.Diagnostics = append(failed.Diagnostics, Diagnostic{Code: "journal-update-failed", Message: "The last durable journal may precede the observed effect; retain recovery and inspect the exact record before any further action."})
	}
	failed.Actions = append(failed.Actions, ActionOutcome{Name: "incomplete-operation", Status: "unknown", Detail: "Operation stopped; recovery, possible native locks and effects require manual inspection. No automatic replay or rollback."})
	return failed
}
func tidyV3JournalActions(j V3Journal) []ActionOutcome {
	out := []ActionOutcome{}
	transactions := map[string]string{}
	for _, entry := range j.Entries {
		transactions[entry.ActionID] = entry.Transaction
	}
	for _, effect := range j.KnownEffects {
		status := "complete"
		detail := "Durable prior journal records this effect; this invocation did not replay it."
		if strings.HasPrefix(effect, "possible-") {
			status = "unknown"
		}
		if action, lock := strings.CutPrefix(effect, "native-target-lock:"); lock {
			switch transactions[action] {
			case "aborted":
				detail = "Previously prepared transaction confirmed no COMMIT send; abort was acknowledged and the process reaped."
			case "confirmed":
				detail = "Previously prepared transaction completed with acknowledged COMMIT and process completion."
			default:
				status = "unknown"
			}
		}
		out = append(out, ActionOutcome{Name: effect, Status: status, Detail: detail})
	}
	return out
}
func tidyV3TransactionDetail(o gitRefTransactionOutcome, prefix string) string {
	if o.Uncertain {
		return prefix + " Native acknowledgment or process completion is uncertain; possible locks/effects remain."
	}
	if o.CommitAttempted {
		return prefix + " COMMIT was sent; no rollback is implied."
	}
	return prefix + " COMMIT was not sent; inspect any prepared native lock before retry."
}
