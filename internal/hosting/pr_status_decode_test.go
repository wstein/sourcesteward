// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wstein/sourcesteward/internal/inspection"
)

func statusBody(t *testing.T) map[string]any {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(prJSON(strings.Repeat("a", 40)), &p); err != nil {
		t.Fatal(err)
	}
	p["merged"] = false
	p["merged_at"] = nil
	p["closed_at"] = nil
	p["merge_commit_sha"] = strings.Repeat("c", 40)
	return p
}
func statusBytes(t *testing.T, p map[string]any) []byte {
	t.Helper()
	b, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func statusSelection() Result {
	return Result{Backend: "git", Identity: ptrIdentity(), Repository: &Repository{Host: "github.com", Owner: "a", Name: "b", Selection: "explicit"}, Candidates: []Candidate{}}
}
func ptrIdentity() *inspection.Result {
	p := identity()
	p.Repository.ObjectFormat = "sha1"
	p.Repository.Worktree = stringPointer("/tmp/example")
	p.Repository.Head = inspection.Head{State: "attached", Ref: stringPointer("refs/heads/main"), OID: stringPointer(strings.Repeat("a", 40))}
	return &p
}

func TestPRStatusLifecycleEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		merged      bool
		raw         *string
		want        string
	}{
		{"open-test-merge", "open", false, stringPointer(strings.Repeat("c", 40)), "open"},
		{"open-null", "open", false, nil, "open"},
		{"closed-test-merge", "closed", false, stringPointer(strings.Repeat("c", 40)), "closed-unmerged"},
		{"merged-rebase", "closed", true, stringPointer(strings.Repeat("d", 40)), "merged"},
		{"merged-result-unavailable", "closed", true, nil, "merged"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := statusBody(t)
			p["state"] = tc.state
			p["merged"] = tc.merged
			p["merge_commit_sha"] = tc.raw
			if tc.state == "closed" {
				p["closed_at"] = "2026-10-18T12:00:00Z"
			}
			if tc.merged {
				p["merged_at"] = "2026-10-18T12:00:00Z"
			}
			got, e := decodePRStatus(statusBytes(t, p), statusSelection().Repository, 3)
			if e != nil {
				t.Fatal(e)
			}
			if got.Lifecycle != tc.want || got.Merged != tc.merged || got.HeadSHA != strings.Repeat("a", 40) {
				t.Fatalf("wrong lifecycle/identity: %+v", got)
			}
			if tc.raw == nil {
				if got.MergeCommitSHA != nil {
					t.Fatal("invented raw SHA")
				}
			} else if got.MergeCommitSHA == nil || *got.MergeCommitSHA != *tc.raw {
				t.Fatal("lost raw reported SHA")
			}
			if !tc.merged || tc.raw == nil {
				if got.ResultSHA != nil {
					t.Fatal("test merge/head promoted to result")
				}
			} else if got.ResultSHA == nil || *got.ResultSHA != *tc.raw || *got.ResultSHA == got.HeadSHA {
				t.Fatal("rebase result must retain distinct reported identity")
			}
		})
	}
}
func stringPointer(s string) *string { return &s }

