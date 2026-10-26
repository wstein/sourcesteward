// SPDX-License-Identifier: EUPL-1.2

// Package risk evaluates Spec 044 phase-1 presentation risk from observed facts.
// It performs no observation or selection and grants no effect authorization.
// Callers retain the original verdict/proof and apply guards independently.
package risk

import (
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
)

// Level is a display classification, not integration evidence or permission.
type Level string

const (
	Green  Level = "green"
	Yellow Level = "yellow"
	Orange Level = "orange"
	Red    Level = "red"
	Kept   Level = "kept"
)

// Facts carries existing classifier evidence plus explicit observation facts.
// NoWork means observed no-own-commit evidence, not a zero/default commit count.
// MergedHead means the exact tip matched a PR merged into the selected base;
// it never turns a retained/unmerged row into a choosable one. UpstreamGone is
// local corroboration only. Phase-2 done/adoption and R3 are deliberately absent.
// Strings use the existing classifier kind, verdict, proof and reason spellings
// so adapters can map evidence without a dependency cycle or a schema change.
type Facts struct {
	Kind, Verdict, Proof, Reason     string
	Scope                            owner.Scope
	OwnerSource                      owner.Source
	HasCreationRecord                bool
	Primary, DirectoryMissing        bool
	Dirty, IgnoredExtras, Incomplete bool
	NoWork, MergedHead, UpstreamGone bool
	LastActivity                     time.Time
}

// Assessment contains ordered stable risk-reason codes for a later renderer.
// A structural Kept assessment has an empty, non-nil reason list: the original
// classifier reason explains the retention. No user text is copied into codes.
type Assessment struct {
	Level   Level
	Reasons []string
}

// Evaluate implements the phase-1 ladder, including D6's 72-hour window.
// now is supplied by the run so every row shares one reference time.
func Evaluate(f Facts, now time.Time) Assessment {
	out := Assessment{Level: Kept, Reasons: []string{}}
	// Spec 044 clarification 2 keeps primary checkouts and stale registrations
	// structurally retained, including observed dirt or incomplete evidence
	// (red and kept edges (b)/(c)).
	if f.Primary || f.Kind == "stale-worktree" {
		return corroborate(out, f)
	}
	worktree := f.Kind == "worktree"
	noProof := f.Proof == "none" || f.Proof == ""
	// Content facts outrank the classifier's first structural refusal reason.
	if f.Reason == "unmerged" || (noProof && !f.Primary && !f.DirectoryMissing && f.Kind != "stale-worktree") {
		out.Reasons = append(out.Reasons, ReasonNotIntegrated)
	}
	if f.Dirty || f.Reason == "dirty" {
		out.Reasons = append(out.Reasons, ReasonUncommittedFiles)
	}
	if f.IgnoredExtras || f.Reason == "ignored-extras" {
		out.Reasons = append(out.Reasons, ReasonExtraFiles)
	}
	if f.Incomplete || f.Reason == "evidence-incomplete" {
		out.Reasons = append(out.Reasons, ReasonNotJudged)
	}
	if len(out.Reasons) > 0 {
		out.Level = Red
		return corroborate(out, f)
	}
	// Retained rows keep their unselectable classification even with unknown
	// kind/proof words. Closed vocabulary gates strength only for safe/decision
	// rows; it must never promote retained rows into selectable ones.
	if f.Verdict != "safe" && f.Verdict != "decision" {
		return corroborate(out, f)
	}
	// Validate vocabulary before weak/corroborating flags can lift strength.
	// A hand-mapped typo must never become a green default-deletion row.
	if !knownKind(f.Kind) || !knownProof(f.Proof) {
		out.Level = Red
		out.Reasons = append(out.Reasons, ReasonNotJudged)
		return corroborate(out, f)
	}
	equivalent := false
	switch {
	case f.NoWork:
		out.Level = Green
		out.Reasons = append(out.Reasons, ReasonNoOwnCommit)
	case f.MergedHead:
		out.Level = Green
		out.Reasons = append(out.Reasons, ReasonPRMergedHead)
	case f.Proof == "ancestor":
		out.Level = Green
		out.Reasons = append(out.Reasons, ReasonProofAncestor)
	case f.Proof == "no-net-change":
		out.Level = Green
		out.Reasons = append(out.Reasons, ReasonProofNoNetChange)
	case f.Proof == "github-branch-absent":
		out.Level = Green
		out.Reasons = append(out.Reasons, ReasonProofGitHubBranchAbsent)
	case f.Proof == "patch-id" || f.Proof == "merge-noop" || f.Proof == "empty-working-copy-integrated-parent":
		out.Level = Yellow
		equivalent = true
		out.Reasons = append(out.Reasons, ReasonProofEquivalent)
	default:
		out.Level = Red
		out.Reasons = append(out.Reasons, ReasonNotJudged)
		return corroborate(out, f)
	}
	// Unknown or future activity cannot lower a non-own worktree's risk.
	// Rows younger than 24 hours are normally retained by the classifier; the
	// conservative boundary also covers exact 24h and malformed caller facts.
	if worktree && f.Scope != owner.ScopeOwn && (f.LastActivity.IsZero() || now.Sub(f.LastActivity) <= 72*time.Hour) {
		out.Level = raise(out.Level)
		out.Reasons = append(out.Reasons, ReasonRecentActivity)
	}
	if worktree && equivalent && !f.HasCreationRecord && f.Scope != owner.ScopeOwn && f.Scope != owner.ScopeOther && f.OwnerSource != owner.SourcePathConvention {
		out.Level = raise(out.Level)
		out.Reasons = append(out.Reasons, ReasonNobodyVouches)
	}
	if f.Verdict == "decision" {
		out.Level = Orange
		out.Reasons = append(out.Reasons, ReasonDecisionRow)
	}
	return corroborate(out, f)
}

func raise(level Level) Level {
	if level == Green {
		return Yellow
	}
	return Orange
}
func corroborate(out Assessment, f Facts) Assessment {
	if f.Kind == "branch" && f.UpstreamGone {
		out.Reasons = append(out.Reasons, ReasonUpstreamGone)
	}
	return out
}

func knownKind(kind string) bool {
	return kind == "worktree" || kind == "branch" || kind == "tracking-ref"
}
func knownProof(proof string) bool {
	switch proof {
	case "ancestor", "no-net-change", "github-branch-absent", "patch-id", "merge-noop", "empty-working-copy-integrated-parent":
		return true
	}
	return false
}
