// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"context"
	"fmt"
	"strings"

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
}

// NewAdapter returns the adapter for the repositories, looking creation records up in the stores.
func NewAdapter(repos []RepoSpec, stores []derive.Store) Adapter {
	a := Adapter{Repos: map[string]RepoSpec{}, Observe: workspace.ObserveWorktrees, Records: func(task string) derive.TaskLookup { return derive.LookupTask(stores, task) }}
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
	for _, w := range obs.Worktrees {
		f.Worktrees = append(f.Worktrees, a.worktree(w))
	}
	for _, b := range obs.Branches {
		proof, incomplete := proofFact(b.Proof, b.Incomplete, kindBranch)
		f.Branches = append(f.Branches, Branch{Name: strings.TrimPrefix(b.Name, "refs/heads/"), IsBase: b.IsBase, Proof: proof, Ahead: b.Ahead, AuthorHint: b.AuthorHint, Incomplete: incomplete})
	}
	for _, r := range obs.TrackingRefs {
		proof, incomplete := proofFact(r.Proof, r.Incomplete, kindTracking)
		f.TrackingRefs = append(f.TrackingRefs, TrackingRef{Name: r.Name, Proof: proof, Incomplete: incomplete, Reason: r.Reason})
	}
	if obs.TrackingSkipped != "" {
		f.Notices = append(f.Notices, obs.TrackingSkipped)
	}
	f.Notices = append(f.Notices, obs.Limitations...)
	return f, nil
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
		w.Proof, w.Incomplete = proofFact(e.Proof, "", kindBranch)
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
// integration into the base, a remote-tracking reference by the absence of its source branch.
type proofKind int

const (
	kindBranch proofKind = iota // also the proofs of a worktree
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
	case kindBranch:
		switch p {
		case ProofAncestor, ProofPatchID, ProofMergeNoop, ProofNoNetChange, ProofNone:
			return p, ""
		}
	case kindTracking:
		switch p {
		case ProofBranchAbsent, ProofNone:
			return p, ""
		}
	}
	return "", "unknown-proof"
}
