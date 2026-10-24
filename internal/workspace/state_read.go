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

// EachTask reads the creation records of a private store one at a time, read only, in task id
// order, and hands each to visit; no record is kept, so the peak is one record and not the store.
// visit must copy what it needs. A record that cannot be read (wrong mode, unknown field, over the
// size bound, or whose own task id is not its file name) is skipped and its id returned in
// unreadable, so one bad record never hides the others and is never taken for absent. A store with
// more than observationLimit records, or one that cannot be listed, is an error.
func EachTask(stateDir string, visit func(TaskRecord)) (unreadable []string, err error) {
	s, err := readOnlyTasks(stateDir)
	if s == nil || err != nil {
		return nil, err
	}
	defer s.Close()
	ids, err := s.list("tasks")
	if err != nil {
		return nil, err
	}
	unreadable = []string{}
	for _, id := range ids {
		var record TaskRecord
		if err := s.read("tasks", id, &record); err != nil || record.TaskID != id {
			unreadable = append(unreadable, id)
			continue
		}
		visit(record)
	}
	return unreadable, nil
}
