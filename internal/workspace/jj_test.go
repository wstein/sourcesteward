// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"github.com/wstein/sourcesteward/internal/inspection"
	"testing"
)

func TestJJAdapterRefusesMissingNativeWorkspace(t *testing.T) {
	_, err := (jjAdapter{}).Observe(context.Background(), t.TempDir(), "@")
	if err == nil {
		t.Fatal("accepted non-jj source")
	}
}
func TestJJAdapterMapsNativeIdentities(t *testing.T) {
	state := nativeJJStateFixture()
	got := jjSnapshot(state)
	if got.Identity.Backend != "jj" || got.Identity.ChangeID != state.Source.ChangeID || got.BaseChange != state.BaseChange || got.ReferenceDigest != state.Source.OperationID || got.Identity.GitDir != state.GitDir {
		t.Fatalf("lost native identities: %+v", got)
	}
	restored := jjState(got)
	if restored.Source.WorkspaceName != state.Source.WorkspaceName || restored.Source.MetadataDir != state.Source.MetadataDir || restored.BaseCommit != state.BaseCommit {
		t.Fatalf("lost precondition: %+v", restored)
	}
}

func nativeJJStateFixture() inspection.JJWorkspaceState {
	return inspection.JJWorkspaceState{Source: inspection.JJRepository{WorkspaceRoot: "/source", MetadataDir: "/source/.jj", RepositoryDir: "/source/.jj/repo", WorkspaceName: "default", CommitID: "commit", ChangeID: "change", OperationID: "operation"}, BaseSelector: "base", BaseCommit: "base-commit", BaseChange: "base-change", RegistrationDigest: "registration", SafetyDigest: "safety", GitDir: "/source/.git"}
}
