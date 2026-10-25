// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"regexp"
	"strings"
)

var oidPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// applyHook is a test seam called after the journal record and before the branch change.
var applyHook func(dir string)

func (e env) revParse(ctx context.Context, tree, rev string) string {
	out, err := e.in(ctx, tree, "rev-parse", "--verify", "--quiet", rev)
	if err != nil || out.Code != 0 {
		return ""
	}
	return strings.TrimSpace(out.Out)
}

func (e env) merging(ctx context.Context) bool {
	reason, _ := e.operation(ctx, e.dir)
	return reason != ""
}

// apply changes the current branch of one repository. It never returns without a final result.
// Fast-forward is `git merge --ff-only NEW_OID` (never a merge commit, never a ref name); rebase is
// `git rebase` of the local commits onto the upstream with the configured or requested autostash. Git's
// own messages decide what a refusal means, as for git pull.
func (s *session) apply(ctx context.Context, e env, rp Repo, action, ref, upstream, upstreamOID string, mode pullMode, autostash bool) Repo {
	failed := func(reason string) Repo {
		rp.Result, rp.Reason, rp.NewOID = ResultFailed, ptr(oneLine(reason)), nil
		return rp
	}
	oldOID := *rp.OldOID
	if !oidPattern.MatchString(oldOID) || !oidPattern.MatchString(upstreamOID) {
		return failed("invalid-plan")
	}
	planned := ""
	if action == ResultFastForward {
		planned = upstreamOID
		// A pure fast-forward only: the old tip must be an ancestor of the new one.
		if out, err := e.g(ctx, "merge-base", "--is-ancestor", oldOID, upstreamOID); err != nil || out.Code != 0 {
			return failed("not-fast-forward")
		}
	}
	j, err := s.journal(ctx, e)
	if err != nil {
		return failed("journal-failed: " + err.Error())
	}
	i, err := j.prepare(ctx, ref, e.dir, oldOID, planned)
	if err != nil {
		return failed("journal-failed: " + err.Error())
	}
	s.journaled = true
	finish := func(state, reason string) Repo {
		j.finish(i, state, "", "", reason)
		return failed(reason)
	}
	if applyHook != nil {
		applyHook(e.dir)
	}
	if sym, _ := e.g(ctx, "symbolic-ref", "-q", "HEAD"); strings.TrimSpace(sym.Out) != ref || e.revParse(ctx, e.dir, "HEAD") != oldOID {
		return finish(stateFailed, "head-moved")
	}
	stash := "--no-autostash"
	if autostash {
		stash = "--autostash"
	}
	// refs/stash is shared by all worktrees: remember its entries to recognise the one git stores for this run
	var stashBefore []stashEntry
	stashReadable := false
	if autostash {
		stashBefore, stashReadable = e.stashEntries(ctx)
	}
	var out gitOutput
	if action == ResultMerge {
		// the one case where a merge commit is wanted: only an explicit --merge on a diverged branch, made by git's own merge
		out, err = e.g(ctx, "-c", "submodule.recurse=false", "merge", "--no-edit", "--ff", stash, upstreamOID)
	} else if action == ResultFastForward {
		out, err = e.g(ctx, "-c", "submodule.recurse=false", "merge", "--ff-only", stash, upstreamOID)
	} else {
		args := []string{"-c", "submodule.recurse=false", "-c", "rebase.updateRefs=false", "rebase", "--no-update-refs", stash, "--fork-point"}
		if mode.merges {
			args = append(args, "--rebase-merges")
		}
		out, err = e.g(ctx, append(args, upstream)...)
	}
	text := strings.TrimSpace(out.Out + "\n" + out.Diagnostic)
	if err != nil || out.Code != 0 {
		if e.merging(ctx) { // a rebase stopped on a conflict: leave it for the owner, as git does
			j.finish(i, "conflict", "", "", "stopped on a conflict")
			rp.Result, rp.NewOID = ResultConflict, nil
			rp.Reason = ptr("rebase stopped on a conflict; resolve it, then run git rebase --continue (or git rebase --abort)")
			if action == ResultMerge {
				rp.Reason = ptr("merge stopped on a conflict; resolve it, then run git commit (or git merge --abort)")
			}
			rp.Detail = ptr(detail(text))
			return rp
		}
		reason := firstLine(out.Diagnostic + "\n" + out.Out)
		low := strings.ToLower(out.Diagnostic + out.Out)
		if e.revParse(ctx, e.dir, "HEAD") == oldOID && (strings.Contains(low, "would be overwritten") || strings.Contains(low, "local changes") || strings.Contains(low, "unstaged changes") || strings.Contains(low, "uncommitted changes")) {
			j.finish(i, stateFailed, "", "", reason)
			rp.Result, rp.NewOID, rp.Reason = ResultSkippedDirty, nil, ptr(oneLine("git refused: "+reason))
			return rp
		}
		return finish(stateFailed, "git-failed: "+reason)
	}
	newOID := e.revParse(ctx, e.dir, "HEAD")
	origHead := e.revParse(ctx, e.dir, "ORIG_HEAD")
	if newOID == "" || origHead != oldOID || (planned != "" && newOID != planned) || e.revParse(ctx, e.dir, ref) != newOID {
		return finish(stateFailed, "raced")
	}
	if action == ResultRebase || action == ResultMerge {
		if out, err := e.g(ctx, "merge-base", "--is-ancestor", upstreamOID, newOID); err != nil || out.Code != 0 {
			return finish(stateFailed, "raced")
		}
	}
	conflicted := strings.Contains(text, "resulted in conflicts")
	var stashOID string
	if conflicted && stashReadable {
		stashOID = e.claimAutostash(ctx, stashBefore, oldOID)
	}
	if stashOID != "" {
		// a failed write is reported by finish below, which rewrites the same record (j.stash sets the field first)
		_ = j.stash(i, stashOID)
	}
	if err := j.finish(i, stateCompleted, newOID, origHead, ""); err != nil {
		rp.Reason = ptr("the journal record stays incomplete: " + oneLine(err.Error()))
	}
	rp.NewOID = ptr(newOID)
	rp.Result = ResultFastForwarded
	switch action {
	case ResultRebase:
		rp.Result = ResultRebased
	case ResultMerge:
		rp.Result = ResultMerged
	}
	if conflicted {
		rp.Result, rp.Reason = ResultAutostashConflict, ptr("your changes are kept in the stash; resolve with git stash pop or git stash drop")
		rp.Detail = ptr(detail(text))
		if stashOID != "" {
			rp.StashOID = ptr(stashOID)
		}
	}
	return rp
}

