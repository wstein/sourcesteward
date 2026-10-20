// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
)

// fixtureTidyV3SharedHeads creates refs branches that share oids distinct tips
// none of which is reachable from the selected base.
func fixtureTidyV3SharedHeads(t *testing.T, refs, oids int) (string, string, hosting.TidyRepositoryIdentity, []string) {
	t.Helper()
	dir, _ := fixtureTidyV3Git(t)
	root := gitFixture(t, dir, "rev-parse", "HEAD")
	tree := gitFixture(t, dir, "rev-parse", "HEAD^{tree}")
	repo := hosting.TidyRepositoryIdentity{Host: "github.com", TidyRepositoryID: hosting.TidyRepositoryID{ID: 1, NodeID: "R_fixture", FullName: "owner/repo"}, DefaultBranch: "main"}
	heads := []string{}
	for i := 0; i < oids; i++ {
		heads = append(heads, gitFixture(t, dir, "commit-tree", tree, "-p", root, "-m", fmt.Sprintf("shared %d", i)))
	}
	for i := 0; i < refs; i++ {
		gitFixture(t, dir, "update-ref", fmt.Sprintf("refs/heads/topic-%02d", i), heads[i%oids])
	}
	gitFixture(t, dir, "commit", "--allow-empty", "-qm", "advance base")
	base := gitFixture(t, dir, "symbolic-ref", "HEAD")
	sorted := append([]string{}, heads...)
	sort.Strings(sorted)
	return dir, base, repo, sorted
}

func TestTidyV3FortyRefsSharingTwelveOIDsPlanWithoutHints(t *testing.T) {
	dir, base, repo, heads := fixtureTidyV3SharedHeads(t, 40, 12)
	c, session := fixtureTidyV3Online(t, dir, repo, nil)
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || validateV3Result(r) != nil {
		t.Fatalf("shared heads needed hints: %+v %+v", r.Status, r.Diagnostics)
	}
	if len(session.targets) != 1 || !equalStrings(session.targets[0], heads) {
		t.Fatalf("targets are not the 12 distinct OIDs in ascending order: %v", session.targets)
	}
	if len(session.hintCalls[0]) != 0 {
		t.Fatalf("hints invented: %v", session.hintCalls)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTidyV3IdenticalDuplicateHintIsOneHintOnePlanEntry(t *testing.T) {
	dir, base, repo, heads := fixtureTidyV3SharedHeads(t, 6, 2)
	c, session := fixtureTidyV3Online(t, dir, repo, nil)
	h := V3PRHint{OID: heads[0], Number: 7}
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", ProtectWorktrees: []string{}, PRHints: []V3PRHint{h, h}})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("identical duplicate hint refused: %+v", r.Diagnostics)
	}
	if len(r.Plan.Online.PRHints) != 1 || r.Plan.Online.PRHints[0] != h || len(session.hintCalls[0]) != 1 {
		t.Fatalf("hint not aggregated: %+v %v", r.Plan.Online.PRHints, session.hintCalls)
	}
}

func TestTidyV3HintErrorsNameTheSelector(t *testing.T) {
	dir, base, repo, heads := fixtureTidyV3SharedHeads(t, 4, 2)
	other := strings.Repeat("e", 40)
	c, session := fixtureTidyV3Online(t, dir, repo, nil)
	for name, tc := range map[string]struct {
		hints []V3PRHint
		want  []string
	}{
		"conflict":      {[]V3PRHint{{OID: heads[0], Number: 1}, {OID: heads[0], Number: 2}}, []string{"--pr " + heads[0] + "=1", "--pr " + heads[0] + "=2"}},
		"unselected":    {[]V3PRHint{{OID: other, Number: 5}}, []string{"--pr " + other + "=5", "no selected reference"}},
		"number shared": {[]V3PRHint{{OID: heads[0], Number: 3}, {OID: heads[1], Number: 3}}, []string{"--pr " + heads[0] + "=3", "--pr " + heads[1] + "=3"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", ProtectWorktrees: []string{}, PRHints: tc.hints})
			if r.Status == "ok" || len(r.Diagnostics) == 0 || len(session.targets) != 0 {
				t.Fatalf("bad hints accepted: %+v", r)
			}
			for _, want := range tc.want {
				if !strings.Contains(r.Diagnostics[0].Message, want) {
					t.Fatalf("message %q lacks %q", r.Diagnostics[0].Message, want)
				}
			}
		})
	}
}

// Rows the provider could not decide stay retained with a bounded note; the
// plan completes and claims no absence for incomplete or exhausted discovery.
func TestTidyV3DiscoveryCoverageNotesAndRetention(t *testing.T) {
	dir, base, repo, heads := fixtureTidyV3SharedHeads(t, 8, 4)
	c, session := fixtureTidyV3Online(t, dir, repo, nil)
	results := []string{"no-merged-pr", "discovery-incomplete", "discovery-budget-exhausted", "discovery-budget-exhausted"}
	for i, h := range heads {
		cov := "complete"
		if results[i] == "discovery-incomplete" {
			cov = "incomplete"
		}
		session.rows[h] = hosting.TidyHeadEvidence{HeadOID: h, Reason: "integration-not-proven", Result: results[i], Coverage: cov}
	}
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || validateV3Result(r) != nil {
		t.Fatalf("%+v", r.Diagnostics)
	}
	for _, cand := range r.Plan.Candidates {
		if strings.HasPrefix(cand.Name, "refs/heads/topic-") && (cand.Decision != "retain" || cand.Reason != "integration-not-proven") {
			t.Fatalf("undecided OID not retained: %+v", cand)
		}
	}
	joined := strings.Join(r.Limitations, "\n")
	for _, want := range []string{"discovery-incomplete: 1 head OID", "discovery-budget-exhausted: 2 head OIDs"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("note %q missing from %q", want, joined)
		}
	}
}

func TestTidyV3DiscoverySortsTargetsAscending(t *testing.T) {
	dir, base, repo, heads := fixtureTidyV3SharedHeads(t, 5, 5)
	c, session := fixtureTidyV3Online(t, dir, repo, nil)
	c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", ProtectWorktrees: []string{}})
	if len(session.targets) != 1 || !sort.StringsAreSorted(session.targets[0]) || len(session.targets[0]) != len(heads) {
		t.Fatalf("%v", session.targets)
	}
}
