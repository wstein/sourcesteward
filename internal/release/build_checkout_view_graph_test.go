// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildViewGraphPreservesNativeNormalizationAndCapturedConfig(t *testing.T) {
	root := repoWith(t, map[string][]byte{"tracked": []byte("original\r\n"), ".gitattributes": []byte("tracked text eol=lf\n")})
	commit := commitAll(t, root)
	if err := os.WriteFile(filepath.Join(root, ".git", "selected"), []byte("[fixture]\nmarker = captured\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "config", "--add", "include.path", "selected")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	graph, err := prepareBuildViewGraph(context.Background(), buildViewGraphOptions{Root: root, Commit: commit, Private: buildViewPrivateFixture(t), RealGit: realGit, Executable: binary, Inherited: os.Environ()}, func(ctx context.Context, c buildViewCommand, w io.Writer) (int, []byte, error) {
		status, diagnostic, err := runBuildViewNative(ctx, c, w)
		if len(diagnostic) != 0 {
			t.Logf("synthetic native fixture diagnostic: %s", diagnostic)
		}
		return status, diagnostic, err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if len(graph.roots) != 1 {
		t.Fatal("unexpected native graph roots")
	}
	privateConfig, err := os.ReadFile(filepath.Join(string(graph.roots[0].PrivateCommon), "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(privateConfig), "include") || !strings.Contains(string(privateConfig), "captured") {
		t.Fatal("private config did not flatten native selected include")
	}
	if err := graph.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Original CRLF is normalized to LF in the committed blob. Acceptance above
	// preserves the native predicate rather than comparing worktree raw hashes.
	// A subsequent identical-byte rewrite may be native stat-dirty; do not refresh
	// or assume content equivalence makes diff-index --quiet accept it.
	if err := os.WriteFile(filepath.Join(root, "tracked"), []byte("original\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Root/native identity and config remain unchanged; the direct oracle is a
	// fixture-only native source-index observation, never a production path.
	oracleStatus, _, oracleErr := runBuildViewNative(context.Background(), buildViewCommand{Executable: realGit, Dir: root, Env: graph.env, Args: append(append([]string{}, buildViewGitPrefix...), "diff-index", "--quiet", "--no-ext-diff", "--no-textconv", commit, "--")}, io.Discard)
	if oracleErr != nil {
		t.Fatal(oracleErr)
	}
	if (graph.check(context.Background()) == nil) != (oracleStatus == 0) {
		t.Fatal("private comparison changed native stat/racy decision")
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "selected"), []byte("[fixture]\nmarker = changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := graph.check(context.Background()); err == nil {
		t.Fatal("changed captured config accepted")
	}
}

func TestBuildViewGraphNestedStatusNeverReadsSourceIndex(t *testing.T) {
	root := repoWith(t, map[string][]byte{"tracked": []byte("root\n")})
	commitAll(t, root)
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, child, "init", "-q", "--template=", "-b", "main")
	if err := os.WriteFile(filepath.Join(child, "tracked"), []byte("child\n"), 0600); err != nil {
		t.Fatal(err)
	}
	head := commitAll(t, child)
	git(t, root, "update-index", "--add", "--cacheinfo", "160000,"+head+",child")
	commit := commitAll(t, root)
	git(t, root, "update-index", "--split-index")
	git(t, child, "update-index", "--split-index")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	graph, err := prepareBuildViewGraph(context.Background(), buildViewGraphOptions{Root: root, Commit: commit, Private: buildViewPrivateFixture(t), RealGit: realGit, Executable: binary, Inherited: os.Environ()}, func(ctx context.Context, c buildViewCommand, w io.Writer) (int, []byte, error) {
		status, diagnostic, err := runBuildViewNative(ctx, c, w)
		if len(diagnostic) != 0 {
			t.Logf("synthetic native fixture diagnostic: %s", diagnostic)
		}
		return status, diagnostic, err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if len(graph.roots) != 2 {
		t.Fatal("initialized child not captured")
	}
	if err := graph.check(context.Background()); err != nil {
		t.Fatal("native nested status escaped private index/config binding", err)
	}
}

func TestBuildViewRouteRefusesUnexpectedArgvAndChangedChildBinding(t *testing.T) {
	if err := runBuildViewRoute(context.Background(), []string{"status", "--porcelain=2", "--untracked-files=all"}); err == nil {
		t.Fatal("unexpected child argv accepted")
	}
	root := repoWith(t, map[string][]byte{"tracked": []byte("root\n")})
	commit := commitAll(t, root)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	graph, err := prepareBuildViewGraph(context.Background(), buildViewGraphOptions{Root: root, Commit: commit, Private: buildViewPrivateFixture(t), RealGit: realGit, Executable: binary, Inherited: os.Environ()}, runBuildViewNativeFixture)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	t.Setenv(buildViewManifestVariable, graph.manifest)
	// No process cwd mutation is needed: the current source-test cwd is not a
	// captured worktree, so the route must refuse before native setup/exec.
	if err := runBuildViewRoute(context.Background(), []string{"status", "--porcelain=2"}); err == nil {
		t.Fatal("unbound current directory accepted")
	}
	if err := os.Chmod(graph.options.Private, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := readBuildViewManifest(graph.manifest); err == nil {
		t.Fatal("nonprivate manifest directory accepted")
	}
}

func TestBuildViewGraphUninitializedGitlinkPreservesNativePredicate(t *testing.T) {
	root := repoWith(t, map[string][]byte{"tracked": []byte("root\n")})
	head := commitAll(t, root)
	if err := os.Mkdir(filepath.Join(root, "absent"), 0700); err != nil {
		t.Fatal(err)
	}
	git(t, root, "update-index", "--add", "--cacheinfo", "160000,"+head+",absent")
	commit := commitAll(t, root)
	if err := os.Remove(filepath.Join(root, "absent")); err != nil {
		t.Fatal(err)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	graph, err := prepareBuildViewGraph(context.Background(), buildViewGraphOptions{Root: root, Commit: commit, Private: buildViewPrivateFixture(t), RealGit: realGit, Executable: binary, Inherited: os.Environ()}, runBuildViewNativeFixture)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if len(graph.roots) != 1 {
		t.Fatal("uninitialized gitlink synthesized a repository")
	}
	status, _, err := runBuildViewNativeFixture(context.Background(), buildViewCommand{Executable: realGit, Dir: root, Env: graph.env, Args: append(append([]string{}, buildViewGitPrefix...), "diff-index", "--quiet", "--no-ext-diff", "--no-textconv", commit, "--")}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if (graph.check(context.Background()) == nil) != (status == 0) {
		t.Fatal("uninitialized native predicate changed")
	}
	if err := os.MkdirAll(filepath.Join(root, "absent", ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := graph.check(context.Background()); err == nil {
		t.Fatal("changed uninitialized binding accepted")
	}
}

func runBuildViewNativeFixture(ctx context.Context, c buildViewCommand, out io.Writer) (int, []byte, error) {
	return runBuildViewNative(ctx, c, out)
}

func buildViewPrivateFixture(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func init() {
	// Test-compiler fixture entry only; production CLI handoff is separate.
	if handled, status := RunBuildCheckoutInternal(context.Background()); handled {
		os.Exit(status)
	}
}

func TestBuildViewGraphKeepsLinkedWorktreeRolesDistinct(t *testing.T) {
	main := repoWith(t, map[string][]byte{"tracked": []byte("main\n")})
	commitAll(t, main)
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, main, "worktree", "add", "-q", "-b", "linked-fixture", linked)
	git(t, main, "config", "extensions.worktreeConfig", "true")
	git(t, linked, "config", "--worktree", "fixture.role", "linked")
	commit := git(t, linked, "rev-parse", "HEAD")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	graph, err := prepareBuildViewGraph(context.Background(), buildViewGraphOptions{Root: linked, Commit: commit, Private: buildViewPrivateFixture(t), RealGit: realGit, Executable: binary, Inherited: os.Environ()}, runBuildViewNativeFixture)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	root := graph.roots[0]
	if root.PrivateGitdir == root.PrivateCommon {
		t.Fatal("linked metadata roles collapsed")
	}
	if err := graph.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(string(root.PrivateGitdir), "config.worktree"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "linked") {
		t.Fatal("per-worktree config lost")
	}
}
