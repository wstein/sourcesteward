// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"reflect"
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
