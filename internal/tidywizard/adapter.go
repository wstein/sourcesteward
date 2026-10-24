// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// RepoSpec names one repository of the analysis: its path, the base reference the derivation
// produced and the online selection. Skip is the reason code of a repository that is not
// analysed (for example base-ambiguous); such a repository is never observed.
type RepoSpec struct {
	Name, Path, Base string
	Online           bool
	Remote           string
	Skip             string
}

// Adapter is the Source of the classifier over the read-only observers of internal/workspace and
// the creation record lookup of the state directories. Observe and Records are fields so tests
// can replace them; NewAdapter sets the production functions.
type Adapter struct {
	Repos   map[string]RepoSpec
	Observe func(context.Context, workspace.ObserveOptions) (workspace.WorktreeObservation, error)
	Records func(task string) derive.TaskLookup
	// JJRecords reads the creation records of jj workspaces (no stw lock) once; the adapter calls it
	// at most once per run, the first time a jj repository is analysed, so every workspace of the
	// run is joined against the same snapshot.
	JJRecords func() JJRecords
	jj        *jjSnapshot
}

// JJRecords is the snapshot of the creation records a jj workspace is joined against; see
// derive.DestinationIndex.
type JJRecords interface {
	Lookup(dest, repoDir string) derive.TaskLookup
	Skips() (records []string, stores int)
}

type jjSnapshot struct {
	once sync.Once
	recs JJRecords
}

// jjRecords returns the snapshot of the run. An Adapter built as a struct literal (without
// NewAdapter) has no shared snapshot: it reads the records afresh, and Facts asks once per call.
func (a Adapter) jjRecords() JJRecords {
	if a.jj == nil {
		return a.JJRecords()
	}
	a.jj.once.Do(func() { a.jj.recs = a.JJRecords() })
	return a.jj.recs
}

// NewAdapter returns the adapter for the repositories, looking creation records up in the stores.
func NewAdapter(repos []RepoSpec, stores []derive.Store) Adapter {
	a := Adapter{Repos: map[string]RepoSpec{}, Observe: workspace.ObserveWorktrees, Records: func(task string) derive.TaskLookup { return derive.LookupTask(stores, task) },
		JJRecords: func() JJRecords { return derive.IndexRecords(stores, "jj") }, jj: &jjSnapshot{}}
	for _, r := range repos {
		a.Repos[r.Name] = r
	}
	return a
}

// Facts observes one repository and maps the observation to classifier facts. An error of the
// observation as a whole is returned (the repository is reported as an error); an error of one
// item arrives as its Incomplete fact, which the classifier retains as evidence-incomplete.
func (a Adapter) Facts(ctx context.Context, name string) (RepoFacts, error) {
	spec, ok := a.Repos[name]
	if !ok {
		return RepoFacts{}, fmt.Errorf("unknown repository %q", name)
	}
	if spec.Skip != "" {
		return RepoFacts{Name: name, Skip: spec.Skip}, nil
	}
	obs, err := a.Observe(ctx, workspace.ObserveOptions{Path: spec.Path, Base: spec.Base, Online: spec.Online, Remote: spec.Remote})
	if err != nil {
		return RepoFacts{}, err
	}
	f := RepoFacts{Name: name, Worktrees: []Worktree{}, Branches: []Branch{}, TrackingRefs: []TrackingRef{}}
	var jj JJRecords
	if obs.Backend == "jj" && a.JJRecords != nil {
		jj = a.jjRecords()
	}
	for _, e := range obs.Worktrees {
		w := a.worktree(e)
		if jj != nil && !e.Primary && !e.DirMissing && e.Path != "" && !e.Locked {
			joinJJRecord(jj, &w, obs.RepositoryDir)
		}
		f.Worktrees = append(f.Worktrees, w)
	}
	for _, b := range obs.Branches {
		proof, incomplete := proofFact(b.Proof, b.Incomplete, kindBranch)
		f.Branches = append(f.Branches, Branch{Name: strings.TrimPrefix(b.Name, "refs/heads/"), IsBase: b.IsBase, Proof: proof, Ahead: b.Ahead, AuthorHint: b.AuthorHint, Incomplete: incomplete, Reason: b.Reason})
	}
	for _, r := range obs.TrackingRefs {
		proof, incomplete := proofFact(r.Proof, r.Incomplete, kindTracking)
		reason := r.Reason
		if !trackingReasons[reason] && reason != "" {
			// Not one of the reasons the observers can give: never echoed (it becomes a reason code
			// and evidence text), the reference is retained as incomplete evidence instead.
			reason, incomplete = "", unrecognizedTrackingReason
		}
		f.TrackingRefs = append(f.TrackingRefs, TrackingRef{Name: r.Name, Proof: proof, Incomplete: incomplete, Reason: reason})
	}
	if jj != nil {
		if records, stores := jj.Skips(); len(records)+stores > 0 {
			// A fixed text with counts: the ids of unreadable records are never echoed.
			f.Notices = append(f.Notices, fmt.Sprintf("creation-records-unreadable: %d records, %d stores that could not be listed", len(records), stores))
		}
	}
	if obs.TrackingSkipped != "" {
		f.Notices = append(f.Notices, obs.TrackingSkipped)
	}
	f.Notices = append(f.Notices, obs.Limitations...)
	return f, nil
}

