// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/hosting"
)

// ObserveOptions selects one repository for ObserveWorktrees (Spec 041 FR-052). Base is the
// reference the wizard derived for this repository; it must exist locally and is never fetched.
// Online and Remote are set only by --online: without Online no network is attempted and
// Remote must be empty. The analysis deadline arrives in the context.
type ObserveOptions struct {
	Path, Base string
	Online     bool
	Remote     string
}

// WorktreeEvidence holds the facts of one registered worktree. The registration facts are always
// kept. The content facts are zero when Incomplete names why they could not be observed.
type WorktreeEvidence struct {
	Path, HeadOID, HeadRef                string
	Primary, Locked, Prunable, DirMissing bool
	LockReason                            string
	Dirty, IgnoredExtras                  bool
	LastActivity                          time.Time
	Ahead                                 int
	AuthorHint                            string
	HasOwnCommit                          bool
	Proof                                 string
	Incomplete                            string
}

// BranchEvidence holds the facts of one local branch (a full refs/heads name).
type BranchEvidence struct {
	Name, OID  string
	IsBase     bool
	Proof      string
	Ahead      int
	AuthorHint string
	Incomplete string
}

// TrackingRefEvidence holds the facts of one remote-tracking reference.
type TrackingRefEvidence struct {
	Name string
	// Proof is github-branch-absent or none. Reason is the tracking evidence's reason: the proof
	// itself for an absent source branch, remote-source-present or a protection (workspace-used)
	// for a reference that stays.
	Proof, Reason string
	Incomplete    string
}

// WorktreeObservation is the read-only evidence of one Git repository.
type WorktreeObservation struct {
	Backend, Base, BaseOID string
	Worktrees              []WorktreeEvidence
	Branches               []BranchEvidence
	TrackingRefs           []TrackingRefEvidence
	TrackingSkipped        string
	Limitations            []string
}

// observerWorktreeLimit bounds the worktrees whose content is observed; the surplus keeps its
// registration facts and is Incomplete. A variable only so a test can reach the bound.
var observerWorktreeLimit = observationLimit

// observerBranchLimit bounds the branches whose facts are read; a variable for the same reason.
var observerBranchLimit = observationLimit

const observerAuthorLimit = 128

type worktreeObserver struct {
	refs     tidyV3RefsCoordinator
	tracking tidyV3TrackingCoordinator
	// candidates replaces the read-only tracking evidence of tracking; a test uses it to prove the offline path never calls it.
	candidates func(context.Context, V3PlanOptions, string, tidyV3NativeObservation) (V3Plan, tidyV3TrackingHosting, hosting.TidyBranchAdvertisement, error)
	// read runs the observers' own history reads (commit count, author); a field so a test can
	// make them fail.
	read func(ctx context.Context, bin, dir string, args ...string) (string, error)
	// activity reads the last activity time; a field for the same reason.
	activity func(gitDir, root string, tracked []RetirementStoredFile) (time.Time, error)
}

func defaultWorktreeObserver() worktreeObserver {
	return worktreeObserver{refs: defaultTidyV3RefsCoordinator(), tracking: defaultTidyV3TrackingCoordinator(), read: gitRead, activity: lastActivity}
}

// ObserveWorktrees observes the registrations, worktree contents, branches and (online) tracking
// references of one Git repository without writing anything: no plan record, no store, no fetch,
// and no git status. An error of the repository as a whole is returned; an error of one item sets
// its Incomplete reason and never a guessed value.
func ObserveWorktrees(ctx context.Context, o ObserveOptions) (WorktreeObservation, error) {
	return defaultWorktreeObserver().observe(ctx, o)
}

func validateObserveOptions(o ObserveOptions) error {
	if !v3CanonicalPath(o.Path) || o.Path == "" || o.Base == "" || !validText(o.Base) {
		return failure("invalid-options", "Observation requires a canonical repository path and a base reference.")
	}
	if o.Online != (o.Remote != "") || (o.Remote != "" && !validID(o.Remote)) {
		return failure("invalid-options", "A remote is given exactly when the observation is online.")
	}
	return nil
}

