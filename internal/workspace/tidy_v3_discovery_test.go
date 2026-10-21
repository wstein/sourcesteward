// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
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
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || validateV3Result(r) != nil {
		t.Fatalf("shared heads needed hints: %+v %+v", r.Status, r.Diagnostics)
	}
	if len(session.targets) != 1 || !equalStrings(session.targets[0], heads) {
		t.Fatalf("targets are not the 12 distinct OIDs in ascending order: %v", session.targets)
	}
	if len(session.hintCalls[0]) != 0 {
		t.Fatalf("hints invented: %v", session.hintCalls)
	}
	// One record per OID lists every ref carrying it (Spec 031 FR-014).
	records := r.Plan.Online.Discovery
	total := 0
	if len(records) != 12 {
		t.Fatalf("%d discovery records", len(records))
	}
	for i, d := range records {
		total += len(d.Refs)
		if d.OID != heads[i] || d.Source != "discovered" || d.Coverage != "complete" || d.Result != "no-merged-pr" || len(d.Refs) < 3 || !sort.StringsAreSorted(d.Refs) {
			t.Fatalf("record %d: %+v", i, d)
		}
	}
	if total != 40 {
		t.Fatalf("records cover %d refs", total)
	}
}

func TestTidyV3OfflinePlanHasNoDiscoveryField(t *testing.T) {
	dir, base, _, _ := fixtureTidyV3SharedHeads(t, 3, 2)
	r := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: base, ProtectWorktrees: []string{}})
	raw, err := json.Marshal(r.Plan)
	if err != nil || r.Plan == nil || strings.Contains(string(raw), "discovery") {
		t.Fatalf("%v %s", err, raw)
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
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}, PRHints: []V3PRHint{h, h}})
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
		"conflict":      {[]V3PRHint{{OID: heads[0], Number: 1}, {OID: heads[0], Number: 2}}, []string{`--pr "` + heads[0] + `=1"`, `--pr "` + heads[0] + `=2"`}},
		"unselected":    {[]V3PRHint{{OID: other, Number: 5}}, []string{`--pr "` + other + `=5"`, "no selected reference"}},
		"number shared": {[]V3PRHint{{OID: heads[0], Number: 3}, {OID: heads[1], Number: 3}}, []string{`--pr "` + heads[0] + `=3"`, `--pr "` + heads[1] + `=3"`}},
	} {
		t.Run(name, func(t *testing.T) {
			r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}, PRHints: tc.hints})
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
	dir, base, repo, heads := fixtureTidyV3SharedHeads(t, 10, 5)
	c, session := fixtureTidyV3Online(t, dir, repo, nil)
	results := []string{"no-merged-pr", "discovery-incomplete", "discovery-budget-exhausted", "discovery-budget-exhausted", "ambiguous-needs-hint"}
	for i, h := range heads {
		cov := "complete"
		if results[i] == "discovery-incomplete" {
			cov = "incomplete"
		}
		reason := "integration-not-proven"
		if results[i] == "ambiguous-needs-hint" {
			reason = "pr-ambiguous"
		}
		session.rows[h] = hosting.TidyHeadEvidence{HeadOID: h, Reason: reason, Result: results[i], Coverage: cov}
	}
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || validateV3Result(r) != nil {
		t.Fatalf("%+v", r.Diagnostics)
	}
	for _, cand := range r.Plan.Candidates {
		if strings.HasPrefix(cand.Name, "refs/heads/topic-") && (cand.Decision != "retain" || (cand.Reason != "integration-not-proven" && cand.Reason != "pr-ambiguous")) {
			t.Fatalf("undecided OID not retained: %+v", cand)
		}
	}
	joined := strings.Join(r.Limitations, "\n")
	for _, want := range []string{"discovery-incomplete: 1 head OID", "discovery-budget-exhausted: 2 head OIDs", "ambiguous-needs-hint: 1 head OID", heads[1] + " [refs/heads/", heads[4] + " [refs/heads/", "--pr OID=N"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("note %q missing from %q", want, joined)
		}
	}
	for _, d := range r.Plan.Online.Discovery {
		if len(d.Refs) != 2 || d.Result == "" {
			t.Fatalf("%+v", d)
		}
	}
	for _, note := range r.Limitations {
		if len(note) > tidyV3NoteBytes+64 {
			t.Fatalf("unbounded note %d", len(note))
		}
	}
}

