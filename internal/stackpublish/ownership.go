// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"sort"
	"strings"

	"github.com/wstein/sourcesteward/internal/attribution"
)

// TaskEvidence is what the trusted workspace task record says. Absent and unreadable are
// different facts and are never merged.
type TaskEvidence struct {
	Found      bool
	Unreadable bool
	TaskID     string
	Complete   bool
	HeadRef    string
	Origin     *attribution.Origin
}

// Hints are caller labels and name conventions. They are accepted so that they can be shown to
// widen nothing: ownership comes only from the task record.
type Hints struct {
	CallerLabel  string
	BranchPrefix string
	AuthorEmail  string
}

// Subject is the fresh fact about what the action touches: the topic ref for a ref action, or the
// head ref of the PR for a PR action (nil when it could not be read).
type Subject struct {
	Ref       *string
	PRHeadRef *string
	HeadRepo  *RepoRef
	BaseRepo  *RepoRef
}

// RepoRef is the identity of a repository as the forge reports it.
type RepoRef struct {
	Host  string
	Owner string
	Name  string
}

// OwnershipInput is everything the ownership guards are computed from.
type OwnershipInput struct {
	Plan     Plan // the validated plan: its task and repository are the only ones compared
	PlanTask string
	PlanRepo RepoRef
	Kind     ActionKind
	Subject  Subject
	Task     TaskEvidence
	Current  attribution.Origin
	Hints    Hints // deliberately unused: see Hints
}

// OwnershipGuards returns the ownership guard and the Spec 042 D5 attribution guard of one action.
// It is pure: the task record and the current origin are read by the caller. No guard carries a
// session id.
func OwnershipGuards(in OwnershipInput) []Guard {
	return []Guard{ownershipGuard(in), attributionGuard(in)}
}

func guard(name string, result GuardResult, reason Reason) Guard {
	return Guard{Name: name, Result: result, Reason: reason}
}

// ownershipGuard passes only when the complete task record of THIS plan's task names the very
// branch (a ref action) or the PR head branch (a PR action) the action touches. An absent record
// is not-owned; an unreadable one is unknown; a name, label, prefix or author never counts.
func ownershipGuard(in OwnershipInput) Guard {
	t := in.Task
	switch {
	case t.Unreadable:
		return guard(GuardOwnership, GuardUnknown, ReasonOwnershipUnknown)
	case !t.Found, t.TaskID == "", t.TaskID != in.PlanTask:
		return guard(GuardOwnership, GuardFail, ReasonNotOwned)
	case !t.Complete, t.HeadRef == "":
		return guard(GuardOwnership, GuardUnknown, ReasonOwnershipUnknown)
	}
	var subject *string
	switch in.Kind {
	case KindPushTopic, KindCreatePR:
		subject = in.Subject.Ref
	case KindEditBody, KindRetarget, KindReady:
		subject = in.Subject.PRHeadRef
		// A PR is own only when its head and base live in the plan repository: the same branch
		// name in a fork is somebody else's PR. Unreadable evidence is unknown, never a pass.
		if in.Subject.HeadRepo == nil || in.Subject.BaseRepo == nil || !in.Subject.HeadRepo.known() || !in.Subject.BaseRepo.known() {
			return guard(GuardOwnership, GuardUnknown, ReasonOwnershipUnknown)
		}
		if !in.Subject.HeadRepo.same(in.PlanRepo) || !in.Subject.BaseRepo.same(in.PlanRepo) {
			return guard(GuardOwnership, GuardFail, ReasonNotOwned)
		}
	}
	switch {
	case subject == nil:
		return guard(GuardOwnership, GuardUnknown, ReasonOwnershipUnknown)
	case *subject != t.HeadRef:
		return guard(GuardOwnership, GuardFail, ReasonNotOwned)
	}
	return guard(GuardOwnership, GuardPass, "")
}

func (r RepoRef) known() bool { return r.Host != "" && r.Owner != "" && r.Name != "" }

// same compares host, owner and name the way GitHub does: case-insensitively.
func (r RepoRef) same(o RepoRef) bool {
	return strings.EqualFold(r.Host, o.Host) && strings.EqualFold(r.Owner, o.Owner) && strings.EqualFold(r.Name, o.Name)
}

// attributionGuard is the Spec 042 D5 gate for a push or Ready. A human run is never gated; an
// agent run on a task whose record cannot be read is refused.
func attributionGuard(in OwnershipInput) Guard {
	if in.Kind != KindPushTopic && in.Kind != KindReady {
		return guard(GuardAttribution, GuardNotApplicable, "")
	}
	if in.Current.Kind == attribution.KindHuman {
		return guard(GuardAttribution, GuardPass, "")
	}
	if in.Task.Unreadable {
		return guard(GuardAttribution, GuardFail, ReasonAttributionUnknown)
	}
	var recorded *attribution.Origin
	if in.Task.Found {
		recorded = in.Task.Origin
	}
	switch err := attribution.Gate(recorded, in.Current).(type) {
	case nil:
		return guard(GuardAttribution, GuardPass, "")
	case attribution.Refusal:
		if err.Reason == attribution.ReasonMismatch {
			return guard(GuardAttribution, GuardFail, ReasonAttributionMismatch)
		}
	}
	return guard(GuardAttribution, GuardFail, ReasonAttributionUnknown)
}

// NewModeSummary derives the mode summary from marker NAMES only; values are never kept, and
// anything that is not a plain upper-case name is dropped. Names are unique and sorted.
func NewModeSummary(markers []string, stdinTerminal, stdoutTerminal, human bool) ModeSummary {
	seen := map[string]bool{}
	names := []string{}
	for _, m := range markers {
		if markerGrammar.MatchString(m) && !seen[m] {
			seen[m] = true
			names = append(names, m)
		}
	}
	sort.Strings(names)
	derived := "agent"
	if human {
		derived = "human"
	}
	return ModeSummary{Derived: derived, StdinTerminal: stdinTerminal, StdoutTerminal: stdoutTerminal, Markers: names}
}
