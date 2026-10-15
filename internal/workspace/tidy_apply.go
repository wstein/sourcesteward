// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/wstein/sourcesteward/internal/inspection"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

type TidyActionRecord struct {
	Candidate TidyCandidate   `json:"candidate"`
	Recovery  TidyRecovery    `json:"recovery"`
	Phase     string          `json:"phase"`
	Actions   []ActionOutcome `json:"actions"`
}
type TidyJournal struct {
	SchemaVersion int                `json:"schema_version"`
	PlanID        string             `json:"plan_id"`
	PlanDigest    string             `json:"plan_digest"`
	Phase         string             `json:"phase"`
	Expected      Snapshot           `json:"expected"`
	Entries       []TidyActionRecord `json:"entries"`
	Diagnostics   []Diagnostic       `json:"diagnostics"`
}

func ApplyTidy(ctx context.Context, o TidyApplyOptions) TidyResult {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return boundedTidy((tidyCoordinator{inspect: inspection.Inspect, adapter: selectTidyAdapter}).apply(ctx, o))
}
func tidyApplyEffects(p TidyPlanRecord) []string {
	effects := []string{"Proposed local reference apply v1: persist prepared and confirmed recovery journals before native changes; planning changes no native state."}
	for _, c := range p.Candidates {
		if c.Decision == "delete" {
			r := tidyRecoveryFor(p, c)
			effects = append(effects, "Proposed local reference apply v1: create and retain exact native recovery root "+r.Name+" at commit "+c.CommitID+" change "+c.ChangeID+".", "Proposed local reference apply v1: delete only local "+c.Kind+" "+c.Name+" at commit "+c.CommitID+" change "+c.ChangeID+" after durable recovery and exact stale checks.")
		}
	}
	return effects
}
func tidyApplyRecoveryLimits() []string {
	return []string{"Local reference apply v1: no remote deletion, workspace registration removal, filesystem removal, jj abandonment, automatic rollback or garbage collection.", "Native recovery roots remain reachable until a human explicitly removes them; restoring names is manual and must not overwrite newer work.", "Sampled checks cannot exclude concurrent external or ABA changes; Git target CAS and jj operation-parent checks do not provide a full repository transaction.", "Prepared or incomplete operations require manual inspection; automatic repeated mutation and adoption are refused."}
}
func tidyApplyPurpose(p TidyPlanRecord) bool {
	return p.PlanID != "" && p.StateDir != "" && reflect.DeepEqual(p.Effects, tidyApplyEffects(p)) && reflect.DeepEqual(p.RecoveryLimits, tidyApplyRecoveryLimits())
}
func tidyJournalPointer(p TidyPlanRecord) string {
	return filepath.Join(p.StateDir, "tidy-operations", p.PlanID+".json")
}
func eligibleTidyCandidates(p TidyPlanRecord) []TidyCandidate {
	out := []TidyCandidate{}
	for _, c := range p.Candidates {
		if c.Decision == "delete" {
			out = append(out, c)
		}
	}
	return out
}
func (c tidyCoordinator) observeApply(ctx context.Context, a tidyAdapter, p TidyPlanRecord, expected Snapshot) (TidyObservation, error) {
	if err := ctx.Err(); err != nil {
		return TidyObservation{}, err
	}
	identity := c.inspect(ctx, p.SourcePath)
	if identity.Status != "ok" || identity.Backend != p.Backend {
		return TidyObservation{}, failure("changed-source", "Native repository ownership changed; retain all remaining targets.")
	}
	observation, err := a.Observe(ctx, p.SourcePath, p.Expected.BaseSelector)
	if err != nil {
		return observation, err
	}
	if err = validateTidyObservation(observation, p.Backend, p.SourcePath, p.Expected.BaseSelector); err != nil {
		return observation, err
	}
	if !observation.WorkspacePathsComplete {
		return observation, failure("unknown-workspace-path", "Native registered workspace roots are incomplete; no cleanup is authorized.")
	}
	if err = qualifyTidyWorkspaceRoots(ctx, observation.WorkspacePaths, p.StateDir); err != nil {
		return observation, err
	}
	if !reflect.DeepEqual(observation.Expected, expected) {
		return observation, failure("stale-plan", "Native source, base, references, registrations, operation or safety evidence changed; retain remaining targets.")
	}
	return observation, nil
}
func candidateInObservation(o TidyObservation, c TidyCandidate) bool {
	for _, v := range o.Candidates {
		if v.Name == c.Name {
			return reflect.DeepEqual(v, c)
		}
	}
	return false
}
func (c tidyCoordinator) apply(ctx context.Context, o TidyApplyOptions) TidyResult {
	r := newTidyResult("tidy-apply")
	if !o.Authorize {
		return refuseTidy(r, "authorization-required", "Apply requires explicit --authorize for the reviewed local effects.")
	}
	if !validID(o.PlanID) || !validText(o.StateDir) || o.StateDir == "" {
		return refuseTidy(r, "invalid-input", "A stored immutable plan ID and private state directory are required.")
	}
	if err := ctx.Err(); err != nil {
		return failTidy(r, err)
	}
	s, err := openTidyStore(o.StateDir, true)
	if err != nil {
		return failTidy(r, err)
	}
	defer s.Close()
	var p TidyPlanRecord
	if err = s.read("tidy-plans", o.PlanID, &p); err != nil {
		return failTidy(r, err)
	}
	if err = validateTidyPlan(p, o.StateDir, o.PlanID); err != nil {
		return failTidy(r, err)
	}
	r.Backend = p.Backend
	if !tidyApplyPurpose(p) {
		return refuseTidy(r, "replan-required", "This stored plan does not declare the recognized proposed native cleanup effects; generate a new plan.")
	}
	r.Plan = &p
	r.Limitations = append(r.Limitations, p.RecoveryLimits...)
	r.Limitations = append(r.Limitations, "Recovery journal: "+tidyJournalPointer(p))
	observer, err := c.adapter(p.Backend)
	if err != nil {
		return failTidy(r, err)
	}
	a, ok := observer.(tidyMutationAdapter)
	if !ok {
		return refuseTidy(r, "unsupported-capability", "This native backend cannot safely apply local reference cleanup.")
	}
	var j TidyJournal
	err = s.read("tidy-operations", p.PlanID, &j)
	if err == nil {
		return c.retryTidy(ctx, s, p, j, a, r)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return failTidy(r, err)
	}
	observation, err := c.observeApply(ctx, a, p, p.Expected)
	if err != nil {
		return refuseTidy(r, tidyErrorCode(err), "Stored plan is stale or native evidence is unavailable; generate a new plan.")
	}
	targets := eligibleTidyCandidates(p)
	for _, target := range targets {
		if !candidateInObservation(observation, target) {
			return refuseTidy(r, "stale-plan", "A planned target changed or became protected; retain all targets.")
		}
	}
	j = TidyJournal{SchemaVersion: 1, PlanID: p.PlanID, PlanDigest: p.Digest, Phase: "prepared", Expected: p.Expected, Entries: []TidyActionRecord{}, Diagnostics: []Diagnostic{}}
	if err = writeTidyJournal(s, p, j, true); err != nil {
		return failTidy(r, err)
	}
	for _, target := range targets {
		observation, err = c.observeApply(ctx, a, p, j.Expected)
		if err != nil {
			return c.partialTidy(s, p, j, r, err)
		}
		if !candidateInObservation(observation, target) {
			return c.partialTidy(s, p, j, r, failure("stale-plan", "Planned local target changed or became protected before its action."))
		}
		actionPlan := p
		actionPlan.Expected = j.Expected
		recovery := tidyRecoveryFor(actionPlan, target)
		entry := TidyActionRecord{Candidate: target, Recovery: recovery, Phase: "prepared", Actions: []ActionOutcome{{"prepare-recovery", "completed", "Durably predeclared exact native recovery root " + recovery.Name + " for " + target.Name + " at " + target.CommitID + "."}}}
		j.Entries = append(j.Entries, entry)
		if err = writeTidyJournal(s, p, j, false); err != nil {
			return c.partialTidy(s, p, j, r, err)
		}
		index := len(j.Entries) - 1
		confirmed, fresh, outcome, pinErr := a.PrepareRecovery(ctx, actionPlan, target)
		if !addTidyNativeOutcome(&j.Entries[index], "create-recovery", outcome) {
			outcome.Complete = false
			pinErr = failure("invalid-native-outcome", "Native recovery outcome exceeded complete evidence bounds; target deletion was not attempted.")
		}
		if pinErr == nil && ctx.Err() != nil {
			pinErr = ctx.Err()
		}
		if pinErr != nil || !outcome.Complete || !outcome.Attempted || !validConfirmedTidyRecovery(confirmed, recovery, p.Backend) || !validSnapshot(fresh) {
			if pinErr == nil {
				pinErr = failure("recovery-unconfirmed", "Exact native recovery could not be confirmed; target deletion was not attempted.")
			}
			return c.partialTidy(s, p, j, r, pinErr)
		}
		if _, err = c.observeApply(ctx, a, p, fresh); err != nil {
			return c.partialTidy(s, p, j, r, err)
		}
		j.Expected = fresh
		j.Entries[index].Recovery = confirmed
		j.Entries[index].Phase = "recovery-confirmed"
		if err = writeTidyJournal(s, p, j, false); err != nil {
			return c.partialTidy(s, p, j, r, err)
		}
		if err = ctx.Err(); err != nil {
			return c.partialTidy(s, p, j, r, err)
		}
		actionPlan.Expected = fresh
		outcome, deleteErr := a.Delete(ctx, actionPlan, target, confirmed, fresh)
		if !addTidyNativeOutcome(&j.Entries[index], "delete-reference", outcome) {
			outcome.Complete = false
			deleteErr = failure("invalid-native-outcome", "Native deletion outcome exceeded complete evidence bounds; inspect recovery before further work.")
		}
		if deleteErr == nil && ctx.Err() != nil {
			deleteErr = ctx.Err()
		}
		if deleteErr != nil || !outcome.Attempted || !outcome.Complete || outcome.Expected == nil || !validSnapshot(*outcome.Expected) {
			if deleteErr == nil {
				deleteErr = failure("deletion-unconfirmed", "Native target deletion is not completely verified; inspect the recovery journal.")
			}
			return c.partialTidy(s, p, j, r, deleteErr)
		}
		post, err := c.observeApply(ctx, a, p, *outcome.Expected)
		if err != nil {
			return c.partialTidy(s, p, j, r, err)
		}
		for _, remaining := range post.Candidates {
			if remaining.Name == target.Name {
				return c.partialTidy(s, p, j, r, failure("deletion-unconfirmed", "Target reference still exists after native deletion."))
			}
		}
		j.Expected = *outcome.Expected
		j.Entries[index].Phase = "deleted"
		if err = writeTidyJournal(s, p, j, false); err != nil {
			return c.partialTidy(s, p, j, r, err)
		}
	}
	j.Phase = "complete"
	if err = writeTidyJournal(s, p, j, false); err != nil {
		return c.partialTidy(s, p, j, r, err)
	}
	r.Status = "ok"
	r.Actions = tidyJournalActions(j)
	return boundedTidy(r)
}
func tidyErrorCode(err error) string {
	var operation operationError
	if errors.As(err, &operation) {
		return operation.Code
	}
	return "native-evidence-unavailable"
}
func validConfirmedTidyRecovery(got, want TidyRecovery, backend string) bool {
	if got.Kind != want.Kind || got.Name != want.Name || got.CommitID != want.CommitID || got.ChangeID != want.ChangeID {
		return false
	}
	if backend == "git" {
		return got.OperationID == ""
	}
	return validAlphabet(got.OperationID, 128, "0123456789abcdef")
}
func addTidyNativeOutcome(entry *TidyActionRecord, name string, out TidyNativeOutcome) bool {
	if !validStrings(out.Effects) || !validStrings(out.Limitations) || len(out.Effects)+len(out.Limitations) > observationLimit {
		entry.Actions = append(entry.Actions, ActionOutcome{name, "incomplete", "Native outcome evidence was malformed or exceeded bounds; effects may remain at the predeclared recovery root. Further mutation is refused."})
		return false
	}
	status := "incomplete"
	if out.Complete {
		status = "completed"
	}
	if !out.Attempted {
		status = "refused"
	}
	detail := "Native effect may be incomplete; inspect the predeclared recovery root."
	if !out.Attempted {
		detail = "Native action reports it was not attempted."
	}
	entry.Actions = append(entry.Actions, ActionOutcome{name, status, detail})
	for _, effect := range out.Effects {
		entry.Actions = append(entry.Actions, ActionOutcome{name, status, shortText(effect, 4096)})
	}
	for _, limit := range out.Limitations {
		entry.Actions = append(entry.Actions, ActionOutcome{"native-limit", "incomplete", shortText(limit, 4096)})
	}

	return true
}

