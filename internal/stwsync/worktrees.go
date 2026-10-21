// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// worktree is one entry of `git worktree list` with what makes it busy.
type worktree struct {
	path string
	busy string // empty when nothing is in progress and the worktree is not locked
}

// holders maps a full branch ref to every worktree that holds it: as its HEAD branch, as the branch an
// in-progress rebase is working on, or as the branch a bisect started from.
type holders map[string][]worktree

func (h holders) of(ref string) []worktree { return h[ref] }

// inventory lists every worktree once and records, per branch, all worktrees that hold it.
func (e env) inventory(ctx context.Context) (holders, error) {
	out, err := e.g(ctx, "worktree", "list", "--porcelain", "-z")
	if err != nil || out.Code != 0 {
		return nil, errors.New("cannot list the worktrees")
	}
	h := holders{}
	type entry struct {
		path, branch string
		locked, bare bool
	}
	var list []entry
	var cur *entry
	for _, field := range strings.Split(out.Out, "\x00") {
		switch {
		case strings.HasPrefix(field, "worktree "):
			list = append(list, entry{path: strings.TrimPrefix(field, "worktree ")})
			cur = &list[len(list)-1]
		case cur == nil:
		case strings.HasPrefix(field, "branch "):
			cur.branch = strings.TrimPrefix(field, "branch ")
		case field == "locked" || strings.HasPrefix(field, "locked "):
			cur.locked = true
		case field == "bare":
			cur.bare = true
		case field == "":
			cur = nil
		}
	}
	for _, w := range list {
		if w.bare {
			continue
		}
		wt := worktree{path: w.path}
		refs := map[string]bool{}
		if w.branch != "" {
			refs[w.branch] = true
		}
		reason, involved := e.operation(ctx, w.path)
		for _, r := range involved {
			refs[r] = true
		}
		switch {
		case w.locked:
			wt.busy = "the worktree is locked"
		case reason != "":
			wt.busy = reason
		}
		for r := range refs {
			h[r] = append(h[r], wt)
		}
	}
	for _, l := range h {
		sort.Slice(l, func(i, j int) bool { return l[i].path < l[j].path })
	}
	return h, nil
}

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

// block explains why the current branch of a repository must not be touched, or returns "". The
// repository's own worktree holds the branch (as HEAD, or as the branch of its rebase or bisect); a second
// holder, an operation in progress or a lock makes it busy. (In a submodule `git worktree list` names the
// Git directory instead of the working tree, so the holders are counted, not compared by path.)
func (h holders) block(ref string) string {
	l := h[ref]
	if len(l) > 1 {
		return "the branch is also checked out in another worktree"
	}
	if len(l) == 1 && l[0].busy != "" {
		return l[0].busy
	}
	return ""
}
