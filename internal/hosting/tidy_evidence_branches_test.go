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

func TestTidyBranchesCompleteFingerprintAndReappearance(t *testing.T) {
	rows := []string{tidyBranchJSON("main", strings.Repeat("a", 40)), tidyBranchJSON("feature/stack", strings.Repeat("b", 40))}
	requests := 0
	s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.Path == "/repos/owner/repo" {
			return response(200, tidyRepositoryJSON()), nil
		}
		if req.URL.RequestURI() != "/repos/owner/repo/branches?per_page=100&page=1" {
			t.Fatal(req.URL)
		}
		return response(200, "["+strings.Join(rows, ",")+"]"), nil
	})
	first, err := s.ReadBranches(context.Background())
	if err != nil || len(first.Branches) != 2 || first.Fingerprint == "" {
		t.Fatalf("%+v %v", first, err)
	}
	rows[0], rows[1] = rows[1], rows[0]
	if err = s.RevalidateBranches(context.Background(), first); err != nil {
		t.Fatal("ordering changed fingerprint", err)
	}
	rows = append(rows, tidyBranchJSON("reappeared", strings.Repeat("c", 40)))
	if err = s.RevalidateBranches(context.Background(), first); !errors.Is(err, ErrTidyChanged) {
		t.Fatal("reappearance accepted", err)
	}
	if requests != 6 {
		t.Fatal(requests)
	}
}
func TestTidyBranchesRejectIncompleteAndDuplicateRows(t *testing.T) {
	for _, raw := range []string{`null`, `[null]`, `[{"name":"main","commit":{}}]`, `[{"name":"../bad","commit":{"sha":"` + strings.Repeat("a", 40) + `"}}]`, "[" + tidyBranchJSON("main", strings.Repeat("a", 40)) + "," + tidyBranchJSON("main", strings.Repeat("b", 40)) + "]"} {
		t.Run(fmt.Sprint(len(raw)), func(t *testing.T) {
			s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/repos/owner/repo" {
					return response(200, tidyRepositoryJSON()), nil
				}
				return response(200, raw), nil
			})
			got, err := s.ReadBranches(context.Background())
			if err == nil || got.Branches != nil {
				t.Fatalf("prefix returned: %+v %v", got, err)
			}
		})
	}
}

