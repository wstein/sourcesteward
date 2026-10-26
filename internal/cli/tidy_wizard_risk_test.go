// SPDX-License-Identifier: EUPL-1.2
package cli

import "testing"

// The JSON overview carries the additive risk fields of Spec 044 and the counts per level; the
// existing fields are the ones of the analysis, whatever the level.
func TestWizardPlanJSONCarriesTheRiskFields(t *testing.T) {
	w := newWizardWorld(t, false, false)
	out, err := runTidy(t, "", "plan", "--json")
	if err != nil {
		t.Fatal(err)
	}
	d := docOf(t, out)
	got := map[string]string{}
	for _, c := range d.Candidates {
		got[c.ID] = c.Risk
	}
	want := map[string]string{
		"worktree:a:own1":                      "green",
		"branch:a:claude/old":                  "green",
		"worktree:a:whr1":                      "yellow", // another owner, activity time unknown: one step up
		"worktree:b:dec":                       "red",    // a decision row with no proof
		"worktree:b:dirty":                     "red",
		"tracking-ref:b:refs/remotes/origin/x": "green",
	}
	for id, level := range want {
		if got[id] != level {
			t.Errorf("%s: risk %q, want %q", id, got[id], level)
		}
	}
	if d.Summary.Risk == nil || d.Summary.Risk.Green != 3 || d.Summary.Risk.Yellow != 1 || d.Summary.Risk.Red != 2 {
		t.Errorf("summary risk %+v", d.Summary.Risk)
	}
	// The verdicts and the summary of the analysis are untouched.
	if d.Summary.Safe != 4 || d.Summary.Retain != 1 || d.Summary.Decision != 1 {
		t.Errorf("summary %+v", d.Summary)
	}
	_ = w
}
