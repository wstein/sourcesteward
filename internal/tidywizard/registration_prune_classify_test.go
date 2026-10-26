// SPDX-License-Identifier: EUPL-1.2
package tidywizard

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func TestRegistrationPruneRowIDDomain(t *testing.T) {
	got := RegistrationPruneID("r", "w<&\u2028", "/r/.git/worktrees/w")
	if got != "c323142ec8fc1a37a6a3de1bde70b862d58889946d86acf2b5461b143e6f0bae" {
		t.Fatalf("row id = %s", got)
	}
	if got == RegistrationPruneID("r", "w<&\u2028", "/r/.git/worktrees/w-other") {
		t.Fatal("gitdir omitted from row identity")
	}
}

func TestRegistrationPruneRefusalReasonMapping(t *testing.T) {
	cases := []struct {
		code, reason string
		verdict      RegistrationPruneVerdict
	}{
		{"prune-ownership-refused", "registration-prune-ownership-unproven", RegistrationPruneRetain},
		{"invalid-ownership", "registration-prune-ownership-unproven", RegistrationPruneRetain},
		{"invalid-plan", "registration-prune-ownership-unproven", RegistrationPruneRetain},
		{"prune-lock-refused", "registration-prune-ownership-unproven", RegistrationPruneRetain},
		{"prune-parent-unavailable", "registration-prune-absence-unproven", RegistrationPruneRetain},
		{"prune-mount-unavailable", "registration-prune-absence-unproven", RegistrationPruneRetain},
		{"prune-destination-present", "registration-prune-absence-unproven", RegistrationPruneRetain},
		{"prune-mount-changed", "registration-prune-changed", RegistrationPruneBlocked},
		{"prune-parent-changed", "registration-prune-changed", RegistrationPruneBlocked},
		{"prune-pointer-refused", "registration-prune-changed", RegistrationPruneBlocked},
		{"prune-metadata-refused", "registration-prune-changed", RegistrationPruneBlocked},
		{"prune-sibling-refused", "registration-prune-inventory-blocked", RegistrationPruneBlocked},
		{"prune-metadata-limit", "registration-prune-recovery-unavailable", RegistrationPruneBlocked},
		{"unsupported-backend", "registration-prune-unsupported", RegistrationPruneUnknown},
		{"git-ownership-refused", "registration-prune-unsupported", RegistrationPruneUnknown},
		{"git-sandbox-unavailable", "registration-prune-unsupported", RegistrationPruneUnknown},
		{"unknown-workspace-path", "registration-prune-inventory-blocked", RegistrationPruneBlocked},
		{"native-evidence-unavailable", "registration-prune-inventory-blocked", RegistrationPruneBlocked},
		{"active-workspace", "registration-prune-inventory-blocked", RegistrationPruneBlocked},
		{"incomplete-native", "registration-prune-inventory-blocked", RegistrationPruneBlocked},
		{"incomplete-evidence", "registration-prune-inventory-blocked", RegistrationPruneBlocked},
		{"git-output-limit", "registration-prune-recovery-unavailable", RegistrationPruneBlocked},
		{"git-observation-limit", "registration-prune-recovery-unavailable", RegistrationPruneBlocked},
		{"planning-environment-unavailable", "registration-prune-unsupported", RegistrationPruneUnknown},
		{"unsupported-workspace", "registration-prune-unsupported", RegistrationPruneUnknown},
		{"jj-task-unavailable", "registration-prune-unsupported", RegistrationPruneUnknown},
		{"git-worktree-config-refused", "registration-prune-unsupported", RegistrationPruneUnknown},
		{"git-attributes-refused", "registration-prune-unsupported", RegistrationPruneUnknown},
		{"git-active-filter-refused", "registration-prune-unsupported", RegistrationPruneUnknown},
		{"invalid-path", "registration-prune-unsupported", RegistrationPruneUnknown},
		{"unknown\x1b-secret", "registration-prune-unsupported", RegistrationPruneUnknown},
		{"", "registration-prune-unsupported", RegistrationPruneUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			o := workspace.RegistrationPruneV4Observation{Status: "refused", Registration: workspace.PruneV4Registration{Name: "w", GitDir: "/r/.git/worktrees/w", Destination: "/missing", Owner: "forged", OwnerSource: "task-record", Task: "task"}, Diagnostic: workspace.Diagnostic{Code: tc.code, Message: "secret raw lock bytes"}}
			row := ClassifyRegistrationPrune("r", o, Config{Caller: "forged", Agent: true})
			if row.Reason != tc.reason || row.Verdict != tc.verdict || row.Proof != "none" || len(row.Effects) != 0 || row.EvidenceDigest != "" || row.OwnerSource != owner.SourceUnknown || row.Scope != owner.ScopeUnknown {
				t.Fatalf("row=%+v", row)
			}
			if strings.Contains(strings.Join(row.Limitations, " "), "secret") {
				t.Fatal("raw diagnostic leaked")
			}
		})
	}
	row := ClassifyRegistrationPrune("r", workspace.RegistrationPruneV4Observation{Status: "ok", EvidenceDigest: strings.Repeat("a", 64)}, Config{})
	if row.Verdict == RegistrationPruneSafe {
		t.Fatal("success status and digest alone forged admission")
	}
}

