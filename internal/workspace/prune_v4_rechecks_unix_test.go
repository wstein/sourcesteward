// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRegistrationPruneV4MissingLockedSibling(t *testing.T) {
	o, _ := fixturePruneV4(t)
	sibling := filepath.Join(gitTempDir(t), "locked-sibling")
	gitFixture(t, o.Path, "worktree", "add", "--detach", sibling, "HEAD")
	gitFixture(t, o.Path, "worktree", "lock", "--reason", "fixture", sibling)
	pruneMust(t, os.Rename(sibling, sibling+"-moved"))
	r := PlanRegistrationPruneV4(context.Background(), o)
	if r.Status != "refused" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "unknown-workspace-path" || r.Diagnostics[0].Message != "A registered workspace root is missing, replaced or not a canonical real directory; state-store disjointness is unproven." {
		t.Fatalf("unexpected: %+v", r)
	}
}
func TestRegistrationPruneV4SiblingUnderState(t *testing.T) {
	o, _ := fixturePruneV4(t)
	sibling := filepath.Join(o.StateDir, "protected-sibling")
	gitFixture(t, o.Path, "worktree", "add", "--detach", sibling, "HEAD")
	r := PlanRegistrationPruneV4(context.Background(), o)
	if r.Status != "refused" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "state-overlap" || r.Diagnostics[0].Message != "Private prune state overlaps a registered workspace." {
		t.Fatalf("unexpected: %+v", r)
	}
}
func TestRegistrationPruneV4CommonPathRebound(t *testing.T) {
	path := filepath.Join(gitTempDir(t), "common")
	pruneMust(t, os.Mkdir(path, 0700))
	fired := false
	ctx := pruneHookContext(func(stage, p string, _ any) {
		if stage != "common-mount-observed" || p != path {
			return
		}
		fired = true
		pruneMust(t, os.Rename(path, path+"-pinned"))
		pruneMust(t, os.Mkdir(path, 0700))
	})
	_, err := observePruneV4Common(ctx, path)
	if !fired {
		t.Fatal("hook not reached")
	}
	assertPruneReason(t, err, "prune-mount-changed", "Common-directory mount or descriptor/path binding changed.")
}

func assertPruneV4ResultReason(t *testing.T, r RegistrationPruneV4Result, code, message string) {
	t.Helper()
	if r.Status != "refused" || r.Plan != nil || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != code || r.Diagnostics[0].Message != message {
		t.Fatalf("wrong refusal: %+v", r)
	}
}

func TestRegistrationPruneV4StateInsideSourceOrCommon(t *testing.T) {
	for _, kind := range []string{"source", "common"} {
		t.Run(kind, func(t *testing.T) {
			o, p := fixturePruneV4(t)
			sourceStore := filepath.Join(o.Path, "private-state")
			if kind == "common" {
				sourceStore = filepath.Join(p.Expected.SourceSnapshot.Identity.RepositoryDir, "private-state")
			}
			pruneMust(t, os.Rename(o.StateDir, sourceStore))
			o.StateDir = sourceStore
			r := PlanRegistrationPruneV4(context.Background(), o)
			assertPruneV4ResultReason(t, r, "state-overlap", "Private prune state overlaps protected source metadata or workspace.")
		})
	}
}

func TestRegistrationPruneV4CrossObservationRechecks(t *testing.T) {
	for _, kind := range []string{"common", "owner-second", "owner-third", "evidence-second", "evidence-third", "registration-second", "registration-third"} {
		t.Run(kind, func(t *testing.T) {
			o, p := fixturePruneV4(t)
			count := 0
			fired := false
			stage := "v4-expected-observed"
			when := 2
			code := "prune-evidence-changed"
			message := "Prune evidence changed before private plan persistence."
			switch kind {
			case "common":
				stage = "v4-common-reobserved"
				when = 1
				code = "prune-mount-changed"
				message = "Common-directory mount changed during prune planning."
			case "owner-second", "owner-third":
				stage = "v4-ownership-observed"
				code = "changed-ownership"
				message = "Original prune ownership changed."
			case "registration-second", "registration-third":
				stage = "v4-registration-observed"
			}
			if kind == "owner-third" || kind == "evidence-third" || kind == "registration-third" {
				when = 3
				if code == "prune-evidence-changed" {
					message = "Prune evidence changed under the private store lock."
				}
			}
			ctx := pruneHookContext(func(at, path string, value any) {
				if at != stage {
					return
				}
				count++
				if count != when {
					return
				}
				fired = true
				switch stage {
				case "v4-common-reobserved":
					value.(*PruneV4Directory).Mount += "-changed"
				case "v4-ownership-observed":
					value.(*pruneInspectionOptions).Owner = "changed-owner"
				case "v4-expected-observed":
					value.(*PruneV4Expected).MetadataBytes++
				case "v4-registration-observed":
					value.(*PruneV4Registration).Owner = "changed-owner"
				}
			})
			r := PlanRegistrationPruneV4(ctx, o)
			if !fired {
				t.Fatal("required observation seam not reached")
			}
			assertPruneV4ResultReason(t, r, code, message)
			entries, e := os.ReadDir(filepath.Dir(pruneV4PlanPath(p)))
			pruneMust(t, e)
			if len(entries) != 1 {
				t.Fatalf("refused planner persisted %d plans, want original only", len(entries))
			}
		})
	}
}
