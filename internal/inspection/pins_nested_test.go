// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"path/filepath"
	"testing"
)

func TestPinsNestedSubmodulesAreDetectedFromTheSubmoduleHeadTree(t *testing.T) {
	c := newPublishCase(t)
	wt := c.repo.WorkTree
	head := mustHead(t, c.run, c.repo)
	present, err := pinsNestedPresent(c.run, c.repo, head)
	if err != nil || present {
		t.Fatalf("no .gitmodules: %v %v", present, err)
	}
	// An untracked or staged-only .gitmodules is not in the HEAD tree.
	writeFile(t, filepath.Join(wt, ".gitmodules"), "[submodule \"x\"]\n\tpath = x\n\turl = https://example.invalid/x.git\n")
	if present, err = pinsNestedPresent(c.run, c.repo, head); err != nil || present {
		t.Fatalf("worktree file only: %v %v", present, err)
	}
	git(t, wt, "add", ".gitmodules")
	git(t, wt, "commit", "-qm", "nested")
	head, _ = pinsReadHead(c.run, c.repo)
	if present, err = pinsNestedPresent(c.run, c.repo, head); err != nil || !present {
		t.Fatalf("committed .gitmodules: %v %v", present, err)
	}
}

func TestPinsNestedIgnoresASymlinkedGitmodulesAndUnbornHead(t *testing.T) {
	c := newPublishCase(t)
	wt := c.repo.WorkTree
	// Git refuses to add a symlinked .gitmodules, so the tree is built by hand.
	target := gitHash(t, wt, "file.txt")
	tree := gitStdin(t, wt, "100644 blob "+gitIn(t, wt, "rev-parse", "HEAD:file.txt")+"\tfile.txt\n120000 blob "+target+"\t.gitmodules\n", "mktree")
	commit := gitIn(t, wt, "commit-tree", "-p", "HEAD", "-m", "link", tree)
	git(t, wt, "update-ref", "HEAD", commit)
	head := mustHead(t, c.run, c.repo)
	if present, err := pinsNestedPresent(c.run, c.repo, head); err != nil || present {
		t.Fatalf("a symlink is not a registration: %v %v", present, err)
	}
	gitLog, _ := pinsSentinels(t)
	if present, err := pinsNestedPresent(c.run, c.repo, pinsHead{Unborn: true}); err != nil || present {
		t.Fatalf("%v %v", present, err)
	}
	if len(gitLog()) != 0 {
		t.Fatalf("an unborn HEAD has no tree to read: %v", gitLog())
	}
}

func TestPinsNestedStartsOnlyAllowlistedGitCommands(t *testing.T) {
	c := newPublishCase(t)
	gitLog, _ := pinsSentinels(t)
	head := mustHead(t, c.run, c.repo)
	if _, err := pinsNestedPresent(c.run, c.repo, head); err != nil {
		t.Fatal(err)
	}
	requireAllowlisted(t, gitLog(), "ls-tree <OID> .gitmodules")
}

func TestPinsSubmoduleBackendMarkers(t *testing.T) {
	for name, tc := range map[string]struct {
		plant func(t *testing.T, c publishCase)
		want  string
	}{
		"none":               {func(*testing.T, publishCase) {}, ""},
		"jj directory":       {func(t *testing.T, c publishCase) { mkdirs(t, filepath.Join(c.repo.WorkTree, ".jj", "repo")) }, "jj"},
		"jj file":            {func(t *testing.T, c publishCase) { writeFile(t, filepath.Join(c.repo.WorkTree, ".jj"), "x") }, "jj"},
		"gitbutler metadata": {func(t *testing.T, c publishCase) { mkdirs(t, filepath.Join(c.repo.GitDir, "gitbutler")) }, "gitbutler"},
		"gitbutler refs":     {func(t *testing.T, c publishCase) { mkdirs(t, filepath.Join(c.repo.GitDir, "refs", "gitbutler")) }, "gitbutler"},
		"gitbutler workspace": {func(t *testing.T, c publishCase) {
			writeFile(t, filepath.Join(c.repo.GitDir, "refs", "heads", "gitbutler", "workspace"), "x")
		}, "gitbutler"},
		"both": {func(t *testing.T, c publishCase) {
			mkdirs(t, filepath.Join(c.repo.WorkTree, ".jj"), filepath.Join(c.repo.GitDir, "gitbutler"))
		}, "ambiguous"},
	} {
		t.Run(name, func(t *testing.T) {
			c := newPublishCase(t)
			tc.plant(t, c)
			got, err := pinsSubmoduleBackend(c.dirs)
			if err != nil || got != tc.want {
				t.Fatalf("%q %v", got, err)
			}
		})
	}
}
