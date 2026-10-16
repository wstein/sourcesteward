// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// reposHermetic keeps a test independent of the developer's HOME and global Git configuration. System
// configuration cannot be neutralized for Inspect and Inventory (their runner drops every GIT_ variable);
// the fixtures themselves are created with GIT_CONFIG_NOSYSTEM by git().
func reposHermetic(t *testing.T) { t.Helper(); pinsIsolateHome(t) }

// reposTempDir returns a symlink-free temporary directory, so expectations do not depend on a symlinked
// TMPDIR (macOS /var, /tmp).
func reposTempDir(t *testing.T) string { t.Helper(); return pinsRealDir(t) }

// reposNewRepo creates a Git repository with one commit at dir (created when absent).
func reposNewRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "initial")
	return dir
}

// reposSuper creates a superproject at dir with the named submodules (each cloned from a private source
// repository) and commits the registration.
func reposSuper(t *testing.T, dir string, names ...string) string {
	t.Helper()
	reposNewRepo(t, dir)
	src := reposNewRepo(t, filepath.Join(reposTempDir(t), "src"))
	for _, n := range names {
		git(t, dir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", src, n)
	}
	git(t, dir, "commit", "-qm", "submodules")
	return dir
}

// reposRequireGit skips before any fixture is touched when Git is older than the given minor version.
func reposRequireGit(t *testing.T, minor int) {
	t.Helper()
	out, err := exec.Command("git", "version").Output()
	if err != nil {
		t.Skip("git unavailable")
	}
	var major, mnr int
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		t.Skip("unrecognized git version")
	}
	parts := strings.SplitN(fields[2], ".", 3)
	if len(parts) < 2 {
		t.Skip("unrecognized git version")
	}
	major, mnr = atoiPrefix(parts[0]), atoiPrefix(parts[1])
	if major < 2 || (major == 2 && mnr < minor) {
		t.Skipf("needs Git 2.%d, have %s", minor, fields[2])
	}
}

func atoiPrefix(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// reposGitlinks registers submodules without checking them out: .gitmodules entries (name, path) plus
// gitlinks in the index, committed. The result is a superproject whose submodules are all uninitialized.
func reposGitlinks(t *testing.T, repo string, namePath ...[2]string) {
	t.Helper()
	reposRegister(t, repo, true, namePath...)
}

// reposRegister writes .gitmodules entries (name, path) and, when link is set, a gitlink per path.
func reposRegister(t *testing.T, repo string, link bool, namePath ...[2]string) {
	t.Helper()
	oid := gitIn(t, repo, "rev-parse", "HEAD")
	for _, np := range namePath {
		git(t, repo, "config", "-f", ".gitmodules", "submodule."+np[0]+".path", np[1])
		git(t, repo, "config", "-f", ".gitmodules", "submodule."+np[0]+".url", "../src")
		if link {
			git(t, repo, "update-index", "--add", "--cacheinfo", "160000,"+oid+","+np[1])
		}
	}
	git(t, repo, "add", ".gitmodules")
	git(t, repo, "commit", "-qm", "register submodules")
}

// reposRun runs the real operation.
func reposRun(paths []string, submodules bool) ReposResult {
	return Repos(context.Background(), ReposOptions{Paths: paths, Submodules: submodules})
}

// reposEntry returns the entry with the given reported path.
func reposEntry(t *testing.T, res ReposResult, path string) ReposEntry {
	t.Helper()
	for _, e := range res.Entries {
		if e.Path == path {
			return e
		}
	}
	t.Fatalf("no entry %q in %v", path, reposPaths(res))
	return ReposEntry{}
}

func reposPaths(res ReposResult) []string {
	var p []string
	for _, e := range res.Entries {
		p = append(p, e.Path)
	}
	return p
}

// reposCountingRunner counts the Git processes started through the discovery runner.
func reposCountingRunner(n *atomic.Int32) reposNewRunner {
	return func(ctx context.Context) *pinsRunner {
		r := newReposRunner(ctx)
		r.env = func(env []string) []string { n.Add(1); return env }
		return r
	}
}

// reposRunWith runs the operation with the real inspection and inventory and the given seams.
func reposRunWith(paths []string, inspect inspectFunc, newRunner reposNewRunner) ReposResult {
	if inspect == nil {
		inspect = Inspect
	}
	if newRunner == nil {
		newRunner = newReposRunner
	}
	return reposWithRunner(context.Background(), ReposOptions{Paths: paths, Submodules: true}, inspect, Inventory, newRunner)
}

// reposRecordingInspect records every path whose identity is inspected.
func reposRecordingInspect(paths *[]string, mu interface {
	Lock()
	Unlock()
}) inspectFunc {
	return func(ctx context.Context, p string) Result {
		mu.Lock()
		*paths = append(*paths, p)
		mu.Unlock()
		return Inspect(ctx, p)
	}
}

// reposGitStdin runs git with standard input, for plumbing such as mktree.
func reposGitStdin(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	c.Stdin = strings.NewReader(stdin)
	b, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s %v", args, b, err)
	}
	return string(b)
}
