// SPDX-License-Identifier: EUPL-1.2

// Package stwsync implements `stw sync` (Spec 032): the replacement for `git pull` in a workspace root
// repository and in each of its submodules that sits on a branch. Per repository it fetches the upstream
// remote and then fast-forwards the current branch (or, on request, rebases it or, only with the explicit --merge,
// merges it), creating a merge commit only for --merge and never losing a local commit. It starts Git itself, because the fetch needs the network, which the
// sandboxed internal/workspace readers deny. Branches other than the current one and linked worktrees
// are out of scope.
package stwsync

// Results of one repository. The plan names an action that a run would take (fast-forward, rebase); a
// run reports it as done (fast-forwarded, rebased).
const (
	ResultUpToDate             = "up-to-date"
	ResultAhead                = "ahead"
	ResultFastForward          = "fast-forward"
	ResultFastForwarded        = "fast-forwarded"
	ResultRebase               = "rebase"
	ResultRebased              = "rebased"
	ResultMerge                = "merge"
	ResultMerged               = "merged"
	ResultDiverged             = "diverged"
	ResultSkippedDirty         = "skipped-dirty"
	ResultSkippedDetached      = "skipped-detached"
	ResultSkippedNoUpstream    = "skipped-no-upstream"
	ResultSkippedBusy          = "skipped-busy"
	ResultSkippedUninitialized = "skipped-uninitialized"
	ResultConflict             = "conflict"
	ResultAutostashConflict    = "autostash-conflict"
	ResultFailed               = "failed"
	ResultUpdated              = "updated"     // --submodules: a pinned submodule moved to the recorded commit
	ResultUnreachable          = "unreachable" // --submodules: not initialised, or the recorded commit is not present
	// restore results
	ResultRestored          = "restored"
	ResultAlreadyRestored   = "already-restored"
	ResultChangedSince      = "changed-since"
	ResultJournalIncomplete = "journal-incomplete"
)

// Tri is an option that overrides the user's Git configuration when set.
type Tri int8

const (
	Unset Tri = iota // follow the Git configuration, as git pull does
	On
	Off
)

func (t Tri) json() *bool {
	if t == Unset {
		return nil
	}
	v := t == On
	return &v
}

// Options selects one run. Path is the workspace root repository (default the current directory).
// Rebase On is --rebase and Off is --ff-only; Autostash and Prune On/Off are --autostash/--no-autostash and
// --prune/--no-prune. Unset follows pull.rebase, branch.NAME.rebase, pull.ff, rebase.autoStash,
// merge.autoStash and fetch.prune exactly as `git pull` does.
type Options struct {
	Path      string
	Plan      bool // fetch the remote-tracking refs and report; change no branch, worktree or stash
	Rebase    Tri
	Autostash Tri
	Prune     Tri
	// Merge is --merge, the only way to get a merge commit (owner decision 2026-10-24): a diverged branch is merged. No Git
	// configuration enables it, and a branch that can be fast-forwarded is still fast-forwarded. Exclusive with Rebase.
	Merge bool
	// Submodules is --submodules: detached (pinned) submodules are brought to the commit the superproject records.
	Submodules bool
}

// ResultOptions echoes the requested options; null means the Git configuration decides.
type ResultOptions struct {
	Rebase     *bool `json:"rebase"`
	Prune      *bool `json:"prune"`
	Autostash  *bool `json:"autostash"`
	Merge      bool  `json:"merge"`
	Submodules bool  `json:"submodules"`
}

// Repo is the root repository or one submodule. Branch, Remote, OldOID and NewOID are nil when unknown.
type Repo struct {
	Name   string  `json:"name"`
	Path   string  `json:"path"`
	Kind   string  `json:"kind"`
	Branch *string `json:"branch"`
	Remote *string `json:"remote"`
	Result string  `json:"result"`
	OldOID *string `json:"old_oid"`
	NewOID *string `json:"new_oid"`
	Ahead  int     `json:"ahead"`
	Behind int     `json:"behind"`
	Pruned int     `json:"pruned"`
	Reason *string `json:"reason"`
	Detail *string `json:"detail"` // Git's own message for a stopped rebase or a kept autostash
	// StashOID is the stash entry git kept after an autostash that could not be reapplied (autostash-conflict); nil when
	// there is none or when the entry cannot be identified without doubt.
	StashOID *string `json:"stash_oid"`
}

// Summary counts the repositories by result.
type Summary struct {
	Total    int            `json:"total"`
	ByResult map[string]int `json:"by_result"`
}

// Failure is a refusal or an operational error of the whole run.
type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Result is the one model behind the human and the JSON output.
type Result struct {
	SchemaVersion int           `json:"schema_version"`
	Command       string        `json:"command"`
	Mode          string        `json:"mode"`
	RunID         *string       `json:"run_id"`
	Options       ResultOptions `json:"options"`
	Note          *string       `json:"note"` // set when the command ran inside a linked worktree, which it did not touch
	Repos         []Repo        `json:"repos"`
	Summary       Summary       `json:"summary"`
	Error         *Failure      `json:"error"`
}

// ExitCode is 1 when the run was refused or any repository ended failed, conflict (a rebase or merge left in progress),
// autostash-conflict or unreachable (--submodules), or a restore ended changed-since or journal-incomplete, and 0
// otherwise: a repository that is up to date or skipped (dirty, diverged, detached, busy, without upstream) is not
// an error. Usage errors (exit 2) are decided before any Result exists.
func (r Result) ExitCode() int {
	if r.Error != nil {
		return 1
	}
	for _, it := range r.Repos {
		switch it.Result {
		case ResultFailed, ResultConflict, ResultAutostashConflict, ResultChangedSince, ResultJournalIncomplete, ResultUnreachable:
			return 1
		}
	}
	return 0
}
