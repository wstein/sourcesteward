// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPlanDisclosesAllowlistReachAtCommit(t *testing.T) {
	dir, _ := planRepo(t)
	raw := strings.Replace(string(exampleBytes(t, "skill-pack")), `paths = ["docs/**", "CHANGELOG.md"]`, `paths = ["docs/**", "CHANGELOG.md", "absent/**", "DOCS/*.md"]`, 1)
	write(t, dir, ".stw/release.toml", []byte(raw))
	write(t, dir, "docs/a.md", []byte("a"))
	write(t, dir, "DOCS/hostile\nname.md", []byte("b"))
	if err := os.Symlink("missing", dir+"/docs/link"); err != nil {
		t.Fatal(err)
	}
	commit := commitAll(t, dir)
	git(t, dir, "update-ref", "refs/remotes/upstream/main", commit)
	git(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+commit+",docs/submodule")
	git(t, dir, "commit", "-qm", "tracked gitlink")
	commit = strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	write(t, dir, "docs/untracked.md", []byte("ignored"))
	result := Plan(context.Background(), PlanOptions{Path: dir, Subject: commit, Version: "v1.2.3"})
	data, _ := json.Marshal(result)
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := `[{"pattern":"docs/**","tracked_files":4},{"pattern":"CHANGELOG.md","tracked_files":0},{"pattern":"absent/**","tracked_files":0},{"pattern":"DOCS/*.md","tracked_files":2}]`
	rows, _ := json.Marshal(got["allowlist_matches"])
	if string(rows) != want {
		t.Fatalf("allowlist reach %s want %s; result %s", rows, want, data)
	}
	for _, line := range []string{`"docs/**": 4 tracked files`, `"CHANGELOG.md": 0 tracked files`, `"absent/**": 0 tracked files`, `"DOCS/*.md": 2 tracked files`} {
		if !strings.Contains(result.Text(), line) {
			t.Fatalf("missing %q: %s", line, result.Text())
		}
	}
}

func TestPlanUnavailableAllowlistCountsAreNull(t *testing.T) {
	result := Plan(context.Background(), PlanOptions{Path: t.TempDir(), Subject: strings.Repeat("a", 40), Version: "v1.2.3"})
	data, _ := json.Marshal(result)
	if !strings.Contains(string(data), `"allowlist_matches":null`) {
		t.Fatalf("%s", data)
	}
}

func TestNativePlanDisclosesAllowlistReach(t *testing.T) {
	for lane, binary := range jjLanes(t) {
		t.Run(lane, func(t *testing.T) {
			useJJ(t, binary)
			home := t.TempDir()
			dir, ok := jjLayouts()[1].setup(t, home)
			if !ok {
				t.Skip("standalone native jj unavailable")
			}
			write(t, dir, ".stw/release.toml", exampleBytes(t, "skill-pack"))
			write(t, dir, "DOCS/a.md", []byte("a"))
			if err := os.MkdirAll(dir+"/docs", 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("missing", dir+"/docs/link"); err != nil {
				t.Fatal(err)
			}
			if out, err := jjRun(t, dir, home, "commit", "-m", "allowlist fixture"); err != nil {
				t.Fatalf("%s %v", out, err)
			}
			commit, err := jjRun(t, dir, home, "--ignore-working-copy", "log", "--no-graph", "-r", "@-", "-T", "commit_id")
			if err != nil {
				t.Fatal(err)
			}
			if out, err := jjRun(t, dir, home, "git", "remote", "add", "origin", "https://github.com/example/test.git"); err != nil {
				t.Fatalf("%s %v", out, err)
			}
			write(t, dir, "docs/untracked.md", []byte("ignored"))
			before, state := snapshot(t, dir), jjState(t, dir, home)
			result := Plan(context.Background(), PlanOptions{Path: dir, Subject: strings.TrimSpace(commit), Version: "v1.2.3"})
			if result.Status != "ok" || len(result.AllowlistMatches) != 2 || result.AllowlistMatches[0].Pattern != "docs/**" || result.AllowlistMatches[0].TrackedFiles != 2 || result.AllowlistMatches[1].TrackedFiles != 0 {
				t.Fatalf("%+v", result)
			}
			if diff := differences(before, snapshot(t, dir)); len(diff) != 0 {
				t.Fatal(diff)
			}
			if jjState(t, dir, home) != state {
				t.Fatal("counting changed native state")
			}
		})
	}
}