func tidyJournalActions(j TidyJournal) []ActionOutcome {
	actions := []ActionOutcome{}
	for _, entry := range j.Entries {
		actions = append(actions, entry.Actions...)
	}
	return actions
}
func (c tidyCoordinator) partialTidy(s *store, p TidyPlanRecord, j TidyJournal, r TidyResult, err error) TidyResult {
	j.Phase = "incomplete"
	if len(j.Entries) > 0 {
		j.Entries[len(j.Entries)-1].Phase = "incomplete"
	}
	failed := failResult(newResult("tidy-apply"), err)
	j.Diagnostics = failed.Diagnostics
	writeErr := writeTidyJournal(s, p, j, false)
	r.Status = "partial"
	r.Actions = tidyJournalActions(j)
	r.Diagnostics = j.Diagnostics
	r.Actions = append(r.Actions, ActionOutcome{"remaining-work", "incomplete", "Cleanup stopped; retain remaining targets and inspect the durable journal and predeclared recovery roots. Automatic replay is refused."})
	if writeErr != nil {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{"journal-write-failed", "The latest recovery update failed; inspect the earlier durable prepared journal and exact predeclared roots."})
	}
	return boundedTidy(r)
}
func validTidyJournal(j TidyJournal, p TidyPlanRecord) bool {
	if j.SchemaVersion != 1 || j.PlanID != p.PlanID || j.PlanDigest != p.Digest || !validSnapshot(j.Expected) || j.Expected.Identity.Backend != p.Backend || j.Entries == nil || j.Diagnostics == nil || (j.Phase != "complete" && j.Phase != "prepared" && j.Phase != "incomplete") {
		return false
	}
	targets := eligibleTidyCandidates(p)
	if len(j.Entries) > len(targets) {
		return false
	}
	for i, entry := range j.Entries {
		if !reflect.DeepEqual(entry.Candidate, targets[i]) || entry.Actions == nil || (entry.Phase != "prepared" && entry.Phase != "recovery-confirmed" && entry.Phase != "deleted" && entry.Phase != "incomplete") {
			return false
		}
		want := tidyRecoveryFor(p, targets[i])
		if entry.Recovery.Kind != want.Kind || entry.Recovery.Name != want.Name || entry.Recovery.CommitID != want.CommitID || entry.Recovery.ChangeID != want.ChangeID {
			return false
		}
		for _, action := range entry.Actions {
			if !validText(action.Name) || !validText(action.Status) || !validText(action.Detail) {
				return false
			}
		}
	}
	if j.Phase == "complete" {
		if len(j.Entries) != len(targets) {
			return false
		}
		for _, entry := range j.Entries {
			if entry.Phase != "deleted" || !validConfirmedTidyRecovery(entry.Recovery, tidyRecoveryFor(p, entry.Candidate), p.Backend) {
				return false
			}
		}
	}
	b, err := json.Marshal(j)
	return err == nil && len(b) <= recordLimit
}
func (c tidyCoordinator) retryTidy(ctx context.Context, s *store, p TidyPlanRecord, j TidyJournal, a tidyMutationAdapter, r TidyResult) TidyResult {
	if !validTidyJournal(j, p) {
		return failTidy(r, failure("invalid-state", "Tidy journal is malformed or inconsistent with the stored plan."))
	}
	r.Actions = tidyJournalActions(j)
	if j.Phase != "complete" {
		r.Status = "partial"
		r.Diagnostics = []Diagnostic{{"incomplete-operation", "Prior operation may have native recovery or deletion effects; automatic adoption or replay is refused."}}
		return boundedTidy(r)
	}
	post, err := c.observeApply(ctx, a, p, j.Expected)
	if err != nil {
		return refuseTidy(r, "stale-completed-operation", "Completed-operation native postconditions changed; no effects were repeated.")
	}
	for _, entry := range j.Entries {
		for _, candidate := range post.Candidates {
			if candidate.Name == entry.Candidate.Name {
				return refuseTidy(r, "stale-completed-operation", "A deleted local target exists again; no effects were repeated.")
			}
		}
	}
	r.Status = "ok"
	r.Actions = append(r.Actions, ActionOutcome{"retry", "completed", "Exact completed native postconditions verified; no recovery creation or deletion repeated."})
	return boundedTidy(r)
}

func writeTidyJournal(s *store, p TidyPlanRecord, j TidyJournal, exclusive bool) error {
	count := len(j.Entries) + len(j.Diagnostics)
	for _, entry := range j.Entries {
		count += len(entry.Actions) + len(entry.Candidate.WorkspacePaths) + len(entry.Candidate.Limitations)
	}
	if count > observationLimit {
		return failure("record-limit", "Tidy recovery journal exceeds its aggregate1024-record bound; further native actions are refused.")
	}
	return s.write("tidy-operations", p.PlanID, j, exclusive)
}
