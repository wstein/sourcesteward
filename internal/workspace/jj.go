// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"github.com/wstein/sourcesteward/internal/inspection"
	"path/filepath"
)

type jjAdapter struct{}

func (jjAdapter) Observe(ctx context.Context, path, base string) (Snapshot, error) {
	state, err := inspection.ObserveJJWorkspace(ctx, path, base)
	if err != nil {
		return Snapshot{}, err
	}
	return jjSnapshot(state), nil
}

func jjSnapshot(s inspection.JJWorkspaceState) Snapshot {
	return Snapshot{Identity: jjIdentity(s.Source, s.GitDir), BaseSelector: s.BaseSelector, BaseCommit: s.BaseCommit, BaseChange: s.BaseChange, ReferenceDigest: s.Source.OperationID, RegistrationDigest: s.RegistrationDigest, SafetyDigest: s.SafetyDigest}
}
func jjIdentity(j inspection.JJRepository, git string) NativeIdentity {
	return NativeIdentity{Backend: "jj", RepositoryDir: j.RepositoryDir, WorkspaceRoot: j.WorkspaceRoot, WorkspaceName: j.WorkspaceName, GitDir: git, CommitID: j.CommitID, ChangeID: j.ChangeID, OperationID: j.OperationID, ObjectFormat: "sha1"}
}
func jjState(s Snapshot) inspection.JJWorkspaceState {
	j := s.Identity
	return inspection.JJWorkspaceState{Source: inspection.JJRepository{WorkspaceRoot: j.WorkspaceRoot, MetadataDir: filepath.Join(j.WorkspaceRoot, ".jj"), RepositoryDir: j.RepositoryDir, WorkspaceName: j.WorkspaceName, CommitID: j.CommitID, ChangeID: j.ChangeID, OperationID: j.OperationID}, BaseSelector: s.BaseSelector, BaseCommit: s.BaseCommit, BaseChange: s.BaseChange, RegistrationDigest: s.RegistrationDigest, SafetyDigest: s.SafetyDigest, GitDir: j.GitDir}
}
func (jjAdapter) Create(ctx context.Context, p MutationPlan) (NativeWorkspace, error) {
	got, err := inspection.CreateJJWorkspace(ctx, p.SourcePath, jjState(p.Expected), p.TaskID, p.Destination)
	result := NativeWorkspace{Complete: got.Phase == "materialized" && err == nil, SourceUnchanged: got.SourceUnchanged, Phase: got.Phase, Effects: got.Effects, Limitations: got.Limitations}
	if got.Workspace != nil {
		result.Identity = jjIdentity(*got.Workspace, p.Expected.Identity.GitDir)
	}
	return result, err
}
func (jjAdapter) Verify(ctx context.Context, p MutationPlan) (NativeWorkspace, error) {
	j, err := inspection.VerifyJJWorkspace(ctx, p.SourcePath, p.TaskID, p.Destination, p.Expected.Identity.RepositoryDir)
	if err != nil {
		return NativeWorkspace{}, err
	}
	return NativeWorkspace{Identity: jjIdentity(j, p.Expected.Identity.GitDir), Complete: true, SourceUnchanged: true, Phase: "materialized", Effects: []string{}, Limitations: []string{"Native registration was observed without snapshots; task disk edits and untracked files remain unsnapshotted."}}, nil
}
