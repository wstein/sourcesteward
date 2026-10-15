// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"os"
	"reflect"
)

func selectedRetirementObservation(o RetirementObservation, p RetirementPlanRecord) RetirementObservation {
	o.Actions = append([]RetirementAction{}, o.Actions...)
	for i := range o.Actions {
		a := &o.Actions[i]
		if a.Name == "remove-files" && p.RemoveFiles == "" {
			a.Decision = "retain"
			a.Reason = "Filesystem removal was not independently selected."
			a.Blockers = []string{"Exact filesystem removal is not authorized by this plan."}
		}
		if a.Name == "remove-registration" && p.Backend == "git" && p.RemoveFiles == "" {
			a.Decision = "retain"
			a.Reason = "Native Git cannot remove this present registration without its files."
			a.Blockers = []string{"Git requires separate exact files selection for its coupled native removal."}
		}
	}
	return o
}
func (c retirementCoordinator) initialRetirementObservation(ctx context.Context, s *store, p RetirementPlanRecord, a retirementAdapter) (RetirementObservation, error) {
	if err := verifyRetirementOwnership(s, p); err != nil {
		return RetirementObservation{}, err
	}
	cwd, err := c.cwd()
	if err != nil {
		return RetirementObservation{}, err
	}
	if err = validateRetirementSource(ctx, p, cwd); err != nil {
		return RetirementObservation{}, err
	}
	id := c.inspect(ctx, p.SourcePath)
	if id.Status != "ok" || id.Backend != p.Backend {
		return RetirementObservation{}, failure("changed-source", "Source ownership changed; owned retirement is refused.")
	}
	o, err := a.Observe(ctx, p.SourcePath, p.Expected.Expected.BaseSelector, p.Task, p.CreationPlan)
	if err != nil {
		return o, err
	}
	o = selectedRetirementObservation(o, p)
	if err = validateRetirementObservation(o, p.SourcePath, p.Expected.Expected.BaseSelector, p.Task); err != nil {
		return o, err
	}
	if !o.WorkspacePathsComplete {
		return o, failure("unknown-workspace-path", "Complete registered workspace roots are unavailable.")
	}
	if err = qualifyTidyWorkspaceRoots(ctx, o.WorkspacePaths, p.StateDir); err != nil {
		return o, err
	}
	return o, nil
}
func (c retirementCoordinator) sourceRetirementPost(ctx context.Context, s *store, p RetirementPlanRecord, expected Snapshot, a retirementAdapter, recovery TidyRecovery) error {
	if err := verifyRetirementOwnership(s, p); err != nil {
		return err
	}
	cwd, err := c.cwd()
	if err != nil {
		return err
	}
	if err = validateRetirementSource(ctx, p, cwd); err != nil {
		return err
	}
	id := c.inspect(ctx, p.SourcePath)
	if id.Status != "ok" || id.Backend != p.Backend {
		return failure("changed-source", "Source backend ownership changed after retirement effects.")
	}
	fresh, err := c.sourceObserve(ctx, p.Backend, p.SourcePath, p.Expected.Expected.BaseSelector)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(fresh, expected) {
		return failure("stale-retirement", "Source/base/refs/registration/operation/safety state changed after the recorded native effect.")
	}
	verifier, ok := a.(retirementPostVerifier)
	if !ok {
		return failure("unsupported-capability", "The selected backend cannot read-only verify retained recovery and exact registration absence.")
	}
	return verifier.VerifyPost(ctx, p, recovery, expected)
}
func (c retirementCoordinator) apply(ctx context.Context, o TidyApplyOptions) TidyResult {
	r := newRetirementResult("tidy-apply")
	if !o.Authorize {
		return refuseTidy(r, "authorization-required", "Owned retirement requires explicit --authorize for its separately selected registration and filesystem effects.")
	}
	if !validID(o.PlanID) || o.StateDir == "" || !validText(o.StateDir) {
		return refuseTidy(r, "invalid-input", "A stored owned-retirement plan and private state directory are required.")
	}
	if err := ctx.Err(); err != nil {
		return failTidy(r, err)
	}
	s, err := openRetirementStore(o.StateDir, false)
	if err != nil {
		return failTidy(r, err)
	}
	defer s.Close()
	if err = checkOtherTidyNamespace(s, o.PlanID, "tidy-plans"); err != nil {
		return failTidy(r, err)
	}
	var p RetirementPlanRecord
	if err = s.read("tidy-retirement-plans", o.PlanID, &p); err != nil {
		return failTidy(r, err)
	}
	if err = validateRetirementPlan(p, o.StateDir, o.PlanID); err != nil {
		return failTidy(r, err)
	}
	r.Backend = p.Backend
	r.Retirement = &p
	r.Limitations = append(append([]string{}, p.RecoveryLimits...), "Recovery journal: "+retirementJournalPointer(p))
	if !retirementEligible(p) {
		return refuseTidy(r, "retirement-retained", "This plan retains the target; no selected deletion is fully proven and supported.")
	}
	a, err := c.adapter(p.Backend)
	if err != nil {
		return failTidy(r, err)
	}
	if _, ok := a.(retirementPostVerifier); !ok {
		return refuseTidy(r, "unsupported-capability", "Owned retirement requires read-only native recovery and registration postcondition verification before any effects.")
	}
	var j RetirementJournal
	err = s.read("tidy-retirement-operations", p.PlanID, &j)
	if err == nil {
		return c.retryRetirement(ctx, s, p, j, a, r)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return failTidy(r, err)
	}
	observation, err := c.initialRetirementObservation(ctx, s, p, a)
	if err != nil {
		return failTidy(r, err)
	}
	if !reflect.DeepEqual(observation, p.Expected) {
		return refuseTidy(r, "stale-retirement", "Owned target, disk, metadata, siblings or native state changed; create a new retirement plan.")
	}
	// Initial apply is fully preflighted through read-only state before opening
	// the writable lock. Completed/incomplete retries above never initialize state.
	s.Close()
	s, err = openRetirementStore(o.StateDir, true)
	if err != nil {
		return failTidy(r, err)
	}
	defer s.Close()
	if err = checkOtherTidyNamespace(s, p.PlanID, "tidy-plans"); err != nil {
		return failTidy(r, err)
	}
	var lockedPlan RetirementPlanRecord
	if err = s.read("tidy-retirement-plans", p.PlanID, &lockedPlan); err != nil {
		return failTidy(r, err)
	}
	if !reflect.DeepEqual(lockedPlan, p) {
		return refuseTidy(r, "changed-plan", "Immutable retirement plan changed while locking state.")
	}
	if err = s.read("tidy-retirement-operations", p.PlanID, &j); err == nil {
		return c.retryRetirement(ctx, s, p, j, a, r)
	} else if !errors.Is(err, os.ErrNotExist) {
		return failTidy(r, err)
	}
	observation, err = c.initialRetirementObservation(ctx, s, p, a)
	if err != nil {
		return failTidy(r, err)
	}
	if !reflect.DeepEqual(observation, p.Expected) {
		return refuseTidy(r, "stale-retirement", "Native target or ownership changed while locking state.")
	}
	recovery := retirementRecoveryFor(p)
	j = RetirementJournal{SchemaVersion: 2, PlanID: p.PlanID, PlanDigest: p.Digest, Phase: "prepared", ExpectedSnapshot: p.Expected.Expected, PlannedRecovery: recovery, Actions: []ActionOutcome{{"prepare-recovery", "completed", "Durably predeclared native recovery root " + recovery.Name + " at target " + recovery.CommitID + " before retirement."}}, Diagnostics: []Diagnostic{}}
	if err = writeRetirementJournal(s, p, j, true); err != nil {
		return failTidy(r, err)
	}
	confirmed, outcome, actionErr := a.PrepareRecovery(ctx, p)
	appendRetirementOutcome(&j, "create-recovery", &outcome, &actionErr)
	if actionErr == nil && ctx.Err() != nil {
		actionErr = ctx.Err()
	}
	if actionErr != nil || !outcome.Complete || !outcome.Attempted || outcome.Expected == nil || !validConfirmedTidyRecovery(confirmed, recovery, p.Backend) || !validTidyNativeTransition(p.Expected.Expected, *outcome.Expected) || (p.Backend == "jj" && confirmed.OperationID != outcome.Expected.Identity.OperationID) {
		if actionErr == nil {
			actionErr = failure("recovery-unconfirmed", "Exact native recovery root and permitted poststate could not be confirmed; retirement stopped before removal.")
		}
		return c.partialRetirement(s, p, j, r, actionErr)
	}
	afterPin, err := c.initialRetirementObservation(ctx, s, p, a)
	if err != nil {
		return c.partialRetirement(s, p, j, r, err)
	}
	if !retirementObservationAfterPin(p.Expected, afterPin, *outcome.Expected) {
		return c.partialRetirement(s, p, j, r, failure("unrelated-native-delta", "Recovery altered original target, disk, metadata, siblings, base or ownership beyond its declared native pin effect."))
	}
	j.Recovery = &confirmed
	j.ExpectedSnapshot = *outcome.Expected
	j.Phase = "recovery-confirmed"
	if err = writeRetirementJournal(s, p, j, false); err != nil {
		return c.partialRetirement(s, p, j, r, err)
	}
	// Reread completed creation ownership and the full original physical target
	// again immediately before the native registration action.
	beforeRemoval, err := c.initialRetirementObservation(ctx, s, p, a)
	if err != nil {
		return c.partialRetirement(s, p, j, r, err)
	}
	if !reflect.DeepEqual(beforeRemoval, afterPin) {
		return c.partialRetirement(s, p, j, r, failure("stale-retirement", "Target or ownership changed after recovery confirmation; removal was not attempted."))
	}
	if err = ctx.Err(); err != nil {
		return c.partialRetirement(s, p, j, r, err)
	}
	outcome, actionErr = a.RemoveRegistration(ctx, p, confirmed, j.ExpectedSnapshot)
	appendRetirementOutcome(&j, "remove-registration", &outcome, &actionErr)
	if actionErr == nil && ctx.Err() != nil {
		actionErr = ctx.Err()
	}
	if actionErr != nil || !outcome.Complete || !outcome.Attempted || outcome.Expected == nil || !validSnapshot(*outcome.Expected) || !retirementSnapshotAfterRegistration(j.ExpectedSnapshot, *outcome.Expected) {
		if actionErr == nil {
			actionErr = failure("registration-unconfirmed", "Native registration removal was not fully verified; inspect known effects and recovery.")
		}
		return c.partialRetirement(s, p, j, r, actionErr)
	}
	if err = c.sourceRetirementPost(ctx, s, p, *outcome.Expected, a, confirmed); err != nil {
		return c.partialRetirement(s, p, j, r, err)
	}
	j.ExpectedSnapshot = *outcome.Expected
	j.Phase = "registration-complete"
	if err = writeRetirementJournal(s, p, j, false); err != nil {
		return c.partialRetirement(s, p, j, r, err)
	}
	if p.RemoveFiles != "" {
		if err = c.sourceRetirementPost(ctx, s, p, j.ExpectedSnapshot, a, confirmed); err != nil {
			return c.partialRetirement(s, p, j, r, err)
		}
		if p.Backend == "jj" {
			if err = verifyRecordedRetirementManifest(ctx, p.Expected.Destination, p.Expected.Manifest); err != nil {
				return c.partialRetirement(s, p, j, r, err)
			}
		} else if !retirementPathAbsent(p.Task.Destination, p.Expected.DestinationParent) {
			return c.partialRetirement(s, p, j, r, failure("filesystem-unconfirmed", "Git native removal did not leave the exact destination absent."))
		}
		outcome, actionErr = a.RemoveFiles(ctx, p, confirmed, j.ExpectedSnapshot)
		appendRetirementOutcome(&j, "remove-files", &outcome, &actionErr)
		if actionErr == nil && ctx.Err() != nil {
			actionErr = ctx.Err()
		}
		if actionErr != nil || !outcome.Complete || outcome.Expected == nil || !reflect.DeepEqual(*outcome.Expected, j.ExpectedSnapshot) {
			if actionErr == nil {
				actionErr = failure("filesystem-unconfirmed", "Separately authorized file removal did not preserve exact native source state.")
			}
			return c.partialRetirement(s, p, j, r, actionErr)
		}
		if !retirementPathAbsent(p.Task.Destination, p.Expected.DestinationParent) {
			return c.partialRetirement(s, p, j, r, failure("filesystem-unconfirmed", "The exact owned destination is not absent after file removal."))
		}
		if err = c.sourceRetirementPost(ctx, s, p, j.ExpectedSnapshot, a, confirmed); err != nil {
			return c.partialRetirement(s, p, j, r, err)
		}
		j.Phase = "files-complete"
		if err = writeRetirementJournal(s, p, j, false); err != nil {
			return c.partialRetirement(s, p, j, r, err)
		}
	} else {
		if err = verifyRecordedRetirementManifest(ctx, p.Expected.Destination, p.Expected.Manifest); err != nil {
			return c.partialRetirement(s, p, j, r, err)
		}
	}
	j.Phase = "complete"
	if err = writeRetirementJournal(s, p, j, false); err != nil {
		return c.partialRetirement(s, p, j, r, err)
	}
	r.Status = "ok"
	r.Actions = j.Actions
	return boundedTidy(r)
}
func retirementObservationAfterPin(original, fresh RetirementObservation, expected Snapshot) bool {
	if !reflect.DeepEqual(fresh.Expected, expected) {
		return false
	}
	original.Expected = fresh.Expected
	if original.Target.Backend == "jj" {
		if fresh.Target.OperationID != expected.Identity.OperationID {
			return false
		}
		original.Target.OperationID = fresh.Target.OperationID
	}
	return reflect.DeepEqual(original, fresh)
}
func appendRetirementOutcome(j *RetirementJournal, name string, out *TidyNativeOutcome, err *error) {
	entry := TidyActionRecord{Actions: []ActionOutcome{}}
	if !addTidyNativeOutcome(&entry, name, *out) {
		out.Complete = false
		*err = failure("invalid-native-outcome", "Native retirement outcome is malformed or exceeds bounded evidence; possible effects require manual inspection.")
	}
	if name == "remove-files" && out.Complete && !out.Attempted && *err == nil {
		for i := range entry.Actions {
			entry.Actions[i].Status = "completed"
		}
		if len(entry.Actions) > 0 {
			entry.Actions[0].Detail = "Verified exact filesystem postcondition; no duplicate physical removal was attempted."
		}
	}
	j.Actions = append(j.Actions, entry.Actions...)
}
func writeRetirementJournal(s *store, p RetirementPlanRecord, j RetirementJournal, exclusive bool) error {
	if !retirementJournalEncodedBound(j) {
		return failure("record-limit", "Retirement journal exceeded its complete bounded evidence; further actions are refused.")
	}
	return s.write("tidy-retirement-operations", p.PlanID, j, exclusive)
}
func (c retirementCoordinator) partialRetirement(s *store, p RetirementPlanRecord, j RetirementJournal, r TidyResult, err error) TidyResult {
	j.Phase = "incomplete"
	failed := failResult(newResult("tidy-apply"), err)
	j.Diagnostics = failed.Diagnostics
	writeErr := writeRetirementJournal(s, p, j, false)
	r.Status = "partial"
	r.Actions = j.Actions
	r.Actions = append(r.Actions, ActionOutcome{"remaining-work", "incomplete", "Retirement stopped; retain remaining files and inspect the durable journal and predeclared recovery root. No automatic replay or restoration."})
	r.Diagnostics = j.Diagnostics
	if writeErr != nil {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{"journal-write-failed", "The latest retirement update failed; inspect the earlier durable prepared journal and exact predeclared native root."})
	}
	return boundedTidy(r)
}
func validRetirementJournal(j RetirementJournal, p RetirementPlanRecord) bool {
	if j.SchemaVersion != 2 || j.PlanID != p.PlanID || j.PlanDigest != p.Digest || !validSnapshot(j.ExpectedSnapshot) || j.Actions == nil || j.Diagnostics == nil || j.PlannedRecovery != retirementRecoveryFor(p) || !retirementJournalEncodedBound(j) {
		return false
	}
	before, after := p.Expected.Expected, j.ExpectedSnapshot
	before.RegistrationDigest = after.RegistrationDigest
	if !sameTidyStableState(before, after) {
		return false
	}
	switch j.Phase {
	case "prepared", "recovery-confirmed", "registration-complete", "files-complete", "complete", "incomplete":
	default:
		return false
	}
	for _, action := range j.Actions {
		if action.Name == "" || !validText(action.Name) || !validText(action.Detail) || (action.Status != "completed" && action.Status != "incomplete" && action.Status != "refused") {
			return false
		}
	}
	for _, d := range j.Diagnostics {
		if d.Code == "" || d.Message == "" || !validText(d.Code) || !validText(d.Message) {
			return false
		}
	}
	if j.Phase == "complete" {
		if len(j.Diagnostics) > 0 || j.Recovery == nil || !validConfirmedTidyRecovery(*j.Recovery, j.PlannedRecovery, p.Backend) {
			return false
		}
		created, removed, files := false, false, p.RemoveFiles == ""
		for _, action := range j.Actions {
			if action.Status != "completed" {
				continue
			}
			switch action.Name {
			case "create-recovery":
				created = true
			case "remove-registration":
				removed = true
			case "remove-files":
				files = true
			}
		}
		if !created || !removed || !files {
			return false
		}
	}
	return true
}
func (c retirementCoordinator) retryRetirement(ctx context.Context, s *store, p RetirementPlanRecord, j RetirementJournal, a retirementAdapter, r TidyResult) TidyResult {
	if !validRetirementJournal(j, p) {
		return failTidy(r, failure("invalid-state", "Retirement journal is malformed or not bound to the exact immutable plan."))
	}
	if err := verifyRetirementOwnership(s, p); err != nil {
		return failTidy(r, err)
	}
	r.Actions = j.Actions
	if j.Phase != "complete" {
		r.Status = "partial"
		r.Diagnostics = []Diagnostic{{"incomplete-operation", "Prior retirement may have native recovery, registration or file effects; automatic repeated removal/adoption is refused."}}
		return boundedTidy(r)
	}
	if err := c.sourceRetirementPost(ctx, s, p, j.ExpectedSnapshot, a, *j.Recovery); err != nil {
		return failTidy(r, err)
	}
	if p.RemoveFiles != "" {
		if !retirementPathAbsent(p.Task.Destination, p.Expected.DestinationParent) {
			return refuseTidy(r, "changed-destination", "Completed retirement destination exists or its parent changed; no effects were repeated.")
		}
	} else {
		if err := verifyRecordedRetirementManifest(ctx, p.Expected.Destination, p.Expected.Manifest); err != nil {
			return failTidy(r, err)
		}
	}
	r.Status = "ok"
	r.Actions = append(r.Actions, ActionOutcome{"retry", "completed", "Exact recorded source poststate and allowed remaining-file state verified; no retirement effect was repeated."})
	return boundedTidy(r)
}
