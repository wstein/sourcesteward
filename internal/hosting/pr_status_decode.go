// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var errPRStatusEvidence = errors.New("PR response has unavailable or inconsistent lifecycle evidence")

type prStatusAPIRef struct {
	SHA  *string         `json:"sha"`
	Ref  *string         `json:"ref"`
	Repo json.RawMessage `json:"repo"`
}

func statusNullable(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 {
		return nil, errPRStatusEvidence
	}
	var value *string
	if json.Unmarshal(raw, &value) != nil {
		return nil, errPRStatusEvidence
	}
	return value, nil
}
func statusReference(ref *prStatusAPIRef, nullable bool) (*string, error) {
	if ref == nil || ref.SHA == nil || !fullSHA(*ref.SHA) || ref.Ref == nil || *ref.Ref == "" || len(ref.Repo) == 0 {
		return nil, errPRStatusEvidence
	}
	if bytes.Equal(bytes.TrimSpace(ref.Repo), []byte("null")) {
		if nullable {
			return nil, nil
		}
		return nil, errPRStatusEvidence
	}
	var repo struct {
		FullName *string `json:"full_name"`
	}
	if json.Unmarshal(ref.Repo, &repo) != nil || repo.FullName == nil || !validRepo(*repo.FullName) {
		return nil, errPRStatusEvidence
	}
	return repo.FullName, nil
}

var prStatusDatePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}[Tt]([01][0-9]|2[0-3]):[0-5][0-9]:([0-5][0-9]|60)([.][0-9]+)?([Zz]|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func statusDate(value *string) bool {
	if value == nil {
		return true
	}
	if !prStatusDatePattern.MatchString(*value) {
		return false
	}
	normalized := strings.ToUpper(*value)
	leap := normalized[17:19] == "60"
	if leap {
		normalized = normalized[:17] + "59" + normalized[19:]
	}
	parsed, err := time.Parse(time.RFC3339, normalized)
	return err == nil && (!leap || parsed.UTC().Hour() == 23 && parsed.UTC().Minute() == 59)
}

// decodePRStatus accepts explicit nulls and unknown fields, never missing evidence.
func decodePRStatus(raw []byte, repo *Repository, number int) (*PRStatusPullRequest, error) {
	if len(raw) > maxBytes || !utf8.Valid(raw) || repo == nil || number <= 0 {
		return nil, errPRStatusEvidence
	}
	var p struct {
		Number *int            `json:"number"`
		Title  *string         `json:"title"`
		URL    *string         `json:"html_url"`
		State  *string         `json:"state"`
		Draft  *bool           `json:"draft"`
		Head   *prStatusAPIRef `json:"head"`
		Base   *prStatusAPIRef `json:"base"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Number == nil || *p.Number != number || p.Title == nil || p.URL == nil || p.State == nil || p.Draft == nil {
		return nil, errPRStatusEvidence
	}
	// Decode new members by exact JSON name: encoding/json's struct matching
	// is case-insensitive and must not turn a missing field into evidence.
	var evidence map[string]json.RawMessage
	if json.Unmarshal(raw, &evidence) != nil {
		return nil, errPRStatusEvidence
	}
	var merged *bool
	if json.Unmarshal(evidence["merged"], &merged) != nil || merged == nil {
		return nil, errPRStatusEvidence
	}
	head, err := statusReference(p.Head, true)
	if err != nil {
		return nil, err
	}
	base, err := statusReference(p.Base, false)
	if err != nil || !strings.EqualFold(*base, repo.Owner+"/"+repo.Name) {
		return nil, errPRStatusEvidence
	}
	mergedAt, err := statusNullable(evidence["merged_at"])
	if err != nil {
		return nil, err
	}
	closedAt, err := statusNullable(evidence["closed_at"])
	if err != nil {
		return nil, err
	}
	mergeSHA, err := statusNullable(evidence["merge_commit_sha"])
	if err != nil || mergeSHA != nil && !fullSHA(*mergeSHA) || !statusDate(mergedAt) || !statusDate(closedAt) {
		return nil, errPRStatusEvidence
	}
	lifecycle := ""
	switch {
	case *p.State == "open" && !*merged && closedAt == nil && mergedAt == nil:
		lifecycle = "open"
	case *p.State == "closed" && !*merged && closedAt != nil && mergedAt == nil:
		lifecycle = "closed-unmerged"
	case *p.State == "closed" && *merged && closedAt != nil && mergedAt != nil:
		lifecycle = "merged"
	default:
		return nil, errPRStatusEvidence
	}
	result := &PRStatusPullRequest{Number: *p.Number, Title: *p.Title, URL: *p.URL, State: *p.State, Draft: *p.Draft, HeadSHA: *p.Head.SHA, BaseSHA: *p.Base.SHA, HeadRef: *p.Head.Ref, BaseRef: *p.Base.Ref, HeadRepository: head, BaseRepository: *base, Merged: *merged, MergedAt: mergedAt, ClosedAt: closedAt, MergeCommitSHA: mergeSHA, Lifecycle: lifecycle}
	if result.Merged {
		result.ResultSHA = mergeSHA
	}
	return result, nil
}

// readPRStatusOnce consumes an already resolved selection and an existing client.
// Its caller owns native/auth/client lifetime and the global operation context.
// This seam does not resolve, authenticate, retry, watch or render output.
func readPRStatusOnce(ctx context.Context, api apiReader, selection Result, number int, clock func() time.Time) (r PRStatusResult) {
	r = newPRStatusResult()
	if clock == nil {
		clock = time.Now
	}
	defer func() {
		r.ObservedAt = clock().UTC().Format(time.RFC3339)
		if ctx.Err() != nil {
			r.fail("hosting-canceled", "hosting inspection canceled or exceeded its deadline", "canceled")
		}
		encoded, err := json.Marshal(r)
		if err != nil || len(encoded)+1 > maxBytes {
			r.Identity = nil
			r.Repository = nil
			r.Candidates = []Candidate{}
			r.PullRequest = nil
			r.SampledAt = nil
			r.Observation.InitialHeadSHA = nil
			r.fail("hosting-output-limit", "hosting result exceeded its one MiB limit", "output-limit")
		}
	}()
	if ctx.Err() != nil {
		return r
	}
	if selection.Identity == nil || selection.Identity.Status != "ok" || (selection.Backend != "git" && selection.Backend != "jj") || selection.Identity.Backend != selection.Backend || selection.Repository == nil || !validHost(selection.Repository.Host) || !validRepo(selection.Repository.Owner+"/"+selection.Repository.Name) || number <= 0 {
		r.fail("hosting-invalid-options", "valid native ownership and selected repository are required", "failed")
		return r
	}
	r.Backend = selection.Backend
	r.Identity = selection.Identity
	r.Repository = selection.Repository
	r.Candidates = append([]Candidate{}, selection.Candidates...)
	r.Boundary.Network = true
	r.Boundary.Requests = 1
	raw, err := api.get(ctx, fmt.Sprintf("repos/%s/%s/pulls/%d", selection.Repository.Owner, selection.Repository.Name, number))
	if ctx.Err() != nil {
		return r
	}
	if err != nil {
		r.fail("hosting-pr-failed", "cannot read PR evidence; check selected-host access and rate limits", "failed")
		return r
	}
	pr, err := decodePRStatus(raw, selection.Repository, number)
	if err != nil {
		r.fail("hosting-pr-evidence", errPRStatusEvidence.Error(), "failed")
		return r
	}
	if ctx.Err() != nil {
		return r
	}
	sampled := clock().UTC().Format(time.RFC3339)
	r.SampledAt = &sampled
	r.PullRequest = pr
	r.Observation.InitialHeadSHA = &pr.HeadSHA
	r.Observation.Samples = 1
	r.Observation.Stop = "sampled"
	r.Status = "ok"
	r.Boundary.Complete = true
	return r
}
