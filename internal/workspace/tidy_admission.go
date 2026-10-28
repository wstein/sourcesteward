// SPDX-License-Identifier: EUPL-1.2

package workspace

import (
	"context"
	"errors"
	"path/filepath"
)

// This file exposes, read-only, the checks that the tidy v3 planners run on a repository and on
// a retirement, so that an analysis can keep a row the planner would refuse instead of offering
// it (Spec 044, "an offered row is a plannable row"). Nothing here writes, locks or fetches.

// Codes of the admission checks. The first two are the codes of the planner; the retirement code
// has none there, because the planner reports a plain physical error.
const (
	// AdmissionRetirementParentForeign: the worktree directory, the directory around it or a
	// sibling in that directory is not owned by the user, or not a plain file or directory.
	AdmissionRetirementParentForeign = "retirement-parent-foreign"
	// AdmissionUnknownWorkspacePath is the planner code of a registration whose root is missing,
	// replaced or not a canonical real directory and that the weaker rule does not admit.
	AdmissionUnknownWorkspacePath = "unknown-workspace-path"
	// AdmissionStateOverlap is the planner code of a registration that overlaps the state store.
	AdmissionStateOverlap = "state-overlap"
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

// RegistrationFact is what the registration check reads of one linked worktree registration.
type RegistrationFact struct {
	Path                   string
	Locked, Prunable, Bare bool
}

// RegistrationRefusal names the first registration the planners refuse and the planner code.
type RegistrationRefusal struct {
	Path, Code string
	Locked     bool
}

// RefusedRegistration applies to the registrations of one repository the check that every tidy
// v3 plan of that repository runs (qualifyTidyV3Registrations, with the same weaker rule for a
// prunable, unlocked registration whose directory is absent below a real parent). It returns the
// first registration that is refused, or false when the repository is admitted. state is the
// state directory the plan is written to ("" skips the overlap rule). It reads the file system
// and writes nothing.
func RefusedRegistration(ctx context.Context, regs []RegistrationFact, state string) (RegistrationRefusal, bool) {
	for _, r := range regs {
		if weaklyAdmittedRegistration(V3Registration{Path: r.Path, Locked: r.Locked, Prunable: r.Prunable, Bare: r.Bare}) {
			if state != "" && overlaps(state, r.Path) {
				return RegistrationRefusal{Path: r.Path, Code: AdmissionStateOverlap, Locked: r.Locked}, true
			}
			continue
		}
		if err := qualifyTidyWorkspaceRoots(ctx, []string{r.Path}, state); err != nil {
			code := AdmissionUnknownWorkspacePath
			var op operationError
			if errors.As(err, &op) && op.Code != "" {
				code = op.Code
			} else if ctx.Err() != nil {
				code = "evidence-incomplete"
			}
			return RegistrationRefusal{Path: r.Path, Code: code, Locked: r.Locked}, true
		}
	}
	return RegistrationRefusal{}, false
}