func TestPRStatusRequiredAndInconsistentEvidence(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"case-only-merged", func(p map[string]any) { delete(p, "merged"); p["MERGED"] = false }},
		{"case-only-merged-at", func(p map[string]any) { delete(p, "merged_at"); p["MERGED_AT"] = nil }},
		{"missing-merged", func(p map[string]any) { delete(p, "merged") }},
		{"null-merged", func(p map[string]any) { p["merged"] = nil }},
		{"wrong-merged", func(p map[string]any) { p["merged"] = "false" }},
		{"missing-merged-at", func(p map[string]any) { delete(p, "merged_at") }},
		{"missing-closed-at", func(p map[string]any) { delete(p, "closed_at") }},
		{"missing-merge-sha", func(p map[string]any) { delete(p, "merge_commit_sha") }},
		{"open-merged", func(p map[string]any) { p["merged"] = true }},
		{"open-closed-date", func(p map[string]any) { p["closed_at"] = "2026-10-18T12:00:00Z" }},
		{"open-merged-date", func(p map[string]any) { p["merged_at"] = "2026-10-18T12:00:00Z" }},
		{"closed-without-date", func(p map[string]any) { p["state"] = "closed" }},
		{"merged-without-date", func(p map[string]any) {
			p["state"] = "closed"
			p["merged"] = true
			p["closed_at"] = "2026-10-18T12:00:00Z"
		}},
		{"unmerged-with-merge-date", func(p map[string]any) {
			p["state"] = "closed"
			p["closed_at"] = "2026-10-18T12:00:00Z"
			p["merged_at"] = "2026-10-18T12:00:00Z"
		}},
		{"bad-date", func(p map[string]any) { p["state"] = "closed"; p["closed_at"] = "yesterday" }},
		{"wrong-date-type", func(p map[string]any) { p["closed_at"] = false }},
		{"unknown-state", func(p map[string]any) { p["state"] = "merged" }},
		{"wrong-number", func(p map[string]any) { p["number"] = 4 }},
		{"short-sha", func(p map[string]any) { p["merge_commit_sha"] = "abc" }},
		{"uppercase-sha", func(p map[string]any) { p["merge_commit_sha"] = strings.Repeat("A", 40) }},
		{"64-character-sha", func(p map[string]any) { p["merge_commit_sha"] = strings.Repeat("a", 64) }},
		{"head-ref-empty", func(p map[string]any) { p["head"].(map[string]any)["ref"] = "" }},
		{"head-sha-invalid", func(p map[string]any) { p["head"].(map[string]any)["sha"] = "abc" }},
		{"missing-head-repo", func(p map[string]any) { delete(p["head"].(map[string]any), "repo") }},
		{"invalid-head-repo", func(p map[string]any) { p["head"].(map[string]any)["repo"] = map[string]any{"full_name": "./.."} }},
		{"base-repo-null", func(p map[string]any) { p["base"].(map[string]any)["repo"] = nil }},
		{"base-mismatch", func(p map[string]any) { p["base"].(map[string]any)["repo"] = map[string]any{"full_name": "other/repo"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := statusBody(t)
			tc.mutate(p)
			if got, e := decodePRStatus(statusBytes(t, p), statusSelection().Repository, 3); e == nil || got != nil {
				t.Fatalf("accepted incomplete/inconsistent evidence: %+v %v", got, e)
			}
		})
	}
	p := statusBody(t)
	p["title"] = strings.Repeat("a", maxBytes)
	oversized := statusBytes(t, p)
	invalidUTF8 := bytes.Replace(statusBytes(t, statusBody(t)), []byte(`"title":"title"`), []byte{'"', 't', 'i', 't', 'l', 'e', '"', ':', '"', 0xff, '"'}, 1)
	for _, raw := range [][]byte{[]byte("null"), []byte("[]"), []byte("{broken"), append([]byte(`{"secret":"`), 0xff), bytes.Repeat([]byte(" "), maxBytes+1), oversized, invalidUTF8} {
		if p, e := decodePRStatus(raw, statusSelection().Repository, 3); e == nil || p != nil {
			t.Fatal("accepted malformed/oversized evidence")
		}
	}
}

func TestPRStatusDeletedHeadAndIgnoredMergeability(t *testing.T) {
	for _, mergeable := range []any{nil, false, true, "unknown-future-value"} {
		p := statusBody(t)
		p["head"].(map[string]any)["repo"] = nil
		p["mergeable"] = mergeable
		p["future"] = true
		p["MERGED"] = "ignored unknown field"
		got, e := decodePRStatus(statusBytes(t, p), statusSelection().Repository, 3)
		if e != nil || got.HeadRepository != nil || got.Lifecycle != "open" {
			t.Fatalf("nullable head/unknown fields: %+v %v", got, e)
		}
	}
	p := statusBody(t)
	p["base"].(map[string]any)["repo"] = map[string]any{"full_name": "A/B"}
	if _, e := decodePRStatus(statusBytes(t, p), statusSelection().Repository, 3); e != nil {
		t.Fatal("lost existing case-insensitive base comparison")
	}
}

