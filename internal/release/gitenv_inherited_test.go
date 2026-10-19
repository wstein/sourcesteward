// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"crypto/sha256"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Each repository-selecting variable on its own, as a Git hook or a rebase step
// exports it, must leave the repository it points into byte-identical: the package
// tests start from a clean Git environment.
func TestInheritedRepositoryVariablesLeaveTheRepositoryUntouched(t *testing.T) {
	if os.Getenv("STW_GITENV_PROBE") == "1" {
		for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_PREFIX", "GIT_NAMESPACE", "GIT_CONFIG_COUNT"} {
			if value, ok := os.LookupEnv(name); ok {
				t.Errorf("%s=%q reached the tests", name, value)
			}
		}
		fixture := t.TempDir()
		if err := os.WriteFile(filepath.Join(fixture, "f"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "f"}, {"-c", "user.name=T", "-c", "user.email=t@example.org", "-c", "commit.gpgsign=false", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "commit", "-q", "-m", "fixture"}, {"config", "fixture.probe", "1"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = fixture
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		return
	}
	snapshot := func(root string) map[string][32]byte {
		files := map[string][32]byte{}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			raw, err := os.ReadFile(path)
			rel, _ := filepath.Rel(root, path)
			files[rel] = sha256.Sum256(raw)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY"} {
		t.Run(name, func(t *testing.T) {
			inherited := t.TempDir()
			if err := os.WriteFile(filepath.Join(inherited, "tracked"), []byte("t"), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "tracked"}, {"-c", "user.name=T", "-c", "user.email=t@example.org", "-c", "commit.gpgsign=false", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "commit", "-q", "-m", "inherited"}} {
				cmd := exec.Command("git", args...)
				cmd.Dir = inherited
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			values := map[string]string{"GIT_DIR": filepath.Join(inherited, ".git"), "GIT_WORK_TREE": inherited, "GIT_INDEX_FILE": filepath.Join(inherited, ".git", "index"), "GIT_COMMON_DIR": filepath.Join(inherited, ".git"), "GIT_OBJECT_DIRECTORY": filepath.Join(inherited, ".git", "objects")}
			before := snapshot(inherited)
			probe := exec.Command(os.Args[0], "-test.run=^TestInheritedRepositoryVariablesLeaveTheRepositoryUntouched$", "-test.count=1")
			probe.Env = append(os.Environ(), "STW_GITENV_PROBE=1", name+"="+values[name])
			if out, err := probe.CombinedOutput(); err != nil {
				t.Fatalf("probe run: %v\n%s", err, out)
			}
			if after := snapshot(inherited); !reflect.DeepEqual(before, after) {
				t.Fatalf("the tests changed the repository that %s selects", name)
			}
		})
	}
}

func TestInheritedGitDirGuardRefusesLinkedWorktreesAndGitfiles(t *testing.T) {
	temp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(temp, "linked")
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, "commondir"), []byte("/workspaces/real/.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := inheritedGitDirProblem(linked, temp); err == nil {
		t.Fatal("a linked-worktree directory of a repository outside the temporary root was accepted")
	}
	if err := os.WriteFile(filepath.Join(linked, "commondir"), []byte("../main/.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := inheritedGitDirProblem(linked, temp); err != nil {
		t.Fatal(err)
	}
	gitfile := filepath.Join(temp, "gitfile")
	if err := os.WriteFile(gitfile, []byte("gitdir: /workspaces/real/.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := inheritedGitDirProblem(gitfile, temp); err == nil {
		t.Fatal("a gitfile was accepted")
	}
	link := filepath.Join(temp, "link")
	if err := os.Symlink("/", link); err != nil {
		t.Fatal(err)
	}
	if err := inheritedGitDirProblem(link, temp); err == nil {
		t.Fatal("a symbolic link out of the temporary root was accepted")
	}
}

func TestJJEnvironmentDropsInheritedRepositoryVariables(t *testing.T) {
	t.Setenv("GIT_INDEX_FILE", "/inherited/index")
	t.Setenv("GIT_DIR", "/inherited")
	for _, kv := range jjEnvironment(t.TempDir()) {
		if strings.HasPrefix(kv, "GIT_INDEX_FILE=") || strings.HasPrefix(kv, "GIT_DIR=") {
			t.Fatalf("jj inherited %s", kv)
		}
	}
}

func TestTemporaryRootsExcludeLinksIntoWorkingTrees(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "real", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "real"), filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	if roots := temporaryRoots(filepath.Join(base, "link"), filepath.Join(base, "real")); len(roots) != 0 {
		t.Fatalf("a temporary root inside a working tree was accepted: %v", roots)
	}
	if roots := temporaryRoots(filepath.Join(base, "plain")); len(roots) != 1 || roots[0] != filepath.Join(base, "plain") {
		t.Fatalf("roots %v", roots)
	}
}
