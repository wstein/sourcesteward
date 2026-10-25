// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"regexp"
	"strings"
)

// ChangeClass is the review class of a change: docs-only (specification or documentation prose
// only) or code (code, tests, scripts, generated files, or anything not proved docs-only).
type ChangeClass string

// Change classes.
const (
	ClassDocsOnly ChangeClass = "docs-only"
	ClassCode     ChangeClass = "code"
)

// proseRules is the documentation allow list, the same prose rule as scripts/ci_changes.py: root
// markdown, top-level docs markdown and spec prose (spec contracts and examples are read by tests,
// so they are code).
var proseRules = []*regexp.Regexp{
	regexp.MustCompile(`^[^/]+\.md$`),
	regexp.MustCompile(`^docs/[^/]+\.md$`),
	regexp.MustCompile(`^specs/[^/]+/(?:checklists/)?[^/]+\.md$`),
}

// safePath refuses a path that is not a plain, normalized, relative repository path.
func safePath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00") {
		return false
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// ClassifyPaths classifies changed paths. Docs-only needs a known, non-empty set where every
// path is on the prose allow list; anything else (unsafe path, unknown evidence, empty set) is code.
func ClassifyPaths(paths []string, known bool) ChangeClass {
	if !known || len(paths) == 0 {
		return ClassCode
	}
	for _, p := range paths {
		if !safePath(p) {
			return ClassCode
		}
		prose := false
		for _, rule := range proseRules {
			prose = prose || rule.MatchString(p)
		}
		if !prose {
			return ClassCode
		}
	}
	return ClassDocsOnly
}

// Commit is one commit of the publication range with its actual identities and changed paths.
type Commit struct {
	ID         string
	Author     Identity
	Committer  Identity
	Paths      []string
	PathsKnown bool
}

// RangeEvidence is `<observed remote default>..<actual new head>`, oldest first, including every
// still-open parent commit.
type RangeEvidence struct {
	Default  string
	Complete bool
	Commits  []Commit
}

// RangeDiff is the range-diff pairing of the rewrite: "=" per unchanged pair.
type RangeDiff struct {
	Complete bool
	OldCount int
	NewCount int
	Rows     []string
}

// MovedScopeSource is the part of the 015 apply receipt the adapter reads.
type MovedScopeSource struct {
	Spec           ReceiptSpec
	Backend        string
	Repository     string
	Task           string
	ReviewGateHead string
	TargetNewHead  string
	ChangeClass    string // review_gate.change_class
	Complete       bool
}

// ConfigOrigin is one `git config --show-origin` user.* entry.
type ConfigOrigin struct {
	Scope string
	Key   string
	Value string
}

// ConfigEvidence is the bounded origin evidence of user.name and user.email.
type ConfigEvidence struct {
	Complete bool
	Origins  []ConfigOrigin
}

// LocalChecks is the result of the applicable local checks on the actual candidate.
type LocalChecks struct {
	Known bool
	Green bool
}

// PublicationInput is all evidence of one before-push evaluation, fetched in this run.
type PublicationInput struct {
	Policy     WorkspacePolicy
	Backend    string
	Repository string
	Task       string
	Rewrite    bool
	NewHead    string
	AuthorID   string
	Range      *RangeEvidence
	Moved      *MovedScopeSource
	RangeDiff  *RangeDiff
	Checks     *LocalChecks
	Review     *ReviewEvidence
	Config     *ConfigEvidence
	Signing    *SigningEvidence
}

// PublicationReceipt is the before-push verdict.
type PublicationReceipt struct {
	NewHead         string
	MovedScopeClass ChangeClass // empty when no moved scope applies (a new, non-rewritten topic)
	RangeClass      ChangeClass
	Class           ChangeClass // effective: code unless both are proved docs-only
	ClassUnknown    bool        // some evidence of the class was missing or unrecognized, so it counts as code
	PureRebase      bool
	ReviewRequired  bool
	MinTier         ReviewTier
	Blockers        []Refusal
}