func TestTidyV3DiscoverySortsTargetsAscending(t *testing.T) {
	dir, base, repo, heads := fixtureTidyV3SharedHeads(t, 5, 5)
	c, session := fixtureTidyV3Online(t, dir, repo, nil)
	c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
	if len(session.targets) != 1 || !sort.StringsAreSorted(session.targets[0]) || len(session.targets[0]) != len(heads) {
		t.Fatalf("%v", session.targets)
	}
}

// budgetedTidyV3Hosting mirrors the hosting ledger: a shared 128-request
// ceiling that discovery, repository revalidation and PR revalidation all draw
// on, so a plan that re-reads the provider after discovery cannot hide.
type budgetedTidyV3Hosting struct {
	*fixtureTidyV3Hosting
	used, pages int
}

const budgetedTidyV3Limit = 128

func (s *budgetedTidyV3Hosting) spend(n int) error {
	if s.used+n > budgetedTidyV3Limit {
		s.used = budgetedTidyV3Limit
		return hosting.ErrTidyLimit
	}
	s.used += n
	return nil
}
func (s *budgetedTidyV3Hosting) Budget() hosting.TidyHTTPBudget {
	return hosting.TidyHTTPBudget{Requests: s.used, RawBytes: 1, NetworkAttempted: true}
}
func (s *budgetedTidyV3Hosting) RevalidateRepository(ctx context.Context) error {
	if err := s.spend(1); err != nil {
		return err
	}
	return ctx.Err()
}
func (s *budgetedTidyV3Hosting) RevalidatePullRequest(ctx context.Context, pr hosting.TidyPullRequest) error {
	if err := s.spend(2); err != nil {
		return err
	}
	return s.fixtureTidyV3Hosting.RevalidatePullRequest(ctx, pr)
}
func (s *budgetedTidyV3Hosting) DiscoverPullRequests(ctx context.Context, targets []string, hints map[string]int) ([]hosting.TidyHeadEvidence, error) {
	if err := s.spend(1 + s.pages); err != nil {
		return nil, err
	}
	room := budgetedTidyV3Limit - s.used
	rows, err := s.fixtureTidyV3Hosting.DiscoverPullRequests(ctx, targets, hints)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if i >= room {
			rows[i] = hosting.TidyHeadEvidence{HeadOID: rows[i].HeadOID, Reason: "integration-not-proven", Result: "discovery-budget-exhausted", Coverage: "complete"}
			continue
		}
		s.used++
	}
	return rows, nil
}

func fixtureTidyV3Budgeted(t *testing.T, dir string, repo hosting.TidyRepositoryIdentity, rows []hosting.TidyHeadEvidence, pages int) (tidyV3RefsCoordinator, *budgetedTidyV3Hosting) {
	c, inner := fixtureTidyV3Online(t, dir, repo, rows)
	b := &budgetedTidyV3Hosting{fixtureTidyV3Hosting: inner, used: 1, pages: pages}
	c.openHosting = func(context.Context, string, hosting.TidySessionOptions) (tidyV3Hosting, error) { return b, nil }
	return c, b
}

func TestTidyV3PlanCompletesWhenDiscoveryExhaustsTheBudget(t *testing.T) {
	dir, base, repo, heads := fixtureTidyV3SharedHeads(t, 120, 120)
	c, s := fixtureTidyV3Budgeted(t, dir, repo, nil, 10)
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, StateDir: privateDir(t), ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || validateV3Result(r) != nil {
		t.Fatalf("plan failed at the budget: %+v %+v", r.Status, r.Diagnostics)
	}
	if s.used != budgetedTidyV3Limit || len(s.rechecks) != 0 || len(heads) != 120 {
		t.Fatalf("used=%d rechecks=%d", s.used, len(s.rechecks))
	}
	if !strings.Contains(strings.Join(r.Limitations, "\n"), "discovery-budget-exhausted: 4 head OIDs") {
		t.Fatalf("exhausted OIDs not reported: %v", r.Limitations)
	}
}

