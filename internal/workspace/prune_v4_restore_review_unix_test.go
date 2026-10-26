// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func restoreV4ReviewReason(t *testing.T, r RegistrationPruneV4Result, status, code, message string) {
	t.Helper()
	pruneV4GuardReason(t, r, status, code)
	if r.Diagnostics[0].Message != message {
		t.Fatalf("want diagnostic %q, got %q", message, r.Diagnostics[0].Message)
	}
}
func restoreV4ReviewApplied(p RegistrationPruneV4Plan) RegistrationPruneV4RestoreOptions {
	o := restoreV4Options(p)
	o.Apply, o.Authorize = true, true
	return o
}
func restoreV4ReviewPruneDigest(t *testing.T, p RegistrationPruneV4Plan) string {
	t.Helper()
	b, e := os.ReadFile(filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json"))
	pruneMust(t, e)
	return pruneV4Hash("stw-registration-journal-file-v4", b)
}
func TestRegistrationPruneV4RestoreReviewNativeMaintenanceParentRefusal(t *testing.T) {
	for _, kind := range []string{"removed", "replaced"} {
		t.Run(kind, func(t *testing.T) {
			p := restoreV4Fixture(t)
			gitFixture(t, p.SourcePath, "worktree", "prune")
			parent := filepath.Dir(p.Registration.GitDir)
			if _, e := os.Lstat(parent); !os.IsNotExist(e) {
				t.Fatalf("native prune did not remove empty administrative parent: %v", e)
			}
			if kind == "replaced" {
				pruneMust(t, os.Mkdir(parent, 0700))
			}
			before := treeSnapshot(t, p.StateDir, p.Expected.SourceSnapshot.Identity.RepositoryDir)
			for _, o := range []RegistrationPruneV4RestoreOptions{restoreV4Options(p), restoreV4ReviewApplied(p)} {
				r := RestoreRegistrationPruneV4(context.Background(), o)
				restoreV4ReviewReason(t, r, "refused", "prune-registration-parent-changed", "The native worktrees directory was removed or replaced; retained recovery remains available for separately reviewed manual recovery.")
				if !strings.Contains(strings.Join(r.Limitations, " "), "Native maintenance can remove the empty worktrees directory") {
					t.Fatalf("parent recovery limit omitted: %+v", r.Limitations)
				}
			}
			if !reflect.DeepEqual(before, treeSnapshot(t, p.StateDir, p.Expected.SourceSnapshot.Identity.RepositoryDir)) {
				t.Fatal("parent refusal changed protected state")
			}
		})
	}
}
func TestRegistrationPruneV4RestoreReviewNoAppliedJournal(t *testing.T) {
	_, p := fixturePruneV4(t)
	for _, kind := range []string{"no-directories", "empty-directories"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "empty-directories" {
				for _, name := range []string{"journals", "payloads"} {
					pruneMust(t, os.Mkdir(filepath.Join(p.StateDir, pruneV4Namespace, name), 0700))
				}
			}
			before := treeSnapshot(t, p.StateDir, p.Expected.SourceSnapshot.Identity.RepositoryDir)
			r := RestoreRegistrationPruneV4(context.Background(), restoreV4Options(p))
			restoreV4ReviewReason(t, r, "refused", "prune-not-applied", "No applied prune journal and recovery payload are recorded for this plan; inspection creates no recovery state.")
			if !reflect.DeepEqual(before, treeSnapshot(t, p.StateDir, p.Expected.SourceSnapshot.Identity.RepositoryDir)) {
				t.Fatal("inspection initialized missing recovery state")
			}
		})
	}
}
func TestRegistrationPruneV4RestoreReviewDistinctStaleEvidence(t *testing.T) {
	for _, kind := range []string{"occupied-slot", "plan", "prune-journal"} {
		t.Run(kind, func(t *testing.T) {
			p := restoreV4Fixture(t)
			j := pruneV4GuardJournal(t, p)
			digest := restoreV4ReviewPruneDigest(t, p)
			code, message := "prune-registration-occupied", "The original administrative registration slot is occupied; restore never overwrites or adopts an existing entry."
			switch kind {
			case "occupied-slot":
				pruneMust(t, os.Mkdir(p.Registration.GitDir, 0700))
			case "plan":
				q := clonePruneV4(t, p)
				q.RecoveryLimits = append(q.RecoveryLimits, "Changed immutable plan.")
				savePruneV4Fixture(t, q)
				code, message = "prune-restore-plan-changed", "The stored prune plan changed during restore inspection; retained recovery has not been published."
			case "prune-journal":
				j.Diagnostics = append(j.Diagnostics, Diagnostic{Code: "external-change", Message: "Changed original journal bytes."})
				b, e := pruneV4Canonical(j, "")
				pruneMust(t, e)
				pruneMust(t, os.WriteFile(filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json"), append(b, '\n'), 0600))
				code, message = "prune-journal-changed", "The original prune journal bytes changed during restore inspection; retained recovery has not been published."
			}
			assertPruneReason(t, validatePruneV4Restore(context.Background(), p, j, digest), code, message)
		})
	}
}
func TestRegistrationPruneV4RestoreReviewRepairArgvLiteralPlaceholder(t *testing.T) {
	p := RegistrationPruneV4Plan{SourcePath: "/tmp/source <quoted>"}
	data := pruneV4RestoreRepairArgv(p)
	if !strings.Contains(data, "<owner-selected-found-path>") || strings.Contains(data, `\u003c`) {
		t.Fatalf("repair argv is HTML escaped: %s", data)
	}
	var args []string
	pruneMust(t, json.Unmarshal([]byte(data), &args))
	if args[2] != p.SourcePath || args[len(args)-1] != "<owner-selected-found-path>" {
		t.Fatalf("wrong argv: %#v", args)
	}
}
