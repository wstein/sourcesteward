// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/render"
)

func overviewV4Raw(t *testing.T, mutate func(*tidywizard.RegistrationPrune)) []byte {
	t.Helper()
	p := tidywizard.RegistrationPrune{ID: strings.Repeat("a", 64), Kind: "registration-prune", Repo: "a", Name: "missing", GitDir: "/repo/.git/worktrees/missing", Destination: "/missing", Owner: "codex", OwnerSource: owner.SourceTaskRecord, Scope: owner.ScopeOwn, Task: "task", Verdict: tidywizard.RegistrationPruneSafe, Reason: "registration-prune-ready", Proof: "owned-missing-registration", Effects: []string{"remove-registration"}, EvidenceDigest: strings.Repeat("b", 64), Limitations: []string{"Metadata only."}}
	if mutate != nil {
		mutate(&p)
	}
	in := render.Input{Workspace: "/fixture", Mode: owner.Evidence{Mode: owner.ModeAgent, Caller: "codex", CallerSource: "env"}, Repos: map[string]render.RepoMeta{"a": {Path: "/repo", Backend: "git"}}, Result: tidywizard.Result{Repositories: []tidywizard.RepoResult{{Name: "a", State: "analysed"}}}}
	b, _, e := render.JSONV4(render.BuildV4(in, []tidywizard.RegistrationPrune{p}))
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestTidyOverviewV4SchemaAcceptsTypedProducer(t *testing.T) {
	s := schemaAt(t, "v4", "tidy-overview")
	raw := overviewV4Raw(t, nil)
	valid(t, s, raw)
	valid(t, s, overviewV4Raw(t, func(p *tidywizard.RegistrationPrune) {
		p.Verdict = tidywizard.RegistrationPruneBlocked
		p.Reason = "registration-prune-inventory-blocked"
		p.Owner = ""
		p.OwnerSource = owner.SourceUnknown
		p.Scope = owner.ScopeUnknown
		p.Task = ""
		p.Proof = "none"
		p.Effects = nil
		p.EvidenceDigest = ""
	}))
	if schemaAt(t, "v2", "tidy-overview").Validate(value(t, raw)) == nil {
		t.Fatal("legacy schema accepted overview4")
	}
}
func TestTidyOverviewV4SchemaRequiresEveryRowField(t *testing.T) {
	s := schemaAt(t, "v4", "tidy-overview")
	for _, field := range []string{"id", "kind", "repo", "name", "gitdir", "destination", "owner", "owner_source", "scope", "task", "verdict", "reason", "proof", "effects", "evidence_digest", "limitations"} {
		t.Run(field, func(t *testing.T) {
			v := value(t, overviewV4Raw(t, nil)).(map[string]any)
			delete(v["registration_prunes"].([]any)[0].(map[string]any), field)
			if s.Validate(v) == nil {
				t.Fatalf("missing row field %s accepted", field)
			}
		})
	}
}
func TestTidyOverviewV4SchemaRefusesVersionAndProofConfusion(t *testing.T) {
	s := schemaAt(t, "v4", "tidy-overview")
	row := func(v map[string]any) map[string]any { return v["registration_prunes"].([]any)[0].(map[string]any) }
	cases := map[string]func(map[string]any){
		"old version":         func(v map[string]any) { v["schema_version"] = 1 },
		"old manifest":        func(v map[string]any) { v["plan_id"] = strings.Repeat("a", 12) },
		"raw metadata":        func(v map[string]any) { row(v)["raw_config"] = "secret" },
		"old candidate kind":  func(v map[string]any) { row(v)["kind"] = "stale-worktree" },
		"guessed owner":       func(v map[string]any) { row(v)["owner_source"] = "path-convention" },
		"integration proof":   func(v map[string]any) { row(v)["proof"] = "ancestor" },
		"filesystem deletion": func(v map[string]any) { row(v)["effects"] = []any{"remove-files"} },
		"missing safe effect": func(v map[string]any) { row(v)["effects"] = []any{} },
		"bad digest":          func(v map[string]any) { row(v)["evidence_digest"] = "bad" },
		"unknown verdict":     func(v map[string]any) { row(v)["verdict"] = "decision" },
		"unknown reason":      func(v map[string]any) { row(v)["reason"] = "unmerged" },
		"null rows":           func(v map[string]any) { v["registration_prunes"] = nil },
		"missing rows":        func(v map[string]any) { delete(v, "registration_prunes") },
		"null limitations":    func(v map[string]any) { row(v)["limitations"] = nil },
		"unknown root":        func(v map[string]any) { v["raw_metadata"] = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			v := value(t, overviewV4Raw(t, nil)).(map[string]any)
			mutate(v)
			if s.Validate(v) == nil {
				b, _ := json.Marshal(v)
				t.Fatalf("accepted invalid overview4 %s", b)
			}
		})
	}
}

