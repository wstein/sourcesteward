// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, file, content string) {
	t.Helper()
	write(t, filepath.Join(dir, file), content)
	runGit(t, dir, "add", file)
	runGit(t, dir, "commit", "-q", "-m", "change "+file)
}

// workspace is a root repository "ws" with submodules a, b (on main) and c (detached), each with its own bare remote.
type workspace struct {
	root, ws string
	bare     map[string]string // "ws", "a", "b", "c"
}

func newWorkspace(t *testing.T) *workspace {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &workspace{root: root, ws: filepath.Join(root, "ws"), bare: map[string]string{}}
	for _, name := range []string{"ws", "a", "b", "c", "n", "dev"} {
		w.bare[name] = filepath.Join(root, name+".git")
		runGit(t, root, "init", "-q", "--bare", "-b", "main", w.bare[name])
		seed := filepath.Join(root, "seed-"+name)
		runGit(t, root, "clone", "-q", w.bare[name], seed)
		runGit(t, seed, "checkout", "-q", "-b", "main")
		commit(t, seed, name+".txt", "one\n")
		runGit(t, seed, "push", "-q", "origin", "main")
	}
	runGit(t, filepath.Join(root, "seed-dev"), "checkout", "-q", "-b", "develop")
	commit(t, filepath.Join(root, "seed-dev"), "develop.txt", "d\n")
	runGit(t, filepath.Join(root, "seed-dev"), "push", "-q", "origin", "develop")
	runGit(t, root, "clone", "-q", w.bare["ws"], w.ws)
	for _, name := range []string{"a", "b", "c"} {
		runGit(t, w.ws, "submodule", "add", "-q", "-b", "main", w.bare[name], name)
	}
	runGit(t, w.ws, "submodule", "add", "-q", "-b", "develop", w.bare["dev"], "dev")
	runGit(t, filepath.Join(w.ws, "a"), "submodule", "add", "-q", "-b", "main", w.bare["n"], "n")
	runGit(t, filepath.Join(w.ws, "a"), "commit", "-q", "-m", "add nested submodule")
	runGit(t, filepath.Join(w.ws, "a"), "push", "-q", "origin", "main")
	runGit(t, w.ws, "add", "a")
	runGit(t, w.ws, "commit", "-q", "-m", "add submodules")
	runGit(t, w.ws, "push", "-q", "origin", "main")
	runGit(t, filepath.Join(w.ws, "c"), "checkout", "-q", "--detach")
	return w
}

func (w *workspace) dir(name string) string {
	if name == "." {
		return w.ws
	}
	return filepath.Join(w.ws, name)
}

func (w *workspace) bareKey(name string) string {
	switch name {
	case ".":
		return "ws"
	case "a/n":
		return "n"
	}
	return name
}

// advance adds a commit to branch (default main) of the remote of repo name through a throwaway clone.
func (w *workspace) advance(t *testing.T, name, branch string) {
	t.Helper()
	w.advanceFile(t, name, branch, "")
}

// advanceFile is advance with the commit changing FILE (a new file when empty).
func (w *workspace) advanceFile(t *testing.T, name, branch, file string) {
	t.Helper()
	clone := filepath.Join(w.root, "adv-"+name+"-"+itoa(len(w.bare)))
	w.bare[name+"#"+itoa(len(w.bare))] = clone
	runGit(t, w.root, "clone", "-q", w.bare[w.bareKey(name)], clone)
	runGit(t, clone, "checkout", "-q", "-B", branch, "origin/"+branch)
	if file == "" {
		file = "remote-" + itoa(len(w.bare)) + ".txt"
	}
	commit(t, clone, file, "remote edit "+itoa(len(w.bare))+"\n")
	runGit(t, clone, "push", "-q", "origin", branch)
}

func (w *workspace) oid(t *testing.T, name, ref string) string {
	t.Helper()
	return runGit(t, w.dir(name), "rev-parse", "--verify", ref)
}

func repoByName(t *testing.T, r Result) map[string]Repo {
	t.Helper()
	m := map[string]Repo{}
	for _, it := range r.Repos {
		m[it.Name] = it
	}
	return m
}

func itoa(i int) string { return strconv.Itoa(i) }

func gitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd
}

func hookScript(t *testing.T, repoGitDir, name, body string) {
	t.Helper()
	path := filepath.Join(repoGitDir, "hooks", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, path, "#!/bin/sh\n"+body)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func sortStrings(s []string) { sort.Strings(s) }

func worktreeState(t *testing.T, tree string) string {
	t.Helper()
	var keep []string
	for _, l := range strings.Split(runGit(t, tree, "status", "--porcelain=v2", "--branch"), "\n") {
		if !strings.HasPrefix(l, "# branch.ab") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n") + runGit(t, tree, "rev-parse", "HEAD")
}

func snapshotTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		raw, err := os.ReadFile(p)
		out[strings.TrimPrefix(p, dir)] = string(raw) + "|" + info.Mode().String()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