func selectionPruneRow(name string) RegistrationPrune {
	row := RegistrationPrune{Kind: "registration-prune", Repo: "r", Name: name, GitDir: "/r/.git/worktrees/" + name, Destination: "/gone/" + name, Owner: "codex", OwnerSource: owner.SourceTaskRecord, Scope: owner.ScopeOwn, Task: name, Verdict: RegistrationPruneSafe, Reason: "registration-prune-ready", Proof: "owned-missing-registration", Effects: []string{"remove-registration"}, EvidenceDigest: strings.Repeat("a", 64), Limitations: []string{}}
	row.ID = RegistrationPruneID(row.Repo, row.Name, row.GitDir)
	return row
}

func TestSelectRegistrationPrunes(t *testing.T) {
	cases := []struct {
		name   string
		change func(*RegistrationPrune)
	}{
		{"kind", func(r *RegistrationPrune) { r.Kind = "worktree" }},
		{"verdict", func(r *RegistrationPrune) { r.Verdict = RegistrationPruneRetain }},
		{"reason", func(r *RegistrationPrune) { r.Reason = "registration-prune-absence-unproven" }},
		{"effect-name", func(r *RegistrationPrune) { r.Effects = []string{"remove-files"} }},
		{"effects-empty", func(r *RegistrationPrune) { r.Effects = []string{} }},
		{"effects-extra", func(r *RegistrationPrune) { r.Effects = []string{"remove-registration", "delete-branch"} }},
		{"scope-unknown", func(r *RegistrationPrune) { r.Scope = owner.ScopeUnknown }},
		{"scope-mismatch", func(r *RegistrationPrune) { r.Owner = "someone" }},
		{"owner-empty", func(r *RegistrationPrune) { r.Owner = "" }},
		{"source", func(r *RegistrationPrune) { r.OwnerSource = owner.SourcePathConvention }},
		{"proof", func(r *RegistrationPrune) { r.Proof = "ancestor" }},
		{"digest-empty", func(r *RegistrationPrune) { r.EvidenceDigest = "" }},
		{"digest-short", func(r *RegistrationPrune) { r.EvidenceDigest = strings.Repeat("a", 63) }},
		{"digest-nonhex", func(r *RegistrationPrune) { r.EvidenceDigest = strings.Repeat("z", 64) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			own := selectionPruneRow("valid-control")
			bad := selectionPruneRow(tc.name)
			tc.change(&bad)
			if bad.ID == own.ID {
				t.Fatal("negative row must not be hidden by duplicate handling")
			}
			for _, agent := range []bool{false, true} {
				got := SelectRegistrationPrunes([]RegistrationPrune{own, bad}, Config{Caller: "codex", Agent: agent})
				if len(got) != 1 || got[0].ID != own.ID {
					t.Fatalf("agent=%v selected=%+v", agent, got)
				}
			}
		})
	}
	own := selectionPruneRow("own")
	other := selectionPruneRow("other")
	other.Owner = "someone"
	other.Scope = owner.ScopeOther
	if got := SelectRegistrationPrunes([]RegistrationPrune{other, own}, Config{Caller: "codex", Agent: true}); len(got) != 1 || got[0].ID != own.ID {
		t.Fatalf("agent selection=%+v", got)
	}
	got := SelectRegistrationPrunes([]RegistrationPrune{other, own}, Config{Caller: "codex"})
	ids := []string{own.ID, other.ID}
	sort.Strings(ids)
	if len(got) != 2 || got[0].ID != ids[0] || got[1].ID != ids[1] {
		t.Fatalf("human selection=%+v", got)
	}
	if len(SelectRegistrationPrunes([]RegistrationPrune{own}, Config{Agent: true})) != 0 {
		t.Fatal("empty caller gained own selection")
	}
}

func TestSelectRegistrationPrunesIdentityAndAmbiguity(t *testing.T) {
	own := selectionPruneRow("owned")
	control := selectionPruneRow("independent")
	for _, change := range []func(*RegistrationPrune){
		func(r *RegistrationPrune) { r.ID = RegistrationPruneID("other", r.Name, r.GitDir) },
		func(r *RegistrationPrune) { r.ID = "" },
	} {
		bad := own
		change(&bad)
		if got := SelectRegistrationPrunes([]RegistrationPrune{bad}, Config{Caller: "codex"}); len(got) != 0 {
			t.Fatalf("invalid row identity selected: %+v", got)
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*RegistrationPrune)
	}{
		{"evidence", func(r *RegistrationPrune) { r.EvidenceDigest = strings.Repeat("b", 64) }},
		{"owner", func(r *RegistrationPrune) { r.Owner = "someone"; r.Scope = owner.ScopeOther }},
		{"task", func(r *RegistrationPrune) { r.Task = "different-task" }},
		{"effect", func(r *RegistrationPrune) { r.Effects = []string{"remove-files"} }},
		{"limitations", func(r *RegistrationPrune) { r.Limitations = []string{"different limits"} }},
		{"claimed-id", func(r *RegistrationPrune) { r.Name = "another" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conflict := own
			tc.change(&conflict)
			for _, rows := range [][]RegistrationPrune{{own, conflict, control}, {conflict, control, own}, {own, conflict, own, control}} {
				got := SelectRegistrationPrunes(rows, Config{Caller: "codex"})
				if len(got) != 1 || !reflect.DeepEqual(got[0], control) {
					t.Fatalf("conflicting identity retained: %+v", got)
				}
			}
		})
	}
	got := SelectRegistrationPrunes([]RegistrationPrune{own, own, control, own}, Config{Caller: "codex"})
	if len(got) != 2 {
		t.Fatalf("identical duplicate count=%d", len(got))
	}
	counts := map[string]int{}
	for _, row := range got {
		counts[row.ID]++
	}
	if counts[own.ID] != 1 || counts[control.ID] != 1 {
		t.Fatalf("identical duplicates=%v", counts)
	}
}
