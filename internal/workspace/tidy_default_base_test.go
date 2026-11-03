// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"strings"
	"testing"
)

func originHeadFixture(t *testing.T) string {
	t.Helper()
	dir, _ := fixtureTidyV3Git(t)
	gitFixture(t, dir, "remote", "add", "origin", "https://github.com/owner/repo.git")
	gitFixture(t, dir, "update-ref", "refs/remotes/origin/trunk", "HEAD")
	return dir
}

// Issue 389: the default base is the remote default branch named by refs/remotes/origin/HEAD.
func TestDefaultTidyBaseIsTheTargetOfOriginHead(t *testing.T) {
	dir := originHeadFixture(t)
	gitFixture(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	got, err := DefaultTidyBase(context.Background(), dir)
	if err != nil || got != "refs/remotes/origin/trunk" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// Without origin/HEAD, and for every origin/HEAD that cannot be a base, the error names the command that sets it.
func TestDefaultTidyBaseErrorNamesTheCommandThatSetsOriginHead(t *testing.T) {
	const hint = "git remote set-head origin -a"
	dir := originHeadFixture(t)
	if _, err := DefaultTidyBase(context.Background(), dir); err == nil || !strings.Contains(err.Error(), hint) || !strings.Contains(err.Error(), "--base") {
		t.Fatalf("no origin/HEAD: %v", err)
	}
	// The target of origin/HEAD does not exist.
	gitFixture(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/gone")
	if _, err := DefaultTidyBase(context.Background(), dir); err == nil || !strings.Contains(err.Error(), hint) {
		t.Fatalf("dangling origin/HEAD: %v", err)
	}
	// origin/HEAD is not symbolic.
	gitFixture(t, dir, "update-ref", "--no-deref", "refs/remotes/origin/HEAD", gitFixture(t, dir, "rev-parse", "HEAD"))
	if _, err := DefaultTidyBase(context.Background(), dir); err == nil || !strings.Contains(err.Error(), hint) {
		t.Fatalf("direct origin/HEAD: %v", err)
	}
}

// A symbolic origin/HEAD that points outside the origin namespace is not accepted as a base.
func TestDefaultTidyBaseRefusesATargetOutsideOrigin(t *testing.T) {
	dir := originHeadFixture(t)
	gitFixture(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/heads/"+strings.TrimPrefix(gitFixture(t, dir, "symbolic-ref", "HEAD"), "refs/heads/"))
	if got, err := DefaultTidyBase(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "git remote set-head origin -a") {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDefaultTidyBaseOutsideARepository(t *testing.T) {
	dir := gitTempDir(t)
	for name, path := range map[string]string{"a directory without a repository": dir, "a missing directory": dir + "/missing"} {
		if got, err := DefaultTidyBase(context.Background(), path); err == nil || !strings.Contains(err.Error(), "--base is required") {
			t.Errorf("%s: got %q, %v", name, got, err)
		}
	}
}

// The command passes the path as typed, which is "." without a PATH.
func TestDefaultTidyBaseAcceptsARelativePath(t *testing.T) {
	dir := originHeadFixture(t)
	gitFixture(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	t.Chdir(dir)
	got, err := DefaultTidyBase(context.Background(), ".")
	if err != nil || got != "refs/remotes/origin/trunk" {
		t.Fatalf("got %q, %v", got, err)
	}
}
