package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitFixturesIgnoreInheritedRepositoryVariables(t *testing.T) {
	inherited := t.TempDir()
	git(t, inherited, "init", "-q", "-b", "main")
	before, err := os.ReadFile(filepath.Join(inherited, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"GIT_DIR", "GIT_COMMON_DIR"} {
		t.Setenv(name, filepath.Join(inherited, ".git"))
	}
	t.Setenv("GIT_WORK_TREE", inherited)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(inherited, ".git", "index"))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "fixture.leak")
	t.Setenv("GIT_CONFIG_VALUE_0", "1")
	other := t.TempDir()
	git(t, other, "init", "-q", "-b", "main")
	git(t, other, "config", "fixture.value", "1")
	after, err := os.ReadFile(filepath.Join(inherited, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("a fixture wrote into the inherited repository:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(other, ".git", "config")); err != nil {
		t.Fatal("the fixture did not land in its own directory", err)
	}
}

func TestGitEnvDropsInheritedRepositorySelection(t *testing.T) {
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_PREFIX", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS"} {
		t.Setenv(name, "/inherited")
	}
	for _, kv := range gitEnv() {
		for _, bad := range []string{"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE=", "GIT_PREFIX=", "GIT_COMMON_DIR=", "GIT_OBJECT_DIRECTORY=", "GIT_ALTERNATE_OBJECT_DIRECTORIES=", "GIT_NAMESPACE=", "GIT_CONFIG_COUNT=", "GIT_CONFIG_PARAMETERS="} {
			if strings.HasPrefix(kv, bad) {
				t.Fatalf("gitEnv kept %s", kv)
			}
		}
	}
}

func TestInheritedGitDirGuard(t *testing.T) {
	temp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := inheritedGitDirProblem("", temp); err != nil {
		t.Fatal(err)
	}
	if err := inheritedGitDirProblem(filepath.Join(temp, "r", ".git"), temp); err != nil {
		t.Fatal(err)
	}
	if err := inheritedGitDirProblem("/tmp/other/.git", "/elsewhere", "/tmp"); err != nil {
		t.Fatal("a repository under the default temporary root was refused: ", err)
	}
	if err := inheritedGitDirProblem("/workspaces/real/.git", temp); err == nil {
		t.Fatal("a GIT_DIR outside the temporary directory was accepted")
	}
	if err := inheritedGitDirProblem(temp+"/../escape", temp); err == nil {
		t.Fatal("a GIT_DIR that climbs out of the temporary directory was accepted")
	}
}
