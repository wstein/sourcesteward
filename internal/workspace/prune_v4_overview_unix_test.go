// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func TestRegistrationPruneV4OverviewDigestBindsEveryWitness(t *testing.T) {
	o, _ := overviewPruneFixture(t)
	original := ObserveRegistrationPruneV4(context.Background(), o)
	if !original.Complete() {
		t.Fatalf("fixture not complete: %+v", original)
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	leaves := []string{}
	var collect func(reflect.Value, string)
	collect = func(v reflect.Value, path string) {
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				collect(v.Field(i), path+"."+v.Type().Field(i).Name)
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				collect(v.Index(i), fmt.Sprintf("%s[%d]", path, i))
			}
		case reflect.String, reflect.Bool, reflect.Int, reflect.Int64, reflect.Uint32, reflect.Uint64:
			leaves = append(leaves, path)
		}
	}
	collect(reflect.ValueOf(original.Registration), "Registration")
	collect(reflect.ValueOf(original.Expected), "Expected")
	for _, path := range leaves {
		t.Run(path, func(t *testing.T) {
			var changed RegistrationPruneV4Observation
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			changed.admittedDigest = original.admittedDigest
			var mutate func(reflect.Value, string)
			mutate = func(v reflect.Value, current string) {
				if current == path {
					switch v.Kind() {
					case reflect.String:
						v.SetString(v.String() + "x")
					case reflect.Bool:
						v.SetBool(!v.Bool())
					case reflect.Int, reflect.Int64:
						v.SetInt(v.Int() + 1)
					case reflect.Uint32, reflect.Uint64:
						v.SetUint(v.Uint() + 1)
					}
					return
				}
				switch v.Kind() {
				case reflect.Struct:
					for i := 0; i < v.NumField(); i++ {
						mutate(v.Field(i), current+"."+v.Type().Field(i).Name)
					}
				case reflect.Slice:
					for i := 0; i < v.Len(); i++ {
						mutate(v.Index(i), fmt.Sprintf("%s[%d]", current, i))
					}
				}
			}
			mutate(reflect.ValueOf(&changed.Registration).Elem(), "Registration")
			mutate(reflect.ValueOf(&changed.Expected).Elem(), "Expected")
			if changed.Complete() {
				t.Fatal("changed witness remains complete")
			}
		})
	}
	if len(leaves) < 100 {
		t.Fatalf("unexpectedly incomplete witness coverage: %d", len(leaves))
	}
	expected := map[string]any{"schema_version": 4, "operation": "registration-prune", "registration": original.Registration, "expected": original.Expected}
	// Recursively sorted map encoding is independent of the typed field order.
	b, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	var canonical any
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	if err = decoder.Decode(&canonical); err != nil {
		t.Fatal(err)
	}
	b, err = json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if got := pruneV4Hash("stw-registration-evidence-v4", b); got != original.EvidenceDigest {
		t.Fatalf("evidence digest omitted a field or used the wrong domain: %s", got)
	}
	if pruneV4Hash("stw-registration-row-v4", b) == original.EvidenceDigest {
		t.Fatal("row and evidence domains interchangeable")
	}
}
