// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func restoreV4GuardNoPublication(t *testing.T, p RegistrationPruneV4Plan) {
	t.Helper()
	if _, e := os.Lstat(p.Registration.GitDir); !os.IsNotExist(e) {
		t.Fatalf("refused restore published metadata: %v", e)
	}
}
func restoreV4GuardStaging(t *testing.T, p RegistrationPruneV4Plan) string {
	t.Helper()
	entries, e := os.ReadDir(filepath.Join(p.StateDir, pruneV4Namespace, "restores"))
	pruneMust(t, e)
	if len(entries) != 1 {
		t.Fatalf("restore journal count=%d", len(entries))
	}
	return pruneV4RestoreStaging(p, strings.TrimSuffix(entries[0].Name(), ".json"))
}
func restoreV4GuardOwnershipChange(t *testing.T, p RegistrationPruneV4Plan) {
	t.Helper()
	path := filepath.Join(p.StateDir, "tasks", p.Registration.Task+".json")
	b, e := os.ReadFile(path)
	pruneMust(t, e)
	var task TaskRecord
	pruneMust(t, json.Unmarshal(b, &task))
	task.RecoveryLimits = append(task.RecoveryLimits, "Changed valid creation ownership evidence.")
	b, e = json.Marshal(task)
	pruneMust(t, e)
	pruneMust(t, os.WriteFile(path, append(b, '\n'), 0600))
}
func restoreV4GuardRecord(t *testing.T, p RegistrationPruneV4Plan) pruneV4RestoreJournal {
	t.Helper()
	j := pruneV4GuardJournal(t, p)
	return pruneV4RestoreJournal{pruneV4Journal: pruneV4Journal{SchemaVersion: 4, Operation: "registration-restore", PlanID: p.PlanID, PlanDigest: p.Digest, Phase: "prepared", PayloadDigest: j.PayloadDigest, HistoryRef: j.HistoryRef, HistoryOID: j.HistoryOID, Effects: []pruneV4Effect{{"restore-registration", p.Registration.GitDir, "create-absent", "planned"}}, Remaining: []string{"restore-registration"}, Diagnostics: []Diagnostic{}}, RestoreID: strings.Repeat("a", 32), PruneJournalDigest: restoreV4ReviewPruneDigest(t, p)}
}
func restoreV4GuardWriteRecord(t *testing.T, p RegistrationPruneV4Plan, j pruneV4RestoreJournal) {
	t.Helper()
	dir := filepath.Join(p.StateDir, pruneV4Namespace, "restores")
	pruneMust(t, os.MkdirAll(dir, 0700))
	b, e := pruneV4Canonical(j, "")
	pruneMust(t, e)
	pruneMust(t, os.WriteFile(filepath.Join(dir, j.RestoreID+".json"), append(b, '\n'), 0600))
}
func TestRegistrationPruneV4RestoreGuardOtherOwnerConfirmation(t *testing.T) {
	opts, _ := fixturePruneV4(t)
	opts.Mode.Mode = "human"
	opts.Mode.Caller = "another-human"
	opts.Confirm = func(string, string) error { return nil }
	planned := PlanRegistrationPruneV4(context.Background(), opts)
	if planned.Status != "ok" || planned.Plan == nil || planned.Plan.Registration.Scope != "other" {
		t.Fatalf("other-owner plan: %+v", planned)
	}
	p := *planned.Plan
	apply := pruneV4GuardOptions(p)
	apply.Confirm = opts.Confirm
	if r := ApplyRegistrationPruneV4(context.Background(), apply); r.Status != "ok" {
		t.Fatalf("other-owner prune: %+v", r)
	}
	o := restoreV4ReviewApplied(p)
	before := treeSnapshot(t, p.StateDir, p.Expected.SourceSnapshot.Identity.RepositoryDir)
	restoreV4ReviewReason(t, RestoreRegistrationPruneV4(context.Background(), o), "refused", "prune-scope-refused", "Other-owner restore requires fresh typed owner and plan confirmation.")
	calls := 0
	o.Confirm = func(owner, id string) error {
		calls++
		if owner != p.Registration.Owner || id != p.PlanID {
			t.Fatal("confirmation not bound to owner and plan")
		}
		return errors.New("typed confirmation declined")
	}
	restoreV4ReviewReason(t, RestoreRegistrationPruneV4(context.Background(), o), "refused", "prune-scope-refused", "Other-owner restore requires fresh typed owner and plan confirmation.")
	if calls != 1 || !reflect.DeepEqual(before, treeSnapshot(t, p.StateDir, p.Expected.SourceSnapshot.Identity.RepositoryDir)) {
		t.Fatal("declined confirmation changed protected state")
	}
	o.Confirm = func(owner, id string) error {
		calls++
		if owner != p.Registration.Owner || id != p.PlanID {
			t.Fatal("accepted confirmation used wrong identity")
		}
		return nil
	}
	if r := RestoreRegistrationPruneV4(context.Background(), o); r.Status != "ok" || calls != 2 {
		t.Fatalf("confirmed restore: %+v calls=%d", r, calls)
	}
}
func TestRegistrationPruneV4RestoreGuardIncompletePrune(t *testing.T) {
	for _, phase := range []string{"prepared", "removing", "partial"} {
		t.Run(phase, func(t *testing.T) {
			p := restoreV4Fixture(t)
			j := pruneV4GuardJournal(t, p)
			j.Phase = phase
			j.Remaining = []string{"remove-registration"}
			if phase == "prepared" {
				j.Effects = j.Effects[:1]
			}
			b, e := pruneV4Canonical(j, "")
			pruneMust(t, e)
			pruneMust(t, os.WriteFile(filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json"), append(b, '\n'), 0600))
			before := treeSnapshot(t, p.StateDir, p.Expected.SourceSnapshot.Identity.RepositoryDir)
			restoreV4ReviewReason(t, RestoreRegistrationPruneV4(context.Background(), restoreV4ReviewApplied(p)), "refused", "prune-restore-incomplete-prune", "An incomplete prune requires manual inspection; automatic administrative restore is unavailable.")
			if !reflect.DeepEqual(before, treeSnapshot(t, p.StateDir, p.Expected.SourceSnapshot.Identity.RepositoryDir)) {
				t.Fatal("incomplete prune restore wrote state")
			}
		})
	}
}
func TestRegistrationPruneV4RestoreGuardJournalDigestBinding(t *testing.T) {
	p := restoreV4Fixture(t)
	j := restoreV4GuardRecord(t, p)
	j.PruneJournalDigest = strings.Repeat("0", 64)
	restoreV4GuardWriteRecord(t, p, j)
	restoreV4ReviewReason(t, RestoreRegistrationPruneV4(context.Background(), restoreV4ReviewApplied(p)), "refused", "prune-restore-journal-changed", "The restore journal no longer binds the exact original prune journal bytes.")
	restoreV4GuardNoPublication(t, p)
}

type restoreV4ForeignOwnerInfo struct{ os.FileInfo }

func (i restoreV4ForeignOwnerInfo) Sys() any {
	original := i.FileInfo.Sys().(*syscall.Stat_t)
	copy := *original
	copy.Uid++
	return &copy
}
func TestRegistrationPruneV4RestoreGuardPrivateRestoreDirectory(t *testing.T) {
	for _, kind := range []string{"mode", "owner"} {
		t.Run(kind, func(t *testing.T) {
			p := restoreV4Fixture(t)
			dir := filepath.Join(p.StateDir, pruneV4Namespace, "restores")
			pruneMust(t, os.Mkdir(dir, 0700))
			if kind == "mode" {
				pruneMust(t, os.Chmod(dir, 0755))
			}
			fired := false
			ctx := pruneHookContext(func(stage, _ string, value any) {
				if stage == "v4-restore-store-observed" {
					fired = true
					if kind == "owner" {
						info := value.(*os.FileInfo)
						*info = restoreV4ForeignOwnerInfo{*info}
					}
				}
			})
			restoreV4ReviewReason(t, RestoreRegistrationPruneV4(ctx, restoreV4ReviewApplied(p)), "refused", "prune-restore-store-refused", "Administrative restore records require a private user-owned 0700 directory.")
			if !fired {
				t.Fatal("restore store observation seam not reached")
			}
			restoreV4GuardNoPublication(t, p)
		})
	}
}
func TestRegistrationPruneV4RestoreGuardRechecksPriorUnderLock(t *testing.T) {
	p := restoreV4Fixture(t)
	fired := false
	ctx := pruneHookContext(func(stage, _ string, _ any) {
		if stage == "v4-restore-before-lock" {
			fired = true
			restoreV4GuardWriteRecord(t, p, restoreV4GuardRecord(t, p))
		}
	})
	restoreV4ReviewReason(t, RestoreRegistrationPruneV4(ctx, restoreV4ReviewApplied(p)), "refused", "prune-restore-already-recorded", "An administrative restore attempt appeared under the state lock; inspect its retained journal before recovery.")
	if !fired {
		t.Fatal("pre-lock seam not reached")
	}
	restoreV4GuardNoPublication(t, p)
	names, e := os.ReadDir(filepath.Join(p.StateDir, pruneV4Namespace, "restores"))
	pruneMust(t, e)
	if len(names) != 1 {
		t.Fatalf("created a second restore intent: %d", len(names))
	}
}
func TestRegistrationPruneV4RestoreGuardHeldDescriptorBindings(t *testing.T) {
	for _, kind := range []string{"root", "parent", "staged"} {
		t.Run(kind, func(t *testing.T) {
			p := restoreV4Fixture(t)
			fired := false
			stage, code, message := "v4-restore-root-opened", "prune-restore-root-changed", "The held common-directory root no longer matches the prune plan; retained recovery has not been published."
			if kind == "parent" {
				stage, code, message = "v4-restore-parent-opened", "prune-registration-parent-changed", "The native worktrees directory was removed or replaced; retained recovery remains available for separately reviewed manual recovery."
			}
			if kind == "staged" {
				stage, code, message = "v4-restore-staged-opened", "prune-restore-staging-changed", "The staged registration pathname no longer identifies its held directory; retained recovery has not been published."
			}
			ctx := pruneHookContext(func(at, path string, value any) {
				if at != stage {
					return
				}
				fired = true
				if kind != "staged" {
					value.(*PathIdentity).Inode++
					return
				}
				saved := filepath.Join(gitTempDir(t), "held-stage")
				pruneMust(t, os.Rename(path, saved))
				pruneMust(t, os.CopyFS(path, os.DirFS(saved)))
			})
			restoreV4ReviewReason(t, RestoreRegistrationPruneV4(ctx, restoreV4ReviewApplied(p)), "partial", code, message)
			if !fired {
				t.Fatalf("descriptor seam %s not reached", stage)
			}
			restoreV4GuardNoPublication(t, p)
		})
	}
}
func TestRegistrationPruneV4RestoreGuardRevalidatesBeforePublication(t *testing.T) {
	for _, kind := range []string{"ownership", "staged-bytes"} {
		t.Run(kind, func(t *testing.T) {
			p := restoreV4Fixture(t)
			fired := false
			ctx := pruneHookContext(func(stage, _ string, _ any) {
				if stage != "v4-restore-before-publish" {
					return
				}
				fired = true
				if kind == "ownership" {
					restoreV4GuardOwnershipChange(t, p)
				} else {
					file := filepath.Join(restoreV4GuardStaging(t, p), "index")
					b, e := os.ReadFile(file)
					pruneMust(t, e)
					b[len(b)-1] ^= 1
					pruneMust(t, os.WriteFile(file, b, 0600))
				}
			})
			code, message := "prune-ownership-refused", "Original creation ownership changed before restore."
			if kind == "staged-bytes" {
				code, message = "prune-restore-metadata-changed", "Restored administrative metadata differs from the exact saved manifest, bytes or modes."
			}
			restoreV4ReviewReason(t, RestoreRegistrationPruneV4(ctx, restoreV4ReviewApplied(p)), "partial", code, message)
			if !fired {
				t.Fatal("publication seam not reached")
			}
			restoreV4GuardNoPublication(t, p)
		})
	}
}
func TestRegistrationPruneV4RestoreGuardCopyChecksBytesAfterPayloadRead(t *testing.T) {
	p := restoreV4Fixture(t)
	root, _, e := openCanonical(p.Expected.SourceSnapshot.Identity.RepositoryDir)
	pruneMust(t, e)
	defer root.Close()
	fired := false
	ctx := pruneHookContext(func(stage, path string, _ any) {
		if stage != "v4-restore-before-copy-file" || path != "index" {
			return
		}
		fired = true
		file := filepath.Join(p.PayloadPath, "files", path)
		b, e := os.ReadFile(file)
		pruneMust(t, e)
		b[len(b)-1] ^= 1
		pruneMust(t, os.WriteFile(file, b, 0600))
	})
	assertPruneReason(t, copyPruneV4Restore(ctx, p, root, "review-copy-race"), "prune-recovery-refused", "Complete private registration recovery could not be verified.")
	if !fired {
		t.Fatal("copy seam not reached")
	}
	restoreV4GuardNoPublication(t, p)
}
func TestRegistrationPruneV4RestoreGuardVerifiesRestoredModes(t *testing.T) {
	p := restoreV4Fixture(t)
	root, _, e := openCanonical(p.Expected.SourceSnapshot.Identity.RepositoryDir)
	pruneMust(t, e)
	defer root.Close()
	pruneMust(t, copyPruneV4Restore(context.Background(), p, root, "review-mode"))
	staging := filepath.Join(p.Expected.SourceSnapshot.Identity.RepositoryDir, "review-mode")
	pruneMust(t, os.Chmod(filepath.Join(staging, "index"), 0400))
	assertPruneReason(t, verifyPruneV4RestoredMetadata(context.Background(), p, staging, true), "prune-restore-metadata-changed", "Restored administrative metadata differs from the exact saved manifest, bytes or modes.")
}
func TestRegistrationPruneV4RestoreGuardPostMountAndSiblings(t *testing.T) {
	for _, kind := range []string{"mount", "sibling"} {
		t.Run(kind, func(t *testing.T) {
			p := restoreV4Fixture(t)
			fired := false
			ctx := pruneHookContext(func(stage, _ string, _ any) {
				if stage != "v4-restore-before-post" {
					return
				}
				fired = true
				if kind == "sibling" {
					pruneMust(t, os.Mkdir(filepath.Join(filepath.Dir(p.Registration.GitDir), "unexpected-sibling"), 0700))
				}
			})
			ctx = context.WithValue(ctx, pruneV4MountReaderKey{}, func(fd int) (string, error) {
				if fired && kind == "mount" {
					return "replaced-mount", nil
				}
				return pruneMountIdentity(fd)
			})
			code, message := "prune-mount-changed", "Common-directory witness changed during restore."
			if kind == "sibling" {
				code, message = "prune-restore-siblings-changed", "Protected administrative siblings changed during restore."
			}
			restoreV4ReviewReason(t, RestoreRegistrationPruneV4(ctx, restoreV4ReviewApplied(p)), "partial", code, message)
			if !fired {
				t.Fatal("postpublication seam not reached")
			}
			if _, e := os.Lstat(p.Registration.GitDir); e != nil {
				t.Fatalf("postcondition failure did not preserve published metadata: %v", e)
			}
		})
	}
}
func TestRegistrationPruneV4RestoreGuardPrepublicationOwnership(t *testing.T) {
	p := restoreV4Fixture(t)
	fired := false
	ctx := pruneHookContext(func(stage, _ string, _ any) {
		if stage == "v4-restore-before-ownership" {
			fired = true
			restoreV4GuardOwnershipChange(t, p)
		}
	})
	assertPruneReason(t, validatePruneV4Restore(ctx, p, pruneV4GuardJournal(t, p), restoreV4ReviewPruneDigest(t, p)), "prune-ownership-refused", "Original creation ownership changed before restore.")
	if !fired {
		t.Fatal("preownership seam not reached")
	}
	restoreV4GuardNoPublication(t, p)
}
func TestRegistrationPruneV4RestoreGuardExactModeLimitation(t *testing.T) {
	p := restoreV4Fixture(t)
	o := restoreV4ReviewApplied(p)
	o.Mode.Markers = append(o.Mode.Markers, "IDE_SESSION_MARKER")
	r := RestoreRegistrationPruneV4(context.Background(), o)
	restoreV4ReviewReason(t, r, "refused", "prune-scope-refused", "Restore requires the stored caller and mode evidence.")
	if !strings.Contains(strings.Join(r.Limitations, " "), "including marker names and TTY facts") {
		t.Fatalf("exact mode limitation not disclosed: %+v", r.Limitations)
	}
}
