// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// discoveryCollection builds a closed-PR collection of pages*100 records.
// Record i (1-based) is PR number i with head %040x of i for i <= merged and a
// filler head otherwise, so target OID i resolves to PR i.
type discoveryCollection struct {
	pages, last int
	link        bool
	calls       map[string]int
	repoReads   int
	order       []int
	t           *testing.T
}

func (c *discoveryCollection) head(i int) string {
	if i <= 100000 {
		return fmt.Sprintf("%040x", i)
	}
	return fmt.Sprintf("%040x", i)
}

func (c *discoveryCollection) handle(req *http.Request) (*http.Response, error) {
	t := c.t
	if req.Method != http.MethodGet || req.URL.Host != "api.github.com" {
		t.Fatalf("non-GET or foreign host: %s %s", req.Method, req.URL)
	}
	switch {
	case req.URL.Path == "/repos/owner/repo":
		c.calls["repository"]++
		return response(200, tidyRepositoryJSON()), nil
	case req.URL.Path == "/repos/owner/repo/pulls":
		var page int
		if _, err := fmt.Sscanf(req.URL.RawQuery, "state=closed&per_page=100&page=%d", &page); err != nil || page < 1 || page > 10 {
			t.Fatalf("unexpected collection query %q", req.URL.RawQuery)
		}
		c.calls["page"]++
		rows := []string{}
		if page <= c.pages {
			for i := (page-1)*100 + 1; i <= page*100 && i <= c.last; i++ {
				rows = append(rows, tidyPRJSON(i, c.head(i), "main"))
			}
		}
		res := response(200, "["+strings.Join(rows, ",")+"]")
		if c.link {
			parts := []string{}
			base := "https://api.github.com/repos/owner/repo/pulls?state=closed&per_page=100&page=%d"
			if page < c.pages {
				parts = append(parts, fmt.Sprintf("<"+base+`>; rel="next"`, page+1))
			}
			if page > 1 {
				parts = append(parts, fmt.Sprintf("<"+base+`>; rel="prev"`, page-1), fmt.Sprintf("<"+base+`>; rel="first"`, 1))
			}
			parts = append(parts, fmt.Sprintf("<"+base+`>; rel="last"`, c.pages))
			res.Header.Set("Link", strings.Join(parts, ", "))
		}
		return res, nil
	}
	var n int
	if _, err := fmt.Sscanf(req.URL.Path, "/repos/owner/repo/pulls/%d", &n); err != nil || n < 1 || n > c.last {
		t.Fatalf("route outside the closed endpoint set: %s", req.URL)
	}
	c.calls["pull"]++
	c.order = append(c.order, n)
	return response(200, tidyPRJSON(n, c.head(n), "main")), nil
}

func discoveryTargets(n int) []string {
	out := []string{}
	for i := 1; i <= n; i++ {
		out = append(out, fmt.Sprintf("%040x", i))
	}
	return out
}

// A 120-OID fixture over ten full pages: 1 session-open read (preloaded in the
// ledger, as OpenTidySession would have made it), 1 revalidation, 10 pages and
// 116 candidate reads is exactly the 128-request ceiling (Spec 031 FR-018).
func TestTidyDiscoveryBudgetExhaustsLastFourOIDs(t *testing.T) {
	c := &discoveryCollection{pages: 10, last: 1000, link: true, calls: map[string]int{}, t: t}
	s := tidyFixtureSession(t, c.handle)
	s.ledger.stats.Requests = 1
	targets := discoveryTargets(120)
	got, err := s.DiscoverPullRequests(context.Background(), targets, nil)
	if err != nil || len(got) != 120 {
		t.Fatalf("%d rows, %v", len(got), err)
	}
	if c.calls["repository"] != 1 || c.calls["page"] != 10 || c.calls["pull"] != 116 || s.Budget().Requests != 128 {
		t.Fatalf("accounting %+v total %d", c.calls, s.Budget().Requests)
	}
	if !sort.IntsAreSorted(c.order) {
		t.Fatalf("candidate reads not in ascending OID order: %v", c.order)
	}
	for i, row := range got {
		if row.HeadOID != targets[i] {
			t.Fatalf("row %d is %s", i, row.HeadOID)
		}
		if i < 116 && (row.Result != "github-merged-head" || row.PullRequest == nil || row.Coverage != "complete") {
			t.Fatalf("row %d: %+v", i, row)
		}
		if i >= 116 && (row.Result != "discovery-budget-exhausted" || row.PullRequest != nil || row.Reason != "integration-not-proven") {
			t.Fatalf("row %d: %+v", i, row)
		}
	}
}

