// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// linkedWorktrees adds linked worktrees of the root and of submodule a on their own branches.
func linkedWorktrees(t *testing.T, w *workspace) []string {
	t.Helper()
	root := filepath.Join(w.root, "linked-root")
	sub := filepath.Join(w.root, "linked-a")
	runGit(t, w.ws, "worktree", "add", "-q", "-b", "topic", root)
	runGit(t, w.dir("a"), "worktree", "add", "-q", "-b", "feat", sub)
	return []string{root, sub}
}

func trees(t *testing.T, dirs []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, d := range dirs {
		var b strings.Builder
		snap := snapshotTree(t, d)
		var keys []string
		for k := range snap {
			keys = append(keys, k)
		}
		sortStrings(keys)
		for _, k := range keys {
			b.WriteString(k + "=" + snap[k] + "\n")
		}
		out[d] = b.String() + runGit(t, d, "status", "--porcelain=v2", "--branch") + runGit(t, d, "rev-parse", "HEAD")
	}
	return out
}

func names(r Result) string {
	var n []string
	for _, rp := range r.Repos {
		n = append(n, rp.Name)
	}
	return strings.Join(n, " ")
}

// The whole workspace is found from any directory inside it, and linked worktrees are never touched.
func TestSyncFindsTheWholeWorkspaceFromAnywhereInsideIt(t *testing.T) {
	starts := map[string]func(w *workspace, linked []string) string{
		"root":              func(w *workspace, _ []string) string { return w.ws },
		"subdirectory":      func(w *workspace, _ []string) string { return filepath.Join(w.ws, "docs") },
		"submodule":         func(w *workspace, _ []string) string { return w.dir("a") },
		"nested submodule":  func(w *workspace, _ []string) string { return w.dir("a/n") },
		"develop submodule": func(w *workspace, _ []string) string { return w.dir("dev") },
		"linked root":       func(_ *workspace, l []string) string { return l[0] },
		"linked submodule":  func(_ *workspace, l []string) string { return l[1] },
	}
	for name, start := range starts {
		t.Run(name, func(t *testing.T) {
			w := newWorkspace(t)
			if err := os.MkdirAll(filepath.Join(w.ws, "docs"), 0o755); err != nil {
				t.Fatal(err)
			}
			linked := linkedWorktrees(t, w)
			w.advance(t, ".", "main")
			w.advance(t, "a", "main")
			w.advance(t, "a/n", "main")
			w.advance(t, "dev", "develop")
			w.advance(t, "c", "main")
			untouched := trees(t, linked)
			oldC := w.oid(t, "c", "HEAD")

			r := Sync(context.Background(), Options{Path: start(w, linked)})
			if r.Error != nil {
				t.Fatal(r.Error)
			}
			if got := names(r); got != ". a a/n b c dev" {
				t.Fatalf("parent first, then submodules depth-first: %q", got)
			}
			got := repoByName(t, r)
			for _, n := range []string{".", "a", "a/n", "dev"} {
				if got[n].Result != ResultFastForwarded {
					t.Errorf("%s: %+v %v", n, got[n], deref(got[n].Reason))
				}
			}
			if *got["dev"].Branch != "develop" || w.oid(t, "dev", "HEAD") != *got["dev"].NewOID {
				t.Errorf("a repository on develop syncs develop: %+v", got["dev"])
			}
			if got["b"].Result != ResultUpToDate {
				t.Errorf("b: %+v", got["b"])
			}
			if c := got["c"]; c.Result != ResultSkippedDetached || c.OldOID == nil || *c.OldOID != oldC || !strings.Contains(*c.Reason, "detached at "+oldC[:7]) || !strings.Contains(*c.Reason, "pinned") || w.oid(t, "c", "HEAD") != oldC {
				t.Errorf("c: %+v %v", c, deref(c.Reason))
			}
			after := trees(t, linked)
			for d, s := range untouched {
				if after[d] != s {
					t.Errorf("the linked worktree %s changed:\n%s\n---\n%s", d, s, after[d])
				}
			}
			for _, rp := range r.Repos {
				if strings.Contains(rp.Path, "linked-") {
					t.Errorf("a linked worktree is listed: %+v", rp)
				}
			}
			if strings.HasPrefix(name, "linked") != (r.Note != nil) {
				t.Errorf("note %v for start %s", deref(r.Note), name)
			}
			if r.Note != nil && !strings.Contains(*r.Note, "linked worktree") {
				t.Errorf("note: %s", *r.Note)
			}
			if r.ExitCode() != 0 {
				t.Errorf("exit %d", r.ExitCode())
			}
		})
	}
}

func TestSyncReportsUninitializedSubmodulesAndNeverClonesThem(t *testing.T) {
	w := newWorkspace(t)
	runGit(t, w.dir("a"), "submodule", "deinit", "-q", "-f", "n")
	runGit(t, w.ws, "submodule", "deinit", "-q", "-f", "b")
	got := repoByName(t, Sync(context.Background(), Options{Path: w.ws}))
	for _, n := range []string{"a/n", "b"} {
		g := got[n]
		if g.Result != ResultSkippedUninitialized || g.Reason == nil || *g.Reason != "not initialised" {
			t.Errorf("%s: %+v", n, g)
		}
		if _, err := os.Stat(filepath.Join(w.dir(n), ".git")); err == nil {
			t.Errorf("%s was cloned", n)
		}
	}
}

func TestSyncDeinitializedParentHidesItsNestedSubmodules(t *testing.T) {
	w := newWorkspace(t)
	runGit(t, w.ws, "submodule", "deinit", "-q", "-f", "--all")
	r := Sync(context.Background(), Options{Path: w.ws})
	if _, ok := repoByName(t, r)["a/n"]; ok || repoByName(t, r)["a"].Result != ResultSkippedUninitialized {
		t.Fatalf("%v", names(r))
	}
}

// A failure in a nested submodule does not stop the others; the exit code reflects it.
func TestSyncFailureInANestedSubmoduleIsIsolated(t *testing.T) {
	w := newWorkspace(t)
	w.advance(t, "a/n", "main")
	w.advance(t, "dev", "develop")
	hookScript(t, filepath.Join(w.ws, ".git", "modules", "a", "modules", "n"), "reference-transaction", "[ \"$1\" = prepared ] || exit 0\nwhile read o n ref; do [ \"$ref\" = refs/heads/main ] && exit 1; done\nexit 0\n")
	old := w.oid(t, "a/n", "HEAD")
	r := Sync(context.Background(), Options{Path: w.ws})
	got := repoByName(t, r)
	if got["a/n"].Result != ResultFailed || w.oid(t, "a/n", "HEAD") != old || got["dev"].Result != ResultFastForwarded || r.ExitCode() != 1 {
		t.Fatalf("%+v %+v", got["a/n"], got["dev"])
	}
}

func TestSyncSkipsABranchWithoutUpstreamNamingTheBranch(t *testing.T) {
	w := newWorkspace(t)
	runGit(t, w.dir("dev"), "branch", "--unset-upstream")
	g := repoByName(t, Sync(context.Background(), Options{Path: w.ws}))["dev"]
	if g.Result != ResultSkippedNoUpstream || g.Reason == nil || !strings.Contains(*g.Reason, "branch develop") {
		t.Fatalf("%+v %v", g, deref(g.Reason))
	}
}