func TestTidyBranchesPaginationProvesExhaustionAtRecordLimit(t *testing.T) {
	for _, proven := range []bool{false, true} {
		t.Run(fmt.Sprint(proven), func(t *testing.T) {
			pages := 0
			s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/repos/owner/repo" {
					return response(200, tidyRepositoryJSON()), nil
				}
				pages++
				want := fmt.Sprintf("per_page=100&page=%d", pages)
				if req.URL.RawQuery != want {
					t.Fatal(req.URL.RawQuery)
				}
				rows := []string{}
				for i := 0; i < 100; i++ {
					rows = append(rows, tidyBranchJSON(fmt.Sprintf("branch-%04d", (pages-1)*100+i), strings.Repeat("a", 40)))
				}
				r := response(200, "["+strings.Join(rows, ",")+"]")
				if proven {
					r.Header.Set("Link", fmt.Sprintf(`<https://api.github.com/repos/owner/repo/branches?per_page=100&page=10>; rel="last"`))
					if pages < 10 {
						r.Header.Add("Link", fmt.Sprintf(`<https://api.github.com/repos/owner/repo/branches?per_page=100&page=%d>; rel="next"`, pages+1))
					}
				}
				return r, nil
			})
			got, err := s.ReadBranches(context.Background())
			if pages != 10 {
				t.Fatal(pages)
			}
			if proven {
				if err != nil || len(got.Branches) != 1000 {
					t.Fatalf("proven exhaustion rejected: %d %v", len(got.Branches), err)
				}
			} else if !errors.Is(err, ErrTidyLimit) || got.Branches != nil {
				t.Fatalf("full final page accepted: %+v %v", got, err)
			}
		})
	}
}
func TestTidyBranchesFullPageNeedsCompleteTerminalSample(t *testing.T) {
	calls := 0
	s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/repos/owner/repo" {
			return response(200, tidyRepositoryJSON()), nil
		}
		calls++
		if calls == 2 {
			return response(200, `[]`), nil
		}
		rows := []string{}
		for i := 0; i < 100; i++ {
			rows = append(rows, tidyBranchJSON(fmt.Sprintf("branch-%d", i), strings.Repeat("a", 40)))
		}
		return response(200, "["+strings.Join(rows, ",")+"]"), nil
	})
	got, err := s.ReadBranches(context.Background())
	if err != nil || len(got.Branches) != 100 || calls != 2 {
		t.Fatalf("%+v %v pages=%d", got, err, calls)
	}
}
func TestTidyBranchesErrorAndRepositoryReuseNeverProveAbsence(t *testing.T) {
	for _, code := range []int{301, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/repos/owner/repo" {
					return response(200, tidyRepositoryJSON()), nil
				}
				return response(code, `[]`), nil
			})
			got, err := s.ReadBranches(context.Background())
			if err == nil || got.Branches != nil {
				t.Fatalf("absence inferred from %d: %+v %v", code, got, err)
			}
		})
	}
	for _, changed := range []string{strings.Replace(tidyRepositoryJSON(), `"id":41`, `"id":42`, 1), strings.Replace(tidyRepositoryJSON(), `"node_id":"R_repo"`, `"node_id":"R_reused"`, 1), strings.Replace(tidyRepositoryJSON(), `"default_branch":"main"`, `"default_branch":"next"`, 1)} {
		s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/repos/owner/repo" {
				t.Fatal("collection read after identity change")
			}
			return response(200, changed), nil
		})
		got, err := s.ReadBranches(context.Background())
		if !errors.Is(err, ErrTidyChanged) || got.Branches != nil {
			t.Fatalf("reused repository accepted: %+v %v", got, err)
		}
	}
}
func TestTidyBranchesUnrelatedOIDChangeAndForgedFingerprintRefuse(t *testing.T) {
	oid := strings.Repeat("a", 40)
	s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/repos/owner/repo" {
			return response(200, tidyRepositoryJSON()), nil
		}
		return response(200, "["+tidyBranchJSON("unrelated", oid)+"]"), nil
	})
	first, err := s.ReadBranches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	forged := first
	forged.Branches = []TidyBranch{}
	if err = s.RevalidateBranches(context.Background(), forged); !errors.Is(err, ErrTidyChanged) {
		t.Fatal("fingerprint alone accepted", err)
	}
	oid = strings.Repeat("b", 40)
	if err = s.RevalidateBranches(context.Background(), first); !errors.Is(err, ErrTidyChanged) {
		t.Fatal("unrelated change ignored", err)
	}
}
func TestTidyBranchesRepeatedCollectionsShareBudgetAndResultBound(t *testing.T) {
	t.Run("requests", func(t *testing.T) {
		s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/repos/owner/repo" {
				return response(200, tidyRepositoryJSON()), nil
			}
			return response(200, `[]`), nil
		})
		for i := 0; i < 64; i++ {
			if _, err := s.ReadBranches(context.Background()); err != nil {
				t.Fatalf("sample %d: %v", i, err)
			}
		}
		before := s.Budget()
		got, err := s.ReadBranches(context.Background())
		if err == nil || got.Branches != nil || s.Budget() != before {
			t.Fatalf("reset across collections: %+v %v", got, err)
		}
	})
	t.Run("normalized-result", func(t *testing.T) {
		page := 0
		s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/repos/owner/repo" {
				return response(200, tidyRepositoryJSON()), nil
			}
			page++
			rows := []string{}
			count := 100
			if page == 4 {
				count = 1
			}
			for i := 0; i < count; i++ {
				name := fmt.Sprintf("%d-%d-", page, i) + strings.Repeat("x", 4000)
				rows = append(rows, tidyBranchJSON(name, strings.Repeat("a", 40)))
			}
			return response(200, "["+strings.Join(rows, ",")+"]"), nil
		})
		got, err := s.ReadBranches(context.Background())
		if !errors.Is(err, ErrTidyLimit) || got.Branches != nil {
			t.Fatalf("oversized normalized result accepted: %d %v", len(got.Branches), err)
		}
	})
}

