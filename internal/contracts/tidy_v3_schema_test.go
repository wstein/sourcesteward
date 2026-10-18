// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/workspace"
)

func tidyV3SchemaResult() workspace.V3Result {
	return workspace.V3Result{SchemaVersion: 3, Command: "tidy-plan", Status: "refused", Backend: "unknown", Boundary: workspace.V3Boundary{Freshness: "sampled-nontransactional", ObservedAt: "2026-10-18T00:00:00Z"}, Actions: []workspace.ActionOutcome{}, Diagnostics: []workspace.Diagnostic{{Code: "unsupported", Message: "Native capability unavailable."}}, Limitations: []string{}}
}
func TestTidyV3SchemaClosedEnvelope(t *testing.T) {
	s := schemaAt(t, "v3", "tidy")
	r := tidyV3SchemaResult()
	raw, _ := json.Marshal(r)
	valid(t, s, raw)
	for _, status := range []string{"refused", "partial", "error"} {
		for _, backend := range []string{"git", "jj", "gitbutler", "unknown", "ambiguous"} {
			r.Status = status
			r.Backend = backend
			raw, _ = json.Marshal(r)
			valid(t, s, raw)
		}
	}
	r = tidyV3SchemaResult()
	raw, _ = json.Marshal(r)
	for name, mutate := range map[string]func(map[string]any){"version": func(v map[string]any) { v["schema_version"] = 2 }, "unknown-field": func(v map[string]any) { v["extra"] = true }, "status": func(v map[string]any) { v["status"] = "success" }, "null-array": func(v map[string]any) { v["actions"] = nil }, "missing-field": func(v map[string]any) { delete(v, "plan") }, "open-boundary": func(v map[string]any) { v["boundary"].(map[string]any)["extra"] = true }, "unknown-plan": func(v map[string]any) { v["plan"] = map[string]any{} }, "incomplete-ok": func(v map[string]any) { v["status"] = "ok" }} {
		t.Run(name, func(t *testing.T) {
			v := value(t, raw).(map[string]any)
			mutate(v)
			if s.Validate(v) == nil {
				t.Fatal("invalid v3 result admitted")
			}
		})
	}
}

