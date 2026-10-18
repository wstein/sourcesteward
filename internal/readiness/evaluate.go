// SPDX-License-Identifier: EUPL-1.2
package readiness

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// SourceReference names a fixed reader and a field, never an arbitrary URL.
type SourceReference struct {
	Source string `json:"source"`
	Field  string `json:"field"`
}
type Reason struct {
	Code    string            `json:"code"`
	Subject string            `json:"subject"`
	Sources []SourceReference `json:"sources"`
}
type Component struct {
	State    string            `json:"state"`
	Complete bool              `json:"complete"`
	Reasons  []Reason          `json:"reasons"`
	Evidence []SourceReference `json:"evidence"`
}
type Components struct {
	Review      Component `json:"review"`
	CI          Component `json:"ci"`
	Constraints Component `json:"constraints"`
	Deployment  Component `json:"deployment"`
}
type PullRequest struct {
	Number           int64  `json:"number"`
	HeadSHA          string `json:"head_sha"`
	BaseSHA          string `json:"base_sha"`
	HeadRef          string `json:"head_ref"`
	BaseRef          string `json:"base_ref"`
	HeadRepositoryID *int64 `json:"head_repository_id"`
	BaseRepositoryID int64  `json:"base_repository_id"`
	State            string `json:"state"`
	Merged           bool   `json:"merged"`
	Draft            bool   `json:"draft"`
}
type PolicyBinding struct {
	ID         string  `json:"id"`
	Version    int64   `json:"version"`
	Commit     string  `json:"commit"`
	SHA256     string  `json:"sha256"`
	Generation int64   `json:"generation"`
	LockSHA256 string  `json:"lock_sha256"`
	Checker    Checker `json:"checker"`
	Authority  string  `json:"authority"`
}
type ReviewEvidence struct {
	AssociatedOpenPRs    []int64      `json:"associated_open_prs"`
	Singleton            bool         `json:"singleton"`
	StatusID             *int64       `json:"status_id"`
	CommentID            *int64       `json:"comment_id"`
	ReportID             string       `json:"report_id"`
	ReportSHA256         string       `json:"report_sha256"`
	Tier                 string       `json:"tier"`
	ConfiguredModel      Model        `json:"configured_model"`
	DeclaredIndependence Independence `json:"declared_independence"`
	RuntimeAttestation   string       `json:"runtime_attestation"`
}
type GitHubReported struct {
	Mergeable      *bool  `json:"mergeable"`
	Rebaseable     *bool  `json:"rebaseable"`
	MergeableState string `json:"mergeable_state"`
}
type Source struct {
	Name                  string   `json:"name"`
	Outcome               string   `json:"outcome"`
	RequestCount          int      `json:"request_count"`
	ObservationIdentities []string `json:"observation_identities"`
	Failure               string   `json:"failure"`
}
type Usage struct {
	Requests int `json:"requests"`
	Bytes    int `json:"bytes"`
	Pages    int `json:"pages"`
	Records  int `json:"records"`
	Seconds  int `json:"seconds"`
}
type Boundary struct {
	Complete         bool     `json:"complete"`
	NetworkAttempted bool     `json:"network_attempted"`
	Budgets          Usage    `json:"budgets"`
	Usage            Usage    `json:"usage"`
	Limitations      []string `json:"limitations"`
}
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Sample is an operation-owned snapshot. Identity contains the existing native
// inspection result representation; copying it proves no native ownership or
// source authenticity. Components are projections from separately tested pure
// predicates, not report verdict strings. This foundation implements no such
// authentication, CI, constraint or deployment predicates yet.
//
// All fields are concrete values: no readers, callbacks or custom marshalers can
// enter evaluation. ObservedAt must be an operation-supplied RFC3339 UTC string.
type Sample struct {
	ObservedAt       string
	Backend          string
	Identity         json.RawMessage
	Repository       *Repository
	PullRequest      *PullRequest
	Policy           PolicyBinding
	Components       Components
	ReviewEvidence   ReviewEvidence
	GitHubReported   GitHubReported
	Sources          []Source
	Boundary         Boundary
	Diagnostics      []Diagnostic
	OperationalError bool
}
type ResultData struct {
	SchemaVersion  int             `json:"schema_version"`
	Kind           string          `json:"kind"`
	Decision       string          `json:"decision"`
	ObservedAt     string          `json:"observed_at"`
	Backend        *string         `json:"backend"`
	Identity       json.RawMessage `json:"identity"`
	Repository     *Repository     `json:"repository"`
	PullRequest    *PullRequest    `json:"pull_request"`
	Policy         PolicyBinding   `json:"policy"`
	Components     Components      `json:"components"`
	ReviewEvidence ReviewEvidence  `json:"review_evidence"`
	GitHubReported GitHubReported  `json:"github_reported"`
	Sources        []Source        `json:"sources"`
	Boundary       Boundary        `json:"boundary"`
	Diagnostics    []Diagnostic    `json:"diagnostics"`
}

