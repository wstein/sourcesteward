// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"fmt"
	"strings"

	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// Fact names of the local subjects, one per fact kind of a repository.
const (
	factOperation  = "operation"
	factConflicts  = "conflicts"
	factDirty      = "dirty"
	factDetached   = "detached"
	factUntracked  = "untracked"
	factBehind     = "behind"
	factDiverged   = "diverged"
	factNoUpstream = "no-upstream"
	factStash      = "stash"
	factBranches   = "branches"
)

// localItems maps the Spec 033 facts of one repository to its items. The attention reasons of the
// status are the authority for the yellow and red rows; information facts (stash, unmerged branches,
// gitlink ahead, linked worktrees) are read from the facts. Every fact kind is its own subject, so a
// dirty and behind repository yields two items; one worktree yields one item (dirty before stale).
func localItems(root string, rp stwsync.RepoStatus) []Item {
	repo := rp.Name
	tgt := target{Path: rp.Path, Root: root}
	fact := func(c Category, name, title string, ev map[string]any) Item {
		it := newItem(c, repo, Subject{Kind: KindFact, Name: name}, tgt)
		it.Path, it.Title, it.Evidence = rp.Path, title, ev
		return it
	}
	if rp.State == "error" {
		it := newItem(RepoError, repo, Subject{Kind: KindRepository}, tgt)
		it.Path, it.Title = rp.Path, "repository cannot be read"
		if rp.Error != nil {
			it.Title += ": " + watchcmd.Esc(rp.Error.Code+": "+rp.Error.Message)
			it.Evidence = map[string]any{"code": watchcmd.Esc(rp.Error.Code)}
		}
		return []Item{it}
	}
	has := map[string]bool{}
	for _, r := range rp.Reasons {
		has[r] = true
	}
	var items []Item
	if has["operation"] && rp.Operation != nil {
		op := watchcmd.Esc(*rp.Operation)
		items = append(items, fact(LocalOperation, factOperation, op+" in progress", map[string]any{"operation": op}))
	}
	if has["conflicts"] {
		items = append(items, fact(LocalOperation, factConflicts, fmt.Sprintf("%d conflicted files", rp.Changes.Conflicts), map[string]any{"conflicts": rp.Changes.Conflicts}))
	}
	if has["staged"] || has["unstaged"] {
		items = append(items, fact(LocalChanges, factDirty, fmt.Sprintf("%d staged, %d unstaged files", rp.Changes.Staged, rp.Changes.Unstaged),
			map[string]any{"staged": rp.Changes.Staged, "unstaged": rp.Changes.Unstaged}))
	}
	if has["detached"] {
		head := ""
		if rp.Head != nil {
			head = " at " + watchcmd.Esc(short(*rp.Head))
		}
		items = append(items, fact(LocalDetached, factDetached, "detached HEAD"+head, nil))
	}
	if has["untracked"] {
		items = append(items, fact(LocalUntracked, factUntracked, fmt.Sprintf("%d untracked files", rp.Changes.Untracked), map[string]any{"untracked": rp.Changes.Untracked}))
	}
	if has["diverged"] {
		items = append(items, fact(LocalBehind, factDiverged, fmt.Sprintf("diverged from %s (ahead %d, behind %d)", upstream(rp), count(rp.Ahead), count(rp.Behind)),
			map[string]any{"ahead": count(rp.Ahead), "behind": count(rp.Behind)}))
	} else if has["behind"] {
		items = append(items, fact(LocalBehind, factBehind, fmt.Sprintf("behind %s by %d commits", upstream(rp), count(rp.Behind)), map[string]any{"behind": count(rp.Behind)}))
	}
	if has["no-upstream"] {
		branch := ""
		if rp.Branch != nil {
			branch = " " + watchcmd.Esc(*rp.Branch)
		}
		items = append(items, fact(LocalNoUpstream, factNoUpstream, "branch"+branch+" has no upstream", nil))
	}
	if g := rp.Gitlink; g != nil && g.State == "drift" {
		rel := "unknown"
		if g.Relation != nil {
			rel = watchcmd.Esc(*g.Relation)
		}
		c := GitlinkDrift
		if rel == "ahead" {
			c = GitlinkAhead
		}
		if c == GitlinkAhead || has["gitlink-drift"] {
			it := newItem(c, repo, Subject{Kind: KindGitlink, Name: rp.Path}, tgt)
			it.Path, it.Title, it.Evidence = rp.Path, "checked-out commit is "+rel+" the recorded gitlink", map[string]any{"relation": rel}
			items = append(items, it)
		}
	}
	for _, w := range rp.Worktrees.List {
		wt := tgt
		wt.Worktree = w.Path
		switch {
		case w.Dirty:
			it := newItem(WorktreeDirty, repo, Subject{Kind: KindWorktree, Name: w.Path}, wt)
			it.Path, it.Title = rp.Path, "linked worktree "+watchcmd.Esc(w.Path)+" has changes"
			items = append(items, it)
		case w.Missing || w.Prunable || w.Locked != nil:
			it := newItem(WorktreeStale, repo, Subject{Kind: KindWorktree, Name: w.Path}, wt)
			it.Path, it.Title = rp.Path, "linked worktree "+watchcmd.Esc(w.Path)+" is "+staleWord(w)
			items = append(items, it)
		}
	}
	if rp.Stash > 0 {
		items = append(items, fact(LocalStash, factStash, fmt.Sprintf("%d stashes", rp.Stash), map[string]any{"stash": rp.Stash}))
	}
	if n := rp.Branches.NotIntegrated; n != nil && *n > 0 {
		items = append(items, fact(BranchesUnmerged, factBranches, fmt.Sprintf("%d branches not integrated", *n), map[string]any{"not_integrated": *n}))
	}
	return items
}

// staleWord names why a linked worktree is stale, in the order missing, prunable, locked.
func staleWord(w stwsync.WorktreeInfo) string {
	var words []string
	if w.Missing {
		words = append(words, "missing")
	}
	if w.Prunable {
		words = append(words, "prunable")
	}
	if w.Locked != nil {
		words = append(words, "locked")
	}
	return strings.Join(words, " and ")
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func count(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func upstream(rp stwsync.RepoStatus) string {
	if rp.Upstream == nil {
		return "its upstream"
	}
	return watchcmd.Esc(*rp.Upstream)
}
