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

// CodeViewerUnknown is the run-level error of `--assignee @me` when the login of the token
// could not be determined.
const CodeViewerUnknown = "viewer-unknown"

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
// the issues were read (state ok or partial) or the repository has issues disabled (zero). Counts
// is the number of issues of the repository per priority with the filters applied and the cap not,
// as Summary.Counts is for all repositories.
type RepoStatus struct {
	Repo         string
	State        string
	Reason       string // issues-disabled or viewer-unknown, only with StateSkipped
	Open         *int   // issues read, before the filters
	Counts       Counts
	PullsDropped *int
	Error        string
}

// States of the viewer read and the reasons of a skipped one.
const (
	ViewerOK         = "ok"
	ViewerSkipped    = "skipped"
	ViewerError      = "error"
	ReasonNoIdentity = "no-identity"       // 403 or 404 without rate-limit evidence: the token class cannot read `user`
	ReasonNotGitHub  = "not-github-origin" // no repository to read, so no request
)

// Viewer is the outcome of the one `user` read of a run. Login is set only with ViewerOK,
// Reason only with ViewerSkipped and Error (a code) only with ViewerError.
type Viewer struct{ State, Login, Reason, Error string }

// Error is one failure. Repo is empty for a failure of the whole run.
type Error struct{ Repo, Code, Message string }

// Counts is the number of issues per priority.
type Counts struct{ Red, Yellow, Blue, Violet int }

// Summary counts the rows. Total..Violet and Bots cover the listed rows (after the filters and
// the cap); Counts covers everything read with the filters applied and the cap not, which is
// what `stw status` shows. A repository is read when its state is ok or skipped because its issues
// are disabled, failed when it is partial or an error; one skipped for any other reason (viewer-unknown, a run that read nothing) is neither.
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
	Limit   int // the row cap of this run
	Viewer  Viewer
	Rows    []Row
	Repos   []RepoStatus // sorted by repository, ASCII case-insensitive
	Errors  []Error
	Summary Summary
}
