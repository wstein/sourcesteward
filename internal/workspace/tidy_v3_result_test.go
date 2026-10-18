// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func v3TestPlan() V3Plan {
	oid := strings.Repeat("a", 40)
	p := V3Plan{SchemaVersion: 3, PlanID: "v3-plan", Policy: "tidy-git-v3", Mode: "local-refs", SourcePath: "/repo", StateDir: "/state", Expected: Snapshot{Identity: NativeIdentity{Backend: "git", RepositoryDir: "/repo/.git", WorkspaceRoot: "/repo", GitDir: "/repo/.git", HeadRef: "refs/heads/main", CommitID: oid, ObjectFormat: "sha1"}, BaseSelector: "refs/heads/main", BaseCommit: oid, ReferenceDigest: strings.Repeat("b", 64), RegistrationDigest: strings.Repeat("c", 64), SafetyDigest: strings.Repeat("d", 64)}, SelectedRefs: []string{"refs/heads/topic"}, Candidates: []V3Candidate{{ActionID: "action-1", Kind: "branch", Name: "refs/heads/topic", OID: strings.Repeat("f", 40), Decision: "delete", Reason: "native-ancestry", Proof: "native-ancestry", WorkspacePaths: []string{}, Evidence: &V3ProofEvidence{Native: &V3AncestryProof{TargetOID: strings.Repeat("f", 40), BaseOID: oid}}}}, Effects: []string{}, RecoveryLimits: v3RecoveryLimits()}
	p.Expected = v3SnapshotWithState(p.Expected, v3TestNative(p))
	p.Effects = v3PlanEffects(p)
	p.Digest, _ = v3PlanDigest(p)
	return p
}
func TestV3ModelRejectsMutatedAuthority(t *testing.T) {
	p := v3TestPlan()
	if e := validateV3Plan(p, p.StateDir, p.PlanID); e != nil {
		t.Fatal(e)
	}
	for name, mutate := range map[string]func(*V3Plan){
		"version": func(p *V3Plan) { p.SchemaVersion = 2 }, "policy": func(p *V3Plan) { p.Policy = "other" }, "mode": func(p *V3Plan) { p.Mode = "jj" },
		"wrong-source": func(p *V3Plan) { p.SourcePath = "/elsewhere" }, "wrong-base": func(p *V3Plan) { p.Candidates[0].Evidence.Native.BaseOID = strings.Repeat("e", 40) },
		"nil-array": func(p *V3Plan) { p.SelectedRefs = nil }, "forged-effects": func(p *V3Plan) { p.Effects = append(p.Effects, "fetch") },
		"proof-union": func(p *V3Plan) { p.Candidates[0].Proof = "github-merged-head" }, "duplicate": func(p *V3Plan) { p.Candidates = append(p.Candidates, p.Candidates[0]) },
		"selected-base": func(p *V3Plan) { p.Candidates[0].Name = p.Expected.BaseSelector }, "workspace-used": func(p *V3Plan) { p.Candidates[0].WorkspacePaths = []string{"/active"} },
	} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(v3TestPlan())
			var q V3Plan
			_ = json.Unmarshal(raw, &q)
			mutate(&q)
			q.Digest, _ = v3PlanDigest(q)
			if validateV3Plan(q, q.StateDir, q.PlanID) == nil {
				t.Fatal("mutated authority admitted")
			}
		})
	}
}
func TestV3DigestExcludesOnlyDigest(t *testing.T) {
	p := v3TestPlan()
	original, _ := v3PlanDigest(p)
	p.Digest = "different"
	got, _ := v3PlanDigest(p)
	if got != original {
		t.Fatal("digest included itself")
	}
	p.RecoveryLimits = append(p.RecoveryLimits, "new fact")
	got, _ = v3PlanDigest(p)
	if got == original {
		t.Fatal("digest omitted immutable field")
	}
}
func TestV3StrictResultAndLimits(t *testing.T) {
	r := newV3Result("tidy-plan")
	r.Status = "refused"
	r.Boundary.ObservedAt = time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	raw, _ := json.Marshal(r)
	var got V3Result
	if e := decodeV3Result(raw, &got); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(string(raw), `"schema_version":3`, `"schema_version":3,"schema_version":3`, 1), strings.Replace(string(raw), `"schema_version":3`, `"schema_version":3,"unknown":true`, 1), strings.Replace(string(raw), `"actions":[]`, `"actions":null`, 1), strings.Replace(string(raw), `"status":"refused"`, `"status":"maybe"`, 1), string(raw) + "{}", string([]byte{0xff})} {
		if decodeV3Result([]byte(bad), &got) == nil {
			t.Fatalf("invalid result admitted: %.100s", bad)
		}
	}
	p := v3TestPlan()
	r.Plan = &p
	r.Backend = "git"
	if validateV3Result(r) == nil {
		t.Fatal("incomplete evidence exposed executable plan")
	}
	r.Status = "ok"
	r.Boundary.Complete = true
	if e := validateV3Result(r); e != nil {
		t.Fatal(e)
	}
	r.Limitations = []string{strings.Repeat("x", recordLimit)}
	bounded := boundV3Result(r)
	if bounded.Plan != nil || bounded.Boundary.Complete || bounded.Status != "partial" {
		t.Fatal("oversize retained authority")
	}
	b, _ := json.Marshal(bounded)
	if len(b) > recordLimit {
		t.Fatal("oversize envelope")
	}
}

