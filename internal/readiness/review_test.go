// SPDX-License-Identifier: EUPL-1.2
package readiness

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const reviewPolicyJSON = `{"schema_version":1,"policy_id":"fixture","version":1,"repository":{"host":"github.com","id":123,"full_name":"example/project","base_ref":"main"},"review":{"context":"independent/check","status_creator_ids":[456],"report_author_ids":[457],"tiers":[{"name":"ordinary","rank":1,"models":[{"id":"configured","effort":"medium"}]},{"name":"strong","rank":2,"models":[{"id":"strong-configured","effort":"high"}]}],"default_tier":"ordinary","path_tiers":[{"prefix":"secure/","tier":"strong"}],"max_rounds":4,"independence":"declared-execution","allow_low_followups":false},"ci":[],"merge":{"method":"rebase","require_strict":true},"deployment":{"profile":"none","publisher_app_id":null},"trust_roots":[".github/workflows/"]}`
const reviewHead = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
const reviewBase = "ffffffffffffffffffffffffffffffffffffffff"
const reviewCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func reviewFixture(t *testing.T) (Policy, ReviewSample, ReportData) {
	t.Helper()
	p, err := ParsePolicy([]byte(reviewPolicyJSON))
	if err != nil {
		t.Fatal(err)
	}
	r := ReportData{SchemaVersion: 1, ReportID: "public-review", RepositoryID: 123, Number: 42, HeadSHA: reviewHead, BaseSHA: reviewBase, PolicyCommit: reviewCommit, PolicySHA256: p.SHA256(), Scope: "full-pr", FilesSHA256: fmt.Sprintf("%x", sha256.Sum256(nil)), Verdict: "clear", Reviewer: "public reviewer", Model: ReportModel{ID: "configured", Effort: "medium", Evidence: "host-configured"}, Independence: Independence{Kind: "declared-execution", SeparateAuthor: true, SeparateContext: true}, Rounds: []Round{{Number: 1, HeadSHA: reviewHead, Verdict: "clear", Findings: []string{}}}, Findings: []Finding{}, Blockers: []string{}}
	s := ReviewSample{Repository: p.Value().Repository, PullRequest: PullRequest{Number: 42, HeadSHA: reviewHead, BaseSHA: reviewBase, BaseRef: "main", BaseRepositoryID: 123, State: "open"}, Policy: PolicyBinding{Commit: reviewCommit, SHA256: p.SHA256()}, WebOrigin: "https://github.com", APIOrigin: "https://api.github.com", AssociatedOpenPRs: []int64{42}, ChangedFiles: 0, Files: []ReviewFile{}, Statuses: []ReviewStatus{{ID: 10, CreatorID: 456, Context: "independent/check", State: "success", TargetURL: "https://github.com/example/project/pull/42#issuecomment-88"}}, Comment: ReviewComment{ID: 88, AuthorID: 457, IssueURL: "https://api.github.com/repos/example/project/issues/42"}, Sources: []ReviewSource{{Name: "full-statuses", Selector: reviewHead, Outcome: "ok", Requests: 1, Complete: true, Host: "github.com", Repository: "example/project"}, {Name: "files", Selector: "42", Outcome: "ok", Requests: 1, Complete: true, Host: "github.com", Repository: "example/project"}, {Name: "open-prs", Outcome: "ok", Requests: 1, Complete: true, Host: "github.com", Repository: "example/project"}, {Name: "report", Selector: "88", Outcome: "ok", Requests: 1, Complete: true, Host: "github.com", Repository: "example/project"}}, Complete: true, Stable: true}
	bindReport(t, &s, r)
	return p, s, r
}
func bindReport(t *testing.T, s *ReviewSample, r ReportData) {
	t.Helper()
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	s.Comment.Body = string(raw)
	s.Statuses[0].Description = fmt.Sprintf("stw-review-v1 sha256=%x", sha256.Sum256(raw))
}
func reviewReason(c Component, code string) bool {
	for _, r := range c.Reasons {
		if r.Code == code {
			return true
		}
	}
	return false
}
func requireReview(t *testing.T, p Policy, s ReviewSample, state, reason string) ReviewEvidence {
	t.Helper()
	c, e := EvaluateReview(p, s)
	if c.State != state || reason != "" && !reviewReason(c, reason) {
		t.Fatalf("got %#v, want %s / %s", c, state, reason)
	}
	return e
}

