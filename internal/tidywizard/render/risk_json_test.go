// SPDX-License-Identifier: EUPL-1.2

package render

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
)

var runNow = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

// riskInput is the shared overview with the run clock set, which turns the additive risk fields on.
func riskInput() Input {
	in := input()
	in.Now = runNow
	for i, c := range in.Result.Candidates {
		if c.Verdict == tidywizard.VerdictDecision {
			// A real decision row has a proof; the shared fixture leaves it at none.
			in.Result.Candidates[i].Proof = tidywizard.ProofPatchID
		}
	}
	return in
}

func byID(d Document) map[string]tidywizard.Candidate {
	out := map[string]tidywizard.Candidate{}
	for _, c := range d.Candidates {
		out[c.ID] = c
	}
	return out
}

func TestBuildAddsTheRiskFieldsOfEveryCandidate(t *testing.T) {
	d := Build(riskInput())
	got := byID(d)
	want := map[string]struct {
		level   risk.Level
		reasons []string
		group   bool
	}{
		"worktree:sourcesteward:/tmp/wt-037":              {risk.Yellow, []string{risk.ReasonProofEquivalent}, true},
		"worktree:sourcesteward:/tmp/whr010-a":            {risk.Yellow, []string{risk.ReasonNoOwnCommit, risk.ReasonRecentActivity}, true},
		"worktree:sourcesteward:/tmp/wt-dec":              {risk.Orange, []string{risk.ReasonProofEquivalent, risk.ReasonRecentActivity, risk.ReasonDecisionRow}, true},
		"worktree:sourcesteward:/tmp/wt-041":              {risk.Red, []string{risk.ReasonNotIntegrated, risk.ReasonUncommittedFiles}, false},
		"branch:bundledock:codex/old-1":                   {risk.Yellow, []string{risk.ReasonProofEquivalent}, true},
		"branch:bundledock:" + strings.Repeat("n", 10000): {risk.Red, []string{risk.ReasonNotIntegrated}, false},
	}
	for id, w := range want {
		c, ok := got[id]
		if !ok {
			t.Errorf("candidate %.40q is missing", id)
			continue
		}
		if c.Risk != string(w.level) || (len(w.reasons) > 0 && !reflect.DeepEqual(c.RiskReasons, w.reasons)) || (c.Group != "") != w.group {
			t.Errorf("%.40q: risk %q %v group %q, want %s %v group %v", id, c.Risk, c.RiskReasons, c.Group, w.level, w.reasons, w.group)
		}
	}
	// A kept row has the level and no reason words.
	for _, c := range d.Candidates {
		if c.Risk == "" {
			t.Errorf("%.40q has no risk", c.ID)
		}
		if c.Risk == string(risk.Kept) && len(c.RiskReasons) != 0 {
			t.Errorf("a kept row carries reasons: %+v", c.RiskReasons)
		}
	}
}

func TestBuildWithoutTheClockAddsNothing(t *testing.T) {
	d := Build(input())
	for _, c := range d.Candidates {
		if c.Risk != "" || c.RiskReasons != nil || c.Group != "" || c.PR != 0 {
			t.Fatalf("the document without a clock must stay as before: %+v", c)
		}
	}
	if d.Summary.Risk != nil {
		t.Fatalf("summary risk without a clock: %+v", d.Summary.Risk)
	}
}

func TestSummaryCountsEveryLevel(t *testing.T) {
	d := Build(riskInput())
	if d.Summary.Risk == nil {
		t.Fatal("no risk counts")
	}
	got := *d.Summary.Risk
	// Three red rows: the dirty worktree, the dirty hostile one and the unmerged long branch.
	want := RiskCounts{Green: 0, Yellow: 3, Orange: 1, Red: 3, Kept: 0}
	if got != want {
		t.Fatalf("counts %+v, want %+v", got, want)
	}
	if n := got.Green + got.Yellow + got.Orange + got.Red + got.Kept; n != len(d.Candidates) {
		t.Fatalf("counts add up to %d of %d candidates", n, len(d.Candidates))
	}
}

func TestMergedPullRequestMakesAnEquivalentRowGreenAndShowsInJSON(t *testing.T) {
	in := riskInput()
	for i, c := range in.Result.Candidates {
		if c.ID == "branch:bundledock:codex/old-1" {
			in.Result.Candidates[i].PR = 17
		}
	}
	d := Build(in)
	c := byID(d)["branch:bundledock:codex/old-1"]
	if c.Risk != "green" || !reflect.DeepEqual(c.RiskReasons, []string{risk.ReasonPRMergedHead}) || c.PR != 17 {
		t.Fatalf("%+v", c)
	}
	b, _, err := JSON(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"pr": 17`) {
		t.Fatalf("no pr in the JSON")
	}
}

func TestGroupKeysAreOpaqueLettersAndShared(t *testing.T) {
	d := Build(riskInput())
	re := regexp.MustCompile(`^group[a-p]{64}$`)
	keys := map[string]int{}
	for _, c := range d.Candidates {
		if c.Group == "" {
			continue
		}
		if !re.MatchString(c.Group) {
			t.Errorf("key %q", c.Group)
		}
		keys[c.Group]++
	}
	// The branch row has its own group; the three worktrees of this fixture are fewer than a
	// family, so they share "other in /tmp", although one of them also deletes its branch.
	if len(keys) != 2 {
		t.Fatalf("groups %v", keys)
	}
	for _, n := range keys {
		if n != 1 && n != 3 {
			t.Fatalf("group sizes %v", keys)
		}
	}
}

func TestRiskFieldsAreOmittedFromJSONUntilSet(t *testing.T) {
	b, err := json.Marshal(tidywizard.Candidate{ID: "x", Effects: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"risk", "risk_reasons", "group", `"pr"`} {
		if strings.Contains(string(b), k) {
			t.Errorf("an unset %s is emitted: %s", k, b)
		}
	}
	_ = owner.ScopeOwn
}
