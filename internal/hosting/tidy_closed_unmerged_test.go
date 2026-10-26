// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func unmergedPRJSON(number int, head string) string {
	return fmt.Sprintf(`{"id":%d,"node_id":"PR_%d","number":%d,"state":"closed","merged":false,"merged_at":null,"draft":false,"head":{"sha":%q,"ref":"topic-%d","repo":{"id":41,"node_id":"R_repo","full_name":"owner/repo"}},"base":{"sha":%q,"ref":"main","repo":{"id":41,"node_id":"R_repo","full_name":"owner/repo"}},"merge_commit_sha":null}`, 100+number, number, number, head, number, strings.Repeat("b", 40))
}

// Spec 044 T005: a head whose only pull requests were closed without a merge reports their numbers
// and still proves nothing; a merged pull request of the same head wins; a head without any closed
// pull request reports none.
func TestDiscoveryReportsClosedUnmergedPullRequestsWithoutProvingAnything(t *testing.T) {
	heads := map[int]string{1: fmt.Sprintf("%040x", 1), 2: fmt.Sprintf("%040x", 2), 3: fmt.Sprintf("%040x", 2)}
	merged := map[int]bool{3: true}
	s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Path == "/repos/owner/repo":
			return response(200, tidyRepositoryJSON()), nil
		case req.URL.Path == "/repos/owner/repo/pulls":
			rows := []string{}
			for n := 1; n <= 3; n++ {
				rows = append(rows, unmergedPRJSON(n, heads[n]))
			}
			return response(200, "["+strings.Join(rows, ",")+"]"), nil
		}
		var n int
		if _, err := fmt.Sscanf(req.URL.Path, "/repos/owner/repo/pulls/%d", &n); err != nil {
			t.Fatalf("route %s", req.URL)
		}
		if merged[n] {
			return response(200, tidyPRJSON(n, heads[n], "main")), nil
		}
		return response(200, unmergedPRJSON(n, heads[n])), nil
	})
	targets := []string{heads[1], heads[2], fmt.Sprintf("%040x", 9)}
	rows, err := s.DiscoverPullRequests(context.Background(), targets, nil)
	if err != nil || len(rows) != 3 {
		t.Fatalf("%v %+v", err, rows)
	}
	if !reflect.DeepEqual(rows[0].ClosedUnmerged, []int{1}) || rows[0].Result != "no-merged-pr" || rows[0].PullRequest != nil {
		t.Errorf("closed unmerged only: %+v", rows[0])
	}
	if rows[1].Result != "github-merged-head" || rows[1].PullRequest == nil || rows[1].PullRequest.Number != 3 || !reflect.DeepEqual(rows[1].ClosedUnmerged, []int{2}) {
		t.Errorf("merged wins: %+v", rows[1])
	}
	if rows[2].Result != "no-merged-pr" || len(rows[2].ClosedUnmerged) != 0 {
		t.Errorf("no pull request: %+v", rows[2])
	}
}
