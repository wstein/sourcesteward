// SPDX-License-Identifier: EUPL-1.2
package reviewstate

// The hints (Spec 046), in the order of the documents.
const (
	HintClearNotReady        = "clear-not-ready"
	HintNoFinalReview        = "no-final-review"
	HintBaseNotDefault       = "base-not-default"
	HintParentMergedRetarget = "parent-merged-retarget"
	// HintTruncatedReviewUnknown: the comment thread was cut, so a verdict may be missing.
	HintTruncatedReviewUnknown = "truncated-review-unknown"
	// HintForeignReviewIgnored: a review-like comment of an author who does not count was ignored.
	HintForeignReviewIgnored = "foreign-review-ignored"
	HintReadyMergeable       = "ready-mergeable"
)

// AllHints lists the hints in the order of the documents.
var AllHints = []string{HintClearNotReady, HintNoFinalReview, HintBaseNotDefault, HintParentMergedRetarget, HintTruncatedReviewUnknown, HintForeignReviewIgnored, HintReadyMergeable}

// HintInput is what the hints look at. State is empty and ReviewRead false when the comments were
// not read; Group is the verdict group of Spec 036; DefaultBranch is empty when unknown; Truncated
// and Ignored come from the Result.
type HintInput struct {
	Truncated     bool
	Ignored       int
	Draft         bool
	ReviewRead    bool
	State         string
	Group         string
	Base          string
	DefaultBranch string
	ParentMerged  bool
}

// Hints returns the hints of one pull request, in document order and without duplicates.
func Hints(in HintInput) []string {
	var out []string
	clear := in.ReviewRead && (in.State == StateClear || in.State == StateClearCarried)
	if clear && in.Draft {
		out = append(out, HintClearNotReady)
	}
	if in.ReviewRead && (in.State == StateNone || in.State == StateClearStale || in.State == StateUnparsed) {
		out = append(out, HintNoFinalReview)
	}
	known := in.DefaultBranch != ""
	notDefault := known && in.Base != in.DefaultBranch
	if notDefault {
		out = append(out, HintBaseNotDefault)
		if in.ParentMerged {
			out = append(out, HintParentMergedRetarget)
		}
	}
	if in.ReviewRead && in.Truncated {
		out = append(out, HintTruncatedReviewUnknown)
	}
	if in.ReviewRead && in.Ignored > 0 {
		out = append(out, HintForeignReviewIgnored)
	}
	if clear && !in.Truncated && !in.Draft && in.Group == "ready" && known && !notDefault {
		out = append(out, HintReadyMergeable)
	}
	return out
}
