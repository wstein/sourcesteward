// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGitTidyNativeAncestryAndNonProofs(t *testing.T) {
	d := fixtureGit(t)
	root := gitFixture(t, d, "rev-parse", "HEAD")
	gitFixture(t, d, "branch", "integrated", root)
	gitFixture(t, d, "commit", "--allow-empty", "-qm", "advance base")
	base := gitFixture(t, d, "symbolic-ref", "HEAD")
	gitFixture(t, d, "branch", "base-alias", "HEAD")
	gitFixture(t, d, "checkout", "-qb", "empty-unmerged")
	gitFixture(t, d, "commit", "--allow-empty", "-qm", "unintegrated empty commit")
	gitFixture(t, d, "config", "branch.empty-unmerged.remote", "absent")
	gitFixture(t, d, "config", "branch.empty-unmerged.merge", "refs/heads/gone")
	gitFixture(t, d, "checkout", "-q", base[len("refs/heads/"):])
	index, _ := os.ReadFile(filepath.Join(d, ".git", "index"))
	config, _ := os.ReadFile(filepath.Join(d, ".git", "config"))
	o, e := (gitTidyAdapter{}).Observe(context.Background(), d, base)
	if e != nil {
		t.Fatal(e)
	}
	got := map[string]TidyCandidate{}
	for _, c := range o.Candidates {
		got[c.Name] = c
	}
	if got["refs/heads/integrated"].Decision != "delete" || got["refs/heads/integrated"].Proof != "native-ancestry" {
		t.Fatalf("missing native proof: %+v", o)
	}
	for _, name := range []string{base, "refs/heads/base-alias", "refs/heads/empty-unmerged"} {
		if got[name].Decision != "retain" {
			t.Fatalf("unsafe candidate %s: %+v", name, got[name])
		}
	}
	for name, want := range map[string][]byte{"index": index, "config": config} {
		after, e := os.ReadFile(filepath.Join(d, ".git", name))
		if e != nil || !reflect.DeepEqual(after, want) {
			t.Fatalf("source %s changed", name)
		}
	}
	if after := gitFixture(t, d, "rev-parse", "refs/heads/integrated"); after != root {
		t.Fatal("planning changed branch")
	}
}

func TestGitTidyProtectsDetachedAndPrunableWorktrees(t *testing.T) {
	d := fixtureGit(t)
	root := gitFixture(t, d, "rev-parse", "HEAD")
	gitFixture(t, d, "branch", "integrated", root)
	gitFixture(t, d, "commit", "--allow-empty", "-qm", "advance base")
	linked := filepath.Join(gitTempDir(t), "linked")
	gitFixture(t, d, "worktree", "add", "--detach", linked, root)
	for _, missing := range []bool{false, true} {
		if missing {
			if e := os.RemoveAll(linked); e != nil {
				t.Fatal(e)
			}
		}
		o, e := (gitTidyAdapter{}).Observe(context.Background(), d, "HEAD")
		if e != nil {
			t.Fatal(e)
		}
		for _, c := range o.Candidates {
			if c.Name == "refs/heads/integrated" && (c.Decision != "retain" || len(c.WorkspacePaths) != 1 || c.WorkspacePaths[0] != linked) {
				t.Fatalf("registered detached target unprotected: %+v", c)
			}
		}
	}
}

func TestGitTidyLiteralRefsAndMalformedRegistrations(t *testing.T) {
	d := fixtureGit(t)
	base := gitFixture(t, d, "symbolic-ref", "HEAD")
	gitFixture(t, d, "symbolic-ref", "refs/heads/alias", base)
	o, e := (gitTidyAdapter{}).Observe(context.Background(), d, base)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range o.Candidates {
		if c.Name == "refs/heads/alias" && c.Decision != "retain" {
			t.Fatal("symbolic local ref admitted")
		}
	}
	for _, raw := range []string{"worktree /tmp/x\x00HEAD bad\x00\x00", "worktree /tmp/x\x00HEAD " + o.Expected.BaseCommit + "\x00unknown x\x00\x00", "worktree /tmp/x\x00\x00", "worktree /tmp/x\x00HEAD " + o.Expected.BaseCommit + "\x00HEAD " + o.Expected.BaseCommit + "\x00\x00"} {
		if _, e := parseGitTidyWorktrees(raw, o.Expected.Identity.ObjectFormat); e == nil {
			t.Fatalf("malformed registration admitted %q", raw)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := (gitTidyAdapter{}).Observe(ctx, d, base); e == nil {
		t.Fatal("cancellation admitted")
	}
}
