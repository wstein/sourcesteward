// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
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

// EachTask reads every creation record of a store, read only: a record that cannot be read is
// reported by id and skipped, never fatal, and a store without a tasks directory has none.
// collectTasks gathers what EachTask hands over, for the assertions below.
func collectTasks(stateDir string) (records []TaskRecord, unreadable []string, err error) {
	unreadable, err = EachTask(stateDir, func(r TaskRecord) { records = append(records, r) })
	return records, unreadable, err
}

func TestEachTaskSkipsAndReportsUnreadableRecords(t *testing.T) {
	p := privateDir(t)
	if recs, bad, err := collectTasks(p); err != nil || len(recs) != 0 || len(bad) != 0 {
		t.Fatalf("a store without a tasks directory: %v %v %v", recs, bad, err)
	}
	if _, err := os.Lstat(filepath.Join(p, "tasks")); err == nil {
		t.Fatal("scanning created the tasks directory")
	}
	s, err := openStore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"b", "a"} {
		if err = s.write("tasks", id, TaskRecord{TaskID: id, Owner: "claude"}, true); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.write("tasks", "renamed", TaskRecord{TaskID: "other-task", Owner: "claude"}, true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err = os.WriteFile(filepath.Join(p, "tasks", "bad.json"), []byte(`{"unknown":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(p, "tasks", "open.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	recs, bad, err := collectTasks(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0].TaskID != "a" || recs[1].TaskID != "b" {
		t.Errorf("records (sorted by id): %+v", recs)
	}
	if !reflect.DeepEqual(bad, []string{"bad", "open", "renamed"}) {
		t.Errorf("unreadable: %v", bad)
	}
}

// A store with more records than the bound is an error of the whole scan: nothing is guessed.
func TestEachTaskIsBounded(t *testing.T) {
	p := privateDir(t)
	s, err := openStore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	for i := 0; i <= observationLimit; i++ {
		if err = os.WriteFile(filepath.Join(p, "tasks", "t"+strconv.Itoa(i)+".json"), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = collectTasks(p); err == nil {
		t.Fatal("visited a store over the record bound")
	}
}

// EachTask hands the records over one at a time and keeps none: the peak is one record, not the
// store (Spec 041 T011a5 follow-up). 40 records of about 1 MiB each are visited with the heap
// growing by far less than their 40 MiB.
func TestEachTaskStreamsTheRecords(t *testing.T) {
	p := privateDir(t)
	s, err := openStore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("w", 1<<20-4096)
	const count = 40
	for i := 0; i < count; i++ {
		id := "t" + strconv.Itoa(1000+i)
		if err = s.write("tasks", id, TaskRecord{TaskID: id, Owner: "claude", RemainingWork: []string{big}}, true); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	var base, peak uint64
	heap := func() uint64 {
		var m runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}
	base = heap()
	seen := 0
	unreadable, err := EachTask(p, func(r TaskRecord) {
		seen++
		if len(r.RemainingWork) != 1 || r.Owner != "claude" {
			t.Errorf("record %+v", r.TaskID)
		}
		peak = max(peak, heap())
	})
	if err != nil || len(unreadable) != 0 || seen != count {
		t.Fatalf("seen %d unreadable %v err %v", seen, unreadable, err)
	}
	if growth := int64(peak) - int64(base); growth > 12<<20 {
		t.Errorf("the heap grew by %d MiB while the store holds %d MiB: the records are retained", growth>>20, count)
	}
}
