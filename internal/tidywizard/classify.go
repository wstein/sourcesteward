// SPDX-License-Identifier: EUPL-1.2

// Package tidywizard classifies the cleanup candidates of the stw tidy wizard (Spec 041): linked
// worktrees, local branches, remote-tracking references and stale worktree registrations. The
// classifier is a pure function over observed facts; the facts come from a Source, which an
// adapter over the read-only evidence of internal/workspace implements (a later task). Nothing here
// reads the file system or changes a repository: a candidate's effects are descriptions that only
// a later apply step may carry out, and no candidate that is not safe or a decision carries one.
package tidywizard

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/fanout"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/untrusted"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// Bounds of the output (Spec 041 JSON section).
const (
	MaxCandidates    = 4096
	MaxEvidenceBytes = 512
	// MaxNotices bounds the notices of one repository.
	MaxNotices = 8
	// RecentWindow is how recent the activity of an other or unknown worktree must be to retain it.
	RecentWindow = 24 * time.Hour
)

// Kind is the candidate kind.
type Kind string

// The candidate kinds.
const (
	KindWorktree      Kind = "worktree"
	KindBranch        Kind = "branch"
	KindTrackingRef   Kind = "tracking-ref"
	KindStaleWorktree Kind = "stale-worktree"
)

// Verdict is the single verdict of a candidate.
type Verdict string

// The verdicts.
const (
	VerdictSafe     Verdict = "safe"
	VerdictRetain   Verdict = "retain"
	VerdictDecision Verdict = "decision"
)

// Proof names the integration proof of the Spec 011 evidence.
type Proof string

// The proofs; ProofNone means not proven integrated.
const (
	ProofAncestor    Proof = "ancestor"
	ProofPatchID     Proof = "patch-id"
	ProofMergeNoop   Proof = "merge-noop"
	ProofNoNetChange Proof = "no-net-change"
	ProofNone        Proof = "none"
	// ProofEmptyWorkingCopy proves a jj workspace: its working copy is empty and its parent is integrated.
	ProofEmptyWorkingCopy Proof = "empty-working-copy-integrated-parent"
	// ProofBranchAbsent proves a remote-tracking reference: the remote no longer has its source branch.
	ProofBranchAbsent Proof = "github-branch-absent"
)

// AllProofs lists every proof the classifier can emit, in the order of the constants above. The
// tidy-overview schema enumerates the same words; a test keeps this list and the constants equal.
var AllProofs = []Proof{ProofAncestor, ProofPatchID, ProofMergeNoop, ProofNoNetChange, ProofEmptyWorkingCopy, ProofBranchAbsent, ProofNone}

// Lock is the lock state of a worktree.
type Lock int

// The lock states: none, the exact stw:<task> form, any other lock.
const (
	LockNone Lock = iota
	LockStw
	LockForeign
)

// Record is the outcome of the creation record lookup of a stw-locked worktree. It is read only
// together with LockStw; for a stw-locked worktree the zero value RecordNone means no record
// was supplied and fails closed like RecordMissing.
type Record int

// The record states. RecordFound is the only one that yields owner_source task-record.
const (
	RecordNone Record = iota
	RecordFound
	RecordMissing
	RecordMismatch
	RecordAmbiguous
)

// Worktree holds the observed facts of one linked worktree.
type Worktree struct {
	Path                 string
	Primary              bool
	DirMissing, Prunable bool
	Branch               string // attached local branch, empty when detached
	// Tip is the full object id of HEAD (the tip an online lookup asks about); empty when unknown.
	Tip       string
	Lock      Lock
	Task      string
	Record    Record
	TaskOwner string // owner label of the found record
	// JJOwned: a jj workspace (it has no stw lock) whose complete creation record was found by its
	// destination and repository; Task and TaskOwner come from it.
	JJOwned              bool
	Dirty, IgnoredExtras bool
	Proof                Proof // integration proof of HEAD
	Ahead                int
	AuthorHint           string
	LastActivity         time.Time // newest of HEAD, index and tracked-file mtime
	HasOwnCommit         bool
	// Incomplete is the reason the observation of this worktree could not finish; the item is
	// then retained as evidence-incomplete whatever the other facts say.
	Incomplete string
}

