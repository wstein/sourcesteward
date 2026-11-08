// SPDX-License-Identifier: EUPL-1.2

package landcmd

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/land"
)

// fakeReader serves canned bodies by endpoint and records every endpoint read.
type fakeReader struct {
	bodies map[string]string
	status map[string]int
	log    []string
}

func (f *fakeReader) LandRead(_ context.Context, endpoint string) (hosting.WatchResponse, error) {
	f.log = append(f.log, endpoint)
	if st := f.status[endpoint]; st != 0 {
		return hosting.WatchResponse{}, &hosting.WatchError{Code: hosting.CodeForbidden, Status: st, Message: "refused"}
	}
	body, ok := f.bodies[endpoint]
	if !ok {
		return hosting.WatchResponse{}, &hosting.WatchError{Code: hosting.CodeForbidden, Status: 404, Message: "not found"}
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(body)}, nil
}

func newForge(f *fakeReader) *GitHubForge { return NewGitHubForge(f, "o/r") }

func TestRepositoryDecode(t *testing.T) {
	f := &fakeReader{bodies: map[string]string{"repos/o/r": `{"default_branch":"main","delete_branch_on_merge":true}`}}
	got, err := newForge(f).Repository(context.Background())
	if err != nil || got.DefaultBranch != "main" || got.DeleteBranchOnMerge == nil || !*got.DeleteBranchOnMerge {
		t.Fatalf("%+v %v", got, err)
	}
	f.bodies["repos/o/r"] = `{"default_branch":"main"}`
	got, _ = newForge(f).Repository(context.Background())
	if got.DeleteBranchOnMerge != nil {
		t.Fatal("an absent delete_branch_on_merge is unknown, not false")
	}
}