// Result owns a serialized immutable snapshot. Accessors return independent
// copies. A zero Result is not a constructed assessment and has no JSON bytes.
type Result struct{ normalized string }

func (r Result) RawJSON() []byte { return []byte(r.normalized) }
func (r Result) Value() (v ResultData) {
	_ = json.Unmarshal([]byte(r.normalized), &v)
	if string(v.Identity) == "null" {
		v.Identity = nil
	}
	return v
}

// Evaluate is the shared result/reduction scaffold. It does not authenticate the
// operation-supplied component projections. Invalid operation inputs return an
// error instead of manufacturing an observation timestamp or native identity.
func Evaluate(policy Policy, sample Sample) (Result, error) {
	if !validObservedAt(sample.ObservedAt) {
		return Result{}, fmt.Errorf("observation timestamp must be operation-supplied RFC3339 UTC")
	}
	if len(policy.RawJSON()) == 0 {
		return Result{}, fmt.Errorf("parsed policy is required")
	}
	if err := validNativeSnapshot(sample); err != nil {
		return Result{}, err
	}
	if len(sample.GitHubReported.MergeableState) > 2048 || !utf8.ValidString(sample.GitHubReported.MergeableState) {
		return Result{}, fmt.Errorf("mergeable_state exceeds the contract string bound")
	}
	r := ResultData{SchemaVersion: 1, Kind: "ready-check", ObservedAt: sample.ObservedAt, Backend: nil, Identity: sample.Identity, Repository: sample.Repository, PullRequest: sample.PullRequest, Policy: sample.Policy, Components: sample.Components, ReviewEvidence: sample.ReviewEvidence, GitHubReported: sample.GitHubReported, Sources: sample.Sources, Boundary: sample.Boundary, Diagnostics: sample.Diagnostics}
	if sample.Backend != "" {
		backend := sample.Backend
		r.Backend = &backend
	}
	// Deep-copy before normalization; sorting or changing completeness must never
	// mutate an operation's evidence (including nested source-reference slices).
	raw, err := json.Marshal(r)
	if err != nil {
		return Result{}, fmt.Errorf("invalid result snapshot")
	}
	var snapshot ResultData
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return Result{}, fmt.Errorf("invalid result snapshot")
	}
	r = snapshot
	r.ReviewEvidence.RuntimeAttestation = "unavailable"
	if sample.OperationalError {
		r.Boundary.Complete = false
	}
	if sample.OperationalError || !r.Boundary.Complete {
		r.Components.Review.Complete = false
	}
	undeployed := policy.Value().Deployment.Profile == "none"
	components := []*Component{&r.Components.Review, &r.Components.CI, &r.Components.Constraints, &r.Components.Deployment}
	blocked, unknown := false, !r.Boundary.Complete
	for i, c := range components {
		allowedNotRequired := i == 3 && undeployed
		switch c.State {
		case "pass":
			if !c.Complete {
				c.State = "unknown"
			}
		case "fail", "unknown":
		case "not-required":
			if !allowedNotRequired || !c.Complete {
				c.State = "unknown"
			}
		default:
			c.State = "unknown"
		}
		blocked = blocked || c.State == "fail"
		unknown = unknown || c.State == "unknown"
		orderReasons(c.Reasons)
		orderReferences(c.Evidence)
	}
	r.Decision = "eligible"
	if unknown {
		r.Decision = "indeterminate"
	}
	if blocked {
		r.Decision = "blocked"
	}
	if sample.OperationalError {
		r.Decision = "error"
	}
	raw, err = json.Marshal(r)
	if err != nil {
		return Result{}, fmt.Errorf("invalid result snapshot")
	}
	return Result{string(raw)}, nil
}

