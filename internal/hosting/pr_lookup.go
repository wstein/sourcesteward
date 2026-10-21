// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/inspection"
)

// lookupPageSize is one page: 30 list items keep a realistic body under the one MiB cap.
const lookupPageSize = 30

// LookupPullRequest is a PR GitHub associates with a commit. HeadMatch compares the
// PR's current head with the queried SHA; the command never selects among entries.
type LookupPullRequest struct {
	PRStatusPullRequest
	HeadMatch bool `json:"head_match"`
}
type LookupQuery struct {
	HeadSHA string `json:"head_sha"`
}

// LookupResult is the schema 2 `prs` result.
type LookupResult struct {
	SchemaVersion int                     `json:"schema_version"`
	Kind          string                  `json:"kind"`
	Status        string                  `json:"status"`
	Backend       string                  `json:"backend"`
	Identity      *inspection.Result      `json:"identity"`
	Repository    *Repository             `json:"repository"`
	Candidates    []Candidate             `json:"candidates"`
	Query         LookupQuery             `json:"query"`
	PullRequests  []LookupPullRequest     `json:"pull_requests"`
	Count         int                     `json:"count"`
	Truncated     bool                    `json:"truncated"`
	Diagnostics   []inspection.Diagnostic `json:"diagnostics"`
	ObservedAt    string                  `json:"observed_at"`
	SampledAt     *string                 `json:"sampled_at"`
	Boundary      PRStatusBoundary        `json:"boundary"`
}

func newLookupResult(sha string) LookupResult {
	return LookupResult{SchemaVersion: 2, Kind: "prs", Status: "error", Backend: "unknown", Candidates: []Candidate{}, Query: LookupQuery{HeadSHA: sha}, PullRequests: []LookupPullRequest{}, Diagnostics: []inspection.Diagnostic{},
		Boundary: PRStatusBoundary{MaxBytes: maxBytes, TimeoutSeconds: 30, MaxRequests: 1, Limitations: []string{"Lists the PRs GitHub associates with the commit; it does not select one or state that the SHA is a branch tip.", "One page only; the PR set may have changed since it was read."}}}
}
func (r *LookupResult) fail(code, message string) {
	r.Status = "error"
	r.Boundary.Complete = false
	r.PullRequests, r.Count, r.Truncated, r.SampledAt = []LookupPullRequest{}, 0, false, nil
	r.Diagnostics = []inspection.Diagnostic{{Code: code, Message: message}}
}
func (r LookupResult) Error() string {
	if len(r.Diagnostics) > 0 {
		return r.Diagnostics[0].Message
	}
	return "PR lookup failed"
}

// decodeLookup decodes one list page. List items carry no `merged` boolean, so it is
// derived from `merged_at` and then checked by the same lifecycle table as `pr`.
func decodeLookup(raw []byte, repo *Repository, sha string) ([]LookupPullRequest, error) {
	if len(raw) > maxBytes || !utf8.Valid(raw) || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, errPRStatusEvidence
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || len(items) > lookupPageSize {
		return nil, errPRStatusEvidence
	}
	out := make([]LookupPullRequest, 0, len(items))
	for _, item := range items {
		var evidence map[string]json.RawMessage
		var number *int
		if json.Unmarshal(item, &evidence) != nil || json.Unmarshal(evidence["number"], &number) != nil || number == nil || *number <= 0 {
			return nil, errPRStatusEvidence
		}
		mergedAt, ok := evidence["merged_at"]
		if !ok {
			return nil, errPRStatusEvidence
		}
		evidence["merged"] = json.RawMessage(fmt.Sprint(!bytes.Equal(bytes.TrimSpace(mergedAt), []byte("null"))))
		derived, err := json.Marshal(evidence)
		if err != nil {
			return nil, errPRStatusEvidence
		}
		pr, err := decodePRStatus(derived, repo, *number)
		if err != nil {
			return nil, err
		}
		out = append(out, LookupPullRequest{PRStatusPullRequest: *pr, HeadMatch: pr.HeadSHA == sha})
	}
	return out, nil
}

// InspectLookup finds the PRs GitHub associates with a head SHA: one GET, no pagination.
func InspectLookup(ctx context.Context, path string, o Options) LookupResult {
	return InspectLookupWithTransport(ctx, path, o, nil)
}

// InspectLookupWithTransport permits an in-process HTTP fixture; nil selects HTTPS.
func InspectLookupWithTransport(ctx context.Context, path string, o Options, rt http.RoundTripper) LookupResult {
	return inspectLookup(ctx, path, o, productionDependencies(rt))
}

func inspectLookup(parent context.Context, path string, o Options, d dependencies) (r LookupResult) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	r = newLookupResult(o.HeadSHA)
	defer func() {
		if ctx.Err() != nil {
			r.fail("hosting-canceled", "hosting inspection canceled or exceeded its deadline")
		}
		if encoded, err := json.Marshal(r); err != nil || len(encoded)+1 > maxBytes {
			r.Identity, r.Repository, r.Candidates = nil, nil, []Candidate{}
			r.fail("hosting-output-limit", "hosting result exceeded its one MiB limit")
		}
		r.ObservedAt = time.Now().UTC().Format(time.RFC3339)
	}()
	sel := newResult("prs")
	target, ok := resolve(ctx, path, o, d, &sel)
	r.Backend, r.Identity, r.Repository, r.Candidates = sel.Backend, sel.Identity, sel.Repository, sel.Candidates
	if !ok {
		code, msg := "hosting-native-failed", "hosting inspection failed"
		if len(sel.Diagnostics) > 0 {
			code, msg = sel.Diagnostics[0].Code, sel.Diagnostics[0].Message
		}
		r.fail(code, msg)
		return r
	}
	api, cleanup, err := d.api(ctx, target, sel.Repository.Host)
	if err != nil {
		r.fail("hosting-api-unavailable", err.Error())
		return r
	}
	defer cleanup()
	r.Boundary.Network, r.Boundary.Requests = true, 1
	raw, err := api.get(ctx, fmt.Sprintf("repos/%s/%s/commits/%s/pulls?per_page=%d&page=1", sel.Repository.Owner, sel.Repository.Name, o.HeadSHA, lookupPageSize))
	if ctx.Err() != nil {
		return r
	}
	if err != nil {
		r.fail("hosting-pr-failed", "cannot read PR evidence; check selected-host access and rate limits")
		return r
	}
	prs, err := decodeLookup(raw, sel.Repository, o.HeadSHA)
	if err != nil {
		r.fail("hosting-pr-evidence", errPRStatusEvidence.Error())
		return r
	}
	sampled := time.Now().UTC().Format(time.RFC3339)
	r.SampledAt, r.PullRequests, r.Count = &sampled, prs, len(prs)
	if r.Count == lookupPageSize {
		r.Truncated = true
		r.Boundary.Limitations = append(r.Boundary.Limitations, "lookup-single-page")
	}
	r.Status, r.Boundary.Complete = "ok", true
	return r
}
