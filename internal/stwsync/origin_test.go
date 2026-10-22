// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBehindOrigin(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	commit(t, dir, "a.txt", "one\n")
	base := runGit(t, dir, "rev-parse", "HEAD")
	commit(t, dir, "a.txt", "two\n")
	tip := runGit(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "branch", "old", base)
	runGit(t, dir, "checkout", "-q", "-b", "side", base)
	commit(t, dir, "b.txt", "side\n")
	side := runGit(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "checkout", "-q", "main")
	absent := strings.Repeat("ab", 20)
	for _, tc := range []struct {
		name, branch, head string
		behind, known      bool
	}{
		{"origin head is the local tip", "main", tip, false, true},
		{"origin head is an ancestor of the local tip", "main", base, false, true},
		{"origin head is present but ahead of the local branch", "old", tip, true, true},
		{"origin head is absent from the local store", "main", absent, true, true},
		{"origin head is present and diverged", "main", side, true, true},
		{"the local default branch does not exist", "missing", tip, false, false},
		{"a revision expression is not a branch", "main~1", tip, false, false},
		{"origin head is not a full SHA", "main", "main", false, false},
		{"origin head is not lower case", "main", strings.ToUpper(tip), false, false},
	} {
		behind, known := BehindOrigin(context.Background(), dir, tc.branch, tc.head)
		if behind != tc.behind || known != tc.known {
			t.Errorf("%s: behind=%v known=%v, want %v %v", tc.name, behind, known, tc.behind, tc.known)
		}
	}
}

// The lookup decides from the local object store alone: a commit that origin has and the clone lacks is
// still absent afterwards, so nothing was fetched.
func TestBehindOriginDoesNotFetch(t *testing.T) {
	origin := t.TempDir()
	runGit(t, origin, "init", "-q", "-b", "main")
	commit(t, origin, "a.txt", "one\n")
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, filepath.Dir(clone), "clone", "-q", origin, clone)
	commit(t, origin, "a.txt", "two\n")
	newHead := runGit(t, origin, "rev-parse", "HEAD")
	before := runGit(t, clone, "for-each-ref")
	behind, known := BehindOrigin(context.Background(), clone, "main", newHead)
	if !behind || !known {
		t.Fatalf("%v %v", behind, known)
	}
	cmd := exec.Command("git", "-C", clone, "cat-file", "-e", newHead+"^{commit}")
	if cmd.Run() == nil {
		t.Fatal("the commit of origin was fetched")
	}
	if after := runGit(t, clone, "for-each-ref"); after != before {
		t.Fatalf("refs changed: %q -> %q", before, after)
	}
}

func TestBehindOriginWithoutARepositoryIsUnknown(t *testing.T) {
	if behind, known := BehindOrigin(context.Background(), t.TempDir(), "main", strings.Repeat("ab", 20)); behind || known {
		t.Fatalf("%v %v", behind, known)
	}
}

// In a partial clone Git fetches a missing object from the promisor remote on demand; the lookup must
// not: the commit stays absent, and the answer is that the clone is behind.
func TestBehindOriginDoesNotLazyFetchInAPartialClone(t *testing.T) {
	origin := t.TempDir()
	runGit(t, origin, "init", "-q", "-b", "main")
	runGit(t, origin, "config", "uploadpack.allowFilter", "true")
	runGit(t, origin, "config", "uploadpack.allowAnySHA1InWant", "true")
	commit(t, origin, "a.txt", "one\n")
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, filepath.Dir(clone), "-c", "protocol.file.allow=always", "clone", "-q", "--filter=blob:none", "--no-checkout", "file://"+origin, clone)
	if runGit(t, clone, "config", "remote.origin.promisor") != "true" {
		t.Fatal("the fixture is not a partial clone")
	}
	commit(t, origin, "a.txt", "two\n")
	newHead := runGit(t, origin, "rev-parse", "HEAD")
	behind, known := BehindOrigin(context.Background(), clone, "main", newHead)
	if !behind || !known {
		t.Fatalf("%v %v", behind, known)
	}
	if exec.Command("git", "-C", clone, "--no-lazy-fetch", "cat-file", "-e", newHead+"^{commit}").Run() == nil {
		t.Fatal("the commit of origin was fetched on demand")
	}
}

// Git before 2.44 does not know --no-lazy-fetch and exits 129: the answer is undecided, never a guess.
func TestBehindOriginIsUndecidedWhenGitRefusesTheOption(t *testing.T) {
	tip := strings.Repeat("a", 40)
	for _, failing := range []string{"show-ref", "rev-parse", "merge-base"} {
		var calls [][]string
		run := func(_ context.Context, args ...string) (gitOutput, error) {
			calls = append(calls, args)
			if args[0] != "--no-lazy-fetch" {
				t.Fatalf("a call without --no-lazy-fetch first: %v", args)
			}
			if args[1] == failing {
				return gitOutput{Code: 129}, nil
			}
			return gitOutput{Out: tip + "\n"}, nil
		}
		behind, known := behindOrigin(context.Background(), run, "main", strings.Repeat("b", 40))
		if behind || known {
			t.Errorf("%s: %v %v", failing, behind, known)
		}
	}
}