// firstLine is Git's own refusal in one line: its first two message lines (the second names the files).
func firstLine(s string) string {
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "hint:") && !strings.HasPrefix(l, "Aborting") {
			keep = append(keep, l)
		}
		if len(keep) == 2 {
			break
		}
	}
	if len(keep) == 0 {
		return "no message"
	}
	return oneLine(strings.Join(keep, " "))
}

func detail(s string) string {
	if len(s) > 1500 {
		s = strings.ToValidUTF8(s[:1500], "") + "..."
	}
	return s
}

// journal creates the journal of one repository on first use.
func (s *session) journal(ctx context.Context, e env) (*journal, error) {
	if s.journals == nil {
		s.journals = map[string]*journal{}
	}
	if j, ok := s.journals[e.dir]; ok {
		return j, nil
	}
	j, err := newJournal(ctx, e, s.runID, "")
	if err != nil {
		return nil, err
	}
	s.journals[e.dir] = j
	return j, nil
}

// stashEntry is one entry of the stash reflog: the stash commit, its reflog subject and its first parent.
type stashEntry struct{ oid, subject, parent string }

// stashEntries lists the entries of refs/stash. A missing ref is an empty list; any other failure is
// reported as not readable so that nothing is claimed.
func (e env) stashEntries(ctx context.Context) ([]stashEntry, bool) {
	if out, err := e.g(ctx, "rev-parse", "--verify", "--quiet", "refs/stash"); err != nil {
		return nil, false
	} else if out.Code == 1 {
		return nil, true
	} else if out.Code != 0 {
		return nil, false
	}
	out, err := e.g(ctx, "log", "-g", "--format=%H%x1f%gs%x1f%P%x1e", "refs/stash", "--")
	if err != nil || out.Code != 0 {
		return nil, false
	}
	var entries []stashEntry
	for _, rec := range strings.Split(out.Out, "\x1e") {
		f := strings.Split(strings.TrimSpace(rec), "\x1f")
		if len(f) != 3 || !oidPattern.MatchString(f[0]) {
			continue
		}
		parent, _, _ := strings.Cut(f[2], " ")
		entries = append(entries, stashEntry{f[0], f[1], parent})
	}
	return entries, true
}

// claimAutostash returns the OID of the one new stash entry that is this run's autostash: stored by
// git with the reflog subject "autostash" on top of the old tip. An entry another worktree pushed or one
// that predates the run is never claimed; with none or more than one candidate it returns "".
func (e env) claimAutostash(ctx context.Context, before []stashEntry, oldOID string) string {
	after, ok := e.stashEntries(ctx)
	if !ok {
		return ""
	}
	known := map[string]bool{}
	for _, en := range before {
		known[en.oid] = true
	}
	found := ""
	for _, en := range after {
		if known[en.oid] || en.subject != "autostash" || en.parent != oldOID {
			continue
		}
		if found != "" {
			return "" // ambiguous
		}
		found = en.oid
	}
	return found
}