// With every later OID already beyond the budget the first exhausted OID must
// stop the run: no further read is made after the remaining budget is gone.
func TestTidyDiscoveryNeverExceedsRequestCeiling(t *testing.T) {
	c := &discoveryCollection{pages: 10, last: 1000, link: true, calls: map[string]int{}, t: t}
	s := tidyFixtureSession(t, c.handle)
	got, err := s.DiscoverPullRequests(context.Background(), discoveryTargets(200), nil)
	if err != nil || len(got) != 200 || s.Budget().Requests > 128 {
		t.Fatalf("%d rows %v requests %d", len(got), err, s.Budget().Requests)
	}
	if exhausted := countResult(got, "discovery-budget-exhausted"); exhausted != 200-117 {
		t.Fatalf("exhausted %d", exhausted)
	}
}

func countResult(rows []TidyHeadEvidence, result string) int {
	n := 0
	for _, r := range rows {
		if r.Result == result {
			n++
		}
	}
	return n
}

func TestTidyDiscoveryCompleteCollectionWithoutMatchIsNoMergedPR(t *testing.T) {
	c := &discoveryCollection{pages: 1, last: 5, calls: map[string]int{}, t: t}
	s := tidyFixtureSession(t, c.handle)
	missing := fmt.Sprintf("%040x", 999)
	got, err := s.DiscoverPullRequests(context.Background(), []string{fmt.Sprintf("%040x", 2), missing}, nil)
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].Result != "github-merged-head" || got[0].Coverage != "complete" {
		t.Fatalf("%+v", got[0])
	}
	if got[1].Result != "no-merged-pr" || got[1].Coverage != "complete" || got[1].Reason != "integration-not-proven" || got[1].PullRequest != nil {
		t.Fatalf("%+v", got[1])
	}
}

// A tenth full page with no authoritative end is incomplete: positive evidence
// found earlier still proves its OID, the rest are discovery-incomplete and the
// operation does not fail (Spec 031 FR-017, amending Spec 029 amendment 5).
func TestTidyDiscoveryTenthFullPageIsIncompleteButKeepsFoundProof(t *testing.T) {
	c := &discoveryCollection{pages: 10, last: 1000, calls: map[string]int{}, t: t}
	s := tidyFixtureSession(t, c.handle)
	missing := fmt.Sprintf("%040x", 5000)
	got, err := s.DiscoverPullRequests(context.Background(), []string{fmt.Sprintf("%040x", 250), missing}, nil)
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].Result != "github-merged-head" || got[0].PullRequest == nil || got[0].Coverage != "incomplete" {
		t.Fatalf("found exact-head PR lost: %+v", got[0])
	}
	if got[1].Result != "discovery-incomplete" || got[1].Coverage != "incomplete" || got[1].PullRequest != nil || got[1].Reason != "integration-not-proven" {
		t.Fatalf("absence claimed or wrong result: %+v", got[1])
	}
	if c.calls["page"] != 10 {
		t.Fatalf("pages %d", c.calls["page"])
	}
}

func TestTidyDiscoveryNotFoundOrErrorNeverProvesAbsence(t *testing.T) {
	for _, code := range []int{404, 500} {
		c := &discoveryCollection{pages: 1, last: 5, calls: map[string]int{}, t: t}
		s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
			if strings.HasPrefix(req.URL.Path, "/repos/owner/repo/pulls/") {
				return response(code, `{"message":"x"}`), nil
			}
			return c.handle(req)
		})
		got, err := s.DiscoverPullRequests(context.Background(), []string{fmt.Sprintf("%040x", 2)}, nil)
		if err == nil || got != nil || errors.Is(err, ErrTidyChanged) {
			t.Fatalf("status %d: %+v %v", code, got, err)
		}
	}
}

