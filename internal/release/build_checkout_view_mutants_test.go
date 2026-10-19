// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// These tests pin single guarded lines of the checkout view that earlier tests passed
// without. Each one fails when the named guard is removed or inverted.

type buildViewFixture struct {
	root, commit, git, binary string
}

func newBuildViewFixture(t *testing.T, files map[string][]byte) buildViewFixture {
	t.Helper()
	if files == nil {
		files = map[string][]byte{"tracked": []byte("root\n")}
	}
	root := repoWith(t, files)
	commit := commitAll(t, root)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return buildViewFixture{root: root, commit: commit, git: realGit, binary: binary}
}

func (f buildViewFixture) prepare(t *testing.T, native buildViewNative) (*buildViewGraph, error) {
	t.Helper()
	if native == nil {
		native = runBuildViewNativeFixture
	}
	return prepareBuildViewGraph(context.Background(), buildViewGraphOptions{Root: f.root, Commit: f.commit, Private: buildViewPrivateFixture(t), RealGit: f.git, Executable: f.binary, Inherited: os.Environ()}, native)
}

func (f buildViewFixture) mustPrepare(t *testing.T, native buildViewNative) *buildViewGraph {
	t.Helper()
	g, err := f.prepare(t, native)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

// routeAt binds the process to the graph's manifest, enters dir and observes what the
// route would execute instead of replacing the test process.
func routeAt(t *testing.T, g *buildViewGraph, dir string) *[]buildViewCommand {
	t.Helper()
	t.Setenv(buildViewManifestVariable, g.manifest)
	t.Chdir(dir)
	var executed []buildViewCommand
	previous := buildViewExec
	buildViewExec = func(c buildViewCommand) error { executed = append(executed, c); return nil }
	t.Cleanup(func() { buildViewExec = previous })
	return &executed
}

func TestBuildViewRouteAdmitsOnlyTheNestedStatusArgv(t *testing.T) {
	f := newBuildViewFixture(t, nil)
	g := f.mustPrepare(t, nil)
	executed := routeAt(t, g, f.root)
	for _, args := range [][]string{
		{"log"},
		{"status"},
		{"status", "--porcelain"},
		{"status", "--porcelain=2", "-uall"},
		{"status", "--porcelain=2", "--untracked-files=all"},
		{"status", "--porcelain=2", "-uno", "extra"},
		{"diff", "--porcelain=2"},
		{},
	} {
		if err := runBuildViewRoute(context.Background(), args); err == nil {
			t.Fatalf("argv %q accepted", args)
		}
	}
	if len(*executed) != 0 {
		t.Fatalf("refused argv reached native Git: %v", *executed)
	}
	for _, args := range [][]string{{"status", "--porcelain=2"}, {"status", "--porcelain=2", "-uno"}} {
		*executed = nil
		if err := runBuildViewRoute(context.Background(), args); err != nil {
			t.Fatalf("argv %q refused: %v", args, err)
		}
		if len(*executed) != 1 {
			t.Fatalf("argv %q executed %d times", args, len(*executed))
		}
		c := (*executed)[0]
		if c.Executable != f.git || !slices.Equal(c.Args[len(c.Args)-len(args):], args) {
			t.Fatalf("unexpected native command %+v", c)
		}
	}
}

func rewriteBuildViewManifest(t *testing.T, path string, edit func(*buildViewManifest)) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m buildViewManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	edit(&m)
	raw, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestBuildViewRouteRefusesARootWhoseWorktreePathDiffers(t *testing.T) {
	f := newBuildViewFixture(t, nil)
	g := f.mustPrepare(t, nil)
	// The working directory keeps the bound identity, but the recorded canonical path
	// is another one: identity alone must not admit the route.
	rewriteBuildViewManifest(t, g.manifest, func(m *buildViewManifest) { m.Roots[0].Worktree = buildViewPath(filepath.Join(f.root, "other")) })
	executed := routeAt(t, g, f.root)
	if err := runBuildViewRoute(context.Background(), []string{"status", "--porcelain=2"}); err == nil || len(*executed) != 0 {
		t.Fatalf("route admitted a worktree path that differs from the manifest: %v %v", err, *executed)
	}
}

func TestBuildViewRouteRefusesAnotherExecutable(t *testing.T) {
	f := newBuildViewFixture(t, nil)
	f.binary = f.git // the manifest names an executable that is not this process
	g := f.mustPrepare(t, nil)
	executed := routeAt(t, g, f.root)
	if _, err := readBuildViewManifest(g.manifest); err == nil {
		t.Fatal("manifest naming another executable accepted")
	}
	if err := runBuildViewRoute(context.Background(), []string{"status", "--porcelain=2"}); err == nil || len(*executed) != 0 {
		t.Fatalf("route ran for another executable: %v %v", err, *executed)
	}
}

func TestBuildViewGraphRefusesNativeDiagnosticsOnSuccess(t *testing.T) {
	f := newBuildViewFixture(t, nil)
	_, err := f.prepare(t, func(ctx context.Context, c buildViewCommand, w io.Writer) (int, []byte, error) {
		status, _, err := runBuildViewNative(ctx, c, w)
		return status, []byte("warning: a diagnostic on a zero exit"), err
	})
	if err == nil {
		t.Fatal("native diagnostics on a successful command were accepted")
	}
}

func TestBuildViewGraphRefusesConfiguredConversionHelpers(t *testing.T) {
	for _, key := range []string{"filter.evil.clean", "filter.evil.process"} {
		f := newBuildViewFixture(t, nil)
		git(t, f.root, "config", key, "touch sentinel")
		if _, err := f.prepare(t, nil); err == nil || !strings.Contains(err.Error(), "conversion helper") {
			t.Fatalf("%s accepted: %v", key, err)
		}
	}
	f := newBuildViewFixture(t, nil)
	git(t, f.root, "config", "filter.inert.clean", "")
	f.mustPrepare(t, nil)
}

func TestBuildViewGraphNeverRunsAConfiguredFsmonitor(t *testing.T) {
	f := newBuildViewFixture(t, nil)
	sentinel := filepath.Join(t.TempDir(), "ran")
	hook := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+sentinel+"'\nprintf '\\0'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	git(t, f.root, "config", "core.fsmonitor", hook)
	g := f.mustPrepare(t, nil)
	if err := g.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("the configured fsmonitor hook ran inside the native comparison")
	}
}