func TestTidyPaginationRefusesChangingLastPage(t *testing.T) {
	calls := 0
	s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/repos/owner/repo" {
			return response(200, tidyRepositoryJSON()), nil
		}
		calls++
		rows := []string{}
		for i := 0; i < 100; i++ {
			rows = append(rows, tidyBranchJSON(fmt.Sprintf("branch-%d-%d", calls, i), strings.Repeat("a", 40)))
		}
		r := response(200, "["+strings.Join(rows, ",")+"]")
		last := 3
		if calls == 2 {
			last = 4
		}
		r.Header.Set("Link", fmt.Sprintf(`<https://api.github.com/repos/owner/repo/branches?per_page=100&page=%d>; rel="next", <https://api.github.com/repos/owner/repo/branches?per_page=100&page=%d>; rel="last"`, calls+1, last))
		if calls == 3 {
			return response(200, `[]`), nil
		}
		return r, nil
	})
	_, err := s.ReadBranches(context.Background())
	if err == nil {
		t.Fatal("changed last-page total accepted")
	}
	if calls != 2 {
		t.Fatalf("failure discovered after changed page: %d", calls)
	}
}

func TestTidyBranchesRequireExactMemberNames(t *testing.T) {
	for _, path := range [][]string{{"name"}, {"commit"}, {"commit", "sha"}} {
		for _, only := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/alias-only=%v", strings.Join(path, "/"), only), func(t *testing.T) {
				raw := tidyAliasedMember(t, tidyBranchJSON("still-live", strings.Repeat("a", 40)), path, only)
				s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
					if req.URL.Path == "/repos/owner/repo" {
						return response(200, tidyRepositoryJSON()), nil
					}
					return response(200, "["+raw+"]"), nil
				})
				if got, err := s.ReadBranches(context.Background()); err == nil || got.Branches != nil {
					t.Fatalf("aliased branch inventory accepted: %+v %v", got, err)
				}
			})
		}
	}
}
func TestTidyCaseAliasCannotHideLiveBranch(t *testing.T) {
	raw := fmt.Sprintf(`[{"name":"still-live","NAME":"other","commit":{"sha":%q}}]`, strings.Repeat("a", 40))
	s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/repos/owner/repo" {
			return response(200, tidyRepositoryJSON()), nil
		}
		return response(200, raw), nil
	})
	if got, err := s.ReadBranches(context.Background()); err == nil || got.Branches != nil {
		t.Fatalf("live branch hidden by alias: %+v %v", got, err)
	}
}

func TestTidyUnrelatedProviderMembersRemainAllowed(t *testing.T) {
	head := strings.Repeat("a", 40)
	extra := map[string]any{"ID": 99, "MERGED": false, "NAME": "unrelated", "nested": map[string]any{"SHA": "ignored"}}
	repo := mutateTidyJSON(t, tidyRepositoryJSON(), []string{"provider_metadata"}, extra, false)
	pr := tidyPRJSON(7, head, "deleted-stack")
	for _, path := range [][]string{{"provider_metadata"}, {"head", "provider_metadata"}, {"base", "provider_metadata"}, {"head", "repo", "provider_metadata"}, {"base", "repo", "provider_metadata"}} {
		pr = mutateTidyJSON(t, pr, path, extra, false)
	}
	branch := mutateTidyJSON(t, tidyBranchJSON("still-live", head), []string{"provider_metadata"}, extra, false)
	branch = mutateTidyJSON(t, branch, []string{"commit", "provider_metadata"}, extra, false)
	s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/repos/owner/repo":
			return response(200, repo), nil
		case "/repos/owner/repo/pulls":
			return response(200, "["+pr+"]"), nil
		case "/repos/owner/repo/pulls/7":
			return response(200, pr), nil
		default:
			return response(200, "["+branch+"]"), nil
		}
	})
	proof, err := s.DiscoverPullRequests(context.Background(), []string{head}, nil)
	if err != nil || len(proof) != 1 || proof[0].PullRequest == nil {
		t.Fatalf("unrelated provider data rejected: %+v %v", proof, err)
	}
	advertisement, err := s.ReadBranches(context.Background())
	if err != nil || len(advertisement.Branches) != 1 || advertisement.Branches[0].Name != "still-live" {
		t.Fatalf("unrelated branch data rejected: %+v %v", advertisement, err)
	}
}
