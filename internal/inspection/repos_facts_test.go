// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"testing"
)

func TestReposFactsDerivationGit(t *testing.T) {
	wt := "/path/to/repo"
	refName := "refs/heads/main"
	oid := "1234567890abcdef1234567890abcdef12345678"
	entry := ReposEntry{
		Index:        0,
		Path:         wt,
		ResolvedPath: &wt,
		Origin:       "explicit",
		Backend:      "git",
		Status:       "ok",
		Inventory: &InventoryResult{
			Status:  "ok",
			Backend: "git",
			Identity: Result{
				Status:  "ok",
				Backend: "git",
				Repository: &Repository{
					GitDir:    wt + "/.git",
					CommonDir: wt + "/.git",
					Worktree:  &wt,
					Head: Head{
						State: "attached",
						Ref:   &refName,
						OID:   &oid,
					},
				},
			},
			Workspaces: []InventoryWorkspace{
				{
					Path:         &wt,
					Manager:      "git",
					Registration: "ok",
					Tracked:      InventoryObservation{Value: "false"},
					Untracked:    InventoryObservation{Value: "false"},
				},
				{
					Name:         "stale-ws",
					Manager:      "git",
					Registration: "missing",
				},
				{
					Name:         "foreign-ws",
					Manager:      "jj",
					Registration: "ok",
				},
				{
					Name:         "unverified-ws",
					Manager:      "git",
					Registration: "unknown",
				},
			},
			References: []InventoryReference{
				{
					Name: "refs/heads/normal",
					AddedTargets: []InventoryTarget{
						{CommitID: oid},
					},
				},
				{
					Name: "refs/heads/conflicted",
					AddedTargets: []InventoryTarget{
						{CommitID: "1111"},
						{CommitID: "2222"},
					},
				},
				{
					Name: "refs/heads/deleted",
					RemovedTargets: []InventoryTarget{
						{CommitID: "3333"},
					},
				},
			},
		},
	}

	facts := deriveFacts(&entry)
	if facts == nil {
		t.Fatal("expected non-nil facts")
	}

	if facts.HeadState != "attached" {
		t.Errorf("expected head_state attached, got %q", facts.HeadState)
	}
	if facts.HeadRef == nil || *facts.HeadRef != refName {
		t.Errorf("expected head_ref %q, got %+v", refName, facts.HeadRef)
	}
	if facts.HeadCommitID == nil || *facts.HeadCommitID != oid {
		t.Errorf("expected head_commit_id %q, got %+v", oid, facts.HeadCommitID)
	}
	if facts.ChangeID != nil {
		t.Errorf("expected nil change_id for Git, got %+v", facts.ChangeID)
	}
	if facts.Dirty.Value != "false" {
		t.Errorf("expected dirty.value false, got %q", facts.Dirty.Value)
	}
	if facts.ConflictedReferences != 2 {
		t.Errorf("expected 2 conflicted references, got %d", facts.ConflictedReferences)
	}
	if facts.Workspaces.Total != 4 {
		t.Errorf("expected 4 total workspaces, got %d", facts.Workspaces.Total)
	}
	if facts.Workspaces.Stale != 1 {
		t.Errorf("expected 1 stale workspace, got %d", facts.Workspaces.Stale)
	}
	if facts.Workspaces.Foreign != 1 {
		t.Errorf("expected 1 foreign workspace, got %d", facts.Workspaces.Foreign)
	}
	if facts.Workspaces.Unverified != 1 {
		t.Errorf("expected 1 unverified workspace, got %d", facts.Workspaces.Unverified)
	}
}