// trackingReasons is the closed list of reasons the observers give for a tracking reference:
// remote-source-present (the source branch still exists) and the protections of the reference
// plan (symbolic-reference, selected-base, workspace-used), plus the proof github-branch-absent
// that a deletable reference carries as its reason. Spec 041 lists them in the reason codes.
var trackingReasons = map[string]bool{
	"remote-source-present": true, "symbolic-reference": true, "selected-base": true,
	"workspace-used": true, "github-branch-absent": true,
}

// unrecognizedTrackingReason is the fixed evidence text of a reference whose reason is not listed.
const unrecognizedTrackingReason = "tracking-reason-unrecognized"

// joinJJRecord joins a jj workspace to its creation record by destination (Spec 041 T011a5). A
// found record owns it (owner and scope only; the classifier still retains it); several differing
// records retain it as incomplete evidence; none leaves it unregistered, as before. Unreadable
// records are collected, never fatal.
func joinJJRecord(jj JJRecords, w *Worktree, repoDir string) {
	l := jj.Lookup(w.Path, repoDir)
	switch {
	case l.Ambiguity != nil:
		if l.Ambiguity.Code == "registration-record-ambiguous" && w.Incomplete == "" {
			w.Incomplete = "registration-record-ambiguous"
		}
	case l.Derived.Source == "task-record" && l.Record.TaskID != "" && validTask(l.Record.TaskID):
		w.JJOwned, w.Task, w.TaskOwner = true, l.Record.TaskID, l.Record.Owner
	}
}

func (a Adapter) worktree(e workspace.WorktreeEvidence) Worktree {
	w := Worktree{
		Path: e.Path, Primary: e.Primary, DirMissing: e.DirMissing, Prunable: e.Prunable,
		Branch: strings.TrimPrefix(e.HeadRef, "refs/heads/"), Dirty: e.Dirty, IgnoredExtras: e.IgnoredExtras,
		Ahead: e.Ahead, AuthorHint: e.AuthorHint, LastActivity: e.LastActivity, HasOwnCommit: e.HasOwnCommit,
	}
	w.Lock, w.Task = lockOf(e)
	if w.Lock == LockStw {
		w.Record, w.TaskOwner = a.record(w.Task, e.Path)
	}
	// The registration facts are always usable; the content facts, and with them the proof, only
	// when the observation finished. A worktree that is missing its directory has no content.
	switch {
	case e.Incomplete != "":
		w.Incomplete = e.Incomplete
	case e.Primary || e.DirMissing:
	default:
		w.Proof, w.Incomplete = proofFact(e.Proof, "", kindWorktree)
	}
	return w
}

// lockOf tells the lock of stw (the exact stw:<task> form) from any other lock.
func lockOf(e workspace.WorktreeEvidence) (Lock, string) {
	if !e.Locked {
		return LockNone, ""
	}
	if task, ok := strings.CutPrefix(e.LockReason, "stw:"); ok && validTask(task) {
		return LockStw, task
	}
	return LockForeign, ""
}

func validTask(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case i > 0 && (c == '.' || c == '_' || c == '-'):
		default:
			return false
		}
	}
	return true
}

// record joins the stw lock with its creation record: a record that names this task and this
// path owns the worktree; a record that does not is a lock mismatch; none and several differing
// ones are missing and ambiguous. Any other failure of the lookup is treated as missing, which
// retains the worktree.
func (a Adapter) record(task, path string) (Record, string) {
	l := a.Records(task)
	if l.Ambiguity != nil {
		if l.Ambiguity.Code == "registration-record-ambiguous" {
			return RecordAmbiguous, ""
		}
		return RecordMissing, ""
	}
	if l.Derived.Source != "task-record" || l.Record.TaskID == "" {
		return RecordMissing, ""
	}
	if l.Record.TaskID != task || l.Record.Destination != path {
		return RecordMismatch, ""
	}
	return RecordFound, l.Record.Owner
}

// proofKind selects the proofs a kind of candidate may carry: a worktree or a branch is proven by
// integration into the base (a jj workspace also by an empty working copy on an integrated parent),
// a remote-tracking reference by the absence of its source branch.
type proofKind int

const (
	kindWorktree proofKind = iota
	kindBranch
	kindTracking
)

// proofFact maps an observer proof word to the classifier proof. A word of another kind, an unknown
// word, or none at all for a finished observation is not a proof: the item is Incomplete, never
// unmerged by guess and never safe by a proof that does not belong to it.
func proofFact(word, incomplete string, kind proofKind) (Proof, string) {
	if incomplete != "" {
		return "", incomplete
	}
	p := Proof(word)
	switch kind {
	case kindWorktree, kindBranch:
		switch p {
		case ProofAncestor, ProofPatchID, ProofMergeNoop, ProofNoNetChange, ProofNone:
			return p, ""
		case ProofEmptyWorkingCopy:
			if kind == kindWorktree {
				return p, ""
			}
		}
	case kindTracking:
		switch p {
		case ProofBranchAbsent, ProofNone:
			return p, ""
		}
	}
	return "", "unknown-proof"
}
