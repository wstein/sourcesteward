// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func dispatchFixture() DispatchPlan {
	return DispatchPlan{
		Repository: "owner/project", Host: "github.com", Workflow: "release.yml", Ref: "main",
		Inputs:         DispatchInputs{Mode: "commit", Product: "", Commit: strings.Repeat("a", 40), Tag: "", Version: "v0.1.0"},
		ResolvedCommit: strings.Repeat("a", 40), MainCommit: strings.Repeat("b", 40), ConfigSHA256: strings.Repeat("c", 64), Verdict: "pass",
	}
}

func TestDispatchPlanCanonicalIdentity(t *testing.T) {
	p := dispatchFixture()
	// Literal bytes specify key ordering, all five workflow inputs, indentation,
	// empty selectors and the final newline independently of the production encoder.
	want := `{
  "config_sha256": "` + strings.Repeat("c", 64) + `",
  "host": "github.com",
  "inputs": {
    "commit": "` + strings.Repeat("a", 40) + `",
    "mode": "commit",
    "product": "",
    "tag": "",
    "version": "v0.1.0"
  },
  "main_commit": "` + strings.Repeat("b", 40) + `",
  "ref": "main",
  "repository": "owner/project",
  "resolved_commit": "` + strings.Repeat("a", 40) + `",
  "verdict": "pass",
  "workflow": "release.yml"
}
`
	got, err := p.CanonicalJSON()
	if err != nil || string(got) != want {
		t.Fatalf("canonical bytes = %s, %v; want %s", got, err, want)
	}
	wantID := fmt.Sprintf("%x", sha256.Sum256([]byte(want)))
	for range 3 {
		id, err := p.ID()
		if err != nil || id != wantID {
			t.Fatalf("id = %q, %v; want %q", id, err, wantID)
		}
	}
	if p != dispatchFixture() {
		t.Fatal("encoding mutated caller's plan")
	}
}

func TestDispatchPlanEveryFieldBindsIdentity(t *testing.T) {
	mutations := map[string]func(*DispatchPlan){
		"repository":      func(p *DispatchPlan) { p.Repository = "owner/other" },
		"host":            func(p *DispatchPlan) { p.Host = "github.example.com" },
		"workflow":        func(p *DispatchPlan) { p.Workflow = "build.yaml" },
		"ref":             func(p *DispatchPlan) { p.Ref = "other" },
		"mode":            func(p *DispatchPlan) { p.Inputs.Mode = "tag" },
		"product":         func(p *DispatchPlan) { p.Inputs.Product = "tool" },
		"commit":          func(p *DispatchPlan) { p.Inputs.Commit = strings.Repeat("d", 40) },
		"tag":             func(p *DispatchPlan) { p.Inputs.Tag = "v0.1.0" },
		"version":         func(p *DispatchPlan) { p.Inputs.Version = "v0.2.0" },
		"resolved_commit": func(p *DispatchPlan) { p.ResolvedCommit = strings.Repeat("d", 40) },
		"main_commit":     func(p *DispatchPlan) { p.MainCommit = strings.Repeat("d", 40) },
		"config_sha256":   func(p *DispatchPlan) { p.ConfigSHA256 = strings.Repeat("d", 64) },
		"verdict":         func(p *DispatchPlan) { p.Verdict = "fail" },
	}
	original, err := dispatchFixture().ID()
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			p := dispatchFixture()
			mutate(&p)
			// Changing fixed ref or a selector alone invalidates the plan. Other
			// mutations remain valid and must produce a different usable id.
			id, err := p.ID()
			refused := name == "ref" || name == "mode" || name == "tag"
			if (err != nil) != refused {
				t.Fatalf("mutation refusal = %v, want %v", err, refused)
			}
			if err == nil && id == original {
				t.Fatal("changed field retained original plan id")
			}
			// The serialized identity binds every field, including fixed/refused values.
			raw, err := canonicalValue(p)
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprintf("%x", sha256.Sum256(raw)) == original {
				t.Fatal("field absent from canonical identity")
			}
		})
	}
}

