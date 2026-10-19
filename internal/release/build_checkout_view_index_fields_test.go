// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every captured root, the first and each gitlink checkout, binds its index by mode,
// object, stage, path and the assume-unchanged, skip-worktree and intent-to-add flags.
// The digest is the only guard of a nested root, so each field is changed on its own.
func newNestedIndexFixture(t *testing.T) (*buildViewGraph, string, string) {
	t.Helper()
	root := repoWith(t, map[string][]byte{"tracked": []byte("root\n"), "empty": nil})
	commitAll(t, root)
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, child, "init", "-q", "-b", "main")
	write(t, child, "tracked", []byte("child\n"))
	write(t, child, "other", []byte("other\n"))
	write(t, child, "empty", nil)
	head := commitAll(t, child)
	git(t, root, "update-index", "--add", "--cacheinfo", "160000,"+head+",child")
	commit := commitAll(t, root)
	f := buildViewFixture{root: root, commit: commit}
	f.git, _ = exec.LookPath("git")
	f.binary, _ = os.Executable()
	graph := f.mustPrepare(t, nil)
	if len(graph.roots) != 2 {
		t.Fatalf("roots %d", len(graph.roots))
	}
	return graph, root, child
}

func TestBuildViewIndexDigestBindsEveryFieldOfEveryRoot(t *testing.T) {
	blob := func(t *testing.T, dir, content string) string {
		cmd := exec.Command("git", "hash-object", "-w", "--stdin")
		cmd.Dir, cmd.Env = dir, gitEnv()
		cmd.Stdin = strings.NewReader(content)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out[:len(out)-1])
	}
	cases := map[string]func(t *testing.T, dir string){
		"assume-unchanged": func(t *testing.T, dir string) { git(t, dir, "update-index", "--assume-unchanged", "tracked") },
		"skip-worktree":    func(t *testing.T, dir string) { git(t, dir, "update-index", "--skip-worktree", "tracked") },
		"intent-to-add": func(t *testing.T, dir string) {
			write(t, dir, "added", []byte("a\n"))
			git(t, dir, "add", "-N", "added")
		},
		// The listing header shows the object and mode but not this flag: an empty tracked
		// file re-added as intent-to-add keeps the same header and differs only by flag.
		"intent-to-add on an empty tracked file": func(t *testing.T, dir string) {
			git(t, dir, "rm", "-q", "--cached", "empty")
			git(t, dir, "add", "-N", "empty")
		},
		"mode": func(t *testing.T, dir string) { git(t, dir, "update-index", "--chmod=+x", "tracked") },
		"object": func(t *testing.T, dir string) {
			git(t, dir, "update-index", "--cacheinfo", "100644,"+blob(t, dir, "different\n")+",tracked")
		},
		"path": func(t *testing.T, dir string) {
			id := git(t, dir, "rev-parse", ":tracked")
			git(t, dir, "update-index", "--force-remove", "tracked")
			git(t, dir, "update-index", "--add", "--cacheinfo", "100644,"+id+",zrenamed")
		},
		"removed entry": func(t *testing.T, dir string) { git(t, dir, "update-index", "--force-remove", "tracked") },
		"conflict stage": func(t *testing.T, dir string) {
			id := git(t, dir, "rev-parse", ":tracked")
			cmd := exec.Command("git", "update-index", "--index-info")
			cmd.Dir, cmd.Env = dir, gitEnv()
			cmd.Stdin = strings.NewReader("0 0000000000000000000000000000000000000000\ttracked\n100644 " + id + " 2\ttracked\n")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
		},
	}
	for name, tamper := range cases {
		for _, nested := range []bool{false, true} {
			label := name + "/root"
			if nested {
				label = name + "/gitlink checkout"
			}
			t.Run(label, func(t *testing.T) {
				graph, root, child := newNestedIndexFixture(t)
				defer graph.Close()
				if err := graph.checkIndexes(context.Background(), false); err != nil {
					t.Fatal("unchanged indexes refused: ", err)
				}
				dir := root
				if nested {
					dir = child
				}
				tamper(t, dir)
				if err := graph.checkIndexes(context.Background(), false); err == nil {
					t.Fatal("index change accepted")
				}
			})
		}
	}
	t.Run("stat refresh stays accepted", func(t *testing.T) {
		graph, root, child := newNestedIndexFixture(t)
		defer graph.Close()
		for _, dir := range []string{root, child} {
			future := time.Now().Add(time.Minute)
			if err := os.Chtimes(filepath.Join(dir, "tracked"), future, future); err != nil {
				t.Fatal(err)
			}
			git(t, dir, "update-index", "--refresh")
		}
		if err := graph.checkIndexes(context.Background(), false); err != nil {
			t.Fatal("stat refresh refused: ", err)
		}
	})
}
