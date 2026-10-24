// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	skipIfSandboxUnavailable(t, err)
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
	// A deleted workspace directory is Missing, where jj still reports its registered root. jj 0.45.1
	// reports no root for a workspace whose directory is gone (null): the path is then unknown,
	// and the workspace is neither Missing nor present.
	if err := os.RemoveAll(second); err != nil {
		t.Fatal(err)
	}
	reports := jjReportsRootOfMissingWorkspace(t, repo, "second")
	o, err = ObserveJJWorkspaces(context.Background(), repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range o.Workspaces {
		if w.Name != "second" {
			continue
		}
		found = true
		if reports && (!w.Missing || w.Path != second) {
			t.Errorf("a missing directory must be reported: %+v", w)
		}
		if !reports && (w.Missing || w.Path != "") {
			t.Errorf("without a root from jj the path is unknown, not missing: %+v", w)
		}
	}
	if !found {
		t.Error("the workspace of the deleted directory is still registered and must be listed")
	}
}

// jjReportsRootOfMissingWorkspace asks the same jj binary whether the root of a workspace whose
// directory was deleted is still reported.
func jjReportsRootOfMissingWorkspace(t *testing.T, repo, name string) bool {
	t.Helper()
	out := jjFixture(t, repo, "workspace", "list", "--template", `json(self.name()) ++ " " ++ json(self.root()) ++ "\n"`)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, strconv.Quote(name)+" ") {
			return !strings.HasSuffix(line, " null")
		}
	}
	t.Fatalf("workspace %s is not listed: %s", name, out)
	return false
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
	skipIfSandboxUnavailable(t, err)
	for _, b := range o.Bookmarks {
		if b.Name == "ghost" {
			t.Fatalf("a remote bookmark was observed as local: %+v", o.Bookmarks)
		}
	}
	if len(o.Bookmarks) != 2 {
		t.Fatalf("%+v", o.Bookmarks)
	}
}

// skipIfSandboxUnavailable skips only when the native jj sandbox is genuinely unavailable and
// STW_REQUIRE_JJ is unset; every other error fails the test.
func skipIfSandboxUnavailable(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var setup *jjSessionError
	if errors.As(err, &setup) && setup.code == "jj-sandbox-unavailable" && os.Getenv("STW_REQUIRE_JJ") != "1" {
		t.Skipf("native jj sandbox unavailable: %v", err)
	}
	t.Fatalf("observation failed: %v", err)
}

// A different jj repository at a registered path is no workspace of this one: it is Foreign and
// never Primary, although its .jj/repo is a directory.
func TestObserveJJWorkspacesMarksAForeignRepositoryAtARegisteredPath(t *testing.T) {
	root, repo, second := jjObserveFixture(t)
	if err := os.RemoveAll(second); err != nil {
		t.Fatal(err)
	}
	jjFixture(t, root, "git", "init", second)
	o, err := ObserveJJWorkspaces(context.Background(), repo, "main")
	skipIfSandboxUnavailable(t, err)
	for _, w := range o.Workspaces {
		switch w.Name {
		case "second":
			if !w.Foreign || w.Primary || w.Missing {
				t.Errorf("foreign repository: %+v", w)
			}
		case "default":
			if w.Foreign || !w.Primary {
				t.Errorf("default: %+v", w)
			}
		}
	}
}

func TestObserveJJWorkspacesHistoryFailureAndLimit(t *testing.T) {
	_, repo, _ := jjObserveFixture(t)
	t.Run("a failing history read leaves the commit out", func(t *testing.T) {
		defer func(f func(jjSession, string, string, string, string) (JJHistory, error)) { jjHistoryRead = f }(jjHistoryRead)
		jjHistoryRead = func(jjSession, string, string, string, string) (JJHistory, error) {
			return JJHistory{}, errors.New("fixture failure")
		}
		o, err := ObserveJJWorkspaces(context.Background(), repo, "main")
		skipIfSandboxUnavailable(t, err)
		if len(o.History) != 0 || len(o.Workspaces) != 2 || len(o.Bookmarks) != 2 {
			t.Fatalf("%+v", o)
		}
	})
	t.Run("the history limit keeps the workspaces first", func(t *testing.T) {
		defer func(n int) { jjHistoryLimit = n }(jjHistoryLimit)
		jjHistoryLimit = 2
		o, err := ObserveJJWorkspaces(context.Background(), repo, "main")
		skipIfSandboxUnavailable(t, err)
		if len(o.History) != 2 {
			t.Fatalf("history: %+v", o.History)
		}
		for _, w := range o.Workspaces {
			if _, ok := o.History[w.CommitID]; !ok {
				t.Errorf("a workspace commit lost its history to a bookmark: %+v", w)
			}
		}
	})
}
