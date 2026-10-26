// SPDX-License-Identifier: EUPL-1.2

package render

import (
	"strings"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
)

// assess evaluates the risk of every candidate and groups the groupable ones. It reads the
// candidates only; the verdict, the proof and every other field of the analysis stay as they are.
func (d *Document) assess(in Input) {
	d.assessed = make(map[string]risk.Assessment, len(in.Result.Candidates))
	rows := make([]risk.Row, 0, len(in.Result.Candidates))
	levels := RiskCounts{}
	for _, c := range in.Result.Candidates {
		a := risk.Evaluate(tidywizard.RiskFacts(c, c.PR), in.Now)
		d.assessed[c.ID] = a
		switch a.Level {
		case risk.Green:
			levels.Green++
		case risk.Yellow:
			levels.Yellow++
		case risk.Orange:
			levels.Orange++
		case risk.Kept:
			levels.Kept++
		default:
			levels.Red++
		}
		rows = append(rows, riskRow(c, a))
	}
	d.groups = risk.GroupRows(rows)
	d.groupOf = map[string]string{}
	for _, g := range d.groups {
		for _, r := range g.Rows {
			d.groupOf[r.ID] = g.Key
		}
	}
	if !in.Now.IsZero() {
		d.Summary.Risk = &levels
	}
}

// riskRow is the presentation row of a candidate for the grouping. A worktree that also deletes
// its branch stays in the group of its directory: the branch is counted by the group's text
// ("also delete their branch"), not by a group of its own.
func riskRow(c tidywizard.Candidate, a risk.Assessment) risk.Row {
	effects := c.Effects
	if c.Kind == tidywizard.KindWorktree {
		effects = nil
		for _, e := range c.Effects {
			if !strings.HasPrefix(e, "delete-branch:") {
				effects = append(effects, e)
			}
		}
	}
	return risk.Row{ID: c.ID, Kind: string(c.Kind), Repo: c.Repo, Path: c.Path, Effects: effects, Risk: a}
}

// withRisk sets the additive fields of a candidate when the document carries them.
func (d *Document) withRisk(c tidywizard.Candidate, expose bool) tidywizard.Candidate {
	if !expose {
		return c
	}
	a := d.assessed[c.ID]
	c.Risk = string(a.Level)
	c.RiskReasons = append([]string(nil), a.Reasons...)
	if len(c.RiskReasons) == 0 {
		c.RiskReasons = nil
	}
	c.Group = d.groupOf[c.ID]
	return c
}
