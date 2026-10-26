// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"
)

// TidyPullRequest is a sampled tuple, never native ancestry or deletion authority.
// Null repository/merge fields preserve complete unsupported/unmerged evidence.
type TidyPullRequest struct {
	Repository     TidyRepositoryIdentity `json:"repository"`
	ID             int64                  `json:"id"`
	NodeID         string                 `json:"node_id"`
	Number         int                    `json:"number"`
	State          string                 `json:"state"`
	Merged         bool                   `json:"merged"`
	MergedAt       *string                `json:"merged_at"`
	Draft          bool                   `json:"draft"`
	HeadRepository *TidyRepositoryID      `json:"head_repository"`
	HeadRef        string                 `json:"head_ref"`
	HeadOID        string                 `json:"head_oid"`
	BaseRepository *TidyRepositoryID      `json:"base_repository"`
	BaseRef        string                 `json:"base_ref"`
	BaseOID        string                 `json:"base_oid"`
	MergeOID       *string                `json:"merge_oid"`
}

// TidyHeadEvidence explicitly distinguishes complete negative observations from
// operation errors. Every requested full SHA-1 OID has exactly one ordered row.
// A nil PullRequest is never an admissible provider proof.
type TidyHeadEvidence struct {
	HeadOID     string           `json:"head_oid"`
	Reason      string           `json:"reason"`
	PullRequest *TidyPullRequest `json:"pull_request"`
	// Result and Coverage report how the row was reached (Spec 031 FR-017):
	// github-merged-head, no-merged-pr, discovery-incomplete,
	// discovery-budget-exhausted or ambiguous-needs-hint, and whether the
	// closed collection was read to an authoritative end.
	Result   string `json:"result"`
	Coverage string `json:"coverage"`
	// ClosedUnmerged lists, in ascending order, the numbers of the pull requests of this head
	// that were read in full and are closed without being merged (Spec 044 T005). It is empty
	// unless there are such pull requests and never makes a proof.
	ClosedUnmerged []int `json:"closed_unmerged,omitempty"`
}

type tidyPRRef struct {
	SHA  *string         `json:"sha"`
	Ref  *string         `json:"ref"`
	Repo json.RawMessage `json:"repo"`
}