func TestTidyV3PlanWithFortyProvenOIDsMakesNoPlanTimeProviderReads(t *testing.T) {
	dir, _ := fixtureTidyV3Git(t)
	root := gitFixture(t, dir, "rev-parse", "HEAD")
	tree := gitFixture(t, dir, "rev-parse", "HEAD^{tree}")
	repo := hosting.TidyRepositoryIdentity{Host: "github.com", TidyRepositoryID: hosting.TidyRepositoryID{ID: 1, NodeID: "R_fixture", FullName: "owner/repo"}, DefaultBranch: "main"}
	rows := []hosting.TidyHeadEvidence{}
	parent := root
	for i := 0; i < 40; i++ {
		head := gitFixture(t, dir, "commit-tree", tree, "-p", root, "-m", fmt.Sprintf("original %d", i))
		result := gitFixture(t, dir, "commit-tree", tree, "-p", parent, "-m", fmt.Sprintf("integrated %d", i))
		parent = result
		gitFixture(t, dir, "update-ref", fmt.Sprintf("refs/heads/topic-%02d", i), head)
		rows = append(rows, tidyV3PR(repo, head, root, result, "main", i+1))
	}
	gitFixture(t, dir, "update-ref", "refs/heads/selected-base", parent)
	c, s := fixtureTidyV3Budgeted(t, dir, repo, rows, 1)
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: "refs/heads/selected-base", Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, StateDir: privateDir(t), ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("%+v", r.Diagnostics)
	}
	deleted := 0
	for _, cand := range r.Plan.Candidates {
		if cand.Decision == "delete" {
			deleted++
		}
	}
	if deleted != 40 || len(s.rechecks) != 0 || s.used != 1+1+1+40 {
		t.Fatalf("deleted=%d rechecks=%d used=%d", deleted, len(s.rechecks), s.used)
	}
}

// Discovery records have their own bounds and never make a plan refuse
// (Spec 031 FR-017): at, above and far above the record cap the plan is ok,
// decisions are untouched and overflow is dropped with a note.
func TestTidyV3DiscoveryRecordsNeverRefuseThePlan(t *testing.T) {
	isolateTidyV3Environment(t)
	for _, tc := range []struct {
		name       string
		refs, oids int
		kept       int
		truncated  bool
	}{
		{"at the record cap", 256, 256, 256, false},
		{"above the record cap", 257, 257, 256, true},
		{"previous aggregate limit", 400, 400, 256, true},
		{"far above", 700, 700, 256, true},
		{"ref cap with shared heads", 600, 100, 85, true},
		{"ref cap far exceeded", 700, 100, 73, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parallelFixture(t)
			dir, base, repo, _ := fixtureTidyV3SharedHeads(t, tc.refs, tc.oids)
			c, _ := fixtureTidyV3Online(t, dir, repo, nil)
			r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
			if r.Status != "ok" || r.Plan == nil || validateV3Result(r) != nil {
				t.Fatalf("plan refused: %s %+v", r.Status, r.Diagnostics)
			}
			if len(r.Plan.Candidates) < tc.refs {
				t.Fatalf("%d candidates", len(r.Plan.Candidates))
			}
			notes := strings.Contains(strings.Join(r.Limitations, "\n"), "discovery-records-truncated")
			if len(r.Plan.Online.Discovery) != tc.kept || notes != tc.truncated {
				t.Fatalf("kept %d note=%v", len(r.Plan.Online.Discovery), notes)
			}
		})
	}
}

func TestTidyV3DiscoveryRefCapDropsOverflowWithNote(t *testing.T) {
	dir, base, repo, _ := fixtureTidyV3SharedHeads(t, 700, 100) // 7 refs per OID: 73 records fit 511 refs
	c, _ := fixtureTidyV3Online(t, dir, repo, nil)
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("%+v", r.Diagnostics)
	}
	refs := 0
	for _, d := range r.Plan.Online.Discovery {
		refs += len(d.Refs)
	}
	if refs > v3DiscoveryRefCap || len(r.Plan.Online.Discovery) != 73 || !strings.Contains(strings.Join(r.Limitations, "\n"), "stored 73 of 100 discovery records") {
		t.Fatalf("refs=%d records=%d %v", refs, len(r.Plan.Online.Discovery), r.Limitations)
	}
}

