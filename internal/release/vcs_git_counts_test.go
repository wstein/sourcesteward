// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllowlistMetadataStreamBoundsAndCompleteness(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        uint64
		err         error
	}{
		{"empty", "", 0, nil},
		{"path bound", strings.Repeat("x", maxPathBytes) + "\x00", 1, nil},
		{"too long", strings.Repeat("x", maxPathBytes+1) + "\x00", 0, errReleaseLimit},
		{"truncated", "docs/a", 0, errGitFailed},
		{"empty record", "\x00", 0, errGitFailed},
		{"no diff or buffered-output cap", strings.Repeat(strings.Repeat("x", 220)+"\x00", 20001), 20001, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := newAllowlistMatches([]string{"**"})
			err := countAllowlistPaths(strings.NewReader(tc.input), rows)
			if !errors.Is(err, tc.err) || err == nil && rows[0].TrackedFiles != tc.want {
				t.Fatalf("rows=%+v err=%v", rows, err)
			}
		})
	}
}

func TestGitAllowlistStreamRequiresSuccessfulProcess(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"late failure", "printf 'docs/a\\000'; exit 7"},
		{"truncated", "printf docs/a"},
		{"path overflow", "i=0; while [ \"$i\" -lt 4097 ]; do printf x; i=$((i+1)); done; printf '\\000'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\n"+tc.body+"\n"), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			source := &gitSource{ctx: context.Background(), root: bin, commit: strings.Repeat("a", 40)}
			rows, err := source.allowlistMatches([]string{"docs/**"})
			if err == nil || rows != nil {
				t.Fatalf("partial counts exposed: %+v %v", rows, err)
			}
		})
	}
}

func TestGitAllowlistStreamHasNoRepositoryCountCap(t *testing.T) {
	dir, commit := planRepo(t)
	blob := strings.TrimSpace(git(t, dir, "rev-parse", commit+":.stw/release.toml"))
	var tree strings.Builder
	for i := 0; i < 20001; i++ {
		fmt.Fprintf(&tree, "100644 blob %s\t%05d%s\n", blob, i, strings.Repeat("x", 215))
	}
	cmd := gitCommand(context.Background(), dir, "mktree")
	cmd.Stdin = strings.NewReader(tree.String())
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	cmd = gitCommand(context.Background(), dir, "commit-tree", strings.TrimSpace(string(out)), "-p", commit, "-m", "metadata fixture")
	cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=t@example.org", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=t@example.org")
	out, err = cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	source, err := newGitSource(context.Background(), dir, strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := source.allowlistMatches([]string{"*", "absent/**"})
	if err != nil || len(rows) != 2 || rows[0].TrackedFiles != 20001 || rows[1].TrackedFiles != 0 {
		t.Fatalf("%+v %v", rows, err)
	}
}
