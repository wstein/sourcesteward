// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// submoduleAfterHook is a test seam called after `git submodule update` and before its result is checked.
var submoduleAfterHook func(dir string)

// validSubmodulePath is the check of a path that comes from the index or .gitmodules, both untrusted: no
// absolute path, no .. segment, no option-like name and no control character.
func validSubmodulePath(p string) bool {
	if p == "" || strings.HasPrefix(p, "-") || filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." || seg == "" {
			return false
		}
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// symlinkedPath reports whether any component of sub below dir is a symbolic link.
func symlinkedPath(dir, sub string) bool {
	cur := dir
	for _, seg := range strings.Split(sub, "/") {
		cur = filepath.Join(cur, seg)
		if fi, err := os.Lstat(cur); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

// recordedCommit is the commit the superproject index records for the submodule path.
func (e env) recordedCommit(ctx context.Context, sub string) string {
	out, err := e.g(ctx, "ls-files", "--stage", "-z", "--", sub)
	if err != nil || out.Code != 0 {
		return ""
	}
	for _, rec := range strings.Split(out.Out, "\x00") {
		meta, path, ok := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if ok && path == sub && len(fields) == 3 && fields[0] == "160000" && fields[2] == "0" && oidPattern.MatchString(fields[1]) {
			return fields[1]
		}
	}
	return ""
}

// unreachableSubmodule is the item of a submodule path that stw refuses before it looks at the directory.
func unreachableSubmodule(e env, name, reason string) Repo {
	return Repo{Name: name, Path: e.dir, Kind: "submodule", Result: ResultUnreachable, Reason: ptr(oneLine(reason))}
}

// submoduleStep is the `--submodules` action on one initialized submodule that is detached (pinned): it
// brings the submodule to the commit the superproject records when that is a fast-forward of its HEAD, with
// `git submodule sync -- PATH` and `git submodule update --no-fetch --checkout -- PATH`. It never clones, fetches
// or initializes, never changes a gitlink and is not journaled. It reports whether the walk goes on into it.
func (s *session) submoduleStep(ctx context.Context, parent env, sub string, e env, rp Repo) (Repo, bool) {
	if rp.Result == ResultSkippedUninitialized {
		rp.Result, rp.Reason = ResultUnreachable, ptr("not initialised; init or fetch it with plain git, stw never clones")
		return rp, false
	}
	if rp.Result != ResultSkippedDetached || rp.OldOID == nil {
		return rp, true // on a branch: synced like git pull; anything else is reported as before
	}
	head := *rp.OldOID
	note := ""
	finish := func(result, reason string) (Repo, bool) {
		rp.Result = result
		if note != "" {
			if reason != "" {
				reason += "; "
			}
			reason += note
		}
		rp.Reason = nil
		if reason != "" {
			rp.Reason = ptr(oneLine(reason))
		}
		return rp, true
	}
	oldURL := e.config(ctx, "remote.origin.url")
	if out, err := parent.g(ctx, "submodule", "sync", "--", sub); err != nil || out.Code != 0 {
		return finish(ResultFailed, "git submodule sync failed: "+firstLine(out.Diagnostic+"\n"+out.Out))
	}
	if newURL := e.config(ctx, "remote.origin.url"); newURL != oldURL {
		note = "remote URL changed by git submodule sync: " + oneLine(oldURL) + " -> " + oneLine(newURL)
	}
	recorded := parent.recordedCommit(ctx, sub)
	if recorded == "" {
		return finish(ResultFailed, "the superproject records no commit for the submodule")
	}
	if out, err := e.g(ctx, "cat-file", "-e", recorded+"^{commit}"); err != nil || out.Code != 0 {
		return finish(ResultUnreachable, "the recorded commit "+recorded[:7]+" is not in the submodule; fetch it with plain git, stw never fetches")
	}
	if reason, _ := e.operation(ctx, e.dir); reason != "" {
		return finish(ResultSkippedBusy, reason)
	}
	if hasTrackedChanges(ctx, e, e.dir) {
		return finish(ResultSkippedDirty, "the submodule has local changes")
	}
	if head == recorded {
		return finish(ResultUpToDate, "")
	}
	if out, err := e.g(ctx, "merge-base", "--is-ancestor", head, recorded); err != nil || out.Code != 0 {
		return finish(ResultDiverged, "the recorded commit "+recorded[:7]+" is not a fast-forward of HEAD")
	}
	out, err := parent.g(ctx, "-c", "submodule.recurse=false", "submodule", "update", "--no-fetch", "--checkout", "--", sub)
	if submoduleAfterHook != nil {
		submoduleAfterHook(e.dir)
	}
	if err != nil || out.Code != 0 {
		return finish(ResultFailed, "git submodule update failed: "+firstLine(out.Diagnostic+"\n"+out.Out))
	}
	if e.revParse(ctx, e.dir, "HEAD") != recorded {
		return finish(ResultFailed, "the submodule did not land on the recorded commit")
	}
	rp.NewOID = ptr(recorded)
	return finish(ResultUpdated, "")
}
