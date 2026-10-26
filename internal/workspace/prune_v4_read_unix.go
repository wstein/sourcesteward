// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

// Contract-4 plans cannot inherit the larger legacy store record allowance.
func pruneV4ReadPlanBytes(s *store, id string) ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	before, err := s.dirs["plans"].Lstat(id + ".json")
	if err != nil {
		return nil, err
	}
	if !regularPrivate(before) || before.Size() > pruneV4RecordLimit {
		return nil, failure("invalid-state", "Record must be a private bounded single-link regular file.")
	}
	b, err := readPruneV4PrivateFile(s.dirs["plans"], id+".json", pruneV4RecordLimit)
	if err != nil {
		return nil, failure("changed-state", "Stored contract-4 plan changed while reading.")
	}
	if err = s.validate(); err != nil {
		return nil, err
	}
	return b, nil
}