func (c worktreeObserver) observe(ctx context.Context, o ObserveOptions) (WorktreeObservation, error) {
	if err := validateObserveOptions(o); err != nil {
		return WorktreeObservation{}, err
	}
	// The history hard stops (grafts, alternates) end as Incomplete proofs of the items, not as a
	// refusal of the whole repository.
	ctx, _, closeSession, err := withPlanningSession(withHistoryHardStops(ctx))
	if err != nil {
		return WorktreeObservation{}, err
	}
	defer closeSession()
	obsCtx, cancel := context.WithTimeout(ctx, planObservationTimeout)
	defer cancel()
	observed, err := c.refs.observe(obsCtx, o.Path, o.Base)
	if err != nil {
		return WorktreeObservation{}, err
	}
	bin, err := gitExecutable()
	if err != nil {
		return WorktreeObservation{}, err
	}
	root := observed.Expected.Identity.WorkspaceRoot
	out := WorktreeObservation{Backend: "git", Base: o.Base, BaseOID: observed.Expected.BaseCommit, Worktrees: []WorktreeEvidence{}, Branches: []BranchEvidence{}, TrackingRefs: []TrackingRefEvidence{}, Limitations: []string{"Evidence is sampled and nontransactional; nothing was changed and nothing was fetched."}}
	if !o.Online {
		out.TrackingSkipped = "tracking-requires-online"
	}
	facts := newCommitFacts(c.read, bin, root, observed.Expected.BaseCommit)
	// Content phase: registrations, then each worktree with its own content budget.
	for i, reg := range observed.Native.Registrations {
		w := WorktreeEvidence{Path: reg.Path, HeadOID: reg.HeadOID, HeadRef: reg.HeadRef, Locked: reg.Locked, LockReason: reg.LockReason, Prunable: reg.Prunable}
		if i >= observerWorktreeLimit {
			w.Incomplete = "worktree-limit"
		} else {
			c.observeWorktreeContent(obsCtx, bin, facts, observed.Expected.Identity.RepositoryDir, &w)
		}
		out.Worktrees = append(out.Worktrees, w)
	}
	var branches []V3Reference
	for _, r := range observed.Native.References {
		if strings.HasPrefix(r.Name, "refs/heads/") {
			branches = append(branches, r)
		}
	}
	for i, r := range branches {
		b := BranchEvidence{Name: r.Name, OID: r.OID, IsBase: isBaseBranch(r.Name, o.Base)}
		if i >= observerBranchLimit {
			b.Incomplete = "branch-limit"
		} else {
			b.Ahead, b.AuthorHint, b.Incomplete = facts.history(obsCtx, r.OID)
		}
		out.Branches = append(out.Branches, b)
	}
	// Proof phase: one integration run for the repository, every distinct tip once.
	proofCtx, cancelProof := context.WithTimeout(ctx, integrationPhaseTimeout)
	defer cancelProof()
	proofs, closeRun, err := c.proofs(proofCtx, observed)
	if err != nil {
		return WorktreeObservation{}, err
	}
	defer closeRun()
	for i := range out.Worktrees {
		w := &out.Worktrees[i]
		if w.Incomplete != "" || w.DirMissing || w.Primary {
			continue
		}
		if w.Proof, w.Incomplete = proofs(proofCtx, w.HeadOID); w.Incomplete != "" {
			clearContent(w)
		}
	}
	for i := range out.Branches {
		b := &out.Branches[i]
		if b.Incomplete != "" {
			continue
		}
		if b.Proof, b.Incomplete = proofs(proofCtx, b.OID); b.Incomplete != "" {
			b.Ahead, b.AuthorHint = 0, ""
		}
	}
	if o.Online {
		c.observeTracking(ctx, o, observed, &out)
	}
	return out, nil
}

