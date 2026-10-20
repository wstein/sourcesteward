// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"os"
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