// Branch holds the facts of one local branch or jj bookmark.
type Branch struct {
	Name   string
	IsBase bool
	// Tip is the full object id of the branch tip; empty when unknown.
	Tip        string
	Proof      Proof
	Ahead      int
	AuthorHint string
	Incomplete string // as Worktree.Incomplete
	// Reason is a native reason to keep the branch whatever its proof says (jj: remote-counterpart,
	// recovery-reference, selected-base, workspace-used); the branch is then retained with it.
	Reason string
	// Upstream is the upstream-gone fact read from the local reference database (Spec 044 T004).
	Upstream workspace.UpstreamState
}

// TrackingRef holds the facts of one local remote-tracking reference.
type TrackingRef struct {
	Name       string
	Proof      Proof
	Incomplete string // as Worktree.Incomplete
	// Reason is why the tracking evidence keeps the reference (remote-source-present, or a
	// protection); empty means no reason was given and an unproven reference is unmerged.
	Reason string
}

// RepoFacts is everything the classifier reads about one repository. Skip is the reason code of a
// repository that is not analysed (for example base-ambiguous).
type RepoFacts struct {
	Name         string
	Skip         string
	Worktrees    []Worktree
	Branches     []Branch
	TrackingRefs []TrackingRef
	// Notices are facts about the whole repository that the overview shows (for example that
	// tracking references were skipped offline); the classifier carries them unchanged.
	Notices []string
	// OnlineFallback is the reason (one of the Reason words) that the implicit online read of this
	// repository failed and it was judged offline; empty otherwise (Spec 044 D13).
	OnlineFallback string
}

// Source supplies the facts of a repository; it is the I/O seam of the classifier.
type Source interface {
	Facts(ctx context.Context, repo string) (RepoFacts, error)
}

// Config holds the run facts: the caller label, whether the run is in agent mode, the working
// directory and the clock.
type Config struct {
	Caller string
	Agent  bool
	CWD    string
	Now    time.Time
}

// Candidate is one row of the overview (the candidates[] entry of the tidy-overview document).
type Candidate struct {
	ID          string       `json:"id"`
	Repo        string       `json:"repo"`
	Kind        Kind         `json:"kind"`
	Verdict     Verdict      `json:"verdict"`
	Reason      string       `json:"reason"`
	Owner       string       `json:"owner"`
	OwnerSource owner.Source `json:"owner_source"`
	Scope       owner.Scope  `json:"scope"`
	Path        string       `json:"path"`
	Branch      string       `json:"branch"`
	Task        string       `json:"task"`
	Proof       Proof        `json:"proof"`
	Effects     []string     `json:"effects"`
	Evidence    string       `json:"evidence"`
	// Upstream is the upstream-gone fact of a branch candidate (Spec 044 T004). It is evidence
	// text for the renderer, never a level and never part of the JSON document.
	Upstream workspace.UpstreamState `json:"-"`
	// Tip is the full object id of the tip of a worktree or branch candidate, the key of the online
	// pull request evidence (Spec 044 T005); never part of the JSON document.
	Tip string `json:"-"`
}

// RepoResult reports one repository: State is analysed, skipped or error.
type RepoResult struct {
	Name    string   `json:"name"`
	State   string   `json:"state"`
	Reason  string   `json:"reason"`
	Notices []string `json:"notices"`
	// OnlineFallback is RepoFacts.OnlineFallback; never part of the JSON document.
	OnlineFallback string `json:"-"`
}

// EscapedNotices is the form of the notices the overview shows: each one passes `esc` (control,
// ANSI, CR, NUL and bidirectional characters become visible escapes) and the 120-character bound
// of Spec 041 FR-042. The bound counts the characters of the notice before escaping, so escapes
// may lengthen what is displayed (up to about six times, in the worst case). The notices carry text of the observation (limitations, tracking skips), so
// they must never reach a terminal unescaped. The stored notices are not changed.
func (r RepoResult) EscapedNotices() []string {
	out := make([]string, 0, len(r.Notices))
	for _, n := range r.Notices {
		out = append(out, untrusted.Esc(n))
	}
	return out
}

// Result is the classification of all repositories, ordered independently of the input order.
type Result struct {
	Repositories []RepoResult
	Candidates   []Candidate
	Truncated    bool
}

