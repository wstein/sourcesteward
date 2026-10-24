// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/render"
)

// overviewRaw is a real document produced by the renderer, with every shape of row and hostile text.
func overviewRaw(t *testing.T, mutate func(*render.Input)) []byte {
	t.Helper()
	cand := func(repo string, kind tidywizard.Kind, subject string, v tidywizard.Verdict, scope owner.Scope) tidywizard.Candidate {
		c := tidywizard.Candidate{ID: string(kind) + ":" + repo + ":" + subject, Repo: repo, Kind: kind, Verdict: v, Owner: "claude", OwnerSource: owner.SourceTaskRecord, Scope: scope, Proof: tidywizard.ProofNone, Effects: []string{}}
		if kind == tidywizard.KindWorktree {
			c.Path = subject
		} else {
			c.Branch = subject
		}
		return c
	}
	safe := cand("a", tidywizard.KindWorktree, "/tmp/\x1b[2Jwt", tidywizard.VerdictSafe, owner.ScopeOwn)
	safe.Task, safe.Proof, safe.Effects = "t1", tidywizard.ProofPatchID, []string{"remove-registration", "remove-files"}
	jj := cand("a", tidywizard.KindWorktree, "/tmp/jj", tidywizard.VerdictRetain, owner.ScopeOwn)
	jj.Proof, jj.Reason = tidywizard.ProofEmptyWorkingCopy, "jj-retirement-unsupported"
	tr := cand("b", tidywizard.KindTrackingRef, "refs/remotes/origin/x", tidywizard.VerdictSafe, owner.ScopeOther)
	tr.Owner, tr.OwnerSource, tr.Proof, tr.Effects = "codex", owner.SourcePathConvention, tidywizard.ProofBranchAbsent, []string{"delete-tracking-ref:refs/remotes/origin/x"}
	dec := cand("b", tidywizard.KindWorktree, "/tmp/dec", tidywizard.VerdictDecision, owner.ScopeUnknown)
	dec.Reason, dec.Evidence = "locked-stw", strings.Repeat("e", 512)
	in := render.Input{
		Workspace: "/ws",
		Derived:   []derive.Derived{{Name: "remote", Value: "origin", Source: "default"}},
		Repos:     map[string]render.RepoMeta{"a": {Path: "/ws/a", Backend: "jj", Base: "main", Remote: "origin"}, "b": {Path: "/ws/b", Backend: "git", Base: "refs/heads/main"}, "c": {Path: "/ws/c", Backend: "git"}},
		Result: tidywizard.Result{
			Repositories: []tidywizard.RepoResult{{Name: "a", State: "analysed", Notices: []string{"n\x1b"}}, {Name: "b", State: "analysed", Notices: []string{}}, {Name: "c", State: "skipped", Reason: "base-ambiguous", Notices: []string{}}},
			Candidates:   []tidywizard.Candidate{safe, jj, tr, dec},
		},
		Mode:    owner.Evidence{Mode: owner.ModeAgent, StdinTTY: false, StdoutTTY: true, Markers: []string{"CLAUDECODE"}, Mine: true, ByOwner: []string{"codex"}, Caller: "claude", CallerSource: "env"},
		PlanID:  "3f9c2a7e41b8",
		Notices: []string{"run"},
	}
	if mutate != nil {
		mutate(&in)
	}
	b, _, err := render.JSON(render.Build(in))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTidyOverviewSchemaAcceptsTheRenderedDocument(t *testing.T) {
	s := schemaAt(t, "v2", "tidy-overview")
	valid(t, s, overviewRaw(t, nil))
	// An empty plan id (nothing saved), an empty result and a truncated document are valid too.
	valid(t, s, overviewRaw(t, func(in *render.Input) { in.PlanID = "" }))
	valid(t, s, overviewRaw(t, func(in *render.Input) {
		in.Result = tidywizard.Result{Repositories: []tidywizard.RepoResult{}, Candidates: []tidywizard.Candidate{}}
		in.Mode.Markers, in.Mode.ByOwner = nil, nil
		in.Notices = nil
	}))
	valid(t, s, overviewRaw(t, func(in *render.Input) { in.Result.Truncated = true }))
}

func TestTidyOverviewSchemaRefusesViolations(t *testing.T) {
	s := schemaAt(t, "v2", "tidy-overview")
	raw := overviewRaw(t, nil)
	first := func(v map[string]any, key string) map[string]any { return v[key].([]any)[0].(map[string]any) }
	mutations := map[string]func(map[string]any){
		"unknown top-level field": func(v map[string]any) { v["extra"] = 1 },
		"command":                 func(v map[string]any) { v["command"] = "tidy-plan" },
		"schema version":          func(v map[string]any) { v["schema_version"] = 2 },
		"missing workspace":       func(v map[string]any) { delete(v, "workspace") },
		"missing summary":         func(v map[string]any) { delete(v, "summary") },
		"missing truncated":       func(v map[string]any) { delete(v, "truncated") },
		"truncated type":          func(v map[string]any) { v["truncated"] = "yes" },
		"plan id with a path":     func(v map[string]any) { v["plan_id"] = "../x" },
		"plan id upper case":      func(v map[string]any) { v["plan_id"] = "3F9C2A7E41B8" },
		"plan id too short":       func(v map[string]any) { v["plan_id"] = "3f9c" },
		"null candidates":         func(v map[string]any) { v["candidates"] = nil },
		"null notices":            func(v map[string]any) { v["notices"] = nil },
		"notice type":             func(v map[string]any) { v["notices"] = []any{1} },
		"too many candidates": func(v map[string]any) {
			one := first(v, "candidates")
			list := make([]any, 4097)
			for i := range list {
				list[i] = one
			}
			v["candidates"] = list
		},
		"candidate field":          func(v map[string]any) { first(v, "candidates")["extra"] = 1 },
		"candidate missing field":  func(v map[string]any) { delete(first(v, "candidates"), "evidence") },
		"candidate kind":           func(v map[string]any) { first(v, "candidates")["kind"] = "tag" },
		"candidate verdict":        func(v map[string]any) { first(v, "candidates")["verdict"] = "maybe" },
		"candidate scope":          func(v map[string]any) { first(v, "candidates")["scope"] = "mine" },
		"candidate owner source":   func(v map[string]any) { first(v, "candidates")["owner_source"] = "guess" },
		"candidate proof":          func(v map[string]any) { first(v, "candidates")["proof"] = "trust-me" },
		"candidate evidence long":  func(v map[string]any) { first(v, "candidates")["evidence"] = strings.Repeat("e", 513) },
		"candidate effects type":   func(v map[string]any) { first(v, "candidates")["effects"] = "remove" },
		"candidate effects null":   func(v map[string]any) { first(v, "candidates")["effects"] = nil },
		"candidate id type":        func(v map[string]any) { first(v, "candidates")["id"] = 7 },
		"repository state":         func(v map[string]any) { first(v, "repositories")["state"] = "unknown" },
		"repository field":         func(v map[string]any) { first(v, "repositories")["extra"] = 1 },
		"repository missing field": func(v map[string]any) { delete(first(v, "repositories"), "backend") },
		"derived field":            func(v map[string]any) { first(v, "derived")["extra"] = 1 },
		"derived overridden type":  func(v map[string]any) { first(v, "derived")["overridden"] = "no" },
		"summary negative":         func(v map[string]any) { v["summary"].(map[string]any)["safe"] = -1 },
		"summary field":            func(v map[string]any) { v["summary"].(map[string]any)["other"] = 1 },
		"summary type":             func(v map[string]any) { v["summary"].(map[string]any)["retain"] = "2" },
		"mode value":               func(v map[string]any) { v["mode"].(map[string]any)["mode"] = "root" },
		"mode field":               func(v map[string]any) { v["mode"].(map[string]any)["token"] = "x" },
		"mode missing field":       func(v map[string]any) { delete(v["mode"].(map[string]any), "caller") },
		"mode markers null":        func(v map[string]any) { v["mode"].(map[string]any)["markers"] = nil },
		"mode markers many": func(v map[string]any) {
			list := make([]any, 33)
			for i := range list {
				list[i] = "A"
			}
			v["mode"].(map[string]any)["markers"] = list
		},
		"mode marker long":         func(v map[string]any) { v["mode"].(map[string]any)["markers"] = []any{strings.Repeat("A", 129)} },
		"mode by_owner entry type": func(v map[string]any) { v["mode"].(map[string]any)["by_owner"] = []any{7} },
		"mode tty type":            func(v map[string]any) { v["mode"].(map[string]any)["stdin_tty"] = "yes" },
		"mode caller long":         func(v map[string]any) { v["mode"].(map[string]any)["caller"] = strings.Repeat("a", 129) },
		"mode source long":         func(v map[string]any) { v["mode"].(map[string]any)["caller_source"] = strings.Repeat("s", 65) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			v := value(t, raw).(map[string]any)
			mutate(v)
			if s.Validate(v) == nil {
				t.Fatal("the schema admitted a violation")
			}
		})
	}
}