func TestReviewAuthenticConfiguredDeclaration(t *testing.T) {
	p, s, _ := reviewFixture(t)
	before, _ := json.Marshal(s)
	e := requireReview(t, p, s, "pass", "")
	after, _ := json.Marshal(s)
	if string(before) != string(after) {
		t.Fatal("mutated input")
	}
	if e.RuntimeAttestation != "unavailable" || e.ConfiguredModel.ID != "configured" || e.DeclaredIndependence.Kind != "declared-execution" || e.Tier != "ordinary" {
		t.Fatalf("proof inflation: %#v", e)
	}
	if strings.Contains(fmt.Sprintf("%+v", e), "public reviewer") {
		t.Fatal("unneeded reviewer label copied")
	}
}
func TestReviewNewestBeforeIssuerSelection(t *testing.T) {
	for _, state := range []string{"success", "failure", "pending"} {
		t.Run(state, func(t *testing.T) {
			p, s, _ := reviewFixture(t)
			newer := s.Statuses[0]
			newer.ID++
			newer.CreatorID = 999
			newer.State = state
			s.Statuses = append(s.Statuses, newer)
			requireReview(t, p, s, "fail", "review-issuer-untrusted")
		})
	}
}
func TestReviewAuthenticationMutations(t *testing.T) {
	cases := []struct {
		name, reason string
		change       func(*ReviewSample)
	}{
		{"creator", "review-issuer-untrusted", func(s *ReviewSample) { s.Statuses[0].CreatorID = 999 }},
		{"author", "review-issuer-untrusted", func(s *ReviewSample) { s.Comment.AuthorID = 456 }},
		{"pending", "review-not-clear", func(s *ReviewSample) { s.Statuses[0].State = "pending" }},
		{"description", "report-binding-mismatch", func(s *ReviewSample) { s.Statuses[0].Description += " " }},
		{"edited bytes", "report-binding-mismatch", func(s *ReviewSample) { s.Comment.Body += " " }},
		{"foreign locator", "report-binding-mismatch", func(s *ReviewSample) {
			s.Statuses[0].TargetURL = "https://evil.test/example/project/pull/42#issuecomment-88"
		}},
		{"wrong PR locator", "report-binding-mismatch", func(s *ReviewSample) {
			s.Statuses[0].TargetURL = "https://github.com/example/project/pull/43#issuecomment-88"
		}},
		{"leading zero", "report-binding-mismatch", func(s *ReviewSample) {
			s.Statuses[0].TargetURL = "https://github.com/example/project/pull/42#issuecomment-088"
		}},
		{"wrong issue", "report-binding-mismatch", func(s *ReviewSample) { s.Comment.IssueURL = "https://api.github.com/repos/example/project/issues/43" }},
		{"comment ID", "report-binding-mismatch", func(s *ReviewSample) { s.Comment.ID = 89 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, s, _ := reviewFixture(t)
			tc.change(&s)
			requireReview(t, p, s, "fail", tc.reason)
		})
	}
}
func TestReviewReportBindings(t *testing.T) {
	for _, field := range []string{"repository", "PR", "head", "base", "policy commit", "policy digest", "files"} {
		t.Run(field, func(t *testing.T) {
			p, s, r := reviewFixture(t)
			switch field {
			case "repository":
				r.RepositoryID++
			case "PR":
				r.Number++
			case "head":
				r.HeadSHA = reviewBase
			case "base":
				r.BaseSHA = reviewHead
			case "policy commit":
				r.PolicyCommit = reviewBase
			case "policy digest":
				r.PolicySHA256 = strings.Repeat("0", 64)
			case "files":
				r.FilesSHA256 = strings.Repeat("0", 64)
			}
			bindReport(t, &s, r)
			requireReview(t, p, s, "fail", "review-binding-mismatch")
		})
	}
}
func TestReviewIncompleteAndAmbiguous(t *testing.T) {
	for _, which := range []string{"boundary", "drift", "source", "missing source", "duplicate status", "files count", "duplicate paths", "association", "zero association"} {
		t.Run(which, func(t *testing.T) {
			p, s, _ := reviewFixture(t)
			reason := "evidence-incomplete"
			switch which {
			case "boundary":
				s.Complete = false
			case "drift":
				s.Stable = false
				reason = "sample-changed"
			case "source":
				s.Sources[0].Outcome = "denied"
			case "missing source":
				s.Sources = s.Sources[1:]
			case "duplicate status":
				s.Statuses = append(s.Statuses, s.Statuses[0])
			case "files count":
				s.ChangedFiles = 1
			case "duplicate paths":
				s.ChangedFiles = 2
				s.Files = []ReviewFile{{Filename: "a", Status: "modified"}, {Filename: "a", Status: "removed"}}
			case "association":
				s.AssociatedOpenPRs = []int64{42, 43}
				reason = "shared-head-ambiguous"
			case "zero association":
				s.AssociatedOpenPRs = nil
				reason = "association-incomplete"
			}
			requireReview(t, p, s, "unknown", reason)
		})
	}
}
func TestReviewHistoryAndFindings(t *testing.T) {
	for _, which := range []string{"ordinal", "final head", "final verdict", "missing reference", "duplicate reference", "unreferenced", "duplicate finding", "open low", "open high", "followup disabled", "followup high", "followup missing owner", "followup whitespace evidence", "blocker", "not clear", "threshold"} {
		t.Run(which, func(t *testing.T) {
			p, s, r := reviewFixture(t)
			state, reason := "unknown", "report-history-invalid"
			f := Finding{ID: "F1", Severity: "low", State: "resolved"}
			r.Findings = []Finding{f}
			r.Rounds[0].Findings = []string{"F1"}
			switch which {
			case "ordinal":
				r.Rounds[0].Number = 2
			case "final head":
				r.Rounds[0].HeadSHA = reviewBase
			case "final verdict":
				r.Rounds[0].Verdict = "not-clear"
			case "missing reference":
				r.Rounds[0].Findings = []string{"missing"}
			case "duplicate reference":
				reason = "report-malformed"
				r.Rounds[0].Findings = []string{"F1", "F1"}
			case "unreferenced":
				r.Rounds[0].Findings = []string{}
			case "duplicate finding":
				f.Severity = "high"
				r.Findings = append(r.Findings, f)
			case "open low", "open high":
				r.Findings[0].State = "open"
				if which == "open high" {
					r.Findings[0].Severity = "high"
				}
				state, reason = "fail", "review-not-clear"
			case "followup disabled", "followup high", "followup missing owner", "followup whitespace evidence":
				r.Findings[0].State = "accepted-followup"
				owner, evidence := "owner", "public issue"
				r.Findings[0].Owner = &owner
				r.Findings[0].Evidence = &evidence
				if which != "followup disabled" {
					pd := p.Value()
					pd.Review.AllowLowFollowups = true
					p = parsedReviewPolicy(t, pd)
					s.Policy.SHA256 = p.SHA256()
					r.PolicySHA256 = p.SHA256()
				}
				if which == "followup high" {
					r.Findings[0].Severity = "high"
				}
				if which == "followup missing owner" {
					r.Findings[0].Owner = nil
				}
				if which == "followup whitespace evidence" {
					evidence = " \n"
				}
				state, reason = "fail", "review-not-clear"
			case "blocker":
				r.Blockers = []string{"remaining"}
				state, reason = "fail", "review-not-clear"
			case "not clear":
				r.Verdict = "not-clear"
				r.Rounds[0].Verdict = "not-clear"
				state, reason = "fail", "review-not-clear"
			case "threshold":
				r.Rounds = nil
				for i := int64(1); i <= 4; i++ {
					r.Rounds = append(r.Rounds, Round{Number: i, HeadSHA: reviewBase, Verdict: "not-clear", Findings: []string{"F1"}})
				}
				r.Rounds = append(r.Rounds, Round{Number: 5, HeadSHA: reviewHead, Verdict: "clear", Findings: []string{}})
				state, reason = "fail", "review-not-clear"
			}
			bindReport(t, &s, r)
			requireReview(t, p, s, state, reason)
		})
	}
}
func parsedReviewPolicy(t *testing.T, p PolicyData) Policy {
	t.Helper()
	raw, _ := json.Marshal(p)
	out, e := ParsePolicy(raw)
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func TestReviewPolicySemanticCounterexamples(t *testing.T) {
	for _, which := range []string{"duplicate ranks", "duplicate names", "default reference", "path reference", "absolute", "parent", "empty segment", "glob", "duplicate prefix", "duplicate model"} {
		t.Run(which, func(t *testing.T) {
			p, s, _ := reviewFixture(t)
			pd := p.Value()
			switch which {
			case "duplicate ranks":
				pd.Review.Tiers[1].Rank = 1
			case "duplicate names":
				pd.Review.Tiers[1].Name = "ordinary"
			case "default reference":
				pd.Review.DefaultTier = "absent"
			case "path reference":
				pd.Review.PathTiers[0].Tier = "absent"
			case "absolute":
				pd.Review.PathTiers[0].Prefix = "/secure/"
			case "parent":
				pd.Review.PathTiers[0].Prefix = "a/../secure/"
			case "empty segment":
				pd.Review.PathTiers[0].Prefix = "a//secure/"
			case "glob":
				pd.Review.PathTiers[0].Prefix = "secure/*"
			case "duplicate prefix":
				pd.Review.PathTiers = append(pd.Review.PathTiers, PathTier{Prefix: "secure/", Tier: "ordinary"})
			case "duplicate model":
				pd.Review.Tiers[1].Models = pd.Review.Tiers[0].Models
			}
			p = parsedReviewPolicy(t, pd)
			s.Policy.SHA256 = p.SHA256()
			requireReview(t, p, s, "unknown", "policy-invalid")
		})
	}
}
func TestReviewRenamedPathStrengthAndFramedDigest(t *testing.T) {
	p, s, r := reviewFixture(t)
	old := "secure/key"
	s.Files = []ReviewFile{{Filename: "docs/key", PreviousFilename: &old, Status: "renamed"}}
	s.ChangedFiles = 1
	r.FilesSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte("8:docs/key10:secure/key7:renamed")))
	bindReport(t, &s, r)
	requireReview(t, p, s, "fail", "review-tier-insufficient")
	r.Model = ReportModel{ID: "strong-configured", Effort: "high", Evidence: "host-configured"}
	bindReport(t, &s, r)
	e := requireReview(t, p, s, "pass", "")
	if e.Tier != "strong" {
		t.Fatal(e.Tier)
	}
}
func TestReviewConfiguredModelExactAndPublicOnly(t *testing.T) {
	p, s, r := reviewFixture(t)
	r.Model.Effort = "high"
	bindReport(t, &s, r)
	requireReview(t, p, s, "fail", "review-tier-insufficient")
	p, s, r = reviewFixture(t)
	r.Reviewer = "reviewer"
	bindReport(t, &s, r)
	e := requireReview(t, p, s, "pass", "")
	if !reflect.DeepEqual(e.ConfiguredModel, Model{ID: "configured", Effort: "medium"}) || e.RuntimeAttestation != "unavailable" {
		t.Fatal(e)
	}
}