func TestTidyOverviewV4SchemaAcceptsMixedLegacyProducerAndAdditions(t *testing.T) {
	schema := schemaAt(t, "v4", "tidy-overview")
	for _, cut := range []bool{false, true} {
		in := overviewInput(nil)
		in.PlanID = ""
		in.Result.Truncated = cut
		for _, kind := range []tidywizard.Kind{tidywizard.KindBranch, tidywizard.KindStaleWorktree} {
			row := in.Result.Candidates[0]
			row.ID = string(kind)
			row.Kind = kind
			in.Result.Candidates = append(in.Result.Candidates, row)
		}
		var prune render.DocumentV4
		if err := json.Unmarshal(overviewV4Raw(t, nil), &prune); err != nil {
			t.Fatal(err)
		}
		doc := render.BuildV4(in, prune.RegistrationPrunes)
		raw, _, err := render.JSONV4(doc)
		if err != nil {
			t.Fatal(err)
		}
		valid(t, schema, raw)
		if len(doc.Candidates) != 6 || len(doc.Derived) == 0 || len(doc.Notices) == 0 || doc.Repositories[0].Backend == "" || doc.Truncated != cut {
			t.Fatalf("fixture lost legacy coverage: %+v", doc)
		}
		value := value(t, raw).(map[string]any)
		for _, item := range value["candidates"].([]any) {
			row := item.(map[string]any)
			row["risk"] = "yellow"
			row["risk_reasons"] = []any{"proof-equivalent"}
			row["group"] = "group" + strings.Repeat("a", 64)
			row["pr"] = 42
		}
		value["summary"].(map[string]any)["risk"] = map[string]any{"green": 0, "yellow": 6, "orange": 0, "red": 0, "kept": 0}
		if err := schema.Validate(value); err != nil {
			t.Fatalf("Spec 044 additive compatibility: %v", err)
		}
	}
}

// These are the optional shapes emitted by Spec 044 P3, not new v4 producer fields.
func overviewV4RiskValue(t *testing.T) map[string]any {
	t.Helper()
	in := overviewInput(nil)
	in.PlanID = ""
	raw, _, err := render.JSONV4(render.BuildV4(in, nil))
	if err != nil {
		t.Fatal(err)
	}
	v := value(t, raw).(map[string]any)
	row := v["candidates"].([]any)[0].(map[string]any)
	row["risk"] = "yellow"
	row["risk_reasons"] = []any{"proof-equivalent"}
	row["group"] = "group" + strings.Repeat("a", 64)
	row["pr"] = 42
	v["summary"].(map[string]any)["risk"] = map[string]any{"green": 0, "yellow": 1, "orange": 0, "red": 0, "kept": 0}
	return v
}