func TestTidyV3SchemaFailedHTTPDetectionEvidence(t *testing.T) {
	s := schemaAt(t, "v3", "tidy")
	r := tidyV3SchemaResult()
	r.Backend = "git"
	r.Boundary.NetworkAttempted = true
	r.Boundary.Budget = workspace.V3BudgetCounters{NativeProcesses: 7, NativeBytes: 4325377, HTTPRequests: 128, HTTPBytes: (32 << 20) + 1, Observations: 2, UserBytes: 17, MetadataBytes: 19, FramingBytes: 23, StderrBytes: 29}
	r.Diagnostics = []workspace.Diagnostic{{Code: "hosting-evidence-limit", Message: "Provider evidence exceeded the shared request, byte or deadline limit."}}
	for _, command := range []string{"tidy-plan", "tidy-apply"} {
		for _, status := range []string{"error", "refused", "partial"} {
			r.Command, r.Status = command, status
			raw, _ := json.Marshal(r)
			valid(t, s, raw)
		}
	}
	raw, _ := json.Marshal(r)
	for name, mutate := range map[string]func(map[string]any){
		"two-detection-bytes": func(v map[string]any) {
			v["boundary"].(map[string]any)["budget"].(map[string]any)["http_bytes"] = (32 << 20) + 2
		},
		"complete":        func(v map[string]any) { v["boundary"].(map[string]any)["complete"] = true },
		"ok":              func(v map[string]any) { v["status"] = "ok" },
		"no-typed-reason": func(v map[string]any) { v["diagnostics"] = []any{} },
		"unrelated-reason": func(v map[string]any) {
			v["diagnostics"].([]any)[0].(map[string]any)["code"] = "hosting-evidence-unavailable"
		},
		"reason-only-in-text": func(v map[string]any) {
			v["diagnostics"] = []any{map[string]any{"code": "error", "message": "hosting-evidence-limit"}}
		},
		"no-request":     func(v map[string]any) { v["boundary"].(map[string]any)["budget"].(map[string]any)["http_requests"] = 0 },
		"unsent-request": func(v map[string]any) { v["boundary"].(map[string]any)["network_attempted"] = false },
		"request-overrun": func(v map[string]any) {
			v["boundary"].(map[string]any)["budget"].(map[string]any)["http_requests"] = 129
		},
		"negative-counter": func(v map[string]any) { v["boundary"].(map[string]any)["budget"].(map[string]any)["native_bytes"] = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			v := value(t, raw).(map[string]any)
			mutate(v)
			if s.Validate(v) == nil {
				t.Fatal("impossible or authoritative failed-bound result admitted")
			}
		})
	}
	// The extra measurement byte never changes the admitted success maximum.
	success := tidyV3SchemaPlan()
	success.Boundary.NetworkAttempted = true
	success.Boundary.Budget.HTTPRequests = 128
	success.Boundary.Budget.HTTPBytes = 32 << 20
	raw, _ = json.Marshal(success)
	valid(t, s, raw)
	success.Boundary.Budget.HTTPBytes++
	success.Diagnostics = r.Diagnostics
	raw, _ = json.Marshal(success)
	if s.Validate(value(t, raw)) == nil {
		t.Fatal("detection byte increased admitted schema success cap")
	}
}
func TestTidyV3OldSchemaBytesRemainUnchanged(t *testing.T) {
	// T002 frozen source contract; adding v3 must never reinterpret old records.
	for path, want := range map[string]string{"v1/tidy.schema.json": "7b32f169e81b5351c9b542af7b09025a068279de98f3441ec8966d1239ec1161", "v2/tidy.schema.json": "c964a85de0941f41ceef84b6cc57235de984c7084ea1b4bacc8ebd799de19f2d"} {
		b, e := os.ReadFile(filepath.Join(root(t), "schemas", path))
		if e != nil {
			t.Fatal(e)
		}
		if got := digestV3ContractBytes(b); got != want {
			t.Fatalf("old schema changed: %s %s", path, got)
		}
	}
}
func TestTidyV3SchemaAllModesProofAndProvenanceUnions(t *testing.T) {
	s := schemaAt(t, "v3", "tidy")
	fixture := tidyV3SchemaPlan()
	raw, _ := json.Marshal(fixture)
	valid(t, s, raw)
	for name, mutate := range map[string]func(map[string]any){"mode": func(p map[string]any) { p["mode"] = "other" }, "policy": func(p map[string]any) { p["policy"] = "tidy-v2" }, "null-candidates": func(p map[string]any) { p["candidates"] = nil }, "null-selected": func(p map[string]any) { p["selected_refs"] = nil }, "unknown-proof": func(p map[string]any) { p["candidates"].([]any)[0].(map[string]any)["proof"] = "patch-similar" }, "wrong-proof-arm": func(p map[string]any) { p["candidates"].([]any)[0].(map[string]any)["proof"] = "github-merged-head" }, "none-delete": func(p map[string]any) {
		c := p["candidates"].([]any)[0].(map[string]any)
		c["proof"] = "none"
		c["evidence"] = nil
	}, "retirement-with-refs": func(p map[string]any) { p["mode"] = "legacy-retirement" }, "public-private-roots": func(p map[string]any) { p["protected_roots"] = []any{} }} {
		t.Run(name, func(t *testing.T) {
			v := value(t, raw).(map[string]any)
			mutate(v["plan"].(map[string]any))
			if s.Validate(v) == nil {
				t.Fatal("invalid mode/proof authority admitted")
			}
		})
	}
	fixture.Plan.Candidates[0].Decision = "retain"
	fixture.Plan.Candidates[0].Proof = "none"
	fixture.Plan.Candidates[0].Evidence = nil
	raw, _ = json.Marshal(fixture)
	valid(t, s, raw)
}
func tidyV3SchemaPlan() workspace.V3Result {
	r := tidyV3SchemaResult()
	r.Status = "ok"
	r.Backend = "git"
	r.Boundary.Complete = true
	r.Diagnostics = []workspace.Diagnostic{}
	oid := strings.Repeat("a", 40)
	target := strings.Repeat("b", 40)
	hash := strings.Repeat("c", 64)
	r.Plan = &workspace.V3Plan{SchemaVersion: 3, PlanID: "v3-plan", Digest: hash, Policy: "tidy-git-v3", Mode: "local-refs", SourcePath: "/repo", StateDir: "/state", Expected: workspace.Snapshot{Identity: workspace.NativeIdentity{Backend: "git", RepositoryDir: "/repo/.git", WorkspaceRoot: "/repo", GitDir: "/repo/.git", HeadRef: "refs/heads/main", CommitID: oid, ObjectFormat: "sha1"}, BaseSelector: "refs/heads/main", BaseCommit: oid, ReferenceDigest: hash, RegistrationDigest: hash, SafetyDigest: hash}, SelectedRefs: []string{"refs/heads/topic"}, Candidates: []workspace.V3Candidate{{ActionID: "action-1", Kind: "branch", Name: "refs/heads/topic", OID: target, Decision: "delete", Reason: "native-ancestry", Proof: "native-ancestry", WorkspacePaths: []string{}, Evidence: &workspace.V3ProofEvidence{Native: &workspace.V3AncestryProof{TargetOID: target, BaseOID: oid}}}}, Effects: []string{}, RecoveryLimits: []string{}}
	return r
}

