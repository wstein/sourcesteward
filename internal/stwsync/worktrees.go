// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// operation reports an operation in progress in the worktree (rebase, merge, cherry-pick, revert or
// bisect) and the branch refs it works on. An unreadable Git directory counts as busy.
func (e env) operation(ctx context.Context, tree string) (string, []string) {
	out, err := e.in(ctx, tree, "rev-parse", "--absolute-git-dir")
	dir := strings.TrimSpace(out.Out)
	if err != nil || out.Code != 0 || dir == "" {
		return "the worktree state cannot be read", nil
	}
	exists := func(name string) bool { _, err := os.Lstat(filepath.Join(dir, name)); return err == nil }
	read := func(name string) string {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(raw))
	}
	var reason string
	var involved []string
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if exists(d) {
			reason = "a rebase is in progress in the worktree"
			if name := read(d + "/head-name"); strings.HasPrefix(name, "refs/heads/") {
				involved = append(involved, name)
			}
		}
	}
	for file, what := range map[string]string{
		"MERGE_HEAD": "a merge", "CHERRY_PICK_HEAD": "a cherry-pick", "REVERT_HEAD": "a revert", "BISECT_LOG": "a bisect",
	} {
		if reason == "" && exists(file) {
			reason = what + " is in progress in the worktree"
		}
	}
	if exists("BISECT_START") {
		if reason == "" {
			reason = "a bisect is in progress in the worktree"
		}
		if name := read("BISECT_START"); name != "" {
			involved = append(involved, "refs/heads/"+strings.TrimPrefix(name, "refs/heads/"))
		}
	}
	return reason, involved
}
