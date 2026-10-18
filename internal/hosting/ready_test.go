// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/readiness"
)

const collectionPR = "/repos/o/r/pulls/7"
const collectionOpen = "/repos/o/r/pulls?state=open&per_page=100&page=1"
const collectionFiles = "/repos/o/r/pulls/7/files?per_page=100&page=1"
const collectionStatuses = "/repos/o/r/commits/" + readySHA + "/statuses?per_page=100&page=1"
const collectionReport = "/repos/o/r/issues/comments/9"

func TestReadyPreservesExistingHostingResultAndSelectors(t *testing.T) {
	for _, kind := range []string{"repo", "pr", "ci"} {
		number := 42
		if kind == "repo" {
			number = 0
		}
		if err := ValidateOptions(Options{Kind: kind, Number: number}); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(Result{})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(result))
	for key := range result {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"backend", "boundary", "candidates", "checks", "diagnostics", "identity", "kind", "observed_at", "pull_request", "repository", "schema_version", "status", "statuses"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("existing hosting result fields changed: %v", keys)
	}
	if err := ValidateOptions(Options{Kind: "ready-check", Number: 42}); err == nil {
		t.Fatal("existing inspection must retain its closed kind set")
	}
}

type collectionFixture struct {
	t                *testing.T
	lockPath, target string
	lock             readiness.LockData
	routes           map[string]string
	calls            map[string]int
	change           func(string, int, string) (int, string)
	s                *readySources
}

func newCollectionFixture(t *testing.T) *collectionFixture {
	t.Helper()
	l, policy, _ := policyLoaderFixture(t)
	f := &collectionFixture{t: t, lock: l, target: t.TempDir(), routes: map[string]string{}, calls: map[string]int{}}
	f.lockPath = loaderLockFile(t, loaderJSON(t, l))
	ref := func(sha, branch string) any {
		return map[string]any{"sha": sha, "ref": branch, "repo": map[string]any{"id": 1, "full_name": "o/r"}}
	}
	pr := map[string]any{"id": 11, "number": 7, "state": "open", "merged": false, "draft": true, "head": ref(readySHA, "topic"), "base": ref(readyBaseSHA, "main"), "changed_files": 1, "mergeable": true, "rebaseable": true, "mergeable_state": "clean"}
	f.routes[collectionPR] = string(loaderJSON(t, pr))
	f.routes[collectionOpen] = "[" + f.routes[collectionPR] + "]"
	f.routes[collectionFiles] = `[{"filename":"a.go","status":"modified"}]`
	r := readiness.ReportData{SchemaVersion: 1, ReportID: "public-review", RepositoryID: 1, Number: 7, HeadSHA: readySHA, BaseSHA: readyBaseSHA, PolicyCommit: readySHA, PolicySHA256: document(policy).Digest, Scope: "full-pr", FilesSHA256: document([]byte("4:a.go0:8:modified")).Digest, Verdict: "clear", Reviewer: "public label", Model: readiness.ReportModel{ID: "configured-review-model", Effort: "high", Evidence: "host-configured"}, Independence: readiness.Independence{Kind: "declared-execution", SeparateAuthor: true, SeparateContext: true}, Rounds: []readiness.Round{{Number: 1, HeadSHA: readySHA, Verdict: "clear", Findings: []string{}}}, Findings: []readiness.Finding{}, Blockers: []string{}}
	body := string(loaderJSON(t, r))
	f.routes[collectionReport] = string(loaderJSON(t, map[string]any{"id": 9, "user": map[string]any{"id": 456}, "body": body, "issue_url": "https://api.github.com/repos/o/r/issues/7", "updated_at": "2026-10-18T00:00:00Z"}))
	f.routes[collectionStatuses] = string(loaderJSON(t, []any{map[string]any{"id": 3, "creator": map[string]any{"id": 456}, "context": "review/independent", "state": "success", "created_at": "2026-10-18T00:00:00Z", "updated_at": "2026-10-18T00:00:00Z", "target_url": "https://github.com/o/r/pull/7#issuecomment-9", "description": "stw-review-v1 sha256=" + document([]byte(body)).Digest}}))
	for _, x := range []struct {
		path, name string
		id         int
	}{{"/repos/o/r", "o/r", 1}, {"/repos/policies/r", "policies/r", 2}} {
		f.routes[x.path] = fmt.Sprintf(`{"id":%d,"full_name":%q,"default_branch":"main","allow_rebase_merge":true,"allow_merge_commit":false,"allow_squash_merge":true}`, x.id, x.name)
	}
	f.routes["/repos/policies/r/contents/.stw/ready-policy.json?ref="+readySHA] = fmt.Sprintf(`{"type":"file","encoding":"base64","size":%d,"content":%q}`, len(policy), base64.StdEncoding.EncodeToString(policy))
	f.s = readyFixture(t, func(r *http.Request) (int, string) {
		path := r.URL.RequestURI()
		f.calls[path]++
		raw, ok := f.routes[path]
		if !ok {
			t.Errorf("unapproved or unexpected read: %s", path)
			return 500, `{}`
		}
		if f.change != nil {
			return f.change(path, f.calls[path], raw)
		}
		return 200, raw
	})
	return f
}
func (f *collectionFixture) collect() readyReviewCollection {
	return collectReadyReview(f.target, f.lockPath, 7, f.s)
}
func collectionReason(c readiness.Component, code string) bool {
	for _, r := range c.Reasons {
		if r.Code == code {
			return true
		}
	}
	return false
}
func requireCollectionNonpass(t *testing.T, got readyReviewCollection, code string) {
	t.Helper()
	if got.Review.State == "pass" || got.Review.State == "" || code != "" && !collectionReason(got.Review, code) {
		t.Fatalf("unexpected projection: %+v", got)
	}
}
func mutateCollectionJSON(t *testing.T, raw string, fn func(map[string]any)) string {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	fn(v)
	return string(loaderJSON(t, v))
}