// Every proof the classifier can emit is valid in a candidate row: the schema enum cannot drift
// from the constants (AllProofs is tied to them by a test of its own).
func TestTidyOverviewSchemaAcceptsEveryProofTheClassifierEmits(t *testing.T) {
	s := schemaAt(t, "v2", "tidy-overview")
	for _, proof := range tidywizard.AllProofs {
		raw := overviewRaw(t, func(in *render.Input) { in.Result.Candidates[0].Proof = proof })
		valid(t, s, raw)
	}
	if len(tidywizard.AllProofs) != 7 {
		t.Errorf("%d proofs", len(tidywizard.AllProofs))
	}
	v := value(t, overviewRaw(t, nil)).(map[string]any)
	v["candidates"].([]any)[0].(map[string]any)["proof"] = "trust-me"
	if s.Validate(v) == nil {
		t.Error("an unknown proof was admitted")
	}
}

// A schema never invents a bound the producer does not enforce: whatever render.JSON writes within
// its own 4 MiB cap validates. These sizes are all reachable (STW_OWNER and the agent variable
// prefixes are unbounded, a result can list any number of repositories and notices).
func TestTidyOverviewSchemaAcceptsEverythingTheRendererCanWrite(t *testing.T) {
	s := schemaAt(t, "v2", "tidy-overview")
	cases := map[string]func(*render.Input){
		"10k-character caller": func(in *render.Input) { in.Mode.Caller = strings.Repeat("c", 10000) },
		"long caller source":   func(in *render.Input) { in.Mode.CallerSource = strings.Repeat("s", 5000) },
		"100 markers": func(in *render.Input) {
			in.Mode.Markers = nil
			for i := 0; i < 100; i++ {
				in.Mode.Markers = append(in.Mode.Markers, "CODEX_"+strings.Repeat("M", 300)+string(rune('A'+i%26))+strings.Repeat("x", i))
			}
		},
		"100 by_owner entries": func(in *render.Input) {
			in.Mode.ByOwner = nil
			for i := 0; i < 100; i++ {
				in.Mode.ByOwner = append(in.Mode.ByOwner, strings.Repeat("o", 300)+strings.Repeat("x", i))
			}
		},
		"2000 repositories": func(in *render.Input) {
			in.Result.Repositories = nil
			for i := 0; i < 2000; i++ {
				in.Result.Repositories = append(in.Result.Repositories, tidywizard.RepoResult{Name: "r" + strings.Repeat("x", i%7) + string(rune(0x4e00+i)), State: "analysed", Notices: []string{}})
			}
		},
		"6000 notices": func(in *render.Input) {
			in.Notices = nil
			for i := 0; i < 6000; i++ {
				in.Notices = append(in.Notices, "run notice "+strings.Repeat("n", i%5)+string(rune(0x4e00+i)))
			}
		},
		"200 derived values": func(in *render.Input) {
			in.Derived = nil
			for i := 0; i < 200; i++ {
				in.Derived = append(in.Derived, derive.Derived{Name: "d" + string(rune(0x4e00+i)), Value: "v", Source: "s"})
			}
		},
		"long workspace": func(in *render.Input) { in.Workspace = "/" + strings.Repeat("w", 20000) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			raw := overviewRaw(t, mutate)
			if len(raw) > render.MaxDocumentBytes {
				t.Fatalf("%d bytes", len(raw))
			}
			valid(t, s, raw)
		})
	}
}

