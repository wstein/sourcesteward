// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/cli"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func TestRetirementV2Schema(t *testing.T) {
	s := schemaAt(t, "v2", "tidy")
	for _, backend := range []string{"git", "jj"} {
		id := workspace.NativeIdentity{Backend: backend, RepositoryDir: "/repo/.git", WorkspaceRoot: "/repo", GitDir: "/repo/.git", CommitID: strings.Repeat("a", 40), ObjectFormat: "sha1"}
		if backend == "jj" {
			id.RepositoryDir = "/repo/.jj/repo"
			id.WorkspaceName = "default"
			id.ChangeID = strings.Repeat("k", 32)
			id.OperationID = strings.Repeat("b", 128)
		}
		snap := workspace.Snapshot{Identity: id, BaseSelector: "refs/heads/main", BaseCommit: id.CommitID, BaseChange: id.ChangeID, ReferenceDigest: strings.Repeat("c", 64), RegistrationDigest: strings.Repeat("d", 64), SafetyDigest: strings.Repeat("e", 64)}
		if backend == "jj" {
			snap.ReferenceDigest = id.OperationID
		}
		parent := workspace.PathIdentity{Path: "/tasks", Device: 1, Inode: 1}
		dest := workspace.PathIdentity{Path: "/tasks/task", Device: 1, Inode: 2}
		creation := workspace.MutationPlan{SchemaVersion: 1, PlanID: "create-id", TaskID: "task", Owner: "owner", SourcePath: "/repo", Destination: dest.Path, StateDir: "/state", Backend: backend, Reason: "task", Digest: strings.Repeat("f", 64), Expected: snap, DestinationParent: parent, Effects: []string{}, RecoveryLimits: []string{}}
		target := id
		target.WorkspaceRoot = dest.Path
		task := workspace.TaskRecord{SchemaVersion: 1, TaskID: "task", Owner: "owner", PlanID: creation.PlanID, Backend: backend, Destination: dest.Path, RecoveryPointer: "/state/operations/create-id.json", Native: target, DestinationIdentity: dest, BaseCommit: id.CommitID, BaseChange: id.ChangeID, State: "complete", RemainingWork: []string{}, RecoveryLimits: []string{}}
		manifest := workspace.RetirementManifest{Entries: []workspace.RetirementEntry{}, Digest: strings.Repeat("1", 64), Complete: true}
		observation := workspace.RetirementObservation{Expected: snap, Target: target, Destination: dest, DestinationParent: parent, RegistrationParent: parent, Manifest: manifest, Metadata: manifest, Siblings: []workspace.RetirementSibling{}, SiblingDigest: strings.Repeat("2", 64), WorkspacePaths: []string{"/repo", dest.Path}, WorkspacePathsComplete: true, Proof: "native-ancestry", Actions: []workspace.RetirementAction{{Name: "registration", Target: "task", Decision: "delete", Reason: "integrated-clean-owned", Proof: "native-ancestry", Effects: []string{"registration removal"}, Blockers: []string{}}}, Limitations: []string{}}
		plan := &workspace.RetirementPlanRecord{SchemaVersion: 2, PlanID: "retire-id", SourcePath: "/repo", StateDir: "/state", Backend: backend, Digest: strings.Repeat("3", 64), CreationPlan: creation, CreationJournalDigest: strings.Repeat("4", 64), Task: task, Expected: observation, RemoveRegistration: true, Effects: []string{"journal", "recovery pin", "registration removal"}, RecoveryLimits: []string{"manual recovery"}}
		seed := workspace.TidyResult{SchemaVersion: 2, Command: "tidy-plan", Status: "ok", Backend: backend, Retirement: plan, Actions: []workspace.ActionOutcome{}, Diagnostics: []workspace.Diagnostic{}, Limitations: []string{}}
		raw, err := json.Marshal(seed)
		if err != nil {
			t.Fatal(err)
		}
		valid(t, s, raw)
		for _, tc := range []struct {
			name   string
			mutate func(map[string]any)
		}{
			{"version", func(v map[string]any) { v["schema_version"] = 1 }},
			{"missing-retirement", func(v map[string]any) { delete(v, "retirement") }},
			{"legacy-plan", func(v map[string]any) { v["plan"] = map[string]any{} }},
			{"untyped-retirement", func(v map[string]any) { v["retirement"] = map[string]any{} }},
			{"unknown-proof", func(v map[string]any) {
				v["retirement"].(map[string]any)["expected"].(map[string]any)["proof"] = "empty-alone"
			}},
			{"no-registration-authority", func(v map[string]any) { v["retirement"].(map[string]any)["remove_registration"] = false }},
			{"wrong-backend", func(v map[string]any) { v["backend"] = "unknown" }},
			{"malformed-manifest", func(v map[string]any) {
				v["retirement"].(map[string]any)["expected"].(map[string]any)["manifest"].(map[string]any)["entries"] = []any{map[string]any{"path": "file"}}
			}},
		} {
			t.Run(backend+"/"+tc.name, func(t *testing.T) {
				v := value(t, raw).(map[string]any)
				tc.mutate(v)
				if s.Validate(v) == nil {
					t.Fatal("invalid retirement accepted")
				}
			})
		}
		seed.Command = "tidy-apply"
		seed.Status = "partial"
		seed.Actions = []workspace.ActionOutcome{{Name: "remove-registration", Status: "complete", Detail: "files retained"}}
		seed.Limitations = []string{"Recovery journal: /state/tidy-retirement-operations/retire-id.json"}
		raw, _ = json.Marshal(seed)
		valid(t, s, raw)
	}
	early := workspace.TidyResult{SchemaVersion: 2, Command: "tidy-plan", Status: "refused", Backend: "unknown", Actions: []workspace.ActionOutcome{}, Diagnostics: []workspace.Diagnostic{{Code: "ownership-refused", Message: "retirement requires completed task ownership"}}, Limitations: []string{}}
	raw, _ := json.Marshal(early)
	valid(t, s, raw)
}
func TestRealRetirementCLIErrorSchema(t *testing.T) {
	var out, diagnostic bytes.Buffer
	code := cli.Run(context.Background(), "test", []string{"tidy", "plan", "/nonexistent-source-stw-retire-contract", "--base", "refs/heads/main", "--task", "task", "--owner", "owner", "--state-dir", "/nonexistent-state-stw-retire-contract", "--remove-registration", "--json"}, &out, &diagnostic)
	if code != 1 {
		t.Fatalf("exit%d %s %s", code, out.String(), diagnostic.String())
	}
	valid(t, schemaAt(t, "v2", "tidy"), out.Bytes())
}