// AdaptMovedScope validates the 015 receipt and returns the 016-only moved-scope class
// (moved_scope_change_class := source review_gate.change_class). The contract (producer, command,
// kind and schema version together) must be accepted by the trusted policy; backend, repository and
// task must match; review_gate.head_sha, target.new_head and the candidate must be the same full
// SHA; the moved scope must be complete and its class recognized. Anything else is code, with the
// separate refusal.
func AdaptMovedScope(p WorkspacePolicy, backend, repository, task, newHead string, src *MovedScopeSource) (ChangeClass, *Refusal) {
	refuse := func(detail string) (ChangeClass, *Refusal) {
		return ClassCode, &Refusal{ReasonIdentityMismatch, "moved-scope-" + detail}
	}
	accepted := false
	if src != nil {
		for _, spec := range p.RestackReceipts {
			accepted = accepted || spec == src.Spec
		}
	}
	switch {
	case src == nil:
		return refuse("absent")
	case !accepted:
		return refuse("contract-unaccepted")
	case src.Backend != backend || src.Repository != repository || src.Task != task:
		return refuse("binding-mismatch")
	case !isLowerHex(newHead, 40) || src.ReviewGateHead != newHead || src.TargetNewHead != newHead:
		return refuse("head-mismatch")
	case !src.Complete:
		return refuse("incomplete")
	}
	switch src.ChangeClass {
	case string(ClassDocsOnly):
		return ClassDocsOnly, nil
	case string(ClassCode):
		return ClassCode, nil
	}
	return refuse("class-unrecognized")
}

func rangeRefusal(in PublicationInput) *Refusal {
	refuse := func(reason Reason, detail string) *Refusal { return &Refusal{reason, detail} }
	rg := in.Range
	switch {
	case !isLowerHex(in.NewHead, 40):
		return refuse(ReasonIdentityMismatch, "range-head-mismatch")
	case rg == nil:
		return refuse(ReasonIdentityMismatch, "range-absent")
	case !isLowerHex(rg.Default, 40):
		return refuse(ReasonIdentityMismatch, "default-invalid")
	case !rg.Complete:
		return refuse(ReasonIdentityMismatch, "range-incomplete")
	case len(rg.Commits) == 0:
		return refuse(ReasonIdentityMismatch, "range-empty")
	case len(rg.Commits) > DefaultLimits().IdentityCommits:
		return refuse(ReasonBoundExceeded, "identity-range-too-long")
	}
	for _, c := range rg.Commits {
		if !isLowerHex(c.ID, 40) {
			return refuse(ReasonIdentityMismatch, "range-commit-invalid")
		}
	}
	if rg.Commits[len(rg.Commits)-1].ID != in.NewHead {
		return refuse(ReasonIdentityMismatch, "range-head-mismatch")
	}
	return nil
}

// identityRefusal compares every author and committer of the whole default..new-head range, parents
// included, with exactly the trusted owner identity; a co-author trailer never replaces them.
func identityRefusal(owner Identity, commits []Commit) *Refusal {
	if owner.Name == "" || owner.Email == "" {
		return &Refusal{ReasonIdentityMismatch, "owner-missing"}
	}
	for _, c := range commits {
		if c.Author != owner || c.Committer != owner {
			return &Refusal{ReasonIdentityMismatch, "foreign-identity"}
		}
	}
	return nil
}

// configRefusal checks the bounded `git config --show-origin` user.* evidence: complete, scoped,
// only the pinned keys, and no value other than the owner's.
func configRefusal(owner Identity, cfg *ConfigEvidence) *Refusal {
	if cfg == nil || !cfg.Complete {
		return &Refusal{ReasonIdentityConfigForeign, "config-origin-unknown"}
	}
	for _, o := range cfg.Origins {
		var want string
		switch o.Key {
		case "user.name":
			want = owner.Name
		case "user.email":
			want = owner.Email
		default:
			return &Refusal{ReasonIdentityConfigForeign, "config-key-not-allowed"}
		}
		if o.Scope == "" {
			return &Refusal{ReasonIdentityConfigForeign, "config-origin-unknown"}
		}
		if o.Value != want {
			return &Refusal{ReasonIdentityConfigForeign, "config-foreign"}
		}
	}
	return nil
}