func v3HTTPBudgetFailureResult() V3Result {
	r := newV3Result("tidy-plan")
	r.Backend = "git"
	r.Boundary.NetworkAttempted = true
	r.Boundary.Budget = V3BudgetCounters{NativeProcesses: 7, NativeBytes: 4325377, HTTPRequests: 128, HTTPBytes: (32 << 20) + 1, Observations: 2, UserBytes: 17, MetadataBytes: 19, FramingBytes: 23, StderrBytes: 29}
	r.Diagnostics = []Diagnostic{{Code: "hosting-evidence-limit", Message: "Provider evidence exceeded the shared request, byte or deadline limit."}}
	return r
}

func TestV3FailedHTTPBoundPreservesMeasuredEvidence(t *testing.T) {
	for _, command := range []string{"tidy-plan", "tidy-apply"} {
		for _, status := range []string{"error", "refused", "partial"} {
			t.Run(command+"/"+status, func(t *testing.T) {
				r := v3HTTPBudgetFailureResult()
				r.Command, r.Status = command, status
				bounded := boundV3Result(r)
				if !reflect.DeepEqual(bounded, r) {
					t.Fatalf("failed HTTP bound erased actual counters, typed reason or outcome: %+v", bounded)
				}
				raw, err := json.Marshal(bounded)
				if err != nil {
					t.Fatal(err)
				}
				var got V3Result
				if err := decodeV3Result(raw, &got); err != nil || !reflect.DeepEqual(got, r) {
					t.Fatalf("measured refusal did not roundtrip: %+v %v", got, err)
				}
			})
		}
	}
}

