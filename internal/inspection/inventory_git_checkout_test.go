// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// superWithSubmodule builds a superproject with one initialized submodule at mods/lib.
func superWithSubmodule(t *testing.T) (super, sub string) {
	t.Helper()
	root := canonical(t, t.TempDir())
	src := filepath.Join(root, "lib-src")
	os.Mkdir(src, 0o755)
	git(t, src, "init", "-q", "-b", "main")
	for _, n := range []string{"a", "b"} {
		os.WriteFile(filepath.Join(src, n), []byte(n+"\n"), 0o644)
	}
	git(t, src, "add", "-A")
	git(t, src, "commit", "-qm", "lib")
	super = filepath.Join(root, "super")
	os.Mkdir(super, 0o755)
	git(t, super, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(super, "s"), []byte("s\n"), 0o644)
	git(t, super, "add", "-A")
	git(t, super, "commit", "-qm", "super")
	git(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", src, "mods/lib")
	git(t, super, "commit", "-qm", "submodule")
	return super, filepath.Join(super, "mods", "lib")
}

type streamCall struct {
	dir  string
	args []string
}

// recordingInventory runs the Git adapter and records every buffered and streamed invocation.
func recordingInventory(t *testing.T, d string, o InventoryOptions) (InventoryResult, []streamCall) {
	t.Helper()
	if len(o.Scopes) == 0 && o.Query != "ref-exists" && o.Query != "ancestor" {
		o.Scopes = []string{"disk", "tracked", "untracked"}
	}
	var mu sync.Mutex
	var calls []streamCall
	b := defaultInventoryBounds()
	real := newStreamGit(b)
	run := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		mu.Lock()
		calls = append(calls, streamCall{dir, args})
		mu.Unlock()
		return runGit(ctx, dir, args...)
	}
	stream := func(ctx context.Context, dir string, in io.Reader, delim string, args []string, each func([]byte) error) (int, bool, error) {
		mu.Lock()
		calls = append(calls, streamCall{dir, args})
		mu.Unlock()
		return real(ctx, dir, in, delim, args, each)
	}
	id := Inspect(context.Background(), d)
	if id.Status != "ok" {
		t.Fatalf("identity %+v", id)
	}
	return inventoryGitBounded(context.Background(), id, o, run, stream, b), calls
}

func callsIn(calls []streamCall, dir string, commands ...string) []streamCall {
	var out []streamCall
	for _, c := range calls {
		if c.dir != dir {
			continue
		}
		for _, a := range c.args {
			for _, want := range commands {
				if a == want {
					out = append(out, c)
				}
			}
		}
	}
	return out
}

func TestGitInventorySubmoduleCheckoutIsDescribedByItsFiles(t *testing.T) {
	isolatedHome(t)
	_, sub := superWithSubmodule(t)
	r := gitInventory(t, sub, InventoryOptions{})
	if r.Status != "ok" || len(r.Workspaces) != 1 {
		t.Fatalf("%s %+v", r.Status, r.Diagnostics)
	}
	w := r.Workspaces[0]
	if w.Path == nil || *w.Path != sub || w.Name != sub || w.Tracked.Value != "false" || w.Untracked.Value != "false" || w.Conflicts.Value != "false" || len(r.Files) != 0 {
		t.Fatalf("clean submodule must be clean: %+v", w)
	}
	for _, q := range []string{"clean", "dirty"} {
		res := gitInventory(t, sub, InventoryOptions{Query: q})
		want := "true"
		if q == "dirty" {
			want = "false"
		}
		if res.Status != "ok" || res.Query.Value != want {
			t.Fatalf("%s %+v", q, res.Query)
		}
	}
	os.WriteFile(filepath.Join(sub, "a"), []byte("changed\n"), 0o644)
	os.WriteFile(filepath.Join(sub, "new.txt"), []byte("new\n"), 0o644)
	r = gitInventory(t, sub, InventoryOptions{})
	got := []string{}
	for _, f := range r.Files {
		got = append(got, f.Scope+":"+f.Path)
	}
	if strings.Join(got, ",") != "disk:a,tracked:a,untracked:new.txt" || r.Workspaces[0].Tracked.Value != "true" || r.Workspaces[0].Untracked.Value != "true" {
		t.Fatalf("%v %+v", got, r.Workspaces[0])
	}
	if q := gitInventory(t, sub, InventoryOptions{Query: "dirty"}); q.Status != "ok" || q.Query.Value != "true" {
		t.Fatalf("%+v", q.Query)
	}
}