func TestReviewClosedSourceVocabularyAccountsEveryRecord(t *testing.T) {
	for _, requests := range []int{1, 77} {
		t.Run(fmt.Sprint(requests), func(t *testing.T) {
			p, s, _ := reviewFixture(t)
			s.Sources = append(s.Sources, ReviewSource{Host: "github.com", Name: "pr", Repository: "example/project", Selector: "42", Outcome: "ok", Complete: true, Requests: requests})
			requireReview(t, p, s, "unknown", "evidence-incomplete")
		})
	}
}

func TestReviewPermittedLowFollowupRetainsFinding(t *testing.T) {
	p, s, r := reviewFixture(t)
	pd := p.Value()
	pd.Review.AllowLowFollowups = true
	p = parsedReviewPolicy(t, pd)
	s.Policy.SHA256 = p.SHA256()
	r.PolicySHA256 = p.SHA256()
	owner, evidence := "public owner", "public issue"
	r.Findings = []Finding{{ID: "F1", Severity: "low", State: "accepted-followup", Owner: &owner, Evidence: &evidence}}
	r.Rounds = []Round{{Number: 1, HeadSHA: reviewBase, Verdict: "not-clear", Findings: []string{"F1"}}, {Number: 2, HeadSHA: reviewHead, Verdict: "clear", Findings: []string{"F1"}}}
	bindReport(t, &s, r)
	requireReview(t, p, s, "pass", "")
}
func TestReviewScopeFramingOrderAndExactPrefix(t *testing.T) {
	// Unicode byte lengths and newline/colon paths remain raw; no JSON or path
	// normalization participates. A sibling prefix must not raise the tier.
	p, s, r := reviewFixture(t)
	s.Files = []ReviewFile{{Filename: "securely/a", Status: "modified"}, {Filename: "é\n:", Status: "added"}}
	s.ChangedFiles = 2
	r.FilesSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte("10:securely/a0:8:modified4:é\n:0:5:added")))
	bindReport(t, &s, r)
	first, e := EvaluateReview(p, s)
	if first.State != "pass" || e.Tier != "ordinary" {
		t.Fatal(first, e)
	}
	s.Files[0], s.Files[1] = s.Files[1], s.Files[0]
	second, _ := EvaluateReview(p, s)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("order-sensitive scope")
	}
}
func TestReviewMalformedPublicMetadataAndBounds(t *testing.T) {
	for _, which := range []string{"private field", "runtime proof", "model evidence", "scope", "source accounting", "source duplicate", "status limit", "file limit", "association limit", "source limit", "source selector", "source incomplete", "source request total"} {
		t.Run(which, func(t *testing.T) {
			p, s, _ := reviewFixture(t)
			code := "report-malformed"
			switch which {
			case "private field":
				s.Comment.Body = strings.TrimSuffix(s.Comment.Body, "}") + `,"session_id":"private"}`
			case "runtime proof":
				s.Comment.Body = strings.Replace(s.Comment.Body, `"declared-execution"`, `"runtime-attested"`, 1)
			case "model evidence":
				s.Comment.Body = strings.Replace(s.Comment.Body, `"host-configured"`, `"runtime-attested"`, 1)
			case "scope":
				s.Comment.Body = strings.Replace(s.Comment.Body, `"full-pr"`, `"narrowed-rebase"`, 1)
			case "source selector":
				s.Sources[0].Selector = reviewBase
				code = "evidence-incomplete"
			case "source incomplete":
				s.Sources[0].Complete = false
				code = "evidence-incomplete"
			case "source request total":
				s.Sources[0].Requests = 79
				code = "evidence-incomplete"
			case "source accounting":
				s.Sources[0].Requests = 0
				code = "evidence-incomplete"
			case "source duplicate":
				s.Sources = append(s.Sources, s.Sources[0])
				code = "evidence-incomplete"
			case "status limit":
				s.Statuses = make([]ReviewStatus, 1001)
				code = "evidence-incomplete"
			case "file limit":
				s.Files = make([]ReviewFile, 1001)
				s.ChangedFiles = 1001
				code = "evidence-incomplete"
			case "association limit":
				s.AssociatedOpenPRs = make([]int64, 1001)
				code = "evidence-incomplete"
			case "source limit":
				s.Sources = make([]ReviewSource, 81)
				code = "evidence-incomplete"
			}
			s.Statuses[0].Description = fmt.Sprintf("stw-review-v1 sha256=%x", sha256.Sum256([]byte(s.Comment.Body)))
			requireReview(t, p, s, "unknown", code)
		})
	}
}
