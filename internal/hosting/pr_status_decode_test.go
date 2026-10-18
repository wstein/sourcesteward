// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

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
