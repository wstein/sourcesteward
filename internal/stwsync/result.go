// SPDX-License-Identifier: EUPL-1.2

// Package stwsync implements the read-only planning half of `stw sync` (Spec 032): one fetch of the
// remote-tracking refs, then a classification of every local branch against its upstream. It starts
// Git itself, because the fetch needs the network, which the sandboxed internal/workspace readers
// deny. It never changes a local branch, worktree, stash or tag.
package stwsync

// Item results of a plan. Only fast-forward is an action; skipped-dirty and diverged need the owner.
const (
	ResultFastForward  = "fast-forward"
	ResultUpToDate     = "up-to-date"
	ResultAhead        = "ahead"
	ResultDiverged     = "diverged"
	ResultSkippedDirty = "skipped-dirty"
	ResultUpstreamGone = "upstream-gone"
)

// Options selects one plan. Remote defaults to origin.
type Options struct {
	Path   string
	Remote string
	Prune  bool
}

// ResultOptions echoes the options of the run. Rebase, autostash and submodules arrive in later slices.
type ResultOptions struct {
	Rebase     bool `json:"rebase"`
	Prune      bool `json:"prune"`
	Autostash  bool `json:"autostash"`
	Submodules bool `json:"submodules"`
}

// Fetch reports the one fetch of the run. PrunedRefs lists at most 256 removed remote-tracking refs.
type Fetch struct {
	Performed   bool     `json:"performed"`
	PrunedRefs  []string `json:"pruned_refs"`
	PrunedTotal int      `json:"pruned_total"`
}

// Item is one local branch with an upstream under the selected remote.
type Item struct {
	Branch   string  `json:"branch"`
	Worktree *string `json:"worktree"`
	Result   string  `json:"result"`
	OldOID   *string `json:"old_oid"`
	NewOID   *string `json:"new_oid"`
	Reason   *string `json:"reason"`
}

// Submodule is reserved for the submodule slice; a plan always reports an empty list.
type Submodule struct {
	Path   string  `json:"path"`
	Result string  `json:"result"`
	OldOID *string `json:"old_oid"`
	NewOID *string `json:"new_oid"`
}

// Summary counts the items by result; Untracked counts local branches without an upstream under the remote.
type Summary struct {
	Total     int            `json:"total"`
	ByResult  map[string]int `json:"by_result"`
	Untracked int            `json:"untracked"`
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
	Remote        string        `json:"remote"`
	Options       ResultOptions `json:"options"`
	Fetch         Fetch         `json:"fetch"`
	Items         []Item        `json:"items"`
	Submodules    []Submodule   `json:"submodules"`
	Summary       Summary       `json:"summary"`
	Error         *Failure      `json:"error"`
}

// ExitCode is 1 when the run failed or an item needs the owner (skipped-dirty, diverged), else 0.
// upstream-gone is only a note, so a routine --prune run exits 0.
func (r Result) ExitCode() int {
	if r.Error != nil {
		return 1
	}
	for _, it := range r.Items {
		if it.Result == ResultSkippedDirty || it.Result == ResultDiverged {
			return 1
		}
	}
	return 0
}
