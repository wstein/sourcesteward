// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func requireRetirementRefusal(t *testing.T, o V3PlanOptions, code string, contains ...string) {
	t.Helper()
	_, e := observeTidyV3Legacy(context.Background(), o, newTidyV3ProofBudget(false))
	var op operationError
	if !errors.As(e, &op) || op.Code != code {
		t.Fatalf("want %s, got %v", code, e)
	}
	for _, text := range contains {
		if !strings.Contains(op.Message, text) {
			t.Fatalf("message %q lacks %q", op.Message, text)
		}
	}
}

// An untracked or ignored file used to end as workspace-failed without a cause.
func TestTidyV3LegacyNamesTheUnrecordedEntry(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	if e := os.WriteFile(filepath.Join(o.CompletedWorktree, "scratch.tmp"), []byte("x"), 0600); e != nil {
		t.Fatal(e)
	}
	requireRetirementRefusal(t, o, "retirement-unrecorded-entry", "scratch.tmp", "untracked")
}

func TestTidyV3LegacyNamesTheRefusedSibling(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	if e := os.Symlink("/nonexistent", filepath.Join(filepath.Dir(o.CompletedWorktree), "stray-link")); e != nil {
		t.Fatal(e)
	}
	requireRetirementRefusal(t, o, "retirement-sibling-refused", "stray-link", "symbolic link")
}

func TestTidyV3LegacyExplainsTheGitlinkRefusal(t *testing.T) {
	source := fixtureGit(t)
	gitFixture(t, source, "update-index", "--add", "--cacheinfo", "160000,"+gitFixture(t, source, "rev-parse", "HEAD")+",module")
	gitFixture(t, source, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "gitlink")
	dest := filepath.Join(gitTempDir(t), "completed")
	gitFixture(t, source, "worktree", "add", "-q", "-b", "completed", dest, "HEAD")
	active := filepath.Join(gitTempDir(t), "active")
	gitFixture(t, source, "worktree", "add", "-q", "-b", "active", active, "HEAD")
	o := V3PlanOptions{Path: source, Base: "HEAD", StateDir: gitTempDir(t), CompletedWorktree: dest, Owner: "operator", RemoveRegistration: true, RemoveFiles: dest, ProtectWorktrees: []string{active}}
	ctx, _, closeSession, e := withPlanningSession(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer closeSession()
	_, e = observeTidyV3Legacy(ctx, o, newTidyV3ProofBudget(false))
	var op operationError
	if !errors.As(e, &op) || op.Code != "git-tree-refused" || !strings.Contains(op.Message, "initialized submodule") {
		t.Fatalf("gitlink retirement refusal not explained: %v", e)
	}
}

// A failure without a classified code still says what failed.
func TestFailResultNamesTheCauseOfAnUnclassifiedError(t *testing.T) {
	r := failResult(newResult("tidy-plan"), errors.New("some internal condition"))
	d := r.Diagnostics[0]
	if d.Code != "workspace-failed" || !strings.HasSuffix(d.Message, ": some internal condition") {
		t.Fatalf("%+v", d)
	}
	long := failResult(newResult("tidy-plan"), errors.New(strings.Repeat("x", 1000))).Diagnostics[0].Message
	if len(long) > 300 {
		t.Fatalf("unbounded cause: %d", len(long))
	}
}

// A worktree of several hundred files must not exhaust the 1,024 aggregate
// entry bound meant for references; the manifest keeps its own bound.
func TestV3BoundsDoNotCountPhysicalManifestEntriesAgainstTheAggregate(t *testing.T) {
	entries := make([]V3ManifestEntry, 0, 900)
	m := V3Manifest{Entries: entries}
	if e := v3Bounds(struct{ M V3Manifest }{m}); e != nil {
		t.Fatal(e)
	}
	m.Entries = make([]V3ManifestEntry, retirementManifestEntries+1)
	if e := v3Bounds(struct{ M V3Manifest }{m}); e == nil {
		t.Fatal("manifest above its own bound admitted")
	}
	// The other entries come first, so a manifest wrongly counted would cross
	// the 1,024 aggregate bound (200 + 900) while counted alone it would not.
	refs := struct {
		R []string
		M V3Manifest
	}{make([]string, 200), V3Manifest{Entries: make([]V3ManifestEntry, 900)}}
	if e := v3Bounds(refs); e != nil {
		t.Fatalf("manifest entries counted against references: %v", e)
	}
}

// Entries beyond the aggregate bound outside a manifest still refuse.
func TestV3BoundsStillRefuseAggregateEntriesOutsideTheManifest(t *testing.T) {
	over := struct {
		R []string
		M V3Manifest
	}{make([]string, v3AggregateLimit+1), V3Manifest{Entries: make([]V3ManifestEntry, 1)}}
	if e := v3Bounds(over); e == nil {
		t.Fatal("aggregate bound not enforced")
	}
}
