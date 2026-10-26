// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// tipPR is one item of `commits/{sha}/pulls`: head repository id headRepo, base repository 41.
func tipPR(number int, head, baseRef string, mergedAt bool, state string, headRepo int, draft bool) string {
	merged, mergeSHA := "null", "null"
	if mergedAt {
		merged, mergeSHA = `"2026-10-18T00:00:00Z"`, `"`+strings.Repeat("c", 40)+`"`
	}
	node := "R_repo"
	if headRepo != 41 {
		node = fmt.Sprintf("R_%d", headRepo)
	}
	return fmt.Sprintf(`{"id":%d,"node_id":"PR_%d","number":%d,"state":%q,"merged_at":%s,"draft":%v,"head":{"sha":%q,"ref":"topic-%d","repo":{"id":%d,"node_id":%q,"full_name":"owner/repo"}},"base":{"sha":%q,"ref":%q,"repo":{"id":41,"node_id":"R_repo","full_name":"owner/repo"}},"merge_commit_sha":%s}`,
		100+number, number, number, state, merged, draft, head, number, headRepo, node, strings.Repeat("b", 40), baseRef, mergeSHA)
}

// tipServer answers each tip's list; a missing tip is a 404. It records the requests.
type tipServer struct {
	t        *testing.T
	lists    map[string]string
	status   map[string]int
	requests []string
}

func (s *tipServer) handle(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet || req.URL.Host != "api.github.com" {
		s.t.Fatalf("non-GET or foreign host: %s %s", req.Method, req.URL)
	}
	s.requests = append(s.requests, req.URL.RequestURI())
	if req.URL.Path == "/repos/owner/repo" {
		return response(200, tidyRepositoryJSON()), nil
	}
	var sha string
	if _, err := fmt.Sscanf(strings.TrimPrefix(req.URL.Path, "/repos/owner/repo/commits/"), "%40s", &sha); err != nil || !strings.HasSuffix(req.URL.Path, "/pulls") || req.URL.RawQuery != "per_page=30&page=1" {
		s.t.Fatalf("route outside the closed endpoint set: %s", req.URL)
	}
	if code, ok := s.status[sha]; ok {
		return response(code, `{}`), nil
	}
	list, ok := s.lists[sha]
	if !ok {
		return response(404, `{"message":"No commit found"}`), nil
	}
	return response(200, list), nil
}

func tipN(n int) string { return fmt.Sprintf("%040x", n) }

func TestReadTipPullRequestsClassifiesByRepositoryAndBase(t *testing.T) {
	srv := &tipServer{t: t, lists: map[string]string{
		// 1: merged into main by two pull requests: the smallest number.
		tipN(1): "[" + tipPR(5, tipN(1), "main", true, "closed", 41, false) + "," + tipPR(9, tipN(1), "main", true, "closed", 41, false) + "]",
		// 2: merged only into a parent feature branch (a stacked child): not on the base.
		tipN(2): "[" + tipPR(6, tipN(2), "feature/parent", true, "closed", 41, false) + "]",
		// 3: closed unmerged.
		tipN(3): "[" + tipPR(7, tipN(3), "main", false, "closed", 41, false) + "]",
		// 4: a fork pull request with the same head: neither merged nor closed-unmerged.
		tipN(4): "[" + tipPR(8, tipN(4), "main", true, "closed", 99, false) + "," + tipPR(10, tipN(4), "main", false, "closed", 99, false) + "]",
		// 5: the tip is only contained in a pull request whose head is another commit.
		tipN(5): "[" + tipPR(11, tipN(6), "main", true, "closed", 41, false) + "]",
		// 6: an empty answer proves nothing.
		tipN(6): "[]",
		// 8: an open pull request and a draft are no evidence; 3 plus a merge wins over closed unmerged.
		tipN(8): "[" + tipPR(12, tipN(8), "main", false, "open", 41, false) + "," + tipPR(13, tipN(8), "main", true, "closed", 41, true) + "]",
		// 10: closed by a merge whose merge commit is missing is not a merge.
		tipN(10): strings.Replace("["+tipPR(16, tipN(10), "main", true, "closed", 41, false)+"]", `"`+strings.Repeat("c", 40)+`"`, "null", 1),
		tipN(9):  "[" + tipPR(14, tipN(9), "main", false, "closed", 41, false) + "," + tipPR(15, tipN(9), "main", true, "closed", 41, false) + "]",
	}}
	s := tidyFixtureSession(t, srv.handle)
	tips := []string{tipN(9), tipN(1), tipN(2), tipN(3), tipN(4), tipN(5), tipN(6), tipN(7), tipN(8), tipN(10)}
	rows, err := s.ReadTipPullRequests(context.Background(), tips)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]TidyTipEvidence{
		tipN(1):  {Tip: tipN(1), State: TidyTipMerged, Number: 5},
		tipN(2):  {Tip: tipN(2), State: TidyTipUnknown},
		tipN(3):  {Tip: tipN(3), State: TidyTipClosedUnmerged},
		tipN(4):  {Tip: tipN(4), State: TidyTipUnknown},
		tipN(5):  {Tip: tipN(5), State: TidyTipUnknown},
		tipN(6):  {Tip: tipN(6), State: TidyTipUnknown},
		tipN(7):  {Tip: tipN(7), State: TidyTipUnknown}, // 404: GitHub does not know the commit
		tipN(8):  {Tip: tipN(8), State: TidyTipUnknown},
		tipN(9):  {Tip: tipN(9), State: TidyTipMerged, Number: 15},
		tipN(10): {Tip: tipN(10), State: TidyTipUnknown},
	}
	if len(rows) != len(want) {
		t.Fatalf("%d rows", len(rows))
	}
	for i, r := range rows {
		if r != want[r.Tip] || (i > 0 && rows[i-1].Tip >= r.Tip) {
			t.Errorf("row %d: %+v, want %+v (rows are in ascending tip order)", i, r, want[r.Tip])
		}
	}
	// One repository read at open (not made by the fixture), one revalidation, one GET per tip.
	if len(srv.requests) != 1+len(tips) || s.Budget().Requests != 1+len(tips) {
		t.Errorf("%d requests: %v", len(srv.requests), srv.requests)
	}
	for _, r := range srv.requests[1:] {
		if !strings.HasSuffix(r, "/pulls?per_page=30&page=1") || !strings.Contains(r, "/commits/") {
			t.Errorf("request %s", r)
		}
	}
}