func TestPRStatusOnceResultAndExplicitNulls(t *testing.T) {
	n := 0
	times := []time.Time{time.Date(2026, 10, 18, 12, 0, 1, 0, time.FixedZone("offset", 3600)), time.Date(2026, 10, 18, 12, 0, 2, 0, time.UTC)}
	clock := func() time.Time { v := times[n]; n++; return v }
	calls := 0
	p := statusBody(t)
	p["head"].(map[string]any)["repo"] = nil
	p["mergeable"] = nil
	r := readPRStatusOnce(context.Background(), apiReaderFunc(func(_ context.Context, path string) ([]byte, error) {
		calls++
		if path != "repos/a/b/pulls/3" {
			t.Fatal(path)
		}
		return statusBytes(t, p), nil
	}), statusSelection(), 3, clock)
	if calls != 1 || r.SchemaVersion != 2 || r.Status != "ok" || !r.Boundary.Complete || r.Boundary.Requests != 1 || r.Observation.Samples != 1 || r.Observation.Mode != "once" || r.Observation.Stop != "sampled" {
		t.Fatalf("wrong one-shot result: %+v calls=%d", r, calls)
	}
	if r.SampledAt == nil || *r.SampledAt != "2026-10-18T11:00:01Z" || r.ObservedAt != "2026-10-18T12:00:02Z" {
		t.Fatalf("sample/final timestamps: %+v", r)
	}
	raw, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	var out map[string]any
	json.Unmarshal(raw, &out)
	pr := out["pull_request"].(map[string]any)
	for _, key := range []string{"merged_at", "closed_at", "result_sha", "head_repository"} {
		if value, ok := pr[key]; !ok || value != nil {
			t.Fatalf("missing explicit null %s: %s", key, raw)
		}
	}
	obs := out["observation"].(map[string]any)
	if v, ok := obs["expected_head"]; !ok || v != nil {
		t.Fatal("missing explicit expected_head null")
	}
	if out["diagnostics"] == nil || out["candidates"] == nil {
		t.Fatal("nil arrays")
	}
	if _, ok := out["checks"]; ok {
		t.Fatal("v1 CI fields leaked into v2 PR")
	}
}

func TestPRStatusOnceCancellationAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		name                                  string
		before, during, apiError, badEvidence bool
		wantRequests                          int
		wantCode                              string
	}{
		{name: "before", before: true, wantCode: "hosting-canceled"},
		{name: "during", during: true, wantRequests: 1, wantCode: "hosting-canceled"},
		{name: "transport-secret", apiError: true, wantRequests: 1, wantCode: "hosting-pr-failed"},
		{name: "evidence-secret", badEvidence: true, wantRequests: 1, wantCode: "hosting-pr-evidence"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.before {
				cancel()
			}
			calls := 0
			r := readPRStatusOnce(ctx, apiReaderFunc(func(context.Context, string) ([]byte, error) {
				calls++
				if tc.during {
					cancel()
				}
				if tc.apiError {
					return nil, errors.New("credential-secret")
				}
				if tc.badEvidence {
					return []byte(`{"secret":"credential-secret"}`), nil
				}
				return statusBytes(t, statusBody(t)), nil
			}), statusSelection(), 3, time.Now)
			if calls != tc.wantRequests || r.Boundary.Requests != calls || r.Boundary.Network != (calls > 0) || r.Status != "error" || r.Boundary.Complete || r.PullRequest != nil || r.SampledAt != nil || r.Observation.Samples != 0 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != tc.wantCode {
				t.Fatalf("unsafe error result: %+v calls=%d", r, calls)
			}
			raw, _ := json.Marshal(r)
			if bytes.Contains(raw, []byte("credential-secret")) {
				t.Fatal("raw auth/API/evidence detail leaked")
			}
		})
	}
}

func TestPRStatusOnceOutputBoundAndLegacyReader(t *testing.T) {
	p := statusBody(t)
	p["title"] = strings.Repeat("<", maxBytes/4)
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(p); err != nil {
		t.Fatal(err)
	}
	r := readPRStatusOnce(context.Background(), apiReaderFunc(func(context.Context, string) ([]byte, error) { return body.Bytes(), nil }), statusSelection(), 3, time.Now)
	raw, e := json.Marshal(r)
	if e != nil || len(raw)+1 > maxBytes || r.Status != "error" || r.Observation.Stop != "output-limit" || r.PullRequest != nil || r.SampledAt != nil || r.Observation.InitialHeadSHA != nil || r.Observation.Samples != 1 || r.Boundary.Requests != 1 {
		t.Fatalf("unbounded/incorrect fallback: %+v bytes=%d %v", r, len(raw), e)
	}
	got, e := readPR(context.Background(), apiReaderFunc(func(context.Context, string) ([]byte, error) { return prJSON(strings.Repeat("a", 40)), nil }), statusSelection().Repository, 3)
	if e != nil || got.HeadRepository != "fork/b" {
		t.Fatal("legacy reader changed")
	}
	p = statusBody(t)
	p["head"].(map[string]any)["repo"] = nil
	if _, e := readPR(context.Background(), apiReaderFunc(func(context.Context, string) ([]byte, error) { return statusBytes(t, p), nil }), statusSelection().Repository, 3); e == nil {
		t.Fatal("legacy null-head behavior changed")
	}
}

