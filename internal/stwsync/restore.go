// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// stateRestored marks an entry whose incomplete record a restore has dealt with; retention treats it
// as a finished record.
const stateRestored = "restored"

// RestoreOptions selects one restore: the workspace path and the run to undo (empty: the latest run).
type RestoreOptions struct {
	Path  string
	RunID string
}

// recordRunID is the form of a run ID, also the file name of its record.
var recordRunID = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}([0-9]{6})?Z-[0-9a-f]{8}$`)

// Restore moves each branch that run RunID changed back to its journaled old OID, but only where the
// branch still points at the journaled new OID: `git reset --keep` in the main worktree when the branch is
// checked out there and the worktree is clean and idle, `git update-ref` with the expected value when it
// is checked out nowhere. Anything else is changed-since and left alone. A restore of a record that was
// never completed follows Spec 032 "Journal and restore". Pruned refs, stash reapplies and submodule HEAD
// moves are not undone.
func Restore(ctx context.Context, o RestoreOptions) Result {
	r := newResult(Options{})
	r.Mode = "restore"
	if err := ValidateOptions(Options{Path: o.Path}); err != nil {
		return r.fail("sync-invalid-options", err.Error())
	}
	if o.RunID != "" && !recordRunID.MatchString(o.RunID) {
		return r.fail("sync-invalid-options", "--run needs a run ID as printed by stw sync")
	}
	root, _, failure := openWorkspace(ctx, o.Path)
	if failure != nil {
		r.Error = failure
		return r
	}
	repos := []env{root}
	var collect func(e env, depth int) bool
	collect = func(e env, depth int) bool {
		subs, err := e.submodules(ctx)
		if err != nil {
			return true
		}
		for _, p := range subs {
			child := e.withDir(filepath.Join(e.dir, filepath.FromSlash(p)))
			if _, err := os.Lstat(filepath.Join(child.dir, ".git")); err != nil {
				continue
			}
			if repos = append(repos, child); len(repos) > repoLimit || depth > 16 {
				return false
			}
			if !collect(child, depth+1) {
				return false
			}
		}
		return true
	}
	if !collect(root, 0) {
		return r.fail("sync-limit", "the workspace has too many repositories")
	}
	id := o.RunID
	if id == "" {
		// the latest run is the one whose record was written last; run IDs have second resolution, so the
		// ID only breaks a tie of the modification times
		var latest time.Time
		for _, e := range repos {
			dir := journalDirOf(ctx, e)
			for _, found := range recordIDs(ctx, e) {
				fi, err := os.Stat(filepath.Join(dir, found+".json"))
				if err != nil {
					continue
				}
				if id == "" || fi.ModTime().After(latest) || (fi.ModTime().Equal(latest) && found > id) {
					id, latest = found, fi.ModTime()
				}
			}
		}
	}
	if id == "" {
		return r.fail("sync-restore-no-run", "no stw sync run is journaled in this workspace")
	}
	found := false
	for i, e := range repos {
		name := "."
		if i > 0 {
			name = relName(root.dir, e.dir)
		}
		j, ok := loadJournal(ctx, e, id)
		if !ok {
			continue
		}
		found = true
		kind := "submodule"
		if i == 0 {
			kind = "root"
		}
		for k := range j.rec.Entries {
			rp := restoreEntry(ctx, e, j, k)
			rp.Name, rp.Path, rp.Kind = name, e.dir, kind
			r.Repos = append(r.Repos, rp)
		}
	}
	if !found {
		return r.fail("sync-restore-no-run", "run "+id+" is not journaled in this workspace")
	}
	r.RunID = ptr(id)
	for _, it := range r.Repos {
		r.Summary.ByResult[it.Result]++
	}
	r.Summary.Total = len(r.Repos)
	return r
}

func relName(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return dir
	}
	return filepath.ToSlash(rel)
}

func journalDirOf(ctx context.Context, e env) string {
	out, err := e.g(ctx, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || out.Code != 0 || strings.TrimSpace(out.Out) == "" {
		return ""
	}
	return filepath.Join(strings.TrimSpace(out.Out), "stw", "sync")
}

// recordIDs lists the run IDs that have a record file in the repository.
func recordIDs(ctx context.Context, e env) []string {
	dir := journalDirOf(ctx, e)
	if dir == "" {
		return nil
	}
	files, _ := os.ReadDir(dir)
	var ids []string
	for _, f := range files {
		if id, ok := strings.CutSuffix(f.Name(), ".json"); ok && recordRunID.MatchString(id) && !f.IsDir() {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func loadJournal(ctx context.Context, e env, id string) (*journal, bool) {
	dir := journalDirOf(ctx, e)
	if dir == "" {
		return nil, false
	}
	raw, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, false
	}
	j := &journal{e: e, dir: dir, path: filepath.Join(dir, id+".json")}
	if json.Unmarshal(raw, &j.rec) != nil || j.rec.Type != "sync" || j.rec.RunID != id {
		return nil, false
	}
	return j, true
}

// heldElsewhere reports whether a linked worktree has the branch checked out.
func heldElsewhere(ctx context.Context, e env, ref string) bool {
	out, err := e.g(ctx, "worktree", "list", "--porcelain")
	if err != nil || out.Code != 0 {
		return true // unreadable counts as held: nothing is moved
	}
	for _, l := range strings.Split(out.Out, "\n") {
		if strings.TrimSpace(l) == "branch "+ref {
			return true
		}
	}
	return false
}

// restoreHook is a test seam called after the checks and before the branch change.
var restoreHook func(dir string)

// restoreEntry restores entry k of the record and reports it. The record is data: a ref that is not a
// branch or an OID that is not an object ID is failed before Git sees it.
func restoreEntry(ctx context.Context, e env, j *journal, k int) Repo {
	en := j.rec.Entries[k]
	rp := Repo{Result: ResultFailed}
	if strings.HasPrefix(en.Ref, "refs/heads/") {
		rp.Branch = ptr(strings.TrimPrefix(en.Ref, "refs/heads/"))
	}
	fail := func(reason string) Repo { rp.Result, rp.Reason = ResultFailed, ptr(oneLine(reason)); return rp }
	changed := func(reason string) Repo { rp.Result, rp.Reason = ResultChangedSince, ptr(reason); return rp }
	expected := en.NewOID
	if expected == "" {
		expected = en.PlannedNewOID
	}
	if !strings.HasPrefix(en.Ref, "refs/heads/") || strings.HasPrefix(strings.TrimPrefix(en.Ref, "refs/heads/"), "-") ||
		!oidPattern.MatchString(en.OldOID) || (expected != "" && !oidPattern.MatchString(expected)) {
		return fail("bad-record")
	}
	if out, err := e.g(ctx, "check-ref-format", en.Ref); err != nil || out.Code != 0 {
		return fail("bad-record")
	}
	switch en.State {
	case stateFailed, "conflict":
		// the change never happened (or a rebase is left to the owner): nothing to restore
		rp.Result, rp.Reason = ResultAlreadyRestored, ptr("the run did not change the branch")
		return rp
	case statePrepared:
		if expected == "" {
			rp.Result, rp.Reason = ResultJournalIncomplete, ptr("no planned new tip is recorded; the recovery ref "+oneLine(en.RecoveryRef)+" names the old tip for a manual reset")
			return rp
		}
	}
	cur := e.revParse(ctx, e.dir, en.Ref)
	if cur == "" {
		return changed("the branch no longer exists")
	}
	rp.OldOID = ptr(cur)
	done := func(result string) Repo {
		rp.Result = result
		rp.NewOID = ptr(en.OldOID)
		if en.State == statePrepared {
			if err := j.finish(k, stateRestored, expected, "", "restored after an incomplete record"); err != nil {
				rp.Reason = ptr("restored, but the record stays incomplete: " + oneLine(err.Error()))
			}
		}
		return rp
	}
	if cur == en.OldOID {
		return done(ResultAlreadyRestored)
	}
	if cur != expected {
		return changed("the branch moved since the run")
	}
	sym, _ := e.g(ctx, "symbolic-ref", "-q", "HEAD")
	if strings.TrimSpace(sym.Out) == en.Ref {
		if reason, _ := e.operation(ctx, e.dir); reason != "" {
			return changed(reason)
		}
		if hasTrackedChanges(ctx, e, e.dir) {
			return changed("the worktree has local changes")
		}
		if restoreHook != nil {
			restoreHook(e.dir)
		}
		if out, err := e.g(ctx, "-c", "submodule.recurse=false", "reset", "--keep", en.OldOID, "--"); err != nil || out.Code != 0 {
			return fail("git-failed: " + firstLine(out.Diagnostic+"\n"+out.Out))
		}
		if e.revParse(ctx, e.dir, en.Ref) != en.OldOID {
			return fail("raced")
		}
		return done(ResultRestored)
	}
	if heldElsewhere(ctx, e, en.Ref) {
		return changed("the branch is checked out in another worktree")
	}
	if restoreHook != nil {
		restoreHook(e.dir)
	}
	if out, err := e.g(ctx, "update-ref", en.Ref, en.OldOID, expected); err != nil || out.Code != 0 {
		return changed("the branch moved while restoring")
	}
	return done(ResultRestored)
}
