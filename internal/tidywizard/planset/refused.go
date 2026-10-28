// SPDX-License-Identifier: EUPL-1.2

package planset

import (
	"fmt"
	"strings"
)

// SubPlanFailure is one sub-plan that is refused: its repository, kind and worktree, the ids of the
// rows it covers (so a caller can name the group) and the cause. Its text carries the prefix
// "planning KIND sub-plan of REPO" once.
type SubPlanFailure struct {
	Repo     string
	Kind     SubKind
	Worktree string
	Items    []string
	Err      error
}

func (f *SubPlanFailure) Error() string {
	return fmt.Sprintf("planning %s sub-plan of %s: %v", f.Kind, f.Repo, f.Err)
}

func (f *SubPlanFailure) Unwrap() error { return f.Err }

// Refused is the error of a set that cannot be planned: every sub-plan that fails its check, in
// the order of the sub-plans, not only the first. Nothing was planned or stored.
type Refused struct {
	Failures []*SubPlanFailure
}

func (r *Refused) Error() string {
	parts := make([]string, 0, len(r.Failures))
	for _, f := range r.Failures {
		parts = append(parts, f.Error())
	}
	return strings.Join(parts, "; ")
}

// Unwrap lets errors.Is and errors.As see every failure.
func (r *Refused) Unwrap() []error {
	out := make([]error, 0, len(r.Failures))
	for _, f := range r.Failures {
		out = append(out, f)
	}
	return out
}