func TestPullDecode(t *testing.T) {
	f := &fakeReader{bodies: map[string]string{"repos/o/r/pulls/12": `{"number":12,"state":"open","merged":false,"draft":true,
 "head":{"sha":"` + strings.Repeat("a", 40) + `","ref":"feat","repo":{"full_name":"o/r"}},"base":{"ref":"main","repo":{"full_name":"o/r"}},
 "mergeable":null,"mergeable_state":"unknown","rebaseable":true,"merge_commit_sha":null}`}}
	p, err := newForge(f).Pull(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	if p.Number != 12 || !p.Draft || p.Mergeable != nil || p.Rebaseable == nil || !*p.Rebaseable || p.HeadRepo != "o/r" || p.BaseRef != "main" || p.HeadRef != "feat" || p.HeadSHA != strings.Repeat("a", 40) {
		t.Fatalf("%+v", p)
	}
	// a fork has no head repo name in common; a deleted fork has a null head repo
	f.bodies["repos/o/r/pulls/13"] = `{"number":13,"state":"open","head":{"sha":"` + strings.Repeat("b", 40) + `","ref":"x","repo":null},"base":{"ref":"main","repo":{"full_name":"o/r"}}}`
	p, err = newForge(f).Pull(context.Background(), 13)
	if err != nil || p.HeadRepo != "" {
		t.Fatalf("%+v %v", p, err)
	}
	f.bodies["repos/o/r/pulls/14"] = `{"number":15}`
	if _, err = newForge(f).Pull(context.Background(), 14); err == nil {
		t.Fatal("a body for another pull request must be refused")
	}
	f.bodies["repos/o/r/pulls/16"] = `not json`
	if _, err = newForge(f).Pull(context.Background(), 16); err == nil {
		t.Fatal("malformed body accepted")
	}
}

func TestOpenPullsPagination(t *testing.T) {
	page := func(n int) string {
		var parts []string
		for i := 0; i < n; i++ {
			parts = append(parts, fmt.Sprintf(`{"number":%d,"base":{"ref":"main"},"head":{"ref":"h%d"}}`, i+1, i))
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	f := &fakeReader{bodies: map[string]string{
		"repos/o/r/pulls?state=open&per_page=100&page=1": page(100),
		"repos/o/r/pulls?state=open&per_page=100&page=2": page(3),
	}}
	got, trunc, err := newForge(f).OpenPulls(context.Background())
	if err != nil || trunc || len(got) != 103 {
		t.Fatalf("%d %v %v", len(got), trunc, err)
	}
	f = &fakeReader{bodies: map[string]string{}}
	for p := 1; p <= 5; p++ {
		f.bodies[fmt.Sprintf("repos/o/r/pulls?state=open&per_page=100&page=%d", p)] = page(100)
	}
	_, trunc, err = newForge(f).OpenPulls(context.Background())
	if err != nil || !trunc {
		t.Fatalf("five full pages must read as truncated: %v %v", trunc, err)
	}
}

func TestChecksMapping(t *testing.T) {
	sha := strings.Repeat("c", 40)
	runs := "repos/o/r/commits/" + sha + "/check-runs?per_page=100&page=1"
	stat := "repos/o/r/commits/" + sha + "/statuses?per_page=100&page=1"
	f := &fakeReader{bodies: map[string]string{
		runs: `{"total_count":5,"check_runs":[{"name":"a","status":"completed","conclusion":"success"},{"name":"b","status":"in_progress","conclusion":null},
 {"name":"c","status":"completed","conclusion":"failure"},{"name":"d","status":"completed","conclusion":"skipped"},{"name":"e","status":"completed","conclusion":"cancelled"}]}`,
		stat: `[{"context":"s1","state":"success"},{"context":"s1","state":"failure"},{"context":"s2","state":"error"},{"context":"s3","state":"pending"}]`,
	}}
	got, err := newForge(f).Checks(context.Background(), sha)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]land.CheckState{"a": land.CheckSuccess, "b": land.CheckPending, "c": land.CheckFailure, "d": land.CheckSuccess, "e": land.CheckFailure,
		"s1": land.CheckSuccess, "s2": land.CheckFailure, "s3": land.CheckPending}
	for k, v := range want {
		if got.Contexts[k] != v {
			t.Errorf("%s: %q want %q", k, got.Contexts[k], v)
		}
	}
	if len(got.Contexts) != len(want) {
		t.Errorf("%v", got.Contexts)
	}
}

func TestReviewsAndEvents(t *testing.T) {
	f := &fakeReader{bodies: map[string]string{
		"repos/o/r/pulls/12/reviews?per_page=100&page=1": `[{"user":{"login":"rev"},"state":"APPROVED","commit_id":"` + strings.Repeat("a", 40) + `"}]`,
		"repos/o/r/issues/12/events?per_page=100&page=1": `[{"event":"head_ref_force_pushed","actor":{"login":"github-actions[bot]"}},{"event":"labeled","actor":null}]`,
		"user": `{"login":"wstein"}`,
	}}
	g := newForge(f)
	rv, err := g.Reviews(context.Background(), 12)
	if err != nil || len(rv) != 1 || rv[0].Reviewer != "rev" || rv[0].State != "APPROVED" {
		t.Fatalf("%+v %v", rv, err)
	}
	ev, err := g.Events(context.Background(), 12)
	if err != nil || len(ev) != 2 || ev[0].Actor != "github-actions[bot]" || ev[1].Actor != "" {
		t.Fatalf("%+v %v", ev, err)
	}
	if login, err := g.Viewer(context.Background()); err != nil || login != "wstein" {
		t.Fatalf("%q %v", login, err)
	}
}

func TestRequiredUnionAndReadability(t *testing.T) {
	rules := "repos/o/r/rules/branches/main?per_page=100&page=1"
	prot := "repos/o/r/branches/main/protection"
	f := &fakeReader{
		bodies: map[string]string{
			rules: `[{"type":"required_status_checks","ruleset_id":1,"parameters":{"required_status_checks":[{"context":"ci/all"},{"context":"lint"}]}},{"type":"deletion","ruleset_id":1}]`,
			prot:  `{"required_status_checks":{"contexts":["legacy"],"checks":[{"context":"lint"}]}}`,
		},
	}
	got, err := newForge(f).Required(context.Background(), "main")
	if err != nil || !got.Readable || strings.Join(got.Contexts, ",") != "ci/all,legacy,lint" {
		t.Fatalf("%+v %v", got, err)
	}
	f.status = map[string]int{prot: 404}
	got, _ = newForge(f).Required(context.Background(), "main")
	if !got.Readable || strings.Join(got.Contexts, ",") != "ci/all,lint" {
		t.Fatalf("a 404 protection means none: %+v", got)
	}
	f.status = map[string]int{rules: 403}
	if got, _ = newForge(f).Required(context.Background(), "main"); got.Readable {
		t.Fatal("a 403 on the rules must make the required checks unreadable")
	}
	f.status = map[string]int{prot: 403}
	if got, _ = newForge(f).Required(context.Background(), "main"); got.Readable {
		t.Fatal("a 403 on the protection must make the required checks unreadable")
	}
	f.status = map[string]int{prot: 404}
	f.bodies[rules] = `[]`
	got, _ = newForge(f).Required(context.Background(), "main")
	if !got.Readable || len(got.Contexts) != 0 {
		t.Fatalf("both readable and empty: %+v", got)
	}
}

func TestRulesetsShape(t *testing.T) {
	rules := "repos/o/r/rules/branches/main?per_page=100&page=1"
	prot := "repos/o/r/branches/main/protection"
	f := &fakeReader{
		bodies: map[string]string{
			rules:                  `[{"type":"pull_request","ruleset_id":7},{"type":"required_status_checks","ruleset_id":7},{"type":"deletion","ruleset_id":8}]`,
			"repos/o/r/rulesets/7": `{"id":7,"bypass_actors":[{"actor_type":"RepositoryRole","actor_id":5,"bypass_mode":"pull_request"}]}`,
			"repos/o/r/rulesets/8": `{"id":8,"bypass_actors":[]}`,
		},
		status: map[string]int{prot: 404},
	}
	got, err := newForge(f).Rulesets(context.Background(), "main")
	if err != nil || !got.Readable || got.Protection != "none" {
		t.Fatalf("%+v %v", got, err)
	}
	if strings.Join(got.RuleTypes, ",") != "deletion,pull_request,required_status_checks" || strings.Join(got.BypassModes, ",") != "pull_request" {
		t.Fatalf("%+v", got)
	}
	f.status = map[string]int{prot: 404, "repos/o/r/rulesets/7": 403}
	if got, _ = newForge(f).Rulesets(context.Background(), "main"); got.Readable {
		t.Fatal("an unreadable ruleset detail must make the shape unreadable")
	}
	f.status = map[string]int{prot: 403}
	if got, _ = newForge(f).Rulesets(context.Background(), "main"); got.Protection != "unreadable" || got.Readable {
		t.Fatalf("protection 403: %+v", got)
	}
	f.status = map[string]int{rules: 403}
	if got, _ = newForge(f).Rulesets(context.Background(), "main"); got.Readable {
		t.Fatal("rules 403 must be unreadable")
	}
}

func TestAdapterIsReadOnly(t *testing.T) {
	// every endpoint the adapter builds satisfies the hosting land guard; none is a write shape
	f := &fakeReader{bodies: map[string]string{}, status: map[string]int{}}
	g := newForge(f)
	ctx := context.Background()
	g.Repository(ctx)
	g.Pull(ctx, 1)
	g.OpenPulls(ctx)
	g.Checks(ctx, strings.Repeat("a", 40))
	g.Reviews(ctx, 1)
	g.Events(ctx, 1)
	g.Required(ctx, "main")
	g.Rulesets(ctx, "main")
	g.Viewer(ctx)
	if len(f.log) == 0 {
		t.Fatal("no reads")
	}
	for _, e := range f.log {
		if !hosting.LandEndpointAllowed(e) {
			t.Errorf("endpoint %q is outside the land guard", e)
		}
	}
}

func fullRulesPage(n int) string {
	var parts []string
	for i := 0; i < n; i++ {
		parts = append(parts, `{"type":"deletion","ruleset_id":1}`)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func TestRulesPaginationAndTruncationFailClosed(t *testing.T) {
	r1 := "repos/o/r/rules/branches/main?per_page=100&page=1"
	r2 := "repos/o/r/rules/branches/main?per_page=100&page=2"
	r3 := "repos/o/r/rules/branches/main?per_page=100&page=3"
	prot := "repos/o/r/branches/main/protection"
	// the required_status_checks rule sits on page 2: it must be found
	f := &fakeReader{bodies: map[string]string{
		r1: fullRulesPage(100),
		r2: `[{"type":"required_status_checks","ruleset_id":2,"parameters":{"required_status_checks":[{"context":"late"}]}}]`,
	}, status: map[string]int{prot: 404}}
	got, err := newForge(f).Required(context.Background(), "main")
	if err != nil || !got.Readable || strings.Join(got.Contexts, ",") != "late" {
		t.Fatalf("page 2 rule lost: %+v %v", got, err)
	}
	// three full pages: the list may continue, so it is unreadable, not complete
	f.bodies = map[string]string{r1: fullRulesPage(100), r2: fullRulesPage(100), r3: fullRulesPage(100)}
	if got, _ = newForge(f).Required(context.Background(), "main"); got.Readable {
		t.Fatal("a truncated rule list must make the required checks unreadable")
	}
	if rs, _ := newForge(f).Rulesets(context.Background(), "main"); rs.Readable {
		t.Fatal("a truncated rule list must make the shape unreadable")
	}
}

func TestBypassActorsAbsentIsUnreadableNotEmpty(t *testing.T) {
	rules := "repos/o/r/rules/branches/main?per_page=100&page=1"
	prot := "repos/o/r/branches/main/protection"
	f := &fakeReader{bodies: map[string]string{
		rules:                  `[{"type":"pull_request","ruleset_id":7}]`,
		"repos/o/r/rulesets/7": `{"id":7,"name":"main"}`, // no bypass_actors key: a read without admin rights
	}, status: map[string]int{prot: 404}}
	if rs, _ := newForge(f).Rulesets(context.Background(), "main"); rs.Readable {
		t.Fatalf("an absent bypass_actors must not read as an empty list: %+v", rs)
	}
	f.bodies["repos/o/r/rulesets/7"] = `{"id":7,"bypass_actors":[]}`
	rs, _ := newForge(f).Rulesets(context.Background(), "main")
	if !rs.Readable || len(rs.BypassModes) != 0 {
		t.Fatalf("an empty list is readable and empty: %+v", rs)
	}
}

func TestEventsPaginationAndTruncation(t *testing.T) {
	ev := func(page int, n int, name string) string {
		var parts []string
		for i := 0; i < n; i++ {
			parts = append(parts, `{"event":"`+name+`","actor":{"login":"a"}}`)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	e := func(p int) string { return fmt.Sprintf("repos/o/r/issues/12/events?per_page=100&page=%d", p) }
	f := &fakeReader{bodies: map[string]string{e(1): ev(1, 100, "labeled"), e(2): ev(2, 1, "head_ref_force_pushed")}}
	got, err := newForge(f).Events(context.Background(), 12)
	if err != nil || len(got) != 101 || got[100].Name != "head_ref_force_pushed" {
		t.Fatalf("a late event on page 2 must be read: %d %v", len(got), err)
	}
	f.bodies = map[string]string{e(1): ev(1, 100, "labeled"), e(2): ev(2, 100, "labeled"), e(3): ev(3, 100, "labeled")}
	if _, err := newForge(f).Events(context.Background(), 12); err == nil {
		t.Fatal("a truncated event list must be an error, not a complete list")
	}
}

// A token without administration read answers 403 on the classic protection. The branch endpoint
// is readable with read access and says whether classic protection exists (Spec 043 FR-090).
func TestProtectionFallsBackToTheBranchEndpointOn403(t *testing.T) {
	rules := "repos/o/r/rules/branches/main?per_page=100&page=1"
	prot := "repos/o/r/branches/main/protection"
	br := "repos/o/r/branches/main"
	ruleBody := `[{"type":"required_status_checks","ruleset_id":1,"parameters":{"required_status_checks":[{"context":"ci/all"}]}}]`
	cases := []struct {
		name     string
		branch   string // body of the branch endpoint, "" for absent (404)
		status   map[string]int
		readable bool
		state    string
		contexts string
	}{
		{"classic protection off", `{"protected":true,"protection":{"enabled":false,"required_status_checks":{"contexts":[],"checks":[],"enforcement_level":"off"}}}`, nil, true, "none", "ci/all"},
		{"not protected at all", `{"protected":false}`, nil, true, "none", "ci/all"},
		{"classic protection on", `{"protected":true,"protection":{"enabled":true,"required_status_checks":{"contexts":["legacy"],"checks":[{"context":"lint"}],"enforcement_level":"everyone"}}}`, nil, true, "present", "ci/all,legacy,lint"},
		{"protection object missing", `{"protected":true}`, nil, false, "unreadable", ""},
		{"protection object empty", `{"protected":true,"protection":{}}`, nil, false, "unreadable", ""},
		{"whole body empty object", `{}`, nil, false, "unreadable", ""},
		{"enabled without required checks object", `{"protected":true,"protection":{"enabled":true}}`, nil, false, "unreadable", ""},
		{"enabled with null required checks", `{"protected":true,"protection":{"enabled":true,"required_status_checks":null}}`, nil, false, "unreadable", ""},
		{"branch endpoint 403", "", map[string]int{br: 403}, false, "unreadable", ""},
		{"branch endpoint 404", "", nil, false, "unreadable", ""},
		{"branch endpoint malformed", `not json`, nil, false, "unreadable", ""},
		{"protection 500 does not fall back", `{"protected":false}`, map[string]int{prot: 500}, false, "unreadable", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeReader{bodies: map[string]string{rules: ruleBody}, status: map[string]int{prot: 403}}
			if c.branch != "" {
				f.bodies[br] = c.branch
			}
			for k, v := range c.status {
				f.status[k] = v
			}
			got, _ := newForge(f).Required(context.Background(), "main")
			if got.Readable != c.readable || strings.Join(got.Contexts, ",") != c.contexts {
				t.Fatalf("required: %+v", got)
			}
			f.bodies["repos/o/r/rulesets/1"] = `{"id":1,"bypass_actors":[]}`
			rs, _ := newForge(f).Rulesets(context.Background(), "main")
			if rs.Protection != c.state || rs.Readable != c.readable {
				t.Fatalf("rulesets: %+v", rs)
			}
			for _, e := range f.log {
				if !hosting.LandEndpointAllowed(e) {
					t.Errorf("endpoint %q is outside the land guard", e)
				}
			}
		})
	}
}

// 404 on classic protection stays "none" without a second read (observed on wstein/sourcesteward).
func TestProtection404NeedsNoFallbackRead(t *testing.T) {
	rules := "repos/o/r/rules/branches/main?per_page=100&page=1"
	f := &fakeReader{bodies: map[string]string{rules: `[]`}, status: map[string]int{"repos/o/r/branches/main/protection": 404}}
	got, _ := newForge(f).Required(context.Background(), "main")
	if !got.Readable {
		t.Fatalf("%+v", got)
	}
	for _, e := range f.log {
		if e == "repos/o/r/branches/main" {
			t.Fatal("the branch endpoint was read although the protection answered 404")
		}
	}
}
