// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func subs(t *testing.T, w *workspace, o Options) Result {
	t.Helper()
	o.Submodules = true
	return run(t, w, o)
}

// pinAhead makes the superproject record a newer commit of the detached submodule than its HEAD. The new commit is
// fetched into the submodule with plain git first (stw never fetches); it returns the old HEAD and the recorded commit.
func pinAhead(t *testing.T, w *workspace, name string) (head, recorded string) {
	t.Helper()
	w.advance(t, name, "main")
	runGit(t, w.dir(name), "fetch", "-q", "origin")
	recorded = runGit(t, w.dir(name), "rev-parse", "origin/main")
	head = runGit(t, w.dir(name), "rev-parse", "HEAD")
	runGit(t, w.ws, "update-index", "--cacheinfo", "160000,"+recorded+","+name)
	return head, recorded
}

func superproject(t *testing.T, w *workspace) string {
	t.Helper()
	return runGit(t, w.ws, "ls-files", "--stage") + "|" + runGit(t, w.ws, "rev-parse", "HEAD", "HEAD^{tree}")
}

func TestSubmodulesFlagUpdatesADetachedSubmoduleToTheRecordedCommit(t *testing.T) {
	w := newWorkspace(t)
	head, recorded := pinAhead(t, w, "c")
	before := superproject(t, w)
	r := subs(t, w, Options{})
	c := repoByName(t, r)["c"]
	if c.Result != ResultUpdated || c.OldOID == nil || *c.OldOID != head || c.NewOID == nil || *c.NewOID != recorded || w.oid(t, "c", "HEAD") != recorded {
		t.Fatalf("%+v", c)
	}
	if r.ExitCode() != 0 || !r.Options.Submodules {
		t.Fatalf("exit %d options %+v", r.ExitCode(), r.Options)
	}
	if superproject(t, w) != before {
		t.Fatal("a gitlink, the index or the superproject HEAD changed")
	}
	// the branch-attached submodules are synced like git pull and untouched by the flag
	for _, n := range []string{"a", "b", "dev"} {
		if g := repoByName(t, r)[n]; g.Result != ResultUpToDate {
			t.Errorf("%s: %+v", n, g)
		}
	}
}

func TestSubmodulesFlagIsOffByDefault(t *testing.T) {
	w := newWorkspace(t)
	head, _ := pinAhead(t, w, "c")
	c := repoByName(t, run(t, w, Options{}))["c"]
	if c.Result != ResultSkippedDetached || w.oid(t, "c", "HEAD") != head {
		t.Fatalf("%+v", c)
	}
}

func TestSubmodulesFlagUpToDateDirtyAndDiverged(t *testing.T) {
	w := newWorkspace(t)
	if c := repoByName(t, subs(t, w, Options{}))["c"]; c.Result != ResultUpToDate {
		t.Fatalf("%+v", c)
	}
	// dirty
	w = newWorkspace(t)
	head, _ := pinAhead(t, w, "c")
	write(t, filepath.Join(w.dir("c"), "c.txt"), "dirty\n")
	r := subs(t, w, Options{})
	if c := repoByName(t, r)["c"]; c.Result != ResultSkippedDirty || w.oid(t, "c", "HEAD") != head || r.ExitCode() != 0 {
		t.Fatalf("dirty: %+v exit %d", c, r.ExitCode())
	}
	// diverged: HEAD has a commit the recorded one lacks
	w = newWorkspace(t)
	head, _ = pinAhead(t, w, "c")
	commit(t, w.dir("c"), "local.txt", "l\n")
	local := w.oid(t, "c", "HEAD")
	r = subs(t, w, Options{})
	if c := repoByName(t, r)["c"]; c.Result != ResultDiverged || w.oid(t, "c", "HEAD") != local || r.ExitCode() != 0 {
		t.Fatalf("diverged: %+v (head was %s)", c, head)
	}
	// HEAD ahead of the recorded commit is also diverged
	w = newWorkspace(t)
	runGit(t, w.dir("c"), "checkout", "-q", "main")
	commit(t, w.dir("c"), "ahead.txt", "a\n")
	runGit(t, w.dir("c"), "checkout", "-q", "--detach")
	ahead := w.oid(t, "c", "HEAD")
	if c := repoByName(t, subs(t, w, Options{}))["c"]; c.Result != ResultDiverged || w.oid(t, "c", "HEAD") != ahead {
		t.Fatalf("ahead: %+v", c)
	}
}