func digestV3ContractBytes(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func TestTidyV3SchemaPositiveModesAndExclusiveProvenance(t *testing.T) {
	s := schemaAt(t, "v3", "tidy")
	r := tidyV3SchemaPlan()
	p := r.Plan
	candidate := p.Candidates[0]
	hash := strings.Repeat("c", 64)
	repo := workspace.V3Repository{Host: "github.com", ID: 1, NodeID: "repo-node", Name: "owner/repo"}
	p.Online = &workspace.V3OnlineSelection{Repository: repo, Remote: "origin", RemoteURL: "https://github.com/owner/repo.git", ConfigDigest: hash, PRHints: []workspace.V3PRHint{}, ObservationDigests: []string{hash}, BudgetPolicy: "github-read-v3"}
	merged := workspace.V3MergeProof{Repository: repo, ID: 10, NodeID: "pr-node", Number: 42, State: "closed", Merged: true, MergedAt: "2026-10-18T00:00:00Z", Head: workspace.V3PRSide{Repository: repo, Ref: "topic", OID: candidate.OID}, Base: workspace.V3PRSide{Repository: repo, Ref: "deleted-stack-parent", OID: strings.Repeat("e", 40)}, MergeOID: strings.Repeat("d", 40), ChosenBaseOID: p.Expected.BaseCommit, ObservationDigest: hash}
	p.Candidates[0].Proof = "github-merged-head"
	p.Candidates[0].Evidence = &workspace.V3ProofEvidence{GitHub: &merged}
	raw, _ := json.Marshal(r)
	valid(t, s, raw)
	p.Mode = "tracking-refs"
	p.SelectedRefs = []string{"refs/remotes/origin/gone"}
	p.Candidates[0].Name = p.SelectedRefs[0]
	p.Candidates[0].Kind = "tracking-ref"
	p.Candidates[0].Proof = "github-branch-absent"
	p.Candidates[0].Evidence = &workspace.V3ProofEvidence{Absence: &workspace.V3AbsenceProof{Repository: repo, Remote: "origin", SourceRef: "refs/heads/gone", TrackingRef: p.SelectedRefs[0], Refspec: "+refs/heads/*:refs/remotes/origin/*", ConfigDigest: hash, AdvertisementDigest: hash}}
	raw, _ = json.Marshal(r)
	valid(t, s, raw)
	physical := func(path string, inode uint64) workspace.V3PhysicalIdentity {
		return workspace.V3PhysicalIdentity{Path: path, Device: 1, Inode: inode, UID: 1000, OwnerKnown: true, Kind: "directory", Mode: 0755, Nlink: 2, Size: 1, MtimeNS: 1, CtimeNS: 1, TimestampPrecision: "nanosecond"}
	}
	destination := physical("/old", 2)
	target := p.Expected.Identity
	target.WorkspaceRoot = "/old"
	target.GitDir = "/repo/.git/worktrees/old"
	target.HeadRef = candidate.Name
	target.CommitID = candidate.OID
	manifest := workspace.V3Manifest{Complete: true, Entries: []workspace.V3ManifestEntry{}, Digest: hash}
	p.Mode = "legacy-retirement"
	p.Candidates = []workspace.V3Candidate{}
	p.SelectedRefs = []string{}
	p.Online = nil
	p.Retirement = &workspace.V3Retirement{Actions: []workspace.V3RetirementAction{{Name: "remove-registration", Target: "/old", Decision: "delete", Reason: "integrated", Blockers: []string{}}, {Name: "remove-files", Target: "/old", Decision: "delete", Reason: "clean", Blockers: []string{}}}, ActionID: "retire-1", RegisteredRoots: []string{"/old", "/repo"}, ProtectedRoots: []string{"/active"}, Provenance: workspace.V3Provenance{Kind: "legacy-completion", Legacy: &workspace.V3LegacyProvenance{Owner: "owner", Completed: true, SameUserAuthority: true, SelectedPath: "/old", ProtectedRoots: []string{"/active"}, ObservedIdentity: destination}}, Destination: destination, Parent: physical("/", 1), Registration: physical(target.GitDir, 3), Target: target, RemoveRegistration: true, RemoveFiles: "/old", ContentPolicy: "git-v3", Disk: manifest, Metadata: manifest, Siblings: []workspace.V3PhysicalIdentity{}, Integration: workspace.V3CommitProof{OID: candidate.OID, Decision: candidate.Decision, Reason: candidate.Reason, Proof: candidate.Proof, WorkspacePaths: []string{"/old"}, Evidence: candidate.Evidence}}
	p.Retirement.Integration.WorkspacePaths = []string{"/old"}
	raw, _ = json.Marshal(r)
	valid(t, s, raw)
	// A detached legacy target retains the same commit proof and paths without
	// inventing a local branch or reference action identity.
	p.Retirement.Target.HeadRef = ""
	raw, _ = json.Marshal(r)
	valid(t, s, raw)
	for _, field := range []string{"action_id", "kind", "name"} {
		v := value(t, raw).(map[string]any)
		proof := v["plan"].(map[string]any)["retirement"].(map[string]any)["integration"].(map[string]any)
		if _, exists := proof[field]; exists {
			t.Fatalf("detached commit proof emitted ref field %s", field)
		}
		proof[field] = "invented"
		if s.Validate(v) == nil {
			t.Fatalf("commit proof accepted ref-only field %s", field)
		}
	}
	for _, paths := range [][]string{{"relative"}, {"/x/../old"}, {"/old", "/old"}} {
		v := value(t, raw).(map[string]any)
		v["plan"].(map[string]any)["retirement"].(map[string]any)["integration"].(map[string]any)["workspace_paths"] = paths
		if s.Validate(v) == nil {
			t.Fatalf("schema accepted malformed original retirement paths %q", paths)
		}
	}
	for name, mutate := range map[string]func(map[string]any){"both-provenances": func(v map[string]any) {
		provenance := v["provenance"].(map[string]any)
		provenance["owned"] = map[string]any{}
	}, "missing-completion": func(v map[string]any) {
		v["provenance"].(map[string]any)["legacy"].(map[string]any)["completed"] = false
	}, "wrong-content-policy": func(v map[string]any) { v["content_policy"] = "git-v2" }, "null-disk-entries": func(v map[string]any) { v["disk"].(map[string]any)["entries"] = nil }, "null-protection": func(v map[string]any) { v["protected_roots"] = nil }} {
		t.Run(name, func(t *testing.T) {
			v := value(t, raw).(map[string]any)
			mutate(v["plan"].(map[string]any)["retirement"].(map[string]any))
			if s.Validate(v) == nil {
				t.Fatal("invalid retirement provenance admitted")
			}
		})
	}
	retirement := p.Retirement
	p.Mode = "owned-retirement"
	target.HeadRef = ""
	retirement.Target = target
	creation := workspace.MutationPlan{SchemaVersion: 1, PlanID: "create-1", TaskID: "task-1", Owner: "owner", SourcePath: "/repo", Destination: "/old", StateDir: "/state", Backend: "git", Reason: "requested", Digest: hash, Expected: p.Expected, DestinationParent: workspace.PathIdentity{Path: "/", Device: 1, Inode: 1}, Effects: []string{}, RecoveryLimits: []string{}}
	task := workspace.TaskRecord{SchemaVersion: 1, TaskID: "task-1", Owner: "owner", PlanID: creation.PlanID, Backend: "git", Destination: "/old", RecoveryPointer: "/state/operations/create-1.json", Native: target, DestinationIdentity: workspace.PathIdentity{Path: "/old", Device: 1, Inode: 2}, BaseCommit: p.Expected.BaseCommit, State: "complete", RemainingWork: []string{}, RecoveryLimits: []string{}}
	retirement.Provenance = workspace.V3Provenance{Kind: "owned", Owned: &workspace.V3OwnedProvenance{CreationPlan: creation, Task: task, CreationJournalDigest: hash, LockReason: "stw:task-1"}}
	raw, _ = json.Marshal(r)
	valid(t, s, raw)
	v := value(t, raw).(map[string]any)
	v["plan"].(map[string]any)["mode"] = "legacy-retirement"
	if s.Validate(v) == nil {
		t.Fatal("owned provenance accepted under legacy mode")
	}
}
