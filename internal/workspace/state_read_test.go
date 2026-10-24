// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadTaskAndTaskIDsAreReadOnly(t *testing.T) {
	p := privateDir(t)
	if ids, err := TaskIDs(p); err != nil || len(ids) != 0 {
		t.Fatalf("a store without a tasks directory: %v %v", ids, err)
	}
	if _, found, err := ReadTask(p, "t"); found || err != nil {
		t.Fatalf("%v %v", found, err)
	}
	if _, err := os.Lstat(filepath.Join(p, "tasks")); err == nil {
		t.Fatal("reading created the tasks directory")
	}
	s, err := openStore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.write("tasks", "t", TaskRecord{TaskID: "t", Owner: "claude"}, true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	rec, found, err := ReadTask(p, "t")
	if err != nil || !found || rec.Owner != "claude" {
		t.Fatalf("%+v %v %v", rec, found, err)
	}
	if _, found, err = ReadTask(p, "other"); found || err != nil {
		t.Fatalf("%v %v", found, err)
	}
	if ids, err := TaskIDs(p); err != nil || !reflect.DeepEqual(ids, []string{"t"}) {
		t.Fatalf("%v %v", ids, err)
	}
	if err = os.WriteFile(filepath.Join(p, "tasks", "bad.json"), []byte(`{"unknown":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = ReadTask(p, "bad"); err == nil {
		t.Fatal("accepted an unknown field")
	}
}
