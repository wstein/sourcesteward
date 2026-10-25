// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// conflictWorkspace is a workspace whose root has a local edit that conflicts with the next update.
func conflictWorkspace(t *testing.T) *workspace {
	t.Helper()
	w := newWorkspace(t)
	w.advanceFile(t, ".", "main", "ws.txt")
	write(t, filepath.Join(w.ws, "ws.txt"), "local edit conflicting with the update\n")
	return w
}

func stashOIDs(t *testing.T, w *workspace) []string {
	t.Helper()
	if gitCmd(w.ws, "rev-parse", "--verify", "--quiet", "refs/stash").Run() != nil {
		return nil
	}
	out := runGit(t, w.ws, "log", "-g", "--format=%H", "refs/stash")
	return strings.Split(out, "\n")
}

func journaledStash(t *testing.T, w *workspace) string {
	t.Helper()
	return readJournals(t, filepath.Join(w.ws, ".git"))[0].Entries[0].StashOID
}

// The autostash that could not be reapplied is reported and journaled by its OID; git keeps the entry.
func TestAutostashConflictReportsAndJournalsTheStashOID(t *testing.T) {
	w := conflictWorkspace(t)
	g := repoByName(t, run(t, w, Options{Autostash: On}))["."]
	top := runGit(t, w.ws, "rev-parse", "refs/stash")
	if g.Result != ResultAutostashConflict || g.StashOID == nil || *g.StashOID != top || journaledStash(t, w) != top {
		t.Fatalf("%+v journal %q top %s", g, journaledStash(t, w), top)
	}
	if len(stashOIDs(t, w)) != 1 {
		t.Fatalf("the stash must be kept: %v", stashOIDs(t, w))
	}
}

func TestAutostashConflictInARebaseReportsTheStashOID(t *testing.T) {
	w := newWorkspace(t)
	commit(t, w.ws, "local.txt", "l\n")
	w.advanceFile(t, ".", "main", "ws.txt")
	write(t, filepath.Join(w.ws, "ws.txt"), "local edit conflicting with the update\n")
	g := repoByName(t, run(t, w, Options{Rebase: On, Autostash: On}))["."]
	if g.Result != ResultAutostashConflict || g.StashOID == nil || *g.StashOID != runGit(t, w.ws, "rev-parse", "refs/stash") {
		t.Fatalf("%+v", g)
	}
}

// A clean reapply leaves no stash entry and no OID anywhere.
func TestCleanAutostashReportsNoStashOID(t *testing.T) {
	w := newWorkspace(t)
	w.advance(t, ".", "main")
	write(t, filepath.Join(w.ws, "ws.txt"), "local edit, no conflict\n")
	g := repoByName(t, run(t, w, Options{Autostash: On}))["."]
	if g.Result != ResultFastForwarded || g.StashOID != nil || journaledStash(t, w) != "" || len(stashOIDs(t, w)) != 0 {
		t.Fatalf("%+v", g)
	}
}

// refs/stash with an older entry of the user, and refs/stash whose reflog was cleared: only the new entry is ours.
func TestStashOIDWithAnExistingStashAndWithAClearedReflog(t *testing.T) {
	w := conflictWorkspace(t)
	write(t, filepath.Join(w.ws, "g.txt"), "user stash\n")
	runGit(t, w.ws, "add", "g.txt")
	runGit(t, w.ws, "stash", "push", "-q", "-m", "user entry", "g.txt")
	write(t, filepath.Join(w.ws, "ws.txt"), "local edit conflicting with the update\n")
	userTop := runGit(t, w.ws, "rev-parse", "refs/stash")
	g := repoByName(t, run(t, w, Options{Autostash: On}))["."]
	if g.Result != ResultAutostashConflict || g.StashOID == nil || *g.StashOID == userTop || *g.StashOID != runGit(t, w.ws, "rev-parse", "refs/stash") {
		t.Fatalf("existing stash: %+v", g)
	}
	if !strings.Contains(runGit(t, w.ws, "stash", "list"), "user entry") {
		t.Fatal("the user's stash entry was touched")
	}

	w = conflictWorkspace(t)
	write(t, filepath.Join(w.ws, "g.txt"), "user stash\n")
	runGit(t, w.ws, "add", "g.txt")
	runGit(t, w.ws, "stash", "push", "-q", "-m", "user entry", "g.txt")
	write(t, filepath.Join(w.ws, "ws.txt"), "local edit conflicting with the update\n")
	runGit(t, w.ws, "reflog", "expire", "--expire=all", "refs/stash")
	g = repoByName(t, run(t, w, Options{Autostash: On}))["."]
	if g.Result != ResultAutostashConflict || g.StashOID == nil || *g.StashOID != runGit(t, w.ws, "rev-parse", "refs/stash") {
		t.Fatalf("cleared reflog: %+v", g)
	}
}

