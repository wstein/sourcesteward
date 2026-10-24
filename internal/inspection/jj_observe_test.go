// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// jjObserveFixture is a colocated repository whose bookmark main is the commit before the working
// copy, a bookmark feature one commit ahead of it, and a second workspace "second".
func jjObserveFixture(t *testing.T) (root, repo, second string) {
	t.Helper()
	requireJJ(t)
	r, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, repo, second = r, filepath.Join(r, "repo"), filepath.Join(r, "second")
	jjFixture(t, root, "git", "init", "--colocate", repo)
	if err := os.WriteFile(filepath.Join(repo, "file"), []byte("stored\n"), 0600); err != nil {
		t.Fatal(err)
	}
	jjFixture(t, repo, "commit", "-m", "base")
	jjFixture(t, repo, "bookmark", "create", "main", "-r", "@-")
	if err := os.WriteFile(filepath.Join(repo, "feature"), []byte("work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	jjFixture(t, repo, "commit", "-m", "feature work")
	jjFixture(t, repo, "bookmark", "create", "feature", "-r", "@-")
	jjFixture(t, repo, "workspace", "add", "--name", "second", "-r", "main", second)
	return root, repo, second
}

func TestObserveJJWorkspacesReadsRegistrationsBookmarksAndHistory(t *testing.T) {
	_, repo, second := jjObserveFixture(t)
	o, err := ObserveJJWorkspaces(context.Background(), repo, "main")
	if err != nil {
		t.Skipf("native jj sandbox unavailable: %v", err)
	}
	if o.Expected.BaseCommit == "" || len(o.Workspaces) != 2 {
		t.Fatalf("%+v", o)
	}
	ws := map[string]JJObservedWorkspace{}
	for _, w := range o.Workspaces {
		ws[w.Name] = w
	}
	if d := ws["default"]; !d.Primary || d.Path != repo || d.Missing || d.CommitID == "" {
		t.Errorf("default: %+v", d)
	}
	if s := ws["second"]; s.Primary || s.Path != second || s.Missing {
		t.Errorf("second: %+v", s)
	}
	bm := map[string]JJObservedBookmark{}
	for _, b := range o.Bookmarks {
		bm[b.Name] = b
	}
	if len(bm) != 2 || bm["main"].CommitID != o.Expected.BaseCommit {
		t.Fatalf("bookmarks: %+v", o.Bookmarks)
	}
	if h := o.History[bm["main"].CommitID]; h.Ahead != 0 || h.Author != "Test" {
		t.Errorf("main history: %+v", h)
	}
	if h := o.History[bm["feature"].CommitID]; h.Ahead != 1 || h.Author != "Test" {
		t.Errorf("feature history: %+v", h)
	}
	if h, ok := o.History[ws["second"].CommitID]; !ok || h.Ahead != 1 {
		t.Errorf("a workspace commit is an empty change on main: %+v %v", h, ok)
	}
	// A deleted workspace directory is Missing.
	if err := os.RemoveAll(second); err != nil {
		t.Fatal(err)
	}
	o, err = ObserveJJWorkspaces(context.Background(), repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range o.Workspaces {
		if w.Name == "second" && !w.Missing {
			t.Errorf("a missing directory must be reported: %+v", w)
		}
	}
}

func TestObserveJJWorkspacesWritesNothing(t *testing.T) {
	_, repo, second := jjObserveFixture(t)
	if err := os.WriteFile(filepath.Join(second, "scratch"), []byte("not snapshotted\n"), 0600); err != nil {
		t.Fatal(err)
	}
	snap := func() map[string]string {
		m := map[string]string{}
		for _, root := range []string{repo, second} {
			err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				entry := info.Mode().String() + " " + info.ModTime().String()
				if info.Mode().IsRegular() {
					b, _ := os.ReadFile(p)
					entry += " " + string(b)
				}
				m[p] = entry
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		return m
	}
	before := snap()
	if _, err := ObserveJJWorkspaces(context.Background(), repo, "main"); err != nil {
		t.Skipf("native jj sandbox unavailable: %v", err)
	}
	after := snap()
	if len(before) != len(after) {
		t.Fatalf("files appeared or vanished: %d -> %d", len(before), len(after))
	}
	for p, v := range before {
		if after[p] != v {
			t.Fatalf("changed: %s", p)
		}
	}
	// The harness can fail: a plain jj status snapshots the working copy and records an operation.
	jjFixture(t, second, "status")
	changed := snap()
	same := len(changed) == len(before)
	for p, v := range before {
		if changed[p] != v {
			same = false
		}
	}
	if same {
		t.Fatal("the snapshot comparison cannot see a jj working-copy snapshot")
	}
}

func TestObserveJJWorkspacesRejectsBadInput(t *testing.T) {
	_, repo, _ := jjObserveFixture(t)
	for _, base := range []string{"", "no-such-bookmark", strings.Repeat("x", 5000)} {
		if _, err := ObserveJJWorkspaces(context.Background(), repo, base); err == nil {
			t.Errorf("base %q accepted", base)
		}
	}
	if _, err := ObserveJJWorkspaces(context.Background(), t.TempDir(), "main"); err == nil {
		t.Error("a directory that is no jj workspace was accepted")
	}
}

// A remote bookmark is not a local one: it is not observed.
func TestObserveJJWorkspacesIgnoresRemoteBookmarks(t *testing.T) {
	_, repo, _ := jjObserveFixture(t)
	c := exec.Command("git", "-C", repo, "remote", "add", "origin", "https://example.invalid/r.git")
	if b, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%s %v", b, err)
	}
	fields := strings.Fields(jjFixture(t, repo, "log", "-r", "main", "--no-graph", "-T", "commit_id"))
	head := fields[len(fields)-1]
	c = exec.Command("git", "-C", repo, "update-ref", "refs/remotes/origin/ghost", head)
	if b, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%s %v", b, err)
	}
	if out := jjFixture(t, repo, "bookmark", "list", "--all-remotes"); !strings.Contains(out, "ghost@origin") {
		t.Fatalf("fixture: no remote bookmark: %s", out)
	}
	o, err := ObserveJJWorkspaces(context.Background(), repo, "main")
	if err != nil {
		t.Skipf("native jj sandbox unavailable: %v", err)
	}
	for _, b := range o.Bookmarks {
		if b.Name == "ghost" {
			t.Fatalf("a remote bookmark was observed as local: %+v", o.Bookmarks)
		}
	}
	if len(o.Bookmarks) != 2 {
		t.Fatalf("%+v", o.Bookmarks)
	}
}