func TestReadyCollectionAuthenticStableCallerReview(t *testing.T) {
	f := newCollectionFixture(t)
	got := f.collect()
	if got.Review.State != "pass" || !got.Review.Complete || !got.Complete || got.Policy.Authority != "caller-supplied" || got.Evidence.RuntimeAttestation != "unavailable" || !got.Evidence.Singleton || got.Requests != 15 {
		t.Fatalf("%+v", got)
	}
	if f.calls[collectionPR] != 2 || f.calls[collectionOpen] != 2 || f.calls[collectionStatuses] != 2 || f.calls[collectionReport] != 2 || f.calls[collectionFiles] != 1 {
		t.Fatalf("resamples: %v", f.calls)
	}
	accounted := got.PolicyRequests
	for _, src := range got.Sources {
		accounted += src.Requests
	}
	if accounted != got.Requests {
		t.Fatalf("unaccounted requests: %d != %d", accounted, got.Requests)
	}
	if strings.Contains(fmt.Sprint(got), "public label") || strings.Contains(fmt.Sprint(got), "fixture-token") {
		t.Fatal("raw/private data escaped projection")
	}
}
func TestReadyCollectionIncompleteSourcesNeverPass(t *testing.T) {
	for _, path := range []string{collectionPR, collectionOpen, collectionFiles, collectionStatuses, collectionReport, "/repos/policies/r"} {
		for _, status := range []int{403, 404, 500} {
			t.Run(fmt.Sprintf("%s-%d", path, status), func(t *testing.T) {
				f := newCollectionFixture(t)
				f.change = func(p string, n int, raw string) (int, string) {
					if p == path {
						return status, `secret response`
					}
					return 200, raw
				}
				requireCollectionNonpass(t, f.collect(), "")
			})
		}
	}
}
func TestReadyCollectionMalformedAndUntrustedProof(t *testing.T) {
	for _, kind := range []string{"missing-creator", "missing-author", "foreign-creator", "foreign-author", "wrong-comment-issue", "bad-locator", "newer-foreign", "wrong-files"} {
		t.Run(kind, func(t *testing.T) {
			f := newCollectionFixture(t)
			switch kind {
			case "missing-creator":
				f.routes[collectionStatuses] = strings.Replace(f.routes[collectionStatuses], `"creator":{"id":456},`, "", 1)
			case "foreign-creator":
				f.routes[collectionStatuses] = strings.Replace(f.routes[collectionStatuses], `"id":456`, `"id":999`, 1)
			case "missing-author":
				f.routes[collectionReport] = strings.Replace(f.routes[collectionReport], `,"user":{"id":456}`, "", 1)
			case "foreign-author":
				f.routes[collectionReport] = strings.Replace(f.routes[collectionReport], `"id":456`, `"id":999`, 1)
			case "wrong-comment-issue":
				f.routes[collectionReport] = strings.Replace(f.routes[collectionReport], "issues/7", "issues/8", 1)
			case "bad-locator":
				f.routes[collectionStatuses] = strings.Replace(f.routes[collectionStatuses], "https://github.com/o/r/pull/7", "https://attacker.invalid/o/r/pull/7", 1)
			case "newer-foreign":
				newer := strings.Replace(strings.Trim(f.routes[collectionStatuses], "[]"), `"id":3,`, `"id":4,`, 1)
				newer = strings.Replace(newer, `"id":456`, `"id":999`, 1)
				f.routes[collectionStatuses] = strings.TrimSuffix(f.routes[collectionStatuses], "]") + "," + newer + "]"
			case "wrong-files":
				f.routes[collectionFiles] = `[]`
			}
			requireCollectionNonpass(t, f.collect(), "")
			if kind == "bad-locator" && f.calls[collectionReport] != 0 {
				t.Fatal("invalid locator caused report fetch")
			}
		})
	}
}

