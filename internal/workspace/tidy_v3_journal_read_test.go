// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

// Spec 031 T047: the shared journal reader is read-only, bounded and leaves the
// V3Journal exactly as production wrote it.
func TestJournalReaderReadsTheStoredJournalWithoutTouchingTheStore(t *testing.T) {
	f := newRestoreFix(t, "one", "two")
	before := treeBytes(t, f.state)
	s, err := openV3ReadStore(f.state)
	if err != nil {
		t.Fatal(err)
	}
	view, err := s.readJournalView(f.stored.PlanID)
	s.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(view.Plan, f.stored) || view.Journal.Phase != "complete" || len(view.Journal.Entries) != 2 {
		t.Fatalf("wrong view: %+v", view.Journal)
	}
	if !reflect.DeepEqual(readTidyV3ApplyJournal(t, f.stored), view.Journal) {
		t.Fatal("the shared reader and the apply reader disagree")
	}
	info, err := os.Lstat(f.journalPath())
	if err != nil || !view.Time.Equal(info.ModTime()) {
		t.Fatalf("journal time %v, file time %v (%v)", view.Time, info.ModTime(), err)
	}
	if !reflect.DeepEqual(before, treeBytes(t, f.state)) {
		t.Fatal("reading changed the private store")
	}
}

// A reader never takes the writer lock: it reads while another operation holds it.
func TestJournalReaderReadsWhileTheWriterLockIsHeld(t *testing.T) {
	f := newRestoreFix(t, "one")
	writer, err := openStoreKinds(f.state, true, []string{"tidy-v3-plans", v3OperationsKind})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	s, err := openV3ReadStore(f.state)
	if err != nil {
		t.Fatalf("the reader waited for or took the writer lock: %v", err)
	}
	defer s.Close()
	if _, err := s.readJournalView(f.stored.PlanID); err != nil {
		t.Fatal(err)
	}
}

func TestJournalReaderDistinguishesAbsentFromUnreadable(t *testing.T) {
	f := newRestoreFix(t, "one")
	s, err := openV3ReadStore(f.state)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.readJournalView("no-such-plan"); !errors.Is(err, errV3JournalAbsent) {
		t.Fatalf("unknown plan: %v", err)
	}
	if _, err := s.readJournalView("bad id"); err == nil || errors.Is(err, errV3JournalAbsent) {
		t.Fatalf("malformed identity: %v", err)
	}
	if err := os.WriteFile(f.journalPath(), []byte(`{"schema_version":3,`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readJournalView(f.stored.PlanID); err == nil || errors.Is(err, errV3JournalAbsent) {
		t.Fatalf("damaged journal: %v", err)
	}
	if err := os.Remove(f.journalPath()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readJournalView(f.stored.PlanID); !errors.Is(err, errV3JournalAbsent) {
		t.Fatalf("plan without a journal: %v", err)
	}
}

func TestJournalReaderRefusesAnOversizedRecord(t *testing.T) {
	f := newRestoreFix(t, "one")
	big := make([]byte, recordLimit+1)
	for i := range big {
		big[i] = ' '
	}
	if err := os.WriteFile(f.journalPath(), big, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := openV3ReadStore(f.state)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.readJournalView(f.stored.PlanID); err == nil || errors.Is(err, errV3JournalAbsent) {
		t.Fatalf("oversized journal accepted: %v", err)
	}
}
