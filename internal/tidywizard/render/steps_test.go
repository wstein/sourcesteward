// SPDX-License-Identifier: EUPL-1.2

package render

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
)

// The group steps read the same groups as the first screen, unfolded: one step per group, every
// choosable row once (Spec 044 F-1: 15 groups on the live data).
func TestStepGroupsAreTheUnfoldedGroupsOfTheRealWorkspace(t *testing.T) {
	d := realDocument(t, longAgo)
	groups := Groups(d)
	rows := 0
	seen := map[string]bool{}
	for _, g := range groups {
		rows += len(g.Rows)
		for _, r := range g.Rows {
			if seen[r.ID] {
				t.Errorf("%s is in two groups", r.ID)
			}
			seen[r.ID] = true
		}
	}
	if len(groups) != 15 || rows != 165 {
		t.Errorf("%d groups, %d rows", len(groups), rows)
	}
	if got := d.KeptRows(); got != 964 {
		t.Errorf("kept rows %d", got)
	}
	if len(d.AllCandidates()) != 1129 {
		t.Errorf("candidates %d", len(d.AllCandidates()))
	}
}

// Every step shows owner, effect and recovery lines in sentences and no internal code; the golden
// is the real output of the recorded workspace (SC-007).
func TestStepLinesAreSentencesNotCodes(t *testing.T) {
	d := realDocument(t, longAgo)
	var b strings.Builder
	code := regexp.MustCompile(`patch-id|merge-noop|ancestor|no-net-change|proof-|registration-record|branch-attached|delete-branch|remove-files|remove-registration|unmerged`)
	for i, g := range Groups(d) {
		fmt.Fprintf(&b, "Step %d of %d   %s   risk %s\n", i+1, len(Groups(d)), g.Label(), g.Level)
		lines := d.StepLines(g)
		if len(lines) < 3 || len(lines) > 6 {
			t.Errorf("group %d has %d step lines", i, len(lines))
		}
		for _, l := range lines {
			if code.MatchString(l) {
				t.Errorf("group %d: a code in %q", i, l)
			}
			b.WriteString(l + "\n")
		}
		b.WriteString("\n")
	}
	golden(t, "steps-real.txt.golden", []byte(b.String()))
}

// A hostile owner label reaches the step lines escaped.
func TestStepLinesEscapeTheOwner(t *testing.T) {
	d := realDocument(t, longAgo)
	g := Groups(d)[0]
	g.owners = []ownerCount{{label: "co\x1b[31mdex\n", n: len(g.Rows)}}
	for _, l := range d.StepLines(g) {
		if strings.ContainsAny(l, "\x1b\r") || strings.Count(l, "\n") != 0 {
			t.Errorf("raw control in %q", l)
		}
	}
	if !strings.Contains(strings.Join(d.StepLines(g), "\n"), `co\x1b[31mdex\n`) {
		t.Errorf("the owner is not shown")
	}
}

// Row levels travel with the rows: the group row is the assessed row.
func TestGroupRowsCarryTheirLevel(t *testing.T) {
	d := realDocument(t, longAgo)
	n := map[risk.Level]int{}
	for _, g := range Groups(d) {
		for _, r := range g.Rows {
			n[r.Risk.Level]++
		}
	}
	if n[risk.Green] != 6 || n[risk.Yellow] != 93 || n[risk.Orange] != 66 {
		t.Errorf("%v", n)
	}
	if got := d.CandidateByID("nope"); got != nil {
		t.Errorf("%v", got)
	}
	if all := d.AllCandidates(); len(all) == 0 || d.CandidateByID(all[0].ID) == nil || d.CandidateByID(all[0].ID).Kind != tidywizard.Kind(all[0].Kind) {
		t.Errorf("lookup")
	}
}
