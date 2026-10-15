// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/wstein/sourcesteward/internal/inspection"
)

type jjRetirementAdapter struct{}

// VerifyPost observes native recovery and registration absence without touching retained files.
func (jjRetirementAdapter) VerifyPost(ctx context.Context, p RetirementPlanRecord, r TidyRecovery, expected Snapshot) error {
	return inspection.VerifyJJRetirementPostState(ctx, p.SourcePath, jjTidyNativeState(expected), jjRetirementNativeTarget(p.Expected.Target), inspection.JJTidyRecovery{Name: r.Name, CommitID: r.CommitID, ChangeID: r.ChangeID, OperationID: r.OperationID})
}

func (jjRetirementAdapter) Observe(ctx context.Context, source, base string, task TaskRecord, creation MutationPlan) (RetirementObservation, error) {
	o := RetirementObservation{Siblings: []RetirementSibling{}, WorkspacePaths: []string{}, Actions: []RetirementAction{}, Limitations: []string{}}
	state, e := inspection.ObserveJJRetirement(ctx, source, base, task.Destination, task.TaskID, creation.Expected.Identity.RepositoryDir)
	if e != nil {
		return o, e
	}
	o.Expected = jjSnapshot(state.Expected)
	o.Target = jjIdentity(state.Target, state.Expected.GitDir)
	o.Proof = state.Proof
	o.WorkspacePaths = state.WorkspacePaths
	o.WorkspacePathsComplete = state.WorkspacePathsComplete
	o.Limitations = state.Limitations
	o.Destination, e = currentIdentity(task.Destination)
	if e != nil {
		return o, e
	}
	if o.Destination != task.DestinationIdentity {
		return o, failure("retirement-path-changed", "Owned jj destination identity changed.")
	}
	o.DestinationParent, e = currentIdentity(filepath.Dir(task.Destination))
	if e != nil {
		return o, e
	}
	stored := []RetirementStoredFile{}
	for _, f := range state.StoredFiles {
		stored = append(stored, RetirementStoredFile{Path: f.Path, Digest: f.Digest, Size: f.Size, Executable: f.Executable})
	}
	o.Manifest, e = observeRetirementManifest(ctx, task.Destination, retirementManifestAdmission{Backend: "jj", NativeMetadataPaths: state.MetadataPaths, StoredFiles: stored})
	if e != nil {
		return o, e
	}
	o.Metadata = RetirementManifest{Entries: []RetirementEntry{}, Complete: true}
	for _, entry := range o.Manifest.Entries {
		if entry.Path == ".jj" || strings.HasPrefix(entry.Path, ".jj/") {
			o.Metadata.Entries = append(o.Metadata.Entries, entry)
		}
	}
	o.Metadata.Digest = retirementManifestDigest(o.Metadata)
	b, _ := json.Marshal(o.Siblings)
	sum := sha256.Sum256(b)
	o.SiblingDigest = hex.EncodeToString(sum[:])
	decision, reason, blockers := "delete", "native integration and complete raw disk equality verified", []string{}
	if o.Proof == "none" {
		decision, reason, blockers = "retain", "native integration is unproven", []string{"Target working-copy commit is neither integrated nor an unchanged single-parent child of an integrated parent."}
	}
	o.Actions = []RetirementAction{{Name: "remove-registration", Target: task.TaskID, Decision: decision, Reason: reason, Proof: o.Proof, Effects: []string{"Forget only this exact native jj workspace registration; retain files and changes."}, Blockers: blockers}, {Name: "remove-files", Target: task.Destination, Decision: decision, Reason: reason, Proof: o.Proof, Effects: []string{"Remove only the separately authorized complete owned physical manifest after registration removal."}, Blockers: append([]string{}, blockers...)}}
	return o, nil
}