// One valid document per value of every closed enum, taken from the constants of the code, so that a
// word the classifier or the renderer can write cannot be missing from the schema.
func TestTidyOverviewSchemaAcceptsEveryEnumValue(t *testing.T) {
	s := schemaAt(t, "v2", "tidy-overview")
	for _, kind := range []tidywizard.Kind{tidywizard.KindWorktree, tidywizard.KindBranch, tidywizard.KindTrackingRef, tidywizard.KindStaleWorktree} {
		valid(t, s, overviewRaw(t, func(in *render.Input) { in.Result.Candidates[0].Kind = kind }))
	}
	for _, v := range []tidywizard.Verdict{tidywizard.VerdictSafe, tidywizard.VerdictRetain, tidywizard.VerdictDecision} {
		valid(t, s, overviewRaw(t, func(in *render.Input) { in.Result.Candidates[0].Verdict = v }))
	}
	for _, src := range []owner.Source{owner.SourceTaskRecord, owner.SourcePathConvention, owner.SourceCommitAuthor, owner.SourceUnknown} {
		valid(t, s, overviewRaw(t, func(in *render.Input) { in.Result.Candidates[0].OwnerSource = src }))
	}
	for _, sc := range []owner.Scope{owner.ScopeOwn, owner.ScopeOther, owner.ScopeUnknown} {
		valid(t, s, overviewRaw(t, func(in *render.Input) { in.Result.Candidates[0].Scope = sc }))
	}
	for _, m := range []owner.Mode{owner.ModeHuman, owner.ModeAgent} {
		valid(t, s, overviewRaw(t, func(in *render.Input) { in.Mode.Mode = m }))
	}
	for _, state := range []string{"analysed", "skipped", "error"} {
		valid(t, s, overviewRaw(t, func(in *render.Input) { in.Result.Repositories[0].State = state }))
	}
	// A classifier row can carry every effect it plans: remove registration, files and the branch.
	valid(t, s, overviewRaw(t, func(in *render.Input) {
		in.Result.Candidates[0].Effects = []string{"remove-registration", "remove-files", "delete-branch:b", "delete-tracking-ref:r"}
	}))
}