func decodeTidyRepoID(raw json.RawMessage) (*TidyRepositoryID, error) {
	if string(raw) == "null" {
		return nil, nil
	}
	var r struct {
		ID       *int64  `json:"id"`
		NodeID   *string `json:"node_id"`
		FullName *string `json:"full_name"`
	}
	if len(raw) == 0 || decodeTidyJSON(raw, &r) != nil || r.ID == nil || *r.ID <= 0 || r.NodeID == nil || !tidyAtom(*r.NodeID, 256) || r.FullName == nil || !validRepo(*r.FullName) {
		return nil, errors.New("PR repository identity is missing or invalid")
	}
	return &TidyRepositoryID{ID: *r.ID, NodeID: *r.NodeID, FullName: *r.FullName}, nil
}
func decodeTidyNullableString(raw json.RawMessage) (*string, error) {
	if string(raw) == "null" {
		return nil, nil
	}
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value == "" {
		return nil, errors.New("missing or invalid nullable PR field")
	}
	return &value, nil
}
func decodeTidyPullRequest(raw []byte, repo TidyRepositoryIdentity, number int) (TidyPullRequest, error) {
	var p struct {
		ID       *int64          `json:"id"`
		NodeID   *string         `json:"node_id"`
		Number   *int            `json:"number"`
		State    *string         `json:"state"`
		Merged   *bool           `json:"merged"`
		MergedAt json.RawMessage `json:"merged_at"`
		Draft    *bool           `json:"draft"`
		Head     *tidyPRRef      `json:"head"`
		Base     *tidyPRRef      `json:"base"`
		Merge    json.RawMessage `json:"merge_commit_sha"`
	}
	fail := func() (TidyPullRequest, error) {
		return TidyPullRequest{}, errors.New("PR response is missing or has invalid required facts")
	}
	if decodeTidyJSON(raw, &p) != nil || p.ID == nil || *p.ID <= 0 || p.NodeID == nil || !tidyAtom(*p.NodeID, 256) || p.Number == nil || *p.Number != number || number <= 0 || p.State == nil || (*p.State != "closed" && *p.State != "open") || p.Merged == nil || p.Draft == nil || p.Head == nil || p.Base == nil {
		return fail()
	}
	for _, ref := range []*tidyPRRef{p.Head, p.Base} {
		if ref.SHA == nil || !fullSHA(*ref.SHA) || ref.Ref == nil || !tidyBranchName(*ref.Ref) {
			return fail()
		}
	}
	head, err := decodeTidyRepoID(p.Head.Repo)
	if err != nil {
		return fail()
	}
	base, err := decodeTidyRepoID(p.Base.Repo)
	if err != nil {
		return fail()
	}
	mergedAt, err := decodeTidyNullableString(p.MergedAt)
	if err != nil {
		return fail()
	}
	mergeOID, err := decodeTidyNullableString(p.Merge)
	if err != nil {
		return fail()
	}
	if mergeOID != nil && !fullSHA(*mergeOID) {
		return fail()
	}
	if mergedAt != nil {
		if _, err := time.Parse(time.RFC3339, *mergedAt); err != nil {
			return fail()
		}
	}
	if *p.Merged && (*p.State != "closed" || mergedAt == nil || mergeOID == nil) {
		return fail()
	}
	if !*p.Merged && mergedAt != nil {
		return fail()
	}
	return TidyPullRequest{Repository: repo, ID: *p.ID, NodeID: *p.NodeID, Number: number, State: *p.State, Merged: *p.Merged, MergedAt: mergedAt, Draft: *p.Draft, HeadRepository: head, HeadRef: *p.Head.Ref, HeadOID: *p.Head.SHA, BaseRepository: base, BaseRef: *p.Base.Ref, BaseOID: *p.Base.SHA, MergeOID: mergeOID}, nil
}
func (s *TidySession) readPullRequest(ctx context.Context, number int) (TidyPullRequest, error) {
	if number <= 0 {
		return TidyPullRequest{}, errors.New("invalid PR number")
	}
	raw, _, err := s.get(ctx, fmt.Sprintf("repos/%s/%s/pulls/%d", s.selected.Owner, s.selected.Name, number))
	if err != nil {
		return TidyPullRequest{}, err
	}
	result, err := decodeTidyPullRequest(raw, s.repository, number)
	if err == nil {
		err = tidyResultBound(result)
	}
	if err != nil {
		return TidyPullRequest{}, err
	}
	return result, nil
}
func (s *TidySession) ReadPullRequest(parent context.Context, number int) (TidyPullRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel, err := s.operationContext(parent)
	defer cancel()
	if err != nil {
		return TidyPullRequest{}, err
	}
	if err = s.revalidateRepository(ctx); err != nil {
		return TidyPullRequest{}, err
	}
	return s.readPullRequest(ctx, number)
}
func (s *TidySession) RevalidatePullRequest(parent context.Context, expected TidyPullRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel, err := s.operationContext(parent)
	defer cancel()
	if err != nil {
		return err
	}
	if expected.Repository != s.repository {
		return ErrTidyChanged
	}
	if err = s.revalidateRepository(ctx); err != nil {
		return err
	}
	current, err := s.readPullRequest(ctx, expected.Number)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, expected) {
		return ErrTidyChanged
	}
	return nil
}
func (s *TidySession) admissiblePR(p TidyPullRequest, head string) string {
	if p.HeadOID != head {
		return "pr-head-mismatch"
	}
	if p.HeadRepository == nil || p.BaseRepository == nil || *p.HeadRepository != s.repository.TidyRepositoryID || *p.BaseRepository != s.repository.TidyRepositoryID {
		return "hosting-proof-unsupported"
	}
	if p.State != "closed" || !p.Merged || p.MergedAt == nil || p.Draft || p.MergeOID == nil {
		return "integration-not-proven"
	}
	return "github-merged-head"
}

type tidyPRHint struct {
	ID     int64
	NodeID string
	Number int
	Head   string
}

// discoverPRHints reads the closed collection. The boolean reports an
// authoritative end within the page and record bounds; a tenth full page
// without one returns the rows found so far and false, never an error.
func (s *TidySession) discoverPRHints(ctx context.Context, targets map[string]bool) (map[string][]tidyPRHint, bool, error) {
	result := map[string][]tidyPRHint{}
	ids := map[int64]bool{}
	nodes := map[string]bool{}
	numbers := map[int]bool{}
	lastPage := 0
	for page := 1; page <= maxPages; page++ {
		endpoint := fmt.Sprintf("repos/%s/%s/pulls?state=closed&per_page=100&page=%d", s.selected.Owner, s.selected.Name, page)
		raw, header, err := s.get(ctx, endpoint)
		if err != nil {
			return nil, false, err
		}
		var rows []json.RawMessage
		if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > 100 {
			return nil, false, errors.New("closed PR collection is incomplete or invalid")
		}
		for _, row := range rows {
			var p struct {
				ID     *int64  `json:"id"`
				NodeID *string `json:"node_id"`
				Number *int    `json:"number"`
				State  *string `json:"state"`
				Head   *struct {
					SHA *string `json:"sha"`
				} `json:"head"`
			}
			if decodeTidyJSON(row, &p) != nil || p.ID == nil || *p.ID <= 0 || p.NodeID == nil || !tidyAtom(*p.NodeID, 256) || p.Number == nil || *p.Number <= 0 || p.State == nil || *p.State != "closed" || p.Head == nil || p.Head.SHA == nil || !fullSHA(*p.Head.SHA) || ids[*p.ID] || nodes[*p.NodeID] || numbers[*p.Number] {
				return nil, false, errors.New("closed PR collection has malformed or duplicate identities")
			}
			ids[*p.ID] = true
			nodes[*p.NodeID] = true
			numbers[*p.Number] = true
			if len(ids) > maxRecords {
				return nil, false, ErrTidyLimit
			}
			if targets[*p.Head.SHA] {
				result[*p.Head.SHA] = append(result[*p.Head.SHA], tidyPRHint{ID: *p.ID, NodeID: *p.NodeID, Number: *p.Number, Head: *p.Head.SHA})
			}
		}
		terminal, err := tidyPagination(header, endpoint, page, len(rows), &lastPage)
		if err != nil {
			if errors.Is(err, ErrTidyLimit) && page == maxPages && len(rows) == 100 {
				return result, false, nil
			}
			return nil, false, err
		}
		if terminal {
			return result, true, nil
		}
	}
	return result, false, nil
}

