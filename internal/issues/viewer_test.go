// SPDX-License-Identifier: EUPL-1.2
package issues

import (
	"fmt"
	"slices"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
)

func assignedFixture(viewer string) *fakeForge {
	f := newForge()
	f.viewer = viewer
	for _, r := range []string{"a/x", "b/y", "c/z"} {
		f.pages[r] = []string{body(raw(1, withAssignees("WStein")), raw(2, withLabels("bug"), withAssignees("WStein")))}
	}
	return f
}

func TestViewerOKActivatesTheAssignedRuleAndIsReadOnce(t *testing.T) {
	f := assignedFixture("wstein")
	res := run(t, f, Options{Repos: []string{"a/x", "b/y", "c/z"}})
	if res.Viewer != (Viewer{State: ViewerOK, Login: "wstein"}) {
		t.Errorf("viewer %+v", res.Viewer)
	}
	if f.count("user") != 1 || f.callList()[0] != "user" {
		t.Errorf("calls %v", f.callList())
	}
	for _, r := range res.Rows {
		if r.Priority != Yellow || r.Reason != ReasonAssigned {
			t.Errorf("row %+v", r)
		}
	}
	if len(res.Errors) != 0 {
		t.Errorf("errors %+v", res.Errors)
	}
}

func TestViewerWithoutIdentityDisablesOnlyTheAssignedRule(t *testing.T) {
	for _, status := range []int{403, 404} {
		f := assignedFixture("")
		f.viewerErr = &hosting.WatchError{Code: hosting.CodeForbidden, Status: status, Message: "access was refused"}
		res := run(t, f, Options{Repos: []string{"a/x", "b/y"}})
		if res.Viewer != (Viewer{State: ViewerSkipped, Reason: ReasonNoIdentity}) {
			t.Errorf("%d: viewer %+v", status, res.Viewer)
		}
		if len(res.Errors) != 0 || res.Summary.ReposFailed != 0 {
			t.Errorf("%d: a skipped viewer is no failure: %+v", status, res.Errors)
		}
		if got := numbers(res.Rows); !slices.Equal(got, []string{"a/x#2", "b/y#2", "a/x#1", "b/y#1"}) {
			t.Errorf("%d: rows %v", status, got)
		}
		if res.Rows[0].Reason != ReasonBug || res.Rows[2].Reason != ReasonNone {
			t.Errorf("%d: rows %+v", status, res.Rows)
		}
		if f.count("user") != 1 {
			t.Errorf("%d: user read %d times", status, f.count("user"))
		}
	}
}

func TestViewerRateLimitStopsTheRun(t *testing.T) {
	f := assignedFixture("")
	f.viewerErr = &hosting.WatchError{Code: hosting.CodeRateLimited, Status: 403, Message: "rate limit reached"}
	res := run(t, f, Options{Repos: []string{"a/x", "b/y"}})
	if res.Viewer != (Viewer{State: ViewerError, Error: hosting.CodeRateLimited}) {
		t.Errorf("viewer %+v", res.Viewer)
	}
	if f.count("a/x") != 0 || f.count("b/y") != 0 || f.count("user") != 1 {
		t.Errorf("requests after the rate limit: %v", f.callList())
	}
	var codes []string
	for _, e := range res.Errors {
		codes = append(codes, e.Repo+":"+e.Code)
	}
	if !slices.Equal(codes, []string{":rate-limited", "a/x:rate-limited", "b/y:rate-limited"}) {
		t.Errorf("errors %v", codes)
	}
	if len(res.Rows) != 0 || res.Summary.ReposFailed != 2 {
		t.Errorf("rows %d summary %+v", len(res.Rows), res.Summary)
	}
}

func TestViewerFailureIsAnErrorButIssuesAreStillRead(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		code string
	}{
		"401":       {&hosting.WatchError{Code: hosting.CodeUnauthorized, Status: 401, Message: "authentication was refused"}, hosting.CodeUnauthorized},
		"500":       {&hosting.WatchError{Code: hosting.CodeHTTP5xx, Status: 500, Message: "server error"}, hosting.CodeHTTP5xx},
		"malformed": {&hosting.WatchError{Code: hosting.CodeMalformed, Message: "user response has no valid login"}, hosting.CodeMalformed},
		"network":   {fmt.Errorf("dial tcp: connection refused"), hosting.CodeNetwork},
	} {
		f := assignedFixture("")
		f.viewerErr = tc.err
		res := run(t, f, Options{Repos: []string{"a/x"}})
		if res.Viewer != (Viewer{State: ViewerError, Error: tc.code}) {
			t.Errorf("%s: viewer %+v", name, res.Viewer)
		}
		if len(res.Errors) != 1 || res.Errors[0].Repo != "" || res.Errors[0].Code != tc.code {
			t.Errorf("%s: errors %+v", name, res.Errors)
		}
		if len(res.Rows) != 2 || res.Rows[0].Reason != ReasonBug || f.count("user") != 1 {
			t.Errorf("%s: rows %+v calls %v", name, res.Rows, f.callList())
		}
	}
}

func TestNoRepositoryOnGitHubMeansNoViewerRequest(t *testing.T) {
	f := newForge()
	res := run(t, f, Options{})
	if res.Viewer != (Viewer{State: ViewerSkipped, Reason: ReasonNotGitHub}) || len(f.callList()) != 0 {
		t.Errorf("viewer %+v calls %v", res.Viewer, f.callList())
	}
}
