// SPDX-License-Identifier: EUPL-1.2
package reviewstate

// The hints (Spec 046), in the order of the documents.
const (
	HintClearNotReady        = "clear-not-ready"
	HintNoFinalReview        = "no-final-review"
	HintBaseNotDefault       = "base-not-default"
	HintParentMergedRetarget = "parent-merged-retarget"
	HintReadyMergeable       = "ready-mergeable"
)

// AllHints lists the hints in the order of the documents.
var AllHints = []string{HintClearNotReady, HintNoFinalReview, HintBaseNotDefault, HintParentMergedRetarget, HintReadyMergeable}

// HintInput is what the hints look at. State is empty and ReviewRead false when the comments were
// not read; Group is the verdict group of Spec 036; DefaultBranch is empty when unknown.
type HintInput struct {
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
	if clear && !in.Draft && in.Group == "ready" && known && !notDefault {
		out = append(out, HintReadyMergeable)
	}
	return out
}