// repoWidth is the number of repositories analysed at the same time (Spec 041 T011a4). Only the
// read-only per-repository work runs in parallel; every result goes to the slot of its repository,
// so the output does not depend on the completion order.
const repoWidth = 4

// Classify analyses the repositories through the source. A repository that fails or is skipped is
// reported and never hides the others; the output is sorted and bounded.
func Classify(ctx context.Context, src Source, repos []string, cfg Config) Result {
	names := append([]string{}, repos...)
	sort.Strings(names)
	names = dedupe(names)
	results := make([]RepoResult, len(names))
	found := make([][]Candidate, len(names))
	// A panic or Goexit of the source is re-raised by fanout.Do after the others stopped; a
	// cancelled context leaves the repositories that were never started to the loop below.
	cancelled := fanout.Do(ctx, len(names), repoWidth, func(ctx context.Context, i int) {
		name := names[i]
		ctx = workspace.WithObserveRepo(ctx, name, i+1, len(names))
		workspace.ReportObserve(ctx, workspace.ObserveEvent{Phase: workspace.ObservePhaseStart})
		defer func() {
			workspace.ReportObserve(ctx, workspace.ObserveEvent{Phase: workspace.ObservePhaseDone, Note: results[i].State})
		}()
		results[i] = RepoResult{Name: name, State: "analysed", Notices: []string{}}
		if err := ctx.Err(); err != nil {
			results[i] = RepoResult{Name: name, State: "error", Reason: bound(err.Error(), MaxEvidenceBytes), Notices: []string{}}
			return
		}
		facts, err := src.Facts(ctx, name)
		switch {
		case err != nil:
			results[i] = RepoResult{Name: name, State: "error", Reason: bound(err.Error(), MaxEvidenceBytes), Notices: []string{}}
		case facts.Skip != "":
			results[i] = RepoResult{Name: name, State: "skipped", Reason: facts.Skip, Notices: []string{}}
		default:
			facts.Name = name
			found[i] = classifyRepo(facts, cfg)
			results[i].Notices = boundNotices(facts.Notices)
			results[i].OnlineFallback = facts.OnlineFallback
		}
	})
	if cancelled != nil {
		// Never started because the context ended (no state was ever set): reported, never left empty.
		for i, name := range names {
			if results[i].State == "" {
				results[i] = RepoResult{Name: name, State: "error", Reason: bound(cancelled.Error(), MaxEvidenceBytes), Notices: []string{}}
			}
		}
	}
	res := Result{Repositories: results, Candidates: []Candidate{}}
	for _, c := range found {
		res.Candidates = append(res.Candidates, c...)
	}
	sort.SliceStable(res.Candidates, func(i, j int) bool {
		a, b := res.Candidates[i], res.Candidates[j]
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		return a.ID < b.ID
	})
	if len(res.Candidates) > MaxCandidates {
		res.Candidates, res.Truncated = res.Candidates[:MaxCandidates], true
	}
	return res
}

// boundNotices keeps the first MaxNotices notices, each cut to MaxEvidenceBytes.
func boundNotices(in []string) []string {
	out := []string{}
	for _, n := range in {
		if len(out) == MaxNotices {
			break
		}
		out = append(out, bound(n, MaxEvidenceBytes))
	}
	return out
}

