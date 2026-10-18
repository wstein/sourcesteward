// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const readySHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const readyBaseSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const readyRefJSON = `{"sha":"` + readySHA + `","ref":"topic","repo":{"id":1,"full_name":"o/r"}}`
const readyPRJSON = `{"id":11,"number":7,"state":"open","merged":false,"draft":true,"head":` + readyRefJSON + `,"base":` + readyRefJSON + `,"changed_files":1,"mergeable":true,"rebaseable":null,"mergeable_state":"clean"}`

func readyFixture(t *testing.T, fn func(*http.Request) (int, string)) *readySources {
	t.Helper()
	client, err := newSDKClient("github.com", "fixture-token", t.TempDir(), roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
			t.Error("API pin changed")
		}
		if r.Method != "GET" {
			t.Error("write attempted")
		}
		status, body := fn(r)
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	return newReadySources(context.Background(), &ghAPI{host: "github.com", client: client}, Repository{Host: "github.com", Owner: "o", Name: "r"})
}
func TestReadyFixedReaders(t *testing.T) {
	fixtures := map[string]string{
		"/repos/o/r/pulls/7":                                 readyPRJSON,
		"/repos/o/r":                                         `{"id":1,"full_name":"o/r","default_branch":"main","allow_rebase_merge":true,"allow_merge_commit":false,"allow_squash_merge":true}`,
		"/repos/o/r/branches/main":                           `{"name":"main","commit":{"sha":"` + readySHA + `"},"protected":false}`,
		"/repos/o/r/branches/main/protection":                `{"required_status_checks":{"strict":true,"contexts":["ci"],"checks":[{"context":"ci","app_id":123}]},"required_pull_request_reviews":null}`,
		"/repos/o/r/rules/branches/main?per_page=100&page=1": `[{"type":"required_linear_history","ruleset_id":1,"ruleset_source_type":"Repository","ruleset_source":"o/r"}]`,
		"/repos/o/r/compare/" + readyBaseSHA + "..." + readySHA + "?per_page=1&page=2":  `{"base_commit":{"sha":"` + readyBaseSHA + `"},"merge_base_commit":{"sha":"` + readyBaseSHA + `"},"status":"ahead","ahead_by":1,"behind_by":0,"total_commits":1,"commits":[]}`,
		"/repos/o/r/commits/" + readySHA + "/check-runs?filter=all&per_page=100&page=1": `{"total_count":1,"check_runs":[{"id":2,"name":"ci","status":"completed","conclusion":"success","head_sha":"` + readySHA + `","app":{"id":123},"output":{"summary":"envelope"}}]}`,
		"/repos/o/r/commits/" + readySHA + "/statuses?per_page=100&page=1":              `[{"id":3,"context":"review","state":"success","creator":{"id":42},"created_at":"2026-10-18T00:00:00Z","updated_at":"2026-10-18T00:00:00Z","target_url":"https://github.com/o/r/issues/7#issuecomment-9","description":null}]`,
		"/repos/o/r/pulls?state=open&per_page=100&page=1":                               `[` + readyPRJSON + `]`,
		"/repos/o/r/pulls/7/files?per_page=100&page=1":                                  `[{"filename":"a.go","status":"renamed","previous_filename":"b.go"}]`,
		"/repos/o/r/issues/comments/9":                                                  `{"id":9,"body":"{}","user":{"id":42},"issue_url":"https://api.github.com/repos/o/r/issues/7","updated_at":"2026-10-18T00:00:00Z"}`,
		"/repos/o/r/contents/.stw/ready-policy.json?ref=" + readySHA:                    `{"type":"file","encoding":"base64","size":2,"content":"e30="}`,
		"/repos/o/r/environments/production/variables/STW_READY_BASELINE":               `{"name":"STW_READY_BASELINE","value":"{}"}`,
	}
	seen := map[string]bool{}
	s := readyFixture(t, func(r *http.Request) (int, string) {
		key := r.URL.RequestURI()
		body, ok := fixtures[key]
		if !ok {
			t.Errorf("unexpected endpoint %s", key)
		}
		seen[key] = true
		return 200, body
	})
	assert := func(e readySource) {
		t.Helper()
		if e.Outcome != readyOK || !e.Complete || e.Host != "github.com" {
			t.Errorf("incomplete source: %+v", e)
		}
	}
	p := s.pullRequest(7)
	assert(p.Source)
	if p.Value.Head.RepositoryID == nil || *p.Value.Head.RepositoryID != 1 || p.Value.Rebaseable != nil {
		t.Fatal("PR identity/nullability lost")
	}
	assert(s.repository().Source)
	assert(s.branch("main").Source)
	assert(s.protection("main").Source)
	assert(s.rules("main").Source)
	assert(s.compare(readyBaseSHA, readySHA).Source)
	checks := s.checks(readySHA)
	assert(checks.Source)
	if len(checks.Value) != 1 || checks.Value[0].AppID != 123 || checks.Value[0].Summary == nil || *checks.Value[0].Summary != "envelope" {
		t.Fatal("check source metadata lost")
	}
	statuses := s.statuses(readySHA)
	assert(statuses.Source)
	if len(statuses.Value) != 1 || statuses.Value[0].CreatorID != 42 {
		t.Fatal("numeric creator lost")
	}
	prs := s.openPullRequests()
	assert(prs.Source)
	if len(prs.Value) != 1 || !prs.Value[0].Draft {
		t.Fatal("association set filtered")
	}
	assert(s.files(7, 1).Source)
	assert(s.report(9, 7).Source)
	policy := s.policy(readySHA)
	assert(policy.Source)
	if string(policy.Value.Raw) != "{}" {
		t.Fatal("policy bytes changed")
	}
	assert(s.activation(readyActivationChannel{Profile: "app", Environment: "production", Accepted: true}).Source)
	if len(seen) != len(fixtures) || s.budget.requests != len(fixtures) {
		t.Fatalf("request accounting %d/%d", s.budget.requests, len(fixtures))
	}
}
func TestReadyHTTPOutcomes(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   readyOutcome
	}{{401, readyDenied}, {403, readyDenied}, {404, readyNotFoundAmbiguous}, {422, readyUnsupported}, {500, readyTransport}, {302, readyTransport}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			s := readyFixture(t, func(r *http.Request) (int, string) { return tc.status, `secret-token-raw-body` })
			got := s.repository()
			if got.Source.Outcome != tc.want || got.Source.Complete {
				t.Fatalf("%+v", got.Source)
			}
			if strings.Contains(fmt.Sprint(got.Source), "secret") {
				t.Fatal("body disclosed")
			}
		})
	}
	s := readyFixture(t, func(*http.Request) (int, string) { return 200, `{}` })
	if got := s.protection("main"); got.Source.Outcome == readyAbsentProven {
		t.Fatal("absence inferred")
	}
}
func TestReadyRefusals(t *testing.T) {
	s := readyFixture(t, func(*http.Request) (int, string) { t.Fatal("invalid input reached network"); return 200, `{}` })
	for _, ref := range []string{"../main", "main/protection/x", "main?x", "a%2fb", "a..b", "-main"} {
		if s.branch(ref).Source.Outcome != readyUnsupported {
			t.Errorf("accepted %q", ref)
		}
	}
	if s.checks(strings.ToUpper(readySHA)).Source.Outcome != readyMalformed {
		t.Fatal("uppercase SHA accepted")
	}
	if s.activation(readyActivationChannel{Profile: "app", Environment: "prod", Accepted: false}).Source.Outcome != readyUnsupported {
		t.Fatal("unaccepted activation accepted")
	}
	if s.activation(readyActivationChannel{Profile: "required-workflow", Environment: "prod", Accepted: true}).Source.Outcome != readyUnsupported {
		t.Fatal("unspecified profile accepted")
	}
	if s.files(7, 1001).Source.Outcome != readyLimit {
		t.Fatal("file bound not enforced before read")
	}
}
func TestReadyMalformedCollections(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `[{}]`, `[` + readyPRJSON + `,` + readyPRJSON + `]`} {
		s := readyFixture(t, func(*http.Request) (int, string) { return 200, body })
		if got := s.openPullRequests(); got.Source.Outcome != readyMalformed || got.Source.Complete {
			t.Fatalf("%s: %+v", body, got.Source)
		}
	}
}
func TestReadyBudgetsAndCancellation(t *testing.T) {
	s := readyFixture(t, func(*http.Request) (int, string) { return 200, `{}` })
	s.budget.requests = 80
	if s.repository().Source.Outcome != readyLimit || s.budget.requests != 80 {
		t.Fatal("request budget escaped")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.ctx = ctx
	if s.repository().Source.Outcome != readyCanceled {
		t.Fatal("cancellation lost")
	}
}

func TestReadyPaginationProof(t *testing.T) {
	t.Run("all-open-PRs-before-filter", func(t *testing.T) {
		calls := 0
		s := readyFixture(t, func(r *http.Request) (int, string) {
			calls++
			if calls == 1 {
				rows := make([]string, 100)
				for i := range rows {
					rows[i] = strings.ReplaceAll(strings.Replace(readyPRJSON, `"id":11`, fmt.Sprintf(`"id":%d`, i+1), 1), `"number":7`, fmt.Sprintf(`"number":%d`, i+1))
				}
				return 200, "[" + strings.Join(rows, ",") + "]"
			}
			return 200, `[]`
		})
		v := s.openPullRequests()
		if v.Source.Outcome != readyOK || !v.Source.Complete || len(v.Value) != 100 || v.Source.Requests != 2 {
			t.Fatalf("%+v", v.Source)
		}
	})
	t.Run("ten-full-pages-incomplete", func(t *testing.T) {
		page := 0
		s := readyFixture(t, func(r *http.Request) (int, string) {
			rows := make([]string, 100)
			for i := range rows {
				rows[i] = strings.Replace(readyPRJSON, `"id":11`, fmt.Sprintf(`"id":%d`, page*100+i+1), 1)
			}
			page++
			return 200, "[" + strings.Join(rows, ",") + "]"
		})
		got := s.openPullRequests()
		if got.Source.Outcome != readyLimit || got.Source.Complete || len(got.Value) != 0 || page != 10 {
			t.Fatalf("%+v page=%d", got.Source, page)
		}
	})
	t.Run("check-total-changes", func(t *testing.T) {
		page := 0
		s := readyFixture(t, func(r *http.Request) (int, string) {
			page++
			if page == 2 {
				return 200, `{"total_count":101,"check_runs":[]}`
			}
			row := `{"id":%d,"name":"ci","status":"completed","conclusion":"success","head_sha":"` + readySHA + `","app":{"id":123},"output":{"summary":null}}`
			rows := make([]string, 100)
			for i := range rows {
				rows[i] = fmt.Sprintf(row, i+1)
			}
			return 200, `{"total_count":200,"check_runs":[` + strings.Join(rows, ",") + `]}`
		})
		if got := s.checks(readySHA); got.Source.Outcome != readyMalformed || got.Source.Complete {
			t.Fatalf("%+v", got.Source)
		}
	})
	t.Run("file-count-mismatch", func(t *testing.T) {
		s := readyFixture(t, func(*http.Request) (int, string) { return 200, `[]` })
		if got := s.files(7, 1); got.Source.Outcome != readyMalformed || got.Source.Complete {
			t.Fatalf("%+v", got.Source)
		}
	})
	t.Run("normalized-collection-limit", func(t *testing.T) {
		page := 0
		s := readyFixture(t, func(*http.Request) (int, string) {
			page++
			rows := make([]string, 100)
			for i := range rows {
				rows[i] = fmt.Sprintf(`{"filename":"%s%d","status":"added"}`, strings.Repeat("a", 1800), page*100+i)
			}
			return 200, "[" + strings.Join(rows, ",") + "]"
		})
		if got := s.files(7, 1000); got.Source.Outcome != readyLimit || s.budget.requests > 6 {
			t.Fatalf("%+v requests=%d", got.Source, s.budget.requests)
		}
	})
}
func TestReadySampledCIBudget(t *testing.T) {
	s := readyFixture(t, func(*http.Request) (int, string) {
		return 200, `[{"id":3,"context":"review","state":"success","creator":{"id":42},"created_at":"2026-10-18T00:00:00Z","updated_at":"2026-10-18T00:00:00Z","target_url":null,"description":null}]`
	})
	s.budget.ciRows = 999
	if got := s.statuses(readySHA); got.Source.Outcome != readyOK {
		t.Fatalf("initial %+v", got.Source)
	}
	if got := s.statuses(readySHA); got.Source.Outcome != readyOK {
		t.Fatalf("resample wrongly double-counted: %+v", got.Source)
	}
}
func TestReadySourceMetadataRequired(t *testing.T) {
	tests := []struct {
		name, body string
		run        func(*readySources) readySource
	}{
		{"check-app", `{"total_count":1,"check_runs":[{"id":2,"name":"ci","status":"completed","conclusion":"success","head_sha":"` + readySHA + `","output":{"summary":null}}]}`, func(s *readySources) readySource { return s.checks(readySHA).Source }},
		{"status-creator", `[{"id":3,"context":"review","state":"success","created_at":"2026-10-18T00:00:00Z","updated_at":"2026-10-18T00:00:00Z","target_url":null,"description":null}]`, func(s *readySources) readySource { return s.statuses(readySHA).Source }},
		{"status-creator-overflow", `[{"id":3,"context":"review","state":"success","creator":{"id":9223372036854775808},"created_at":"2026-10-18T00:00:00Z","updated_at":"2026-10-18T00:00:00Z","target_url":null,"description":null}]`, func(s *readySources) readySource { return s.statuses(readySHA).Source }},
		{"policy-symlink", `{"type":"symlink","encoding":"base64","size":2,"content":"e30="}`, func(s *readySources) readySource { return s.policy(readySHA).Source }},
		{"policy-size", `{"type":"file","encoding":"base64","size":3,"content":"e30="}`, func(s *readySources) readySource { return s.policy(readySHA).Source }},
		{"policy-base64", `{"type":"file","encoding":"base64","size":2,"content":"e31="}`, func(s *readySources) readySource { return s.policy(readySHA).Source }},
		{"activation-wrong-name", `{"name":"OTHER","value":"{}"}`, func(s *readySources) readySource {
			return s.activation(readyActivationChannel{Profile: "app", Environment: "prod", Accepted: true}).Source
		}},
		{"duplicate-key", `{"id":1,"id":2}`, func(s *readySources) readySource { return s.repository().Source }},
		{"report-binding", `{"id":9,"body":"{}","user":{"id":42},"issue_url":"https://api.github.com/repos/o/r/issues/8","updated_at":"2026-10-18T00:00:00Z"}`, func(s *readySources) readySource { return s.report(9, 7).Source }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := readyFixture(t, func(*http.Request) (int, string) { return 200, tc.body })
			if got := tc.run(s); got.Outcome != readyMalformed || got.Complete {
				t.Fatalf("%+v", got)
			}
		})
	}
}
func TestReadyRedirectAndTransportGuards(t *testing.T) {
	calls := 0
	client, e := newSDKClient("github.com", "fixture-token", t.TempDir(), roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://api.github.com/repos/o/r"}}, Body: io.NopCloser(strings.NewReader("secret")), Request: r}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	s := newReadySources(context.Background(), &ghAPI{host: "github.com", client: client}, Repository{Host: "github.com", Owner: "o", Name: "r"})
	if got := s.repository(); got.Source.Outcome != readyTransport || calls != 1 {
		t.Fatalf("%+v calls=%d", got.Source, calls)
	}
	for _, ep := range []string{"repos/o/r/pulls?per_page=100&state=open&page=1", "repos/o/r/commits/" + readySHA + "/check-runs?filter=latest&per_page=100&page=1", "repos/o/r/rules/branches/main?per_page=100&page=11", "repos/o/r/environments/prod/variables/OTHER", "repos/o/r/contents/.stw/ready-policy.json?ref=main", "repos/o/r/branches/main/protection/protection"} {
		if readyEndpoint(ep) {
			t.Errorf("endpoint accepted %s", ep)
		}
	}
	for _, ref := range []string{"release/v1", "a_b/x.y"} {
		if !readyBaseRef(ref) {
			t.Errorf("safe slash ref refused %s", ref)
		}
	}
}

func TestReadyPolicyRepositorySharesBudget(t *testing.T) {
	s := readyFixture(t, func(*http.Request) (int, string) { t.Fatal("exhausted shared budget reached HTTP"); return 200, `{}` })
	other := s.forRepository(Repository{Host: "github.com", Owner: "policy", Name: "baseline"})
	s.budget.requests = 80
	if got := other.policy(readySHA); got.Source.Outcome != readyLimit || got.Source.Repository != "policy/baseline" || other.budget != s.budget {
		t.Fatalf("separate policy budget: %+v", got.Source)
	}
}
func TestReadyResponseBounds(t *testing.T) {
	for _, mode := range []string{"body", "headers"} {
		t.Run(mode, func(t *testing.T) {
			client, e := newSDKClient("github.com", "fixture-token", t.TempDir(), roundTripFunc(func(r *http.Request) (*http.Response, error) {
				res := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}
				if mode == "body" {
					res.ContentLength = maxBytes + 1
				} else {
					res.Header.Set("X-Large", strings.Repeat("x", 32769))
				}
				return res, nil
			}))
			if e != nil {
				t.Fatal(e)
			}
			s := newReadySources(context.Background(), &ghAPI{host: "github.com", client: client}, Repository{Host: "github.com", Owner: "o", Name: "r"})
			if got := s.repository(); got.Source.Outcome != readyLimit || got.Source.Complete {
				t.Fatalf("%+v", got.Source)
			}
		})
	}
}

