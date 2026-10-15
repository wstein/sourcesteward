// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

func openRetirementStore(path string, write bool) (*store, error) {
	return openStoreKinds(path, write, []string{"plans", "tasks", "operations", "tidy-retirement-plans", "tidy-retirement-operations"})
}
func retirementRecordDigest(value any) string {
	b, _ := json.Marshal(value)
	hash := sha256.Sum256(b)
	return fmt.Sprintf("%x", hash)
}
func retirementPlanDigest(p RetirementPlanRecord) string {
	p.Digest = ""
	return retirementRecordDigest(p)
}
func readRetirementOwnership(s *store, taskID, owner string) (MutationPlan, TaskRecord, string, error) {
	var p MutationPlan
	var task TaskRecord
	var j journal
	if !validID(taskID) || !validID(owner) {
		return p, task, "", failure("invalid-ownership", "Exact task and owner labels are required.")
	}
	if err := s.read("tasks", taskID, &task); err != nil {
		return p, task, "", err
	}
	if task.TaskID != taskID || task.Owner != owner || !validID(task.PlanID) {
		return p, task, "", failure("invalid-ownership", "Task record does not match the exact requested identity and owner label.")
	}
	if err := s.read("plans", task.PlanID, &p); err != nil {
		return p, task, "", err
	}
	if err := validatePlan(p, s.path, task.PlanID); err != nil {
		return p, task, "", err
	}
	if !validTask(task, p) {
		return p, task, "", failure("invalid-ownership", "Task ownership does not agree with its immutable creation plan.")
	}
	if err := s.read("operations", p.PlanID, &j); err != nil {
		return p, task, "", err
	}
	if !validJournal(j, p) || j.Phase != "complete" || j.Native == nil || !j.Native.Complete || !j.Native.SourceUnchanged || j.DestinationIdentity == nil || *j.DestinationIdentity != task.DestinationIdentity || j.Native.Identity != task.Native {
		return p, task, "", failure("invalid-ownership", "Completed creation journal, native creation identity and task ownership disagree.")
	}
	return p, task, retirementRecordDigest(j), nil
}
func verifyRetirementOwnership(s *store, p RetirementPlanRecord) error {
	creation, task, digest, err := readRetirementOwnership(s, p.Task.TaskID, p.Task.Owner)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(creation, p.CreationPlan) || !reflect.DeepEqual(task, p.Task) || digest != p.CreationJournalDigest {
		return failure("changed-ownership", "Original creation plan, task or completed journal changed; retirement is refused.")
	}
	return nil
}
func protectRetirementWorkspace(source, target, cwd string) error {
	if target == string(filepath.Separator) || overlaps(source, target) || cwd == target || strings.HasPrefix(cwd, target+string(filepath.Separator)) {
		return failure("active-workspace", "The explicit source, repository root or actual current workspace cannot be retired.")
	}
	return nil
}
func retirementJournalPointer(p RetirementPlanRecord) string {
	return filepath.Join(p.StateDir, "tidy-retirement-operations", p.PlanID+".json")
}

// Only known namespaces are inspected, and every selected directory is reopened
// through the shared descriptor-pinned private store validator. No write occurs.
func openOptionalTidyStore(path string) (*store, error) {
	root, _, err := openCanonical(path)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	kinds := []string{}
	for _, kind := range []string{"tidy-plans", "tidy-retirement-plans"} {
		_, err := root.Lstat(kind)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		kinds = append(kinds, kind)
	}
	return openStoreKinds(path, false, kinds)
}
func tidyStoredKind(o TidyApplyOptions) (string, error) {
	if !validID(o.PlanID) || o.StateDir == "" || !validText(o.StateDir) {
		return "reference", nil
	}
	s, err := openOptionalTidyStore(o.StateDir)
	if err != nil {
		return "reference", err
	}
	defer s.Close()
	found := ""
	for _, kind := range []string{"tidy-plans", "tidy-retirement-plans"} {
		if s.dirs[kind] == nil {
			continue
		}
		st, err := s.dirs[kind].Lstat(o.PlanID + ".json")
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "reference", err
		}
		if !regularPrivate(st) || st.Size() > recordLimit {
			return "retirement", failure("invalid-state", "Stored plan must be private bounded regular state.")
		}
		if found != "" {
			return "retirement", failure("plan-collision", "Both local reference and retirement namespaces claim this plan ID.")
		}
		found = kind
	}
	if found == "tidy-retirement-plans" {
		return "retirement", nil
	}
	if found == "" && o.Authorize {
		return "reference", failure("unknown-plan", "No immutable tidy plan with this ID exists in the selected private state directory.")
	}
	return "reference", nil
}
func checkOtherTidyNamespace(s *store, id, other string) error {
	if err := s.validate(); err != nil {
		return err
	}
	before, err := s.root.Lstat(other)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !before.IsDir() || before.Mode().Perm() != 0700 || !privateOwner(before) {
		return failure("unsafe-state", "Other tidy namespace is not a private real directory.")
	}
	root, err := s.root.OpenRoot(other)
	if err != nil {
		return err
	}
	defer root.Close()
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		return failure("changed-state", "Other tidy namespace changed while opening.")
	}
	_, err = root.Lstat(id + ".json")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return failure("plan-collision", "Both local reference and retirement namespaces claim this plan ID.")
}
func currentRetirementCWD() (string, error) {
	path, err := os.Getwd()
	if err != nil {
		return "", failure("unknown-current-workspace", "Actual current directory is unavailable; retirement is refused.")
	}
	return canonicalSource(path)
}
func validateRetirementSource(ctx context.Context, p RetirementPlanRecord, cwd string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := protectRetirementWorkspace(p.SourcePath, p.Task.Destination, cwd); err != nil {
		return err
	}
	for _, path := range []string{p.SourcePath, p.Task.Destination, p.Expected.Target.RepositoryDir, p.Expected.Target.GitDir} {
		if path != "" && overlaps(p.StateDir, path) {
			return failure("state-overlap", "Retirement state must be disjoint from source, target and native metadata.")
		}
	}
	return nil
}