func TestReadyCollectionDeletedForkKeepsNullableRepositoryIdentity(t *testing.T) {
	f := newCollectionFixture(t)
	f.routes[collectionPR] = mutateCollectionJSON(t, f.routes[collectionPR], func(v map[string]any) { v["head"].(map[string]any)["repo"] = nil })
	f.routes[collectionOpen] = "[" + f.routes[collectionPR] + "]"
	got := f.collect()
	if got.Review.State != "pass" || !got.Review.Complete || got.PullRequest.HeadRepositoryID != nil {
		t.Fatalf("nullable head repository changed exact-head review semantics: %+v", got)
	}
}
func TestReadyCollectionSingletonIncludesDraftAndOtherBases(t *testing.T) {
	f := newCollectionFixture(t)
	other := mutateCollectionJSON(t, f.routes[collectionPR], func(v map[string]any) { v["id"] = 12; v["number"] = 8; v["base"].(map[string]any)["ref"] = "other" })
	f.routes[collectionOpen] = "[" + f.routes[collectionPR] + "," + other + "]"
	requireCollectionNonpass(t, f.collect(), "shared-head-ambiguous")
}
func TestReadyCollectionFinalDriftNeverPasses(t *testing.T) {
	for _, kind := range []string{"head", "base", "head-ref", "draft", "pr-id", "association", "status", "status-time", "report-body", "report-author", "report-edit", "lock-whitespace", "lock-transition", "lock-generation", "final-denied"} {
		t.Run(kind, func(t *testing.T) {
			f := newCollectionFixture(t)
			f.change = func(path string, n int, raw string) (int, string) {
				if n != 2 {
					return 200, raw
				}
				switch kind {
				case "head", "base", "head-ref", "draft", "pr-id":
					if path == collectionPR {
						raw = mutateCollectionJSON(t, raw, func(v map[string]any) {
							switch kind {
							case "head", "base":
								v[kind].(map[string]any)["sha"] = strings.Repeat("c", 40)
							case "head-ref":
								v["head"].(map[string]any)["ref"] = "moved"
							case "draft":
								v["draft"] = false
							case "pr-id":
								v["id"] = 12
							}
						})
					}
				case "association":
					if path == collectionOpen {
						other := mutateCollectionJSON(t, f.routes[collectionPR], func(v map[string]any) { v["id"] = 12; v["number"] = 8 })
						raw = strings.TrimSuffix(raw, "]") + "," + other + "]"
					}
				case "status":
					if path == collectionStatuses {
						raw = strings.Replace(raw, `"id":3,`, `"id":4,`, 1)
					}
				case "status-time":
					if path == collectionStatuses {
						raw = strings.ReplaceAll(raw, "2026-10-18T00:00:00Z", "2026-10-18T00:00:01Z")
					}
				case "report-body":
					if path == collectionReport {
						raw = mutateCollectionJSON(t, raw, func(v map[string]any) { v["body"] = v["body"].(string) + " " })
					}
				case "report-author":
					if path == collectionReport {
						raw = strings.Replace(raw, `"id":456`, `"id":999`, 1)
					}
				case "report-edit":
					if path == collectionReport {
						raw = strings.Replace(raw, "00:00:00Z", "00:00:01Z", 1)
					}
				case "lock-whitespace", "lock-transition", "lock-generation":
					if path == collectionPR {
						l := f.lock
						if kind == "lock-transition" {
							l.Transition = transitionFixture(l)
						}
						if kind == "lock-generation" {
							l.Generation++
						}
						b := loaderJSON(t, l)
						if kind == "lock-whitespace" {
							b = append(b, ' ')
						}
						if err := os.WriteFile(f.lockPath, b, 0600); err != nil {
							t.Fatal(err)
						}
					}
				case "final-denied":
					if path == collectionReport {
						return 403, `{}`
					}
				}
				return 200, raw
			}
			got := f.collect()
			requireCollectionNonpass(t, got, "")
			if got.Review.Complete || got.Complete {
				t.Fatalf("drift kept completeness: %+v", got)
			}
		})
	}
}
func TestReadyCollectionBoundsCancellationAndInstalledRefusal(t *testing.T) {
	for _, kind := range []string{"request-limit", "deadline", "canceled", "cancel-after-last-read", "installed"} {
		t.Run(kind, func(t *testing.T) {
			f := newCollectionFixture(t)
			switch kind {
			case "request-limit":
				f.s.budget.requests = 79
			case "deadline":
				f.s.budget.started = time.Now().Add(-31 * time.Second)
			case "canceled":
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				f.s.ctx = ctx
			case "cancel-after-last-read":
				ctx, cancel := context.WithCancel(context.Background())
				f.s.ctx = ctx
				f.change = func(path string, n int, raw string) (int, string) {
					if path == collectionReport && n == 2 {
						cancel()
					}
					return 200, raw
				}
			case "installed":
				l := f.lock
				l.Deployment.Profile = "app"
				env := "attacker"
				id := int64(123)
				l.Deployment.Environment = &env
				l.Deployment.PublisherAppID = &id
				if err := os.WriteFile(f.lockPath, loaderJSON(t, l), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got := f.collect()
			requireCollectionNonpass(t, got, "")
			if kind == "installed" && len(f.calls) != 0 {
				t.Fatal("installed candidate routed HTTP")
			}
			if f.s.budget.requests > 80 {
				t.Fatal("request ceiling exceeded")
			}
		})
	}
}

func (f *collectionFixture) setFile(filename, previous string) {
	f.t.Helper()
	status := "modified"
	row := map[string]any{"filename": filename, "status": status}
	if previous != "" {
		status = "renamed"
		row["status"] = status
		row["previous_filename"] = previous
	}
	f.routes[collectionFiles] = string(loaderJSON(f.t, []any{row}))
	var comment map[string]any
	if err := json.Unmarshal([]byte(f.routes[collectionReport]), &comment); err != nil {
		f.t.Fatal(err)
	}
	var report readiness.ReportData
	if err := json.Unmarshal([]byte(comment["body"].(string)), &report); err != nil {
		f.t.Fatal(err)
	}
	framed := fmt.Sprintf("%d:%s%d:%s%d:%s", len(filename), filename, len(previous), previous, len(status), status)
	report.FilesSHA256 = document([]byte(framed)).Digest
	body := string(loaderJSON(f.t, report))
	comment["body"] = body
	f.routes[collectionReport] = string(loaderJSON(f.t, comment))
	var statuses []map[string]any
	if err := json.Unmarshal([]byte(f.routes[collectionStatuses]), &statuses); err != nil {
		f.t.Fatal(err)
	}
	statuses[0]["description"] = "stw-review-v1 sha256=" + document([]byte(body)).Digest
	f.routes[collectionStatuses] = string(loaderJSON(f.t, statuses))
}

func TestReadyCollectionBaselineRootsCannotApproveThemselves(t *testing.T) {
	for _, path := range []string{".github/workflows/new.yml", ".stw/ready-policy.json", "scripts/review-evidence.py"} {
		for _, rename := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-rename=%v", path, rename), func(t *testing.T) {
				f := newCollectionFixture(t)
				if rename {
					f.setFile("ordinary.go", path)
				} else {
					f.setFile(path, "")
				}
				l := f.lock
				l.Transition = transitionFixture(l)
				if err := os.WriteFile(f.lockPath, loaderJSON(t, l), 0600); err != nil {
					t.Fatal(err)
				}
				got := f.collect()
				requireCollectionNonpass(t, got, "policy-transition-required")
				if got.Policy.Commit != f.lock.PolicySource.Commit || got.Policy.Authority != "caller-supplied" {
					t.Fatal("proposed transition replaced baseline")
				}
				if len(got.Limitations) < 3 {
					t.Fatal("closure/installed limits omitted")
				}
			})
		}
	}
	for _, path := range []string{".github/workflows-other/ordinary.go", "scripts/review-evidence.py.extra", "ordinary.go"} {
		t.Run(path, func(t *testing.T) {
			f := newCollectionFixture(t)
			f.setFile(path, "")
			got := f.collect()
			if got.Review.State != "pass" {
				t.Fatalf("literal-root positive control failed: %+v", got)
			}
		})
	}
}

