// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	streamingFixtureRefs      = 150
	streamingFixtureWorktrees = 120
)

// fixtureGitStreamingInventory builds a temporary repository whose native
// for-each-ref and worktree list outputs each exceed the 64 KiB buffered bound
// (#80): 150 long branch names, 120 of them checked out in long-path worktrees.
func fixtureGitStreamingInventory(t *testing.T) (dir string, refBytes, worktreeBytes int) {
	t.Helper()
	dir = fixtureGit(t)
	long := func(prefix string, i int) string {
		return fmt.Sprintf("%s-%03d-%s", prefix, i, strings.Repeat("n", 130))
	}
	holder := gitTempDir(t)
	for i := 0; i < streamingFixtureRefs; i++ {
		branch := fmt.Sprintf("%s/%s/%s", long("topic", i), long("part", i), long("leaf", i))
		if i < streamingFixtureWorktrees {
			deep := filepath.Join(holder, long("a", i), long("b", i))
			if e := os.MkdirAll(deep, 0o700); e != nil {
				t.Fatal(e)
			}
			gitFixture(t, dir, "worktree", "add", "-q", "-b", branch, filepath.Join(deep, long("w", i)))
		} else {
			gitFixture(t, dir, "branch", branch)
		}
	}
	refs := gitFixture(t, dir, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)%00%(objecttype)%00")
	registrations := gitFixture(t, dir, "worktree", "list", "--porcelain", "-z")
	if len(refs) <= nativeOutputLimit || len(registrations) <= nativeOutputLimit {
		t.Fatalf("fixture must exceed 64 KiB: refs=%d worktrees=%d", len(refs), len(registrations))
	}
	return dir, len(refs), len(registrations)
}

func TestTidyV1PlansHundredsOfRefsAndWorktreesOverBufferedBound(t *testing.T) {
	dir, refBytes, worktreeBytes := fixtureGitStreamingInventory(t)
	base := gitFixture(t, dir, "symbolic-ref", "HEAD")
	start := time.Now()
	o, e := (gitTidyAdapter{}).Observe(context.Background(), dir, base)
	elapsed := time.Since(start)
	if e != nil {
		t.Fatalf("v1 plan refused: %v", e)
	}
	if len(o.Candidates) != streamingFixtureRefs+1 || len(o.WorkspacePaths) != streamingFixtureWorktrees+1 {
		t.Fatalf("v1 candidates=%d workspaces=%d", len(o.Candidates), len(o.WorkspacePaths))
	}
	t.Logf("v1 plan: %s, for-each-ref %d bytes, worktree list %d bytes, 0 requests (offline)", elapsed, refBytes, worktreeBytes)
}

func TestTidyV3PlansHundredsOfRefsAndWorktreesOverBufferedBound(t *testing.T) {
	dir, refBytes, worktreeBytes := fixtureGitStreamingInventory(t)
	base := gitFixture(t, dir, "symbolic-ref", "HEAD")
	start := time.Now()
	o, e := observeTidyV3Git(context.Background(), dir, base)
	elapsed := time.Since(start)
	if e != nil {
		t.Fatalf("v3 plan refused: %v", e)
	}
	if len(o.Native.References) != streamingFixtureRefs+1 || len(o.Native.Registrations) != streamingFixtureWorktrees+1 {
		t.Fatalf("v3 references=%d registrations=%d", len(o.Native.References), len(o.Native.Registrations))
	}
	t.Logf("v3 plan: %s, for-each-ref %d bytes, worktree list %d bytes, 0 requests (offline)", elapsed, refBytes, worktreeBytes)
}

// fixtureGitBranchCount creates a repository with exactly n local branches.
func fixtureGitBranchCount(t *testing.T, n int) (dir, base string) {
	t.Helper()
	dir = fixtureGit(t)
	base = gitFixture(t, dir, "symbolic-ref", "HEAD")
	head := gitFixture(t, dir, "rev-parse", "HEAD")
	var input strings.Builder
	for i := 1; i < n; i++ {
		fmt.Fprintf(&input, "create refs/heads/b%04d %s\n", i, head)
	}
	cmd := exec.Command("git", "-C", dir, "update-ref", "--stdin")
	cmd.Stdin = strings.NewReader(input.String())
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("update-ref: %v %s", e, out)
	}
	return dir, base
}

// The collection reads admit 1,024 records. The stored plan and result keep the
// Spec 029 aggregate bound of 1,024 records over references, registrations and
// the other plan lists, so 1,024 references plus the primary registration still
// refuse with the existing combined code; see Spec 031 FR-025.
func TestTidyPlansRefCountBoundaryThroughStreamedReads(t *testing.T) {
	ctx := context.Background()
	// 1,023 branches plus the primary registration are 1,024 aggregate records.
	dir, base := fixtureGitBranchCount(t, 1023)
	if _, e := (gitTidyAdapter{}).Observe(ctx, dir, base); e != nil {
		t.Fatalf("v1 at the aggregate bound: %v", e)
	}
	if _, e := observeTidyV3Git(ctx, dir, base); e != nil {
		t.Fatalf("v3 at the aggregate bound: %v", e)
	}
	// 1,024 references: the streamed collection read passes, the aggregate bound refuses.
	dir, base = fixtureGitBranchCount(t, 1024)
	if _, e := (gitAdapter{}).Observe(ctx, dir, base); e != nil {
		t.Fatalf("1,024 references through the streamed read: %v", e)
	}
	if _, e := (gitTidyAdapter{}).Observe(ctx, dir, base); !strings.HasPrefix(collectionCode(e), "tidy-observation-limit:") {
		t.Fatalf("v1 aggregate bound: %s", collectionCode(e))
	}
	if _, e := observeTidyV3Git(ctx, dir, base); !strings.HasPrefix(collectionCode(e), "record-limit:") {
		t.Fatalf("v3 aggregate bound: %s", collectionCode(e))
	}
	// 1,025 references exceed the collection bound itself.
	dir, base = fixtureGitBranchCount(t, 1025)
	if _, e := (gitAdapter{}).Observe(ctx, dir, base); !strings.HasPrefix(collectionCode(e), "git-observation-limit:") {
		t.Fatalf("snapshot over the bound: %s", collectionCode(e))
	}
	if _, e := (gitTidyAdapter{}).Observe(ctx, dir, base); !strings.HasPrefix(collectionCode(e), "git-observation-limit:") {
		t.Fatalf("v1 over the bound: %s", collectionCode(e))
	}
	if _, e := observeTidyV3Git(ctx, dir, base); !strings.HasPrefix(collectionCode(e), "git-observation-limit:") {
		t.Fatalf("v3 over the bound: %s", collectionCode(e))
	}
}
