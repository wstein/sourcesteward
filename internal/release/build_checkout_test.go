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
	"time"
)

// Trusted native-read protocol fixture: only this already compiled Go test
// binary executes. The parent sets the opt-in variable after initialization.
func init() {
	mode := os.Getenv("STW_BUILD_CONFIG_PROTOCOL_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "order":
		for _, arg := range os.Args {
			switch arg {
			case "config":
				fmt.Fprint(os.Stdout, "filter.fixture.clean\nsynthetic-helper\x00")
				os.Exit(0)
			case "rev-parse":
				fmt.Fprintln(os.Stdout, strings.Repeat("a", 40))
				os.Exit(0)
			case "diff-index":
				if err := os.WriteFile(os.Getenv("STW_BUILD_NATIVE_MARKER"), []byte(os.Getenv("GITHUB_TOKEN")), 0600); err != nil {
					os.Exit(46)
				}
				os.Exit(0)
			}
		}
		os.Exit(47)
	case "active":
		fmt.Fprint(os.Stdout, "filter.fixture.process\nsynthetic-helper\x00")
	case "no-matches":
		os.Exit(1)
	case "empty":
		fmt.Fprint(os.Stdout, "filter.fixture.clean\n\x00filter.fixture.process\n\x00")
	case "malformed":
		fmt.Fprint(os.Stdout, "unrecognized\n\x00")
	case "unterminated":
		fmt.Fprint(os.Stdout, "filter.fixture.clean\n")
	case "tail":
		fmt.Fprint(os.Stdout, "filter.fixture.clean\n\x00tail")
	case "nonzero":
		os.Exit(3)
	case "record-nonzero":
		fmt.Fprint(os.Stdout, "filter.fixture.clean\n\x00")
		os.Exit(1)
	case "stderr":
		fmt.Fprint(os.Stderr, "synthetic diagnostic")
		os.Exit(1)
	case "overflow":
		fmt.Fprint(os.Stderr, strings.Repeat("x", (64<<10)+1))
		os.Exit(1)
	case "deadline":
		time.Sleep(time.Hour)
	default:
		os.Exit(45)
	}
	os.Exit(0)
}
func TestBuildCheckoutHelperNativeProtocolFailsClosed(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err = os.Symlink(binary, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	for _, mode := range []string{"no-matches", "empty", "active", "malformed", "unterminated", "tail", "nonzero", "record-nonzero", "stderr", "overflow", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("STW_BUILD_CONFIG_PROTOCOL_HELPER", mode)
			timeout := 5 * time.Second
			if mode == "deadline" {
				timeout = 150 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			err := checkBuildConversionHelpers(ctx, t.TempDir())
			if mode == "empty" || mode == "no-matches" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("unsafe configuration observation accepted")
			}
			if mode == "overflow" && !errors.Is(err, errReleaseLimit) {
				t.Fatalf("lost typed limit: %v", err)
			}
			if mode == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lost deadline: %v", err)
			}
		})
	}
}
func TestBuildCheckoutAllowsInertConfiguration(t *testing.T) {
	for _, mode := range []string{"none", "empty-clean", "empty-process", "smudge-only"} {
		t.Run(mode, func(t *testing.T) {
			dir := repoWith(t, map[string][]byte{"tracked": []byte("original\n")})
			commit := commitAll(t, dir)
			switch mode {
			case "empty-clean":
				git(t, dir, "config", "filter.fixture.clean", "")
			case "empty-process":
				git(t, dir, "config", "filter.fixture.process", "")
			case "smudge-only":
				git(t, dir, "config", "filter.fixture.smudge", "unused-synthetic-command")
			}
			if err := checkBuildCheckout(context.Background(), dir, commit); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestBuildConversionConfigStreamsLargeEmptyMetadata(t *testing.T) {
	raw := "filter." + strings.Repeat("a", 1<<20) + ".clean\n\x00"
	if n, err := inspectBuildConversionConfig(strings.NewReader(raw)); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestBuildCheckoutHelperInspectionPrecedesPotentialConversion(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err = os.Symlink(binary, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "comparison-marker")
	t.Setenv("PATH", bin)
	t.Setenv("STW_BUILD_CONFIG_PROTOCOL_HELPER", "order")
	t.Setenv("STW_BUILD_NATIVE_MARKER", marker)
	t.Setenv("GITHUB_TOKEN", "synthetic-review-secret")
	err = checkBuildCheckout(context.Background(), t.TempDir(), strings.Repeat("a", 40))
	if err == nil || !strings.Contains(err.Error(), "checkout conversion helper") {
		t.Fatalf("preflight refused incorrectly: %v", err)
	}
	if raw, err := os.ReadFile(marker); err == nil {
		t.Fatalf("native comparison saw secret before helper refusal: %q", raw)
	}
}

func TestBuildCheckoutDoesNotExecuteNestedConversionHelpers(t *testing.T) {
	root := repoWith(t, map[string][]byte{"tracked": []byte("parent\n")})
	child := filepath.Join(root, "nested")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	git(t, child, "init", "-q", "-b", "main")
	write(t, child, ".gitattributes", []byte("tracked filter=reviewer\n"))
	write(t, child, "tracked", []byte("original\n"))
	childCommit := commitAll(t, child)
	git(t, root, "update-index", "--add", "--cacheinfo", "160000,"+childCommit+",nested")
	commit := commitAll(t, root)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	git(t, child, "config", "filter.reviewer.clean", binary+" -test.run=^TestReviewerSyntheticFilterHelper$")
	marker := filepath.Join(t.TempDir(), "marker")
	t.Setenv("STW_REVIEWER_FILTER_HELPER", "1")
	t.Setenv("STW_REVIEWER_FILTER_MARKER", marker)
	t.Setenv("GITHUB_TOKEN", "synthetic-review-secret")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(child, "tracked"), old, old); err != nil {
		t.Fatal(err)
	}
	_ = checkBuildCheckout(context.Background(), root, commit)
	if raw, err := os.ReadFile(marker); err == nil {
		t.Fatalf("nested checkout conversion helper saw synthetic secret: %q", raw)
	}
}

func TestBuildCheckoutPreservesNestedCleanAndDirtyControls(t *testing.T) {
	root := repoWith(t, map[string][]byte{"tracked": []byte("parent\n")})
	child := filepath.Join(root, "nested")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	git(t, child, "init", "-q", "-b", "main")
	write(t, child, "tracked", []byte("original\n"))
	childCommit := commitAll(t, child)
	git(t, root, "update-index", "--add", "--cacheinfo", "160000,"+childCommit+",nested")
	commit := commitAll(t, root)
	if err := checkBuildCheckout(context.Background(), root, commit); err != nil {
		t.Fatalf("clean nested checkout: %v", err)
	}
	write(t, child, "tracked", []byte("different tracked bytes\n"))
	if err := checkBuildCheckout(context.Background(), root, commit); err == nil {
		t.Fatal("dirty nested checkout accepted")
	}
}

func TestRound4RedirectedNestedWorktreeDoesNotBypassHelperScreening(t *testing.T) {
	root := repoWith(t, map[string][]byte{"tracked": []byte("root\n")})
	child := filepath.Join(root, "nested")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	git(t, child, "init", "-q", "-b", "main")
	actual := t.TempDir()
	grand := filepath.Join(actual, "deep")
	if err := os.Mkdir(grand, 0755); err != nil {
		t.Fatal(err)
	}
	git(t, grand, "init", "-q", "-b", "main")
	write(t, grand, ".gitattributes", []byte("tracked filter=reviewer\n"))
	write(t, grand, "tracked", []byte("original\n"))
	grandCommit := commitAll(t, grand)
	git(t, child, "update-index", "--add", "--cacheinfo", "160000,"+grandCommit+",deep")
	childCommit := git(t, child, "-c", "user.name=Review", "-c", "user.email=review@example.invalid", "commit", "-qm", "gitlink fixture")
	_ = childCommit
	childID := git(t, child, "rev-parse", "HEAD")
	git(t, root, "update-index", "--add", "--cacheinfo", "160000,"+childID+",nested")
	commit := commitAll(t, root)
	git(t, child, "config", "core.worktree", actual)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	git(t, grand, "config", "filter.reviewer.clean", binary+" -test.run=^TestReviewerSyntheticFilterHelper$")
	marker := filepath.Join(t.TempDir(), "marker")
	t.Setenv("STW_REVIEWER_FILTER_HELPER", "1")
	t.Setenv("STW_REVIEWER_FILTER_MARKER", marker)
	t.Setenv("GITHUB_TOKEN", "synthetic-review-secret")
	old := time.Now().Add(-time.Hour)
	if err = os.Chtimes(filepath.Join(grand, "tracked"), old, old); err != nil {
		t.Fatal(err)
	}
	err = checkBuildCheckout(context.Background(), root, commit)
	if b, e := os.ReadFile(marker); e == nil {
		t.Fatalf("redirected nested helper inherited %q; checkout error=%v", b, err)
	}
	t.Logf("checkout refused/result: %v", err)
}

func TestBuildCheckoutAllowsMatchingConfiguredWorktree(t *testing.T) {
	root := repoWith(t, map[string][]byte{"tracked": []byte("ordinary\n")})
	commit := commitAll(t, root)
	git(t, root, "config", "core.worktree", root)
	if err := checkBuildCheckout(context.Background(), root, commit); err != nil {
		t.Fatalf("matching worktree refused: %v", err)
	}
}
func TestBuildCheckoutAllowsOrdinaryLinkedNestedWorktree(t *testing.T) {
	source := repoWith(t, map[string][]byte{"tracked": []byte("nested\n")})
	childCommit := commitAll(t, source)
	root := repoWith(t, map[string][]byte{"tracked": []byte("parent\n")})
	child := filepath.Join(root, "nested")
	git(t, source, "worktree", "add", "--detach", child, childCommit)
	git(t, root, "update-index", "--add", "--cacheinfo", "160000,"+childCommit+",nested")
	commit := commitAll(t, root)
	if err := checkBuildCheckout(context.Background(), child, childCommit); err != nil {
		t.Fatalf("linked checkout refused: %v", err)
	}
	if err := checkBuildCheckout(context.Background(), root, commit); err != nil {
		t.Fatalf("linked nested checkout refused: %v", err)
	}
}
