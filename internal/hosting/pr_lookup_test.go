// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wstein/sourcesteward/internal/inspection"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var lookupSHA = strings.Repeat("a", 40)

func lookupEndpoint(sha string) string {
	return "repos/a/b/commits/" + sha + "/pulls?per_page=30&page=1"
}

// lookupItem is one entry of GitHub's commit-pulls list: it carries no `merged` boolean.
func lookupItem(t *testing.T, number int, head string, mutate func(map[string]any)) map[string]any {
	t.Helper()
	p := statusBody(t)
	delete(p, "merged")
	p["number"] = number
	p["head"].(map[string]any)["sha"] = head
	if mutate != nil {
		mutate(p)
	}
	return p
}
func lookupBody(t *testing.T, items ...map[string]any) []byte {
	t.Helper()
	if items == nil {
		items = []map[string]any{}
	}
	b, e := json.Marshal(items)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func runLookup(t *testing.T, body []byte, err error) (LookupResult, int, string) {
	t.Helper()
	calls, path := 0, ""
	d := fixtureDeps(func(_ context.Context, p string) ([]byte, error) { calls++; path = p; return body, err })
	d.remotes = func(context.Context, string) inspection.RemoteResult {
		return inspection.RemoteResult{Identity: *ptrIdentity(), Status: "ok", Remotes: []inspection.Remote{{Name: "origin", FetchURLs: []string{"https://github.com/a/b.git"}, PushURLs: []string{}}}}
	}
	r := inspectLookup(context.Background(), ".", Options{Kind: "prs", HeadSHA: lookupSHA}, d)
	return r, calls, path
}

func TestLookupZeroOneMany(t *testing.T) {
	merged := func(p map[string]any) {
		p["state"], p["merge_commit_sha"] = "closed", strings.Repeat("d", 40)
		p["closed_at"], p["merged_at"] = "2026-10-18T12:00:00Z", "2026-10-18T12:00:00Z"
	}
	for _, tc := range []struct {
		name  string
		items []map[string]any
		want  []string // lifecycle per item
		match []bool
	}{
		{"zero", nil, nil, nil},
		{"one-open", []map[string]any{lookupItem(t, 3, lookupSHA, nil)}, []string{"open"}, []bool{true}},
		{"many-with-moved-head-and-merged", []map[string]any{lookupItem(t, 3, lookupSHA, nil), lookupItem(t, 3, strings.Repeat("e", 40), merged)}, []string{"open", "merged"}, []bool{true, false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, calls, path := runLookup(t, lookupBody(t, tc.items...), nil)
			if calls != 1 || path != lookupEndpoint(lookupSHA) || r.Status != "ok" || r.Count != len(tc.want) || r.Truncated || !r.Boundary.Complete || r.Boundary.Requests != 1 || r.Kind != "prs" || r.SchemaVersion != 2 || r.Query.HeadSHA != lookupSHA || r.SampledAt == nil {
				t.Fatalf("%+v calls=%d path=%s", r, calls, path)
			}
			for i, w := range tc.want {
				if r.PullRequests[i].Lifecycle != w || r.PullRequests[i].HeadMatch != tc.match[i] {
					t.Fatalf("item %d: %+v", i, r.PullRequests[i])
				}
			}
			if w := r.PullRequests; len(w) == 2 && (w[1].Merged != true || w[1].ResultSHA == nil || *w[1].ResultSHA != strings.Repeat("d", 40)) {
				t.Fatalf("merged is derived from merged_at: %+v", w[1])
			}
		})
	}
}

func TestLookupTruncationExactlyAtPageSize(t *testing.T) {
	for _, n := range []int{29, 30} {
		items := make([]map[string]any, n)
		for i := range items {
			items[i] = lookupItem(t, i+1, lookupSHA, nil)
		}
		r, _, _ := runLookup(t, lookupBody(t, items...), nil)
		if r.Status != "ok" || r.Count != n || r.Truncated != (n == 30) || (n == 30) != contains(r.Boundary.Limitations, "lookup-single-page") {
			t.Fatalf("%d: %+v", n, r)
		}
	}
	items := make([]map[string]any, 31)
	for i := range items {
		items[i] = lookupItem(t, i+1, lookupSHA, nil)
	}
	if r, _, _ := runLookup(t, lookupBody(t, items...), nil); r.Status != "error" || r.Count != 0 || len(r.PullRequests) != 0 {
		t.Fatalf("more than one page accepted: %+v", r)
	}
}
func contains(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

// A realistic page of 30 large items stays under the cap; an oversize body is an
// error without any partial list.
func TestLookupSizeBounds(t *testing.T) {
	items := make([]map[string]any, 30)
	for i := range items {
		items[i] = lookupItem(t, i+1, lookupSHA, func(p map[string]any) { p["body"] = strings.Repeat("x", 20*1024) })
	}
	body := lookupBody(t, items...)
	if len(body) > maxBytes || len(body) < 500*1024 {
		t.Fatalf("fixture size %d", len(body))
	}
	if r, _, _ := runLookup(t, body, nil); r.Status != "ok" || r.Count != 30 {
		t.Fatalf("%+v", r.Diagnostics)
	}
	r, calls, _ := runLookup(t, nil, errBodyLimit)
	if calls != 1 || r.Status != "error" || len(r.PullRequests) != 0 || r.Count != 0 || r.Diagnostics[0].Code != "hosting-pr-failed" || r.SampledAt != nil {
		t.Fatalf("%+v", r)
	}
}

func TestLookupEvidenceAndFailuresAreWholeResultErrorsWithoutRetry(t *testing.T) {
	good := lookupItem(t, 3, lookupSHA, nil)
	bad := func(mutate func(map[string]any)) []byte {
		return lookupBody(t, good, lookupItem(t, 4, lookupSHA, mutate))
	}
	for name, tc := range map[string]struct {
		body []byte
		err  error
		code string
	}{
		"missing-merged-at": {bad(func(p map[string]any) { delete(p, "merged_at") }), nil, "hosting-pr-evidence"},
		"open-with-merge":   {bad(func(p map[string]any) { p["merged_at"] = "2026-10-18T12:00:00Z" }), nil, "hosting-pr-evidence"},
		"closed-no-date":    {bad(func(p map[string]any) { p["state"] = "closed" }), nil, "hosting-pr-evidence"},
		"bad-number":        {bad(func(p map[string]any) { p["number"] = 0 }), nil, "hosting-pr-evidence"},
		"not-an-array":      {[]byte(`{}`), nil, "hosting-pr-evidence"},
		"foreign-base":      {bad(func(p map[string]any) { p["base"].(map[string]any)["repo"] = map[string]any{"full_name": "x/y"} }), nil, "hosting-pr-evidence"},
		"forbidden-429":     {nil, errors.New("HTTP 429 Retry-After: 60 token-secret"), "hosting-pr-failed"},
		"unknown-sha-422":   {nil, errors.New("HTTP 422"), "hosting-pr-failed"},
		"invalid-utf8":      {[]byte("[\xff]"), nil, "hosting-pr-evidence"},
		"null-body":         {[]byte("null"), nil, "hosting-pr-evidence"},
	} {
		t.Run(name, func(t *testing.T) {
			r, calls, _ := runLookup(t, tc.body, tc.err)
			raw, _ := json.Marshal(r)
			if calls != 1 || r.Status != "error" || len(r.PullRequests) != 0 || r.Count != 0 || r.Truncated || r.SampledAt != nil || r.Boundary.Complete || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != tc.code || strings.Contains(string(raw), "secret") {
				t.Fatalf("%+v calls=%d", r, calls)
			}
		})
	}
}

func TestLookupNativeFailureMakesNoRequestAndCancellationWins(t *testing.T) {
	calls := 0
	d := fixtureDeps(func(context.Context, string) ([]byte, error) { calls++; return lookupBody(t), nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := inspectLookup(ctx, ".", Options{Kind: "prs", HeadSHA: lookupSHA}, d)
	if r.Status != "error" || r.Diagnostics[0].Code != "hosting-canceled" || r.Count != 0 {
		t.Fatalf("%+v", r)
	}
	calls = 0
	r = inspectLookup(context.Background(), ".", Options{Kind: "prs", HeadSHA: "short"}, d)
	if calls != 0 || r.Status != "error" || r.Boundary.Network {
		t.Fatalf("%+v calls=%d", r, calls)
	}
}

// The new route is the only addition: exact path and query, GET only.
func TestLookupRouteAllowlistAndMethod(t *testing.T) {
	calls := 0
	client, e := newSDKClient("github.com", "fixturetoken", t.TempDir(), roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.String() != "https://api.github.com/"+lookupEndpoint(lookupSHA) {
			t.Fatalf("%s %s", r.Method, r.URL)
		}
		return response(200, `[]`), nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	api := &ghAPI{client: client, host: "github.com"}
	if _, e = api.get(context.Background(), lookupEndpoint(lookupSHA)); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{
		"repos/a/b/commits/" + lookupSHA + "/pulls",
		"repos/a/b/commits/" + lookupSHA + "/pulls?per_page=100&page=1",
		"repos/a/b/commits/" + lookupSHA + "/pulls?per_page=30&page=2",
		"repos/a/b/commits/" + lookupSHA + "/pulls?per_page=30&page=1&x=1",
		"repos/a/b/commits/" + lookupSHA + "/comments?per_page=30&page=1",
		"repos/a/b/commits/" + lookupSHA + "/pulls/1",
		"repos/a/b/commits/" + strings.Repeat("A", 40) + "/pulls?per_page=30&page=1",
		"repos/a/b/commits/abc/pulls?per_page=30&page=1",
		"repos/a/b/commits/" + strings.Repeat("a", 64) + "/pulls?per_page=30&page=1",
	} {
		if _, e = api.get(context.Background(), bad); e == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		req, _ := http.NewRequest(m, "https://api.github.com/"+lookupEndpoint(lookupSHA), nil)
		if _, e = client.Do(req); e == nil {
			t.Fatalf("method %s reached transport", m)
		}
	}
	if calls != 1 {
		t.Fatal(fmt.Sprint("rejected requests reached transport: ", calls))
	}
}

func TestLookupResultsMatchPublishedSchema(t *testing.T) {
	raw, e := os.ReadFile("../../schemas/v2/hosting-prs.schema.json")
	if e != nil {
		t.Fatal(e)
	}
	doc, e := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(prStatusSchemaLoader{})
	c.AssertFormat()
	const id = "https://wstein.github.io/sourcesteward/schemas/v2/hosting-prs.schema.json"
	if e = c.AddResource(id, doc); e != nil {
		t.Fatal(e)
	}
	schema, e := c.Compile(id)
	if e != nil {
		t.Fatal(e)
	}
	validate := func(name string, v any) {
		b, _ := json.Marshal(v)
		value, e := jsonschema.UnmarshalJSON(bytes.NewReader(b))
		if e != nil {
			t.Fatal(e)
		}
		if e = schema.Validate(value); e != nil {
			t.Fatalf("%s: %s %v", name, b, e)
		}
	}
	files, _ := filepath.Glob("../../specs/030-pr-status/contracts/lookup/*.json")
	if len(files) != 2 {
		t.Fatal(files)
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		var m map[string]any
		json.Unmarshal(b, &m)
		validate(f, m)
	}
	one := lookupBody(t, lookupItem(t, 3, lookupSHA, nil))
	for name, tc := range map[string]struct {
		body []byte
		err  error
	}{"zero": {lookupBody(t), nil}, "one": {one, nil}, "http": {nil, errors.New("x")}, "evidence": {[]byte("{}"), nil}} {
		r, _, _ := runLookup(t, tc.body, tc.err)
		validate(name, r)
	}
	full := make([]map[string]any, 30)
	for i := range full {
		full[i] = lookupItem(t, i+1, lookupSHA, nil)
	}
	r, _, _ := runLookup(t, lookupBody(t, full...), nil)
	validate("truncated", r)
}

// Escaping can grow the encoded result far past the raw body (< becomes six bytes):
// the output cap then fails the whole result and clears identity and repository.
func TestLookupOutputLimit(t *testing.T) {
	items := make([]map[string]any, 30)
	for i := range items {
		items[i] = lookupItem(t, i+1, lookupSHA, func(p map[string]any) { p["title"] = strings.Repeat("<", 25000) })
	}
	body := bytes.ReplaceAll(lookupBody(t, items...), []byte(`\u003c`), []byte("<"))
	if len(body) >= maxBytes {
		t.Fatalf("raw body must fit: %d", len(body))
	}
	r, _, _ := runLookup(t, body, nil)
	if r.Status != "error" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "hosting-output-limit" || r.Identity != nil || r.Repository != nil || r.Count != 0 || len(r.PullRequests) != 0 || r.SampledAt != nil || len(r.Candidates) != 0 {
		t.Fatalf("%+v", r.Diagnostics)
	}
}