func dedupe(sorted []string) []string {
	out := sorted[:0]
	for i, s := range sorted {
		if i == 0 || s != sorted[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// classifyRepo returns the candidates of one repository sorted by id.
func classifyRepo(f RepoFacts, cfg Config) []Candidate {
	protected := map[string]string{}
	for _, p := range derive.Protected(cfg.CWD, toDerive(f.Worktrees)) {
		protected[p.Path] = p.Reason
	}
	branches := map[string]Branch{}
	for _, b := range f.Branches {
		branches[b.Name] = b
	}
	attached := map[string]int{}
	for _, w := range f.Worktrees {
		if w.Branch != "" && !w.DirMissing {
			attached[w.Branch]++
		}
	}
	out := []Candidate{}
	deleted := map[string]bool{}
	for _, w := range f.Worktrees {
		c := classifyWorktree(f.Name, w, protected[w.Path], cfg)
		if w.Branch != "" && c.Verdict == VerdictSafe && c.Kind == KindWorktree {
			if b, ok := branches[w.Branch]; ok && !b.IsBase && b.Incomplete == "" && b.Reason == "" && proven(b.Proof) && attached[w.Branch] == 1 {
				c.Effects = append(c.Effects, "delete-branch:"+w.Branch)
				deleted[w.Branch] = true
			}
		}
		out = append(out, c)
	}
	for _, b := range f.Branches {
		if b.IsBase || deleted[b.Name] {
			continue
		}
		out = append(out, classifyBranch(f.Name, b, attached[b.Name] > 0, cfg))
	}
	for _, r := range f.TrackingRefs {
		out = append(out, classifyTrackingRef(f.Name, r, cfg))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func toDerive(ws []Worktree) []derive.Worktree {
	out := make([]derive.Worktree, 0, len(ws))
	for _, w := range ws {
		if !w.DirMissing {
			out = append(out, derive.Worktree{Path: w.Path, Primary: w.Primary})
		}
	}
	return out
}

func newCandidate(repo string, kind Kind, subject string) Candidate {
	return Candidate{ID: fmt.Sprintf("%s:%s:%s", kind, repo, subject), Repo: repo, Kind: kind, Proof: ProofNone, Effects: []string{}}
}

func (c Candidate) retain(reason, evidence string) Candidate {
	c.Verdict, c.Reason, c.Evidence = VerdictRetain, reason, bound(evidence, MaxEvidenceBytes)
	return c
}

func (c Candidate) withOwner(o owner.Owner) Candidate {
	c.Owner, c.OwnerSource, c.Scope = o.Label, o.Source, o.Scope
	return c
}

func classifyWorktree(repo string, w Worktree, protectedReason string, cfg Config) Candidate {
	kind := KindWorktree
	if w.DirMissing {
		kind = KindStaleWorktree
	}
	c := newCandidate(repo, kind, w.Path)
	c.Path, c.Branch, c.Task, c.Proof, c.Tip = w.Path, w.Branch, w.Task, normalize(w.Proof), w.Tip
	record := w.Record
	if w.Lock != LockStw {
		record = RecordNone
	} else if record == RecordNone {
		record = RecordMissing
	}
	it := owner.Item{Path: w.Path, Branch: w.Branch, AuthorHint: w.AuthorHint}
	if record == RecordFound || (w.JJOwned && w.Lock == LockNone) {
		it.TaskOwner = w.TaskOwner
	}
	c = c.withOwner(owner.Resolve(it, cfg.Caller, cfg.Agent))

	if w.DirMissing {
		return classifyStale(c, w, record)
	}
	switch {
	case protectedReason != "":
		return c.retain(protectedReason, "")
	case w.Incomplete != "":
		return c.retain("evidence-incomplete", w.Incomplete)
	case w.Lock == LockForeign:
		return c.retain("locked-foreign", "locked by something other than stw")
	case record == RecordMissing:
		return c.retain("registration-record-missing", missingHint)
	case record == RecordAmbiguous:
		return c.retain("registration-record-ambiguous", ambiguousHint)
	case w.Dirty:
		return c.retain("dirty", "uncommitted changes")
	case w.IgnoredExtras:
		return c.retain("ignored-extras", "untracked or ignored files the stored tree does not hold")
	case !proven(c.Proof):
		return c.retain("unmerged", aheadEvidence(w.Ahead, w.Branch))
	case c.Scope != owner.ScopeOwn && w.LastActivity.IsZero():
		return c.retain("evidence-incomplete", "no activity time was observed")
	case c.Scope != owner.ScopeOwn && cfg.Now.Sub(w.LastActivity) < RecentWindow:
		return c.retain("recently-active", "HEAD, index or a tracked file changed within the last 24 hours")
	case w.JJOwned && w.Lock == LockNone:
		// The owner is known from the creation record, but no plan kind retires a jj workspace
		// yet (Spec 041 T011a5): the workspace stays and no deletion path is opened for it.
		return c.retain("jj-retirement-unsupported", "owned by a creation record; no plan kind retires a jj workspace yet")
	case record == RecordMismatch:
		c.Verdict, c.Reason = VerdictDecision, "locked-stw"
		c.Evidence = "stw lock whose creation record does not match"
		c.Effects = []string{"remove-registration", "remove-files"}
		return c
	}
	c.Verdict = VerdictSafe
	c.Effects = []string{"remove-registration", "remove-files"}
	if w.Lock != LockStw {
		c.Reason = "unregistered"
		if !w.HasOwnCommit {
			c.Evidence = "no-work"
		}
	}
	return c
}

func classifyStale(c Candidate, w Worktree, record Record) Candidate {
	switch {
	case w.Lock != LockStw:
		return c.retain("unregistered", "registration of a missing directory without an stw lock")
	case record == RecordMissing:
		return c.retain("registration-record-missing", missingHint)
	case record == RecordAmbiguous:
		return c.retain("registration-record-ambiguous", ambiguousHint)
	case record == RecordMismatch:
		return c.retain("lock-mismatch", "stw lock whose creation record does not match")
	case !w.Prunable:
		return c.retain("evidence-incomplete", "the Git administrative entry is not provably prunable")
	}
	// Provably prunable and stw-owned, but no existing plan kind removes a registration whose
	// directory is missing (Spec 041 T012b): retained until Spec 011 gains a narrow prune kind.
	return c.retain("stale-registration-no-plan-kind", "the registration is prunable; no plan kind removes it yet")
}

func classifyBranch(repo string, b Branch, attached bool, cfg Config) Candidate {
	c := newCandidate(repo, KindBranch, b.Name)
	c.Branch, c.Proof, c.Upstream, c.Tip = b.Name, normalize(b.Proof), b.Upstream, b.Tip
	c = c.withOwner(owner.Resolve(owner.Item{Branch: b.Name, AuthorHint: b.AuthorHint}, cfg.Caller, cfg.Agent))
	switch {
	case b.Incomplete != "":
		return c.retain("evidence-incomplete", b.Incomplete)
	case b.Reason != "":
		return c.retain(b.Reason, b.Reason)
	case !proven(c.Proof):
		return c.retain("unmerged", aheadEvidence(b.Ahead, b.Name))
	case attached:
		return c.retain("branch-attached-elsewhere", "checked out in a worktree that is retained or kept")
	}
	c.Verdict = VerdictSafe
	c.Effects = []string{"delete-branch:" + b.Name}
	return c
}

func classifyTrackingRef(repo string, r TrackingRef, cfg Config) Candidate {
	c := newCandidate(repo, KindTrackingRef, r.Name)
	c.Branch, c.Proof = r.Name, normalize(r.Proof)
	c = c.withOwner(owner.Resolve(owner.Item{Branch: trackingBranch(r.Name)}, cfg.Caller, cfg.Agent))
	if r.Incomplete != "" {
		return c.retain("evidence-incomplete", r.Incomplete)
	}
	if !proven(c.Proof) {
		if r.Reason != "" {
			return c.retain(r.Reason, r.Reason)
		}
		return c.retain("unmerged", "not integrated into the base")
	}
	c.Verdict = VerdictSafe
	c.Effects = []string{"delete-tracking-ref:" + r.Name}
	return c
}

const (
	missingHint   = "no creation record in any known state directory; pass the --state-dir that created it"
	ambiguousHint = "several differing creation records"
)

// proven reports whether a proof is a real integration proof; the zero value is not.
func proven(p Proof) bool { return p != "" && p != ProofNone }

func normalize(p Proof) Proof {
	if !proven(p) {
		return ProofNone
	}
	return p
}

// trackingBranch is the branch part of a remote-tracking reference name, which the owner
// conventions of branches (codex/, claude/) match: refs/remotes/<remote>/<branch>.
func trackingBranch(name string) string {
	if rest, ok := strings.CutPrefix(name, "refs/remotes/"); ok {
		if _, branch, ok := strings.Cut(rest, "/"); ok {
			return branch
		}
	}
	return name
}

func aheadEvidence(n int, name string) string {
	word := "commits"
	if n == 1 {
		word = "commit"
	}
	return fmt.Sprintf("%d %s ahead, not in the base (%s)", n, word, name)
}

// bound cuts s to at most max bytes on a rune boundary and marks the cut with "...".
func bound(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max - len("...")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