func separateGitDirRepo(t *testing.T) (work, gitDir string) {
	t.Helper()
	root := canonical(t, t.TempDir())
	work, gitDir = filepath.Join(root, "work"), filepath.Join(root, "gitdir")
	git(t, root, "init", "-q", "-b", "main", "--separate-git-dir", gitDir, work)
	os.MkdirAll(filepath.Join(work, "sub"), 0o755)
	os.WriteFile(filepath.Join(work, "sub", "f"), []byte("f\n"), 0o644)
	git(t, work, "add", "-A")
	git(t, work, "commit", "-qm", "init")
	return work, gitDir
}

func TestGitInventorySeparateGitDirCheckoutFromTheTopAndASubdirectory(t *testing.T) {
	isolatedHome(t)
	work, _ := separateGitDirRepo(t)
	os.WriteFile(filepath.Join(work, "sub", "f"), []byte("edited\n"), 0o644)
	for _, from := range []string{work, filepath.Join(work, "sub")} {
		r := gitInventory(t, from, InventoryOptions{})
		if r.Status != "ok" || len(r.Workspaces) != 1 || *r.Workspaces[0].Path != work || r.Workspaces[0].Tracked.Value != "true" || len(r.Files) != 2 {
			t.Fatalf("from %s: %s %+v %+v", from, r.Status, r.Workspaces, r.Files)
		}
	}
}

func TestGitInventoryLinkedWorktreeOfASubmoduleResolvesTheMainCheckout(t *testing.T) {
	isolatedHome(t)
	_, sub := superWithSubmodule(t)
	linked := filepath.Join(canonical(t, t.TempDir()), "linked-sub")
	git(t, sub, "worktree", "add", "-qb", "sl", linked)
	for _, from := range []string{sub, linked} {
		r := gitInventory(t, from, InventoryOptions{})
		paths := []string{}
		for _, w := range r.Workspaces {
			paths = append(paths, *w.Path)
			if w.Tracked.Value != "false" {
				t.Fatalf("from %s: %+v", from, w)
			}
		}
		if r.Status != "ok" || len(paths) != 2 || !hasString(paths, sub) || !hasString(paths, linked) {
			t.Fatalf("from %s: %s %v", from, r.Status, paths)
		}
	}
}

func TestGitInventorySeparateGitDirLinkedWorktreeQualifiesTheMainEntry(t *testing.T) {
	isolatedHome(t)
	work, gitDir := separateGitDirRepo(t)
	linked := filepath.Join(canonical(t, t.TempDir()), "linked")
	git(t, work, "worktree", "add", "-qb", "side", linked)
	r, calls := recordingInventory(t, linked, InventoryOptions{})
	var main InventoryWorkspace
	for _, w := range r.Workspaces {
		if w.Path != nil && *w.Path == gitDir {
			main = w
		}
	}
	if r.Status != "partial" || main.Path == nil || main.Registration != "present" || main.Tracked.Reason != txtCheckoutMissing || main.Untracked.Value != "unknown" || main.Conflicts.Value != "unknown" || !hasString(main.Limitations, txtCheckoutMissing) || !hasLimitation(r, txtCheckoutMissing) {
		t.Fatalf("%s %+v %q", r.Status, main, r.Boundary.Limitations)
	}
	if bad := callsIn(calls, gitDir, "ls-files", "diff", "check-attr", "for-each-ref"); len(bad) != 0 {
		t.Fatalf("content commands ran against the Git directory: %+v", bad)
	}
	// A query run from the linked worktree is unaffected by the qualified main entry.
	q := gitInventory(t, linked, InventoryOptions{Query: "clean"})
	if q.Status != "ok" || q.Query.Value != "true" {
		t.Fatalf("%s %+v", q.Status, q.Query)
	}
	// The same layout run from the main checkout resolves it (CK-1).
	r = gitInventory(t, work, InventoryOptions{})
	if r.Status != "ok" {
		t.Fatalf("%s %+v %q", r.Status, r.Workspaces, r.Boundary.Limitations)
	}
}

func TestGitInventoryHostileCoreWorktreeIsNeverDescribed(t *testing.T) {
	isolatedHome(t)
	work, gitDir := separateGitDirRepo(t)
	other := repo(t)
	git(t, other, "commit", "--allow-empty", "-qm", "other")
	other = canonical(t, other)
	linked := filepath.Join(canonical(t, t.TempDir()), "linked")
	git(t, work, "worktree", "add", "-qb", "side", linked)
	for _, hostile := range []string{other, filepath.Join(other, "missing")} {
		git(t, work, "--git-dir", gitDir, "config", "core.worktree", hostile)
		r, calls := recordingInventory(t, linked, InventoryOptions{})
		for _, w := range r.Workspaces {
			if w.Path != nil && (*w.Path == hostile || *w.Path == other) {
				t.Fatalf("hostile worktree described: %+v", w)
			}
		}
		if r.Status != "partial" || !hasLimitation(r, txtCheckoutMissing) {
			t.Fatalf("%s %q", r.Status, r.Boundary.Limitations)
		}
		if bad := callsIn(calls, other, "ls-files", "diff", "check-attr"); len(bad) != 0 {
			t.Fatalf("content commands ran in the hostile target: %+v", bad)
		}
	}
}

