// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func overviewPruneFixture(t *testing.T) (RegistrationPruneV4Options, TaskRecord) {
	t.Helper()
	o, _, task, _ := fixturePruneObservation(t)
	return RegistrationPruneV4Options{Path: o.Source, StateDir: o.StateDir, Name: o.Registrations[0], GitDir: task.Native.GitDir, Mode: V3RunMode{Mode: "agent", Caller: o.Owner, CallerSource: "explicit", Markers: []string{}, ByOwner: []string{}}}, task
}

func TestRegistrationPruneV4OverviewReadOnly(t *testing.T) {
	o, task := overviewPruneFixture(t)
	before := snapshotPruneOverviewTree(t, o.Path, o.StateDir, task.Destination+"-moved")
	inventory := ObserveRegistrationPruneV4Inventory(context.Background(), o.Path)
	if inventory.Status != "ok" || len(inventory.Candidates) != 1 {
		t.Fatalf("inventory: %+v", inventory)
	}
	candidate := inventory.Candidates[0]
	if candidate.Name != o.Name || candidate.GitDir != o.GitDir || candidate.Destination != task.Destination || candidate.Task != task.TaskID {
		t.Fatalf("candidate: %+v", candidate)
	}
	got := ObserveRegistrationPruneV4(context.Background(), o)
	if got.Status != "ok" || !got.Complete() || len(got.EvidenceDigest) != 64 || got.Registration.Scope != "own" {
		t.Fatalf("observation: %+v", got)
	}
	if got.Expected.CommonDirMount.Mount == "" || len(got.Expected.Absence.Ancestors) == 0 || got.Expected.CreationDigest == "" {
		t.Fatal("incomplete witness")
	}
	after := snapshotPruneOverviewTree(t, o.Path, o.StateDir, task.Destination+"-moved")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("read-only overview changed repository, store, or moved checkout")
	}
	for _, reg := range got.Expected.Native.Registrations {
		if reg.Path == task.Destination && (!reg.Locked || reg.Prunable) {
			t.Fatal("real locked missing registration must need no prunable marker")
		}
	}
	got.Expected.Absence.Ancestors[0].Mount += "changed"
	if got.Complete() {
		t.Fatal("changed witness remains complete")
	}
}

func snapshotPruneOverviewTree(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			value := info.Mode().String()
			if info.Mode().IsRegular() {
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				value += gitDigest(string(b))
			}
			out[path] = value
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestRegistrationPruneV4OverviewRefusesIncompleteEvidence(t *testing.T) {
	cases := []struct {
		name, code string
		change     func(*testing.T, *RegistrationPruneV4Options, TaskRecord)
	}{
		{"unlocked", "prune-lock-refused", func(t *testing.T, o *RegistrationPruneV4Options, task TaskRecord) {
			mustOverviewChange(t, os.Remove(filepath.Join(o.GitDir, "locked")))
		}},
		{"foreign-lock", "prune-ownership-refused", func(t *testing.T, o *RegistrationPruneV4Options, task TaskRecord) {
			mustOverviewChange(t, os.WriteFile(filepath.Join(o.GitDir, "locked"), []byte("stw:foreign\n"), 0600))
		}},
		{"present", "prune-destination-present", func(t *testing.T, o *RegistrationPruneV4Options, task TaskRecord) {
			mustOverviewChange(t, os.Mkdir(task.Destination, 0700))
		}},
		{"dangling-symlink", "prune-destination-present", func(t *testing.T, o *RegistrationPruneV4Options, task TaskRecord) {
			mustOverviewChange(t, os.Symlink("not-there", task.Destination))
		}},
		{"missing-ancestor", "prune-parent-unavailable", func(t *testing.T, o *RegistrationPruneV4Options, task TaskRecord) {
			mustOverviewChange(t, os.Rename(filepath.Dir(task.Destination), filepath.Dir(task.Destination)+"-away"))
		}},
		{"changed-parent", "prune-parent-changed", func(t *testing.T, o *RegistrationPruneV4Options, task TaskRecord) {
			parent := filepath.Dir(task.Destination)
			mustOverviewChange(t, os.Rename(parent, parent+"-away"))
			mustOverviewChange(t, os.Mkdir(parent, 0700))
		}},
		{"pointer", "prune-pointer-refused", func(t *testing.T, o *RegistrationPruneV4Options, task TaskRecord) {
			mustOverviewChange(t, os.WriteFile(filepath.Join(o.GitDir, "gitdir"), []byte(task.Destination+"-other/.git\n"), 0600))
		}},
		{"metadata", "prune-metadata-refused", func(t *testing.T, o *RegistrationPruneV4Options, task TaskRecord) {
			mustOverviewChange(t, os.WriteFile(filepath.Join(o.GitDir, "unexpected"), []byte("secret"), 0600))
		}},
		{"wrong-selector", "prune-selection-refused", func(t *testing.T, o *RegistrationPruneV4Options, task TaskRecord) { o.Name += "-other" }},
		{"current", "prune-selection-refused", func(t *testing.T, o *RegistrationPruneV4Options, task TaskRecord) {
			o.Path = task.Destination + "-moved"
			gitFixture(t, o.Path, "worktree", "repair", o.Path)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, task := overviewPruneFixture(t)
			tc.change(t, &o, task)
			got := ObserveRegistrationPruneV4(context.Background(), o)
			if got.Status != "refused" || got.Diagnostic.Code != tc.code || got.Complete() || got.EvidenceDigest != "" {
				t.Fatalf("got status=%s code=%s complete=%v want refused/%s", got.Status, got.Diagnostic.Code, got.Complete(), tc.code)
			}
		})
	}
}
func mustOverviewChange(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestRegistrationPruneV4OverviewMountAndRaceRefusal(t *testing.T) {
	for _, stage := range []string{"common-mount-observed", "v4-expected-observed"} {
		t.Run(stage, func(t *testing.T) {
			o, _ := overviewPruneFixture(t)
			calls := 0
			ctx := pruneHookContext(func(at, path string, v any) {
				if at != stage {
					return
				}
				calls++
				if stage == "common-mount-observed" {
					v.(*pruneDirectoryWitness).Mount += "changed"
				} else if calls == 1 {
					v.(*PruneV4Expected).CreationDigest = "changed"
				}
			})
			got := ObserveRegistrationPruneV4(ctx, o)
			want := "prune-evidence-changed"
			if stage == "common-mount-observed" {
				want = "prune-mount-changed"
			}
			if got.Status != "refused" || got.Diagnostic.Code != want || got.Complete() {
				t.Fatalf("status=%s code=%s want refused/%s", got.Status, got.Diagnostic.Code, want)
			}
		})
	}
	o, _ := overviewPruneFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := ObserveRegistrationPruneV4(ctx, o)
	if got.Status != "refused" || got.Complete() {
		t.Fatalf("canceled: %+v", got)
	}
}
