// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/wstein/sourcesteward/internal/release"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseVerifyRecordUsageBeforeReads(t *testing.T) {
	for _, args := range [][]string{{"verify"}, {"verify", "bad\nname"}, {"verify", strings.Repeat("a", 64)}, {"verify", "v1.0.0", "--signer-workflow-file", "a"}, {"verify", "v1.0.0", "--source-ref", "a"}, {"verify", "v1.0.0", "--record-commit", "abc"}, {"verify", "v1.0.0", "--artifacts="}, {"record"}, {"record", "--commit", "abc", "--version", "v1", "--from", "x"}, {"record", "--commit", strings.Repeat("a", 40), "--version", "v1", "--from", "x", "--write", "--check", "x"}} {
		status, out, _ := runRelease(t, args...)
		if status != 2 || out != "" {
			t.Fatalf("%q: %d %s", args, status, out)
		}
	}
}
func TestReleaseVerifyRecordRefusalJSON(t *testing.T) {
	for _, args := range [][]string{{"verify", "v1.0.0", t.TempDir(), "--json"}, {"record", t.TempDir(), "--commit", strings.Repeat("a", 40), "--version", "v1.0.0", "--from", t.TempDir(), "--json"}} {
		status, out, diag := runRelease(t, args...)
		var r map[string]any
		if json.Unmarshal([]byte(out), &r) != nil || status != 1 || r["status"] != "refused" {
			t.Fatalf("%q: %d %s %s", args, status, out, diag)
		}
	}
}
func TestReleaseRecordFailureExit(t *testing.T) {
	if got := exitStatus(io.Discard, releaseRecordFailed{}); got != 3 {
		t.Fatalf("got %d", got)
	}
}

func releaseRecordCLIFileFixture(t *testing.T) (string, string, string) {
	t.Helper()
	config := releaseExample(t, "go-cli")
	d := releaseRepo(t, map[string][]byte{".stw/release.toml": config})
	runGit := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = d
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.org", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.org")
		raw, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("%q %s %v", args, raw, e)
		}
		return strings.TrimSpace(string(raw))
	}
	runGit("add", "-A")
	runGit("commit", "-qm", "source")
	c := runGit("rev-parse", "HEAD")
	raw, e := os.ReadFile(filepath.Join("..", "..", "specs", "026-release-process", "examples", "go-cli.manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	doc, ff := release.ParseReleaseDocument(raw)
	if len(ff) > 0 {
		t.Fatal(ff)
	}
	manifest := doc.(*release.BuildManifest)
	manifest.Commit = c
	manifest.ConfigSHA256 = fmt.Sprintf("%x", sha256.Sum256(config))
	dir := t.TempDir()
	sums := ""
	for _, a := range manifest.Assets {
		data := []byte(a.File.Name)
		a.File.Size = int64(len(data))
		a.File.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
		os.WriteFile(filepath.Join(dir, a.File.Name), data, 0644)
		sums += a.File.SHA256 + "  " + a.File.Name + "\n"
	}
	raw, e = release.EncodeReleaseDocument(manifest)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "build-manifest.json"), raw, 0644)
	sums += fmt.Sprintf("%x  build-manifest.json\n", sha256.Sum256(raw))
	os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums), 0644)
	return d, c, dir
}
func TestReleaseRecordAndVerifyCLIEntryPoints(t *testing.T) {
	d, c, dir := releaseRecordCLIFileFixture(t)
	args := []string{"record", d, "--commit", c, "--version", "v0.2.0", "--from", dir}
	status, out, diag := runRelease(t, args...)
	var doc map[string]any
	if status != 0 || json.Unmarshal([]byte(out), &doc) != nil || doc["kind"] != "release-record" {
		t.Fatalf("%d %s %s", status, out, diag)
	}
	status, out, diag = runRelease(t, append(args, "--write")...)
	if status != 0 || !strings.Contains(out, "wrote docs/releases/v0.2.0.json") {
		t.Fatalf("%d %s %s", status, out, diag)
	}
	status, out, diag = runRelease(t, append(args, "--write")...)
	if status != 0 || !strings.Contains(out, "identical file unchanged") {
		t.Fatalf("%d %s %s", status, out, diag)
	}
	status, out, diag = runRelease(t, append(args, "--check", filepath.Join(d, "docs/releases/v0.2.0.json"))...)
	if status != 0 || !strings.Contains(out, "checked equal") {
		t.Fatalf("%d %s %s", status, out, diag)
	}
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = d
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.org", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.org")
		raw, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("%q %s %v", args, raw, e)
		}
		return strings.TrimSpace(string(raw))
	}
	git("add", "-A")
	git("commit", "-qm", "record")
	end := git("rev-parse", "HEAD")
	git("remote", "add", "origin", "https://github.com/wstein/sourcesteward.git")
	git("update-ref", "refs/remotes/origin/main", end)
	git("tag", "-a", "v0.2.0", "-m", "release")
	verify := []string{"verify", "v0.2.0", d, "--artifacts", dir, "--json"}
	status, out, diag = runRelease(t, verify...)
	if status != 1 || json.Unmarshal([]byte(out), &doc) != nil || doc["verdict"] != "incomplete" {
		t.Fatalf("%d %s %s", status, out, diag)
	}
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		if f.Name() != "SHA256SUMS" && f.Name() != "build-manifest.json" {
			os.WriteFile(filepath.Join(dir, f.Name()), []byte("tampered"), 0644)
			break
		}
	}
	status, out, diag = runRelease(t, verify...)
	if !strings.Contains(diag, "release verify: gates failed") {
		t.Fatalf("wrong command diagnostic: %s", diag)
	}
	if status != 3 || json.Unmarshal([]byte(out), &doc) != nil || doc["verdict"] != "fail" {
		t.Fatalf("%d %s %s", status, out, diag)
	}
}

func TestReleaseRecordExplicitZeroRunIDIsCompared(t *testing.T) {
	d, c, dir := releaseRecordCLIFileFixture(t)
	status, out, diag := runRelease(t, "record", d, "--commit", c, "--version", "v0.2.0", "--from", dir, "--run-id", "0", "--json")
	var model map[string]any
	if status != 3 || json.Unmarshal([]byte(out), &model) != nil || model["error"].(map[string]any)["code"] != "artifact-mismatch" {
		t.Fatalf("%d %s %s", status, out, diag)
	}
}

func TestReleaseRecordExplicitZeroRunIDMayMatchManifest(t *testing.T) {
	d, c, dir := releaseRecordCLIFileFixture(t)
	path := filepath.Join(dir, "build-manifest.json")
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	doc, ff := release.ParseReleaseDocument(raw)
	if len(ff) > 0 {
		t.Fatal(ff)
	}
	manifest := doc.(*release.BuildManifest)
	manifest.Run.RunID = 0
	raw, e = release.EncodeReleaseDocument(manifest)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(path, raw, 0644)
	sums, e := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(string(sums), "\n")
	for i, line := range lines {
		if strings.HasSuffix(line, "  build-manifest.json") {
			lines[i] = fmt.Sprintf("%x  build-manifest.json", sha256.Sum256(raw))
		}
	}
	os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(strings.Join(lines, "\n")), 0644)
	status, out, diag := runRelease(t, "record", d, "--commit", c, "--version", "v0.2.0", "--from", dir, "--run-id", "0", "--json")
	if status != 0 {
		t.Fatalf("%d %s %s", status, out, diag)
	}
}
