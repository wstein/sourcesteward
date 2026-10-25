// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

const testNodeID = "PR_kwDOABCD1234"

// wantReadyQuery is an independent copy of the one fixed query string: the production constant
// must equal it, so a change of the mutation text is a reviewed change of this test as well.
const wantReadyQuery = `mutation($id:ID!){markPullRequestReadyForReview(input:{pullRequestId:$id}){pullRequest{isDraft}}}`

var readyOn = StackWriteCapability{Ready: true}
var stackRepo = Repository{Host: "github.com", Owner: "owner", Name: "repo"}

const okReply = `{"data":{"markPullRequestReadyForReview":{"pullRequest":{"isDraft":false}}}}`

func TestReadyMutationIsOneFixedQueryWithTheNodeIdAsTheOnlyVariable(t *testing.T) {
	if readyMutationQuery != wantReadyQuery {
		t.Fatalf("the fixed query changed: %s", readyMutationQuery)
	}
	target, marker := fakeDispatchGH(t, true)
	calls := 0
	res, err := MarkPullRequestReadyWithTransport(context.Background(), target, stackRepo, testNodeID, readyOn, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.String() != "https://api.github.com/graphql" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "token fixturetoken" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("selected-host auth or JSON header missing: %v", r.Header)
		}
		raw, _ := io.ReadAll(r.Body)
		var got struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&got); err != nil || got.Query != wantReadyQuery || len(got.Variables) != 1 || got.Variables["id"] != testNodeID {
			t.Errorf("wrong body %s: %v", raw, err)
		}
		return response(200, okReply), nil
	}))
	if err != nil || res.Requests != 1 || res.StatusCode != 200 || calls != 1 {
		t.Fatalf("%+v %d %v", res, calls, err)
	}
	if raw, err := os.ReadFile(marker); err != nil || string(raw) != "--version\nauth token --hostname github.com\n" {
		t.Fatalf("auth process calls: %s %v", raw, err)
	}
}

func TestTheReadyEffectClassIsOneClosedValue(t *testing.T) {
	if EffectReady != "ready" {
		t.Fatal(EffectReady)
	}
	if (StackWriteCapability{}).Allows(EffectReady) {
		t.Fatal("the zero capability must allow nothing")
	}
	if !readyOn.Allows(EffectReady) || readyOn.Allows("merge") || readyOn.Allows("") || readyOn.Allows("create-pr") {
		t.Fatal("only the ready effect class exists")
	}
}

func TestZeroRequestsAndNoProcessWhileTheCapabilityIsOff(t *testing.T) {
	target, marker := fakeDispatchGH(t, true)
	calls := 0
	res, err := MarkPullRequestReadyWithTransport(context.Background(), target, stackRepo, testNodeID, StackWriteCapability{}, roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(200, okReply), nil }))
	if !errors.Is(err, ErrStackWriteDisabled) || res.Requests != 0 || calls != 0 {
		t.Fatalf("%+v %d %v", res, calls, err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("gh was started while the capability is off")
	}
}

func TestInvalidNodeIdsSendNothing(t *testing.T) {
	ids := []string{"", "abc", `PR_"}){x`, "PR_kw\nDO", "PR_kw DO", "PR_kw{DO}", "PR_kw$id", `PR_kw\DO`, "PR_kw;DO", "PR_kw/DO", "PR_kw\"DO", "PR_kw'DO", "PR_kwéDO", "PR_kw\x00DO", "PR_kw\tDO", strings.Repeat("A", 129), "PR_kw%22DO", "PR_kw),x:(", "}"}
	for _, id := range ids {
		target, marker := fakeDispatchGH(t, true)
		calls := 0
		res, err := MarkPullRequestReadyWithTransport(context.Background(), target, stackRepo, id, readyOn, roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(200, okReply), nil }))
		if err == nil || !errors.Is(err, ErrStackWriteInvalid) || res.Requests != 0 || calls != 0 {
			t.Errorf("%q: %+v %d %v", id, res, calls, err)
		}
		if _, statErr := os.Stat(marker); statErr == nil {
			t.Errorf("%q: gh was started", id)
		}
	}
	for _, id := range []string{testNodeID, "MDExOlB1bGxSZXF1ZXN0MQ==", strings.Repeat("A", 128), "PR_kwDO-ab_9"} {
		if !validNodeID(id) {
			t.Errorf("%q refused", id)
		}
	}
}

