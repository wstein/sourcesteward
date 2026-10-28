// SPDX-License-Identifier: EUPL-1.2

package workspace

import "path/filepath"

// This file exposes, read-only, the checks that the tidy v3 planners run on a repository and on
// a retirement, so that an analysis can keep a row the planner would refuse instead of offering
// it (Spec 044, "an offered row is a plannable row"). Nothing here writes, locks or fetches.

// Codes of the admission checks. The retirement code has no code in the planner, which reports a
// plain physical error.
const (
	// AdmissionRetirementParentForeign: the worktree directory, the directory around it or a
	// sibling in that directory is not owned by the user, or not a plain file or directory.
	AdmissionRetirementParentForeign = "retirement-parent-foreign"
)

// RetirementDestinationAdmission reports whether the worktree directory dest is a real directory
// owned by the user, the first of the physical checks of a retirement plan.
func RetirementDestinationAdmission(dest string) error {
	_, err := retirementV3Directory(dest)
	return err
}

// RetirementParentAdmission reports whether the directory that holds worktrees is a real
// directory owned by the user and holds only plain files and directories that the user owns,
// within the observation limit: the check of a retirement plan on the parent of the destination
// and on its siblings, for every entry of the parent. A shared directory such as /tmp fails it.
func RetirementParentAdmission(parent string) error {
	id, err := retirementV3Directory(parent)
	if err != nil {
		return err
	}
	_, err = retirementV3Siblings(id.Path, "")
	return err
}

// RetirementLocationAdmission is both checks for one worktree directory: the destination and
// its parent. A nil result means these checks pass; the plan runs more.
func RetirementLocationAdmission(dest string) error {
	if err := RetirementDestinationAdmission(dest); err != nil {
		return err
	}
	return RetirementParentAdmission(filepath.Dir(dest))
}
