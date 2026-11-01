// SPDX-License-Identifier: EUPL-1.2

// Package carryover decides, read-only, whether the review of a pull request head may be carried
// over to a rewritten head (specs/051-stw-review-carry-over). It runs the three conditions of the
// agent-workflow carry-over rule and prints the line a human or an agent posts; it posts nothing.
package carryover

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// SchemaVersion and CommandName identify the JSON document.
const (
	SchemaVersion = 1
	CommandName   = "review carry-over"
)

// States of the checks.
const (
	StateEqual      = "equal"
	StateDiffers    = "differs"
	StateUnknown    = "unknown"
	StateNone       = "none"
	StateFound      = "found"
	StateMatch      = "match"
	StateMismatch   = "mismatch"
	StateNotChecked = "not-checked"
)

// Reason codes of a refusal; Reasons lists every one that applies.
const (
	ReasonObjectMissing  = "object-missing"
	ReasonReadFailed     = "read-failed"
	ReasonPatchIDDiffers = "patch-id-differs"
	ReasonPatchIDEmpty   = "patch-id-empty"
	ReasonMergeCommits   = "merge-commits"
	ReasonRangeDiff      = "range-diff-not-equal"
	ReasonRangeEmpty     = "range-empty"
	ReasonHeadMismatch   = "pr-head-mismatch"
)

// MaxDeviations bounds the listed range-diff rows that are not "=".
const MaxDeviations = 200

// Request is a validated command line.
type Request struct {
	PR                                 int
	OldBase, OldHead, NewBase, NewHead string
	// Remote, when set, is fetched (the head of the pull request into the private namespace) and
	// the fetched head must equal NewHead.
	Remote string
}

// Side is one end of the comparison.
type Side struct {
	Base string `json:"base"`
	Head string `json:"head"`
}

// PatchIDCheck is condition 1: git patch-id --stable over the diff against the base, old and new.
type PatchIDCheck struct {
	State string `json:"state"`
	Old   string `json:"old"`
	New   string `json:"new"`
}

// MergeCheck is condition 2: merge commits in newBase..newHead.
type MergeCheck struct {
	State   string   `json:"state"`
	Count   int      `json:"count"`
	Commits []string `json:"commits"`
}

// Deviation is a range-diff row that is not "=" at the same position.
type Deviation struct {
	Old  *int   `json:"old"`
	New  *int   `json:"new"`
	Mark string `json:"mark"`
}

// RangeCheck is condition 3: range-diff shows "=" for every commit, at the same position.
type RangeCheck struct {
	State      string      `json:"state"`
	Total      int         `json:"total"`
	NotEqual   int         `json:"not_equal"`
	Deviations []Deviation `json:"deviations"`
	Truncated  bool        `json:"truncated"`
}

// HeadCheck compares the fetched pull request head with the new head (only with a remote).
type HeadCheck struct {
	State string `json:"state"`
}

// Result is the JSON document and the source of the human output.
type Result struct {
	SchemaVersion int          `json:"schema_version"`
	Command       string       `json:"command"`
	PR            int          `json:"pr"`
	From          Side         `json:"from"`
	To            Side         `json:"to"`
	PatchID       PatchIDCheck `json:"patch_id"`
	MergeCommits  MergeCheck   `json:"merge_commits"`
	RangeDiff     RangeCheck   `json:"range_diff"`
	PRHead        HeadCheck    `json:"pr_head"`
	CarryOver     bool         `json:"carry_over"`
	Reasons       []string     `json:"reasons"`
	Line          *string      `json:"line"`
}

// Git is the read-only Git of the check.
type Git interface {
	// FetchPull fetches the head of pull request n into the private namespace and returns it.
	FetchPull(ctx context.Context, remote string, n int) (string, error)
	HasCommit(ctx context.Context, sha string) (bool, error)
	// PatchID is `git diff base...head | git patch-id --stable`; "" when the diff is empty.
	PatchID(ctx context.Context, base, head string) (string, error)
	// Merges lists the merge commits of base..head.
	Merges(ctx context.Context, base, head string) ([]string, error)
	// Count is the number of commits of base..head.
	Count(ctx context.Context, base, head string) (int, error)
	RangeDiff(ctx context.Context, oldBase, oldHead, newBase, newHead string) (string, error)
}

// UsageError is an invalid command line (exit 2).
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

var (
	shaGrammar    = regexp.MustCompile(`\A[0-9a-f]{40}\z`)
	numberGrammar = regexp.MustCompile(`\A[1-9][0-9]{0,8}\z`)
	remoteGrammar = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9._-]{0,63}\z`)
)

// ValidSHA reports whether s is a full lowercase 40-hex object id. Anything else (a ref name, a
// leading dash, "..", a space) never reaches an argument vector.
func ValidSHA(s string) bool { return shaGrammar.MatchString(s) }

// ValidRemote reports whether s is a plain remote name.
func ValidRemote(s string) bool { return remoteGrammar.MatchString(s) && !strings.Contains(s, "..") }

// ParseRequest validates the four positional arguments, --pr and --remote.
func ParseRequest(args []string, pr, remote string) (Request, error) {
	var r Request
	if len(args) != 4 {
		return r, &UsageError{Msg: "want OLD_BASE OLD_HEAD NEW_BASE NEW_HEAD, four full 40-hex commit ids"}
	}
	for i, name := range []string{"OLD_BASE", "OLD_HEAD", "NEW_BASE", "NEW_HEAD"} {
		if !ValidSHA(args[i]) {
			return r, &UsageError{Msg: fmt.Sprintf("%s must be a full 40-hex commit id in lowercase", name)}
		}
	}
	if !numberGrammar.MatchString(pr) {
		return r, &UsageError{Msg: "--pr NUMBER is required (1 to 999999999)"}
	}
	if remote != "" && !ValidRemote(remote) {
		return r, &UsageError{Msg: "--remote is not a plain remote name"}
	}
	n, _ := strconv.Atoi(pr)
	return Request{PR: n, OldBase: args[0], OldHead: args[1], NewBase: args[2], NewHead: args[3], Remote: remote}, nil
}
