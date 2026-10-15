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

func TestGitTidyRefusesGraftsAndConfiguredHelpers(t *testing.T) {
	for _, kind := range []string{"grafts", "hook", "fsmonitor"} {
		t.Run(kind, func(t *testing.T) {
			d := fixtureGit(t)
			sentinel := filepath.Join(gitTempDir(t), "helper-called")
			switch kind {
			case "grafts":
				if e := os.WriteFile(filepath.Join(d, ".git", "info", "grafts"), []byte(gitFixture(t, d, "rev-parse", "HEAD")+"\n"), 0600); e != nil {
					t.Fatal(e)
				}
			case "hook":
				if e := os.WriteFile(filepath.Join(d, ".git", "hooks", "reference-transaction"), []byte("#!/bin/sh\ntouch '"+sentinel+"'\n"), 0700); e != nil {
					t.Fatal(e)
				}
			case "fsmonitor":
				gitFixture(t, d, "config", "core.fsmonitor", "touch '"+sentinel+"'")
			}
			if _, e := (gitTidyAdapter{}).Observe(context.Background(), d, "HEAD"); e == nil {
				t.Fatal("unsafe integration evidence admitted")
			}
			if _, e := os.Stat(sentinel); !os.IsNotExist(e) {
				t.Fatal("configured helper executed")
			}
		})
	}
}

func TestGitTidyMalformedReferenceEvidence(t *testing.T) {
	oid := "0123456789012345678901234567890123456789"
	for _, raw := range []string{"refs/heads/x\x00" + oid + "\x00\x00commit\x00", "refs/heads/x\x00bad\x00\x00commit\x00\n", "refs/tags/x\x00" + oid + "\x00\x00commit\x00\n", "refs/heads/x\x00" + oid + "\x00\x00blob\x00\n"} {
		if _, e := parseGitTidyRefs(raw, "sha1"); e == nil {
			t.Fatalf("malformed ref admitted %q", raw)
		}
	}
}

func TestGitTidyProtectsExplicitBaseCommitAliases(t *testing.T) {
	d := fixtureGit(t)
	base := gitFixture(t, d, "rev-parse", "HEAD")
	gitFixture(t, d, "branch", "base-target", base)
	gitFixture(t, d, "commit", "--allow-empty", "-qm", "advance source")
	o, e := (gitTidyAdapter{}).Observe(context.Background(), d, base)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range o.Candidates {
		if c.Name == "refs/heads/base-target" && c.Decision != "retain" {
			t.Fatal("explicit base commit alias admitted for cleanup")
		}
	}
}
