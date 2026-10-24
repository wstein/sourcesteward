// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func planSetDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPlanSetStoreRoundTripAndIdempotentRewrite(t *testing.T) {
	dir := planSetDir(t)
	record := []byte(`{"plan_id":"3f9c2a7e41b8"}`)
	if err := WriteTidyPlanSet(dir, "3f9c2a7e41b8", record); err != nil {
		t.Fatal(err)
	}
	if err := WriteTidyPlanSet(dir, "3f9c2a7e41b8", record); err != nil {
		t.Fatalf("an identical rewrite of a content-addressed record must succeed: %v", err)
	}
	got, err := ReadTidyPlanSet(dir, "3f9c2a7e41b8")
	if err != nil || string(got) != string(record) {
		t.Fatalf("read = %q, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(dir, "tidy-plan-sets", "3f9c2a7e41b8.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("record must be a private file: %v %v", info, err)
	}
}

func TestPlanSetStoreRefusesADifferentRecordUnderTheSameID(t *testing.T) {
	dir := planSetDir(t)
	if err := WriteTidyPlanSet(dir, "3f9c2a7e41b8", []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := WriteTidyPlanSet(dir, "3f9c2a7e41b8", []byte(`{"a":2}`)); err == nil {
		t.Fatal("a different record under an existing id must be refused")
	}
}

func TestPlanSetStoreMissingRecord(t *testing.T) {
	dir := planSetDir(t)
	if _, err := ReadTidyPlanSet(dir, "3f9c2a7e41b8"); !errors.Is(err, ErrPlanSetNotFound) {
		t.Fatalf("store without the kind: %v", err)
	}
	if err := WriteTidyPlanSet(dir, "000000000000", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTidyPlanSet(dir, "3f9c2a7e41b8"); !errors.Is(err, ErrPlanSetNotFound) {
		t.Fatalf("absent id: %v", err)
	}
}

func TestPlanSetStoreModeChecks(t *testing.T) {
	loose := planSetDir(t)
	if err := os.Chmod(loose, 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteTidyPlanSet(loose, "3f9c2a7e41b8", []byte(`{}`)); err == nil {
		t.Fatal("a mode 0755 state directory must be refused for writing")
	}
	if _, err := ReadTidyPlanSet(loose, "3f9c2a7e41b8"); err == nil || errors.Is(err, ErrPlanSetNotFound) {
		t.Fatalf("a mode 0755 state directory must be refused for reading: %v", err)
	}

	linked := planSetDir(t)
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(linked, "tidy-plan-sets")); err != nil {
		t.Fatal(err)
	}
	if err := WriteTidyPlanSet(linked, "3f9c2a7e41b8", []byte(`{}`)); err == nil {
		t.Fatal("a symlinked record directory must be refused")
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Fatalf("nothing may be written through the symlink: %v", entries)
	}

	open := planSetDir(t)
	if err := WriteTidyPlanSet(open, "3f9c2a7e41b8", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(open, "tidy-plan-sets", "3f9c2a7e41b8.json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTidyPlanSet(open, "3f9c2a7e41b8"); err == nil {
		t.Fatal("a record that is not private must be refused")
	}
}

func TestPlanSetStoreRejectsMalformedAndOversizedRecords(t *testing.T) {
	dir := planSetDir(t)
	for name, record := range map[string]string{"duplicate key": `{"a":1,"a":2}`, "upper-case key": `{"A":1}`, "not json": `nope`, "trailing": `{} {}`} {
		if err := WriteTidyPlanSet(dir, "3f9c2a7e41b8", []byte(record)); err == nil {
			t.Errorf("%s must be refused at write", name)
		}
	}
	if err := WriteTidyPlanSet(dir, "../escape", []byte(`{}`)); err == nil {
		t.Error("an id that is not a plain identifier must be refused")
	}
	old := v3RecordLimit
	v3RecordLimit = 16
	t.Cleanup(func() { v3RecordLimit = old })
	if err := WriteTidyPlanSet(dir, "3f9c2a7e41b8", []byte(`{"padding":"0123456789012345"}`)); err == nil {
		t.Error("a record over the bound must be refused")
	}
}