func TestTidyV3DiscoveryRecordValidationKillsEachInvariant(t *testing.T) {
	dir, base, repo, heads := fixtureTidyV3SharedHeads(t, 4, 2)
	c, _ := fixtureTidyV3Online(t, dir, repo, nil)
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || len(r.Plan.Online.Discovery) != 2 {
		t.Fatalf("%+v", r.Diagnostics)
	}
	fresh := func() (V3OnlineSelection, V3Plan) {
		p := *r.Plan
		o := *p.Online
		o.Discovery = append([]V3DiscoveryRecord{}, o.Discovery...)
		for i := range o.Discovery {
			o.Discovery[i].Refs = append([]string{}, o.Discovery[i].Refs...)
		}
		o.PRHints = []V3PRHint{}
		return o, p
	}
	if o, p := fresh(); !validV3Online(o, p) {
		t.Fatal("base plan invalid")
	}
	for name, mutate := range map[string]func(*V3OnlineSelection){
		"hint source without a hint":     func(o *V3OnlineSelection) { o.Discovery[0].Source = "hint" },
		"discovered source with a hint":  func(o *V3OnlineSelection) { o.PRHints = []V3PRHint{{OID: heads[0], Number: 3}} },
		"absence on incomplete coverage": func(o *V3OnlineSelection) { o.Discovery[0].Coverage = "incomplete" },
		"duplicate OID": func(o *V3OnlineSelection) {
			o.Discovery[1].OID = o.Discovery[0].OID
			o.Discovery[1].Refs = o.Discovery[0].Refs
		},
		"ref not carrying the OID": func(o *V3OnlineSelection) { o.Discovery[0].Refs = o.Discovery[1].Refs },
		"unsorted refs": func(o *V3OnlineSelection) {
			o.Discovery[0].Refs = []string{o.Discovery[0].Refs[1], o.Discovery[0].Refs[0]}
		},
		"duplicate refs": func(o *V3OnlineSelection) {
			o.Discovery[0].Refs = []string{o.Discovery[0].Refs[0], o.Discovery[0].Refs[0]}
		},
		"no refs":                  func(o *V3OnlineSelection) { o.Discovery[0].Refs = []string{} },
		"PR on a negative result":  func(o *V3OnlineSelection) { o.Discovery[0].PR = &V3PRIdentity{ID: 1, NodeID: "n", Number: 1} },
		"merged head without a PR": func(o *V3OnlineSelection) { o.Discovery[0].Result = "github-merged-head" },
		"unknown result":           func(o *V3OnlineSelection) { o.Discovery[0].Result = "absent" },
		"unknown OID":              func(o *V3OnlineSelection) { o.Discovery[0].OID = strings.Repeat("e", 40) },
		"too many records": func(o *V3OnlineSelection) {
			for len(o.Discovery) <= v3DiscoveryRecordCap {
				o.Discovery = append(o.Discovery, o.Discovery[0])
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			o, p := fresh()
			mutate(&o)
			if validV3Online(o, p) {
				t.Fatal("invalid discovery record accepted")
			}
		})
	}
}

// The per-note bound must hold however many OIDs are undecided.
func TestTidyV3DiscoveryNoteIsBounded(t *testing.T) {
	dir, base, repo, heads := fixtureTidyV3SharedHeads(t, 120, 120)
	c, session := fixtureTidyV3Online(t, dir, repo, nil)
	for _, h := range heads {
		session.rows[h] = hosting.TidyHeadEvidence{HeadOID: h, Reason: "integration-not-proven", Result: "discovery-budget-exhausted", Coverage: "complete"}
	}
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
	if r.Status != "ok" {
		t.Fatalf("%+v", r.Diagnostics)
	}
	found := false
	for _, note := range r.Limitations {
		if strings.HasPrefix(note, "discovery-budget-exhausted: 120 head OIDs") {
			found = true
			if len(note) > tidyV3NoteBytes+64 || !strings.Contains(note, " more") {
				t.Fatalf("note not bounded: %d bytes", len(note))
			}
		}
	}
	if !found {
		t.Fatalf("%v", r.Limitations)
	}
}

// Stored-plan validation enforces the record cap on its own: 257 distinct OIDs
// with one ref each stay under the ref and byte caps, so only the record cap
// can refuse them.
func TestTidyV3StoredDiscoveryRecordCapIsEnforcedAlone(t *testing.T) {
	dir, base, repo, _ := fixtureTidyV3SharedHeads(t, 4, 2)
	c, _ := fixtureTidyV3Online(t, dir, repo, nil)
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("%+v", r.Diagnostics)
	}
	build := func(n int) (V3OnlineSelection, V3Plan) {
		p := *r.Plan
		p.Candidates = nil
		o := *p.Online
		o.PRHints = []V3PRHint{}
		o.Discovery = nil
		for i := 0; i < n; i++ {
			name, oid := fmt.Sprintf("refs/heads/synthetic-%03d", i), fmt.Sprintf("%040x", i+1)
			p.Candidates = append(p.Candidates, V3Candidate{Name: name, OID: oid})
			o.Discovery = append(o.Discovery, V3DiscoveryRecord{OID: oid, Source: "discovered", Refs: []string{name}, Coverage: "complete", Result: "no-merged-pr"})
		}
		return o, p
	}
	if o, p := build(v3DiscoveryRecordCap); !validV3Online(o, p) {
		t.Fatal("records at the cap refused")
	}
	if o, p := build(v3DiscoveryRecordCap + 1); validV3Online(o, p) {
		t.Fatal("records above the cap accepted")
	}
}

