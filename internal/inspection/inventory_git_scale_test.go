// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitInventoryWith runs the Git adapter with explicit bounds so limit rows need no large repository.
func gitInventoryWith(t *testing.T, d string, o InventoryOptions, b inventoryBounds) InventoryResult {
	t.Helper()
	if len(o.Scopes) == 0 && o.Query != "ref-exists" && o.Query != "ancestor" {
		o.Scopes = []string{"disk", "tracked", "untracked"}
	}
	id := Inspect(context.Background(), d)
	if id.Status != "ok" {
		t.Fatalf("identity: %+v", id)
	}
	return inventoryGitBounded(context.Background(), id, o, runGit, newStreamGit(b), b)
}

func hasLimitation(r InventoryResult, want string) bool {
	for _, l := range r.Boundary.Limitations {
		if l == want {
			return true
		}
	}
	return false
}

func truncatedText(n int) string {
	return fmt.Sprintf("file listing truncated: %d file records omitted by the 1,024-record bound; omitted paths are not clean", n)
}

func TestGitInventoryScaleCleanRepositories(t *testing.T) {
	isolatedHome(t)
	long := strings.Repeat("s", 50) + "/" + strings.Repeat("t", 50) + "/" + strings.Repeat("u", 50) + "/"
	for _, tc := range []struct {
		name   string
		n      int
		prefix string
	}{{"150", 150, ""}, {"400", 400, ""}, {"1030", 1030, ""}, {"5000", 5000, ""}, {"long1030", 1030, long}} {
		t.Run(tc.name, func(t *testing.T) {
			d := manyFilesRepo(t, tc.n, tc.prefix)
			start := time.Now()
			r := gitInventory(t, d, InventoryOptions{})
			if r.Status != "ok" || !r.Boundary.Complete || len(r.Files) != 0 || len(r.Workspaces) != 1 || r.Workspaces[0].Tracked.Value != "false" || r.Workspaces[0].Untracked.Value != "false" || r.Workspaces[0].Conflicts.Value != "false" {
				t.Fatalf("%+v", r)
			}
			if time.Since(start) > 9*time.Second {
				t.Fatalf("too slow: %v", time.Since(start))
			}
		})
	}
}

func TestGitInventoryScaleChangedAndUntracked(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 5000, "")
	if err := os.WriteFile(filepath.Join(d, "d000", "file00001.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(d, "untracked.txt"), []byte("new\n"), 0o644)
	r := gitInventory(t, d, InventoryOptions{})
	got := []string{}
	for _, f := range r.Files {
		got = append(got, f.Scope+":"+f.Path+":"+f.Change)
	}
	want := []string{"disk:d000/file00001.txt:modified", "tracked:d000/file00001.txt:modified", "untracked:untracked.txt:untracked"}
	if r.Status != "ok" || strings.Join(got, ",") != strings.Join(want, ",") || r.Workspaces[0].Tracked.Value != "true" || r.Workspaces[0].Untracked.Value != "true" {
		t.Fatalf("%v %+v", got, r)
	}
	q := gitInventory(t, d, InventoryOptions{Query: "dirty"})
	if q.Status != "ok" || q.Query.Value != "true" {
		t.Fatalf("%+v", q)
	}
}

func TestGitInventoryScaleStoredRootCommitIsTruncated(t *testing.T) {
	isolatedHome(t)
	for _, n := range []int{1030, 5000} {
		d := manyFilesRepo(t, n, "")
		r := gitInventory(t, d, InventoryOptions{Scopes: []string{"stored"}})
		cost := len(r.Workspaces) + len(r.Bases)
		for _, ref := range r.References {
			cost += 1 + len(ref.AddedTargets) + len(ref.RemovedTargets) + len(ref.Upstreams)
		}
		if r.Status != "partial" || r.Boundary.Complete || len(r.Files) == 0 || len(r.Files)+cost > inventoryRecordLimit {
			t.Fatalf("n=%d status=%s files=%d cost=%d diag=%+v", n, r.Status, len(r.Files), cost, r.Diagnostics)
		}
		if !hasLimitation(r, truncatedText(n-len(r.Files))) {
			t.Fatalf("n=%d limitations %q files=%d", n, r.Boundary.Limitations, len(r.Files))
		}
		if r.Files[0].Path != "d000/file00000.txt" || r.Workspaces[0].Tracked.Value != "unknown" || r.Workspaces[0].Tracked.Reason != "raw disk comparison not requested" {
			t.Fatalf("%+v %+v", r.Files[0], r.Workspaces[0])
		}
	}
}

func TestGitInventoryScaleRealShapedRepository(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 5000, "")
	for i, size := range []int{100 << 10, 200 << 10, 500 << 10, 1 << 20, 2 << 20} {
		if err := os.WriteFile(filepath.Join(d, fmt.Sprintf("asset%d.bin", i)), []byte(strings.Repeat(fmt.Sprintf("%08d", i), size/8)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, d, "add", "-A")
	git(t, d, "commit", "-qm", "assets")
	for i := 0; i < 200; i++ {
		git(t, d, "branch", fmt.Sprintf("topic-%03d", i))
	}
	var tags strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&tags, "create refs/tags/release-%03d HEAD\n", i)
	}
	cmd := execGit(t, d, tags.String(), "update-ref", "--stdin")
	_ = cmd
	r := gitInventory(t, d, InventoryOptions{})
	if r.Status != "ok" || !r.Boundary.Complete || r.Workspaces[0].Tracked.Value != "false" || len(r.Files) != 0 || len(r.References) != 401 {
		t.Fatalf("%s %v %+v refs=%d", r.Status, r.Boundary.Limitations, r.Workspaces[0].Tracked, len(r.References))
	}
	os.WriteFile(filepath.Join(d, "asset2.bin"), []byte("edited"), 0o644)
	r = gitInventory(t, d, InventoryOptions{Query: "clean"})
	if r.Status != "ok" || r.Query.Value != "false" {
		t.Fatalf("%+v", r)
	}
}