func TestReadTipPullRequestsFailuresProveNothing(t *testing.T) {
	big := "[" + strings.Repeat(" ", maxBytes) + "]"
	for name, tc := range map[string]struct {
		list   string
		status int
		want   error
		any    bool
	}{
		"server error":    {status: 500, any: true},
		"forbidden":       {status: 403, any: true},
		"too large":       {list: big, want: ErrTidyResponseTooLarge},
		"not a list":      {list: `{"a":1}`, any: true},
		"missing field":   {list: `[{"number":1}]`, any: true},
		"duplicate":       {list: "[" + tipPR(1, tipN(1), "main", true, "closed", 41, false) + "," + tipPR(1, tipN(1), "main", true, "closed", 41, false) + "]", any: true},
		"bad sha":         {list: strings.Replace("["+tipPR(1, tipN(1), "main", true, "closed", 41, false)+"]", tipN(1), "xyz", 1), any: true},
		"bad base branch": {list: "[" + tipPR(1, tipN(1), "bad..ref", true, "closed", 41, false) + "]", any: true},
		"31 items":        {list: "[" + strings.TrimSuffix(strings.Repeat(tipPR(1, tipN(1), "main", true, "closed", 41, false)+",", 31), ",") + "]", any: true},
	} {
		srv := &tipServer{t: t, lists: map[string]string{tipN(1): tc.list}, status: map[string]int{}}
		if tc.status != 0 {
			srv.status[tipN(1)] = tc.status
		}
		s := tidyFixtureSession(t, srv.handle)
		rows, err := s.ReadTipPullRequests(context.Background(), []string{tipN(1)})
		if err == nil || rows != nil || (tc.want != nil && !errors.Is(err, tc.want)) {
			t.Errorf("%s: %v %v", name, rows, err)
		}
	}
}

func TestReadTipPullRequestsRefusesBadSelections(t *testing.T) {
	srv := &tipServer{t: t, lists: map[string]string{}}
	s := tidyFixtureSession(t, srv.handle)
	many := []string{}
	for i := 1; i <= MaxTidyTips+1; i++ {
		many = append(many, tipN(i))
	}
	for name, tips := range map[string][]string{
		"none": nil, "too many": many, "duplicate": {tipN(1), tipN(1)}, "short": {"abc"}, "upper": {strings.ToUpper(tipN(255))}, "branch name": {"main"}, "path": {"../x"},
	} {
		if rows, err := s.ReadTipPullRequests(context.Background(), tips); err == nil || rows != nil {
			t.Errorf("%s: %v %v", name, rows, err)
		}
	}
	if len(srv.requests) != 0 {
		t.Errorf("a refused selection sent %v", srv.requests)
	}
	if !tidyTipPullsEndpoint("repos/o/r/commits/"+tipN(1)+"/pulls?per_page=30&page=1") || tidyTipPullsEndpoint("repos/o/r/commits/"+tipN(1)+"/pulls?per_page=100&page=1") || tidyTipPullsEndpoint("repos/o/r/commits/main/pulls?per_page=30&page=1") || tidyTipPullsEndpoint("repos/o/r/pulls/1") {
		t.Error("the endpoint set")
	}
}

func TestTidyRunBudgetContextCarriesTheBudget(t *testing.T) {
	b := NewTidyRunBudget(roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, `{}`), nil }), 5)
	if TidyRunBudgetFrom(context.Background()) != nil || TidyRunBudgetFrom(WithTidyRunBudget(context.Background(), b)) != b {
		t.Fatal("the budget travels in the context")
	}
}