// observeTracking fills the tracking references of the selected remote from the read-only part of
// the contract 3 tracking evidence (the typed branch advertisement, within the online phase of 30 s).
// A failure of that evidence makes every tracking reference of the remote Incomplete.
func (c worktreeObserver) observeTracking(ctx context.Context, o ObserveOptions, observed tidyV3NativeObservation, out *WorktreeObservation) {
	ctx, cancel := context.WithTimeout(ctx, planOnlineTimeout)
	defer cancel()
	root := observed.Expected.Identity.WorkspaceRoot
	candidates := c.candidates
	if candidates == nil {
		candidates = c.tracking.trackingCandidates
	}
	p, session, _, err := candidates(ctx, V3PlanOptions{Path: root, Base: o.Base, Online: true, TrackingRemote: o.Remote}, root, observed)
	if session != nil {
		defer session.Close()
	}
	if err != nil {
		reason := incompleteReason(err)
		for _, r := range observed.Native.References {
			if strings.HasPrefix(r.Name, "refs/remotes/"+o.Remote+"/") && r.Symbolic == "" {
				out.TrackingRefs = append(out.TrackingRefs, TrackingRefEvidence{Name: r.Name, Incomplete: reason})
			}
		}
		return
	}
	for _, cand := range p.Candidates {
		out.TrackingRefs = append(out.TrackingRefs, TrackingRefEvidence{Name: cand.Name, Proof: cand.Proof, Reason: cand.Reason})
	}
}

// clearContent zeroes the content facts and keeps the registration facts.
func clearContent(w *WorktreeEvidence) {
	w.Dirty, w.IgnoredExtras, w.LastActivity, w.Ahead, w.AuthorHint, w.HasOwnCommit, w.Proof = false, false, time.Time{}, 0, "", false, ""
}

// isBaseBranch reports whether a local branch is the base branch: the base reference itself or
// the local branch of the same short name as a remote-tracking base.
func isBaseBranch(name, base string) bool {
	if name == base {
		return true
	}
	short := strings.TrimPrefix(base, "refs/heads/")
	if rest, ok := strings.CutPrefix(base, "refs/remotes/"); ok {
		_, short, _ = strings.Cut(rest, "/")
	} else if short == base {
		return false
	}
	return short != "" && name == "refs/heads/"+short
}

// observeWorktreeContent fills the primary flag and the content facts of one worktree except its
// proof. The directory must still be the registered worktree (same repository, same admin
// directory, same HEAD); otherwise it is Incomplete. The primary checkout is retained as
// primary-checkout whatever its content is, so only its registration facts are kept; it is told
// from a linked worktree by its Git directory being the common directory, which holds for a
// .git directory, a separate Git directory and a submodule checkout alike.
func (c worktreeObserver) observeWorktreeContent(ctx context.Context, bin string, f *commitFacts, common string, w *WorktreeEvidence) {
	if _, err := os.Lstat(w.Path); os.IsNotExist(err) {
		w.DirMissing = true
		return
	}
	fail := func(reason string) {
		clearContent(w)
		w.Incomplete = reason
	}
	id, err := gitNativeIdentity(ctx, w.Path)
	if err != nil {
		fail(incompleteReason(err))
		return
	}
	if id.RepositoryDir != common || id.CommitID != w.HeadOID || id.HeadRef != w.HeadRef || !registeredAt(id, w.Path) {
		fail("registration-mismatch")
		return
	}
	if w.Primary = id.GitDir == id.RepositoryDir; w.Primary {
		return
	}
	policy, err := retirementPolicy("git", 3)
	if err != nil {
		fail(incompleteReason(err))
		return
	}
	// Each worktree gets a fresh content budget; the repository bounds are the deadline and the limit.
	budget := newTidyV3ProofBudget(false)
	if err = budget.beginObservation(); err != nil {
		fail(incompleteReason(err))
		return
	}
	stored, err := gitRetirementStoredFilesWithPolicy(ctx, bin, id, policy, budget)
	dirty, reason := storedOutcome(err)
	if reason != "" {
		fail(reason)
		return
	}
	w.Dirty = dirty
	if err == nil {
		admission := retirementManifestAdmission{Backend: "git", NativeMetadataPaths: []string{".git"}, StoredFiles: stored}
		_, err = observeRetirementV3Manifest(ctx, w.Path, admission, false, policy, budget)
		var extras bool
		if w.Dirty, extras, reason = manifestOutcome(err); reason != "" {
			fail(reason)
			return
		}
		w.IgnoredExtras = extras
	}
	// stored is nil when the index differs from HEAD: then only HEAD and the index count.
	if w.LastActivity, err = c.activity(id.GitDir, w.Path, stored); err != nil {
		fail("activity-unreadable")
		return
	}
	if w.Ahead, w.AuthorHint, reason = f.history(ctx, w.HeadOID); reason != "" {
		fail(reason)
		return
	}
	w.HasOwnCommit = w.Ahead > 0
}