func jjRetirementNativeTarget(n NativeIdentity) inspection.JJRepository {
	s := jjTidyNativeState(Snapshot{Identity: n})
	return s.Source
}
func jjRetirementCurrent(ctx context.Context, p RetirementPlanRecord, expected Snapshot) error {
	fresh, e := (jjRetirementAdapter{}).Observe(ctx, p.SourcePath, expected.BaseSelector, p.Task, p.CreationPlan)
	if e != nil {
		return e
	}
	before := p.Expected
	before.Expected = expected
	before.Target.OperationID = expected.Identity.OperationID
	// Caller selection filters Actions; only the independently observed physical/native facts are compared.
	fresh.Actions, before.Actions = nil, nil
	if !reflect.DeepEqual(fresh, before) {
		return failure("retirement-stale", "Native jj or complete physical retirement evidence changed.")
	}
	return nil
}
func (jjRetirementAdapter) PrepareRecovery(ctx context.Context, p RetirementPlanRecord) (TidyRecovery, TidyNativeOutcome, error) {
	r := retirementRecoveryFor(p)
	o := TidyNativeOutcome{Effects: []string{}, Limitations: []string{}}
	if e := jjRetirementCurrent(ctx, p, p.Expected.Expected); e != nil {
		return r, o, e
	}
	native, state, got, e := inspection.PrepareJJRetirementRecovery(ctx, p.SourcePath, jjTidyNativeState(p.Expected.Expected), jjRetirementNativeTarget(p.Expected.Target), r.Name)
	r.Name, r.CommitID, r.ChangeID, r.OperationID = native.Name, native.CommitID, native.ChangeID, native.OperationID
	o = jjTidyOutcomeResult(got)
	if e != nil {
		return r, o, e
	}
	if e = jjRetirementCurrent(ctx, p, jjSnapshot(state)); e != nil {
		o.Complete = false
		o.Expected = nil
		return r, o, e
	}
	return r, o, nil
}
func (jjRetirementAdapter) RemoveRegistration(ctx context.Context, p RetirementPlanRecord, r TidyRecovery, expected Snapshot) (TidyNativeOutcome, error) {
	o := TidyNativeOutcome{Effects: []string{}, Limitations: []string{}}
	if e := jjRetirementCurrent(ctx, p, expected); e != nil {
		return o, e
	}
	got, e := inspection.ForgetJJRetirement(ctx, p.SourcePath, jjTidyNativeState(expected), jjRetirementNativeTarget(p.Expected.Target), inspection.JJTidyRecovery{Name: r.Name, CommitID: r.CommitID, ChangeID: r.ChangeID, OperationID: r.OperationID})
	o = jjTidyOutcomeResult(got)
	if e != nil {
		return o, e
	}
	if e = verifyRecordedRetirementManifest(ctx, p.Expected.Destination, p.Expected.Manifest); e != nil {
		o.Complete = false
		o.Expected = nil
		return o, e
	}
	return o, nil
}
func (jjRetirementAdapter) RemoveFiles(ctx context.Context, p RetirementPlanRecord, r TidyRecovery, expected Snapshot) (TidyNativeOutcome, error) {
	o := TidyNativeOutcome{Effects: []string{}, Limitations: []string{}}
	if p.RemoveFiles != p.Task.Destination {
		return o, failure("retirement-authorization", "Exact owned filesystem removal selection is required.")
	}
	nativeRecovery := inspection.JJTidyRecovery{Name: r.Name, CommitID: r.CommitID, ChangeID: r.ChangeID, OperationID: r.OperationID}
	if e := inspection.VerifyJJRetirementPostState(ctx, p.SourcePath, jjTidyNativeState(expected), jjRetirementNativeTarget(p.Expected.Target), nativeRecovery); e != nil {
		return o, e
	}
	o, e := removeRecordedManifest(ctx, p.Expected.Destination, p.Expected.DestinationParent, p.Expected.Manifest)
	if e != nil {
		return o, e
	}
	if e = inspection.VerifyJJRetirementPostState(ctx, p.SourcePath, jjTidyNativeState(expected), jjRetirementNativeTarget(p.Expected.Target), nativeRecovery); e != nil {
		o.Complete = false
		return o, e
	}
	s := expected
	o.Expected = &s
	return o, nil
}
