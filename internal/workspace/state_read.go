// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// readOnlyTasks opens the tasks of a private store without writing. A store whose tasks directory
// does not exist yet (a created default that nothing has written to) has no tasks and returns nil.
func readOnlyTasks(stateDir string) (*store, error) {
	if _, err := os.Lstat(filepath.Join(stateDir, "tasks")); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return openStoreKinds(stateDir, false, []string{"tasks"})
}

// TaskIDs lists the task creation records of a private store, read only.
func TaskIDs(stateDir string) ([]string, error) {
	s, err := readOnlyTasks(stateDir)
	if s == nil || err != nil {
		return nil, err
	}
	defer s.Close()
	return s.list("tasks")
}

// ReadTask reads the creation record of one task from a private store, read only. A task without a
// record is not an error: found is false.
func ReadTask(stateDir, taskID string) (record TaskRecord, found bool, err error) {
	s, err := readOnlyTasks(stateDir)
	if s == nil || err != nil {
		return TaskRecord{}, false, err
	}
	defer s.Close()
	if err = s.read("tasks", taskID, &record); errors.Is(err, fs.ErrNotExist) {
		return TaskRecord{}, false, nil
	} else if err != nil {
		return TaskRecord{}, false, err
	}
	return record, true, nil
}

// ScanTasks reads every creation record of a private store, read only, sorted by task id. A record
// that cannot be read (wrong mode, unknown field, over the size bound) is skipped and its id
// returned in unreadable, so one bad record never hides the others and is never taken for absent.
// A store with more than observationLimit records, or one that cannot be listed, is an error.
func ScanTasks(stateDir string) (records []TaskRecord, unreadable []string, err error) {
	s, err := readOnlyTasks(stateDir)
	if s == nil || err != nil {
		return nil, nil, err
	}
	defer s.Close()
	ids, err := s.list("tasks")
	if err != nil {
		return nil, nil, err
	}
	records, unreadable = []TaskRecord{}, []string{}
	for _, id := range ids {
		var record TaskRecord
		// A record whose own task id is not its file name is as unreadable as a damaged one.
		if err := s.read("tasks", id, &record); err != nil || record.TaskID != id {
			unreadable = append(unreadable, id)
			continue
		}
		records = append(records, record)
	}
	return records, unreadable, nil
}