// registeredAt reports whether a linked worktree's administrative directory points back at the
// path (its gitdir file), or the checkout is the primary one.
func registeredAt(id NativeIdentity, path string) bool {
	if id.GitDir == id.RepositoryDir {
		return true
	}
	f, err := os.Open(filepath.Join(id.GitDir, "gitdir"))
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 4097)
	n, _ := io.ReadFull(f, buf)
	return n <= 4096 && strings.TrimSpace(string(buf[:n])) == filepath.Join(path, ".git")
}

// storedOutcome maps the stored-files check of a worktree to a fact. A refusal is never a clean
// worktree: an index that differs from HEAD is Dirty, any other refusal is an Incomplete reason.
func storedOutcome(err error) (dirty bool, reason string) {
	switch {
	case err == nil:
		return false, ""
	case isCode(err, "retirement-index-not-clean"):
		return true, ""
	}
	return false, incompleteReason(err)
}

// manifestOutcome maps the physical manifest check. A tracked file that differs from the stored
// tree or is missing is Dirty; an entry the stored tree does not hold is IgnoredExtras (the
// walk stops at the first finding in path order, so Dirty is then not determined); anything else
// is an Incomplete reason.
func manifestOutcome(err error) (dirty, extras bool, reason string) {
	switch {
	case err == nil:
		return false, false, ""
	case errors.Is(err, errRetirementStoredDiffers), errors.Is(err, errRetirementStoredMissing):
		return true, false, ""
	case isCode(err, "retirement-unrecorded-entry"):
		return false, true, ""
	}
	return false, false, incompleteReason(err)
}

// lastActivity is the newest modification time of the worktree's HEAD, its index and every
// tracked file (at most the stored-file bound of one retirement manifest).
func lastActivity(gitDir, root string, tracked []RetirementStoredFile) (time.Time, error) {
	var newest time.Time
	paths := []string{filepath.Join(gitDir, "HEAD"), filepath.Join(gitDir, "index")}
	for _, f := range tracked {
		paths = append(paths, filepath.Join(root, filepath.FromSlash(f.Path)))
	}
	for i, p := range paths {
		st, err := os.Lstat(p)
		if os.IsNotExist(err) && i >= 2 {
			continue // a deleted tracked file has no time
		}
		if err != nil {
			return time.Time{}, err
		}
		if st.ModTime().After(newest) {
			newest = st.ModTime()
		}
	}
	return newest, nil
}

func isCode(err error, code string) bool {
	var op operationError
	return errors.As(err, &op) && op.Code == code
}

// incompleteReason is the short reason of an item whose observation failed: the failure code when
// it has one, a fixed word otherwise. It never carries a path or a message of the repository.
func incompleteReason(err error) string {
	var op operationError
	switch {
	case errors.As(err, &op):
		return op.Code
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline-exceeded"
	case errors.Is(err, errIntegrationBudget), errors.Is(err, errTidyV3ProofLimit):
		return "proof-budget-exhausted"
	case errors.Is(err, errIntegrationLimit):
		return "proof-limit"
	}
	return "observation-failed"
}

