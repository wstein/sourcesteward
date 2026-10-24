// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// LocalRefs reads the local branches, the remote-tracking references, the remotes they imply and
// the target of each symbolic refs/remotes/<remote>/HEAD, read only and offline.
func TestLocalRefsReadsBranchesRemotesAndSymbolicHeads(t *testing.T) {
	w := newWorkspace(t)
	got, err := LocalRefs(context.Background(), w.dir("."))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Refs["refs/heads/main"] || !got.Refs["refs/remotes/origin/main"] || !got.Refs["refs/remotes/origin/HEAD"] {
		t.Errorf("refs %v", got.Refs)
	}
	if !reflect.DeepEqual(got.Remotes, []string{"origin"}) {
		t.Errorf("remotes %v", got.Remotes)
	}
	if got.Heads["origin"] != "refs/remotes/origin/main" {
		t.Errorf("heads %v", got.Heads)
	}
	// A second remote without a symbolic HEAD, and a branch that is not the remote's default.
	runGit(t, w.dir("dev"), "remote", "add", "extra", w.bare["a"])
	runGit(t, w.dir("dev"), "fetch", "-q", "extra")
	runGit(t, w.dir("dev"), "remote", "set-head", "extra", "--delete")
	runGit(t, w.dir("dev"), "branch", "feature/x")
	got, err = LocalRefs(context.Background(), w.dir("dev"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Remotes, []string{"extra", "origin"}) || got.Heads["origin"] != "refs/remotes/origin/main" || got.Heads["extra"] != "" || !got.Refs["refs/heads/feature/x"] {
		t.Errorf("remotes %v heads %v", got.Remotes, got.Heads)
	}
}

// Tags and other namespaces are not read, and a path that is not a repository is an error.
func TestLocalRefsIsBoundedToBranchesAndRemotes(t *testing.T) {
	w := newWorkspace(t)
	runGit(t, w.dir("."), "tag", "v1")
	runGit(t, w.dir("."), "update-ref", "refs/stash-like", "HEAD")
	got, err := LocalRefs(context.Background(), w.dir("."))
	if err != nil {
		t.Fatal(err)
	}
	for ref := range got.Refs {
		if ref == "refs/tags/v1" || ref == "refs/stash-like" {
			t.Errorf("unexpected ref %s", ref)
		}
	}
	if _, err := LocalRefs(context.Background(), t.TempDir()); err == nil {
		t.Error("a directory that is not a repository must fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LocalRefs(ctx, w.dir(".")); err == nil {
		t.Error("a cancelled context must fail")
	}
}

// The remotes are the configured ones (git remote), not the ones that happen to have
// remote-tracking references: a remote that was never fetched is still a remote, a remote name may
// contain a slash, and the symbolic HEAD is looked up per configured remote.
func TestLocalRefsReadsTheConfiguredRemotes(t *testing.T) {
	w := newWorkspace(t)
	dir := w.dir("dev")
	runGit(t, dir, "remote", "add", "team/up", w.bare["a"])
	runGit(t, dir, "fetch", "-q", "team/up")
	runGit(t, dir, "remote", "set-head", "team/up", "main")
	runGit(t, dir, "remote", "add", "never", w.bare["b"]) // configured, never fetched
	got, err := LocalRefs(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Remotes, []string{"never", "origin", "team/up"}) {
		t.Errorf("remotes %v", got.Remotes)
	}
	if got.Heads["team/up"] != "refs/remotes/team/up/main" || got.Heads["never"] != "" || got.Heads["origin"] != "refs/remotes/origin/main" {
		t.Errorf("heads %v", got.Heads)
	}
	if _, ok := got.Heads["never"]; !ok {
		t.Errorf("every configured remote has an entry: %v", got.Heads)
	}
	if got.Refs["refs/remotes/never/HEAD"] {
		t.Error("a never-fetched remote has no references")
	}
	// Only never-fetched remotes: still all of them.
	runGit(t, w.dir("b"), "remote", "rename", "origin", "gone")
	runGit(t, w.dir("b"), "update-ref", "-d", "refs/remotes/gone/main")
	runGit(t, w.dir("b"), "update-ref", "-d", "refs/remotes/gone/HEAD")
	runGit(t, w.dir("b"), "remote", "add", "fresh", w.bare["a"])
	got, err = LocalRefs(context.Background(), w.dir("b"))
	if err != nil || !reflect.DeepEqual(got.Remotes, []string{"fresh", "gone"}) || got.Heads["fresh"] != "" || got.Heads["gone"] != "" {
		t.Errorf("%v %+v", err, got)
	}
}

func TestLocalRefsWithoutRemotes(t *testing.T) {
	w := newWorkspace(t)
	runGit(t, w.dir("."), "remote", "remove", "origin")
	got, err := LocalRefs(context.Background(), w.dir("."))
	if err != nil || got.Remotes == nil || len(got.Remotes) != 0 || len(got.Heads) != 0 || !got.Refs["refs/heads/main"] {
		t.Errorf("%v %+v", err, got)
	}
}

// The number of references is bounded: more than maxRefs is an error, never a partial answer.
func TestLocalRefsIsBoundedByMaxRefs(t *testing.T) {
	w := newWorkspace(t)
	old := maxRefs
	defer func() { maxRefs = old }()
	maxRefs = 3
	for _, b := range []string{"x1", "x2", "x3", "x4"} {
		runGit(t, w.dir("."), "branch", b)
	}
	if _, err := LocalRefs(context.Background(), w.dir(".")); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Errorf("%v", err)
	}
	maxRefs = 100000
	if _, err := LocalRefs(context.Background(), w.dir(".")); err != nil {
		t.Errorf("%v", err)
	}
}
