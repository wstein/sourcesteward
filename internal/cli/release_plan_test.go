// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleasePlanUsageBeforeReads(t *testing.T) {
	for _, args := range [][]string{{"plan"}, {"plan", "abc123", "--version", "v1.0.0"}, {"plan", strings.Repeat("a", 40)}, {"plan", "v1.0.0", "--version", "v1.0.0"}, {"plan", "v1.0.0", "--for", "invalid"}, {"plan", strings.Repeat("a", 40), "--version", "v1.0.0", "--for", "publish"}, {"plan", "bad\nname"}} {
		status, out, _ := runRelease(t, args...)
		if status != 2 || out != "" {
			t.Fatalf("%q: %d %s", args, status, out)
		}
	}
}

func TestReleasePlanGateExitStatuses(t *testing.T) {
	for _, tc := range []struct {
		example string
		want    int
	}{{"go-cli", 1}, {"skill-pack", 3}} {
		t.Run(tc.example, func(t *testing.T) {
			d := releaseRepo(t, map[string][]byte{".stw/release.toml": releaseExample(t, tc.example)})
			run := func(args ...string) string {
				cmd := exec.Command("git", args...)
				cmd.Dir = d
				cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e.org", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e.org")
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%v %s", err, out)
				}
				return strings.TrimSpace(string(out))
			}
			run("add", "-A")
			run("commit", "-qm", "fixture")
			commit := run("rev-parse", "HEAD")
			run("remote", "add", "origin", "https://github.com/example/test.git")
			run("update-ref", "refs/remotes/origin/main", commit)
			status, out, diagnostic := runRelease(t, "plan", commit, d, "--version", "v1.2.3", "--product=", "--json")
			if status != tc.want {
				t.Fatalf("%d %s %s", status, out, diagnostic)
			}
			var r map[string]any
			if err := json.Unmarshal([]byte(out), &r); err != nil {
				t.Fatal(err)
			}
			if r["status"] != "ok" {
				t.Fatal(out)
			}
			status, out, _ = runRelease(t, "plan", commit, d, "--version", "v1.2.3")
			if status != tc.want || !strings.Contains(out, "no project command executed") || !strings.Contains(out, "verdict:") {
				t.Fatalf("%d %s", status, out)
			}
			hostile := filepath.Join(t.TempDir(), "repository\nforged\x1b")
			if err := os.Rename(d, hostile); err != nil {
				t.Fatal(err)
			}
			_, out, _ = runRelease(t, "plan", commit, hostile, "--version", "v1.2.3")
			if strings.Contains(out, "\x1b") || strings.Contains(out, "repository\nforged") {
				t.Fatal("raw hostile path in output")
			}
			if tc.example == "skill-pack" && !strings.Contains(out, `repository\\nforged\\x1b`) {
				t.Fatalf("escaped argv path missing: %s", out)
			}
		})
	}
}

func TestReleasePlanJSONRefusal(t *testing.T) {
	dir := t.TempDir()
	status, out, diagnostic := runRelease(t, "plan", strings.Repeat("a", 40), dir, "--version", "v1.0.0", "--json")
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("%v: %q %s", err, out, diagnostic)
	}
	if status != 1 || result["command"] != "release-plan" || result["status"] != "refused" {
		t.Fatalf("%d %s %s", status, out, diagnostic)
	}
}