// commitFacts reads the history facts of commits (commits ahead of the base, tip author) once
// per distinct OID.
type commitFacts struct {
	run             func(context.Context, string, string, ...string) (string, error)
	bin, root, base string
	ahead           map[string]int
	author          map[string]string
	failed          map[string]string
}

func newCommitFacts(read func(context.Context, string, string, ...string) (string, error), bin, root, base string) *commitFacts {
	return &commitFacts{run: read, bin: bin, root: root, base: base, ahead: map[string]int{}, author: map[string]string{}, failed: map[string]string{}}
}

func (f *commitFacts) history(ctx context.Context, oid string) (int, string, string) {
	if reason, ok := f.failed[oid]; ok {
		return 0, "", reason
	}
	if n, ok := f.ahead[oid]; ok {
		return n, f.author[oid], ""
	}
	n, author, err := f.read(ctx, oid)
	if err != nil {
		f.failed[oid] = incompleteReason(err)
		return 0, "", f.failed[oid]
	}
	f.ahead[oid], f.author[oid] = n, author
	return n, author, ""
}

func (f *commitFacts) read(ctx context.Context, oid string) (int, string, error) {
	out, err := f.run(ctx, f.bin, f.root, "rev-list", "--count", oid, "--not", f.base)
	if err != nil {
		return 0, "", err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || n < 0 {
		return 0, "", failure("git-query-refused", "commit count is unavailable")
	}
	name, err := f.run(ctx, f.bin, f.root, "log", "-1", "--format=%an", oid)
	if err != nil {
		return 0, "", err
	}
	return n, authorHint(name), nil
}

// authorHint bounds and cleans an untrusted author name: valid UTF-8, no control characters, at
// most observerAuthorLimit bytes.
func authorHint(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f || r == utf8.RuneError {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if len(s) > observerAuthorLimit {
		cut := observerAuthorLimit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	return s
}

// proofs returns a function proving distinct tips once under one integration run: the ancestry
// proof first, then the forge-free classes, with the per-tip and phase budgets of Spec 029
// FR-008. A proof that cannot finish is an Incomplete reason, never "none".
func (c worktreeObserver) proofs(ctx context.Context, observed tidyV3NativeObservation) (func(context.Context, string) (string, string), func(), error) {
	policy, err := v3ProofPolicyFromOptions(V3PlanOptions{})
	if err != nil {
		return nil, func() {}, err
	}
	run, closeRun, err := c.refs.integration.begin(ctx, observed.Expected.Identity, observed.ConfigNotes)
	if err != nil {
		return nil, func() {}, err
	}
	local := c.refs.localProofs(run, observed.Expected, policy)
	return func(ctx context.Context, oid string) (string, string) {
		return proofOutcome(local.prove(ctx, oid, []string{}))
	}, closeRun, nil
}

// incompleteProofReasons are the reasons that mean the proof could not be judged: the proof limit,
// the missing private object directory, an ambiguous merge base, the exhausted proof phase and the
// history hard stops. They end as an Incomplete reason, never as the proof "none".
var incompleteProofReasons = []string{"proof-limit", "object-dir-unavailable", "ambiguous-merge-base", "proof-budget-exhausted", "history-override", "unsupported-metadata"}

// proofOutcome maps the integration proof of one tip to the proof tag or an Incomplete reason.
func proofOutcome(p V3CommitProof, err error) (proof, reason string) {
	if err != nil {
		return "", incompleteReason(err)
	}
	if p.Decision == "delete" {
		if p.Proof == "native-ancestry" {
			return integrationAncestor, ""
		}
		return p.Proof, ""
	}
	reasons := []string{p.Reason}
	if p.Evidence != nil && p.Evidence.Integration != nil {
		reasons = append(reasons, p.Evidence.Integration.Reasons...)
	}
	for _, r := range reasons {
		if slices.Contains(incompleteProofReasons, r) {
			return "", r
		}
	}
	return "none", ""
}