func TestV3HTTPDetectionAllowanceCannotAuthorizeSuccess(t *testing.T) {
	// Exact admitted success remains 32 MiB; its next byte is failed evidence.
	success := v3HTTPBudgetFailureResult()
	success.Status, success.Boundary.Complete = "ok", true
	p := v3TestPlan()
	success.Plan = &p
	success.Diagnostics = []Diagnostic{}
	success.Boundary.Budget.HTTPBytes--
	if err := validateV3Result(success); err != nil {
		t.Fatalf("exact success cap refused: %v", err)
	}
	success.Boundary.Budget.HTTPBytes++
	if validateV3Result(success) == nil {
		t.Fatal("detection byte increased admitted success cap")
	}
	for name, mutate := range map[string]func(*V3Result){
		"two-detection-bytes": func(r *V3Result) { r.Boundary.Budget.HTTPBytes++ },
		"complete":            func(r *V3Result) { r.Boundary.Complete = true },
		"ok":                  func(r *V3Result) { r.Status = "ok" },
		"executable-plan":     func(r *V3Result) { p := v3TestPlan(); r.Plan = &p },
		"no-typed-reason":     func(r *V3Result) { r.Diagnostics = []Diagnostic{} },
		"unrelated-reason":    func(r *V3Result) { r.Diagnostics[0].Code = "hosting-evidence-unavailable" },
		"reason-only-in-text": func(r *V3Result) { r.Diagnostics[0] = Diagnostic{Code: "error", Message: "hosting-evidence-limit"} },
		"no-request":          func(r *V3Result) { r.Boundary.Budget.HTTPRequests = 0 },
		"unsent-request":      func(r *V3Result) { r.Boundary.NetworkAttempted = false },
		"request-overrun":     func(r *V3Result) { r.Boundary.Budget.HTTPRequests++ },
		"negative-counter":    func(r *V3Result) { r.Boundary.Budget.NativeBytes = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			r := v3HTTPBudgetFailureResult()
			mutate(&r)
			if validateV3Result(r) == nil {
				t.Fatal("impossible or authoritative over-budget result admitted")
			}
			raw, _ := json.Marshal(r)
			var got V3Result
			if decodeV3Result(raw, &got) == nil {
				t.Fatal("invalid failed-bound wire result admitted")
			}
		})
	}
}

func TestV3FailedNativeAndContentBoundsPreserveMeasuredEvidence(t *testing.T) {
	// These counters span one operation with three planning or eight applying
	// observations. They are measurements, not single-file admission limits.
	for _, command := range []string{"tidy-plan", "tidy-apply"} {
		n := int64(3)
		if command == "tidy-apply" {
			n = 8
		}
		for channel, set := range map[string]func(*V3BudgetCounters){
			"native":   func(b *V3BudgetCounters) { b.NativeBytes = 4325376 + 1 },
			"user":     func(b *V3BudgetCounters) { b.UserBytes = n*192*1024*1024 + 1 },
			"metadata": func(b *V3BudgetCounters) { b.MetadataBytes = n*(128*1024*1024+128*1024) + 1 },
			"framing":  func(b *V3BudgetCounters) { b.FramingBytes = n*128*1024 + 1 },
			"stderr":   func(b *V3BudgetCounters) { b.StderrBytes = n*64*1024 + 1 },
		} {
			t.Run(command+"/"+channel, func(t *testing.T) {
				r := newV3Result(command)
				r.Backend = "git"
				r.Boundary.Budget.Observations = n
				set(&r.Boundary.Budget)
				r.Diagnostics = []Diagnostic{{Code: "content-limit", Message: "The failed proof read includes its detection byte."}}
				bounded := boundV3Result(r)
				raw, _ := json.Marshal(bounded)
				var got V3Result
				if err := decodeV3Result(raw, &got); err != nil || !reflect.DeepEqual(got, r) {
					t.Fatalf("failed %s evidence was clamped or erased: %+v %v", channel, got, err)
				}
			})
		}
	}
}
func TestV3OptionsModesAndAcknowledgment(t *testing.T) {
	base := V3PlanOptions{Path: "/repo", Base: "refs/heads/main", Refs: []string{}, ProtectWorktrees: []string{}, PRHints: []V3PRHint{}}
	if e := validateV3PlanOptions(base); e != nil {
		t.Fatal(e)
	}
	for name, mutate := range map[string]func(*V3PlanOptions){"online-no-remote": func(o *V3PlanOptions) { o.Online = true }, "tracking-offline": func(o *V3PlanOptions) { o.TrackingRemote = "origin" }, "retire-no-store": func(o *V3PlanOptions) {
		o.CompletedWorktree = "/old"
		o.Owner = "owner"
		o.RemoveRegistration = true
		o.RemoveFiles = "/old"
	}, "duplicate-ref": func(o *V3PlanOptions) { o.Refs = []string{"refs/heads/a", "refs/heads/a"} }, "cross-mode": func(o *V3PlanOptions) { o.Task = "task"; o.CompletedWorktree = "/old" }, "path-alias": func(o *V3PlanOptions) { o.Path = "/repo/../repo" }, "host-override": func(o *V3PlanOptions) { o.Host = "evil.example" }} {
		t.Run(name, func(t *testing.T) {
			o := base
			mutate(&o)
			if validateV3PlanOptions(o) == nil {
				t.Fatal("invalid options admitted")
			}
		})
	}
	p := v3TestPlan()
	if validateV3ApplyOptions(V3ApplyOptions{StateDir: "/state", PlanID: p.PlanID}, p) == nil {
		t.Fatal("unacknowledged apply admitted")
	}
	if e := validateV3ApplyOptions(V3ApplyOptions{StateDir: "/state", PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}}, p); e != nil {
		t.Fatal(e)
	}
}