// A recorded commit that is not in the submodule is unreachable: no fetch, no clone, exit 1.
func TestSubmodulesFlagMissingRecordedCommitIsUnreachableAndNothingIsFetched(t *testing.T) {
	w := newWorkspace(t)
	w.advance(t, "c", "main")
	missing := runGit(t, w.root, "--git-dir="+w.bare["c"], "rev-parse", "main")
	if gitCmd(w.dir("c"), "cat-file", "-e", missing+"^{commit}").Run() == nil {
		t.Fatal("fixture: the commit is already local")
	}
	runGit(t, w.ws, "update-index", "--cacheinfo", "160000,"+missing+",c")
	runGit(t, w.dir("c"), "remote", "set-url", "origin", filepath.Join(w.root, "no-such-remote.git"))
	head := w.oid(t, "c", "HEAD")
	refs := runGit(t, w.dir("c"), "for-each-ref")
	r := subs(t, w, Options{})
	c := repoByName(t, r)["c"]
	if c.Result != ResultUnreachable || c.Reason == nil || r.ExitCode() != 1 || w.oid(t, "c", "HEAD") != head {
		t.Fatalf("%+v", c)
	}
	if gitCmd(w.dir("c"), "cat-file", "-e", missing+"^{commit}").Run() == nil || runGit(t, w.dir("c"), "for-each-ref") != refs {
		t.Fatal("something was fetched")
	}
}

func TestSubmodulesFlagUninitializedIsUnreachableAndNeverCloned(t *testing.T) {
	w := newWorkspace(t)
	clone := filepath.Join(w.root, "clone")
	runGit(t, w.root, "clone", "-q", w.bare["ws"], clone)
	o := Options{Path: clone, Submodules: true}
	r := Sync(context.Background(), o)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	for _, name := range []string{"a", "b", "c", "dev"} {
		if g := repoByName(t, r)[name]; g.Result != ResultUnreachable {
			t.Errorf("%s: %+v", name, g)
		}
		if _, err := os.Stat(filepath.Join(clone, name, ".git")); err == nil {
			t.Errorf("%s was cloned", name)
		}
	}
	if r.ExitCode() != 1 {
		t.Errorf("exit %d", r.ExitCode())
	}
	// without the flag the old result stays
	r = Sync(context.Background(), Options{Path: clone})
	if g := repoByName(t, r)["a"]; g.Result != ResultSkippedUninitialized {
		t.Fatalf("%+v", g)
	}
}

// git submodule sync rewrites the remote URL from .gitmodules: the change is reported, never fetched.
func TestSubmodulesFlagReportsAChangedRemoteURL(t *testing.T) {
	w := newWorkspace(t)
	head, recorded := pinAhead(t, w, "c")
	oldURL := runGit(t, w.dir("c"), "config", "remote.origin.url")
	newURL := w.bare["b"]
	runGit(t, w.ws, "config", "-f", ".gitmodules", "submodule.c.url", newURL)
	refs := runGit(t, w.dir("c"), "for-each-ref")
	c := repoByName(t, subs(t, w, Options{}))["c"]
	if c.Result != ResultUpdated || c.Reason == nil || !strings.Contains(*c.Reason, oldURL) || !strings.Contains(*c.Reason, newURL) {
		t.Fatalf("%+v %v", c, deref(c.Reason))
	}
	if runGit(t, w.dir("c"), "config", "remote.origin.url") != newURL || w.oid(t, "c", "HEAD") != recorded || runGit(t, w.dir("c"), "for-each-ref") != refs {
		t.Fatalf("head %s: url or refs wrong", head)
	}
}

