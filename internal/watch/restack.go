// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// ErrMalformed marks a read whose answer cannot be used (a malformed response or a name that cannot be
// encoded). The restacker skips the affected PR with a diagnostic instead of failing the cycle.
var ErrMalformed = errors.New("malformed")

const (
	commitPages = 3 // Spec 035: at most 3 pages of 100 per restack candidate
)

// CommitLister reads one page of pulls/N/commits as commit SHAs. more reports that a further page may exist.
type CommitLister interface {
	PullCommits(ctx context.Context, repo string, number, page int) (shas []string, more bool, err error)
}

// GitRestacker implements Restacker (Spec 035, "restack-needed rules").
type GitRestacker struct {
	Commits CommitLister
	// Diag receives one line per skipped PR; nil discards it.
	Diag func(string)
	// BranchOK validates a branch name before it reaches an argv; a failure skips the PR with a
	// diagnostic. watchhost.NewRestacker wires the hosting branch-segment encoder, so a name the watch
	// reader cannot encode (WP-B: malformed) is never emitted. Nil only refuses empty names and names
	// that start with "-".
	BranchOK func(string) error
}

func (g *GitRestacker) diag(format string, args ...any) {
	if g.Diag != nil {
		g.Diag(fmt.Sprintf(format, args...))
	}
}

func defaultBranchOK(name string) error {
	if name == "" || name[0] == '-' {
		return errors.New("empty or option-like name")
	}
	return nil
}

func (g *GitRestacker) branchOK(name string) error {
	if g.BranchOK != nil {
		return g.BranchOK(name)
	}
	return defaultBranchOK(name)
}

// Candidates returns the open children of a merged PR: class 1 (`child-of-merged-branch`) by the base ref
// name alone, class 2 (`contains-merged-commits`) by the old parent SHA in the commit list of an open PR on
// the merge base. A rebase merge rewrites the SHAs, so the old head only survives in the children's own commits.
func (g *GitRestacker) Candidates(ctx context.Context, repo string, due RestackDue, open []OpenPR) ([]RestackCandidate, error) {
	sorted := append([]OpenPR(nil), open...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Number < sorted[j].Number })
	var out []RestackCandidate
	for _, p := range sorted {
		if p.Number == due.MergedPR {
			continue
		}
		var reason string
		switch {
		case due.HeadRef != "" && p.Base == due.HeadRef:
			reason = ReasonChildOfMerged
		case p.Base == due.Base:
			reason = ReasonContains
		default:
			continue
		}
		if err := g.branchOK(p.HeadRef); err != nil {
			g.diag("restack: skipped %s#%d: head branch cannot be named: %v", repo, p.Number, err)
			continue
		}
		if reason == ReasonContains {
			found, err := g.contains(ctx, repo, p.Number, due.OldParent)
			if errors.Is(err, ErrMalformed) {
				g.diag("restack: skipped %s#%d: commit list unusable: %v", repo, p.Number, err)
				continue
			}
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
		}
		c := RestackCandidate{PR: p.Number, Reason: reason, Branch: p.HeadRef}
		if due.MergeCommit != nil && !p.Fork {
			c.Argv = []string{"git", "-c", "rebase.updateRefs=false", "-c", "rerere.enabled=false", "rebase", "--onto", *due.MergeCommit, due.OldParent, p.HeadRef}
		}
		out = append(out, c)
	}
	return out, nil
}

// contains reads at most commitPages pages of the PR's commits and looks for sha.
func (g *GitRestacker) contains(ctx context.Context, repo string, number int, sha string) (bool, error) {
	for page := 1; page <= commitPages; page++ {
		shas, more, err := g.Commits.PullCommits(ctx, repo, number, page)
		if err != nil {
			return false, err
		}
		for _, s := range shas {
			if s == sha {
				return true, nil
			}
		}
		if !more {
			break
		}
	}
	return false, nil
}