func v3TestNative(p V3Plan) V3NativeState {
	return V3NativeState{ProtectedRoots: []string{}, References: []V3Reference{{Name: "refs/heads/main", OID: p.Expected.BaseCommit}, {Name: p.Candidates[0].Name, OID: p.Candidates[0].OID}}, Registrations: []V3Registration{{Path: p.SourcePath, HeadOID: p.Expected.Identity.CommitID, HeadRef: p.Expected.Identity.HeadRef}}}
}

func TestV3StrictRequiredFieldsAndNullScalars(t *testing.T) {
	r := newV3Result("tidy-plan")
	r.Status = "refused"
	b, _ := json.Marshal(r)
	for name, bad := range map[string]string{"missing-false": strings.Replace(string(b), `"complete":false,`, "", 1), "null-false": strings.Replace(string(b), `"complete":false`, `"complete":null`, 1), "null-zero": strings.Replace(string(b), `"http_bytes":0`, `"http_bytes":null`, 1), "null-empty-array": strings.Replace(string(b), `"actions":[]`, `"actions":null`, 1)} {
		t.Run(name, func(t *testing.T) {
			var got V3Result
			if decodeV3Result([]byte(bad), &got) == nil {
				t.Fatal("missing field or null scalar admitted")
			}
		})
	}
	r.Limitations = []string{string([]byte{0xff})}
	if validateV3Result(r) == nil {
		t.Fatal("in-memory invalid UTF-8 normalized into valid authority")
	}
}
func TestV3ResultExactEncodedLimit(t *testing.T) {
	r := newV3Result("tidy-plan")
	r.Status = "refused"
	r.Limitations = []string{}
	for {
		b, _ := json.Marshal(r)
		if recordLimit-len(b) <= 4099 {
			r.Limitations = append(r.Limitations, "")
			b, _ = json.Marshal(r)
			remaining := recordLimit - len(b)
			if remaining < 0 {
				t.Fatal("bad boundary fixture")
			}
			r.Limitations[len(r.Limitations)-1] = strings.Repeat("x", remaining)
			break
		}
		r.Limitations = append(r.Limitations, strings.Repeat("x", 4096))
	}
	b, _ := json.Marshal(r)
	if len(b) != recordLimit {
		t.Fatalf("fixture %d", len(b))
	}
	var got V3Result
	if e := decodeV3Result(b, &got); e != nil {
		t.Fatal(e)
	}
	r.Limitations[len(r.Limitations)-1] += "x"
	b, _ = json.Marshal(r)
	if decodeV3Result(b, &got) == nil {
		t.Fatal("one-byte over record admitted")
	}
}