func TestGitInventoryBareStoredQueryIsStillKnown(t *testing.T) {
	isolatedHome(t)
	src := manyFilesRepo(t, 3, "")
	bare := filepath.Join(canonical(t, t.TempDir()), "bare.git")
	git(t, src, "clone", "-q", "--bare", src, bare)
	q := gitInventory(t, bare, InventoryOptions{Query: "clean", Scopes: []string{"stored"}})
	if q.Status != "ok" || q.Query.Value == "unknown" {
		t.Fatalf("%s %+v", q.Status, q.Query)
	}
}

func TestGitInventoryGitlinkSuperprojectRules(t *testing.T) {
	isolatedHome(t)
	super, sub := superWithSubmodule(t)
	t.Run("clean is unknown and partial", func(t *testing.T) {
		r := gitInventory(t, super, InventoryOptions{})
		w := r.Workspaces[0]
		if r.Status != "partial" || w.Tracked.Value != "unknown" || w.Tracked.Reason != txtGitlink || !hasLimitation(r, txtGitlink) {
			t.Fatalf("%s %+v", r.Status, w)
		}
		for _, q := range []string{"clean", "dirty"} {
			res := gitInventory(t, super, InventoryOptions{Query: q})
			if res.Status != "partial" || res.Query.Value != "unknown" {
				t.Fatalf("%s %s %+v", q, res.Status, res.Query)
			}
		}
	})
	t.Run("modified ordinary file", func(t *testing.T) {
		os.WriteFile(filepath.Join(super, "s"), []byte("edited\n"), 0o644)
		defer git(t, super, "checkout", "--", "s")
		r := gitInventory(t, super, InventoryOptions{})
		w := r.Workspaces[0]
		if r.Status != "partial" || r.Boundary.Complete || w.Tracked.Value != "true" || !hasString(w.Limitations, txtGitlinkTrue) || !hasLimitation(r, txtGitlink) || len(r.Files) != 2 {
			t.Fatalf("%s %+v %+v", r.Status, w, r.Files)
		}
		d := gitInventory(t, super, InventoryOptions{Query: "dirty"})
		if d.Status != "ok" || !d.Boundary.Complete || d.Query.Value != "true" {
			t.Fatalf("%s %+v", d.Status, d.Query)
		}
		c := gitInventory(t, super, InventoryOptions{Query: "clean"})
		if c.Status != "ok" || c.Query.Value != "false" {
			t.Fatalf("%s %+v", c.Status, c.Query)
		}
	})
	t.Run("staged ordinary file", func(t *testing.T) {
		os.WriteFile(filepath.Join(super, "s"), []byte("staged\n"), 0o644)
		git(t, super, "add", "s")
		defer func() { git(t, super, "reset", "-q", "HEAD", "s"); git(t, super, "checkout", "--", "s") }()
		r := gitInventory(t, super, InventoryOptions{Scopes: []string{"staged"}})
		if r.Workspaces[0].Tracked.Value != "true" {
			t.Fatalf("%+v", r.Workspaces[0])
		}
	})
	t.Run("staged gitlink bump", func(t *testing.T) {
		git(t, sub, "-c", "user.name=T", "-c", "user.email=t@example.org", "commit", "--allow-empty", "-qm", "bump")
		git(t, super, "add", "mods/lib")
		defer func() {
			git(t, super, "reset", "-q", "HEAD", "mods/lib")
			git(t, sub, "reset", "-q", "--hard", "HEAD~1")
		}()
		r := gitInventory(t, super, InventoryOptions{})
		if r.Workspaces[0].Tracked.Value != "true" {
			t.Fatalf("%+v", r.Workspaces[0])
		}
		found := false
		for _, f := range r.Files {
			found = found || f.Path == "mods/lib" && f.Scope == "tracked" && f.Change == "modified"
		}
		if !found {
			t.Fatalf("%+v", r.Files)
		}
	})
	t.Run("unstaged gitlink move stays unknown", func(t *testing.T) {
		git(t, sub, "-c", "user.name=T", "-c", "user.email=t@example.org", "commit", "--allow-empty", "-qm", "move")
		defer git(t, sub, "reset", "-q", "--hard", "HEAD~1")
		r := gitInventory(t, super, InventoryOptions{})
		if r.Status != "partial" || r.Workspaces[0].Tracked.Value != "unknown" {
			t.Fatalf("%s %+v", r.Status, r.Workspaces[0])
		}
	})
	t.Run("filters and scopes", func(t *testing.T) {
		q := gitInventory(t, super, InventoryOptions{Query: "clean", Scopes: []string{"untracked"}})
		if q.Status != "ok" || q.Query.Value != "true" {
			t.Fatalf("%s %+v", q.Status, q.Query)
		}
		q = gitInventory(t, super, InventoryOptions{Query: "clean", Paths: []string{"s"}})
		if q.Status != "ok" || q.Query.Value != "true" {
			t.Fatalf("a gitlink outside the path filter must not block the predicate: %s %+v", q.Status, q.Query)
		}
	})
	t.Run("transformation with a gitlink stays unknown", func(t *testing.T) {
		os.WriteFile(filepath.Join(super, ".gitattributes"), []byte("s text eol=crlf\n"), 0o644)
		os.WriteFile(filepath.Join(super, "s"), []byte("edited\n"), 0o644)
		defer func() { os.Remove(filepath.Join(super, ".gitattributes")); git(t, super, "checkout", "--", "s") }()
		r := gitInventory(t, super, InventoryOptions{Query: "dirty"})
		if r.Query.Value != "unknown" {
			t.Fatalf("%s %+v", r.Status, r.Query)
		}
	})
	t.Run("oversize file with a gitlink stays unknown", func(t *testing.T) {
		os.WriteFile(filepath.Join(super, "s"), []byte(strings.Repeat("x", 100)), 0o644)
		defer git(t, super, "checkout", "--", "s")
		b := defaultInventoryBounds()
		b.FileBytes = 10
		r := gitInventoryWith(t, super, InventoryOptions{Query: "dirty"}, b)
		if r.Query.Value != "unknown" {
			t.Fatalf("%s %+v", r.Status, r.Query)
		}
	})
	t.Run("linked worktree of the superproject", func(t *testing.T) {
		linked := filepath.Join(canonical(t, t.TempDir()), "super-wt")
		git(t, super, "worktree", "add", "-qb", "wt", linked)
		defer func() { git(t, super, "worktree", "remove", "--force", linked) }()
		os.WriteFile(filepath.Join(super, "s"), []byte("edited\n"), 0o644)
		defer git(t, super, "checkout", "--", "s")
		r := gitInventory(t, super, InventoryOptions{})
		for _, w := range r.Workspaces {
			switch *w.Path {
			case super:
				if w.Tracked.Value != "true" {
					t.Fatalf("%+v", w)
				}
			case linked:
				if w.Tracked.Value != "unknown" || w.Tracked.Reason != txtGitlink {
					t.Fatalf("%+v", w)
				}
			}
		}
	})
}

