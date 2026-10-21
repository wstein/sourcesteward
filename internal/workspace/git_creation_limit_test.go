// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// creationRoundTrip plans and applies one worktree creation through the public
// entry points, the way `stw workspace plan` and `apply` do.
func creationRoundTrip(t *testing.T, source string) (plan, applied Result, dest string) {
	t.Helper()
	ctx := context.Background()
	state := gitTempDir(t)
	if e := os.Chmod(state, 0700); e != nil {
		t.Fatal(e)
	}
	dest = filepath.Join(gitTempDir(t), "task")
	plan = PlanCreate(ctx, PlanOptions{Path: source, Base: "HEAD", Destination: dest, Task: "refusals", Owner: "fixture", StateDir: state})
	if plan.Status != "ok" || plan.Plan == nil {
		return plan, applied, dest
	}
	applied = Apply(ctx, ApplyOptions{StateDir: state, PlanID: plan.Plan.PlanID, Authorize: true})
	return plan, applied, dest
}

func requireCreation(t *testing.T, source string) string {
	t.Helper()
	plan, applied, dest := creationRoundTrip(t, source)
	if plan.Status != "ok" || applied.Status != "ok" || applied.Task == nil {
		t.Fatalf("creation refused: plan %+v apply %+v", plan.Diagnostics, applied.Diagnostics)
	}
	return dest
}

func requireCreationRefused(t *testing.T, source, code string) {
	t.Helper()
	plan, applied, _ := creationRoundTrip(t, source)
	if plan.Status == "ok" || applied.Status == "ok" {
		t.Fatalf("creation admitted: %+v", plan)
	}
	if len(plan.Diagnostics) == 0 || plan.Diagnostics[0].Code != code {
		t.Fatalf("diagnostics %+v, want code %s", plan.Diagnostics, code)
	}
}

func fixtureGitLeaves(t *testing.T, total int) string {
	t.Helper()
	d := fixtureGit(t)
	for i := 1; i < total; i++ {
		dir := filepath.Join(d, fmt.Sprintf("pkg%03d", i%200))
		if e := os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%05d", i)), []byte("x\n"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	gitFixture(t, d, "add", ".")
	gitFixture(t, d, "commit", "-qm", "leaves")
	return d
}

// Issue: sourcesteward itself has 1,062 leaves and was refused at 1,024.
func TestCreationAdmitsLeafCountsUpToTheCheckoutLimit(t *testing.T) {
	if checkoutLeafLimit != 16384 {
		t.Fatalf("documented checkout leaf limit changed: %d", checkoutLeafLimit)
	}
	for _, n := range []int{1025, 1062, checkoutLeafLimit} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			if testing.Short() && n > 2000 {
				t.Skip("large fixture")
			}
			d := fixtureGitLeaves(t, n)
			dest := requireCreation(t, d)
			if _, e := os.Stat(filepath.Join(dest, "pkg001", "f00001")); e != nil {
				t.Fatalf("checkout incomplete: %v", e)
			}
		})
	}
}

func TestCreationRefusesAboveTheCheckoutLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("large fixture")
	}
	requireCreationRefused(t, fixtureGitLeaves(t, checkoutLeafLimit+1), "git-checkout-limit")
}

// The legacy v2 retirement read starts one sandboxed cat-file per leaf. The
// manifest refuses above retirementManifestEntries anyway, so the read must
// refuse first instead of spending up to checkoutLeafLimit processes. Short
// paths keep the 64 KiB index listing below its own cap, so only the leaf
// count can refuse here.
func TestLegacyRetirementStoredFilesRefusesAboveTheManifestBoundBeforeReading(t *testing.T) {
	d := fixtureGit(t)
	for i := 0; i < retirementManifestEntries+10; i++ {
		if e := os.WriteFile(filepath.Join(d, fmt.Sprintf("f%04d", i)), []byte("x\n"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	gitFixture(t, d, "add", ".")
	gitFixture(t, d, "commit", "-qm", "many")
	dest := filepath.Join(gitTempDir(t), "legacy-many")
	gitFixture(t, d, "worktree", "add", "--detach", "--lock", "--reason", "stw:legacy", dest, "HEAD")
	bin, e := gitExecutable()
	if e != nil {
		t.Fatal(e)
	}
	id, e := gitNativeIdentity(context.Background(), dest)
	if e != nil {
		t.Fatal(e)
	}
	files, e := gitRetirementStoredFiles(context.Background(), bin, id)
	var op operationError
	if !errors.As(e, &op) || op.Code != "retirement-file-limit" || len(files) != 0 {
		t.Fatalf("legacy read admitted or refused wrongly above the manifest bound: %d files, %v", len(files), e)
	}
}