func v3LegacyTestPlan() V3Plan {
	p := v3TestPlan()
	candidate := p.Candidates[0]
	p.Candidates = []V3Candidate{}
	p.SelectedRefs = []string{}
	p.Mode = "legacy-retirement"
	physical := func(path string, inode uint64) V3PhysicalIdentity {
		return V3PhysicalIdentity{Path: path, Device: 1, Inode: inode, UID: uint64(os.Geteuid()), OwnerKnown: true, Kind: "directory", Mode: 0755, Nlink: 2, Size: 1, MtimeNS: 1, CtimeNS: 1, TimestampPrecision: "nanosecond"}
	}
	destination := physical("/old", 2)
	target := p.Expected.Identity
	target.WorkspaceRoot = "/old"
	target.GitDir = "/repo/.git/worktrees/old"
	target.HeadRef = candidate.Name
	target.CommitID = candidate.OID
	entries := []V3ManifestEntry{}
	digest, _ := v3RecordDigest(entries)
	manifest := V3Manifest{Complete: true, Entries: entries, Digest: digest}
	p.Retirement = &V3Retirement{Actions: []V3RetirementAction{{Name: "remove-registration", Target: "/old", Decision: "delete", Reason: "integrated", Blockers: []string{}}, {Name: "remove-files", Target: "/old", Decision: "delete", Reason: "clean", Blockers: []string{}}}, ActionID: "retire-1", RegisteredRoots: []string{"/old", "/repo"}, ProtectedRoots: []string{"/active"}, Provenance: V3Provenance{Kind: "legacy-completion", Legacy: &V3LegacyProvenance{Owner: "owner", Completed: true, SameUserAuthority: true, SelectedPath: "/old", ProtectedRoots: []string{"/active"}, ObservedIdentity: destination}}, Destination: destination, Parent: physical("/", 1), Registration: physical(target.GitDir, 3), Target: target, RemoveRegistration: true, RemoveFiles: "/old", ContentPolicy: "git-v3", Disk: manifest, Metadata: manifest, Siblings: []V3PhysicalIdentity{}, Integration: V3CommitProof{OID: candidate.OID, Decision: candidate.Decision, Reason: candidate.Reason, Proof: candidate.Proof, WorkspacePaths: []string{"/old"}, Evidence: candidate.Evidence}}
	p.Effects = v3PlanEffects(p)
	p.Digest, _ = v3PlanDigest(p)
	return p
}
func TestV3LegacyProvenanceAndPhysicalPolicy(t *testing.T) {
	p := v3LegacyTestPlan()
	if e := validateV3Plan(p, p.StateDir, p.PlanID); e != nil {
		t.Fatal(e)
	}
	for name, mutate := range map[string]func(*V3Plan){"no-completion": func(p *V3Plan) { p.Retirement.Provenance.Legacy.Completed = false }, "no-authority": func(p *V3Plan) { p.Retirement.Provenance.Legacy.SameUserAuthority = false }, "forged-owned": func(p *V3Plan) { p.Retirement.Provenance.Owned = &V3OwnedProvenance{} }, "wrong-path": func(p *V3Plan) { p.Retirement.Provenance.Legacy.SelectedPath = "/elsewhere" }, "changed-protection": func(p *V3Plan) { p.Retirement.ProtectedRoots = []string{} }, "active": func(p *V3Plan) {
		p.Retirement.ProtectedRoots = []string{"/old"}
		p.Retirement.Provenance.Legacy.ProtectedRoots = []string{"/old"}
	}, "unknown-owner": func(p *V3Plan) { p.Retirement.Parent.OwnerKnown = false }, "no-timestamp": func(p *V3Plan) { p.Retirement.Destination.TimestampPrecision = "" }, "v2-content-policy": func(p *V3Plan) { p.Retirement.ContentPolicy = "git-v2" }, "wrong-registration": func(p *V3Plan) { p.Retirement.Registration.Path = "/another" }, "no-manifest": func(p *V3Plan) { p.Retirement.Disk.Complete = false }, "wrong-mode": func(p *V3Plan) { p.Mode = "owned-retirement" }} {
		t.Run(name, func(t *testing.T) {
			b, _ := json.Marshal(v3LegacyTestPlan())
			var n V3Plan
			_ = json.Unmarshal(b, &n)
			mutate(&n)
			n.Effects = v3PlanEffects(n)
			n.Digest, _ = v3PlanDigest(n)
			if validateV3Plan(n, n.StateDir, n.PlanID) == nil {
				t.Fatal("forged legacy authority admitted")
			}
		})
	}
	o := V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: "/old", Owner: "owner", ProtectWorktrees: []string{"/active"}}
	if e := validateV3ApplyOptions(o, p); e != nil {
		t.Fatal(e)
	}
	o.ProtectWorktrees = []string{"/active", "/new-active"}
	if validateV3ApplyOptions(o, p) == nil {
		t.Fatal("new protection ignored")
	}
}
func TestV3GitHubMergeTupleAndTrackingProof(t *testing.T) {
	p := v3TestPlan()
	repo := V3Repository{Host: "github.com", ID: 1, NodeID: "repo-node", Name: "owner/repo"}
	p.Online = &V3OnlineSelection{Repository: repo, Remote: "origin", RemoteURL: "https://github.com/owner/repo.git", ConfigDigest: strings.Repeat("c", 64), PRHints: []V3PRHint{}, ObservationDigests: []string{}, BudgetPolicy: "github-read-v3"}
	c := &p.Candidates[0]
	proof := V3MergeProof{Repository: repo, ID: 10, NodeID: "pr-node", Number: 42, State: "closed", Merged: true, MergedAt: "2026-10-18T00:00:00Z", Head: V3PRSide{Repository: repo, Ref: "topic", OID: c.OID}, Base: V3PRSide{Repository: repo, Ref: "deleted-stack-parent", OID: strings.Repeat("e", 40)}, MergeOID: strings.Repeat("d", 40), ChosenBaseOID: p.Expected.BaseCommit}
	proof.ObservationDigest, _ = v3MergeDigest(proof)
	p.Online.ObservationDigests = []string{proof.ObservationDigest}
	c.Proof = "github-merged-head"
	c.Reason = c.Proof
	c.Evidence = &V3ProofEvidence{GitHub: &proof}
	p.Effects = v3PlanEffects(p)
	p.Digest, _ = v3PlanDigest(p)
	if e := validateV3Plan(p, p.StateDir, p.PlanID); e != nil {
		t.Fatal(e)
	}
	for name, mutate := range map[string]func(*V3Plan){"wrong-head": func(p *V3Plan) { p.Candidates[0].Evidence.GitHub.Head.OID = strings.Repeat("e", 40) }, "fork": func(p *V3Plan) { p.Candidates[0].Evidence.GitHub.Head.Repository.ID = 2 }, "wrong-host": func(p *V3Plan) { p.Online.Repository.Host = "evil.example" }, "draft": func(p *V3Plan) { p.Candidates[0].Evidence.GitHub.Draft = true }, "wrong-chosen-base": func(p *V3Plan) { p.Candidates[0].Evidence.GitHub.ChosenBaseOID = strings.Repeat("e", 40) }, "unknown-hint": func(p *V3Plan) { p.Online.PRHints = []V3PRHint{{OID: strings.Repeat("e", 40), Number: 42}} }, "remote-repo-mismatch": func(p *V3Plan) { p.Online.RemoteURL = "https://github.com/other/repo.git" }} {
		t.Run(name, func(t *testing.T) {
			b, _ := json.Marshal(p)
			var n V3Plan
			_ = json.Unmarshal(b, &n)
			mutate(&n)
			g := n.Candidates[0].Evidence.GitHub
			g.ObservationDigest, _ = v3MergeDigest(*g)
			n.Online.ObservationDigests = []string{g.ObservationDigest}
			n.Effects = v3PlanEffects(n)
			n.Digest, _ = v3PlanDigest(n)
			if validateV3Plan(n, n.StateDir, n.PlanID) == nil {
				t.Fatal("invalid exact-head proof admitted")
			}
		})
	}
	p.Mode = "tracking-refs"
	c = &p.Candidates[0]
	c.Kind = "tracking-ref"
	c.Name = "refs/remotes/origin/gone"
	c.Proof = "github-branch-absent"
	c.Reason = c.Proof
	p.SelectedRefs = []string{c.Name}
	ad := strings.Repeat("a", 64)
	p.Online.ObservationDigests = []string{ad}
	c.Evidence = &V3ProofEvidence{Absence: &V3AbsenceProof{Repository: repo, Remote: "origin", SourceRef: "refs/heads/gone", TrackingRef: c.Name, Refspec: "+refs/heads/*:refs/remotes/origin/*", ConfigDigest: p.Online.ConfigDigest, AdvertisementDigest: ad}}
	p.Effects = v3PlanEffects(p)
	p.Digest, _ = v3PlanDigest(p)
	if e := validateV3Plan(p, p.StateDir, p.PlanID); e != nil {
		t.Fatal(e)
	}
	c.Evidence.Absence.SourceRef = "refs/heads/another"
	p.Digest, _ = v3PlanDigest(p)
	if validateV3Plan(p, p.StateDir, p.PlanID) == nil {
		t.Fatal("forged tracking mapping admitted")
	}
}

