// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitOutputIsCapped(t *testing.T) {
	dir := repoWith(t, map[string][]byte{"big": []byte(strings.Repeat("x", 1000))})
	commit := commitAll(t, dir)
	oid := git(t, dir, "rev-parse", commit+":big")
	out, truncated, status, err := gitOutput(context.Background(), dir, 10, "", "cat-file", "blob", oid)
	if err != nil || !truncated || len(out) != 10 || status != 0 {
		t.Fatalf("%d bytes truncated=%v status=%d err=%v", len(out), truncated, status, err)
	}
	out, truncated, _, err = gitOutput(context.Background(), dir, 1000, "", "cat-file", "blob", oid)
	if err != nil || truncated || len(out) != 1000 {
		t.Fatalf("an output of exactly the limit is complete: %d %v %v", len(out), truncated, err)
	}
	_, _, status, err = gitOutput(context.Background(), dir, 10, "", "cat-file", "-e", strings.Repeat("0", 40))
	if err != nil || status == 0 {
		t.Fatalf("a missing object must be a nonzero status: %d %v", status, err)
	}
}

func TestGitEntriesOfATree(t *testing.T) {
	dir := repoWith(t, map[string][]byte{"dir/file.txt": []byte("x"), "dir/sibling.txt": []byte("x"), "other/z": []byte("x"), "top.txt": []byte("x")})
	if err := os.Symlink("dir/file.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dir", filepath.Join(dir, "dirlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "top.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	commit := commitAll(t, dir)
	git(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+commit+",sub")
	git(t, dir, "commit", "-q", "-m", "gitlink")
	source, err := newGitSource(context.Background(), dir, git(t, dir, "rev-parse", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.Entries([]string{"dir", "dir/file.txt", "top.txt", "sub", "missing", "dir/missing", "top.txt/below", "link", "dirlink", "dirlink/file.txt"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]TreeEntry{"dir": {Kind: "dir"}, "dir/file.txt": {Kind: "file"}, "top.txt": {Kind: "file"}, "sub": {Kind: "dir"}, "link": {Kind: "symlink", Target: "dir/file.txt"}, "dirlink": {Kind: "symlink", Target: "dir"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestGitEntriesRefusesMoreProbesThanItCanCheck(t *testing.T) {
	dir := repoWith(t, map[string][]byte{"x": []byte("x")})
	head := commitAll(t, dir)
	source, err := newGitSource(context.Background(), dir, head)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, maxTreeProbes+1)
	for i := range paths {
		paths[i] = fmt.Sprintf("p%d", i)
	}
	paths[maxTreeProbes] = "x"
	if _, err := source.Entries(paths); err == nil {
		t.Fatal("the probe list was silently truncated")
	}
	if got, err := source.Entries(append(paths[:maxTreeProbes-1:maxTreeProbes-1], "x")); err != nil || got["x"].Kind != "file" {
		t.Fatalf("a list of exactly the limit is probed: %v %v", got, err)
	}
	if _, err := source.Entries([]string{"a\nb"}); err == nil {
		t.Fatal("a path with a newline cannot be probed safely")
	}
}

func TestCheckCommitWithAFileInPlaceOfTheDirectory(t *testing.T) {
	dir := repoWith(t, map[string][]byte{".stw": []byte("not a directory")})
	commit := commitAll(t, dir)
	if r := run(t, CheckOptions{Path: dir, Commit: commit}); r.Status != "refused" || r.Error.Code != "config-missing" {
		t.Fatalf("%+v", r)
	}
}

func TestCheckCommitReadsAnExecutableContractButNotADirectory(t *testing.T) {
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	if err := os.Chmod(filepath.Join(dir, ".stw", "release.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	commit := commitAll(t, dir)
	if r := run(t, CheckOptions{Path: dir, Commit: commit}); r.Status != "ok" {
		t.Fatalf("an executable contract file is a regular file: %+v %q", r, codes(r))
	}
	other := repoWith(t, map[string][]byte{".stw/release.toml/inner": []byte("x")})
	commit = commitAll(t, other)
	r := run(t, CheckOptions{Path: other, Commit: commit})
	if len(r.Findings) != 1 || !strings.Contains(r.Findings[0].Message, "not a directory") {
		t.Fatalf("%+v", r.Findings)
	}
	r = run(t, CheckOptions{Path: other})
	if len(r.Findings) != 1 || !strings.Contains(r.Findings[0].Message, "not a directory") {
		t.Fatalf("working tree: %+v", r.Findings)
	}
}

func TestCheckSymlinkMessagesNameTheKind(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "real.toml", exampleBytes(t, "go-cli"))
	if err := os.MkdirAll(filepath.Join(dir, ".stw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../real.toml", filepath.Join(dir, ".stw", "release.toml")); err != nil {
		t.Fatal(err)
	}
	commit := commitAll(t, dir)
	for _, opts := range []CheckOptions{{Path: dir}, {Path: dir, Commit: commit}} {
		r := run(t, opts)
		if len(r.Findings) != 1 || !strings.Contains(r.Findings[0].Message, "not a symlink") {
			t.Errorf("%+v: %+v", opts, r.Findings)
		}
	}
}
