// SPDX-License-Identifier: EUPL-1.2
package conflicts

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// snapshot is everything a probe must leave alone: HEAD, the index, the working tree status, the
// configuration and every ref and object outside the private namespace.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	s := map[string]string{
		"head":   git(t, dir, "rev-parse", "HEAD"),
		"branch": git(t, dir, "symbolic-ref", "HEAD"),
		"status": git(t, dir, "status", "--porcelain"),
		"index":  git(t, dir, "ls-files", "--stage"),
	}
	cfg, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	s["config"] = string(cfg)
	var refs []string
	for _, l := range strings.Split(git(t, dir, "for-each-ref", "--format=%(refname) %(objectname)"), "\n") {
		if l != "" && !strings.HasPrefix(l, NamespaceRoot) {
			refs = append(refs, l)
		}
	}
	s["refs"] = strings.Join(refs, "\n")
	return s
}

func objectFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	_ = filepath.Walk(filepath.Join(dir, ".git", "objects"), func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	return files
}

func TestRunnerFetchResolveProbe(t *testing.T) {
	f := newRepoFixture(t)
	ctx := context.Background()
	r := NewRunner(f.dir)
	defer r.Close()
	before := snapshot(t, f.dir)
	if err := r.Fetch(ctx, "origin", []int{1, 2, 3}, []string{"main", "feat-one"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Resolve(ctx, NamespaceRoot+"pull/1"); got != f.head1 {
		t.Fatalf("pull/1 = %q, want %q", got, f.head1)
	}
	if got, _ := r.Resolve(ctx, NamespaceRoot+"base/main"); got != f.mainTip {
		t.Fatalf("base/main = %q, want %q", got, f.mainTip)
	}
	if got, err := r.Resolve(ctx, NamespaceRoot+"pull/9"); got != "" || err != nil {
		t.Fatalf("absent ref = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(f.dir, ".git", "FETCH_HEAD")); err == nil {
		t.Fatal("fetch wrote FETCH_HEAD")
	}
	objects := objectFiles(t, f.dir)

	mb, err := r.MergeBase(ctx, f.mainTip, f.head1)
	if err != nil || mb != f.base {
		t.Fatalf("merge base %q, %v; want %q", mb, err, f.base)
	}
	conflict, files, err := r.MergeTree(ctx, f.mainTip, f.head1)
	if err != nil || !conflict || !reflect.DeepEqual(files, []string{"rules.md"}) {
		t.Fatalf("pr 1: conflict=%v files=%v err=%v", conflict, files, err)
	}
	conflict, files, err = r.MergeTree(ctx, f.mainTip, f.head3)
	if err != nil || conflict || len(files) != 0 {
		t.Fatalf("pr 3: conflict=%v files=%v err=%v", conflict, files, err)
	}
	if got, err := r.LastTouch(ctx, f.base, f.mainTip, "rules.md"); err != nil || got != f.mainTouchRules {
		t.Fatalf("last touch %q, %v; want %q", got, err, f.mainTouchRules)
	}
	if got, err := r.LastTouch(ctx, f.base, f.mainTip, "own.md"); err != nil || got != "" {
		t.Fatalf("untouched file: %q, %v", got, err)
	}
	ch, err := r.Changed(ctx, f.base, f.head1)
	if err != nil || !reflect.DeepEqual(ch, []string{"notes.md", "own.md", "rules.md"}) {
		t.Fatalf("changed %v, %v", ch, err)
	}
	if after := objectFiles(t, f.dir); !reflect.DeepEqual(after, objects) {
		t.Fatalf("probe, merge base, last touch and changed wrote objects: %d before, %d after", len(objects), len(after))
	}
	if after := snapshot(t, f.dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("local state changed:\nbefore %v\nafter  %v", before, after)
	}
}

func TestRunnerRefusesUnsafeNames(t *testing.T) {
	f := newRepoFixture(t)
	r := NewRunner(f.dir)
	defer r.Close()
	ctx := context.Background()
	for _, remote := range []string{"", "-u", "a b", "--upload-pack=x"} {
		if err := r.Fetch(ctx, remote, []int{1}, nil); err == nil {
			t.Errorf("remote %q accepted", remote)
		}
	}
	for _, base := range []string{"", "-x", "a..b", "a b", "x/", "/x", "a//b", "x.lock", "a\nb"} {
		if err := r.Fetch(ctx, "origin", nil, []string{base}); err == nil {
			t.Errorf("base %q accepted", base)
		}
	}
	if err := r.Fetch(ctx, "origin", []int{0}, nil); err == nil {
		t.Error("pull request 0 accepted")
	}
}

func TestValidBranch(t *testing.T) {
	for _, ok := range []string{"main", "codex/stw-conflicts", "release-1.2", "a_b", "feat+x"} {
		if !ValidBranch(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", "-x", "--upload-pack=x", "a..b", "a b", "x/", "/x", "a//b", "x.lock", "x.", "a/.b", "a@{1}", "a\nb", "a:b", "a~1", "a^", "a?b", "a*b", "a[b"} {
		if ValidBranch(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	if ValidSHA(strings.Repeat("A", 40)) || ValidSHA("abc") || !ValidSHA(strings.Repeat("a", 40)) {
		t.Error("ValidSHA")
	}
}
