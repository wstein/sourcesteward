// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"encoding/json"
	"errors"
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
