// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testEvent(id string) Event {
	return Event{SchemaVersion: 1, ID: id, Time: "2026-10-22T09:00:00Z", Kind: KindReposChanged,
		Details: &ReposChangedDetails{Added: []string{}, Removed: []string{}, Skipped: []Skipped{}}}
}

func TestJournalSeqIsGaplessAndDerivedFromTheTail(t *testing.T) {
	dir := t.TempDir()
	j, err := openJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if j.Tail() != 0 {
		t.Fatalf("empty tail %d", j.Tail())
	}
	for i, id := range []string{"a", "b", "c"} {
		e, err := j.Append(testEvent(id))
		if err != nil || e.Seq != int64(i+1) {
			t.Fatalf("append %v %v", e.Seq, err)
		}
	}
	j.Close()
	j, err = openJournal(dir)
	if err != nil || j.Tail() != 3 {
		t.Fatalf("reopen tail %d %v", j.Tail(), err)
	}
	defer j.Close()
	e, _ := j.Append(testEvent("d"))
	if e.Seq != 4 {
		t.Fatalf("seq %d", e.Seq)
	}
	got, err := j.After(2)
	if err != nil || len(got) != 2 || got[0].ID != "c" || got[1].ID != "d" {
		t.Fatalf("after: %v %v", got, err)
	}
	st, _ := os.Stat(filepath.Join(dir, "journal.jsonl"))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
}

func TestJournalLinesAreEventsOnly(t *testing.T) {
	dir := t.TempDir()
	j, _ := openJournal(dir)
	j.Append(testEvent("a"))
	j.Close()
	raw, _ := os.ReadFile(filepath.Join(dir, "journal.jsonl"))
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 1 || !strings.HasSuffix(string(raw), "\n") {
		t.Fatalf("%q", raw)
	}
	var m map[string]any
	if json.Unmarshal([]byte(lines[0]), &m) != nil || m["kind"] != KindReposChanged {
		t.Fatal(lines[0])
	}
}

func TestJournalTruncatedTailIsIgnoredAndHealed(t *testing.T) {
	dir := t.TempDir()
	j, _ := openJournal(dir)
	j.Append(testEvent("a"))
	j.Close()
	path := filepath.Join(dir, "journal.jsonl")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"schema_version":1,"seq":2,"id":"b","ti`)
	f.Close()
	j, err := openJournal(dir)
	if err != nil || j.Tail() != 1 {
		t.Fatalf("tail %d %v", j.Tail(), err)
	}
	e, err := j.Append(testEvent("c"))
	if err != nil || e.Seq != 2 {
		t.Fatalf("%v %v", e.Seq, err)
	}
	j.Close()
	j, err = openJournal(dir)
	if err != nil || j.Tail() != 2 {
		t.Fatalf("after heal: %d %v", j.Tail(), err)
	}
	all, _ := j.After(0)
	if len(all) != 2 || all[1].ID != "c" {
		t.Fatalf("%v", all)
	}
	j.Close()
	raw, _ := os.ReadFile(path)
	for _, l := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		var e Event
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("every journal line must validate, got %q: %v", l, err)
		}
	}
}

func TestJournalCompleteButUnterminatedLastLineIsIgnored(t *testing.T) {
	dir := t.TempDir()
	j, _ := openJournal(dir)
	e, _ := j.Append(testEvent("a"))
	j.Close()
	e.Seq = 2
	e.ID = "b"
	raw, _ := json.Marshal(e)
	f, _ := os.OpenFile(filepath.Join(dir, "journal.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	f.Write(raw) // no newline: the write was cut before it, nothing was printed
	f.Close()
	j, err := openJournal(dir)
	if err != nil || j.Tail() != 1 {
		t.Fatalf("%d %v", j.Tail(), err)
	}
	j.Close()
}

func TestJournalCorruptionIsAnError(t *testing.T) {
	for name, body := range map[string]string{
		"garbage middle line": `{"schema_version":1,"seq":1,"id":"a","time":"t","repo":null,"pr":null,"kind":"repos-changed","details":{"added":[],"removed":[],"skipped":[]}}` + "\nnot json\n",
		"seq gap":             `{"schema_version":1,"seq":2,"id":"a","time":"t","repo":null,"pr":null,"kind":"repos-changed","details":{"added":[],"removed":[],"skipped":[]}}` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "journal.jsonl"), []byte(body), 0o600)
			if _, err := openJournal(dir); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
