// SPDX-License-Identifier: EUPL-1.2

package stackpublish

// ReviewVerdict is the verdict of an independent review.
type ReviewVerdict string

// Review verdicts. Only a clear verdict counts.
const (
	VerdictClear    ReviewVerdict = "clear"
	VerdictNotClear ReviewVerdict = "not-clear"
)

// ReviewEvidence is a review receipt as read from a trusted source. It is not a ledger this
// package keeps: the caller passes what the source reported.
type ReviewEvidence struct {
	Source         string
	ReviewerID     string
	Verdict        ReviewVerdict
	SubjectSHA     string
	EvidenceDigest string // lower-case SHA-256 of the evidence, a locator's content identity
	Locator        string
}

// CheckRun is one reported check run of a context at a commit.
type CheckRun struct {
	Context    string
	HeadSHA    string
	Conclusion string // "success" is green; anything else is not
}

// ChecksEvidence is the required-check evidence: the union of the required contexts of every
// source, whether all sources were read completely, and the runs reported for the head.
type ChecksEvidence struct {
	Source       string
	ObservedBase string // the full base the evidence was observed against
	Complete     bool
	Required     []string
	Runs         []CheckRun
}

// RebaseEvidence is the repository merge-method fact `rebaseable`, with the head and base it was
// evaluated at. A value evaluated at another head or base is stale.
type RebaseEvidence struct {
	Rebaseable    Tri
	EvaluatedHead string
	EvaluatedBase string
}

// ScopeEvidence is the accepted-scope fact of the PR. UnmetInScope lists acceptance still missing
// inside the accepted scope; OutOfScope lists follow-up work outside it, which never blocks.
type ScopeEvidence struct {
	Source       string
	Accepted     Tri
	UnmetInScope []string
	OutOfScope   []string
}

// ReadyInput is all the evidence of one Ready evaluation. The caller fetches it fresh, for the
// exact current head and base, immediately before the Ready write; a nil pointer is evidence that
// is absent or could not be read.
type ReadyInput struct {
	Policy   WorkspacePolicy
	Head     string // full current PR head
	BaseSHA  string // full current base
	AuthorID string // the author agent of the PR, who cannot review it
	Guards   []Guard
	Review   *ReviewEvidence
	Checks   *ChecksEvidence
	Rebase   *RebaseEvidence
	Scope    *ScopeEvidence
}

// ReadyReceipt is the result of the Ready gate: the exact head and base evaluated, and every
// blocker found (an empty list means Ready).
type ReadyReceipt struct {
	Head     string
	BaseSHA  string
	Blockers []Refusal
}

// EvaluateReady evaluates the Ready gate as a pure function of the evidence passed in. It names
// every blocker, in a fixed order (guards, review, checks, rebaseability, scope); unknown, stale
// or untrusted evidence blocks, and a missing trusted source never becomes a default. A docs-only
// push exception does not exist here: Ready always needs its own review of the exact head.
func EvaluateReady(in ReadyInput) ReadyReceipt {
	r := ReadyReceipt{Head: in.Head, BaseSHA: in.BaseSHA, Blockers: []Refusal{}}
	if !isLowerHex(in.Head, 40) {
		r.Blockers = append(r.Blockers, Refusal{ReasonHeadChanged, "head-invalid"})
	}
	if !isLowerHex(in.BaseSHA, 40) {
		r.Blockers = append(r.Blockers, Refusal{ReasonHeadChanged, "base-invalid"})
	}
	if len(r.Blockers) > 0 {
		return r
	}
	for _, reason := range guardRefusals(KindReady, in.Guards) {
		r.Blockers = append(r.Blockers, Refusal{reason, "guard"})
	}
	if !in.Policy.Loaded {
		r.Blockers = append(r.Blockers, Refusal{ReasonReviewMissing, "policy-missing"}, Refusal{ReasonChecksUnknown, "policy-missing"})
	} else {
		r.Blockers = appendIf(r.Blockers, reviewRefusal(in.Policy, in.Head, in.AuthorID, in.Review, TierMiddle))
		r.Blockers = appendIf(r.Blockers, checksRefusal(in.Policy, in.Head, in.BaseSHA, in.Checks))
	}
	r.Blockers = appendIf(r.Blockers, rebaseRefusal(in.Head, in.BaseSHA, in.Rebase))
	if !in.Policy.Loaded {
		r.Blockers = append(r.Blockers, Refusal{ReasonScopeNotAccepted, "policy-missing"})
	} else {
		r.Blockers = appendIf(r.Blockers, scopeRefusal(in.Policy, in.Scope))
	}
	return r
}

func appendIf(list []Refusal, r *Refusal) []Refusal {
	if r != nil {
		list = append(list, *r)
	}
	return list
}

// guardRefusals lists, in order, the reason of every malformed, failed or unknown guard and of every
// required guard that is absent (or only not-applicable). Unknown relevant evidence never passes.
func guardRefusals(k ActionKind, gs []Guard) []Reason {
	var out []Reason
	passed, reported := map[string]bool{}, map[string]bool{}
	for _, g := range gs {
		switch {
		case validGuard(g, "guard") != nil:
			out = append(out, ReasonPlanStale)
			reported[g.Name] = true
		case g.Result == GuardFail || g.Result == GuardUnknown:
			out = append(out, g.Reason)
			reported[g.Name] = true
		case g.Result == GuardPass:
			passed[g.Name] = true
		}
	}
	for _, req := range requiredGuards(k) {
		if !passed[req.name] && !reported[req.name] {
			out = append(out, req.reason)
		}
	}
	return out
}

