// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func pruneHookContext(fn func(string, string, any)) context.Context {
	return context.WithValue(context.Background(), pruneObservationHookKey{}, pruneObservationHook(fn))
}
func pruneMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func assertPruneReason(t *testing.T, err error, code, message string) {
	t.Helper()
	assertPruneFailure(t, err, code)
	var op operationError
	if !errors.As(err, &op) || message != "" && op.Message != message {
		t.Fatalf("want reason %q, got %v", message, err)
	}
}
func TestPruneObservationInjectedRaces(t *testing.T) {
	for _, kind := range []string{"second_absence", "metadata_compare", "ownership_compare", "source_snapshot", "pointer_digest", "backend", "selection_disappears"} {
		t.Run(kind, func(t *testing.T) {
			opts, creation, task, _ := fixturePruneObservation(t)
			stage, code := "inventory-read", "prune-destination-present"
			switch kind {
			case "metadata_compare":
				stage, code = "before-metadata-recheck", "prune-metadata-refused"
			case "ownership_compare":
				stage, code = "before-ownership-recheck", "changed-ownership"
			case "source_snapshot":
				stage, code = "before-final-snapshot", "prune-repository-changed"
			case "pointer_digest":
				stage, code = "metadata-sampled", "prune-lock-refused"
			case "backend":
				stage, code = "ownership-read", "prune-repository-refused"
			case "selection_disappears":
				stage, code = "before-inventory", "prune-registration-refused"
			}
			fired := false
			ctx := pruneHookContext(func(at, path string, value any) {
				if at != stage || fired {
					return
				}
				fired = true
				switch kind {
				case "second_absence":
					pruneMust(t, os.Mkdir(task.Destination, 0700))
				case "metadata_compare":
					p := filepath.Join(task.Native.GitDir, "index")
					b, err := os.ReadFile(p)
					pruneMust(t, err)
					b[len(b)-1] ^= 1
					pruneMust(t, os.WriteFile(p, b, 0600))
				case "ownership_compare":
					p := filepath.Join(creation.StateDir, "operations", creation.PlanID+".json")
					b, err := os.ReadFile(p)
					pruneMust(t, err)
					var j journal
					pruneMust(t, json.Unmarshal(b, &j))
					j.Diagnostics = append(j.Diagnostics, Diagnostic{Code: "fixture-race", Message: "changed during observation"})
					b, err = json.Marshal(j)
					pruneMust(t, err)
					pruneMust(t, os.WriteFile(p, b, 0600))
				case "source_snapshot":
					gitFixture(t, opts.Source, "update-ref", "refs/heads/injected-race", "HEAD")
				case "pointer_digest":
					pruneMust(t, os.WriteFile(filepath.Join(task.Native.GitDir, "locked"), []byte("stw:"+task.TaskID), 0600))
				case "backend":
					value.(*TaskRecord).Backend = "jj"
				case "selection_disappears":
					pruneMust(t, os.Rename(task.Native.GitDir, filepath.Join(gitTempDir(t), "retained-registration")))
				}
			})
			_, err := observePruneRegistration(ctx, opts)
			if !fired {
				t.Fatal("injection point not reached")
			}
			assertPruneFailure(t, err, code)
		})
	}
}
func TestPruneObservationNativeBoundaryFixtures(t *testing.T) {
	for _, kind := range []string{"foreign_absolute_common", "linked_source", "duplicate_selection"} {
		t.Run(kind, func(t *testing.T) {
			opts, _, task, moved := fixturePruneObservation(t)
			code := "prune-registration-refused"
			switch kind {
			case "foreign_absolute_common":
				pruneMust(t, os.WriteFile(filepath.Join(task.Native.GitDir, "commondir"), []byte(filepath.Join(gitTempDir(t), "foreign")+"\n"), 0600))
				code = "prune-pointer-refused"
			case "linked_source":
				opts.Source = moved
			case "duplicate_selection":
				code = "tidy-invalid-registration"
				pruneMust(t, os.CopyFS(filepath.Join(task.Native.RepositoryDir, "worktrees", "duplicate"), os.DirFS(task.Native.GitDir)))
			}
			_, err := observePruneRegistration(context.Background(), opts)
			assertPruneFailure(t, err, code)
		})
	}
}
func TestPruneAbsenceInPassRaces(t *testing.T) {
	for _, kind := range []string{"mode_changed", "ancestor_renamed", "leaf_appears", "directory_without_search_bits"} {
		t.Run(kind, func(t *testing.T) {
			parent := filepath.Join(gitTempDir(t), "parent")
			pruneMust(t, os.Mkdir(parent, 0700))
			id, err := currentIdentity(parent)
			pruneMust(t, err)
			target := filepath.Join(parent, "missing")
			stage := "absence-first-missing"
			code := "prune-absence-changed"
			message := "Checkout ancestor or mount changed during absence observation."
			if kind == "ancestor_renamed" {
				message = "Checkout ancestor pathname no longer identifies its pinned directory."
			}
			if kind == "leaf_appears" {
				stage = "absence-before-final-missing"
				code = "prune-destination-present"
				message = ""
			}
			if kind == "directory_without_search_bits" {
				pruneMust(t, os.Chmod(parent, 0600))
				t.Cleanup(func() { _ = os.Chmod(parent, 0700) })
				_, err = observePruneAbsence(context.Background(), target, id)
				assertPruneFailure(t, err, "prune-parent-unavailable")
				return
			}
			fired := false
			ctx := pruneHookContext(func(at, path string, _ any) {
				if at != stage || path != target || fired {
					return
				}
				fired = true
				switch kind {
				case "mode_changed":
					pruneMust(t, os.Chmod(parent, 0750))
				case "ancestor_renamed":
					pruneMust(t, os.Rename(parent, parent+"-moved"))
				case "leaf_appears":
					pruneMust(t, os.Mkdir(target, 0700))
				}
			})
			_, err = observePruneAbsence(ctx, target, id)
			if !fired {
				t.Fatal("injection point not reached")
			}
			assertPruneReason(t, err, code, message)
		})
	}
}
func TestPruneMetadataFileRaces(t *testing.T) {
	for _, kind := range []string{"swapped_before_open", "truncated_before_read", "same_size_rewrite"} {
		t.Run(kind, func(t *testing.T) {
			_, _, task, _ := fixturePruneObservation(t)
			path := filepath.Join(task.Native.GitDir, "index")
			stage := "metadata-before-open"
			reason := "Registration metadata identity changed before reading."
			if kind == "truncated_before_read" {
				stage = "metadata-before-read"
				reason = "Registration metadata size changed while reading."
			}
			if kind == "same_size_rewrite" {
				stage = "metadata-after-read"
				reason = "Registration metadata physical facts changed while reading."
			}
			fired := false
			ctx := pruneHookContext(func(at, p string, _ any) {
				if at != stage || p != path || fired {
					return
				}
				fired = true
				b, err := os.ReadFile(path)
				pruneMust(t, err)
				switch kind {
				case "swapped_before_open":
					tmp := filepath.Join(gitTempDir(t), "replacement")
					pruneMust(t, os.WriteFile(tmp, b, 0600))
					pruneMust(t, os.Rename(tmp, path))
				case "truncated_before_read":
					pruneMust(t, os.Truncate(path, 0))
				case "same_size_rewrite":
					b[len(b)-1] ^= 1
					pruneMust(t, os.WriteFile(path, b, 0600))
					pruneMust(t, os.Chmod(path, 0640))
				}
			})
			_, _, err := observePruneMetadata(ctx, task.Native.GitDir, defaultPruneMetadataLimits())
			if !fired {
				t.Fatal("injection point not reached")
			}
			assertPruneReason(t, err, "prune-metadata-refused", reason)
		})
	}
}

func TestPruneObservationChangedTaskRecompare(t *testing.T) {
	opts, _, task, _ := fixturePruneObservation(t)
	fired := false
	ctx := pruneHookContext(func(stage, _ string, _ any) {
		if stage != "before-ownership-recheck" || fired {
			return
		}
		fired = true
		p := filepath.Join(opts.StateDir, "tasks", task.TaskID+".json")
		b, err := os.ReadFile(p)
		pruneMust(t, err)
		// Recovery limits are valid task data but are part of the original evidence.
		var data map[string]any
		pruneMust(t, json.Unmarshal(b, &data))
		data["recovery_limits"] = append([]string{}, "changed observation limit")
		b, err = json.Marshal(data)
		pruneMust(t, err)
		pruneMust(t, os.WriteFile(p, b, 0600))
	})
	_, err := observePruneRegistration(ctx, opts)
	if !fired {
		t.Fatal("injection point not reached")
	}
	assertPruneFailure(t, err, "changed-ownership")
}