func TestTidyOverviewV4SchemaTypesOptionalRiskExtensions(t *testing.T) {
	schema := schemaAt(t, "v4", "tidy-overview")
	first := func(v map[string]any) map[string]any { return v["candidates"].([]any)[0].(map[string]any) }
	summary := func(v map[string]any) map[string]any { return v["summary"].(map[string]any) }
	counts := func(v map[string]any) map[string]any { return summary(v)["risk"].(map[string]any) }
	for _, level := range []string{"green", "yellow", "orange", "red", "kept"} {
		v := overviewV4RiskValue(t)
		first(v)["risk"] = level
		if level == "kept" {
			delete(first(v), "risk_reasons")
			delete(first(v), "group")
			delete(first(v), "pr")
		}
		if err := schema.Validate(v); err != nil {
			t.Fatalf("valid %s: %v", level, err)
		}
	}
	// Each addition is optional independently; old producer documents remain valid.
	for _, field := range []string{"risk", "risk_reasons", "group", "pr"} {
		v := overviewV4RiskValue(t)
		delete(first(v), field)
		if err := schema.Validate(v); err != nil {
			t.Fatalf("optional %s: %v", field, err)
		}
	}
	v := overviewV4RiskValue(t)
	delete(summary(v), "risk")
	if err := schema.Validate(v); err != nil {
		t.Fatalf("optional summary risk: %v", err)
	}
	cases := map[string]func(map[string]any){
		"unknown level":                  func(v map[string]any) { first(v)["risk"] = "purple" },
		"unknown candidate field":        func(v map[string]any) { first(v)["raw_config"] = "secret" },
		"unknown summary field":          func(v map[string]any) { summary(v)["extra"] = true },
		"flat risk count":                func(v map[string]any) { summary(v)["green"] = 1 },
		"unknown derived field":          func(v map[string]any) { v["derived"].([]any)[0].(map[string]any)["extra"] = true },
		"invented derived online field":  func(v map[string]any) { v["derived"].([]any)[0].(map[string]any)["online"] = "off" },
		"invented derived online reason": func(v map[string]any) { v["derived"].([]any)[0].(map[string]any)["online_reason"] = "flag" },
		"risk null":                      func(v map[string]any) { first(v)["risk"] = nil },
		"risk reasons null":              func(v map[string]any) { first(v)["risk_reasons"] = nil },
		"risk reasons empty":             func(v map[string]any) { first(v)["risk_reasons"] = []any{} },
		"risk reason wrong type":         func(v map[string]any) { first(v)["risk_reasons"] = []any{1} },
		"risk reason invalid word":       func(v map[string]any) { first(v)["risk_reasons"] = []any{"Bad_reason"} },
		"risk reasons wrong type":        func(v map[string]any) { first(v)["risk_reasons"] = "proof-equivalent" },
		"group family instead of key":    func(v map[string]any) { first(v)["group"] = "whr" },
		"group short":                    func(v map[string]any) { first(v)["group"] = "group" + strings.Repeat("a", 63) },
		"group long":                     func(v map[string]any) { first(v)["group"] = "group" + strings.Repeat("a", 65) },
		"group invalid alphabet":         func(v map[string]any) { first(v)["group"] = "group" + strings.Repeat("q", 64) },
		"group wrong type":               func(v map[string]any) { first(v)["group"] = 1 },
		"pr zero":                        func(v map[string]any) { first(v)["pr"] = 0 },
		"pr negative":                    func(v map[string]any) { first(v)["pr"] = -1 },
		"pr fraction":                    func(v map[string]any) { first(v)["pr"] = 1.5 },
		"pr string":                      func(v map[string]any) { first(v)["pr"] = "42" },
		"summary risk null":              func(v map[string]any) { summary(v)["risk"] = nil },
		"summary risk string":            func(v map[string]any) { summary(v)["risk"] = "x" },
		"summary risk unknown field":     func(v map[string]any) { counts(v)["purple"] = 1 },
	}
	for _, level := range []string{"green", "yellow", "orange", "red", "kept"} {
		cases["missing count "+level] = func(v map[string]any) { delete(counts(v), level) }
		cases["negative count "+level] = func(v map[string]any) { counts(v)[level] = -1 }
		cases["fraction count "+level] = func(v map[string]any) { counts(v)[level] = 0.5 }
		cases["string count "+level] = func(v map[string]any) { counts(v)[level] = "1" }
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			v := overviewV4RiskValue(t)
			mutate(v)
			if err := schema.Validate(v); err == nil {
				t.Fatal("strict v4 schema admitted " + name)
			}
		})
	}
}
