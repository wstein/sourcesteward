// SPDX-License-Identifier: EUPL-1.2
package issues

import "time"

// States of a repository in the document.
const (
	StateOK      = "ok"
	StatePartial = "partial"
	StateSkipped = "skipped"
	StateError   = "error"
)

// CodeTooManyOpenIssues is the error of a repository with more than ten pages of open items
// (issues and pull requests together): the oldest 1000 are kept and the repository is partial.
const CodeTooManyOpenIssues = "too-many-open-issues"

// Row is one open issue with its priority. Pointer fields are null in the JSON contract:
// AgeDays is nil exactly when CreatedAt is zero, Comments is nil when GitHub gave none. URL is
// empty unless the forge gave an https address; Command and Effect are empty for blue and
// violet rows, which are not actions.
type Row struct {
	Repo, Title, URL, Author string
	Number                   int
	Bot                      bool
	CreatedAt                time.Time // zero when GitHub gave none
	AgeDays                  *int
	Labels                   []string // at most 20, API order
	LabelsTotal              int      // all labels returned, so a cut is visible
	Assignees                []string // at most 20
	Comments                 *int
	Priority, Reason         string
	Command, Effect          string
}

// RepoStatus is the per-repository line of the document. Open and PullsDropped are nil unless
// the issues were read (state ok or partial) or the repository has issues disabled (zero).
type RepoStatus struct {
	Repo         string
	State        string
	Reason       string // issues-disabled, only with StateSkipped
	Open         *int   // issues read, before the filters
	PullsDropped *int
	Error        string
}

// Error is one failure. Repo is empty for a failure of the whole run.
type Error struct{ Repo, Code, Message string }

// Counts is the number of issues per priority.
type Counts struct{ Red, Yellow, Blue, Violet int }

// Summary counts the rows. Total..Violet and Bots cover the listed rows (after the filters and
// the cap); Counts covers everything read with the filters applied and the cap not, which is
// what `stw status` shows. A repository is read when its state is ok or skipped, failed when it
// is partial or an error.
type Summary struct {
	Total, Red, Yellow, Blue, Violet int
	Omitted                          int
	Counts                           Counts
	Bots, ReposRead, ReposFailed     int
}

// Result is the snapshot. Rows are sorted by priority (red, yellow, blue, violet), then
// repository (ASCII case-insensitive), then number.
type Result struct {
	Time    time.Time
	Rows    []Row
	Repos   []RepoStatus // sorted by repository, ASCII case-insensitive
	Errors  []Error
	Summary Summary
}