func TestTidyDiscoveryHintReadsOnlyTheNamedPR(t *testing.T) {
	c := &discoveryCollection{pages: 1, last: 5, calls: map[string]int{}, t: t}
	s := tidyFixtureSession(t, c.handle)
	head := fmt.Sprintf("%040x", 3)
	got, err := s.DiscoverPullRequests(context.Background(), []string{head}, map[string]int{head: 3})
	if err != nil || len(got) != 1 || got[0].Result != "github-merged-head" {
		t.Fatalf("%+v %v", got, err)
	}
	if c.calls["page"] != 0 || c.calls["pull"] != 1 {
		t.Fatalf("hint widened reads: %+v", c.calls)
	}
}

// Exhaustion is sticky (Spec 031 contract: that OID and all later OIDs): a
// later OID that would need no read is still reported exhausted.
func TestTidyDiscoveryExhaustionIsStickyForLaterOIDs(t *testing.T) {
	c := &discoveryCollection{pages: 1, last: 5, calls: map[string]int{}, t: t}
	s := tidyFixtureSession(t, c.handle)
	s.ledger.stats.Requests = 125 // 1 revalidation + 1 page leave exactly one read
	targets := []string{fmt.Sprintf("%040x", 1), fmt.Sprintf("%040x", 2), fmt.Sprintf("%040x", 999)}
	got, err := s.DiscoverPullRequests(context.Background(), targets, nil)
	if err != nil || len(got) != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	want := []string{"github-merged-head", "discovery-budget-exhausted", "discovery-budget-exhausted"}
	for i, row := range got {
		if row.Result != want[i] {
			t.Fatalf("row %d: %+v", i, row)
		}
	}
	if s.Budget().Requests != 128 {
		t.Fatalf("requests %d", s.Budget().Requests)
	}
}

func TestTidyDiscoverySortsUnsortedTargets(t *testing.T) {
	c := &discoveryCollection{pages: 1, last: 5, calls: map[string]int{}, t: t}
	s := tidyFixtureSession(t, c.handle)
	in := []string{fmt.Sprintf("%040x", 3), fmt.Sprintf("%040x", 1), fmt.Sprintf("%040x", 2)}
	got, err := s.DiscoverPullRequests(context.Background(), in, nil)
	if err != nil || len(got) != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	for i, row := range got {
		if row.HeadOID != fmt.Sprintf("%040x", i+1) {
			t.Fatalf("row %d is %s", i, row.HeadOID)
		}
	}
	if !sort.IntsAreSorted(c.order) {
		t.Fatalf("reads out of order: %v", c.order)
	}
}

// A hint examines one PR only; a non-admissible hinted PR must not be reported
// as a complete absence of any merged PR.
func TestTidyDiscoveryHintedNonAdmissiblePRClaimsNoAbsence(t *testing.T) {
	c := &discoveryCollection{pages: 1, last: 5, calls: map[string]int{}, t: t}
	s := tidyFixtureSession(t, c.handle)
	head := fmt.Sprintf("%040x", 4)
	got, err := s.DiscoverPullRequests(context.Background(), []string{head}, map[string]int{head: 2})
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	row := got[0]
	if row.PullRequest != nil || row.Reason != "pr-head-mismatch" || row.Result != "discovery-incomplete" || row.Coverage != "incomplete" {
		t.Fatalf("%+v", row)
	}
}

func TestTidyDiscoveryTwoAdmissiblePRsAreAmbiguous(t *testing.T) {
	head := fmt.Sprintf("%040x", 7)
	rows := []string{tidyPRJSON(1, head, "main"), tidyPRJSON(2, head, "main")}
	s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/repos/owner/repo":
			return response(200, tidyRepositoryJSON()), nil
		case "/repos/owner/repo/pulls":
			return response(200, "["+strings.Join(rows, ",")+"]"), nil
		case "/repos/owner/repo/pulls/1":
			return response(200, rows[0]), nil
		}
		return response(200, rows[1]), nil
	})
	got, err := s.DiscoverPullRequests(context.Background(), []string{head}, nil)
	if err != nil || len(got) != 1 || got[0].Result != "ambiguous-needs-hint" || got[0].Reason != "pr-ambiguous" || got[0].PullRequest != nil {
		t.Fatalf("%+v %v", got, err)
	}
}
