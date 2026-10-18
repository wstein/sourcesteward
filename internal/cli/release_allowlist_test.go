// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestReleasePlanAllowlistCountsBothFormats(t *testing.T) {
	dir := releaseRepo(t, map[string][]byte{".stw/release.toml": releaseExample(t, "skill-pack"), "DOCS/CHANGELOG.md": []byte("tracked")})
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=t@example.org", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=t@example.org")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("add", ".")
	git("commit", "-qm", "fixture")
	commit := git("rev-parse", "HEAD")
	git("remote", "add", "origin", "https://github.com/example/test.git")
	git("update-ref", "refs/remotes/origin/main", commit)
	status, out, _ := runRelease(t, "plan", commit, dir, "--version", "v1.2.3", "--json")
	if status != 3 {
		t.Fatalf("%d %s", status, out)
	}
	var result struct {
		Allowlist []struct {
			Pattern string `json:"pattern"`
			Count   int    `json:"tracked_files"`
		} `json:"allowlist_matches"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Allowlist) != 2 || result.Allowlist[0].Pattern != "docs/**" || result.Allowlist[0].Count != 1 || result.Allowlist[1].Pattern != "CHANGELOG.md" || result.Allowlist[1].Count != 0 {
		t.Fatalf("%s", out)
	}
	status, out, _ = runRelease(t, "plan", commit, dir, "--version", "v1.2.3")
	if status != 3 || !strings.Contains(out, `"docs/**": 1 tracked files`) || !strings.Contains(out, `"CHANGELOG.md": 0 tracked files`) {
		t.Fatalf("%d %s", status, out)
	}
}