func TestV3CompleteNegativeRetirementHasNoNativeEffects(t *testing.T) {
	p := v3LegacyTestPlan()
	r := p.Retirement
	r.ProtectedRoots = append(r.ProtectedRoots, r.Destination.Path)
	r.Provenance.Legacy.ProtectedRoots = r.ProtectedRoots
	r.Integration.Decision = "retain"
	r.Integration.Proof = "none"
	r.Integration.Reason = "integration-not-proven"
	r.Integration.Evidence = nil
	for i := range r.Actions {
		r.Actions[i].Decision = "retain"
		r.Actions[i].Reason = "active-workspace"
		r.Actions[i].Blockers = []string{"active-workspace"}
	}
	p.Effects = v3PlanEffects(p)
	p.Digest, _ = v3PlanDigest(p)
	if e := validateV3Plan(p, p.StateDir, p.PlanID); e != nil {
		t.Fatal(e)
	}
	if len(v3Eligible(p)) != 0 || len(p.Effects) != 1 {
		t.Fatal("negative retirement declares native mutation")
	}
}

func TestV3PhysicalOwnerAndRegistrationCannotBeForged(t *testing.T) {
	for name, mutate := range map[string]func(*V3Plan){"foreign-owner": func(p *V3Plan) {
		p.Retirement.Destination.UID++
		p.Retirement.Parent.UID++
		p.Retirement.Registration.UID++
		p.Retirement.Provenance.Legacy.ObservedIdentity.UID++
	}, "nested-registration": func(p *V3Plan) {
		p.Retirement.Target.GitDir += "/nested"
		p.Retirement.Registration.Path = p.Retirement.Target.GitDir
	}, "blocked-delete": func(p *V3Plan) {
		p.Retirement.Actions[0].Decision = "delete"
		p.Retirement.Actions[0].Blockers = []string{"active-workspace"}
	}} {
		t.Run(name, func(t *testing.T) {
			p := v3LegacyTestPlan()
			mutate(&p)
			p.Effects = v3PlanEffects(p)
			p.Digest, _ = v3PlanDigest(p)
			if validateV3Plan(p, p.StateDir, p.PlanID) == nil {
				t.Fatal("forged physical/action identity admitted")
			}
		})
	}
}

