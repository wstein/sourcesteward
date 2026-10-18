// SPDX-License-Identifier: EUPL-1.2
package readiness

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
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
