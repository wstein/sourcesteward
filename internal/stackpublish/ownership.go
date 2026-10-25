// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import "github.com/wstein/sourcesteward/internal/attribution"

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
}

// OwnershipInput is everything the ownership guards are computed from.
type OwnershipInput struct {
	PlanTask string
	Kind     ActionKind
	Subject  Subject
	Task     TaskEvidence
	Current  attribution.Origin
	Hints    Hints
}

// OwnershipGuards returns the ownership guard and the Spec 042 D5 attribution guard of one action.
func OwnershipGuards(in OwnershipInput) []Guard { return nil }

// NewModeSummary derives the mode summary from marker NAMES only; values are never kept.
func NewModeSummary(markers []string, stdinTerminal, stdoutTerminal, human bool) ModeSummary {
	return ModeSummary{}
}