func TestOnlyTheSelectedGitHubHostIsWrittenTo(t *testing.T) {
	for _, host := range []string{"", "evil.test", "github.example.com", "api.github.com", "GitHub.com", "github.com:8443", "x.ghe.com"} {
		target, _ := fakeDispatchGH(t, true)
		calls := 0
		repo := stackRepo
		repo.Host = host
		res, err := MarkPullRequestReadyWithTransport(context.Background(), target, repo, testNodeID, readyOn, roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(200, okReply), nil }))
		if !errors.Is(err, ErrStackWriteInvalid) || res.Requests != 0 || calls != 0 {
			t.Errorf("%q: %+v %d %v", host, res, calls, err)
		}
	}
	for _, repo := range []Repository{{Host: "github.com", Owner: "", Name: "x"}, {Host: "github.com", Owner: "a/b", Name: "x"}, {Host: "github.com", Owner: "a", Name: ".."}} {
		target, _ := fakeDispatchGH(t, true)
		if res, err := MarkPullRequestReadyWithTransport(context.Background(), target, repo, testNodeID, readyOn, roundTripFunc(func(*http.Request) (*http.Response, error) { t.Error("request"); return nil, nil })); !errors.Is(err, ErrStackWriteInvalid) || res.Requests != 0 {
			t.Errorf("%+v: %+v %v", repo, res, err)
		}
	}
}

func TestEveryUnclearReplyIsUnknownNotFailure(t *testing.T) {
	replies := map[string]struct {
		status int
		body   string
		hdr    http.Header
	}{
		"graphql errors array":      {200, `{"errors":[{"message":"Could not resolve to a node"}],"data":null}`, nil},
		"errors beside data":        {200, `{"errors":[{"message":"x"}],"data":{"markPullRequestReadyForReview":null}}`, nil},
		"empty errors array":        {200, `{"errors":[],"data":{"markPullRequestReadyForReview":{}}}`, nil},
		"errors null":               {200, `{"errors":null,"data":{"markPullRequestReadyForReview":{}}}`, nil},
		"malformed json":            {200, `{"data":`, nil},
		"empty body":                {200, ``, nil},
		"json array":                {200, `[]`, nil},
		"json string":               {200, `"ok"`, nil},
		"no data":                   {200, `{}`, nil},
		"data null":                 {200, `{"data":null}`, nil},
		"data without the mutation": {200, `{"data":{}}`, nil},
		"mutation null":             {200, `{"data":{"markPullRequestReadyForReview":null}}`, nil},
		"trailing garbage":          {200, okReply + `{}`, nil},
		"server error":              {500, `{"message":"boom"}`, nil},
		"bad gateway":               {502, ``, nil},
		"unauthorized":              {401, `{"message":"Bad credentials"}`, nil},
		"forbidden":                 {403, `{"message":"rate"}`, nil},
		"created instead of ok":     {201, okReply, nil},
		"redirect":                  {302, ``, http.Header{"Location": []string{"https://evil.test/graphql"}}},
		"permanent redirect":        {308, ``, http.Header{"Location": []string{"https://api.github.com/graphql"}}},
		"compressed":                {200, okReply, http.Header{"Content-Encoding": []string{"gzip"}}},
		"response over the bound":   {200, `{"data":{"markPullRequestReadyForReview":{"pullRequest":{"isDraft":false}}},"pad":"` + strings.Repeat("x", 1<<20) + `"}`, nil},
	}
	for name, tc := range replies {
		target, _ := fakeDispatchGH(t, true)
		calls := 0
		res, err := MarkPullRequestReadyWithTransport(context.Background(), target, stackRepo, testNodeID, readyOn, roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			r := response(tc.status, tc.body)
			for k, v := range tc.hdr {
				r.Header[k] = v
			}
			return r, nil
		}))
		if !errors.Is(err, ErrStackWriteUnknown) || errors.Is(err, ErrStackWriteInvalid) || res.Requests != 1 || calls != 1 {
			t.Errorf("%s: %+v calls=%d err=%v", name, res, calls, err)
		}
		if err != nil && strings.Contains(err.Error(), "Bad credentials") {
			t.Errorf("%s: the remote text leaked: %v", name, err)
		}
	}
}

func TestATransportFailureAfterTheRequestWasSentIsUnknown(t *testing.T) {
	target, _ := fakeDispatchGH(t, true)
	calls := 0
	res, err := MarkPullRequestReadyWithTransport(context.Background(), target, stackRepo, testNodeID, readyOn, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("secret-connection-reset")
	}))
	if !errors.Is(err, ErrStackWriteUnknown) || res.Requests != 1 || calls != 1 || strings.Contains(err.Error(), "secret") {
		t.Fatalf("%+v %d %v", res, calls, err)
	}
}

func TestAuthenticationAndCancellationSendNothing(t *testing.T) {
	target, _ := fakeDispatchGH(t, false)
	calls := 0
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(200, okReply), nil })
	res, err := MarkPullRequestReadyWithTransport(context.Background(), target, stackRepo, testNodeID, readyOn, rt)
	if !errors.Is(err, ErrStackWriteAuth) || res.Requests != 0 || calls != 0 || strings.Contains(err.Error(), "secret-auth-body") {
		t.Fatalf("%+v %d %v", res, calls, err)
	}
	target, _ = fakeDispatchGH(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err = MarkPullRequestReadyWithTransport(ctx, target, stackRepo, testNodeID, readyOn, rt)
	if err == nil || res.Requests != 0 || calls != 0 {
		t.Fatalf("%+v %d %v", res, calls, err)
	}
}