func TestV3ImmutableDigestRejectsChangedEvidence(t *testing.T) {
	p := v3TestPlan()
	p.Expected.SafetyDigest = strings.Repeat("e", 64)
	if validateV3Plan(p, p.StateDir, p.PlanID) == nil {
		t.Fatal("changed evidence admitted without immutable digest")
	}
}

func TestV3DerivedRecoveryAndPointerMetadataBounds(t *testing.T) {
	p := v3TestPlan()
	p.PlanID = "unsafe..id"
	p.Effects = v3PlanEffects(p)
	p.Digest, _ = v3PlanDigest(p)
	if validateV3Plan(p, p.StateDir, p.PlanID) == nil {
		t.Fatal("derived recovery ref contains invalid native syntax")
	}
	p = v3LegacyTestPlan()
	file := V3ManifestEntry{Identity: V3PhysicalIdentity{Path: "/old/.git", Device: 1, Inode: 4, UID: uint64(os.Geteuid()), OwnerKnown: true, Kind: "regular", Mode: 0644, Nlink: 1, Size: (64 << 10) + 1, MtimeNS: 1, CtimeNS: 1, TimestampPrecision: "nanosecond"}, Digest: strings.Repeat("a", 64)}
	p.Retirement.Disk.Entries = []V3ManifestEntry{file}
	p.Retirement.Disk.Digest, _ = v3RecordDigest(p.Retirement.Disk.Entries)
	p.Digest, _ = v3PlanDigest(p)
	if validateV3Plan(p, p.StateDir, p.PlanID) == nil {
		t.Fatal("worktree pointer inherited user-content byte allowance")
	}
}

