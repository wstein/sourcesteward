// SPDX-License-Identifier: EUPL-1.2
package pulls

import (
	"context"
	"sort"

	"github.com/wstein/sourcesteward/internal/conflicts"
)

// ReasonParentConflict is the blocked reason of a pull request whose parent in the stack conflicts
// with its base (Spec 047).
const ReasonParentConflict = "parent-conflict"

// ConflictProber probes the pull requests of one repository against their bases (Spec 047). It is
// the local, read-only side of the cycle: *conflictsrun implementations answer from a checkout. A
// pull request without a finding is unknown.
type ConflictProber interface {
	Probe(ctx context.Context, repo string, prs []conflicts.PR) []conflicts.Finding
}

// RepoConflicts is the stack view and the restack plans of one repository (Spec 047).
type RepoConflicts struct {
	Repo   string
	Stacks []conflicts.Stack
	Plans  []conflicts.Plan
}

// applyConflicts probes every row of each repository, attaches the finding, derives the stacks and
// withdraws Ready from a conflicting pull request and from the children above it. It runs after the
// verdicts and before the hints, so ready-mergeable follows the group. It reads no forge data.
func (c *cycle) applyConflicts(ctx context.Context, all []*item) {
	byRepo := map[string][]*item{}
	for _, it := range all {
		byRepo[it.repo] = append(byRepo[it.repo], it)
	}
	repos := make([]string, 0, len(byRepo))
	for r := range byRepo {
		repos = append(repos, r)
	}
	sort.Strings(repos)
	for _, repo := range repos {
		items := byRepo[repo]
		var asked []conflicts.PR
		for _, it := range items {
			if ValidSHA(it.HeadSHA) {
				asked = append(asked, conflicts.PR{Number: it.Number, Base: it.BaseRef, HeadRef: it.HeadRef, Head: it.HeadSHA, Draft: it.Draft, Fork: it.row.Fork})
			}
		}
		got := map[int]conflicts.Finding{}
		if len(asked) > 0 && ctx.Err() == nil {
			for _, f := range c.o.Conflicts.Probe(ctx, repo, asked) {
				got[f.Number] = f
			}
		}
		nodes := make([]conflicts.Node, 0, len(items))
		for _, it := range items {
			f, ok := got[it.Number]
			switch {
			case !ValidSHA(it.HeadSHA):
				f = conflicts.Finding{Number: it.Number, State: conflicts.StateUnknown, Reason: conflicts.ReasonRefInvalid, Base: it.row.Base}
			case !ok:
				f = conflicts.Finding{Number: it.Number, State: conflicts.StateUnknown, Reason: conflicts.ReasonProbeFailed, Base: it.row.Base}
			}
			it.row.BaseConflict = &f
			n := conflicts.Node{Number: it.Number, Repo: repo, Base: it.BaseRef, HeadRef: it.HeadRef, Head: it.HeadSHA, Fork: it.row.Fork, Draft: it.Draft, Finding: f}
			if it.row.Checks != nil {
				n.Checks = *it.row.Checks
			}
			if it.row.Review != nil {
				n.Review = it.row.Review.State
			}
			nodes = append(nodes, n)
		}
		stacks := conflicts.BuildStacks(nodes)
		blocked := map[int]int{}
		for _, s := range stacks {
			for _, l := range s.Levels {
				blocked[l.Number] = l.BlockedBy
			}
		}
		for _, it := range items {
			f := it.row.BaseConflict
			f.BlockedBy = blocked[it.Number]
			if it.row.Group == GroupDraft {
				continue
			}
			switch {
			case f.State == conflicts.StateConflicts:
				it.row.Group, it.row.Reason, it.row.Ready = GroupConflict, "", false
			case f.BlockedBy > 0 && it.row.Group == GroupReady:
				it.row.Group, it.row.Reason, it.row.Ready = GroupBlocked, ReasonParentConflict, false
			}
		}
		rc := RepoConflicts{Repo: repo, Stacks: stacks}
		if c.o.RestackPlan {
			rc.Plans = conflicts.BuildPlans(repo, c.o.Remote, nodes)
		}
		c.res.Conflicts = append(c.res.Conflicts, rc)
	}
}
