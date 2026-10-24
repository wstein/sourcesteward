// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"os"
	"strings"
)

// V3StoredRetirement is the read-only state of a stored retirement plan (Spec 041 plan-set
// apply): the immutable plan, whether an earlier apply completed it, and whether the branch the
// worktree was attached to is gone. Completed means the operation journal is complete and the
// destination no longer exists. BranchGone is only ever true for a completed retirement whose
// plan recorded an attached branch that no longer exists in the source repository; a branch of
// that name at any tip counts as present.
type V3StoredRetirement struct {
	Plan       V3Plan
	Completed  bool
	BranchGone bool
}

// ReadTidyV3Retirement reads the stored plan and its journal without writing anything. A plan
// without a journal is not completed; an unreadable journal is an error.
func ReadTidyV3Retirement(ctx context.Context, stateDir, planID string) (V3StoredRetirement, error) {
	var out V3StoredRetirement
	s, err := openV3ReadStore(stateDir)
	if err != nil {
		return out, err
	}
	defer s.Close()
	if !validID(planID) {
		return out, failure("invalid-options", "A valid plan identity is required.")
	}
	if err = s.admitID(planID); err != nil {
		return out, err
	}
	var stored v3StoredPlan
	if err = s.read("tidy-v3-plans", planID, &stored); err != nil {
		return out, err
	}
	if stored.Native.ProtectedRoots == nil {
		return out, failure("invalid-state", "Stored native snapshot is incomplete.")
	}
	s.protected = append([]string{}, stored.Native.ProtectedRoots...)
	plan, _, err := s.readPlan(planID)
	if err != nil {
		return out, err
	}
	out.Plan = plan
	if plan.Retirement == nil {
		return out, nil
	}
	journal, err := s.readJournal(plan)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if journal.Phase != "complete" {
		return out, nil
	}
	if _, serr := os.Lstat(plan.Retirement.Destination.Path); !errors.Is(serr, os.ErrNotExist) {
		return out, nil
	}
	out.Completed = true
	ref := plan.Retirement.Target.HeadRef
	if ref == "" {
		return out, nil
	}
	bin, err := gitExecutable()
	if err != nil {
		return out, err
	}
	listed, err := gitRead(ctx, bin, plan.SourcePath, "for-each-ref", "--format=%(refname)", "--", ref)
	if err != nil {
		return out, err
	}
	out.BranchGone = true
	for _, line := range strings.Split(listed, "\n") {
		if strings.TrimSpace(line) == ref {
			out.BranchGone = false
		}
	}
	return out, nil
}