func TestGitInventoryScaleTransformationQualifiers(t *testing.T) {
	isolatedHome(t)
	for _, attr := range []string{"text", "eol=crlf", "ident", "working-tree-encoding=UTF-16", "filter=lfsish"} {
		t.Run(attr, func(t *testing.T) {
			d := manyFilesRepo(t, 1030, "")
			git(t, d, "config", "filter.lfsish.clean", "cat")
			os.WriteFile(filepath.Join(d, ".gitattributes"), []byte("d020/file01029.txt "+attr+"\n"), 0o644)
			r := gitInventory(t, d, InventoryOptions{})
			w := r.Workspaces[0]
			if r.Status != "partial" || w.Tracked.Value != "unknown" || !strings.Contains(w.Tracked.Reason, "transformations") {
				t.Fatalf("%s %+v", r.Status, w)
			}
		})
	}
	t.Run("autocrlf", func(t *testing.T) {
		d := manyFilesRepo(t, 1030, "")
		git(t, d, "config", "core.autocrlf", "true")
		r := gitInventory(t, d, InventoryOptions{})
		if r.Status != "partial" || r.Workspaces[0].Tracked.Value != "unknown" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("unrelated attribute is not a transformation", func(t *testing.T) {
		d := manyFilesRepo(t, 1030, "")
		os.WriteFile(filepath.Join(d, ".gitattributes"), []byte("*.txt linguist-generated\n"), 0o644)
		r := gitInventory(t, d, InventoryOptions{})
		if r.Workspaces[0].Tracked.Value == "unknown" && strings.Contains(r.Workspaces[0].Tracked.Reason, "transformations") {
			t.Fatalf("%+v", r.Workspaces[0])
		}
	})
}

func TestGitInventoryScaleUnbornHeadWithStagedFiles(t *testing.T) {
	isolatedHome(t)
	d := repo(t)
	for i := 0; i < 1500; i++ {
		os.WriteFile(filepath.Join(d, fmt.Sprintf("f%05d", i)), []byte("x"), 0o644)
	}
	git(t, d, "add", "-A")
	r := gitInventory(t, d, InventoryOptions{Scopes: []string{"staged"}})
	if r.Status != "partial" || len(r.Files) == 0 || len(r.Files) > inventoryRecordLimit || !hasLimitation(r, truncatedText(1500-len(r.Files))) {
		t.Fatalf("%s files=%d %q diag=%+v", r.Status, len(r.Files), r.Boundary.Limitations, r.Diagnostics)
	}
	q := gitInventory(t, d, InventoryOptions{Query: "dirty", Scopes: []string{"staged"}})
	if q.Status != "ok" || !q.Boundary.Complete || q.Query.Value != "true" || !hasLimitation(q, truncatedText(1500-len(q.Files))) {
		t.Fatalf("%+v", q)
	}
}

func TestGitInventoryScaleHostileHelpersDoNotRunAndNothingChanges(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 1030, "")
	sentinel := filepath.Join(t.TempDir(), "helper-ran")
	helper := "!touch '" + sentinel + "'; cat"
	os.WriteFile(filepath.Join(d, ".gitattributes"), []byte("d000/** filter=hostile diff=hostile\n"), 0o644)
	git(t, d, "add", ".gitattributes")
	git(t, d, "commit", "-qm", "attrs")
	for _, kv := range [][2]string{{"filter.hostile.clean", helper}, {"filter.hostile.smudge", helper}, {"filter.hostile.process", helper}, {"filter.hostile.required", "true"}, {"diff.hostile.textconv", helper}, {"core.fsmonitor", helper}, {"core.hooksPath", filepath.Join(d, "hooks")}} {
		git(t, d, "config", kv[0], kv[1])
	}
	os.MkdirAll(filepath.Join(d, "hooks"), 0o755)
	for _, hook := range []string{"post-index-change", "pre-commit", "fsmonitor-watchman"} {
		os.WriteFile(filepath.Join(d, "hooks", hook), []byte("#!/bin/sh\ntouch '"+sentinel+"'\n"), 0o755)
	}
	os.WriteFile(filepath.Join(d, "d000", "file00001.txt"), []byte("stat-dirty\n"), 0o644)
	indexPath := filepath.Join(d, ".git", "index")
	st, _ := os.Stat(indexPath)
	before := snapshot(t, d)
	r := gitInventory(t, d, InventoryOptions{Scopes: []string{"disk", "tracked", "untracked", "staged", "stored"}})
	if r.Status == "error" {
		t.Fatalf("%+v", r.Diagnostics)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("helper ran: %v", err)
	}
	if !equalSnapshot(before, snapshot(t, d)) {
		t.Fatal("inventory wrote target state")
	}
	st2, _ := os.Stat(indexPath)
	if !st.ModTime().Equal(st2.ModTime()) {
		t.Fatal("index modification time changed")
	}
	locks, _ := filepath.Glob(filepath.Join(d, ".git", "*.lock"))
	if len(locks) != 0 {
		t.Fatalf("locks %v", locks)
	}
}