func TestReposFactsDerivationJJ(t *testing.T) {
	wt := "/path/to/jjrepo"
	commitID := "c1c1c1c1c1c1c1c1"
	changeID := "kmpstuvwxyz"
	entry := ReposEntry{
		Index:        0,
		Path:         wt,
		ResolvedPath: &wt,
		Origin:       "explicit",
		Backend:      "jj",
		Status:       "ok",
		Inventory: &InventoryResult{
			Status:  "ok",
			Backend: "jj",
			Identity: Result{
				Status:  "ok",
				Backend: "jj",
				JJRepository: &JJRepository{
					WorkspaceRoot: wt,
					RepositoryDir: wt + "/.jj/repo",
					ChangeID:      changeID,
					CommitID:      commitID,
					Observation: JJObservation{
						Limitation: "disk freshness needs a snapshot",
					},
				},
			},
			Workspaces: []InventoryWorkspace{
				{
					Path:         &wt,
					Manager:      "jj",
					Registration: "ok",
					Limitations:  []string{"disk freshness needs a snapshot"},
				},
			},
		},
	}

	facts := deriveFacts(&entry)
	if facts == nil {
		t.Fatal("expected non-nil facts")
	}

	if facts.HeadState != "jj-workspace" {
		t.Errorf("expected head_state jj-workspace, got %q", facts.HeadState)
	}
	if facts.HeadRef != nil {
		t.Errorf("expected nil head_ref for jj, got %+v", facts.HeadRef)
	}
	if facts.HeadCommitID == nil || *facts.HeadCommitID != commitID {
		t.Errorf("expected head_commit_id %q, got %+v", commitID, facts.HeadCommitID)
	}
	if facts.ChangeID == nil || *facts.ChangeID != changeID {
		t.Errorf("expected change_id %q, got %+v", changeID, facts.ChangeID)
	}
	if facts.Dirty.Value != "unknown" {
		t.Errorf("expected dirty.value unknown for jj, got %q", facts.Dirty.Value)
	}
	if facts.Dirty.Reason != "disk freshness needs a snapshot" {
		t.Errorf("expected reason 'disk freshness needs a snapshot', got %q", facts.Dirty.Reason)
	}
}

func TestReposFactsSubmoduleCheckoutNotIdentifiedIsUnknown(t *testing.T) {
	wt := "/path/to/super/sub"
	entry := ReposEntry{
		Index:        1,
		Path:         "sub",
		ResolvedPath: &wt,
		Origin:       "submodule",
		Backend:      "git",
		Status:       "ok",
		Inventory: &InventoryResult{
			Status:  "ok",
			Backend: "git",
			// The inventory could not identify the checkout (Spec 027 CK-2), so no workspace matches the resolved root
			Workspaces: []InventoryWorkspace{
				{
					Path: func() *string { p := "/path/to/super/.git/modules/sub"; return &p }(),
				},
			},
		},
	}

	facts := deriveFacts(&entry)
	if facts == nil {
		t.Fatal("expected non-nil facts")
	}
	if facts.Dirty.Value != "unknown" {
		t.Errorf("expected dirty.value unknown for submodule, got %q", facts.Dirty.Value)
	}
	if facts.Dirty.Reason == "" {
		t.Errorf("expected non-empty dirty reason for unmatched submodule checkout")
	}
}

