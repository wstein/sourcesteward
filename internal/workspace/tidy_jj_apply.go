// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"github.com/wstein/sourcesteward/internal/inspection"
)

func jjTidyNativeState(s Snapshot) inspection.JJWorkspaceState {
	return inspection.JJWorkspaceState{Source: inspection.JJRepository{WorkspaceRoot: s.Identity.WorkspaceRoot, MetadataDir: s.Identity.WorkspaceRoot + "/.jj", RepositoryDir: s.Identity.RepositoryDir, WorkspaceName: s.Identity.WorkspaceName, CommitID: s.Identity.CommitID, ChangeID: s.Identity.ChangeID, OperationID: s.Identity.OperationID, Observation: inspection.JJObservation{WorkingCopy: "stored-unsnapshotted", Limitation: "Stored working-copy state; disk edits and untracked files were not snapshotted. Freshness is unknown; observations are not transactional and concurrent or ABA changes may be missed."}}, BaseSelector: s.BaseSelector, BaseCommit: s.BaseCommit, BaseChange: s.BaseChange, RegistrationDigest: s.RegistrationDigest, SafetyDigest: s.SafetyDigest, GitDir: s.Identity.GitDir}
}
func jjTidyNativeCandidate(c TidyCandidate) inspection.JJTidyCandidate {
	return inspection.JJTidyCandidate{Name: c.Name, CommitID: c.CommitID, ChangeID: c.ChangeID, Decision: c.Decision, Reason: c.Reason, Proof: c.Proof, WorkspacePaths: c.WorkspacePaths, Limitations: c.Limitations}
}
func jjTidyOutcomeResult(o inspection.JJTidyNativeOutcome) TidyNativeOutcome {
	r := TidyNativeOutcome{Attempted: o.Attempted, Complete: o.Complete, Effects: o.Effects, Limitations: o.Limitations}
	if o.Expected != nil {
		s := jjSnapshot(*o.Expected)
		r.Expected = &s
	}
	return r
}
func (jjTidyAdapter) PrepareRecovery(ctx context.Context, p TidyPlanRecord, c TidyCandidate) (TidyRecovery, Snapshot, TidyNativeOutcome, error) {
	wanted := tidyRecoveryFor(p, c)
	r, s, o, e := inspection.PrepareJJTidyRecovery(ctx, p.SourcePath, jjTidyNativeState(p.Expected), jjTidyNativeCandidate(c), wanted.Name)
	wanted.Name, wanted.CommitID, wanted.ChangeID, wanted.OperationID = r.Name, r.CommitID, r.ChangeID, r.OperationID
	return wanted, jjSnapshot(s), jjTidyOutcomeResult(o), e
}
func (jjTidyAdapter) Delete(ctx context.Context, p TidyPlanRecord, c TidyCandidate, r TidyRecovery, s Snapshot) (TidyNativeOutcome, error) {
	o, e := inspection.DeleteJJTidyBookmark(ctx, p.SourcePath, jjTidyNativeState(s), jjTidyNativeCandidate(c), inspection.JJTidyRecovery{Name: r.Name, CommitID: r.CommitID, ChangeID: r.ChangeID, OperationID: r.OperationID})
	return jjTidyOutcomeResult(o), e
}
