// SPDX-License-Identifier: EUPL-1.2

// Package conflicts finds the open pull requests that no longer merge with their base (Spec 047):
// a probe merge per pull request against the current tip of its base, the files in conflict with the
// base commit that last touched each, a warning for files both sides changed, the stack status that
// follows from a conflict and the restack plan with the brief for the agent that must do it.
// The detection reads only. Its one local effect is the disclosed fetch of the pull request heads
// and the base tips into the private namespace refs/stw/conflicts/; the probe merge itself writes its
// objects into a temporary quarantine directory that is removed afterwards. The plan and the brief
// are text: nothing here rebases, pushes or changes a branch.
package conflicts

import "context"

// States of a Finding.
const (
	StateConflicts = "conflicts-with-base"
	StateClean     = "clean"
	StateUnknown   = "unknown"
)

// Reasons of an unknown Finding.
const (
	ReasonFetchFailed = "fetch-failed"
	ReasonHeadMoved   = "head-moved"
	ReasonRefInvalid  = "ref-invalid"
	ReasonPastLimit   = "past-probe-limit"
	ReasonProbeFailed = "probe-failed"
	ReasonNoBase      = "no-merge-base"
)

// Bounds.
const (
	MaxProbed = 100 // pull requests probed per run; the rest is unknown
	MaxFiles  = 200 // files listed per finding
	MaxString = 2048
	// NamespaceRoot is the private ref namespace of the fetch.
	NamespaceRoot = "refs/stw/conflicts/"
)

// PR is the part of a pull request the detection needs. Head is the head SHA the forge listed.
type PR struct {
	Number  int
	Base    string // base branch
	HeadRef string
	Head    string
	Draft   bool
	Fork    bool
}

// FileConflict is a file the probe merge could not combine and the commit on the base that last
// touched it since the merge base (empty when none is found).
type FileConflict struct {
	Path       string
	BaseCommit string
}

// Finding is the result of the probe of one pull request.
type Finding struct {
	Number    int
	State     string
	Reason    string // set when State is unknown
	Base      string // base branch
	BaseSHA   string // tip of the base at the probe
	Head      string // head probed (equals the listed head)
	MergeBase string
	Files     []FileConflict // State conflicts-with-base
	Overlap   []string       // State clean: files the base and the pull request both changed (early warning)
	Truncated bool           // a list was cut at MaxFiles
	BlockedBy int            // number of the lowest conflicting ancestor in the stack, 0 when none
}

// Git is the local view of Git the detection uses. Implementations must not change HEAD, the index,
// the working tree, the configuration or any ref outside NamespaceRoot.
type Git interface {
	// Fetch fetches the pull request heads and the base branches into NamespaceRoot.
	Fetch(ctx context.Context, remote string, pulls []int, bases []string) error
	// Resolve resolves a ref to a commit SHA, "" when absent.
	Resolve(ctx context.Context, ref string) (string, error)
	// MergeBase returns the merge base of two commits, "" when there is none.
	MergeBase(ctx context.Context, a, b string) (string, error)
	// MergeTree probes the merge of head into base. It reports the files in conflict, in order.
	MergeTree(ctx context.Context, base, head string) (conflict bool, files []string, err error)
	// LastTouch returns the last commit in from..to that changed path, "" when none.
	LastTouch(ctx context.Context, from, to, path string) (string, error)
	// Changed lists the files changed from..to.
	Changed(ctx context.Context, from, to string) ([]string, error)
}