func readyGuardFixture(next http.RoundTripper) (*stackWriteGuard, []byte) {
	body := readyMutationBody(testNodeID)
	return &stackWriteGuard{next: next, host: "api.github.com", path: "/graphql", body: body}, body
}

func readyRequestFixture(t *testing.T, mutate func(*http.Request), body []byte) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if mutate != nil {
		mutate(req)
	}
	return req
}

func TestTheWriteGuardAdmitsExactlyOneFixedRequest(t *testing.T) {
	calls := 0
	next := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(200, okReply), nil })
	g, body := readyGuardFixture(next)
	if _, err := g.RoundTrip(readyRequestFixture(t, nil, body)); err != nil || calls != 1 {
		t.Fatalf("%d %v", calls, err)
	}
	if _, err := g.RoundTrip(readyRequestFixture(t, nil, body)); err == nil || calls != 1 {
		t.Fatalf("a second request (retry) was admitted: %d", calls)
	}
	refusals := map[string]func(*http.Request){
		"get":          func(r *http.Request) { r.Method = http.MethodGet },
		"put":          func(r *http.Request) { r.Method = http.MethodPut },
		"patch":        func(r *http.Request) { r.Method = http.MethodPatch },
		"http":         func(r *http.Request) { r.URL.Scheme = "http" },
		"other host":   func(r *http.Request) { r.URL.Host = "evil.test" },
		"host header":  func(r *http.Request) { r.Host = "evil.test" },
		"other path":   func(r *http.Request) { r.URL.Path = "/repos/o/r/pulls/1" },
		"rest prefix":  func(r *http.Request) { r.URL.Path = "/graphql/x" },
		"query":        func(r *http.Request) { r.URL.RawQuery = "a=b" },
		"force query":  func(r *http.Request) { r.URL.ForceQuery = true },
		"fragment":     func(r *http.Request) { r.URL.Fragment = "x" },
		"userinfo":     func(r *http.Request) { r.URL.User = url.User("u") },
		"raw path":     func(r *http.Request) { r.URL.RawPath = "/graph%71l" },
		"content type": func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
		"no body":      func(r *http.Request) { r.Body = nil },
		"short length": func(r *http.Request) { r.ContentLength = 3 },
		"chunked":      func(r *http.Request) { r.TransferEncoding = []string{"chunked"} },
	}
	for name, mutate := range refusals {
		calls = 0
		g, body := readyGuardFixture(next)
		if _, err := g.RoundTrip(readyRequestFixture(t, mutate, body)); err == nil || calls != 0 {
			t.Errorf("%s: admitted (calls %d)", name, calls)
		}
	}
	for name, other := range map[string][]byte{
		"another node id": readyMutationBody("PR_kwDOOTHER9999"),
		"another query":   []byte(`{"query":"query{viewer{login}}","variables":{"id":"` + testNodeID + `"}}`),
		"extra variable":  []byte(`{"query":` + string(mustJSON(wantReadyQuery)) + `,"variables":{"id":"` + testNodeID + `","x":"1"}}`),
		"longer body":     append(append([]byte{}, readyMutationBody(testNodeID)...), ' '),
	} {
		calls = 0
		g, _ := readyGuardFixture(next)
		req := readyRequestFixture(t, nil, other)
		if _, err := g.RoundTrip(req); err == nil || calls != 0 {
			t.Errorf("%s: admitted (calls %d)", name, calls)
		}
	}
}

func mustJSON(s string) []byte {
	b, _ := json.Marshal(s)
	return b
}

// The read guards stay read-only and refuse the GraphQL endpoint in every mode and both methods.
func TestTheReadGuardsStillRefuseGraphQLInEveryMode(t *testing.T) {
	for _, mode := range []guardMode{guardOneShot, guardWatch, guardPulls, guardStatus, guardIssues} {
		g := guardedTransport{host: "api.github.com", prefix: "/", mode: mode}
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			req, _ := http.NewRequest(method, "https://api.github.com/graphql", nil)
			if g.allowed(req) {
				t.Errorf("mode %d admitted %s /graphql", mode, method)
			}
		}
		req, _ := http.NewRequest(http.MethodPost, "https://api.github.com/graphql", strings.NewReader(`{}`))
		if g.allowed(req) {
			t.Errorf("mode %d admitted a body", mode)
		}
	}
}

func TestStackWriteDoesNotReuseTheDispatchOrReadGuards(t *testing.T) {
	// The write guard refuses the dispatch endpoint and a read endpoint.
	next := roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("sent"); return nil, nil })
	g, body := readyGuardFixture(next)
	for _, path := range []string{"/repos/owner/repo/actions/workflows/x.yml/dispatches", "/repos/owner/repo/pulls/1"} {
		if _, err := g.RoundTrip(readyRequestFixture(t, func(r *http.Request) { r.URL.Path = path }, body)); err == nil {
			t.Errorf("%s admitted", path)
		}
	}
}
