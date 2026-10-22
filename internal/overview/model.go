// SPDX-License-Identifier: EUPL-1.2

// Package overview is the action model behind `stw status` (Spec 037): pure functions that turn the
// per-repository facts (local status, pull requests, releases, tags, CI state, read outcomes) into
// action items with a priority, a category, a stable id and a suggested command, order and group them
// for the human output, and derive the notices and the summary counts. It reads nothing and writes
// nothing: no file, no network, no clock; the data comes in as plain values and the output is a pure
// function of them, whatever the order of the input.
package overview

import (
	"time"

	"github.com/wstein/sourcesteward/internal/stwsync"
)

// Priority is the level of an item; the order of Priorities is the display and sort order.
type Priority string

// The five priorities, most urgent first.
const (
	Red    Priority = "red"
	Yellow Priority = "yellow"
	Green  Priority = "green"
	Blue   Priority = "blue"
	Violet Priority = "violet"
)

// Priorities lists the priorities in their fixed order.
var Priorities = []Priority{Red, Yellow, Green, Blue, Violet}

// Category is one of the 25 category ids of the action table of Spec 037.
type Category string

// The categories; their position in categoryTable is the category number of the spec.
const (
	RepoError         Category = "repo-error"
	LocalOperation    Category = "local-operation"
	MainRed           Category = "main-red"
	ReleaseRunFailed  Category = "release-run-failed"
	ReadError         Category = "read-error"
	LocalChanges      Category = "local-changes"
	LocalDetached     Category = "local-detached"
	LocalUntracked    Category = "local-untracked"
	LocalBehind       Category = "local-behind"
	LocalNoUpstream   Category = "local-no-upstream"
	MainBehindOrigin  Category = "main-behind-origin"
	GitlinkDrift      Category = "gitlink-drift"
	WorktreeDirty     Category = "worktree-dirty"
	PRConflict        Category = "pr-conflict"
	PRBlocked         Category = "pr-blocked"
	TagWithoutRelease Category = "tag-without-release"
	PRReady           Category = "pr-ready"
	ReleaseDraft      Category = "release-draft"
	PRChecksPending   Category = "pr-checks-pending"
	PRUnread          Category = "pr-unread"
	LocalStash        Category = "local-stash"
	GitlinkAhead      Category = "gitlink-ahead"
	BranchesUnmerged  Category = "branches-unmerged"
	WorktreeStale     Category = "worktree-stale"
	PRBot             Category = "pr-bot"
)

// categoryTable is the action table of the spec in its order: the index plus one is the category
// number used by the ordering, the priority is the one of the row.
var categoryTable = []struct {
	id       Category
	priority Priority
}{
	{RepoError, Red}, {LocalOperation, Red}, {MainRed, Red}, {ReleaseRunFailed, Red},
	{ReadError, Yellow}, {LocalChanges, Yellow}, {LocalDetached, Yellow}, {LocalUntracked, Yellow},
	{LocalBehind, Yellow}, {LocalNoUpstream, Yellow}, {MainBehindOrigin, Yellow}, {GitlinkDrift, Yellow},
	{WorktreeDirty, Yellow}, {PRConflict, Yellow}, {PRBlocked, Yellow}, {TagWithoutRelease, Yellow},
	{PRReady, Green}, {ReleaseDraft, Green},
	{PRChecksPending, Blue}, {PRUnread, Blue}, {LocalStash, Blue}, {GitlinkAhead, Blue},
	{BranchesUnmerged, Blue}, {WorktreeStale, Blue},
	{PRBot, Violet},
}

// Categories returns the 25 category ids in the order of the spec table.
func Categories() []Category {
	out := make([]Category, len(categoryTable))
	for i, c := range categoryTable {
		out[i] = c.id
	}
	return out
}

// PriorityOf returns the priority of a category and false for an unknown one.
func PriorityOf(c Category) (Priority, bool) {
	for _, row := range categoryTable {
		if row.id == c {
			return row.priority, true
		}
	}
	return "", false
}

// Effect says what running a suggested command does; the owner runs it, `stw` never does.
type Effect string

// The two effects: Read changes nothing, Owner changes something and is the owner's operation.
const (
	Read  Effect = "read"
	Owner Effect = "owner"
)

// SubjectKind is the kind of the thing an item is about.
type SubjectKind string

// The subject kinds of the contract.
const (
	KindPR         SubjectKind = "pr"
	KindRelease    SubjectKind = "release"
	KindTag        SubjectKind = "tag"
	KindBranch     SubjectKind = "branch"
	KindRepository SubjectKind = "repository"
	KindWorktree   SubjectKind = "worktree"
	KindGitlink    SubjectKind = "gitlink"
	KindFact       SubjectKind = "fact"
	KindSource     SubjectKind = "source"
)

// Subject identifies the one thing a finding is about; precedence applies only within one subject.
// Number is set for a pull request, Tag for a release or tag, SHA for a tag or branch head, and Name
// carries the remaining id part: the fact kind (`dirty`, `behind`, ...), the branch, a path or a source id.
type Subject struct {
	Kind   SubjectKind
	Number int
	Tag    string
	SHA    string
	Name   string
}

