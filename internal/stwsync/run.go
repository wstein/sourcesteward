// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"regexp"
	"strings"
)

var oidPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// applyHook is a test seam called after the journal record and before the ref change.
var applyHook func(ref string)

// Run fetches once, classifies like Plan and then fast-forwards every item the plan marks fast-forward.
// Only a pure fast-forward is ever applied: a checked-out branch by `git merge --ff-only NEW_OID` in its
// worktree, any other branch by `git update-ref` with the expected old OID. Every change is journaled
// first; an item failure never stops the others.
func Run(ctx context.Context, o Options) Result {
	r, e := plan(ctx, o)
	r.Mode = "sync"
	if r.Error != nil {
		return r
	}
	var j *journal
	for i, it := range r.Items {
		if it.Result != ResultFastForward {
			continue
		}
		if j == nil {
			var err error
			if j, err = newJournal(ctx, e, r.Remote); err != nil {
				r.Items[i].Result, r.Items[i].Reason = ResultFailed, ptr("journal-failed: "+oneLine(err.Error()))
				continue
			}
			r.RunID = ptr(j.runID())
		}
		r.Items[i] = e.applyItem(ctx, j, it)
	}
	r.Summary.ByResult = map[string]int{}
	for _, it := range r.Items {
		r.Summary.ByResult[it.Result]++
	}
	return r
}

func failed(it Item, reason string) Item {
	it.Result, it.Reason, it.NewOID = ResultFailed, ptr(reason), nil
	return it
}

func (e env) revParse(ctx context.Context, tree, rev string) string {
	out, err := e.in(ctx, tree, "rev-parse", "--verify", "--quiet", rev)
	if err != nil || out.Code != 0 {
		return ""
	}
	return strings.TrimSpace(out.Out)
}

// applyItem changes one branch; it never returns without a final item result.
func (e env) applyItem(ctx context.Context, j *journal, it Item) Item {
	if it.OldOID == nil || it.NewOID == nil || !oidPattern.MatchString(*it.OldOID) || !oidPattern.MatchString(*it.NewOID) {
		return failed(it, "invalid-plan")
	}
	oldOID, newOID, ref := *it.OldOID, *it.NewOID, "refs/heads/"+it.Branch
	tree := ""
	if it.Worktree != nil {
		tree = *it.Worktree
	}
	// A pure fast-forward only: the old tip must be an ancestor of the new one.
	if out, err := e.g(ctx, "merge-base", "--is-ancestor", oldOID, newOID); err != nil || out.Code != 0 {
		return failed(it, "not-fast-forward")
	}
	if tree != "" && hasTrackedChanges(e.in2(ctx), tree) {
		it.Result, it.Reason, it.NewOID = ResultSkippedDirty, ptr("the worktree has tracked or staged changes"), nil
		return it
	}
	i, err := j.prepare(ctx, ref, tree, oldOID, newOID)
	if err != nil {
		return failed(it, "journal-failed: "+oneLine(err.Error()))
	}
	fail := func(reason string) Item {
		j.finish(i, stateFailed, "", "", reason)
		return failed(it, reason)
	}
	if applyHook != nil {
		applyHook(ref)
	}
	origHead := ""
	if tree == "" {
		out, err := e.g(ctx, "update-ref", "-m", "stw sync: fast-forward", ref, newOID, oldOID)
		if err != nil || out.Code != 0 {
			return fail("raced")
		}
	} else {
		sym, _ := e.in(ctx, tree, "symbolic-ref", "-q", "HEAD")
		if strings.TrimSpace(sym.Out) != ref || e.revParse(ctx, tree, "HEAD") != oldOID {
			return fail("head-moved")
		}
		out, err := e.in(ctx, tree, "-c", "submodule.recurse=false", "merge", "--ff-only", "--no-autostash", newOID)
		if err != nil || out.Code != 0 {
			return fail("git-failed: " + oneLine(out.Diagnostic+" "+out.Out))
		}
		origHead = e.revParse(ctx, tree, "ORIG_HEAD")
		if e.revParse(ctx, tree, "HEAD") != newOID || origHead != oldOID {
			return fail("raced")
		}
	}
	if e.revParse(ctx, e.dir, ref) != newOID {
		return fail("raced")
	}
	if err := j.finish(i, stateCompleted, newOID, origHead, ""); err != nil {
		it.Reason = ptr("the journal record stays incomplete: " + oneLine(err.Error()))
	}
	it.Result = ResultFastForwarded
	return it
}
