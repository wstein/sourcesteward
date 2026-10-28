// SPDX-License-Identifier: EUPL-1.2

package render

import (
	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// Sentence is the human wording of one internal code (Spec 044): Why says what the code means and
// Action says what the reader can do. Neither repeats the code, which stays in --json, --verbose,
// --rows and the row details. Both are fixed text of this program, never text from outside, and
// carry no period, so the renderer can join them into a line.
type Sentence struct{ Why, Action string }

// reasonSentences covers every code of tidywizard.AllReasons (a test fails for a missing one).
var reasonSentences = map[string]Sentence{
	"unmerged":                        {"not merged: it has commits the base lacks", "nothing is deleted; review: stw tidy plan REPO --contract 3 --base BASE --report-dir DIR"},
	"dirty":                           {"uncommitted changes", "commit or discard them yourself"},
	"ignored-extras":                  {"untracked or ignored files", "look inside; remove them yourself if junk"},
	"evidence-incomplete":             {"not judged (for example a deadline or Git refused)", "assume unique work; after a deadline run again with --timeout 15m"},
	"registration-record-missing":     {"created by stw, record not found", "re-run with the --state-dir that created it"},
	"registration-record-ambiguous":   {"created by stw, several differing records", "find the right --state-dir; nothing is deleted"},
	"locked-foreign":                  {"locked by something other than stw", "unlock it yourself if you know why it is locked"},
	"locked-stw":                      {"stw lock whose record does not match", "decide by hand; it needs its own confirmation"},
	"lock-mismatch":                   {"stw lock whose record does not match", "no stw action yet"},
	"unregistered":                    {"no stw creation record", "check the owner shown with it"},
	"primary-checkout":                {"main checkout of a repository", "never offered"},
	"current-worktree":                {"the worktree you are in", "never offered; run from another directory"},
	"recently-active":                 {"changed within the last 24 hours", "wait, then run again"},
	"branch-attached-elsewhere":       {"branch is checked out in a kept worktree", "decide on that worktree first, then run again"},
	"jj-retirement-unsupported":       {"a jj workspace; stw cannot retire it yet", "no stw action yet"},
	"stale-registration-no-plan-kind": {"worktree entry whose directory is gone", "no stw action yet"},
	"retirement-parent-foreign":       {"its directory sits in a place that is not yours, for example /tmp", "stw cannot remove it; remove it yourself with git worktree remove"},
	"legacy-owner-unknown":            {"no stw record and no owner label, only a commit author", "stw cannot remove it; remove it yourself if you know it is finished"},
	"unknown-workspace-path":          {"a worktree entry of this repository is locked or its directory is gone", "see the notice of the repository: git worktree unlock, then git worktree prune"},
	"state-overlap":                   {"the state directory overlaps a worktree of this repository", "give another --state-dir"},
	"remote-source-present":           {"the branch still exists on the remote", "keep it; it is not gone upstream"},
	"symbolic-reference":              {"a symbolic reference", "never offered"},
	"selected-base":                   {"the base branch", "never offered"},
	"workspace-used":                  {"a workspace uses it", "never offered"},
	"remote-counterpart":              {"the remote has a counterpart", "keep it until the remote is gone"},
	"recovery-reference":              {"a recovery reference", "never offered"},
}

// proofSentences covers every tidywizard.AllProofs word. The action is what a proof allows.
var proofSentences = map[string]Sentence{
	"ancestor":                             {"already on the base branch", "nothing is lost by deleting it"},
	"patch-id":                             {"on the base as equal patches (a rebase merge)", "compare with the base if unsure"},
	"merge-noop":                           {"merging it into the base changes nothing", "compare with the base if unsure"},
	"no-net-change":                        {"its changes cancel out against the base", "nothing is lost by deleting it"},
	"empty-working-copy-integrated-parent": {"empty working copy whose parent is on the base", "compare with the base if unsure"},
	"github-branch-absent":                 {"the remote no longer has the branch", "the local reference can go"},
	"none":                                 {"not on the base", "never offered"},
}

// riskReasonSentences covers every risk.AllReasons word (the "why green, yellow, orange or red").
var riskReasonSentences = map[string]Sentence{
	risk.ReasonProofAncestor:           {"all of its work is already on the base branch", "nothing is lost by deleting it"},
	risk.ReasonProofNoNetChange:        {"its changes cancel out against the base branch", "nothing is lost by deleting it"},
	risk.ReasonProofGitHubBranchAbsent: {"the remote no longer has the branch", "the local reference can go"},
	risk.ReasonNoOwnCommit:             {"it has no commit of its own", "nothing is lost by deleting it"},
	risk.ReasonPRMergedHead:            {"its tip was merged as a pull request", "nothing is lost by deleting it"},
	risk.ReasonProofEquivalent:         {"the work is on the base as equal patches (a rebase merge), not as the same commits", "compare with the base if unsure"},
	risk.ReasonRecentActivity:          {"it changed in the last 3 days or its last change is unknown", "make sure nobody is still working there"},
	risk.ReasonNobodyVouches:           {"no record says whose it is", "make sure it is not somebody's current work"},
	risk.ReasonDecisionRow:             {"it needs a decision of its own", "choose it by its number and confirm it"},
	risk.ReasonUpstreamGone:            {"the upstream branch is gone", "a closed or deleted pull request looks the same; it proves nothing"},
	risk.ReasonNotIntegrated:           {"its commits are not on the base branch", "nothing is deleted; unique work may exist"},
	risk.ReasonUncommittedFiles:        {"it has uncommitted changes", "commit or discard them yourself"},
	risk.ReasonExtraFiles:              {"it holds untracked or ignored files", "look inside; remove them yourself if junk"},
	risk.ReasonNotJudged:               {"it could not be judged", "run again with a larger window; until then assume unique work"},
}

// levelSentences covers the five risk levels.
var levelSentences = map[risk.Level]Sentence{
	risk.Green:  {"provably on the base branch", "offered for deletion"},
	risk.Yellow: {"on the base as equal patches, not as the same commits", "offered for deletion; compare with the base if unsure"},
	risk.Orange: {"on the base as equal patches and one more fact raises the risk", "offered for deletion; check the extra fact first"},
	risk.Red:    {"may hold unique work", "never offered for deletion"},
	risk.Kept:   {"kept for a structural reason", "never offered for deletion"},
}

// ReasonSentence is the wording of a classifier reason code.
func ReasonSentence(code string) (Sentence, bool) { s, ok := reasonSentences[code]; return s, ok }

// ProofSentence is the wording of an integration proof word.
func ProofSentence(code string) (Sentence, bool) { s, ok := proofSentences[code]; return s, ok }

// RiskReasonSentence is the wording of a risk reason word.
func RiskReasonSentence(code string) (Sentence, bool) {
	s, ok := riskReasonSentences[code]
	return s, ok
}

// LevelSentence is the wording of a risk level.
func LevelSentence(level risk.Level) (Sentence, bool) { s, ok := levelSentences[level]; return s, ok }

// reasonSentenceOrFallback never drops a reason code this version does not know (a native reason
// of a newer observer): it names the escaped code and points at the places that keep the details.
func reasonSentenceOrFallback(code string) Sentence {
	if s, ok := ReasonSentence(code); ok {
		return s
	}
	return Sentence{Why: "kept for a reason this version does not describe (" + untrusted.Esc(code) + ")", Action: "see stw tidy --rows or --json"}
}
