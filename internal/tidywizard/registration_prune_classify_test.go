// SPDX-License-Identifier: EUPL-1.2
package tidywizard

import (
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
		{"unknown\x1b-secret", "registration-prune-absence-unproven", RegistrationPruneRetain},
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

func TestSelectRegistrationPrunes(t *testing.T) {
	own := RegistrationPrune{ID: "one", Kind: "registration-prune", Owner: "codex", OwnerSource: owner.SourceTaskRecord, Scope: owner.ScopeOwn, Verdict: RegistrationPruneSafe, Reason: "registration-prune-ready", Proof: "owned-missing-registration", Effects: []string{"remove-registration"}, EvidenceDigest: strings.Repeat("a", 64)}
	other := own
	other.ID = "two"
	other.Owner = "someone"
	other.Scope = owner.ScopeOther
	bads := []RegistrationPrune{}
	for _, change := range []func(*RegistrationPrune){
		func(r *RegistrationPrune) { r.Verdict = RegistrationPruneRetain }, func(r *RegistrationPrune) { r.Scope = owner.ScopeUnknown }, func(r *RegistrationPrune) { r.OwnerSource = owner.SourcePathConvention }, func(r *RegistrationPrune) { r.Proof = "ancestor" }, func(r *RegistrationPrune) { r.Effects = []string{"remove-registration", "delete-branch"} }, func(r *RegistrationPrune) { r.EvidenceDigest = "" }, func(r *RegistrationPrune) { r.Owner = "someone" },
	} {
		r := own
		change(&r)
		bads = append(bads, r)
	}
	rows := append([]RegistrationPrune{other, own}, bads...)
	got := SelectRegistrationPrunes(rows, Config{Agent: true, Caller: "codex"})
	if len(got) != 1 || got[0].ID != "one" {
		t.Fatalf("agent selection=%+v", got)
	}
	got = SelectRegistrationPrunes([]RegistrationPrune{other, own}, Config{Caller: "codex"})
	if len(got) != 2 || got[0].ID != "one" {
		t.Fatalf("human selection=%+v", got)
	}
	if len(SelectRegistrationPrunes([]RegistrationPrune{own}, Config{Agent: true})) != 0 {
		t.Fatal("empty caller gained own selection")
	}
}
