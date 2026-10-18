// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestTidyNineMergedHeadsIncludeSevenStackedBases(t *testing.T) {
	targets := []string{}
	rows := []string{}
	for i := 1; i <= 9; i++ {
		head := fmt.Sprintf("%040x", i)
		targets = append(targets, head)
		base := "main"
		if i <= 7 {
			base = fmt.Sprintf("deleted-stack-%d", i)
		}
		rows = append(rows, tidyPRJSON(i, head, base))
	}
	calls := []string{}
	s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.RequestURI())
		if req.Method != "GET" || req.URL.Host != "api.github.com" {
			t.Fatal("non-read destination")
		}
		if req.URL.Path == "/repos/owner/repo" {
			return response(200, tidyRepositoryJSON()), nil
		}
		if req.URL.Path == "/repos/owner/repo/pulls" {
			if req.URL.RawQuery != "state=closed&per_page=100&page=1" {
				t.Fatal(req.URL.RawQuery)
			}
			return response(200, "["+strings.Join(rows, ",")+"]"), nil
		}
		var n int
		fmt.Sscanf(req.URL.Path, "/repos/owner/repo/pulls/%d", &n)
		if n < 1 || n > 9 {
			t.Fatal(req.URL.Path)
		}
		return response(200, rows[n-1]), nil
	})
	got, err := s.DiscoverPullRequests(context.Background(), targets, nil)
	if err != nil || len(got) != 9 {
		t.Fatalf("%+v %v", got, err)
	}
	for i, item := range got {
		if item.PullRequest == nil || item.HeadOID != targets[i] || item.Reason != "github-merged-head" {
			t.Fatalf("%+v", item)
		}
		want := "main"
		if i < 7 {
			want = fmt.Sprintf("deleted-stack-%d", i+1)
		}
		if item.PullRequest.BaseRef != want {
			t.Fatalf("original base lost: %+v", item)
		}
	}
	if len(calls) != 11 {
		t.Fatal(calls)
	}
}
func TestTidyPRHintsNarrowOnlyDiscoveryAndFullTupleResamples(t *testing.T) {
	head := strings.Repeat("a", 40)
	raw := tidyPRJSON(7, head, "removed-stack")
	requests := 0
	s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.Path == "/repos/owner/repo" {
			return response(200, tidyRepositoryJSON()), nil
		}
		if req.URL.Path != "/repos/owner/repo/pulls/7" {
			t.Fatal(req.URL)
		}
		return response(200, raw), nil
	})
	got, err := s.DiscoverPullRequests(context.Background(), []string{head}, map[string]int{head: 7})
	if err != nil || len(got) != 1 || got[0].PullRequest == nil || requests != 2 {
		t.Fatalf("%+v %v %d", got, err, requests)
	}
	original := *got[0].PullRequest
	if err = s.RevalidatePullRequest(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	raw = strings.Replace(raw, `"ref":"removed-stack"`, `"ref":"renamed-stack"`, 1)
	if err = s.RevalidatePullRequest(context.Background(), original); err != ErrTidyChanged {
		t.Fatalf("changed base accepted: %v", err)
	}
}

func mutateTidyJSON(t *testing.T, raw string, path []string, value any, remove bool) string {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		t.Fatal(err)
	}
	object := root
	for _, key := range path[:len(path)-1] {
		next, ok := object[key].(map[string]any)
		if !ok {
			t.Fatal(path)
		}
		object = next
	}
	key := path[len(path)-1]
	if remove {
		delete(object, key)
	} else {
		object[key] = value
	}
	out, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