func pureRebase(rd *RangeDiff) bool {
	if rd == nil || !rd.Complete || rd.OldCount < 1 || rd.OldCount != rd.NewCount || len(rd.Rows) != rd.OldCount {
		return false
	}
	for _, row := range rd.Rows {
		if row != "=" {
			return false
		}
	}
	return true
}

// EvaluatePublication evaluates the before-push gates as a pure function of the evidence fetched
// in this run: the whole default..new-head range and its owner-only identities, the recomputed
// publication-range class (union of per-commit paths) combined with the validated moved-scope
// class, the pure-rebase fact, local checks, the review row of the matrix at the exact actual SHA,
// and the signature policy. A missing trusted source blocks; nothing is defaulted.
func EvaluatePublication(in PublicationInput) PublicationReceipt {
	r := PublicationReceipt{NewHead: in.NewHead, Blockers: []Refusal{}}
	if !in.Policy.Loaded {
		r.Blockers = append(r.Blockers, Refusal{ReasonIdentityMismatch, "policy-missing"})
		return r
	}
	if ref := rangeRefusal(in); ref != nil {
		r.Blockers = append(r.Blockers, *ref)
		return r
	}
	idRef := identityRefusal(in.Policy.Owner, in.Range.Commits)
	r.Blockers = appendIf(r.Blockers, idRef)
	if idRef != nil && idRef.Detail == "owner-missing" {
		return r // nothing can be compared without the trusted owner identity
	}
	r.Blockers = appendIf(r.Blockers, configRefusal(in.Policy.Owner, in.Config))

	var union []string
	known := true
	for _, c := range in.Range.Commits {
		union = append(union, c.Paths...)
		known = known && c.PathsKnown
	}
	r.RangeClass = ClassifyPaths(union, known)
	r.ClassUnknown = !known
	r.Class = r.RangeClass
	if in.Rewrite {
		moved, ref := AdaptMovedScope(in.Policy, in.Backend, in.Repository, in.Task, in.NewHead, in.Moved)
		r.MovedScopeClass = moved
		if ref != nil {
			r.Blockers = append(r.Blockers, *ref)
			r.ClassUnknown = true
		}
		if moved != ClassDocsOnly {
			r.Class = ClassCode
		}
	}
	r.PureRebase = in.Rewrite && pureRebase(in.RangeDiff)

	switch {
	case !in.Rewrite:
		r.ReviewRequired, r.MinTier = true, TierStrongest
	case r.Class == ClassCode && r.PureRebase && !r.ClassUnknown:
		r.ReviewRequired, r.MinTier = true, TierMiddle
	case r.Class == ClassCode:
		r.ReviewRequired, r.MinTier = true, TierStrongest
	}

	switch {
	case in.Checks == nil || !in.Checks.Known:
		r.Blockers = append(r.Blockers, Refusal{ReasonChecksUnknown, "local-checks-unknown"})
	case !in.Checks.Green:
		r.Blockers = append(r.Blockers, Refusal{ReasonChecksNotGreen, "local-checks-red"})
	}
	if r.ReviewRequired {
		r.Blockers = appendIf(r.Blockers, reviewRefusal(in.Policy, in.NewHead, in.AuthorID, in.Review, r.MinTier))
	}
	r.Blockers = append(r.Blockers, EvaluateSigning(in.Policy, in.Signing, in.NewHead)...)
	return r
}

// Allowed reports whether no blocker remains.
func (r PublicationReceipt) Allowed() bool { return len(r.Blockers) == 0 }

// Status is complete when allowed and refused otherwise (exit 0 or 1).
func (r PublicationReceipt) Status() Status {
	if r.Allowed() {
		return StatusComplete
	}
	return StatusRefused
}
