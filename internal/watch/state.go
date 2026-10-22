// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const (
	stateName      = "state.json"
	maxClosedPRs   = 5000
	stateSchemaVer = 1
)

// State is state.json. seq is not stored here: it comes from the journal tail.
type State struct {
	SchemaVersion   int                   `json:"schema_version"`
	AppliedSeq      int64                 `json:"applied_seq"`
	PrintedSeq      int64                 `json:"printed_seq"`
	IntervalSeconds int                   `json:"interval_seconds"`
	Cycle           int                   `json:"cycle"`
	ReposEpoch      int                   `json:"repos_epoch"`
	Repos           []string              `json:"repos"`
	Skipped         []Skipped             `json:"skipped"`
	Repo            map[string]*RepoState `json:"repo"`
	Health          map[string]*Health    `json:"health"` // key "" is the run level
}

// RepoState is everything accepted about one repository.
type RepoState struct {
	Baselined     bool             `json:"baselined"` // false: the next successful read is a silent baseline
	Since         string           `json:"since"`
	LastOK        string           `json:"last_ok"`
	DefaultBranch string           `json:"default_branch"`
	Cond          *Conditional     `json:"conditional"`
	PRs           map[int]*PRState `json:"prs"`
	Closed        []ClosedPR       `json:"closed_prs"` // closed-unmerged generations, oldest first, at most 5000
	RestackDue    []RestackDue     `json:"restack_due"`
	RestackDone   []string         `json:"restack_done"`
}

// Conditional is the placeholder of the ETag, Link and page store of WP-B: the watcher itself keeps it
// (the go-gh cache stays off, Spec 022 FR-004).
type Conditional struct {
	Pages   map[string]PageEntry `json:"pages"`
	Tips    map[string]TipEntry  `json:"tips"`
	Details map[string]string    `json:"details"` // request path -> ETag
}

// PageEntry is one list page: ETag, the Link header and the parsed items, reused on a 304.
type PageEntry struct {
	ETag  string          `json:"etag"`
	Link  string          `json:"link"`
	Items json.RawMessage `json:"items"`
}

// TipEntry is one branch-tip read.
type TipEntry struct {
	ETag string `json:"etag"`
	SHA  string `json:"sha"`
}

// Epochs are the per-PR counters of the transition families; they only grow.
type Epochs struct {
	Head      int `json:"head"`
	Checks    int `json:"checks"`
	Mergeable int `json:"mergeable"`
	Ready     int `json:"ready"`
	Base      int `json:"base"`
}

func (e Epochs) max(o Epochs) Epochs {
	m := func(a, b int) int {
		if b > a {
			return b
		}
		return a
	}
	return Epochs{m(e.Head, o.Head), m(e.Checks, o.Checks), m(e.Mergeable, o.Mergeable), m(e.Ready, o.Ready), m(e.Base, o.Base)}
}

// PRState is the last accepted observation of one open PR plus its debounce candidates.
type PRState struct {
	Number     int      `json:"number"`
	Title      string   `json:"title"`
	Author     string   `json:"author"`
	Labels     []string `json:"labels"`
	Draft      bool     `json:"draft"`
	Head       string   `json:"head"`
	HeadRef    string   `json:"head_ref"`
	Base       string   `json:"base"`
	Fork       bool     `json:"fork"`
	Generation int      `json:"generation"`
	Epochs     Epochs   `json:"epochs"`

	Class        string     `json:"class"` // "" unobserved, none, pending, green, red
	ClassCand    string     `json:"class_candidate"`
	PendingSince string     `json:"pending_since"`
	PendingNames []string   `json:"pending_names"`
	TimeoutFired bool       `json:"timeout_fired"`
	Mergeable    *bool      `json:"mergeable"`
	MState       string     `json:"mergeable_state"`
	Conflicting  bool       `json:"conflicting"`
	Behind       bool       `json:"behind"`
	MergeCand    *MergeCand `json:"mergeable_candidate"`
	DetailDue    bool       `json:"detail_due"`
	Gone         bool       `json:"gone"` // left the open list; pulls/N unreadable, retried every cycle
}

// MergeCand is a mergeability sighting that needs a second consecutive cycle.
type MergeCand struct {
	Mergeable bool   `json:"mergeable"`
	Behind    bool   `json:"behind"`
	State     string `json:"state"`
}

// ClosedPR remembers a closed-unmerged PR so that a reopened one gets the next generation and keeps its counters.
type ClosedPR struct {
	Number     int    `json:"number"`
	Generation int    `json:"generation"`
	Epochs     Epochs `json:"epochs"`
}

// RestackDue is merge work for the restacker: it stays until a poll has emitted its restack events.
type RestackDue struct {
	MergedPR    int     `json:"merged_pr"`
	MergeCommit *string `json:"merge_commit"`
	OldParent   string  `json:"old_parent"`
	Base        string  `json:"base"`
	HeadRef     string  `json:"head_ref"`
}

// Health is the failure and stall bookkeeping of one repository (or, under "", of the run).
type Health struct {
	Failures     int    `json:"failures"`
	Episode      int    `json:"episode"`
	ErrOpen      bool   `json:"error_open"`
	LastCode     string `json:"last_code"`
	Stalled      bool   `json:"stalled"`
	StallEpisode int    `json:"stall_episode"`
	NextTry      string `json:"next_try"` // backoff: not polled before this time (run level: the hold)
}

func newState() *State {
	return &State{SchemaVersion: stateSchemaVer, Repo: map[string]*RepoState{}, Health: map[string]*Health{}}
}

func newRepoState(since string) *RepoState {
	return &RepoState{Since: since, PRs: map[int]*PRState{}}
}

func (s *State) health(repo string) *Health {
	h := s.Health[repo]
	if h == nil {
		h = &Health{}
		s.Health[repo] = h
	}
	return h
}

func (rs *RepoState) closedRecord(n int) (ClosedPR, int, bool) {
	for i, c := range rs.Closed {
		if c.Number == n {
			return c, i, true
		}
	}
	return ClosedPR{}, -1, false
}

func (rs *RepoState) recordClosed(c ClosedPR) {
	if _, i, ok := rs.closedRecord(c.Number); ok {
		rs.Closed = append(rs.Closed[:i], rs.Closed[i+1:]...)
	}
	rs.Closed = append(rs.Closed, c)
	if len(rs.Closed) > maxClosedPRs {
		rs.Closed = rs.Closed[len(rs.Closed)-maxClosedPRs:]
	}
}

// sortedPRs returns the PRs ascending by number.
func (rs *RepoState) sortedPRs() []*PRState {
	out := make([]*PRState, 0, len(rs.PRs))
	for _, p := range rs.PRs {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// loadState reads state.json. A missing file is (nil, nil); an unreadable one is ErrCorruptState.
func loadState(dir string) (*State, error) {
	raw, err := os.ReadFile(filepath.Join(dir, stateName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s := newState()
	if err := json.Unmarshal(raw, s); err != nil || s.SchemaVersion != stateSchemaVer {
		return nil, fmt.Errorf("%w", ErrCorruptState)
	}
	if s.Repo == nil {
		s.Repo = map[string]*RepoState{}
	}
	if s.Health == nil {
		s.Health = map[string]*Health{}
	}
	for _, rs := range s.Repo {
		if rs.PRs == nil {
			rs.PRs = map[int]*PRState{}
		}
	}
	return s, nil
}

func saveState(dir string, s *State) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return writeFileAtomic(dir, stateName, raw)
}