func TestGitInventoryNeverOpensASubmoduleWorktree(t *testing.T) {
	isolatedHome(t)
	super, sub := superWithSubmodule(t)
	sentinel := filepath.Join(t.TempDir(), "ran")
	hook := filepath.Join(sub, ".git")
	_ = hook
	subGit := strings.TrimSpace(git(t, sub, "rev-parse", "--absolute-git-dir"))
	git(t, sub, "config", "core.fsmonitor", "!touch '"+sentinel+"'")
	git(t, sub, "config", "filter.x.clean", "!touch '"+sentinel+"'; cat")
	os.MkdirAll(filepath.Join(subGit, "hooks"), 0o755)
	os.WriteFile(filepath.Join(subGit, "hooks", "post-index-change"), []byte("#!/bin/sh\ntouch '"+sentinel+"'\n"), 0o755)
	os.WriteFile(filepath.Join(sub, "a"), []byte("dirty inside the submodule\n"), 0o644)
	os.WriteFile(filepath.Join(super, "s"), []byte("edited\n"), 0o644)
	r, calls := recordingInventory(t, super, InventoryOptions{Scopes: []string{"disk", "tracked", "untracked", "staged"}})
	if r.Status == "error" {
		t.Fatalf("%+v", r.Diagnostics)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("helper ran: %v", err)
	}
	for _, c := range calls {
		if strings.HasPrefix(c.dir, sub) || strings.HasPrefix(c.dir, subGit) {
			t.Fatalf("a process ran in the submodule: %+v", c)
		}
	}
}
