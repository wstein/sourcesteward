// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"context"
	"time"
)

// Observer yields one repository snapshot per call. The hosting reader of WP-B implements it over the
// conditional REST reads; the tests implement it with fake snapshots. The core performs no I/O besides
// its own state directory and never writes to a forge.
type Observer interface {
	Observe(ctx context.Context, repo string, hints Hints) (RepoObservation, error)
}

// Hints tell the observer what the state makes necessary, so that it can read "only when needed"
// (Spec 035, "Observation"): detail for PRs marked due, checks for pending or unobserved PRs.
type Hints struct {
	Cycle       int
	DetailEvery int
	Known       []KnownPR // open PRs of the accepted state, ascending by number
	Cond        *Conditional
}

// KnownPR is the accepted view of one open PR.
type KnownPR struct {
	Number    int
	Head      string
	Class     string // "" when unobserved
	ClassCand string // a checks class seen once and not yet accepted ("" when none)
	DetailDue bool
}

// RepoObservation is one snapshot of a repository.
type RepoObservation struct {
	DefaultBranch string
	PRs           []PRObs         // the complete open list
	Gone          map[int]GoneObs // pulls/N for PRs that left the open list; absent when unreadable
	Cond          *Conditional    // replacement ETag/Link/page store, nil when unchanged
}

// PRObs is one open PR. Detail and Checks are nil when this cycle did not read them: the accepted
// values are then kept.
type PRObs struct {
	Number  int
	Title   string
	Author  string
	Labels  []string
	Draft   bool
	Head    string
	HeadRef string
	Base    string
	Fork    bool // the head repository differs from the base repository
	// ForkKnown is true only when pulls/N was read in this observation; otherwise Fork is not an answer
	// and the accepted value is kept.
	ForkKnown bool
	Detail    *DetailObs
	Checks    *ChecksObs
}

// DetailObs is pulls/N: a nil Mergeable is "not computed" and keeps the previous value.
type DetailObs struct {
	Mergeable *bool
	State     string // GitHub mergeable_state
}

// ChecksObs is the raw check data of the PR head; the core classifies it.
type ChecksObs struct {
	Runs     []CheckRun
	Statuses []StatusContext
}

// GoneObs is pulls/N of a PR that left the open list.
type GoneObs struct {
	Merged      bool
	MergeCommit *string // nil when unreadable
	MergedAt    string
	Base        string
	Head        string
	HeadRef     string
	MethodHint  string // merge, rebase-or-squash, unknown
}

// ObserveError is a classified read failure. Code is one of the watch-error codes; RunWide failures
// (for example rate-limited) belong to the run, not to the repository.
type ObserveError struct {
	Code    string
	Message string
	RunWide bool
	RetryIn time.Duration // zero: use the repository backoff
}

func (e *ObserveError) Error() string { return e.Code + ": " + e.Message }

// Restacker is the hook of WP-D: it selects the open PRs that must be restacked after a merge. The core
// calls it in the cycle of the merge and again after a restart for every restack_due entry, and
// emits only the (child, merge commit) pairs not yet journalled. The engine fills the merged PR, base,
// old parent and new parent of the event from the due entry.
type Restacker interface {
	Candidates(ctx context.Context, repo string, due RestackDue, open []OpenPR) ([]RestackCandidate, error)
}

// OpenPR is an open PR as the restacker sees it.
type OpenPR struct {
	Number  int
	Base    string
	HeadRef string
	Head    string
	Fork    bool
}

// RestackCandidate is one child to report.
type RestackCandidate struct {
	PR     int
	Reason string // child-of-merged-branch or contains-merged-commits
	Branch string
	Argv   []string // nil when the new parent is unknown or the head repository is a fork
}
