// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"errors"
	"os"
	"time"
)

// The shared journal reader of Spec 031 FR-042 and FR-047: restore and history
// read stored tidy v3 journals only through it. It opens the private store
// read-only (nothing is created, locked, repaired or rewritten), reads the
// immutable plan and the journal under the Spec 029 record bounds and
// validation, and leaves the V3Journal bytes exactly as written.

// errV3JournalAbsent means the store holds no journal of the identity.
var errV3JournalAbsent = errors.New("no stored tidy v3 journal")

const v3OperationsKind = "tidy-v3-operations"

// v3JournalView is one stored journal with its immutable plan. Time is the
// modification time of the journal record, which every journal transition
// refreshes; a journal carries no clock of its own.
type v3JournalView struct {
	Plan    V3Plan
	Journal V3Journal
	Time    time.Time
}

// openV3ReadStore opens the plan and operation records read-only. It refuses a
// state directory that is not private, current-user owned and canonical, as every
// tidy v3 store open does.
func openV3ReadStore(stateDir string) (*v3Store, error) {
	if !v3CanonicalPath(stateDir) {
		return nil, failure("invalid-options", "A canonical private state directory is required.")
	}
	raw, err := openStoreKinds(stateDir, false, []string{"tidy-v3-plans", v3OperationsKind})
	if err != nil {
		return nil, err
	}
	return &v3Store{store: raw, protected: []string{}}, nil
}

// readJournalView returns the validated plan and journal of id. The protection set
// that the plan was made under is the one its own record binds; the reader never
// supplies a different one.
func (s *v3Store) readJournalView(id string) (v3JournalView, error) {
	var view v3JournalView
	if !validID(id) {
		return view, failure("invalid-options", "A valid plan identity is required.")
	}
	if err := s.admitID(id); err != nil {
		return view, err
	}
	var stored v3StoredPlan
	if err := s.read("tidy-v3-plans", id, &stored); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return view, errV3JournalAbsent
		}
		return view, err
	}
	if stored.Native.ProtectedRoots == nil {
		return view, failure("invalid-state", "Stored native snapshot is incomplete.")
	}
	s.protected = append([]string{}, stored.Native.ProtectedRoots...)
	plan, _, err := s.readPlan(id)
	if err != nil {
		return view, err
	}
	journal, err := s.readJournal(plan)
	if errors.Is(err, os.ErrNotExist) {
		return view, errV3JournalAbsent
	}
	if err != nil {
		return view, err
	}
	info, err := s.dirs[v3OperationsKind].Lstat(id + ".json")
	if err != nil {
		return view, err
	}
	return v3JournalView{Plan: plan, Journal: journal, Time: info.ModTime().UTC()}, nil
}