// Notes describe every undecided OID even when the stored list is truncated.
func TestTidyV3DiscoveryNotesUseTheFullSetNotTheKeptRecords(t *testing.T) {
	dir, base, repo, heads := fixtureTidyV3SharedHeads(t, 300, 300)
	c, session := fixtureTidyV3Online(t, dir, repo, nil)
	for _, h := range heads {
		session.rows[h] = hosting.TidyHeadEvidence{HeadOID: h, Reason: "integration-not-proven", Result: "discovery-incomplete", Coverage: "incomplete"}
	}
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
	if r.Status != "ok" || len(r.Plan.Online.Discovery) != v3DiscoveryRecordCap {
		t.Fatalf("%s kept %d", r.Status, len(r.Plan.Online.Discovery))
	}
	if joined := strings.Join(r.Limitations, "\n"); !strings.Contains(joined, "discovery-incomplete: 300 head OIDs (300 refs)") || !strings.Contains(joined, "stored 256 of 300") {
		t.Fatalf("%v", r.Limitations)
	}
}

// Long ref names repeated in the records are bounded by bytes as well, so the
// informational list cannot push a plan toward the 1 MiB record limit.
func TestTidyV3DiscoveryListIsByteBounded(t *testing.T) {
	dir, _ := fixtureTidyV3Git(t)
	root := gitFixture(t, dir, "rev-parse", "HEAD")
	tree := gitFixture(t, dir, "rev-parse", "HEAD^{tree}")
	repo := hosting.TidyRepositoryIdentity{Host: "github.com", TidyRepositoryID: hosting.TidyRepositoryID{ID: 1, NodeID: "R_fixture", FullName: "owner/repo"}, DefaultBranch: "main"}
	long := strings.Repeat("a", 200) + "/" + strings.Repeat("b", 200) + "/" + strings.Repeat("c", 200)
	for i := 0; i < 150; i++ {
		head := gitFixture(t, dir, "commit-tree", tree, "-p", root, "-m", fmt.Sprintf("long %d", i))
		gitFixture(t, dir, "update-ref", fmt.Sprintf("refs/heads/%s/%03d", long, i), head)
	}
	gitFixture(t, dir, "commit", "--allow-empty", "-qm", "advance base")
	base := gitFixture(t, dir, "symbolic-ref", "HEAD")
	c, _ := fixtureTidyV3Online(t, dir, repo, nil)
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("%s %+v", r.Status, r.Diagnostics)
	}
	size := 0
	for _, d := range r.Plan.Online.Discovery {
		size += v3DiscoveryBytes(d)
	}
	if size > v3DiscoveryByteCap || len(r.Plan.Online.Discovery) >= 150 || !strings.Contains(strings.Join(r.Limitations, "\n"), "discovery-records-truncated") {
		t.Fatalf("size=%d kept=%d", size, len(r.Plan.Online.Discovery))
	}
}

// Stored-plan validation enforces the byte cap alone: one record with 100 refs
// stays under the record and ref caps but exceeds 128 KiB.
func TestTidyV3StoredDiscoveryByteCapIsEnforcedAlone(t *testing.T) {
	dir, base, repo, _ := fixtureTidyV3SharedHeads(t, 4, 2)
	c, _ := fixtureTidyV3Online(t, dir, repo, nil)
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, Online: true, Remote: "origin", IntegrationProofs: []string{"ancestor"}, ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("%+v", r.Diagnostics)
	}
	build := func(refBytes int) (V3OnlineSelection, V3Plan) {
		p := *r.Plan
		p.Candidates = nil
		o := *p.Online
		o.PRHints = []V3PRHint{}
		oid := fmt.Sprintf("%040x", 1)
		refs := []string{}
		for i := 0; i < 100; i++ {
			name := fmt.Sprintf("refs/heads/%03d-%s", i, strings.Repeat("x", refBytes))
			refs = append(refs, name)
			p.Candidates = append(p.Candidates, V3Candidate{Name: name, OID: oid})
		}
		o.Discovery = []V3DiscoveryRecord{{OID: oid, Source: "discovered", Refs: refs, Coverage: "complete", Result: "no-merged-pr"}}
		return o, p
	}
	if o, p := build(100); !validV3Online(o, p) {
		t.Fatal("small list refused")
	}
	if o, p := build(1500); validV3Online(o, p) {
		t.Fatal("list above the byte cap accepted")
	}
}