// refs/stash is shared by all worktrees: a stash another worktree pushes during the update is never claimed.
func TestStashOIDNeverClaimsAnotherWorktreesStash(t *testing.T) {
	w := conflictWorkspace(t)
	lw := filepath.Join(w.root, "lw")
	runGit(t, w.ws, "worktree", "add", "-q", "--detach", lw)
	flag := filepath.Join(w.root, "fired")
	hookScript(t, filepath.Join(w.ws, ".git"), "reference-transaction", "[ \"$1\" = prepared ] || exit 0\nwhile read o n ref; do case \"$ref\" in refs/heads/main) [ -e '"+flag+"' ] || { : > '"+flag+"'; ( cd '"+lw+"' && unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX && echo other >> ws.txt && git stash push -q -m other-worktree ); };; esac; done\n")
	g := repoByName(t, run(t, w, Options{Autostash: On}))["."]
	list := runGit(t, w.ws, "stash", "list")
	if g.Result != ResultAutostashConflict || g.StashOID == nil || !strings.Contains(list, "other-worktree") {
		t.Fatalf("%+v\n%s", g, list)
	}
	other := runGit(t, w.ws, "log", "-g", "--format=%H %gs", "refs/stash")
	for _, l := range strings.Split(other, "\n") {
		if strings.HasSuffix(l, "other-worktree") && strings.HasPrefix(l, *g.StashOID) {
			t.Fatalf("the other worktree's stash was claimed: %s", l)
		}
	}
	if journaledStash(t, w) != *g.StashOID || len(stashOIDs(t, w)) != 2 {
		t.Fatalf("journal %q, entries %v", journaledStash(t, w), stashOIDs(t, w))
	}
}

// Two new entries that both look like this run's autostash are ambiguous: no OID is claimed, the item is
// still autostash-conflict and nothing is dropped.
func TestAmbiguousStashIsReportedWithoutAnOID(t *testing.T) {
	w := conflictWorkspace(t)
	old := w.oid(t, ".", "HEAD")
	flag := filepath.Join(w.root, "fired")
	hookScript(t, filepath.Join(w.ws, ".git"), "reference-transaction", "[ \"$1\" = prepared ] || exit 0\nwhile read o n ref; do case \"$ref\" in refs/heads/main) [ -e '"+flag+"' ] || { : > '"+flag+"'; i=$(git commit-tree 'HEAD^{tree}' -p "+old+" -m index) && c=$(git commit-tree 'HEAD^{tree}' -p "+old+" -p \"$i\" -m lookalike) && git stash store -m autostash \"$c\"; };; esac; done\n")
	r := run(t, w, Options{Autostash: On})
	g := repoByName(t, r)["."]
	if g.Result != ResultAutostashConflict || g.StashOID != nil || journaledStash(t, w) != "" || len(stashOIDs(t, w)) != 2 || r.ExitCode() != 1 {
		t.Fatalf("%+v reason %v journal %q entries %v", g, deref(g.Reason), journaledStash(t, w), stashOIDs(t, w))
	}
}

// Nothing stw does ever drops a stash entry.
func TestAutostashConflictDropsNoStashEntry(t *testing.T) {
	w := conflictWorkspace(t)
	write(t, filepath.Join(w.ws, "g.txt"), "user stash\n")
	runGit(t, w.ws, "add", "g.txt")
	runGit(t, w.ws, "stash", "push", "-q", "-m", "user entry", "g.txt")
	write(t, filepath.Join(w.ws, "ws.txt"), "local edit conflicting with the update\n")
	before := stashOIDs(t, w)
	run(t, w, Options{Autostash: On})
	after := stashOIDs(t, w)
	if len(after) != len(before)+1 || after[1] != before[0] {
		t.Fatalf("before %v after %v", before, after)
	}
}

