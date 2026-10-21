// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"sort"
	"strings"
)

// pullMode is what `git pull` would do for a diverged branch under the user's configuration.
type pullMode struct {
	rebase  bool // --rebase, pull.rebase or branch.NAME.rebase is true, merges or interactive
	merges  bool // pull.rebase=merges: keep merge commits while rebasing
	ffOnly  bool // --ff-only or pull.ff=only: a diverged branch is an error
	flagged bool
}

func (e env) config(ctx context.Context, key string) string {
	out, err := e.g(ctx, "config", "--get", key)
	if err != nil || out.Code != 0 {
		return ""
	}
	return strings.TrimSpace(out.Out)
}

func (e env) configBool(ctx context.Context, key string) bool {
	out, err := e.g(ctx, "config", "--type=bool", "--get", key)
	return err == nil && out.Code == 0 && strings.TrimSpace(out.Out) == "true"
}

// pullMode resolves --rebase/--ff-only over branch.NAME.rebase over pull.rebase, and pull.ff.
func (e env) pullMode(ctx context.Context, o Options, branch string) pullMode {
	switch o.Rebase {
	case On:
		return pullMode{rebase: true, flagged: true}
	case Off:
		return pullMode{ffOnly: true, flagged: true}
	}
	value := e.config(ctx, "branch."+branch+".rebase")
	if value == "" {
		value = e.config(ctx, "pull.rebase")
	}
	m := pullMode{}
	switch strings.ToLower(value) {
	case "true", "yes", "on", "1", "interactive", "i":
		m.rebase = true
	case "merges", "m":
		m.rebase, m.merges = true, true
	}
	if !m.rebase && strings.ToLower(e.config(ctx, "pull.ff")) == "only" {
		m.ffOnly = true
	}
	return m
}

// autostash resolves --autostash/--no-autostash over rebase.autoStash (rebase) or merge.autoStash.
func (e env) autostash(ctx context.Context, o Options, m pullMode) bool {
	switch o.Autostash {
	case On:
		return true
	case Off:
		return false
	}
	if m.rebase {
		return e.configBool(ctx, "rebase.autoStash")
	}
	return e.configBool(ctx, "merge.autoStash")
}

// dirtyOverlap lists tracked files with local changes that the update from oldOID to newOID also changes:
// the files for which `git merge --ff-only` would refuse to run on a dirty worktree.
func (e env) dirtyOverlap(ctx context.Context, oldOID, newOID string) []string {
	st, err := e.g(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=no")
	if err != nil || st.Code != 0 {
		return []string{"(the worktree cannot be read)"}
	}
	dirty := map[string]bool{}
	fields := strings.Split(st.Out, "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		dirty[f[3:]] = true
		if f[0] == 'R' || f[0] == 'C' || f[1] == 'R' || f[1] == 'C' {
			i++
			if i < len(fields) {
				dirty[fields[i]] = true
			}
		}
	}
	diff, err := e.g(ctx, "diff", "--name-only", "-z", "--no-renames", oldOID, newOID)
	if err != nil || diff.Code != 0 {
		return []string{"(the update cannot be compared)"}
	}
	var hit []string
	for _, p := range strings.Split(diff.Out, "\x00") {
		if p != "" && dirty[p] {
			hit = append(hit, p)
		}
	}
	sort.Strings(hit)
	if len(hit) > 3 {
		hit = append(hit[:3], "...")
	}
	return hit
}