func TestReadyCollectionUnrelatedCIAndEnumerationOrderDoNotAffectReview(t *testing.T) {
	f := newCollectionFixture(t)
	other := mutateCollectionJSON(t, f.routes[collectionPR], func(v map[string]any) {
		v["id"] = 12
		v["number"] = 8
		v["head"].(map[string]any)["sha"] = strings.Repeat("c", 40)
	})
	f.routes[collectionOpen] = "[" + f.routes[collectionPR] + "," + other + "]"
	f.change = func(path string, n int, raw string) (int, string) {
		if n == 2 {
			switch path {
			case collectionOpen:
				raw = "[" + other + "," + f.routes[collectionPR] + "]"
			case collectionStatuses:
				var rows []map[string]any
				if err := json.Unmarshal([]byte(raw), &rows); err != nil {
					t.Fatal(err)
				}
				ci := map[string]any{}
				for k, v := range rows[0] {
					ci[k] = v
				}
				ci["id"] = 4
				ci["context"] = "unrelated-ci"
				ci["state"] = "pending"
				raw = string(loaderJSON(t, append([]map[string]any{ci}, rows...)))
			}
		}
		return 200, raw
	}
	got := f.collect()
	if got.Review.State != "pass" || !got.Complete {
		t.Fatalf("CI/order leaked into review: %+v", got)
	}
}

