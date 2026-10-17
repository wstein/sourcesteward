// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func releaseExample(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "specs", "026-release-process", "examples", name+".release.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func releaseRepo(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runRelease(t *testing.T, args ...string) (status int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	status = Run(context.Background(), "dev", append([]string{"release"}, args...), &out, &errOut)
	return status, out.String(), errOut.String()
}

func TestReleaseCheckValidContract(t *testing.T) {
	dir := releaseRepo(t, map[string][]byte{".stw/release.toml": releaseExample(t, "go-cli")})
	status, out, errOut := runRelease(t, "check", dir)
	if status != 0 || errOut != "" {
		t.Fatalf("%d %q", status, errOut)
	}
	want := []string{
		"SourceSteward release check: " + dir + " (git)",
		"effects: read-only, no network, no project command",
		"contract: .stw/release.toml (schema 1, scheme semver, 3 targets, 1 build)",
		"findings: none",
	}
	if out != strings.Join(want, "\n")+"\n" {
		t.Fatalf("output:\n%s", out)
	}
}

func TestReleaseCheckFindingsExitThree(t *testing.T) {
	contract := strings.Replace(string(releaseExample(t, "go-cli")), `paths = ["docs/**", "README.md", "llms.txt"]`, `paths = ["docs/**", "README.md", "scripts/**"]`, 1)
	dir := releaseRepo(t, map[string][]byte{".stw/release.toml": []byte(contract), "scripts/release-package.sh": []byte("#!/bin/sh\n")})
	status, out, errOut := runRelease(t, "check", dir)
	if status != 3 || !strings.HasPrefix(errOut, "Error: ") {
		t.Fatalf("%d %q", status, errOut)
	}
	for _, want := range []string{"findings: 1 error\n", "  error  /allowlist/paths/2  allowlist-overlaps-input: pattern \"scripts/**\" matches the build input \"scripts/release-package.sh\""} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestReleaseCheckRefusalsExitOne(t *testing.T) {
	dir := releaseRepo(t, nil)
	status, out, errOut := runRelease(t, "check", dir)
	if status != 1 || !strings.Contains(out, "refused: config-missing: ") || !strings.Contains(errOut, "config-missing") {
		t.Fatalf("%d %q %q", status, out, errOut)
	}
	status, out, _ = runRelease(t, "check", "--product", "nope", dir)
	if status != 1 || !strings.Contains(out, "product-unknown") {
		t.Fatalf("%d %q", status, out)
	}
	if status, _, _ = runRelease(t, "check", filepath.Join(dir, "does-not-exist")); status != 1 {
		t.Fatalf("status %d", status)
	}
}

func TestReleaseCheckJSON(t *testing.T) {
	dir := releaseRepo(t, map[string][]byte{".stw/release.toml": releaseExample(t, "go-cli")})
	status, out, _ := runRelease(t, "check", "--json", dir)
	if status != 0 || strings.Count(out, "\n") != 1 {
		t.Fatalf("%d %q", status, out)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result["command"] != "release-check" || result["status"] != "ok" || result["backend"] != "git" || result["source"] != "worktree" || result["error"] != nil || result["contract"] == nil {
		t.Fatalf("%v", result)
	}
	status, out, _ = runRelease(t, "check", "--json", releaseRepo(t, nil))
	if status != 1 {
		t.Fatalf("refused JSON exit %d", status)
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil || result["status"] != "refused" || result["error"].(map[string]any)["code"] != "config-missing" {
		t.Fatalf("%v %v", result, err)
	}
	broken := releaseRepo(t, map[string][]byte{".stw/release.toml": []byte("a.b = 1\n")})
	if status, out, _ = runRelease(t, "check", "--json", broken); status != 3 || !strings.Contains(out, `"status":"findings"`) {
		t.Fatalf("%d %s", status, out)
	}
}

func TestReleaseCheckCommit(t *testing.T) {
	dir := releaseRepo(t, map[string][]byte{".stw/release.toml": releaseExample(t, "go-cli")})
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e.org", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e.org")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("add", "-A")
	run("commit", "-q", "-m", "c")
	commit := run("rev-parse", "HEAD")
	status, out, _ := runRelease(t, "check", "--commit", commit, dir)
	if status != 0 || !strings.Contains(out, "commit "+commit) {
		t.Fatalf("%d %s", status, out)
	}
	if status, out, _ = runRelease(t, "check", "--commit", strings.Repeat("0", 40), dir); status != 1 || !strings.Contains(out, "commit-unresolved") {
		t.Fatalf("%d %s", status, out)
	}
}

func TestReleaseCheckEmptyProductIsAbsent(t *testing.T) {
	dir := releaseRepo(t, map[string][]byte{".stw/release.toml": releaseExample(t, "go-cli")})
	for _, args := range [][]string{{"--product", ""}, {"--product="}} {
		status, out, _ := runRelease(t, append([]string{"check"}, append(args, dir)...)...)
		if status != 0 || !strings.Contains(out, "findings: none") {
			t.Fatalf("%v: %d %s", args, status, out)
		}
	}
}

func TestReleaseCheckUsageErrorsExitTwoWithEmptyStdout(t *testing.T) {
	long := strings.Repeat("a", 39)
	for _, args := range [][]string{
		{"check", "one", "two"},
		{"check", "--bogus"},
		{"check", "--json=maybe"},
		{"check", ""},
		{"check", "a\x00b"},
		{"check", "\xff"},
		{"check", "--commit", "abc123"},
		{"check", "--commit", strings.Repeat("A", 40)},
		{"check", "--commit", long},
		{"check", "--commit", long + "g"},
		{"check", "--commit", "HEAD"},
		{"check", "--commit", ""},
		{"check", "--strict"},
		{"bogus"},
	} {
		status, out, errOut := runRelease(t, args...)
		if status != 2 || out != "" || !strings.HasPrefix(errOut, "Error: ") {
			t.Errorf("%q: %d %q %q", args, status, out, errOut)
		}
	}
}

func TestReleaseCommandStaysHiddenUntilItIsDocumented(t *testing.T) {
	root, _ := newRoot("dev", &bytes.Buffer{}, &bytes.Buffer{})
	command, _, err := root.Find([]string{"release"})
	if err != nil || command == nil || command.Name() != "release" {
		t.Fatalf("release is not registered: %v", err)
	}
	if !command.Hidden {
		t.Fatal("stw release stays hidden until work item WI9 of Spec 026 documents it")
	}
	var out bytes.Buffer
	if status := Run(context.Background(), "dev", []string{"--help"}, &out, &bytes.Buffer{}); status != 0 || strings.Contains(out.String(), "release") {
		t.Fatalf("root help advertises release: %d", status)
	}
	for _, doc := range CommandReference() {
		if strings.Contains(doc.Path, "release") {
			t.Fatalf("generated reference lists %q", doc.Path)
		}
	}
}

func TestReleaseWithoutSubcommandPrintsHelp(t *testing.T) {
	status, out, _ := runRelease(t)
	if status != 0 || !strings.Contains(out, "check") {
		t.Fatalf("%d %q", status, out)
	}
}

func TestReleaseCheckEscapesHostileRepositoryText(t *testing.T) {
	doc := `{"schema_version":1,"evil\nkey\u001b[31m":1}`
	dir := releaseRepo(t, map[string][]byte{".stw/release.json": []byte(doc)})
	_, out, _ := runRelease(t, "check", dir)
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "\x1b") {
			t.Fatalf("raw control character in %q", line)
		}
	}
	if !strings.Contains(out, `/evil\nkey\x1b[31m`) {
		t.Fatalf("escaped pointer missing:\n%s", out)
	}
}