func TestTidyPRAllRequiredFieldsMissingNullOrWrongType(t *testing.T) {
	head := strings.Repeat("a", 40)
	valid := tidyPRJSON(7, head, "intermediate")
	paths := [][]string{{"id"}, {"node_id"}, {"number"}, {"state"}, {"merged"}, {"merged_at"}, {"draft"}, {"head"}, {"base"}, {"merge_commit_sha"}}
	for _, ref := range []string{"head", "base"} {
		paths = append(paths, []string{ref, "sha"}, []string{ref, "ref"}, []string{ref, "repo"})
		for _, field := range []string{"id", "node_id", "full_name"} {
			paths = append(paths, []string{ref, "repo", field})
		}
	}
	for _, path := range paths {
		for _, mode := range []string{"missing", "null", "wrong-type"} {
			// A null head/base repository is a complete deleted-repository observation;
			// discovery retains it as unsupported rather than claiming missing evidence.
			if mode == "null" && len(path) == 2 && path[1] == "repo" {
				continue
			}
			t.Run(strings.Join(path, "/")+"/"+mode, func(t *testing.T) {
				var value any
				if mode == "wrong-type" {
					value = []any{"invalid"}
				}
				raw := mutateTidyJSON(t, valid, path, value, mode == "missing")
				s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
					if req.URL.Path == "/repos/owner/repo" {
						return response(200, tidyRepositoryJSON()), nil
					}
					return response(200, raw), nil
				})
				if _, err := s.ReadPullRequest(context.Background(), 7); err == nil {
					t.Fatal("incomplete required tuple admitted", raw)
				}
			})
		}
	}
}
func TestTidyPRCompleteNegativeReasonsAndAmbiguity(t *testing.T) {
	head := strings.Repeat("a", 40)
	valid := tidyPRJSON(7, head, "old-stack")
	cases := []struct{ name, raw, reason string }{
		{"unmerged", strings.Replace(strings.Replace(strings.Replace(valid, `"merged":true`, `"merged":false`, 1), `"merged_at":"2026-10-18T00:00:00Z"`, `"merged_at":null`, 1), `"merge_commit_sha":"`+strings.Repeat("c", 40)+`"`, `"merge_commit_sha":null`, 1), "integration-not-proven"},
		{"draft", strings.Replace(valid, `"draft":false`, `"draft":true`, 1), "integration-not-proven"},
		{"amended-tip", strings.Replace(valid, head, strings.Repeat("d", 40), 1), "pr-head-mismatch"},
		{"fork", mutateTidyJSON(t, valid, []string{"head", "repo", "id"}, float64(99), false), "hosting-proof-unsupported"},
		{"wrong-base-repository", mutateTidyJSON(t, valid, []string{"base", "repo", "full_name"}, "other/repo", false), "hosting-proof-unsupported"},
		{"deleted-head", mutateTidyJSON(t, valid, []string{"head", "repo"}, nil, false), "hosting-proof-unsupported"},
		{"deleted-base", mutateTidyJSON(t, valid, []string{"base", "repo"}, nil, false), "hosting-proof-unsupported"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/repos/owner/repo" {
					return response(200, tidyRepositoryJSON()), nil
				}
				return response(200, item.raw), nil
			})
			got, err := s.DiscoverPullRequests(context.Background(), []string{head}, map[string]int{head: 7})
			if err != nil || len(got) != 1 || got[0].PullRequest != nil || got[0].Reason != item.reason {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
	t.Run("ambiguous", func(t *testing.T) {
		raw8 := tidyPRJSON(8, head, "main")
		s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/repos/owner/repo":
				return response(200, tidyRepositoryJSON()), nil
			case "/repos/owner/repo/pulls":
				return response(200, "["+valid+","+raw8+"]"), nil
			case "/repos/owner/repo/pulls/7":
				return response(200, valid), nil
			default:
				return response(200, raw8), nil
			}
		})
		got, err := s.DiscoverPullRequests(context.Background(), []string{head}, nil)
		if err != nil || len(got) != 1 || got[0].Reason != "pr-ambiguous" || got[0].PullRequest != nil {
			t.Fatalf("%+v %v", got, err)
		}
		got, err = s.DiscoverPullRequests(context.Background(), []string{head}, map[string]int{head: 7})
		if err != nil || got[0].PullRequest == nil {
			t.Fatalf("explicit hint did not select: %+v %v", got, err)
		}
	})
}
func TestTidyPRRevalidationComparesEveryTupleField(t *testing.T) {
	head := strings.Repeat("a", 40)
	valid := tidyPRJSON(7, head, "removed-base")
	original, err := decodeTidyPullRequest([]byte(valid), TidyRepositoryIdentity{Host: "github.com", TidyRepositoryID: TidyRepositoryID{ID: 41, NodeID: "R_repo", FullName: "owner/repo"}, DefaultBranch: "main"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	changes := []struct {
		path  []string
		value any
	}{
		{[]string{"id"}, float64(999)}, {[]string{"node_id"}, "PR_changed"}, {[]string{"draft"}, true}, {[]string{"merged_at"}, "2026-10-18T01:00:00Z"},
		{[]string{"head", "sha"}, strings.Repeat("d", 40)}, {[]string{"head", "ref"}, "renamed-topic"}, {[]string{"base", "sha"}, strings.Repeat("e", 40)}, {[]string{"base", "ref"}, "renamed-base"}, {[]string{"merge_commit_sha"}, strings.Repeat("f", 40)},
	}
	for _, ref := range []string{"head", "base"} {
		changes = append(changes, struct {
			path  []string
			value any
		}{[]string{ref, "repo", "id"}, float64(42)}, struct {
			path  []string
			value any
		}{[]string{ref, "repo", "node_id"}, "R_changed"}, struct {
			path  []string
			value any
		}{[]string{ref, "repo", "full_name"}, "owner/other"})
	}
	for _, change := range changes {
		t.Run(strings.Join(change.path, "/"), func(t *testing.T) {
			raw := mutateTidyJSON(t, valid, change.path, change.value, false)
			s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/repos/owner/repo" {
					return response(200, tidyRepositoryJSON()), nil
				}
				return response(200, raw), nil
			})
			if err := s.RevalidatePullRequest(context.Background(), original); !errors.Is(err, ErrTidyChanged) {
				t.Fatalf("changed tuple accepted: %v", err)
			}
		})
	}
}
func TestTidyPRDiscoveryCompletenessAndHintValidation(t *testing.T) {
	head := strings.Repeat("a", 40)
	row := tidyPRJSON(7, head, "main")
	for _, raw := range []string{`null`, `[null]`, `[{"id":1,"node_id":"PR_1","number":1,"state":"closed","head":{}}]`, "[" + row + "," + row + "]", "[" + strings.Join(makeRepeatedTidyRows(row, 101), ",") + "]"} {
		t.Run(fmt.Sprint(len(raw)), func(t *testing.T) {
			s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/repos/owner/repo" {
					return response(200, tidyRepositoryJSON()), nil
				}
				return response(200, raw), nil
			})
			got, err := s.DiscoverPullRequests(context.Background(), []string{head}, nil)
			if err == nil || got != nil {
				t.Fatalf("incomplete prefix accepted %+v %v", got, err)
			}
		})
	}
	t.Run("no-match-is-complete", func(t *testing.T) {
		s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/repos/owner/repo" {
				return response(200, tidyRepositoryJSON()), nil
			}
			return response(200, `[]`), nil
		})
		got, err := s.DiscoverPullRequests(context.Background(), []string{head}, nil)
		if err != nil || len(got) != 1 || got[0].Reason != "integration-not-proven" {
			t.Fatalf("%+v %v", got, err)
		}
	})
	for _, item := range []struct {
		targets []string
		hints   map[string]int
	}{{nil, nil}, {[]string{"topic"}, nil}, {[]string{head, head}, nil}, {[]string{head}, map[string]int{strings.Repeat("b", 40): 7}}, {[]string{head}, map[string]int{head: 0}}, {[]string{head, strings.Repeat("b", 40)}, map[string]int{head: 7, strings.Repeat("b", 40): 7}}} {
		s := tidyFixtureSession(t, func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid targets reached transport")
			return nil, nil
		})
		if got, err := s.DiscoverPullRequests(context.Background(), item.targets, item.hints); err == nil || got != nil || s.Budget().Requests != 0 {
			t.Fatalf("invalid targets accepted %+v %v", got, err)
		}
	}
}
func makeRepeatedTidyRows(row string, n int) []string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = row
	}
	return rows
}
func TestTidyPRHintsDoNotHideDiscoveryFailureForUnhintedHeads(t *testing.T) {
	head := strings.Repeat("a", 40)
	other := strings.Repeat("b", 40)
	s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/repos/owner/repo" {
			return response(200, tidyRepositoryJSON()), nil
		}
		if req.URL.Path != "/repos/owner/repo/pulls" {
			t.Fatal("selected PR read before list exhaustion")
		}
		return response(403, `{"message":"denied"}`), nil
	})
	got, err := s.DiscoverPullRequests(context.Background(), []string{head, other}, map[string]int{head: 7})
	if err == nil || got != nil {
		t.Fatalf("hint hid incomplete collection: %+v %v", got, err)
	}
}