func TestSubmodulesPlanPreviewsNothingAndRunsNoSync(t *testing.T) {
	w := newWorkspace(t)
	head, _ := pinAhead(t, w, "c")
	oldURL := runGit(t, w.dir("c"), "config", "remote.origin.url")
	runGit(t, w.ws, "config", "-f", ".gitmodules", "submodule.c.url", w.bare["b"])
	r := subs(t, w, Options{Plan: true})
	c := repoByName(t, r)["c"]
	if c.Result != ResultSkippedDetached || w.oid(t, "c", "HEAD") != head || runGit(t, w.dir("c"), "config", "remote.origin.url") != oldURL || !r.Options.Submodules {
		t.Fatalf("%+v options %+v", c, r.Options)
	}
}

// Fixed Git argument vectors: update with --no-fetch --checkout and recursion off, sync limited to a path,
// and never --init, --remote, --recursive or --force.
func TestSubmodulesFlagGitArguments(t *testing.T) {
	w := newWorkspace(t)
	pinAhead(t, w, "c")
	trace := filepath.Join(w.root, "trace")
	t.Setenv("GIT_TRACE", trace)
	subs(t, w, Options{})
	raw, _ := os.ReadFile(trace)
	var update, syncs int
	for _, l := range strings.Split(string(raw), "\n") {
		if !strings.Contains(l, "trace: exec: git-submodule") {
			continue
		}
		switch {
		case strings.Contains(l, "git-submodule update"):
			update++
			for _, want := range []string{"--no-fetch", "--checkout", " -- c"} {
				if !strings.Contains(l, want) {
					t.Errorf("update lacks %s: %s", want, l)
				}
			}
		case strings.Contains(l, "git-submodule sync"):
			syncs++
			if !strings.HasSuffix(l, "sync -- c") {
				t.Errorf("sync: %s", l)
			}
		}
		for _, bad := range []string{"--init", "--remote", "--recursive", "--force"} {
			if strings.Contains(l, bad) {
				t.Errorf("forbidden %s: %s", bad, l)
			}
		}
	}
	if update != 1 || syncs != 1 {
		t.Fatalf("update %d sync %d\n%s", update, syncs, grep(string(raw), "submodule"))
	}
}

// The submodule's own hooks run on the update (as plain git); a failing hook fails only that item.
func TestSubmodulesFlagHookFailureIsIsolated(t *testing.T) {
	w := newWorkspace(t)
	pinAhead(t, w, "c")
	marker := filepath.Join(w.root, "hook-ran")
	gitDir := runGit(t, w.dir("c"), "rev-parse", "--absolute-git-dir")
	hookScript(t, gitDir, "post-checkout", ": > '"+marker+"'\nexit 1\n")
	r := subs(t, w, Options{})
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the submodule's post-checkout hook did not run")
	}
	by := repoByName(t, r)
	if by["c"].Result != ResultFailed || r.ExitCode() != 1 || by["a"].Result != ResultUpToDate || by["b"].Result != ResultUpToDate {
		t.Fatalf("%+v", by["c"])
	}
}

// A submodule path that is a symlink is never touched.
func TestSubmodulesFlagRefusesASymlinkedPath(t *testing.T) {
	w := newWorkspace(t)
	head, _ := pinAhead(t, w, "c")
	real := filepath.Join(w.root, "c-real")
	runGit(t, w.root, "clone", "-q", w.bare["c"], real)
	runGit(t, real, "checkout", "-q", "--detach", head)
	if err := os.RemoveAll(w.dir("c")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, w.dir("c")); err != nil {
		t.Fatal(err)
	}
	c := repoByName(t, subs(t, w, Options{}))["c"]
	if c.Result != ResultUnreachable || runGit(t, real, "rev-parse", "HEAD") != head {
		t.Fatalf("%+v", c)
	}
}

func TestSubmodulePathsAreValidated(t *testing.T) {
	for p, ok := range map[string]bool{"c": true, "dir/sub": true, "-x": false, "--init": false, "../x": false, "a/../b": false, "/abs": false, "": false, "a\nb": false, "dir/-x": true} {
		if validSubmodulePath(p) != ok {
			t.Errorf("%q: want %v", p, ok)
		}
	}
}