// reviewRefusal checks a review receipt against the exact subject and a minimum reviewer tier (empty:
// any allowlisted tier). The review of another SHA, a prediction or an unsigned head is not this one.
func reviewRefusal(p WorkspacePolicy, subject, author string, rv *ReviewEvidence, min ReviewTier) *Refusal {
	refuse := func(detail string) *Refusal { return &Refusal{ReasonReviewMissing, detail} }
	if rv == nil {
		return refuse("review-absent")
	}
	reviewer, ok := p.reviewer(rv.ReviewerID)
	if !ok || !contains(p.ReviewSources, rv.Source) || reviewer.Tier.rank() == 0 {
		return refuse("review-untrusted")
	}
	if reviewer.Tier.rank() < min.rank() {
		return refuse("review-tier-too-low")
	}
	if !isLowerHex(rv.EvidenceDigest, 64) || rv.Locator == "" || len(rv.Locator) > 512 {
		return refuse("review-evidence-invalid")
	}
	// Independence: the author id comes from the trusted policy (task evidence), never from a forge
	// login (all agents may share one account); reviewer and author ids are exact, equal means not
	// independent, and an author the policy does not know cannot be shown independent.
	if !validID(author) || !contains(p.Authors, author) || rv.ReviewerID == author {
		return refuse("review-not-independent")
	}
	if rv.SubjectSHA != subject {
		return refuse("review-wrong-head")
	}
	if rv.Verdict != VerdictClear {
		return refuse("review-not-clear")
	}
	return nil
}

func checksRefusal(p WorkspacePolicy, head, base string, c *ChecksEvidence) *Refusal {
	unknown := func(detail string) *Refusal { return &Refusal{ReasonChecksUnknown, detail} }
	switch {
	case c == nil:
		return unknown("checks-absent")
	case len(c.Required) > DefaultLimits().Records || len(c.Runs) > DefaultLimits().Records:
		return &Refusal{ReasonBoundExceeded, "checks-too-many"}
	case !contains(p.CheckSources, c.Source):
		return unknown("checks-untrusted")
	case !c.Complete:
		return unknown("checks-incomplete")
	case len(c.Required) == 0:
		return unknown("checks-no-required")
	case c.ObservedBase != base:
		// A base edit invalidates old-base evidence: it is never reused (forge-effects.md, retarget).
		return unknown("checks-stale-base")
	}
	required := map[string]bool{}
	for _, ctx := range c.Required {
		if ctx == "" || required[ctx] {
			return unknown("checks-evidence-invalid")
		}
		required[ctx] = true
	}
	type tally struct{ atHead, elsewhere int }
	seen := map[string]*tally{}
	red := false
	for _, run := range c.Runs {
		if run.Context == "" {
			return unknown("checks-evidence-invalid")
		}
		t := seen[run.Context]
		if t == nil {
			t = &tally{}
			seen[run.Context] = t
		}
		switch {
		case run.HeadSHA != head:
			t.elsewhere++
		case run.Conclusion == "success":
			t.atHead++
		default:
			t.atHead++
			if required[run.Context] {
				red = true
			}
		}
	}
	missing, stale := false, false
	for _, ctx := range c.Required {
		t := seen[ctx]
		switch {
		case t == nil:
			missing = true
		case t.atHead == 0:
			stale = true
		}
	}
	switch {
	case missing:
		return unknown("checks-run-missing")
	case stale:
		return unknown("checks-stale-head")
	case red:
		return &Refusal{ReasonChecksNotGreen, "checks-red"}
	}
	return nil
}

func rebaseRefusal(head, base string, rb *RebaseEvidence) *Refusal {
	refuse := func(detail string) *Refusal { return &Refusal{ReasonRebaseableUnknown, detail} }
	switch {
	case rb == nil:
		return refuse("rebaseable-absent")
	case rb.EvaluatedHead != head || rb.EvaluatedBase != base:
		return refuse("rebaseable-stale")
	case rb.Rebaseable == TriNo:
		return refuse("rebaseable-false")
	case rb.Rebaseable != TriYes:
		return refuse("rebaseable-null")
	}
	return nil
}

func scopeRefusal(p WorkspacePolicy, sc *ScopeEvidence) *Refusal {
	refuse := func(detail string) *Refusal { return &Refusal{ReasonScopeNotAccepted, detail} }
	switch {
	case sc == nil:
		return refuse("scope-absent")
	case !contains(p.ScopeSources, sc.Source):
		return refuse("scope-untrusted")
	case sc.Accepted == TriNo:
		return refuse("scope-not-accepted")
	case sc.Accepted != TriYes:
		return refuse("scope-unknown")
	case len(sc.UnmetInScope) > 0:
		return refuse("scope-unmet-acceptance")
	}
	return nil
}

// Ready reports whether no blocker remains.
func (r ReadyReceipt) Ready() bool { return len(r.Blockers) == 0 }

// Status is complete for a Ready receipt and refused otherwise (exit 0 or 1).
func (r ReadyReceipt) Status() Status {
	if r.Ready() {
		return StatusComplete
	}
	return StatusRefused
}

// Guards renders the receipt as the guard the step API consumes: pass, or fail with the first
// blocker's reason.
func (r ReadyReceipt) Guards() []Guard {
	if r.Ready() {
		return []Guard{{Name: GuardReady, Result: GuardPass}}
	}
	return []Guard{{Name: GuardReady, Result: GuardFail, Reason: r.Blockers[0].Reason}}
}
