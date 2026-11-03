// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"strings"
)

// V3Explanation says, for one retained candidate, why it stays, which proof is missing and the
// command that would provide it (Spec 053, issue 388). It is derived from the plan and not part of
// the plan identity; `stw tidy plan --explain` adds it to the result.
type V3Explanation struct {
	Name        string `json:"name"`
	Reason      string `json:"reason"`
	Text        string `json:"text"`
	NextCommand string `json:"next_command"`
}

// ExplainContext is what an explanation needs besides the candidate.
type ExplainContext struct {
	Source, Base string
	// Remote is the remote of an online plan; empty means origin.
	Remote string
	// PRProof is the proof_policy.pr_proof of the plan (empty for a plan without one).
	PRProof string
	// Contract3 is true when the plan is a contract 3 plan, which already tried the forge-free proofs.
	Contract3 bool
}

// ExplainedReasons lists every reason a retained candidate can carry. Each has an explanation text
// and a next command; any other reason gets the generic explanation. A test keeps the list and the
// explanations equal.
var ExplainedReasons = []string{
	"integration-not-proven", "needs-review", "pr-head-mismatch", "pr-target-mismatch", "hosting-proof-unsupported",
	"pr-ambiguous", "history-override", "unsupported-metadata", "selected-base", "symbolic-reference", "workspace-used",
	"remote-source-present",
}

// ExplainV3 returns one explanation per retained candidate of a contract 3 plan, in plan order.
func ExplainV3(p V3Plan) []V3Explanation {
	ctx := ExplainContext{Source: p.SourcePath, Base: p.Expected.BaseSelector, Contract3: true}
	if p.ProofPolicy != nil {
		ctx.PRProof = p.ProofPolicy.PRProof
	}
	if p.Online != nil {
		ctx.Remote = p.Online.Remote
	}
	out := []V3Explanation{}
	for _, c := range p.Candidates {
		if c.Decision != "retain" {
			continue
		}
		var reasons []string
		if c.Evidence != nil && c.Evidence.Integration != nil {
			reasons = c.Evidence.Integration.Reasons
		}
		out = append(out, ExplainRetention(ctx, c.Name, c.OID, c.Reason, c.WorkspacePaths, reasons))
	}
	return out
}

// ExplainTidyPlan returns one explanation per retained candidate of a default (contract 1/2) plan.
func ExplainTidyPlan(p TidyPlanRecord) []V3Explanation {
	ctx := ExplainContext{Source: p.SourcePath, Base: p.Expected.BaseSelector}
	out := []V3Explanation{}
	for _, c := range p.Candidates {
		if c.Decision == "retain" {
			out = append(out, ExplainRetention(ctx, c.Name, c.CommitID, c.Reason, c.WorkspacePaths, nil))
		}
	}
	return out
}

// ExplainRetention explains one retained candidate. reviewReasons are the integration reasons of a
// needs-review candidate.
func ExplainRetention(ctx ExplainContext, name, oid, reason string, paths, reviewReasons []string) V3Explanation {
	src, base := shellWord(ctx.Source), shellWord(ctx.Base)
	ref := shellWord(name)
	plan := "stw tidy plan " + src + " --contract 3 --base " + base + " --ref " + ref
	remote := ctx.Remote
	if remote == "" {
		remote = "origin"
	}
	e := V3Explanation{Name: name, Reason: reason}
	switch reason {
	case "integration-not-proven":
		switch {
		case !ctx.Contract3:
			e.Text = "It stays because it is not an ancestor of the base, and this plan proves integration by ancestry only; patch-id and merged pull request proofs are missing."
			e.NextCommand = plan + " --online"
		case ctx.PRProof == v3PRProofUnavailable:
			e.Text = "It stays because no ancestry, patch-id or merge proof holds, and the merged pull request proof was not tried because the plan is offline."
			e.NextCommand = plan + " --online"
		default:
			e.Text = "It stays because no ancestry, patch-id, merge or merged pull request head proof shows that all of its commits are in the base."
			e.NextCommand = "git -C " + src + " log --oneline " + base + ".." + ref
		}
	case "needs-review":
		e.Text = "It stays because a proof matched only in part" + reasonsSuffix(reviewReasons) + "; a reviewer decision or a narrower proof is missing."
		e.NextCommand = plan + " --report-dir ./tidy-review"
	case "pr-head-mismatch":
		e.Text = "It stays because the merged pull request of this branch has another head, so the tip changed after the merge."
		e.NextCommand = "git -C " + src + " log --oneline " + base + ".." + ref
	case "pr-target-mismatch":
		e.Text = "It stays because the merged pull request result is not in the selected base."
		e.NextCommand = plan + " --online"
	case "hosting-proof-unsupported":
		e.Text = "It stays because the merged pull request proof is unsupported for this remote (not one canonical GitHub URL, or a fork pull request)."
		e.NextCommand = "git -C " + src + " remote -v"
	case "pr-ambiguous":
		e.Text = "It stays because several merged pull requests match its head and none is selected."
		e.NextCommand = plan + " --online --pr " + shellWord(oid+"=NUMBER")
	case "history-override":
		e.Text = "It stays because grafts or a shallow history make every ancestry proof untrustworthy."
		e.NextCommand = "git -C " + src + " rev-parse --is-shallow-repository"
	case "unsupported-metadata":
		e.Text = "It stays because the repository uses alternate object directories, which no proof accepts."
		e.NextCommand = "git -C " + src + " rev-parse --git-path objects/info/alternates"
	case "selected-base":
		e.Text = "It stays because it is the selected base or stands at the base commit; there is nothing to prove."
		e.NextCommand = "git -C " + src + " for-each-ref --format='%(refname)' refs/remotes"
	case "symbolic-reference":
		e.Text = "It stays because it is a symbolic reference or the target of one, not a branch of its own."
		e.NextCommand = "git -C " + src + " for-each-ref --format='%(refname) %(symref)' refs/heads"
	case "workspace-used":
		e.Text = "It stays because a worktree with an existing directory uses it" + pathSuffix(paths) + "."
		e.NextCommand = "git -C " + src + " worktree list"
	case "remote-source-present":
		e.Text = "It stays because the branch still exists on the remote."
		e.NextCommand = "git -C " + src + " ls-remote --heads " + shellWord(remote)
	default:
		e.Text = "It stays for the reason " + shellWord(reason) + "; no recorded proof allows its deletion."
		e.NextCommand = plan
	}
	return e
}

func reasonsSuffix(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	return " (" + strings.Join(reasons, ", ") + ")"
}

func pathSuffix(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return " at " + shellWord(paths[0])
}

// shellWord quotes s for a POSIX shell unless it is made of plain characters.
func shellWord(s string) string {
	plain := s != ""
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-=:+@%", r)) {
			plain = false
			break
		}
	}
	if plain {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
