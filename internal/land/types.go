// SPDX-License-Identifier: EUPL-1.2

// Package land implements the planning side of `stw land --stack` (Spec 043): the read-only
// evaluation of a named chain of reviewed pull requests, the exact commands a landing would run
// and the JSON report. Every read comes through the Forge and Git interfaces, so the logic is
// tested with fakes and temporary repositories. Nothing in this file or in plan.go writes.
package land

import "context"

// Bounds of the chain mode (FR-060, FR-018).
const (
	MinChain          = 2
	MaxChain          = 8
	DefaultRangeBound = 200
	// MaxOpenPulls bounds the open pull request list read for the side-child check (FR-066);
	// a longer list is read as unknown, never as "no side child".
	MaxOpenPulls = 500
)

// Item is one pull request of the chain with the head the owner states was reviewed.
type Item struct {
	Number   int
	Reviewed string
}

// Options are the validated inputs of a chain plan.
type Options struct {
	Repo       string // OWNER/NAME
	Remote     string
	Chain      []Item // bottom first, tip last
	AckDraft   bool
	RangeBound int
	// Shapes is the allowlist of measured shapes; nil selects the embedded one.
	Shapes *ShapeSet
}

// Mode is the run mode evidence (Spec 041 detection); the land package only echoes it.
type Mode struct {
	Mode      string   `json:"mode"`
	StdinTTY  bool     `json:"stdin_tty"`
	StdoutTTY bool     `json:"stdout_tty"`
	Markers   []string `json:"markers"`
}

// Agent reports whether the mode is the agent mode: every mode but "human" is.
func (m Mode) Agent() bool { return m.Mode != "human" }

// Policy is the trusted workspace policy as far as landing reads it: the exact owner identity.
// A policy that is not Loaded blocks the identity precondition (FR-017); this package never
// invents a default.
type Policy struct {
	Loaded     bool
	OwnerName  string
	OwnerEmail string
	// Reviewers, when not empty, restricts review_evidence to these logins (FR-062).
	Reviewers []string
}

// RepoFacts are the repository facts landing reads (FR-012, FR-067).
type RepoFacts struct {
	DefaultBranch       string
	DeleteBranchOnMerge *bool // nil: unreadable
}

// PullFacts are the facts of one pull request as GitHub reports them.
type PullFacts struct {
	Number         int
	State          string // open or closed
	Merged         bool
	Draft          bool
	HeadSHA        string
	HeadRef        string
	HeadRepo       string
	BaseRef        string
	BaseRepo       string
	Mergeable      *bool // nil: not computed
	MergeableState string
	Rebaseable     *bool
	MergeCommit    string
}

// OpenPull is one entry of the open pull request list.
type OpenPull struct {
	Number  int
	BaseRef string
	HeadRef string
}

// CheckState is the state of one check context.
type CheckState string

// Check states.
const (
	CheckSuccess CheckState = "success"
	CheckPending CheckState = "pending"
	CheckFailure CheckState = "failure"
)

// CheckFacts are the check runs and statuses reported for one head. Contexts maps a context name to
// its state; an empty map is an empty rollup.
type CheckFacts struct {
	Contexts map[string]CheckState
}

// Review is one pull request review.
type Review struct {
	Reviewer string
	State    string
	CommitID string
}

// Event is one issue event of a pull request.
type Event struct {
	Name  string
	Actor string
}

// RequiredFacts are the required check contexts of the default branch: the union of the rulesets
// and classic protection (FR-012). Readable is false when either source could not be read.
type RequiredFacts struct {
	Readable bool
	Contexts []string
}

// RulesetFacts are the readable parts of the rules of the default branch (FR-067). Readable is
// false when the ruleset listing or a ruleset detail could not be read (403, plan limit).
type RulesetFacts struct {
	Readable bool
	// RuleTypes are the rule types of the active rulesets for the branch, deduplicated.
	RuleTypes []string
	// BypassModes are the bypass modes of all bypass actors (always, pull_request), deduplicated.
	BypassModes []string
	// Protection is "none" (404), "present" or "unreadable".
	Protection string
}

// Forge is the read-only view of the forge. Implementations must not write.
type Forge interface {
	Repository(ctx context.Context) (RepoFacts, error)
	Pull(ctx context.Context, number int) (PullFacts, error)
	OpenPulls(ctx context.Context) (pulls []OpenPull, truncated bool, err error)
	Checks(ctx context.Context, sha string) (CheckFacts, error)
	Reviews(ctx context.Context, number int) ([]Review, error)
	Events(ctx context.Context, number int) ([]Event, error)
	Required(ctx context.Context, branch string) (RequiredFacts, error)
	Rulesets(ctx context.Context, branch string) (RulesetFacts, error)
	Viewer(ctx context.Context) (string, error)
}

// Commit is one commit of a range with its identities.
type Commit struct {
	SHA            string
	AuthorName     string
	AuthorEmail    string
	CommitterName  string
	CommitterEmail string
}

// Ancestry is the verdict of `git merge-base --is-ancestor`.
type Ancestry int

// Ancestry verdicts: the exit codes 0, 1 and anything else (128: an object is absent).
const (
	AncestorYes Ancestry = iota
	AncestorNo
	AncestorUnknown
)

// Git is the local view of Git. Fetch is the one disclosed local effect (objects and the private
// namespace refs/stw/land/ only).
type Git interface {
	// RemoteRepo is OWNER/NAME of the remote URL, or "" when it is not a recognized GitHub URL.
	RemoteRepo(ctx context.Context, remote string) (string, error)
	// DefaultTip is the default branch tip of the remote (git ls-remote).
	DefaultTip(ctx context.Context, remote, branch string) (string, error)
	// Fetch fetches the pull request head refs and the default branch into the private namespace.
	Fetch(ctx context.Context, remote, branch string, numbers []int) error
	// Resolve resolves the private ref of a pull request head, "" when absent.
	Resolve(ctx context.Context, number int) (string, error)
	IsAncestor(ctx context.Context, older, newer string) (Ancestry, error)
	// Range lists the commits of from..to (at most limit+1 are returned).
	Range(ctx context.Context, from, to string, limit int) ([]Commit, error)
}
