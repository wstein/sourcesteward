// SPDX-License-Identifier: EUPL-1.2

package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// Groups are the groups of the document in the order of the first screen, unfolded: one step of
// the janitor wizard each (Spec 044 "Wizard steps"). The slice and its rows are copies.
func Groups(d Document) []OverviewGroup { return overviewGroups(d) }

// AllCandidates is every candidate the analysis held, also those past the cut of Candidates.
func (d Document) AllCandidates() []tidywizard.Candidate {
	return append([]tidywizard.Candidate(nil), d.all...)
}

// CandidateByID finds a candidate of the analysis, or nil.
func (d Document) CandidateByID(id string) *tidywizard.Candidate {
	for i := range d.all {
		if d.all[i].ID == id {
			c := d.all[i]
			return &c
		}
	}
	return nil
}

// RiskReasons are the risk words of a candidate (the codes of --json and of `d N`).
func (d Document) RiskReasons(id string) []string {
	return append([]string(nil), d.assessed[id].Reasons...)
}

// KeptRows is how many rows are kept or red: the rows no group offers for deletion.
func (d Document) KeptRows() int {
	n := 0
	for _, r := range retainRows(d) {
		n += r.Count
	}
	return n
}

// OwnerLabel is the owner as the overview shows it: the derived label, a commit-author hint as
// "hint NAME", and "unknown" for none. It is information, never a selector of its own.
func OwnerLabel(c tidywizard.Candidate) string { return ownerLabel(c) }

// Noun names the rows of a group: "directory" and "directories", "branch" and "branches".
func (g OverviewGroup) Noun() (one, many string) {
	switch g.Kind {
	case tidywizard.KindBranch:
		return "branch", "branches"
	case tidywizard.KindTrackingRef:
		return "tracking reference", "tracking references"
	}
	return "directory", "directories"
}

// PlaceText is the escaped place of the group ("/tmp/whr-*", "in sourcesteward").
func (g OverviewGroup) PlaceText() string { return g.place() }

// StepLines are the lines under the header of a group step: owner, evidence, effect, recovery and
// why. All of it is fixed wording and escaped names; no code appears (Spec 044 "Wording").
func (d Document) StepLines(g OverviewGroup) []string {
	e := untrusted.Esc
	var lines []string
	owners := make([]string, 0, len(g.owners))
	for _, o := range g.owners {
		owners = append(owners, fmt.Sprintf("%s (%d)", e(o.label), o.n))
	}
	lines = append(lines, "  Owner     "+strings.Join(owners, ", "))

	// Evidence: the distinct reasons of the rows with their counts, most frequent first.
	counts := map[string]int{}
	for _, r := range g.Rows {
		for _, w := range d.assessed[r.ID].Reasons {
			counts[w]++
		}
	}
	type reason struct {
		word string
		n    int
	}
	var reasons []reason
	for _, w := range risk.AllReasons() {
		if counts[w] > 0 {
			reasons = append(reasons, reason{w, counts[w]})
		}
	}
	sort.SliceStable(reasons, func(i, j int) bool { return reasons[i].n > reasons[j].n })
	var evidence []string
	for _, r := range reasons {
		if s, ok := RiskReasonSentence(r.word); ok {
			evidence = append(evidence, fmt.Sprintf("%s (%d)", s.Why, r.n))
		}
	}
	if len(evidence) > 3 {
		evidence = append(evidence[:3], fmt.Sprintf("and %d more facts", len(evidence)-3))
	}
	for i, f := range evidence {
		prefix := "  Evidence  "
		if i > 0 {
			prefix = "            "
		}
		lines = append(lines, prefix+f)
	}

	repos := map[string]bool{}
	for _, r := range g.Rows {
		repos[r.Repo] = true
	}
	in := fmt.Sprintf("%d %s", len(repos), plural(len(repos) == 1, "repository", "repositories"))
	n := len(g.Rows)
	one, many := g.Noun()
	switch {
	case g.Kind == tidywizard.KindBranch:
		lines = append(lines, fmt.Sprintf("  Effect    %d %s deleted in %s (restorable)", n, plural(n == 1, one+" is", many+" are"), in))
		lines = append(lines, "  Recover   history stays under refs/stw/recovery/v3/...; undo with stw tidy restore (see stw tidy history)")
	case g.Kind == tidywizard.KindTrackingRef:
		lines = append(lines, fmt.Sprintf("  Effect    %d local %s deleted in %s", n, plural(n == 1, one+" is", many+" are"), in))
		lines = append(lines, "  Recover   only the local reference goes; the remote has nothing of it")
	case g.WithBranch:
		lines = append(lines, fmt.Sprintf("  Effect    %d %s deleted for good in %s, and their branches too (restorable)", n, plural(n == 1, one+" is", many+" are"), in))
		lines = append(lines, "  Recover   committed history stays pinned under refs/stw/recovery/v3/...; deleted files are not restored")
	default:
		lines = append(lines, fmt.Sprintf("  Effect    %d %s deleted for good in %s (their branches stay)", n, plural(n == 1, one+" is", many+" are"), in))
		lines = append(lines, "  Recover   committed history stays pinned under refs/stw/recovery/v3/...; deleted files are not restored")
	}

	why := ""
	if s, ok := LevelSentence(g.Level); ok {
		why = s.Why
	}
	var raises []string
	seen := map[string]bool{}
	for _, r := range g.Rows {
		if r.Risk.Level != g.Level {
			continue
		}
		for _, w := range r.Risk.Reasons {
			switch w {
			case risk.ReasonRecentActivity, risk.ReasonNobodyVouches, risk.ReasonDecisionRow:
				if s, ok := RiskReasonSentence(w); ok && !seen[w] {
					seen[w] = true
					raises = append(raises, s.Why)
				}
			}
		}
	}
	if len(raises) > 0 {
		why += "; " + strings.Join(raises, "; ")
	}
	lines = append(lines, fmt.Sprintf("  Why %s  %s", g.Level, why))
	return lines
}