// A configuration source that is rewritten with the same bytes changes no observed value,
// so only its own binding can notice.
func TestBuildViewGraphBindsEveryConfigurationOrigin(t *testing.T) {
	f := newBuildViewFixture(t, nil)
	g := f.mustPrepare(t, nil)
	if err := g.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(f.root, ".git", "config")
	raw, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	replacement := config + ".new"
	if err := os.WriteFile(replacement, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, config); err != nil {
		t.Fatal(err)
	}
	if err := g.check(context.Background()); err == nil {
		t.Fatal("a configuration source replaced with identical bytes was accepted")
	}
}

// Metadata that moves while the native comparison itself runs is caught only by the
// validation that follows it.
func TestBuildViewGraphRevalidatesAfterTheNativeComparison(t *testing.T) {
	f := newBuildViewFixture(t, nil)
	armed := false
	g := f.mustPrepare(t, func(ctx context.Context, c buildViewCommand, w io.Writer) (int, []byte, error) {
		status, diagnostic, err := runBuildViewNative(ctx, c, w)
		if armed && slices.Contains(c.Args, "diff-index") {
			file, e := os.OpenFile(filepath.Join(f.root, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
			if e != nil {
				t.Error(e)
			} else {
				_, _ = file.WriteString("[fixture]\n\tmoved = during-comparison\n")
				file.Close()
			}
		}
		return status, diagnostic, err
	})
	if err := g.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	armed = true
	if err := g.check(context.Background()); err == nil {
		t.Fatal("metadata changed during the native comparison was accepted")
	}
}

func TestBuildViewMetadataSnapshotBindsContentDigest(t *testing.T) {
	main := repoWith(t, map[string][]byte{"tracked": []byte("main\n")})
	commitAll(t, main)
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, main, "worktree", "add", "-q", "-b", "linked-digest", linked)
	f := buildViewFixture{root: linked, commit: git(t, linked, "rev-parse", "HEAD")}
	f.git, _ = exec.LookPath("git")
	f.binary, _ = os.Executable()
	g := f.mustPrepare(t, nil)
	for name, snapshot := range map[string]buildViewMetadataSnapshot{"anchor": g.roots[0].Anchor, "common pointer": g.roots[0].CommonPointer} {
		if !snapshot.HasDigest {
			t.Fatalf("%s snapshot lost its digest", name)
		}
		if err := snapshot.verify(context.Background()); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		snapshot.Digest[0] ^= 1 // same identity, size and timestamps, different content
		if err := snapshot.verify(context.Background()); err == nil {
			t.Fatalf("%s snapshot accepted a content digest mismatch", name)
		}
	}
}

// The comparison screens configured conversion helpers again after the build, before the
// frozen view revalidates: the refusal names the helper, not just a changed configuration.
func TestBuildReleaseRescreensConversionHelpersAfterTheBuild(t *testing.T) {
	dir, commit := buildViewWiringFixture(t)
	fake := buildViewWiringRunner(t, dir, func() {
		f, err := os.OpenFile(filepath.Join(dir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		_, _ = f.WriteString("[filter \"late\"]\n\tclean = touch sentinel\n")
	})
	r := buildRelease(context.Background(), BuildOptions{Mode: "ci", Path: dir, Commit: commit, Version: "v1.2.3", STWVersion: "v1.0.0", Out: filepath.Join(t.TempDir(), "new")}, fake, nil)
	if r.Error == nil || r.Error.Code != "checkout-mismatch" || !strings.Contains(r.Error.Message, "conversion helper") {
		t.Fatalf("a conversion helper configured during the build was not screened: %+v", r.Error)
	}
}

// Native output is bounded like every other Git output: an oversized response is the
// refusal release-limit, never an unbounded private file.
func TestBuildViewGraphBoundsNativeOutput(t *testing.T) {
	t.Run("setup query", func(t *testing.T) {
		f := newBuildViewFixture(t, nil)
		_, err := f.prepare(t, func(ctx context.Context, c buildViewCommand, w io.Writer) (int, []byte, error) {
			if slices.Contains(c.Args, "--show-toplevel") {
				_, err := w.Write([]byte(strings.Repeat("a", int(2*buildViewQueryLimit)) + "\n"))
				return 0, nil, err
			}
			return runBuildViewNative(ctx, c, w)
		})
		if !errors.Is(err, errReleaseLimit) {
			t.Fatalf("oversized setup response not refused as a limit: %v", err)
		}
	})
	t.Run("listing", func(t *testing.T) {
		files := map[string][]byte{}
		for i := 0; i < 64; i++ {
			files[fmt.Sprintf("file-%02d", i)] = []byte("x")
		}
		f := newBuildViewFixture(t, files)
		previous := buildViewListingLimit
		buildViewListingLimit = 512
		t.Cleanup(func() { buildViewListingLimit = previous })
		if _, err := f.prepare(t, nil); !errors.Is(err, errReleaseLimit) {
			t.Fatalf("oversized listing not refused as a limit: %v", err)
		}
	})
}

func TestBuildViewGraphBoundsGitlinkRecursion(t *testing.T) {
	root := repoWith(t, map[string][]byte{"tracked": []byte("root\n")})
	commitAll(t, root)
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, child, "init", "-q", "-b", "main")
	write(t, child, "tracked", []byte("child\n"))
	head := commitAll(t, child)
	git(t, root, "update-index", "--add", "--cacheinfo", "160000,"+head+",child")
	commit := commitAll(t, root)
	f := buildViewFixture{root: root, commit: commit}
	f.git, _ = exec.LookPath("git")
	f.binary, _ = os.Executable()
	if g := f.mustPrepare(t, nil); len(g.roots) != 2 {
		t.Fatalf("roots %d", len(g.roots))
	}
	previous := buildViewMaxRoots
	buildViewMaxRoots = 1
	t.Cleanup(func() { buildViewMaxRoots = previous })
	if _, err := f.prepare(t, nil); !errors.Is(err, errReleaseLimit) {
		t.Fatalf("gitlink recursion beyond the bound not refused as a limit: %v", err)
	}
}

// A clean checkout that uses a sparse index is refused, because native Git expands the
// index and reports it on its diagnostic stream; the refusal names the sparse index.
// A cone-mode sparse checkout with a full index is accepted.
func TestBuildViewGraphRefusesSparseIndexAndNamesIt(t *testing.T) {
	for name, sparseIndex := range map[string]bool{"sparse index": true, "full index": false} {
		t.Run(name, func(t *testing.T) {
			f := newBuildViewFixture(t, map[string][]byte{"a/f": []byte("1\n"), "b/f": []byte("2\n"), "top": []byte("3\n")})
			mode := "--no-sparse-index"
			if sparseIndex {
				mode = "--sparse-index"
			}
			git(t, f.root, "sparse-checkout", "init", "--cone", mode)
			git(t, f.root, "sparse-checkout", "set", "a")
			g, err := f.prepare(t, nil)
			if g != nil {
				defer g.Close()
			}
			if !sparseIndex {
				if err != nil {
					t.Fatal("a sparse checkout with a full index was refused: ", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "sparse index") {
				t.Fatalf("a sparse index was not refused by name: %v", err)
			}
		})
	}
}