func TestV3RetirementOriginalWorkspacePaths(t *testing.T) {
	for name, paths := range map[string][]string{"relative": {"relative"}, "alias": {"/x/../old"}, "duplicate": {"/old", "/old"}, "null": nil, "missing-target": {}, "unregistered": {"/old", "/outside"}} {
		t.Run(name, func(t *testing.T) {
			p := v3LegacyTestPlan()
			p.Retirement.Integration.WorkspacePaths = paths
			p.Digest, _ = v3PlanDigest(p)
			if validateV3Plan(p, p.StateDir, p.PlanID) == nil {
				t.Fatal("original malformed retirement workspace paths admitted")
			}
		})
	}
	p := v3LegacyTestPlan()
	p.Retirement.Integration.WorkspacePaths = []string{"/old"}
	p.Digest, _ = v3PlanDigest(p)
	if e := validateV3Plan(p, p.StateDir, p.PlanID); e != nil {
		t.Fatal(e)
	}
}
func TestV3DetachedRetirementUsesCommitProofWithoutInventedReference(t *testing.T) {
	p := v3LegacyTestPlan()
	p.Retirement.Target.HeadRef = ""
	p.Retirement.Integration.WorkspacePaths = []string{"/old"}
	b, _ := json.Marshal(p)
	var wire map[string]any
	if e := json.Unmarshal(b, &wire); e != nil {
		t.Fatal(e)
	}
	integration := wire["retirement"].(map[string]any)["integration"].(map[string]any)
	for _, field := range []string{"action_id", "kind", "name"} {
		delete(integration, field)
	}
	b, _ = json.Marshal(wire)
	var q V3Plan
	if e := json.Unmarshal(b, &q); e != nil {
		t.Fatal(e)
	}
	q.Digest, _ = v3PlanDigest(q)
	if e := validateV3Plan(q, q.StateDir, q.PlanID); e != nil {
		t.Fatalf("detached commit proof requires no local branch: %v", e)
	}
	encoded, _ := json.Marshal(q.Retirement.Integration)
	var proofFields map[string]any
	_ = json.Unmarshal(encoded, &proofFields)
	for _, field := range []string{"action_id", "kind", "name"} {
		if _, exists := proofFields[field]; exists {
			t.Fatalf("commit proof invented reference field %s", field)
		}
	}
}