func TestDispatchPlanTagAndEscapes(t *testing.T) {
	p := dispatchFixture()
	p.Inputs = DispatchInputs{Mode: "tag", Product: "tool", Tag: `tool/v1.0.0-"é&<>`, Version: `v1.0.0-"é&<>`}
	got, err := p.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"commit": ""`) || !strings.Contains(string(got), `"tag": "tool/v1.0.0-\"é&<>"`) {
		t.Fatalf("wrong selector or escapes: %s", got)
	}
	original, err := p.ID()
	if err != nil {
		t.Fatal(err)
	}
	p.Inputs.Tag += "-other"
	changed, err := p.ID()
	if err != nil || original == changed {
		t.Fatalf("changed tag did not change identity: %q, %v", changed, err)
	}
	p.Inputs.Mode, p.Inputs.Commit, p.Inputs.Tag = "commit", strings.Repeat("a", 64), ""
	p.ResolvedCommit, p.MainCommit = strings.Repeat("a", 64), strings.Repeat("b", 64)
	if _, err := p.ID(); err != nil {
		t.Fatalf("SHA-256 Git identities refused: %v", err)
	}
	for _, verdict := range []string{"fail", "incomplete"} {
		p.Verdict = verdict
		if _, err := p.ID(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDispatchPlanRejectsMalformedIdentity(t *testing.T) {
	cases := map[string]func(*DispatchPlan){
		"empty repository":          func(p *DispatchPlan) { p.Repository = "" },
		"repository path":           func(p *DispatchPlan) { p.Repository = "owner/repo/dispatches" },
		"repository query":          func(p *DispatchPlan) { p.Repository = "owner/repo?x=1" },
		"empty host":                func(p *DispatchPlan) { p.Host = "" },
		"host URL":                  func(p *DispatchPlan) { p.Host = "https://github.com" },
		"host userinfo":             func(p *DispatchPlan) { p.Host = "user@github.com" },
		"workflow path":             func(p *DispatchPlan) { p.Workflow = ".github/workflows/release.yml" },
		"workflow traversal":        func(p *DispatchPlan) { p.Workflow = "../release.yml" },
		"workflow query":            func(p *DispatchPlan) { p.Workflow = "release.yml?x=1" },
		"workflow suffix":           func(p *DispatchPlan) { p.Workflow = "release.txt" },
		"wrong ref":                 func(p *DispatchPlan) { p.Ref = "refs/heads/main" },
		"unknown mode":              func(p *DispatchPlan) { p.Inputs.Mode = "publish" },
		"missing commit":            func(p *DispatchPlan) { p.Inputs.Commit = "" },
		"abbreviated commit":        func(p *DispatchPlan) { p.Inputs.Commit = "abcdef" },
		"uppercase commit":          func(p *DispatchPlan) { p.Inputs.Commit = strings.Repeat("A", 40) },
		"conflicting selectors":     func(p *DispatchPlan) { p.Inputs.Tag = "v0.1.0" },
		"missing tag":               func(p *DispatchPlan) { p.Inputs.Mode = "tag"; p.Inputs.Commit = "" },
		"tag with commit":           func(p *DispatchPlan) { p.Inputs.Mode = "tag"; p.Inputs.Tag = "v0.1.0" },
		"missing version":           func(p *DispatchPlan) { p.Inputs.Version = "" },
		"control version":           func(p *DispatchPlan) { p.Inputs.Version += "\n" },
		"invalid UTF8":              func(p *DispatchPlan) { p.Inputs.Version = string([]byte{255}) },
		"tag control":               func(p *DispatchPlan) { p.Inputs.Mode = "tag"; p.Inputs.Commit = ""; p.Inputs.Tag = "v1\x00" },
		"product path":              func(p *DispatchPlan) { p.Inputs.Product = "../tool" },
		"product limit":             func(p *DispatchPlan) { p.Inputs.Product = strings.Repeat("a", 33) },
		"missing resolved commit":   func(p *DispatchPlan) { p.ResolvedCommit = "" },
		"missing remote main":       func(p *DispatchPlan) { p.MainCommit = "" },
		"short contract digest":     func(p *DispatchPlan) { p.ConfigSHA256 = strings.Repeat("c", 40) },
		"uppercase contract digest": func(p *DispatchPlan) { p.ConfigSHA256 = strings.Repeat("C", 64) },
		"unknown verdict":           func(p *DispatchPlan) { p.Verdict = "unknown" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := dispatchFixture()
			mutate(&p)
			if raw, err := p.CanonicalJSON(); err == nil || raw != nil {
				t.Fatalf("malformed plan encoded: %s, %v", raw, err)
			}
			if id, err := p.ID(); err == nil || id != "" {
				t.Fatalf("malformed plan has id %q, %v", id, err)
			}
		})
	}
}

func TestDispatchPlanAcceptsContractWorkflowBasename(t *testing.T) {
	for _, name := range []string{".release.yml", ".release.yaml"} {
		t.Run(name, func(t *testing.T) {
			doc, findings := ParseTOML(exampleBytes(t, "go-cli"))
			if len(findings) != 0 {
				t.Fatalf("parse contract: %+v", findings)
			}
			doc["ci"].(map[string]any)["release_workflow"] = ".github/workflows/" + name
			contract, findings, err := ValidateContract(doc, ValidateOptions{})
			if err != nil || contract == nil {
				t.Fatalf("valid workflow contract refused: %+v, %v", findings, err)
			}
			p := dispatchFixture()
			p.Workflow = strings.TrimPrefix(contract.CI.ReleaseWorkflow, ".github/workflows/")
			raw, err := p.CanonicalJSON()
			if err != nil || !strings.Contains(string(raw), `"workflow": "`+name+`"`) {
				t.Fatalf("contract workflow missing from canonical plan: %s, %v", raw, err)
			}
			id, err := p.ID()
			if err != nil || len(id) != 64 {
				t.Fatalf("accepted contract cannot form dispatch identity: %q, %v", id, err)
			}
		})
	}
}
