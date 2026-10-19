// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A test run inside a Git hook, a rebase step or any shell that exports repository
// variables must not let fixtures write into that repository: the process
// environment is cleaned before the first test starts.
func TestGitFixturesDoNotWriteIntoAnInheritedRepository(t *testing.T) {
	if os.Getenv("STW_GITENV_PROBE") == "1" {
		fixture := t.TempDir()
		for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "fixture.probe", "1"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = fixture
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		if _, err := os.Stat(filepath.Join(fixture, ".git", "config")); err != nil {
			t.Fatal("the fixture did not land in its own directory: ", err)
		}
		return
	}
	inherited := t.TempDir()
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = inherited
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	config := filepath.Join(inherited, ".git", "config")
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	probe := exec.Command(os.Args[0], "-test.run=^TestGitFixturesDoNotWriteIntoAnInheritedRepository$", "-test.count=1")
	probe.Env = append(os.Environ(), "STW_GITENV_PROBE=1", "GIT_DIR="+filepath.Join(inherited, ".git"), "GIT_WORK_TREE="+inherited, "GIT_INDEX_FILE="+filepath.Join(inherited, ".git", "index"), "GIT_COMMON_DIR="+filepath.Join(inherited, ".git"), "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=fixture.leak", "GIT_CONFIG_VALUE_0=1")
	if out, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("probe run: %v\n%s", err, out)
	}
	after, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("package fixtures wrote into the inherited repository:\n%s", after)
	}
}

func TestInheritedGitDirGuardRefusesRealRepositories(t *testing.T) {
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
	for _, bad := range []string{"/workspaces/real/.git", temp + "/../escape", "relative/.git"} {
		if err := inheritedGitDirProblem(bad, temp); err == nil {
			t.Fatalf("GIT_DIR %q was accepted", bad)
		}
	}
}