type readyTestBody struct{ read func([]byte) (int, error) }

func (b readyTestBody) Read(p []byte) (int, error) { return b.read(p) }
func (b readyTestBody) Close() error               { return nil }
func TestReadyReadFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		cancel, deadline, success bool
		failure                   error
		want                      readyOutcome
	}{
		{name: "parent-canceled-body", cancel: true, failure: context.Canceled, want: readyCanceled},
		{name: "parent-canceled-successful-body", cancel: true, success: true, want: readyCanceled},
		{name: "truncated-body", failure: io.ErrUnexpectedEOF, want: readyTransport},
		{name: "body-cap", failure: errBodyLimit, want: readyLimit},
		{name: "own-deadline-body", deadline: true, failure: context.DeadlineExceeded, want: readyLimit},
		{name: "own-deadline-successful-body", deadline: true, success: true, want: readyLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client, e := newSDKClient("github.com", "fixture-token", t.TempDir(), roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: readyTestBody{func(p []byte) (int, error) {
					if tc.cancel {
						cancel()
					}
					if tc.deadline {
						<-r.Context().Done()
					}
					if tc.success {
						return copy(p, `{}`), io.EOF
					}
					return 0, tc.failure
				}}}, nil
			}))
			if e != nil {
				t.Fatal(e)
			}
			s := newReadySources(ctx, &ghAPI{host: "github.com", client: client}, Repository{Host: "github.com", Owner: "o", Name: "r"})
			if tc.deadline {
				s.budget.started = time.Now().Add(-30*time.Second + 20*time.Millisecond)
			}
			if got := s.repository(); got.Source.Outcome != tc.want || got.Source.Complete {
				t.Fatalf("want %s got %+v", tc.want, got.Source)
			}
		})
	}
	t.Run("parent-canceled-successful-response", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := readyFixture(t, func(*http.Request) (int, string) { cancel(); return 403, `secret` })
		s.ctx = ctx
		if got := s.repository(); got.Source.Outcome != readyCanceled {
			t.Fatalf("%+v", got.Source)
		}
	})
}
func TestReadyExactEndpointAndMissingClient(t *testing.T) {
	for _, ep := range []string{"repos/o/r/", "repos/o/r?", "repos/o/r/?"} {
		if readyEndpoint(ep) {
			t.Errorf("noncanonical endpoint accepted %q", ep)
		}
	}
	t.Run("report-nil-api", func(t *testing.T) {
		s := newReadySources(context.Background(), nil, Repository{Host: "github.com", Owner: "o", Name: "r"})
		if got := s.report(9, 7); got.Source.Outcome != readyMalformed || got.Source.Complete {
			t.Fatalf("%+v", got.Source)
		}
	})
}
func TestReadyNormalizedValueBound(t *testing.T) {
	// JSON's HTML escaping can expand a normalized value beyond its raw bytes.
	src := readySource{Outcome: readyOK}
	got := finishReady(src, strings.Repeat("<", maxBytes/2), true)
	if got.Source.Outcome != readyLimit || got.Source.Complete {
		t.Fatalf("normalized result escaped bound: %+v", got.Source)
	}
}