type prStatusSchemaLoader struct{}

func (prStatusSchemaLoader) Load(string) (any, error) {
	return nil, errors.New("external schema load refused")
}
func TestPRStatusResultSchemaAndJJIdentity(t *testing.T) {
	raw, e := os.ReadFile("../../schemas/v2/hosting-pr.schema.json")
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
	const id = "https://wstein.github.io/sourcesteward/schemas/v2/hosting-pr.schema.json"
	if e = c.AddResource(id, doc); e != nil {
		t.Fatal(e)
	}
	schema, e := c.Compile(id)
	if e != nil {
		t.Fatal(e)
	}
	for _, backend := range []string{"git", "jj"} {
		for _, bad := range []bool{false, true} {
			t.Run(backend+fmt.Sprint(bad), func(t *testing.T) {
				selection := statusSelection()
				if backend == "jj" {
					selection.Backend = "jj"
					selection.Identity = &inspection.Result{SchemaVersion: 2, Status: "ok", Backend: "jj", InputPath: "/repo", Evidence: []inspection.Evidence{{Kind: "jj-metadata", Location: "/repo/.jj"}}, Diagnostics: []inspection.Diagnostic{}, JJRepository: &inspection.JJRepository{WorkspaceRoot: "/repo", MetadataDir: "/repo/.jj", RepositoryDir: "/repo/.jj/repo", WorkspaceName: "default", ChangeID: strings.Repeat("k", 32), CommitID: strings.Repeat("a", 40), OperationID: strings.Repeat("b", 128), Observation: inspection.JJObservation{WorkingCopy: "stored-unsnapshotted", Limitation: "Stored identity only."}}}
				}
				r := readPRStatusOnce(context.Background(), apiReaderFunc(func(context.Context, string) ([]byte, error) {
					if bad {
						return nil, errors.New("private detail")
					}
					return statusBytes(t, statusBody(t)), nil
				}), selection, 3, time.Now)
				if r.Backend != backend || r.Identity != selection.Identity {
					t.Fatal("native identity was converted or replaced")
				}
				encoded, e := json.Marshal(r)
				if e != nil {
					t.Fatal(e)
				}
				value, e := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
				if e != nil {
					t.Fatal(e)
				}
				if e = schema.Validate(value); e != nil {
					t.Fatal(e)
				}
			})
		}
	}
}

func TestPRStatusRFC3339Evidence(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"2026-10-18T12:00:00.123+01:30", true}, {"2026-10-18t12:00:00z", true}, {"2016-12-31T23:59:60Z", true},
		{"2026-10-18T1:00:00Z", false}, {"2026-10-18T12:00:00,1Z", false}, {"2026-10-18T12:00:00+24:00", false}, {"2026-10-18T12:00:00+01:60", false}, {"2026-02-30T12:00:00Z", false}, {"2026-10-18T12:00:60Z", false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			p := statusBody(t)
			p["state"] = "closed"
			p["closed_at"] = tc.value
			got, e := decodePRStatus(statusBytes(t, p), statusSelection().Repository, 3)
			if (e == nil) != tc.valid {
				t.Fatalf("timestamp accepted=%v want=%v", e == nil, tc.valid)
			}
			if tc.valid && (got.ClosedAt == nil || *got.ClosedAt != tc.value) {
				t.Fatal("valid reported timestamp text changed")
			}
		})
	}
}

func TestPRStatusCancellationDuringFinalAssembly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := 0
	clock := func() time.Time {
		ticks++
		if ticks == 2 {
			cancel()
		}
		return time.Now()
	}
	r := readPRStatusOnce(ctx, apiReaderFunc(func(context.Context, string) ([]byte, error) { return statusBytes(t, statusBody(t)), nil }), statusSelection(), 3, clock)
	if r.Status != "error" || r.Boundary.Complete || r.Observation.Stop != "canceled" || r.PullRequest == nil || r.Observation.Samples != 1 {
		t.Fatal("final assembly cancellation must retain only incomplete sampled evidence")
	}
}
