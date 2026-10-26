// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	"github.com/wstein/sourcesteward/internal/hosting"
)

func readTidyV3RetirementRecords(o V3ApplyOptions) (V3Plan, V3NativeState, *V3Journal, error) {
	var p V3Plan
	var native V3NativeState
	raw, e := openStoreKinds(o.StateDir, false, []string{"tidy-v3-plans", "tidy-v3-operations"})
	if e != nil {
		return p, native, nil, e
	}
	defer raw.Close()
	s := &v3Store{store: raw, protected: append([]string{}, o.ProtectWorktrees...)}
	p, native, e = s.readPlan(o.PlanID)
	if e != nil {
		return p, native, nil, e
	}
	if e = validateV3ApplyOptions(o, p); e != nil {
		return p, native, nil, e
	}
	if p.Mode != "legacy-retirement" && p.Mode != "owned-retirement" {
		return p, native, nil, failure("unsupported-mode", "This coordinator applies exact native Git retirement plans only.")
	}
	j, e := s.readJournal(p)
	if errors.Is(e, os.ErrNotExist) {
		return p, native, nil, nil
	}
	if e != nil {
		return p, native, nil, e
	}
	return p, native, &j, nil
}
func retirementV3Options(p V3Plan) V3PlanOptions {
	o := retirementV3BaseOptions(p)
	// A plan stored before retirement reused the integration proofs has no
	// policy and keeps the ancestry proof it was planned with.
	o.IntegrationProofs = []string{integrationAncestor}
	if p.ProofPolicy != nil {
		o.IntegrationProofs, o.AcceptWhitespaceDifferences = p.ProofPolicy.Kinds, p.ProofPolicy.AcceptWhitespaceDifferences
	}
	return o
}
func retirementV3BaseOptions(p V3Plan) V3PlanOptions {
	l := p.Retirement.Provenance.Legacy
	if p.Mode == "owned-retirement" {
		owned := p.Retirement.Provenance.Owned
		o := V3PlanOptions{Path: p.SourcePath, Base: p.Expected.BaseSelector, StateDir: p.StateDir, Owner: owned.Task.Owner, Task: owned.Task.TaskID, RemoveRegistration: true, RemoveFiles: p.Retirement.RemoveFiles, ProtectWorktrees: []string{}}
		if p.Online != nil {
			o.Online = true
			o.Remote = p.Online.Remote
			o.Repo = p.Online.Repository.Name
			o.PRHints = append([]V3PRHint{}, p.Online.PRHints...)
		}
		return o
	}
	o := V3PlanOptions{Path: p.SourcePath, Base: p.Expected.BaseSelector, StateDir: p.StateDir, Owner: l.Owner, CompletedWorktree: l.SelectedPath, RemoveRegistration: true, RemoveFiles: p.Retirement.RemoveFiles, ProtectWorktrees: append([]string{}, l.ProtectedRoots...)}
	if p.Online != nil {
		o.Online = true
		o.Remote = p.Online.Remote
		o.Repo = p.Online.Repository.Name
		o.PRHints = append([]V3PRHint{}, p.Online.PRHints...)
	}
	return o
}
func (c tidyV3RetirementCoordinator) apply(ctx context.Context, o V3ApplyOptions) (r V3Result) {
	ctx, cancel := context.WithCancel(withTidyApplyPolicy(ctx))
	defer cancel()
	budget := newTidyV3ProofBudget(true)
	defer func() { retirementV3BudgetResult(&r, budget); r = boundV3Result(r) }()
	r = newV3Result("tidy-apply")
	if !validV3RunMode(o.RunMode) {
		return failTidyV3(r, failure("invalid-options", "A complete bounded run mode is required."))
	}
	if !o.Authorize || !validID(o.PlanID) || !v3CanonicalPath(o.StateDir) {
		return failTidyV3(r, failure("authorization-required", "Exact private stored identity and explicit effect authorization are required."))
	}
	p, native, prior, e := readTidyV3RetirementRecords(o)
	if e != nil {
		return failTidyV3(r, e)
	}
	c.chain, e = loadV3Chain(o.StateDir, p, native, o.SiblingPlans)
	if e != nil {
		return failTidyV3(r, e)
	}
	// The deadline scales with the stored plan (60 s + 5 s + 50 ms per recorded
	// entry); reading the plan, bounded local state I/O, precedes it.
	ctx, cancelDeadline := context.WithTimeout(ctx, v3Timeout(ctx, v3PlanApplyTimeout(p)))
	defer cancelDeadline()
	r.Backend = "git"
	r.Limitations = append(append([]string{}, p.RecoveryLimits...), "Recovery journal: "+filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json"))
	if prior != nil && prior.Phase != "complete" {
		r = failTidyV3(r, failure("incomplete-operation", "Interrupted retirement requires manual inspection; automatic replay is refused."))
		r.Status = "refused"
		if len(prior.KnownEffects) > 0 {
			r.Status = "partial"
		}
		r.Actions = tidyV3JournalActions(*prior)
		return r
	}
	options := retirementV3Options(p)
	var first tidyV3LegacyObservation
	if prior == nil || !v3RetirementEligible(*p.Retirement) {
		// Native ownership, exact stored authority and all physical/content evidence
		// qualify any credential-bearing provider session before it is opened.
		first, e = c.observe(ctx, options, budget)
		if e != nil {
			return failTidyV3(r, e)
		}
		if p.Online != nil {
			first.Retirement.Integration = c.chain.retirement(*p.Retirement).Integration
			first.Retirement.Actions = p.Retirement.Actions
		}
		wantExpected, wantNative := c.chain.apply(p.Expected, native)
		if first.Native.Expected != wantExpected || !reflect.DeepEqual(first.Native.Native, wantNative) || !c.chain.sameRetirement(first.Retirement, *p.Retirement) {
			return failTidyV3(r, failure("evidence-changed", "Stored retirement authority/native/physical/content evidence changed before apply."))
		}
	} else if e = c.verifyAbsent(ctx, p, prior.CurrentExpected, prior.CurrentNative, nil); e != nil {
		return failTidyV3(r, e)
	}
	var session tidyV3Hosting
	if p.Online != nil {
		session, e = c.refs.openHosting(ctx, p.SourcePath, hosting.TidySessionOptions{Online: true, Apply: true, Remote: p.Online.Remote, Repo: p.Online.Repository.Name})
		if session != nil {
			defer func() {
				b := session.Budget()
				r.Boundary.NetworkAttempted = b.NetworkAttempted
				r.Boundary.Budget.HTTPRequests = int64(b.Requests)
				r.Boundary.Budget.HTTPBytes = b.RawBytes
				session.Close()
			}()
		}
		if e != nil {
			return failTidyV3(r, tidyV3HostingError(e))
		}
		if session == nil {
			return failTidyV3(r, failure("hosting-evidence-unavailable", "Original complete typed provider session is required."))
		}
	}
	if prior != nil {
		e = verifyV3Retry(ctx, p, *prior, func(ctx context.Context, _ V3Plan, j V3Journal) error {
			if !v3RetirementEligible(*p.Retirement) {
				_, err := c.observeCurrent(ctx, options, p, j.CurrentExpected, j.CurrentNative, first, budget, session)
				return err
			}
			return c.verifyAbsent(ctx, p, j.CurrentExpected, j.CurrentNative, session)
		})
		if e != nil {
			return failTidyV3(r, e)
		}
		// Read-only completed retry: re-open exact record, never writable state.
		_, _, again, e := readTidyV3RetirementRecords(o)
		if e != nil || !reflect.DeepEqual(again, prior) {
			return failTidyV3(r, failure("changed-state", "Completed journal changed during read-only verification."))
		}
		r.Status, r.Plan, r.Boundary.Complete = "ok", &p, true
		r.Actions = tidyV3JournalActions(*prior)
		return r
	}
	s, e := c.openStore(p.StateDir, true, v3NativeRoots(native), native.ProtectedRoots)
	if e != nil {
		return failTidyV3(r, e)
	}
	defer s.Close()
	stored, baseline, e := s.readPlan(p.PlanID)
	if e != nil {
		return failTidyV3(r, e)
	}
	if !reflect.DeepEqual(stored, p) || !reflect.DeepEqual(baseline, native) {
		return failTidyV3(r, failure("changed-plan", "Immutable retirement record changed before private lock."))
	}
	if _, e = s.readJournal(p); !errors.Is(e, os.ErrNotExist) {
		return failTidyV3(r, failure("incomplete-operation", "Another operation already claims this immutable retirement."))
	}
	sample := func(sampleCtx context.Context, expected Snapshot, state V3NativeState) error {
		_, e := c.observeCurrent(sampleCtx, options, p, expected, state, first, budget, session)
		return e
	}
	// #2 coordinator preflight under private store lock.
	if e = sample(ctx, p.Expected, native); e != nil {
		return failTidyV3(r, e)
	}
	j := newV3JournalWithMode(p, native, o.RunMode)
	if e = s.prepareJournal(p, j); e != nil {
		return failTidyV3(r, e)
	}
	stop := func(err error) V3Result {
		partial := r
		partial.Actions = append(append([]ActionOutcome{}, r.Actions...), tidyV3JournalActions(j)...)
		return (tidyV3RefsApplyCoordinator{}).stop(s, p, j, partial, err)
	}
	if len(j.Entries) == 0 {
		next := cloneTidyV3Journal(j)
		next.Phase = "complete"
		if e = advanceTidyV3Journal(s, p, &j, next); e != nil {
			return stop(e)
		}
		r.Status, r.Plan, r.Boundary.Complete = "ok", &p, true
		return r
	}
	// Recovery keeps its original tighter 30-second context, subordinate to
	// the non-resetting whole-operation deadline.
	recoveryCtx, recoveryCancel := context.WithTimeout(ctx, v3Timeout(ctx, v3PlanApplyTimeout(p)))
	defer recoveryCancel()
	// #3 recovery adapter before creation.
	if e = sample(recoveryCtx, j.CurrentExpected, j.CurrentNative); e != nil {
		return stop(e)
	}
	bin, e := gitExecutable()
	if e != nil {
		return stop(e)
	}
	recovery := j.Entries[0].PlannedRecovery
	ro, e := gitTidyWriteProtection(j.CurrentExpected.Identity, []string{recovery.Name})
	if e != nil {
		return stop(e)
	}
	next := cloneTidyV3Journal(j)
	next.Entries[0].RecoveryAttempted = true
	next.KnownEffects = v3JournalEffects(p, next)
	if e = advanceTidyV3Journal(s, p, &j, next); e != nil {
		return stop(e)
	}
	attempted, e := c.pin(recoveryCtx, bin, j.CurrentExpected.Identity, recovery, ro)
	if e != nil {
		return stop(e)
	}
	if !attempted {
		return stop(failure("recovery-unconfirmed", "Predeclared recovery creation was not confirmed."))
	}
	afterNative := v3ApplyStateDelta(p, j.CurrentNative, "recovery-created", p.Retirement.ActionID)
	after := v3SnapshotWithState(j.CurrentExpected, afterNative)
	// #4 recovery adapter after creation; recovery remains on any refusal.
	if e = sample(recoveryCtx, after, afterNative); e != nil {
		return stop(e)
	}
	if e = gitTidyLiteralRecovery(recoveryCtx, bin, p.SourcePath, recovery); e != nil {
		return stop(e)
	}
	next = cloneTidyV3Journal(j)
	next.Entries[0].Recovery = &recovery
	next.Entries[0].Phase = "recovery-confirmed"
	next.Entries[0].Deltas = append(next.Entries[0].Deltas, V3StateDelta{Kind: "recovery-created", Before: j.CurrentExpected, After: after})
	next.Phase = "recovery-confirmed"
	next.CurrentExpected, next.CurrentNative = after, afterNative
	next.KnownEffects = v3JournalEffects(p, next)
	if e = advanceTidyV3Journal(s, p, &j, next); e != nil {
		return stop(e)
	}
	// #5 coordinator after recovery; #6 coordinator before removal.
	if e = sample(ctx, j.CurrentExpected, j.CurrentNative); e != nil {
		return stop(e)
	}
	if e = sample(ctx, j.CurrentExpected, j.CurrentNative); e != nil {
		return stop(e)
	}
	removeCtx, removeCancel := context.WithTimeout(ctx, v3Timeout(ctx, v3PlanApplyTimeout(p)))
	defer removeCancel()
	// #7 native removal adapter before parent grant construction.
	if _, e = c.observeCurrent(removeCtx, options, p, j.CurrentExpected, j.CurrentNative, first, budget, session); e != nil {
		return stop(e)
	}
	if e = gitTidyLiteralRecovery(removeCtx, bin, p.SourcePath, recovery); e != nil {
		return stop(e)
	}
	parents := []PathIdentity{{Path: p.Retirement.Parent.Path, Device: p.Retirement.Parent.Device, Inode: p.Retirement.Parent.Inode}, {Path: p.Retirement.RegistrationParent.Path, Device: p.Retirement.RegistrationParent.Device, Inode: p.Retirement.RegistrationParent.Inode}}
	protection, e := gitRetirementProtection(parents, []string{p.Retirement.Destination.Path, p.Retirement.Registration.Path})
	if e != nil {
		return stop(e)
	}
	// #8 full native/content/authority proof after grants, immediately before intent.
	if _, e = c.observeCurrent(removeCtx, options, p, j.CurrentExpected, j.CurrentNative, first, budget, session); e != nil {
		return stop(e)
	}
	next = cloneTidyV3Journal(j)
	next.Entries[0].RemovalAttempted = true
	next.KnownEffects = v3JournalEffects(p, next)
	if e = advanceTidyV3Journal(s, p, &j, next); e != nil {
		return stop(e)
	}
	if e = removeCtx.Err(); e != nil {
		return stop(e)
	}
	var code int
	if p.Mode == "owned-retirement" {
		// All eight samples re-proved original creation identity and exact task lock.
		// Reuse the original owned-only runner without new argv or lock semantics.
		_, code, e = c.ownedRemove(removeCtx, bin, p.SourcePath, p.Retirement.Destination.Path, []string{parents[0].Path, parents[1].Path}, protection)
	} else {
		_, code, e = c.remove(removeCtx, bin, []string{"--no-replace-objects", "worktree", "remove", "--", p.Retirement.Destination.Path}, p.SourcePath, []string{parents[0].Path, parents[1].Path}, protection)
	}
	if e != nil {
		return stop(gitExecutionError(e))
	}
	if code != 0 {
		return stop(failure("retirement-native-remove-failed", "Native coupled removal failed; recovery and possible file/registration effects remain. No force retry or rollback."))
	}
	desiredNative := v3ApplyStateDelta(p, j.CurrentNative, "registration-removed", p.Retirement.ActionID)
	desired := v3SnapshotWithState(j.CurrentExpected, desiredNative)
	if e = c.verifyAbsent(ctx, p, desired, desiredNative, session); e != nil {
		return stop(e)
	}
	next = cloneTidyV3Journal(j)
	next.Entries[0].Phase = "registration-complete"
	next.Entries[0].RegistrationOutcome = "complete"
	next.Entries[0].Deltas = append(next.Entries[0].Deltas, V3StateDelta{Kind: "registration-removed", Before: j.CurrentExpected, After: desired})
	next.Phase = "registration-complete"
	next.CurrentExpected, next.CurrentNative = desired, desiredNative
	next.KnownEffects = v3JournalEffects(p, next)
	if e = advanceTidyV3Journal(s, p, &j, next); e != nil {
		return stop(e)
	}
	next = cloneTidyV3Journal(j)
	next.Entries[0].Phase = "files-complete"
	next.Entries[0].FilesOutcome = "complete"
	next.Phase = "files-complete"
	next.KnownEffects = v3JournalEffects(p, next)
	if e = advanceTidyV3Journal(s, p, &j, next); e != nil {
		return stop(e)
	}
	next = cloneTidyV3Journal(j)
	next.Entries[0].Phase = "action-complete"
	next.Entries[0].Outcome = "complete"
	next.Phase = "action-complete"
	if e = advanceTidyV3Journal(s, p, &j, next); e != nil {
		return stop(e)
	}
	next = cloneTidyV3Journal(j)
	next.Phase = "complete"
	if e = advanceTidyV3Journal(s, p, &j, next); e != nil {
		return stop(e)
	}
	r.Status, r.Plan, r.Boundary.Complete = "ok", &p, true
	r.Actions = tidyV3JournalActions(j)
	return r
}

// This is a postcondition control observation, not another content pass: the
// target is absent and Git performs no extra SourceSteward per-file unlink.
func (c tidyV3RetirementCoordinator) verifyAbsent(ctx context.Context, p V3Plan, expected Snapshot, native V3NativeState, session tidyV3Hosting) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	expected, native = c.chain.apply(expected, native)
	// Revalidate provider first, then sample native postconditions afterward.
	if session != nil {
		if e := (tidyV3RefsApplyCoordinator{refs: c.refs}).revalidateProof(ctx, p, expected, retirementV3Candidate(*p.Retirement), session); e != nil {
			return e
		}
	}
	out, e := c.refs.observe(ctx, p.SourcePath, p.Expected.BaseSelector)
	if e != nil {
		return e
	}
	out.Native.ProtectedRoots = append([]string{}, p.Retirement.ProtectedRoots...)
	out.Expected = v3SnapshotWithState(out.Expected, out.Native)
	if out.Expected != expected || !reflect.DeepEqual(out.Native, native) {
		return failure("evidence-changed", "Native post-state differs from exact recovery/registration delta; branches remain protected.")
	}
	if e = qualifyTidyV3Registrations(ctx, out.Native, nil, p.StateDir); e != nil {
		return e
	}
	if e = retirementV3NoOperation(ctx, out.Expected.Identity); e != nil {
		return e
	}
	bin, e := gitExecutable()
	if e != nil {
		return e
	}
	recovery := v3RecoveryFor(p, p.Retirement.ActionID, p.Retirement.Target.CommitID)
	if e = gitTidyLiteralRecovery(ctx, bin, p.SourcePath, recovery); e != nil {
		return e
	}
	// A writer can enter the target after the final sampled proof. Never turn
	// an unavailable/changed actual cwd after native effects into clean success.
	_, ancestors, e := retirementV3CurrentDirectory(ctx)
	if e != nil {
		return failure("evidence-changed", "Physical current-directory postcondition is unavailable after native effects; inspect recovery and exact outcomes.")
	}
	for _, ancestor := range ancestors {
		if ancestor.Device == p.Retirement.Destination.Device && ancestor.Inode == p.Retirement.Destination.Inode {
			return failure("active-workspace", "Actual current directory entered the selected physical target after its final sample.")
		}
	}
	parent, e := retirementV3Directory(p.Retirement.Parent.Path)
	wantParent := p.Retirement.Parent
	if e != nil || !retirementV3SamePostParent(parent, wantParent) {
		return failure("evidence-changed", "Destination parent physical owner, identity or mode changed across removal.")
	}
	for _, path := range []string{p.Retirement.Destination.Path, p.Retirement.Registration.Path} {
		if _, e := os.Lstat(path); !os.IsNotExist(e) {
			return failure("retirement-outcome-incomplete", "Selected destination and registration must both be proven absent.")
		}
	}
	stored := c.chain.retirement(*p.Retirement)
	siblings, e := retirementV3Siblings(p.Retirement.Parent.Path, p.Retirement.Destination.Path)
	if e != nil {
		return e
	}
	if !retirementV3SamePostSiblings(siblings, stored.Siblings, p.Retirement.RegistrationParent, c.chain) {
		return failure("evidence-changed", "Unrelated destination siblings changed across native removal.")
	}
	regParent, e := retirementV3Directory(p.Retirement.RegistrationParent.Path)
	wantRegParent := p.Retirement.RegistrationParent
	if e != nil || !retirementV3SamePostParent(regParent, wantRegParent) {
		return failure("evidence-changed", "Native registration parent physical owner, identity or mode changed across removal.")
	}
	regSiblings, e := retirementV3Siblings(wantRegParent.Path, p.Retirement.Registration.Path)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(regSiblings, stored.RegistrationSiblings) {
		return failure("evidence-changed", "Unrelated registration siblings changed from the immutable retirement plan.")
	}

	return ctx.Err()
}

// Selected child removal can change only these directory-container attributes.
// Every stable attribute still binds the original canonical parent role.
func retirementV3SamePostParent(got, want V3PhysicalIdentity) bool {
	return got.Path == want.Path && got.Device == want.Device && got.Inode == want.Inode && got.UID == want.UID && got.Mode == want.Mode && got.Kind == want.Kind && got.Kind == "directory" && got.OwnerKnown && want.OwnerKnown && got.TimestampPrecision == want.TimestampPrecision
}
func retirementV3SamePostSiblings(got, want []V3PhysicalIdentity, registrationParent V3PhysicalIdentity, chain *v3Chain) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if want[i] == registrationParent || chain.changed(want[i].Path) {
			if !retirementV3SamePostParent(got[i], want[i]) {
				return false
			}
		} else if got[i] != want[i] {
			return false
		}
	}
	return true
}