func TestTidyPRRequiresExactMemberNames(t *testing.T) {
	head := strings.Repeat("a", 40)
	paths := [][]string{{"id"}, {"node_id"}, {"number"}, {"state"}, {"merged"}, {"merged_at"}, {"draft"}, {"head"}, {"base"}, {"merge_commit_sha"}}
	for _, ref := range []string{"head", "base"} {
		paths = append(paths, []string{ref, "sha"}, []string{ref, "ref"}, []string{ref, "repo"})
		for _, field := range []string{"id", "node_id", "full_name"} {
			paths = append(paths, []string{ref, "repo", field})
		}
	}
	for _, path := range paths {
		for _, only := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/alias-only=%v", strings.Join(path, "/"), only), func(t *testing.T) {
				raw := tidyAliasedMember(t, tidyPRJSON(7, head, "stack"), path, only)
				s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
					if req.URL.Path == "/repos/owner/repo" {
						return response(200, tidyRepositoryJSON()), nil
					}
					return response(200, raw), nil
				})
				if got, err := s.DiscoverPullRequests(context.Background(), []string{head}, map[string]int{head: 7}); err == nil || got != nil {
					t.Fatalf("aliased PR proof accepted: %+v %v", got, err)
				}
			})
		}
	}
}
func TestTidyDiscoveryRequiresExactMemberNames(t *testing.T) {
	head := strings.Repeat("a", 40)
	for _, path := range [][]string{{"id"}, {"node_id"}, {"number"}, {"state"}, {"head"}, {"head", "sha"}} {
		for _, only := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/alias-only=%v", strings.Join(path, "/"), only), func(t *testing.T) {
				raw := tidyAliasedMember(t, tidyPRJSON(7, head, "main"), path, only)
				s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
					if req.URL.Path == "/repos/owner/repo" {
						return response(200, tidyRepositoryJSON()), nil
					}
					if req.URL.Path == "/repos/owner/repo/pulls" {
						return response(200, "["+raw+"]"), nil
					}
					return response(200, tidyPRJSON(7, head, "main")), nil
				})
				if got, err := s.DiscoverPullRequests(context.Background(), []string{head}, nil); err == nil || got != nil {
					t.Fatalf("aliased discovery accepted: %+v %v", got, err)
				}
			})
		}
	}
}
func TestTidyCaseAliasCannotCreateMergedProof(t *testing.T) {
	head := strings.Repeat("a", 40)
	for _, raw := range []string{strings.Replace(tidyPRJSON(7, head, "main"), `"merged":true`, `"merged":false,"MERGED":true`, 1), strings.Replace(tidyPRJSON(7, head, "main"), `"state":"closed"`, `"ſtate":"closed"`, 1)} {
		s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/repos/owner/repo" {
				return response(200, tidyRepositoryJSON()), nil
			}
			return response(200, raw), nil
		})
		if got, err := s.DiscoverPullRequests(context.Background(), []string{head}, map[string]int{head: 7}); err == nil || got != nil {
			t.Fatalf("aliased merge proof accepted: %+v %v", got, err)
		}
	}
}