// Item is one action item. Path, URL, Command, Effect and Hint are empty when absent; Effect is empty
// exactly when Command is. Title is safe for the terminal: forge text passed through untrusted.Esc.
// Evidence holds the small fixed facts of the category as strings, integers or booleans.
type Item struct {
	ID       string
	Priority Priority
	Category Category
	Repo     string
	Path     string
	Title    string
	URL      string
	Subject  Subject
	Evidence map[string]any
	Command  string
	Effect   Effect
	Hint     string
}

// Source is a remote source of one repository.
type Source string

// The remote sources of Spec 037, in the order of the spec.
const (
	SourceRepo      Source = "repo"
	SourcePulls     Source = "pulls"
	SourceReleases  Source = "releases"
	SourceTags      Source = "tags"
	SourceCIMain    Source = "ci-main"
	SourceCIRelease Source = "ci-release"
)

// Sources lists the remote sources in a fixed order.
var Sources = []Source{SourceRepo, SourcePulls, SourceReleases, SourceTags, SourceCIMain, SourceCIRelease}

// Outcome is how one source ended.
type Outcome string

// The outcomes of a source read.
const (
	OutcomeOK      Outcome = "ok"
	OutcomeSkipped Outcome = "skipped"
	OutcomeError   Outcome = "error"
)

// Skip reasons and error codes that the notices name; any other code is reported as it is.
const (
	ReasonNoToken        = "no-token"
	ReasonDeadline       = "deadline"
	ReasonNotFound       = "not-found"
	ReasonNotGitHub      = "not-github-origin"
	ReasonOtherHost      = "other-host"
	ReasonNotReadLocally = "not-read-locally"
	ReasonInvalidRepo    = "invalid-repo"
	CodeRateLimited      = "rate-limited"
	CodeNetwork          = "network"
)

// SourceState is the outcome of one source of one repository. Reason is the skip reason of a skipped
// source, Code the error code of a failed one; the other is empty.
type SourceState struct {
	Outcome Outcome
	Reason  string
	Code    string
}

// Visibility says whether the token can see release drafts.
type Visibility string

// The draft visibility values of the `repo` source.
const (
	WriteAccess   Visibility = "write-access"
	NoWriteAccess Visibility = "no-write-access"
	Unknown       Visibility = "unknown"
)

// The verdict groups of a pull request row (Spec 036), the strings of internal/pulls; a test keeps them equal.
const (
	GroupReady    = "ready"
	GroupChecks   = "checks"
	GroupConflict = "conflict"
	GroupDraft    = "draft"
	GroupBlocked  = "blocked"
)

// PRReasonNotRead is the reason of a blocked row whose detail was not read.
const PRReasonNotRead = "not-read"

// PR is one open pull request row as the pulls cycle read it. Group and Reason are the verdict of
// Spec 036; PastBudget is true when the row was not read because the detail budget was spent.
type PR struct {
	Number     int
	Title      string
	URL        string
	Author     string
	Bot        bool
	Draft      bool
	Group      string
	Reason     string
	PastBudget bool
}

// Draft is a pending release draft; CreatedAt is zero when GitHub gave none.
type Draft struct {
	Name      string
	Tag       string
	URL       string
	Assets    int
	CreatedAt time.Time
}

// Run is the newest release workflow run of a tag.
type Run struct {
	Workflow   string
	Conclusion string
	Status     string
	URL        string
}

// ReleaseTag is a tag the release workflow was read for: the latest tag of a product or an unreleased
// tag. Run is nil when no run was found.
type ReleaseTag struct {
	Tag        string
	SHA        string
	Unreleased bool
	Run        *Run
}

// Main is the CI state of the default branch. Class is `none`, `pending`, `green` or `red`; BehindOrigin
// is nil when it could not be determined.
type Main struct {
	Branch       string
	Head         string
	Class        string
	BehindOrigin *bool
}

// Remote is what the remote sources read for one repository. A field is meaningful only when its
// source ended `ok`; Sources carries the outcome of every source that was attempted or skipped.
type Remote struct {
	Sources    map[Source]SourceState
	Pulls      []PR
	Visibility Visibility
	Drafts     []Draft
	Tags       []ReleaseTag
	Main       *Main
}

// Repo is one repository of the workspace. Slug is its GitHub OWNER/NAME, empty when the remote sources
// did not apply (Remote is then nil or carries only skipped sources).
type Repo struct {
	Local  stwsync.RepoStatus
	Slug   string
	Remote *Remote
}

// Input is everything the model needs. Root is the workspace root used for `git -C ROOT add PATH`.
// Deadline is the overall deadline of the run and RateLimitReset the reset time GitHub gave (zero when
// it gave none); both only shape the wording of the notices.
type Input struct {
	Root           string
	Repos          []Repo
	Deadline       time.Duration
	RateLimitReset time.Time
}

// Notice is one message about a missing source: Code is the skip reason or error code, Source the
// source id (or `remote` when every remote source is affected), Repos the affected repositories.
type Notice struct {
	Code    string
	Source  string
	Repos   []string
	Message string
}

// Counts holds the number of items per priority.
type Counts struct {
	Red, Yellow, Green, Blue, Violet int
}

// Summary is the summary line of the output; Attention and Errors have their Spec 033 meaning.
type Summary struct {
	Repos      int
	Attention  int
	Errors     int
	Priorities Counts
	Degraded   bool
	Failed     bool
	Notices    int
}

// Overview is the complete result of Build.
type Overview struct {
	Items   []Item
	Notices []Notice
	Summary Summary
}
