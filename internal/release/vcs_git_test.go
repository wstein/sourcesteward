// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitSourcesRequireExactCommitObject(t *testing.T) {
	for _, format := range []string{"sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			git(t, dir, "init", "-q", "-b", "main", "--object-format="+format)
			write(t, dir, ".stw/release.toml", exampleBytes(t, "go-cli"))
			commit := commitAll(t, dir)
			git(t, dir, "remote", "add", "origin", "https://github.com/example/test.git")
			git(t, dir, "update-ref", "refs/remotes/origin/main", commit)
			git(t, dir, "tag", "-a", "v1.2.3", "-m", "fixture", commit)
			for _, tc := range []struct {
				name, id string
				valid    bool
			}{
				{"commit", commit, true},
				{"annotated tag", git(t, dir, "rev-parse", "refs/tags/v1.2.3"), false},
				{"tree", git(t, dir, "rev-parse", commit+"^{tree}"), false},
				{"blob", git(t, dir, "rev-parse", commit+":.stw/release.toml"), false},
				{"missing", strings.Repeat("0", len(commit)), false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					source, err := newGitSource(context.Background(), dir, tc.id)
					checked := Check(context.Background(), CheckOptions{Path: dir, Commit: tc.id})
					planned := Plan(context.Background(), PlanOptions{Path: dir, Subject: tc.id, Version: "v1.2.3"})
					if tc.valid {
						if err != nil || source == nil || source.commit != commit || checked.Status != "ok" || checked.Commit != commit || planned.Status != "ok" || planned.Subject.Commit == nil || *planned.Subject.Commit != commit {
							t.Fatalf("actual commit not preserved: source=%+v err=%v check=%+v plan=%+v", source, err, checked, planned)
						}
					} else {
						if !errors.Is(err, errCommitUnresolved) {
							t.Errorf("non-commit source accepted: %+v %v", source, err)
						}
						if checked.Status != "refused" || checked.Error == nil || checked.Error.Code != "commit-unresolved" {
							t.Errorf("check accepted non-commit: %+v", checked)
						}
						if planned.Status != "refused" || planned.Error == nil || planned.Error.Code != "commit-unresolved" || len(planned.WouldBuild) != 0 {
							t.Errorf("plan accepted non-commit: %+v", planned)
						}
					}
				})
			}
		})
	}
}

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

func TestGitEntriesTreatPathsLiterally(t *testing.T) {
	dir := repoWith(t, map[string][]byte{"we*ird[1]/f": []byte("x"), ":colon/x": []byte("x"), ":8080": []byte("x"), "q?/y": []byte("x"), "plain/z": []byte("x")})
	commit := commitAll(t, dir)
	source, err := newGitSource(context.Background(), dir, commit)
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.Entries([]string{"we*ird[1]/f", ":colon/x", ":8080", "q?/y", "plain/z", "we*ird[1]/g", "wexird1/f", "plain/*", ":(top)plain", ":(glob)plain/*"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]TreeEntry{"we*ird[1]/f": {Kind: "file"}, ":colon/x": {Kind: "file"}, ":8080": {Kind: "file"}, "q?/y": {Kind: "file"}, "plain/z": {Kind: "file"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCheckArgvWordsThatLookLikePathspecsAreFine(t *testing.T) {
	contract := strings.Replace(string(exampleBytes(t, "go-cli")), `"sh", "scripts/release-package.sh"`, `"sh", ":8080", ":(top)x", "*", "a[b]", "scripts/release-package.sh"`, 1)
	dir := repoWith(t, map[string][]byte{".stw/release.toml": []byte(contract), ":8080": []byte("x"), "scripts/release-package.sh": []byte("x")})
	commit := commitAll(t, dir)
	for _, opts := range []CheckOptions{{Path: dir}, {Path: dir, Commit: commit}} {
		if r := run(t, opts); r.Status != "ok" {
			t.Fatalf("%+v: %s %+v %q", opts, r.Status, r.Error, codes(r))
		}
	}
	// The word is also an input: allowlisting it is found in both forms.
	contract = strings.Replace(contract, `paths = ["docs/**", "README.md", "llms.txt"]`, `paths = ["*8080"]`, 1)
	write(t, dir, ".stw/release.toml", []byte(contract))
	commit = commitAll(t, dir)
	for _, opts := range []CheckOptions{{Path: dir}, {Path: dir, Commit: commit}} {
		wantCodes(t, run(t, opts), "allowlist-overlaps-input .stw/release.toml /allowlist/paths/0")
	}
}
