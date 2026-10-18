// SPDX-License-Identifier: EUPL-1.2
package hosting

import "github.com/wstein/sourcesteward/internal/inspection"

// PRStatusPullRequest separates GitHub's reported merge result from the head.
// A premerge test SHA is retained only as MergeCommitSHA, never ResultSHA.
type PRStatusPullRequest struct {
	Number         int     `json:"number"`
	Title          string  `json:"title"`
	URL            string  `json:"url"`
	State          string  `json:"state"`
	Draft          bool    `json:"draft"`
	HeadSHA        string  `json:"head_sha"`
	BaseSHA        string  `json:"base_sha"`
	HeadRef        string  `json:"head_ref"`
	BaseRef        string  `json:"base_ref"`
	HeadRepository *string `json:"head_repository"`
	BaseRepository string  `json:"base_repository"`
	Merged         bool    `json:"merged"`
	MergedAt       *string `json:"merged_at"`
	ClosedAt       *string `json:"closed_at"`
	MergeCommitSHA *string `json:"merge_commit_sha"`
	ResultSHA      *string `json:"result_sha"`
	Lifecycle      string  `json:"lifecycle"`
}

type PRStatusObservation struct {
	Mode            string  `json:"mode"`
	Stop            string  `json:"stop"`
	Samples         int     `json:"samples"`
	InitialHeadSHA  *string `json:"initial_head_sha"`
	ExpectedHead    *string `json:"expected_head"`
	Expectation     string  `json:"expectation"`
	IdentityChanged bool    `json:"identity_changed"`
}

type PRStatusBoundary struct {
	Complete       bool     `json:"complete"`
	Network        bool     `json:"network"`
	MaxBytes       int      `json:"max_bytes"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	MaxRequests    int      `json:"max_requests"`
	Requests       int      `json:"requests"`
	Limitations    []string `json:"limitations"`
}

// PRStatusResult is the explicit v2 PR contract. Legacy Result remains unchanged.
type PRStatusResult struct {
	SchemaVersion int                     `json:"schema_version"`
	Kind          string                  `json:"kind"`
	Status        string                  `json:"status"`
	Backend       string                  `json:"backend"`
	Identity      *inspection.Result      `json:"identity"`
	Repository    *Repository             `json:"repository"`
	Candidates    []Candidate             `json:"candidates"`
	PullRequest   *PRStatusPullRequest    `json:"pull_request"`
	Diagnostics   []inspection.Diagnostic `json:"diagnostics"`
	ObservedAt    string                  `json:"observed_at"`
	SampledAt     *string                 `json:"sampled_at"`
	Observation   PRStatusObservation     `json:"observation"`
	Boundary      PRStatusBoundary        `json:"boundary"`
}

func newPRStatusResult() PRStatusResult {
	return PRStatusResult{SchemaVersion: 2, Kind: "pr", Status: "error", Backend: "unknown", Candidates: []Candidate{}, Diagnostics: []inspection.Diagnostic{}, Observation: PRStatusObservation{Mode: "once", Stop: "failed", Expectation: "not-requested"}, Boundary: PRStatusBoundary{MaxBytes: maxBytes, TimeoutSeconds: 30, MaxRequests: 1, Limitations: []string{"Sampled evidence cannot exclude unobserved ABA or future PR changes.", "GitHub result SHA is method-dependent; no review, ancestry or merge permission is inferred."}}}
}
func (r *PRStatusResult) fail(code, message, stop string) {
	r.Status = "error"
	r.Boundary.Complete = false
	r.Observation.Stop = stop
	r.Diagnostics = []inspection.Diagnostic{{Code: code, Message: message}}
}
func (r PRStatusResult) Error() string {
	if len(r.Diagnostics) > 0 {
		return r.Diagnostics[0].Message
	}
	return "PR status observation failed"
}
