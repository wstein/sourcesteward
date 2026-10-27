// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/render"
)

// The additive risk members of Spec 044 (risk, risk_reasons, group, pr, summary.risk) validate
// against the closed overview-4 schema too: an overview-4 document built with the clock of the run
// carries them, and one built without it has no risk members (a pull request number the candidate carries is always a member).
func TestTidyOverviewV4SchemaAcceptsTheRiskMembers(t *testing.T) {
	s := schemaAt(t, "v4", "tidy-overview")
	branch := tidywizard.Candidate{ID: "branch:a:old", Repo: "a", Kind: tidywizard.KindBranch, Verdict: tidywizard.VerdictSafe, Owner: "codex", OwnerSource: owner.SourceTaskRecord, Scope: owner.ScopeOwn, Branch: "old", Proof: tidywizard.ProofPatchID, Effects: []string{"delete-branch:old"}, PR: 17}
	dir := tidywizard.Candidate{ID: "worktree:a:/tmp/x", Repo: "a", Kind: tidywizard.KindWorktree, Verdict: tidywizard.VerdictRetain, Reason: "unmerged", Owner: "codex", OwnerSource: owner.SourceTaskRecord, Scope: owner.ScopeOwn, Path: "/tmp/x", Proof: tidywizard.ProofNone, Effects: []string{}}
	for name, now := range map[string]time.Time{"with the clock": time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC), "without the clock": {}} {
		in := render.Input{Workspace: "/fixture", Now: now, Mode: owner.Evidence{Mode: owner.ModeAgent, Caller: "codex", CallerSource: "env"}, Repos: map[string]render.RepoMeta{"a": {Path: "/repo", Backend: "git"}}, Result: tidywizard.Result{Repositories: []tidywizard.RepoResult{{Name: "a", State: "analysed"}}, Candidates: []tidywizard.Candidate{branch, dir}}}
		raw, _, err := render.JSONV4(render.BuildV4(in, nil))
		if err != nil {
			t.Fatal(err)
		}
		valid(t, s, raw)
		has := func(sub string) bool { return strings.Contains(string(raw), sub) }
		if has(`"risk"`) != !now.IsZero() || has(`"group"`) != !now.IsZero() || !has(`"pr": 17`) {
			t.Errorf("%s: risk members present = %v, group %v, pr %v", name, has(`"risk"`), has(`"group"`), has(`"pr": 17`))
		}
	}
}