func lookalike(parent string) string {
	return "i=$(git commit-tree 'HEAD^{tree}' -p " + parent + " -m index) && c=$(git commit-tree 'HEAD^{tree}' -p " + parent + " -p \"$i\" -m lookalike) && git stash store -m autostash \"$c\""
}

// An older entry that looks like an autostash of this very tip (left by an earlier run) is not claimed.
func TestStashOIDIgnoresAnOlderLookalikeOnTheSameTip(t *testing.T) {
	w := conflictWorkspace(t)
	old := w.oid(t, ".", "HEAD")
	runGit(t, w.ws, "stash", "store", "-m", "autostash", runGit(t, w.ws, "stash", "create"))
	runGit(t, w.ws, "checkout", "-q", "--", ".")
	write(t, filepath.Join(w.ws, "ws.txt"), "local edit conflicting with the update\n")
	preexisting := runGit(t, w.ws, "rev-parse", "refs/stash")
	if p := runGit(t, w.ws, "rev-parse", preexisting+"^"); p != old {
		t.Fatalf("fixture: parent %s want %s", p, old)
	}
	g := repoByName(t, run(t, w, Options{Autostash: On}))["."]
	if g.Result != ResultAutostashConflict || g.StashOID == nil || *g.StashOID == preexisting || *g.StashOID != runGit(t, w.ws, "rev-parse", "refs/stash") {
		t.Fatalf("%+v", g)
	}
}

// A new "autostash" entry made on top of another tip (another worktree's stw run) is not a candidate.
func TestStashOIDIgnoresANewLookalikeOnAnotherTip(t *testing.T) {
	w := conflictWorkspace(t)
	old := w.oid(t, ".", "HEAD")
	flag := filepath.Join(w.root, "fired")
	hookScript(t, filepath.Join(w.ws, ".git"), "reference-transaction", "[ \"$1\" = prepared ] || exit 0\nwhile read o n ref; do case \"$ref\" in refs/heads/main) [ -e '"+flag+"' ] || { : > '"+flag+"'; "+lookalike("HEAD^")+"; };; esac; done\n")
	g := repoByName(t, run(t, w, Options{Autostash: On}))["."]
	if g.Result != ResultAutostashConflict || g.StashOID == nil || len(stashOIDs(t, w)) != 2 {
		t.Fatalf("%+v", g)
	}
	if p := runGit(t, w.ws, "rev-parse", *g.StashOID+"^"); p != old {
		t.Fatalf("claimed an entry on another tip: parent %s, old tip %s", p, old)
	}
}

// An unreadable stash list before the update means nothing can be told apart: no OID is claimed, although the
// entry is readable afterwards. The user's older stash object disappears when the run starts and comes back
// during the update.
func TestStashOIDIsNotClaimedWhenTheStashListWasUnreadableBefore(t *testing.T) {
	defer func() { applyHook = nil }()
	w := conflictWorkspace(t)
	write(t, filepath.Join(w.ws, "g.txt"), "user stash\n")
	runGit(t, w.ws, "add", "g.txt")
	runGit(t, w.ws, "stash", "push", "-q", "-m", "user entry", "g.txt")
	write(t, filepath.Join(w.ws, "ws.txt"), "local edit conflicting with the update\n")
	user := runGit(t, w.ws, "rev-parse", "refs/stash")
	object := filepath.Join(w.ws, ".git", "objects", user[:2], user[2:])
	hidden := object + ".hidden"
	applyHook = func(dir string) {
		if err := os.Rename(object, hidden); err != nil {
			t.Error(err)
		}
	}
	hookScript(t, filepath.Join(w.ws, ".git"), "reference-transaction", "[ \"$1\" = prepared ] || exit 0\n[ -e '"+hidden+"' ] && mv '"+hidden+"' '"+object+"'\nexit 0\n")
	g := repoByName(t, run(t, w, Options{Autostash: On}))["."]
	if g.Result != ResultAutostashConflict || g.StashOID != nil || journaledStash(t, w) != "" {
		t.Fatalf("%+v journal %q", g, journaledStash(t, w))
	}
	if len(stashOIDs(t, w)) != 2 {
		t.Fatalf("entries %v", stashOIDs(t, w))
	}
}