func TestReposDeriveSummary(t *testing.T) {
	wt1 := "/path/to/r1"
	wt2 := "/path/to/r2"
	entries := []ReposEntry{
		{
			Index:        0,
			Path:         wt1,
			ResolvedPath: &wt1,
			Origin:       "explicit",
			Backend:      "git",
			Status:       "ok",
			Facts: &ReposFacts{
				Dirty:                ReposDirty{Value: "false"},
				ConflictedReferences: 1,
				Workspaces:           ReposWorkspacesFacts{Total: 2, Stale: 1, Foreign: 0, Unverified: 0},
			},
		},
		{
			Index:        1,
			Path:         wt2,
			ResolvedPath: &wt2,
			Origin:       "explicit",
			Backend:      "jj",
			Status:       "partial",
			Facts: &ReposFacts{
				Dirty:                ReposDirty{Value: "unknown", Reason: "disk freshness needs a snapshot"},
				ConflictedReferences: 0,
				Workspaces:           ReposWorkspacesFacts{Total: 1, Stale: 0, Foreign: 1, Unverified: 0},
			},
		},
		{
			Index:  2,
			Path:   "uninit",
			Origin: "submodule",
			Status: "skipped",
			Discovery: ReposDiscovery{
				Status: "not-applicable",
			},
		},
		{
			Index:   3,
			Path:    "failed",
			Origin:  "explicit",
			Backend: "unknown",
			Status:  "error",
			Discovery: ReposDiscovery{
				Status: "not-applicable",
			},
		},
	}

	summary := deriveSummary(entries)
	if summary.Entries != 4 {
		t.Errorf("expected 4 entries, got %d", summary.Entries)
	}
	if summary.ByStatus.Ok != 1 || summary.ByStatus.Partial != 1 || summary.ByStatus.Skipped != 1 || summary.ByStatus.Error != 1 {
		t.Errorf("unexpected ByStatus: %+v", summary.ByStatus)
	}
	if summary.ByBackend.Git != 1 || summary.ByBackend.JJ != 1 || summary.ByBackend.Other != 2 {
		t.Errorf("unexpected ByBackend: %+v", summary.ByBackend)
	}
	if summary.Dirty.False != 1 || summary.Dirty.Unknown != 1 || summary.Dirty.True != 0 {
		t.Errorf("unexpected Dirty: %+v", summary.Dirty)
	}
	if summary.ConflictedReferences != 1 {
		t.Errorf("expected 1 conflicted reference, got %d", summary.ConflictedReferences)
	}
	if summary.StaleWorkspaces != 1 {
		t.Errorf("expected 1 stale workspace, got %d", summary.StaleWorkspaces)
	}
	if summary.ForeignWorkspaces != 1 {
		t.Errorf("expected 1 foreign workspace, got %d", summary.ForeignWorkspaces)
	}
	if summary.SkippedUninitialized != 1 {
		t.Errorf("expected 1 skipped uninitialized, got %d", summary.SkippedUninitialized)
	}
	if summary.DiscoveryNotApplicable != 1 { // only entry 3 is explicit and not-applicable
		t.Errorf("expected 1 discovery_not_applicable for explicit entries, got %d", summary.DiscoveryNotApplicable)
	}
}

func TestReposWorkspaceKind(t *testing.T) {
	for _, tc := range []struct {
		manager, registration, backend, want string
	}{
		{"git", "missing", "git", "stale"},
		{"jj", "missing", "git", "stale"}, // stale and foreign: stale only, never both
		{"jj", "unknown", "git", "foreign"},
		{"jj", "present", "git", "foreign"},
		{"", "unknown", "git", "unverified"}, // no manager: never foreign
		{"", "present", "git", ""},
		{"git", "unknown", "git", "unverified"},
		{"git", "present", "git", ""},
		{"git", "present", "jj", "foreign"},
	} {
		got := ReposWorkspaceKind(InventoryWorkspace{Manager: tc.manager, Registration: tc.registration}, tc.backend)
		if got != tc.want {
			t.Errorf("manager %q registration %q backend %q: %q, want %q", tc.manager, tc.registration, tc.backend, got, tc.want)
		}
	}
}

// Facts count stale, foreign and unverified workspaces exactly once each.
func TestReposFactsWorkspaceCountsAreExclusive(t *testing.T) {
	entry := &ReposEntry{Backend: "git", Status: "ok", Inventory: &InventoryResult{Workspaces: []InventoryWorkspace{
		{Manager: "git", Registration: "missing"},
		{Manager: "jj", Registration: "missing"},
		{Manager: "jj", Registration: "unknown"},
		{Manager: "", Registration: "unknown"},
		{Manager: "git", Registration: "present"},
	}}}
	w := deriveFacts(entry).Workspaces
	if w.Total != 5 || w.Stale != 2 || w.Foreign != 1 || w.Unverified != 1 {
		t.Fatalf("%+v", w)
	}
}
