// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"strings"
	"testing"
)

func fabricated(findings ...Finding) CheckResult {
	contract := &Contract{SchemaVersion: 1, Version: Version{Scheme: "semver"}, Build: []Build{{}}, Target: []Target{{}, {}}}
	r := CheckResult{Command: "release-check", Status: "ok", Backend: "git", InputPath: "/in", Root: "/repo", Source: "worktree",
		Products: []CheckedProduct{{File: ".stw/release.toml", Contract: contract}}, Findings: findings}
	return finalizeCheck(r)
}

func TestTextRendersTheSpecifiedLayout(t *testing.T) {
	r := fabricated(Finding{Code: "allowlist-overlaps-input", Severity: severityError, Pointer: "/allowlist/paths/2", Message: "pattern x matches y"})
	want := "SourceSteward release check: /repo (git)\n" +
		"effects: read-only, no network, no project command\n" +
		"contract: .stw/release.toml (schema 1, scheme semver, 2 targets, 1 build)\n" +
		"findings: 1 error\n" +
		"  error  /allowlist/paths/2  allowlist-overlaps-input: pattern x matches y\n"
	if got := r.Text(); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTextCountsAndPluralises(t *testing.T) {
	e := func(p string) Finding { return Finding{Code: "c", Severity: severityError, Pointer: p, Message: "m"} }
	i := func(p string) Finding {
		return Finding{Code: "unused-build", Severity: severityInfo, Pointer: p, Message: "m"}
	}
	text := fabricated(e("/a"), e("/b"), i("/c")).Text()
	for _, want := range []string{"findings: 2 errors, 1 info\n", "  error  /a  c: m\n", "  info   /c  unused-build: m\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if text := fabricated(i("/c")).Text(); !strings.Contains(text, "findings: 1 info\n") || strings.Contains(text, "error") {
		t.Errorf("info only:\n%s", text)
	}
	if text := fabricated(e("/a")).Text(); !strings.Contains(text, "findings: 1 error\n") {
		t.Errorf("singular:\n%s", text)
	}
	if text := fabricated().Text(); !strings.Contains(text, "findings: none\n") {
		t.Errorf("none:\n%s", text)
	}
}

func TestTextNamesDocumentsFilesProductsAndCommits(t *testing.T) {
	r := fabricated(Finding{Code: "profile-violation", Severity: severityError, Pointer: "", Message: "line 1: x", File: ".stw/release.toml"})
	if text := r.Text(); !strings.Contains(text, "  error  (document)  profile-violation: line 1: x\n") {
		t.Errorf("document pointer:\n%s", text)
	}
	r = fabricated(Finding{Code: "c", Severity: severityError, Pointer: "/p", Message: "m", File: ".stw/release.other.toml"})
	if text := r.Text(); !strings.Contains(text, "  error  .stw/release.other.toml /p  c: m\n") {
		t.Errorf("a finding about another file names it:\n%s", text)
	}
	r = fabricated()
	r.Products[0].Contract.Project.Product = "zlib"
	r.Products = append(r.Products, CheckedProduct{File: ".stw/release.bad.toml"})
	text := r.Text()
	for _, want := range []string{"contract: .stw/release.toml (product zlib, schema 1,", "contract: .stw/release.bad.toml (not valid)\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	r = fabricated()
	r.Commit = strings.Repeat("a", 40)
	if text := r.Text(); !strings.Contains(text, "(git, commit "+r.Commit+")") {
		t.Errorf("commit not named:\n%s", text)
	}
	r = fabricated()
	r.Status, r.Error = "refused", &CheckError{Code: "config-missing", Message: "nothing here"}
	if text := r.Text(); !strings.HasSuffix(text, "refused: config-missing: nothing here\n") || strings.Contains(text, "findings:") {
		t.Errorf("refusal:\n%s", text)
	}
	r = fabricated()
	r.Root = ""
	if text := r.Text(); !strings.HasPrefix(text, "SourceSteward release check: /in (git)") {
		t.Errorf("input path fallback:\n%s", text)
	}
}