// A nested submodule is updated after its parent moved: its recorded commit is the one the parent now has.
func TestSubmodulesFlagUpdatesANestedSubmoduleAfterItsParent(t *testing.T) {
	w := newWorkspace(t)
	nHead := w.oid(t, "a/n", "HEAD")
	runGit(t, w.dir("a/n"), "checkout", "-q", "--detach")
	w.advance(t, "a/n", "main")
	runGit(t, w.dir("a/n"), "fetch", "-q", "origin")
	nNew := runGit(t, w.dir("a/n"), "rev-parse", "origin/main")
	aHead := w.oid(t, "a", "HEAD")
	runGit(t, w.dir("a"), "update-index", "--cacheinfo", "160000,"+nNew+",n")
	runGit(t, w.dir("a"), "commit", "-q", "-m", "bump n")
	aNew := w.oid(t, "a", "HEAD")
	runGit(t, w.dir("a"), "checkout", "-q", "--detach", aHead)
	runGit(t, w.ws, "update-index", "--cacheinfo", "160000,"+aNew+",a")
	// a configured recursion (here in the parent submodule's own config, which its checkout reads) must not move the nested submodule inside the update (-c submodule.recurse=false)
	runGit(t, w.dir("a"), "config", "submodule.recurse", "true")
	r := subs(t, w, Options{})
	by := repoByName(t, r)
	if by["a"].Result != ResultUpdated || w.oid(t, "a", "HEAD") != aNew {
		t.Fatalf("a: %+v", by["a"])
	}
	n := by["a/n"]
	if n.Result != ResultUpdated || n.OldOID == nil || *n.OldOID != nHead || w.oid(t, "a/n", "HEAD") != nNew {
		t.Fatalf("a/n: %+v", n)
	}
}

// Restore undoes branch moves only: a submodule HEAD moved by --submodules stays where it is.
func TestRestoreLeavesSubmoduleMovesAlone(t *testing.T) {
	w := newWorkspace(t)
	w.advance(t, ".", "main")
	_, recorded := pinAhead(t, w, "c")
	r := subs(t, w, Options{})
	if repoByName(t, r)["c"].Result != ResultUpdated || r.RunID == nil {
		t.Fatalf("%+v", repoByName(t, r)["c"])
	}
	rr := restore(t, w, "")
	for _, it := range rr.Repos {
		if it.Name == "c" {
			t.Fatalf("restore listed the submodule: %+v", it)
		}
	}
	if w.oid(t, "c", "HEAD") != recorded {
		t.Fatal("restore moved the submodule")
	}
}

// The post-update check: a submodule that did not land on the recorded commit is failed.
func TestSubmodulesFlagChecksWhereTheSubmoduleLanded(t *testing.T) {
	defer func() { submoduleAfterHook = nil }()
	w := newWorkspace(t)
	pinAhead(t, w, "c")
	submoduleAfterHook = func(dir string) { runGit(t, dir, "checkout", "-q", "--detach", "HEAD~1") }
	if c := repoByName(t, subs(t, w, Options{}))["c"]; c.Result != ResultFailed {
		t.Fatalf("%+v", c)
	}
}

func grep(text, sub string) string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, sub) {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// submodule.NAME.update=none or rebase must not change what --submodules does: --checkout is explicit.
func TestSubmodulesFlagIgnoresTheConfiguredUpdateMode(t *testing.T) {
	for _, mode := range []string{"none", "rebase", "merge"} {
		w := newWorkspace(t)
		_, recorded := pinAhead(t, w, "c")
		runGit(t, w.ws, "config", "submodule.c.update", mode)
		if c := repoByName(t, subs(t, w, Options{}))["c"]; c.Result != ResultUpdated || w.oid(t, "c", "HEAD") != recorded {
			t.Fatalf("%s: %+v", mode, c)
		}
	}
}

// An option-like or otherwise odd path from the index is refused before any Git command sees it.
func TestSubmodulesFlagRefusesAnOptionLikeIndexPath(t *testing.T) {
	w := newWorkspace(t)
	runGit(t, w.ws, "update-index", "--add", "--cacheinfo", "160000,"+w.oid(t, "c", "HEAD")+",-evil")
	r := subs(t, w, Options{})
	g := repoByName(t, r)["-evil"]
	if g.Result != ResultUnreachable || g.Reason == nil || !strings.Contains(*g.Reason, "plain directory path") {
		t.Fatalf("%+v %v", g, deref(g.Reason))
	}
}