// DiscoverPullRequests performs one unfiltered closed collection (at most ten
// pages) for unhinted targets and reads each candidate PR in full. Hints bypass
// only discovery, never full tuple validation. Rows are returned in ascending
// OID order, one per distinct target. Positive evidence is proof whatever the
// collection coverage; absence is reported, never claimed, when the collection
// is incomplete, and OIDs the shared request budget cannot cover are reported
// discovery-budget-exhausted (Spec 031 FR-017, FR-018). It returns no rows on
// an observation error: an error or a 404 never proves absence.
func (s *TidySession) DiscoverPullRequests(parent context.Context, targets []string, hints map[string]int) ([]TidyHeadEvidence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel, err := s.operationContext(parent)
	defer cancel()
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 || len(targets) > 1024 {
		return nil, errors.New("invalid full-OID target selection")
	}
	all := map[string]bool{}
	unhinted := map[string]bool{}
	ordered := make([]string, 0, len(targets))
	for _, head := range targets {
		if !fullSHA(head) || all[head] {
			return nil, errors.New("invalid or duplicate full-OID target")
		}
		all[head] = true
		unhinted[head] = true
		ordered = append(ordered, head)
	}
	sort.Strings(ordered)
	usedNumbers := map[int]bool{}
	for head, number := range hints {
		if !all[head] || number <= 0 || usedNumbers[number] {
			return nil, errors.New("invalid or conflicting PR hint")
		}
		usedNumbers[number] = true
		delete(unhinted, head)
	}
	if err = s.revalidateRepository(ctx); err != nil {
		return nil, err
	}
	discovered := map[string][]tidyPRHint{}
	complete := true
	if len(unhinted) > 0 {
		discovered, complete, err = s.discoverPRHints(ctx, unhinted)
		if err != nil {
			return nil, err
		}
	}
	result := make([]TidyHeadEvidence, 0, len(ordered))
	exhausted := false
	for _, head := range ordered {
		candidates := discovered[head]
		hinted := false
		if number, ok := hints[head]; ok {
			candidates = []tidyPRHint{{Number: number, Head: head}}
			hinted = true
		}
		// A hint skips the collection, so only the named PR was examined and
		// absence of any other merged PR is never claimed for it.
		coverage := "complete"
		if hinted || !complete {
			coverage = "incomplete"
		}
		item := TidyHeadEvidence{HeadOID: head, Reason: "integration-not-proven", Coverage: coverage}
		if !exhausted && len(candidates) > tidyMaxRequests-s.ledger.snapshot().Requests {
			exhausted = true
		}
		if exhausted {
			item.Result = "discovery-budget-exhausted"
			result = append(result, item)
			continue
		}
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].Number < candidates[j].Number })
		matches := []TidyPullRequest{}
		for _, candidate := range candidates {
			p, err := s.readPullRequest(ctx, candidate.Number)
			if err != nil {
				return nil, err
			}
			if candidate.ID != 0 && (candidate.ID != p.ID || candidate.NodeID != p.NodeID || candidate.Head != p.HeadOID) {
				return nil, ErrTidyChanged
			}
			reason := s.admissiblePR(p, head)
			if reason == "github-merged-head" {
				matches = append(matches, p)
			} else if item.Reason == "integration-not-proven" {
				item.Reason = reason
			}
			if p.HeadOID == head && p.State == "closed" && !p.Merged {
				item.ClosedUnmerged = append(item.ClosedUnmerged, p.Number)
			}
		}
		switch {
		case len(matches) == 1:
			item.Reason, item.Result = "github-merged-head", "github-merged-head"
			p := matches[0]
			item.PullRequest = &p
		case len(matches) > 1:
			item.Reason, item.Result = "pr-ambiguous", "ambiguous-needs-hint"
		case coverage == "incomplete":
			item.Result = "discovery-incomplete"
		default:
			item.Result = "no-merged-pr"
		}
		result = append(result, item)
	}
	if err = tidyResultBound(result); err != nil {
		return nil, err
	}
	return result, nil
}