func TestReadyCollectionEveryTransitionFieldDriftRevokesCompleteness(t *testing.T) {
	seed := newCollectionFixture(t)
	tr := transitionFixture(seed.lock)
	var fields map[string]any
	if err := json.Unmarshal(loaderJSON(t, tr), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 9 {
		t.Fatal("transition coverage changed")
	}
	for field := range fields {
		t.Run(field, func(t *testing.T) {
			f := newCollectionFixture(t)
			l := f.lock
			l.Transition = transitionFixture(l)
			if err := os.WriteFile(f.lockPath, loaderJSON(t, l), 0600); err != nil {
				t.Fatal(err)
			}
			f.change = func(path string, n int, raw string) (int, string) {
				if path == collectionPR && n == 2 {
					var value map[string]any
					if err := json.Unmarshal(loaderJSON(t, l), &value); err != nil {
						t.Fatal(err)
					}
					transition := value["transition"].(map[string]any)
					switch x := transition[field].(type) {
					case float64:
						transition[field] = x + 1
					case string:
						transition[field] = strings.Repeat("4", len(x))
					}
					if err := os.WriteFile(f.lockPath, loaderJSON(t, value), 0600); err != nil {
						t.Fatal(err)
					}
				}
				return 200, raw
			}
			got := f.collect()
			requireCollectionNonpass(t, got, "")
			if got.Complete || got.Review.Complete {
				t.Fatal("transition drift retained completeness")
			}
		})
	}
}

func TestReadyCollectionClosedPRSkipsUnnecessarySources(t *testing.T) {
	f := newCollectionFixture(t)
	f.routes[collectionPR] = mutateCollectionJSON(t, f.routes[collectionPR], func(v map[string]any) { v["state"] = "closed" })
	got := f.collect()
	requireCollectionNonpass(t, got, "pull-request-closed")
	if len(f.calls) != 1 || f.calls[collectionPR] != 1 {
		t.Fatalf("unnecessary closed-PR reads: %v", f.calls)
	}
}

func (f *collectionFixture) replaceBaselinePolicy(change func(*readiness.PolicyData)) {
	f.t.Helper()
	policyPath := "/repos/policies/r/contents/.stw/ready-policy.json?ref=" + readySHA
	var envelope map[string]any
	if err := json.Unmarshal([]byte(f.routes[policyPath]), &envelope); err != nil {
		f.t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(envelope["content"].(string))
	if err != nil {
		f.t.Fatal(err)
	}
	p, err := readiness.ParsePolicy(raw)
	if err != nil {
		f.t.Fatal(err)
	}
	pv := p.Value()
	change(&pv)
	raw = loaderJSON(f.t, pv)
	envelope["content"] = base64.StdEncoding.EncodeToString(raw)
	envelope["size"] = len(raw)
	f.routes[policyPath] = string(loaderJSON(f.t, envelope))
	f.lock.PolicySource.SHA256 = document(raw).Digest
	if err := os.WriteFile(f.lockPath, loaderJSON(f.t, f.lock), 0600); err != nil {
		f.t.Fatal(err)
	}
	var comment map[string]any
	if err := json.Unmarshal([]byte(f.routes[collectionReport]), &comment); err != nil {
		f.t.Fatal(err)
	}
	var report readiness.ReportData
	if err := json.Unmarshal([]byte(comment["body"].(string)), &report); err != nil {
		f.t.Fatal(err)
	}
	report.PolicySHA256 = f.lock.PolicySource.SHA256
	body := string(loaderJSON(f.t, report))
	comment["body"] = body
	f.routes[collectionReport] = string(loaderJSON(f.t, comment))
	var statuses []map[string]any
	if err := json.Unmarshal([]byte(f.routes[collectionStatuses]), &statuses); err != nil {
		f.t.Fatal(err)
	}
	statuses[0]["description"] = "stw-review-v1 sha256=" + document([]byte(body)).Digest
	f.routes[collectionStatuses] = string(loaderJSON(f.t, statuses))
}

func TestReadyCollectionGenericProjectContextAndIssuerPins(t *testing.T) {
	f := newCollectionFixture(t)
	f.replaceBaselinePolicy(func(p *readiness.PolicyData) {
		p.Review.Context = "project-specific-review"
		p.Review.StatusCreatorIDs = []int64{714}
		p.Review.ReportAuthorIDs = []int64{715}
	})
	f.routes[collectionStatuses] = strings.ReplaceAll(f.routes[collectionStatuses], "review/independent", "project-specific-review")
	f.routes[collectionStatuses] = strings.Replace(f.routes[collectionStatuses], `"id":456`, `"id":714`, 1)
	f.routes[collectionReport] = strings.Replace(f.routes[collectionReport], `"id":456`, `"id":715`, 1)
	got := f.collect()
	if got.Review.State != "pass" || !got.Review.Complete {
		t.Fatalf("project trust values were hardcoded: %+v", got)
	}
}

func TestReadyCollectionAdditionalBaselineRootsAndInvalidRules(t *testing.T) {
	for _, root := range []string{"private-checker/", "../untrusted/", ""} {
		t.Run(root, func(t *testing.T) {
			f := newCollectionFixture(t)
			f.replaceBaselinePolicy(func(p *readiness.PolicyData) { p.TrustRoots = []string{root} })
			f.setFile("private-checker/auth.go", "")
			code := "policy-transition-required"
			if root != "private-checker/" {
				code = "policy-invalid"
			}
			if root == "" { // Shape-invalid policy must fail before evaluating its roots.
				requireCollectionNonpass(t, f.collect(), "")
				return
			}
			requireCollectionNonpass(t, f.collect(), code)
		})
	}
}

func TestReadyCollectionFullAssociationPageBoundNeverProvesSingleton(t *testing.T) {
	f := newCollectionFixture(t)
	for page := 1; page <= 10; page++ {
		rows := []map[string]any{}
		for i := 0; i < 100; i++ {
			var pr map[string]any
			if err := json.Unmarshal([]byte(f.routes[collectionPR]), &pr); err != nil {
				t.Fatal(err)
			}
			pr["id"] = page*100 + i + 100
			pr["number"] = page*100 + i + 100
			pr["head"].(map[string]any)["sha"] = strings.Repeat("c", 40)
			if page == 1 && i == 0 {
				pr["id"] = 11
				pr["number"] = 7
				pr["head"].(map[string]any)["sha"] = readySHA
			}
			rows = append(rows, pr)
		}
		f.routes[fmt.Sprintf("/repos/o/r/pulls?state=open&per_page=100&page=%d", page)] = string(loaderJSON(t, rows))
	}
	got := f.collect()
	requireCollectionNonpass(t, got, "")
	if got.Review.Complete || got.Complete || got.Evidence.Singleton {
		t.Fatal("bounded prefix became a singleton proof")
	}
}

func TestReadyCollectionConstraintOnlyChangesPreserveReview(t *testing.T) {
	for _, field := range []string{"mergeable_state", "mergeable", "rebaseable"} {
		t.Run(field, func(t *testing.T) {
			f := newCollectionFixture(t)
			f.change = func(path string, n int, raw string) (int, string) {
				if path == collectionPR && n == 2 {
					raw = mutateCollectionJSON(t, raw, func(v map[string]any) {
						if field == "mergeable_state" {
							v[field] = "blocked"
						} else {
							v[field] = false
						}
					})
				}
				return 200, raw
			}
			got := f.collect()
			if got.Review.State != "pass" || !got.Review.Complete || !got.Complete || !got.Evidence.Singleton {
				t.Fatalf("constraint-only computation invalidated independent review: %+v", got)
			}
		})
	}
}

func TestReadyCollectionEnumerationCrossBindsDetailIdentity(t *testing.T) {
	for _, field := range []string{"id", "base-sha", "base-ref", "head-ref", "draft", "head-repository", "head-repository-null", "changed-files"} {
		for _, phase := range []string{"initial", "final"} {
			t.Run(field+"/"+phase, func(t *testing.T) {
				f := newCollectionFixture(t)
				f.change = func(path string, n int, raw string) (int, string) {
					if phase == "final" && n != 2 {
						return 200, raw
					}
					// changed_files exists only in detail responses. It is still
					// part of initial/final review scope, separate from constraints.
					if field == "changed-files" {
						if path == collectionPR && n == 2 {
							raw = mutateCollectionJSON(t, raw, func(v map[string]any) { v["changed_files"] = 2 })
						}
					} else if path == collectionOpen {
						var rows []map[string]any
						if err := json.Unmarshal([]byte(raw), &rows); err != nil {
							t.Fatal(err)
						}
						v := rows[0]
						switch field {
						case "id":
							v["id"] = 999
						case "base-sha":
							v["base"].(map[string]any)["sha"] = strings.Repeat("c", 40)
						case "base-ref":
							v["base"].(map[string]any)["ref"] = "other"
						case "head-ref":
							v["head"].(map[string]any)["ref"] = "other"
						case "draft":
							v["draft"] = false
						case "head-repository":
							v["head"].(map[string]any)["repo"] = map[string]any{"id": 999, "full_name": "other/fork"}
						case "head-repository-null":
							v["head"].(map[string]any)["repo"] = nil
						}
						raw = string(loaderJSON(t, rows))
					}
					return 200, raw
				}
				got := f.collect()
				requireCollectionNonpass(t, got, "")
				if got.Complete || got.Review.Complete {
					t.Fatalf("inconsistent identity retained complete proof: %+v", got)
				}
				if field != "changed-files" && got.Evidence.Singleton {
					t.Fatal("contradictory detail/list identity claimed a proven singleton")
				}
			})
		}
	}
}