func validNativeSnapshot(s Sample) error {
	if len(s.Identity) == 0 || string(s.Identity) == "null" {
		if s.OperationalError && (s.Backend == "" || s.Backend == "git" || s.Backend == "jj") {
			return nil
		}
		return fmt.Errorf("native identity is required except on early operational failure")
	}
	if s.Backend != "git" && s.Backend != "jj" {
		return fmt.Errorf("native identity backend must be git or jj")
	}
	// Strict JSON parsing prevents malformed or duplicate-key snapshots. This is
	// only a shape/binding check, never native resolution or source authentication.
	value, err := strictJSON(s.Identity, 1024*1024)
	if err != nil {
		return fmt.Errorf("invalid native identity snapshot")
	}
	object, ok := value.(map[string]any)
	if !ok || object["backend"] != s.Backend {
		return fmt.Errorf("native identity backend mismatch")
	}
	return nil
}

var utcObservation = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|\+00:00)$`)

// Validate the supplied civil timestamp without importing a clock-capable API.
func validObservedAt(s string) bool {
	if len(s) > 2048 || !utcObservation.MatchString(s) {
		return false
	}
	number := func(start, end int) int { n, _ := strconv.Atoi(s[start:end]); return n }
	year, month, day := number(0, 4), number(5, 7), number(8, 10)
	if month < 1 || month > 12 || number(11, 13) > 23 || number(14, 16) > 59 || number(17, 19) > 59 {
		return false
	}
	days := []int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
		days[1] = 29
	}
	return day >= 1 && day <= days[month-1]
}

// The normative family and code ordering precedes UTF-8 byte ordering of
// repeated subjects. Unknown codes are retained and sort after known codes.
var reasonCodes = []string{
	"policy-invalid", "lock-invalid", "policy-source-untrusted", "policy-digest-mismatch", "policy-transition-required", "activation-mismatch",
	"repository-mismatch", "pull-request-closed", "review-binding-mismatch", "sample-changed", "shared-head-ambiguous", "association-incomplete",
	"review-missing", "review-issuer-untrusted", "report-binding-mismatch", "report-malformed", "report-history-invalid", "report-metadata-missing", "review-tier-insufficient", "independence-unverifiable", "review-not-clear",
	"ci-missing", "ci-pending", "ci-failed", "ci-source-unknown", "ci-head-mismatch", "ci-test-merge-unsupported",
	"policy-incomplete", "constraint-unsupported", "merge-method-disallowed", "merge-conflict", "rebase-unavailable", "base-behind",
	"deployment-unverified", "deployment-pending", "deployment-failed", "deployment-stale",
	"evidence-incomplete", "source-denied", "source-malformed", "source-limit", "source-failed", "hosting-canceled", "output-limit",
}

func reasonRank(code string) int {
	for i, c := range reasonCodes {
		if c == code {
			return i
		}
	}
	return len(reasonCodes)
}
func orderReasons(reasons []Reason) {
	for i := range reasons {
		orderReferences(reasons[i].Sources)
	}
	sort.Slice(reasons, func(i, j int) bool {
		a, b := reasons[i], reasons[j]
		if x, y := reasonRank(a.Code), reasonRank(b.Code); x != y {
			return x < y
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		left, _ := json.Marshal(a.Sources)
		right, _ := json.Marshal(b.Sources)
		return string(left) < string(right)
	})
}
func orderReferences(refs []SourceReference) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Source != refs[j].Source {
			return refs[i].Source < refs[j].Source
		}
		return refs[i].Field < refs[j].Field
	})
}

// ReviewStatus preserves full status creator identity; display names confer no trust.
type ReviewStatus struct {
	ID, CreatorID                          int64
	Context, State, TargetURL, Description string
}

// ReviewComment contains the fixed comment reader's exact UTF-8 body bytes.
type ReviewComment struct {
	ID, AuthorID   int64
	IssueURL, Body string
}
type ReviewFile struct {
	Filename, Status string
	PreviousFilename *string
}

// ReviewSample is a value-only snapshot from authenticated fixed readers. Sources
// must contain exactly the complete full-statuses, files, open-prs and report
// readers; unrelated operation sources are rejected. Every supplied request is
// accounted before source selection. Hosting remains responsible for the shared
// operation-wide budget (including PR, policy, CI and final resamples), and must
// set Complete=false when any global bound is exhausted.
// Complete and Stable describe collection and its final resamples, not trust
// inferred from report declarations. Policy source/activation loading belongs to
// hosting; this predicate verifies its supplied immutable policy binding.
type ReviewSource struct {
	Host, Name, Repository, Selector, Outcome string
	Complete                                  bool
	Requests                                  int
}
type ReviewSample struct {
	Repository           Repository
	PullRequest          PullRequest
	Policy               PolicyBinding
	WebOrigin, APIOrigin string
	AssociatedOpenPRs    []int64
	ChangedFiles         int
	Files                []ReviewFile
	Statuses             []ReviewStatus
	Comment              ReviewComment
	Sources              []ReviewSource
	Complete, Stable     bool
}

// EvaluateReview is the shared policy review projection, independent of CI and
// the publisher's own check. It never performs collection or publication.
func EvaluateReview(policy Policy, sample ReviewSample) (Component, ReviewEvidence) {
	c := Component{State: "pass", Complete: true, Reasons: []Reason{}, Evidence: []SourceReference{}}
	e := ReviewEvidence{RuntimeAttestation: "unavailable", AssociatedOpenPRs: append([]int64{}, sample.AssociatedOpenPRs...)}
	add := func(state, code, source, field string) {
		c.Reasons = append(c.Reasons, Reason{Code: code, Subject: field, Sources: []SourceReference{{Source: source, Field: field}}})
		if state == "fail" || c.State == "pass" {
			c.State = state
		}
		if state == "unknown" {
			c.Complete = false
		}
	}
	finish := func() (Component, ReviewEvidence) { orderReasons(c.Reasons); orderReferences(c.Evidence); return c, e }
	if len(policy.RawJSON()) == 0 || !validReviewPolicy(policy.Value().Review) {
		add("unknown", "policy-invalid", "policy", "review")
		return finish()
	}
	p := policy.Value()
	if len(sample.Sources) > 80 || len(sample.Files) > 1000 || len(sample.Statuses) > 1000 || len(sample.AssociatedOpenPRs) > 1000 {
		add("unknown", "evidence-incomplete", "collection", "bounds")
		return finish()
	}
	if !sample.Complete {
		add("unknown", "evidence-incomplete", "collection", "complete")
	}
	if !sample.Stable {
		add("unknown", "sample-changed", "resample", "stable")
	}
	// Readers assert exhaustion separately from HTTP success. A missing or duplicate
	// source and a source without request accounting cannot prove completeness.
	seenSources := map[string]bool{}
	requests := 0
	required := map[string]bool{"full-statuses": false, "files": false, "open-prs": false, "report": false}
	for _, src := range sample.Sources {
		// Account first: the closed review-only vocabulary is no excuse to
		// discard declared requests or silently accept another source.
		requests += src.Requests
		if _, ok := required[src.Name]; !ok {
			add("unknown", "evidence-incomplete", "collection", "source")
			continue
		}
		selector := ""
		switch src.Name {
		case "full-statuses":
			selector = sample.PullRequest.HeadSHA
		case "files":
			selector = strconv.FormatInt(sample.PullRequest.Number, 10)
		case "report":
			selector = strconv.FormatInt(sample.Comment.ID, 10)
		}
		if seenSources[src.Name] || src.Outcome != "ok" || !src.Complete || src.Requests < 1 || src.Requests > 80 || src.Host != sample.Repository.Host || src.Repository != sample.Repository.FullName || src.Selector != selector {
			add("unknown", "evidence-incomplete", src.Name, "source")
		}
		seenSources[src.Name] = true
		required[src.Name] = true
	}
	if requests > 80 {
		add("unknown", "evidence-incomplete", "collection", "requests")
	}
	for _, name := range []string{"full-statuses", "files", "open-prs", "report"} {
		if !required[name] {
			add("unknown", "evidence-incomplete", name, "source")
		}
	}
	pr := sample.PullRequest
	if sample.Repository != p.Repository || pr.BaseRepositoryID != p.Repository.ID || pr.BaseRef != p.Repository.BaseRef {
		add("fail", "repository-mismatch", "pr", "repository")
	}
	if pr.State != "open" || pr.Merged {
		add("fail", "pull-request-closed", "pr", "state")
	}
	if pr.Number <= 0 || !reviewSHA.MatchString(pr.HeadSHA) || !reviewSHA.MatchString(pr.BaseSHA) || !reviewSHA.MatchString(sample.Policy.Commit) || sample.Policy.SHA256 != policy.SHA256() {
		add("fail", "review-binding-mismatch", "policy", "binding")
	}
	if len(sample.AssociatedOpenPRs) > 1000 {
		add("unknown", "evidence-incomplete", "open-prs", "count")
	}
	seenPRs := map[int64]bool{}
	for _, n := range sample.AssociatedOpenPRs {
		if n <= 0 || seenPRs[n] {
			add("unknown", "association-incomplete", "open-prs", "number")
		}
		seenPRs[n] = true
	}
	e.Singleton = len(sample.AssociatedOpenPRs) == 1 && seenPRs[pr.Number]
	if len(sample.AssociatedOpenPRs) > 1 {
		add("unknown", "shared-head-ambiguous", "open-prs", "head")
	} else if !e.Singleton {
		add("unknown", "association-incomplete", "open-prs", "head")
	}
	filesDigest, filesOK := reviewFilesDigest(sample.Files, sample.ChangedFiles)
	if !filesOK {
		add("unknown", "evidence-incomplete", "files", "scope")
	}
	if len(sample.Statuses) > 1000 {
		add("unknown", "evidence-incomplete", "full-statuses", "count")
		return finish()
	}
	var selected *ReviewStatus
	seenIDs := map[int64]bool{}
	for i := range sample.Statuses {
		status := &sample.Statuses[i]
		if status.ID <= 0 || seenIDs[status.ID] || len(status.Context) > 100 || !boundedReviewText(status.Context, 100) || !boundedReviewText(status.Description, 2048) || !boundedReviewText(status.TargetURL, 2048) {
			add("unknown", "evidence-incomplete", "full-statuses", "record")
		}
		seenIDs[status.ID] = true
		if status.Context == p.Review.Context && (selected == nil || status.ID > selected.ID) {
			selected = status
		}
	}
	if selected == nil {
		add("fail", "review-missing", "full-statuses", "context")
		return finish()
	}
	statusID := selected.ID
	e.StatusID = &statusID
	c.Evidence = append(c.Evidence, SourceReference{Source: "full-statuses", Field: "id"})
	if !reviewIDAllowed(selected.CreatorID, p.Review.StatusCreatorIDs) || !reviewIDAllowed(sample.Comment.AuthorID, p.Review.ReportAuthorIDs) {
		add("fail", "review-issuer-untrusted", "report", "issuer")
	}
	if selected.State != "success" {
		add("fail", "review-not-clear", "full-statuses", "state")
	}
	// Exact construction avoids permissive URL normalization, credentials, encoded
	// fragments, cross-origin locators and alternate comment candidates. No URL is fetched.
	locator := sample.WebOrigin + "/" + sample.Repository.FullName + "/pull/" + strconv.FormatInt(pr.Number, 10) + "#issuecomment-"
	commentText := strings.TrimPrefix(selected.TargetURL, locator)
	commentID, err := strconv.ParseInt(commentText, 10, 64)
	expectedIssue := sample.APIOrigin + "/repos/" + sample.Repository.FullName + "/issues/" + strconv.FormatInt(pr.Number, 10)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(sample.Comment.Body)))
	if !validReviewOrigins(sample.Repository.Host, sample.WebOrigin, sample.APIOrigin) || !strings.HasPrefix(selected.TargetURL, locator) || err != nil || commentID <= 0 || strconv.FormatInt(commentID, 10) != commentText || sample.Comment.ID != commentID || sample.Comment.IssueURL != expectedIssue || selected.Description != "stw-review-v1 sha256="+digest {
		add("fail", "report-binding-mismatch", "report", "binding")
	}
	commentID = sample.Comment.ID
	e.CommentID = &commentID
	report, err := ParseReport([]byte(sample.Comment.Body))
	if err != nil {
		add("unknown", "report-malformed", "report", "body")
		return finish()
	}
	r := report.Value()
	// Only authenticated public metadata is projected. Reviewer labels and raw
	// report text (which may contain private identifiers) are never copied out.
	if c.State != "fail" {
		e.ReportID = r.ReportID
		e.ReportSHA256 = report.SHA256()
		e.ConfiguredModel = Model{ID: r.Model.ID, Effort: r.Model.Effort}
		e.DeclaredIndependence = r.Independence
	}
	if r.RepositoryID != sample.Repository.ID || r.Number != pr.Number || r.HeadSHA != pr.HeadSHA || r.BaseSHA != pr.BaseSHA || r.PolicyCommit != sample.Policy.Commit || r.PolicySHA256 != sample.Policy.SHA256 || r.Scope != "full-pr" || filesOK && r.FilesSHA256 != filesDigest {
		add("fail", "review-binding-mismatch", "report", "scope")
	}
	requiredRank, requiredTier := reviewRequiredTier(p.Review, sample.Files)
	e.Tier = requiredTier
	modelRank := int64(0)
	for _, tier := range p.Review.Tiers {
		for _, model := range tier.Models {
			if model.ID == r.Model.ID && model.Effort == r.Model.Effort {
				modelRank = tier.Rank
			}
		}
	}
	if modelRank < requiredRank {
		add("fail", "review-tier-insufficient", "report", "model")
	}
	if p.Review.Independence != "declared-execution" || r.Independence.Kind != "declared-execution" || !r.Independence.SeparateAuthor || !r.Independence.SeparateContext {
		add("unknown", "independence-unverifiable", "report", "independence")
	}
	history, clear := reviewHistory(r, p.Review)
	if !history {
		add("unknown", "report-history-invalid", "report", "rounds")
	}
	if !clear {
		add("fail", "review-not-clear", "report", "findings")
	}
	c.Evidence = append(c.Evidence, SourceReference{Source: "report", Field: "body"}, SourceReference{Source: "files", Field: "scope"}, SourceReference{Source: "open-prs", Field: "head"})
	return finish()
}

var reviewSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func reviewIDAllowed(id int64, ids []int64) bool {
	for _, allowed := range ids {
		if id > 0 && id == allowed {
			return true
		}
	}
	return false
}
func boundedReviewText(s string, max int) bool { return len(s) <= max && utf8.ValidString(s) }
func validReviewOrigins(host, web, api string) bool {
	// Selected host comes from native hosting resolution; reject authority/path
	// metacharacters here rather than accepting caller-provided alternate origins.
	if host == "" || strings.ContainsAny(host, "/:@?#\\ \t\r\n") {
		return false
	}
	if host == "github.com" {
		return web == "https://github.com" && api == "https://api.github.com"
	}
	return web == "https://"+host && api == "https://"+host+"/api/v3"
}
func validReviewPolicy(p ReviewPolicy) bool {
	names := map[string]int64{}
	ranks := map[int64]bool{}
	models := map[Model]bool{}
	for _, tier := range p.Tiers {
		if names[tier.Name] != 0 || ranks[tier.Rank] || tier.Rank < 1 {
			return false
		}
		names[tier.Name] = tier.Rank
		ranks[tier.Rank] = true
		for _, model := range tier.Models {
			if models[model] {
				return false
			}
			models[model] = true
		}
	}
	if names[p.DefaultTier] == 0 {
		return false
	}
	prefixes := map[string]bool{}
	for _, rule := range p.PathTiers {
		if names[rule.Tier] == 0 || !validReviewPrefix(rule.Prefix) || prefixes[rule.Prefix] {
			return false
		}
		prefixes[rule.Prefix] = true
	}
	return true
}
func validReviewPrefix(s string) bool {
	if s == "" || strings.HasPrefix(s, "/") || strings.ContainsAny(s, "\\*?[]{}") || !boundedReviewText(s, 1024) {
		return false
	}
	for _, part := range strings.Split(strings.TrimSuffix(s, "/"), "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func reviewRequiredTier(p ReviewPolicy, files []ReviewFile) (int64, string) {
	ranks := map[string]int64{}
	for _, t := range p.Tiers {
		ranks[t.Name] = t.Rank
	}
	name := p.DefaultTier
	rank := ranks[name]
	for _, f := range files {
		for _, rule := range p.PathTiers {
			for _, path := range []string{f.Filename, reviewPrevious(f)} {
				matches := path == rule.Prefix || strings.HasSuffix(rule.Prefix, "/") && strings.HasPrefix(path, rule.Prefix)
				if matches && ranks[rule.Tier] > rank {
					name, rank = rule.Tier, ranks[rule.Tier]
				}
			}
		}
	}
	return rank, name
}
func reviewPrevious(f ReviewFile) string {
	if f.PreviousFilename != nil {
		return *f.PreviousFilename
	}
	return ""
}
func reviewFilesDigest(files []ReviewFile, count int) (string, bool) {
	if count < 0 || count > 1000 || len(files) != count {
		return "", false
	}
	rows := append([]ReviewFile{}, files...)
	seen := map[string]bool{}
	for _, f := range rows {
		if f.Filename == "" || f.Status == "" || !boundedReviewText(f.Filename, 2048) || !boundedReviewText(f.Status, 2048) || !boundedReviewText(reviewPrevious(f), 2048) || seen[f.Filename] || f.Status == "renamed" && reviewPrevious(f) == "" {
			return "", false
		}
		seen[f.Filename] = true
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		if reviewPrevious(a) != reviewPrevious(b) {
			return reviewPrevious(a) < reviewPrevious(b)
		}
		return a.Status < b.Status
	})
	var framed strings.Builder
	for _, f := range rows {
		for _, value := range []string{f.Filename, reviewPrevious(f), f.Status} {
			framed.WriteString(strconv.Itoa(len(value)))
			framed.WriteByte(':')
			framed.WriteString(value)
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(framed.String()))), true
}