func TestReleaseCheckHonoursCancellation(t *testing.T) {
	dir := releaseRepo(t, map[string][]byte{".stw/release.toml": releaseExample(t, "go-cli")})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	if status := Run(ctx, "dev", []string{"release", "check", dir}, &out, &errOut); status != 1 {
		t.Fatalf("status %d", status)
	}
}

func TestReleaseFindingsExitStatusAndMessage(t *testing.T) {
	for count, want := range map[int]string{1: "Error: release check: 1 error finding\n", 2: "Error: release check: 2 error findings\n", 100: "Error: release check: 100 error findings\n"} {
		var diagnostic bytes.Buffer
		if status := exitStatus(&diagnostic, releaseFindingsError{count: count}); status != 3 || diagnostic.String() != want {
			t.Errorf("%d: %d %q", count, status, diagnostic.String())
		}
	}
}

func TestReleaseCheckProductAmbiguityExitsThree(t *testing.T) {
	zlib := releaseExample(t, "mirror-zlib")
	for name, files := range map[string]map[string][]byte{
		"two files for the product": {".stw/release.zlib.toml": zlib, ".stw/release.zlib.json": []byte("{}")},
		"default mixed with named":  {".stw/release.toml": releaseExample(t, "go-cli"), ".stw/release.zlib.toml": zlib},
	} {
		status, out, _ := runRelease(t, "check", "--product", "zlib", releaseRepo(t, files))
		if status != 3 || !strings.Contains(out, "config-ambiguous") {
			t.Errorf("%s: %d %s", name, status, out)
		}
	}
}
