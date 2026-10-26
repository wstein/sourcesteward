// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// TidyTransport is the inner transport type of a tidy session (an http.RoundTripper); other packages
// name it through this alias so that only internal/hosting imports net/http.
type TidyTransport = http.RoundTripper

// The per-tip pull request states of the wizard (Spec 044 T005, Spec 031 FR-013 amendment).
const (
	// TidyTipMerged: a same-repository pull request whose head is exactly the tip was closed by a
	// merge into the default branch of the repository.
	TidyTipMerged = "merged-head"
	// TidyTipClosedUnmerged: no such merge, but a same-repository pull request of this head was
	// closed without one.
	TidyTipClosedUnmerged = "closed-unmerged"
	// TidyTipUnknown: the answer proves neither. An empty answer, a pull request merged into
	// another branch (a stacked child), a fork pull request and a commit GitHub does not know are
	// all unknown: the read cannot prove absence.
	TidyTipUnknown = "unknown"
)

// TidyTipEvidence is the answer for one tip. Number is set only with TidyTipMerged.
type TidyTipEvidence struct {
	Tip    string `json:"tip"`
	State  string `json:"state"`
	Number int    `json:"number,omitempty"`
}

// tidyTipPullsEndpoint is the one per-tip read of Spec 031 FR-013 as amended: the pull requests
// GitHub associates with one commit, keyed by the full object id, first page of 30.
func tidyTipPullsEndpoint(endpoint string) bool {
	return fixedEndpoint.MatchString(endpoint) && strings.Contains(endpoint, "/commits/") && strings.HasSuffix(endpoint, "/pulls?per_page=30&page=1")
}

// MaxTidyTips is the number of tips one call may ask: two session reads and one read per tip stay
// within the 128 requests of a session.
const MaxTidyTips = 64

// ReadTipPullRequests reads, for each tip, `commits/{tip}/pulls?per_page=30&page=1` (one GET per
// tip, no branch name in any URL) and classifies the validated list items: only pull requests of
// this repository (head and base repository equal to the session's) whose head is exactly the tip
// count; a merge counts only into the default branch of the repository. Rows are returned in
// ascending tip order, one per tip. A commit GitHub does not know (404, 422) is unknown; any
// other failure returns no rows, because an error never proves absence.
func (s *TidySession) ReadTipPullRequests(parent context.Context, tips []string) ([]TidyTipEvidence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel, err := s.operationContext(parent)
	defer cancel()
	if err != nil {
		return nil, err
	}
	if len(tips) == 0 || len(tips) > MaxTidyTips {
		return nil, errors.New("invalid tip selection")
	}
	ordered := append([]string{}, tips...)
	sort.Strings(ordered)
	for i, tip := range ordered {
		if !fullSHA(tip) || (i > 0 && ordered[i-1] == tip) {
			return nil, errors.New("invalid or duplicate full-OID tip")
		}
	}
	if err = s.revalidateRepository(ctx); err != nil {
		return nil, err
	}
	rows := make([]TidyTipEvidence, 0, len(ordered))
	for _, tip := range ordered {
		raw, _, err := s.get(ctx, fmt.Sprintf("repos/%s/%s/commits/%s/pulls?per_page=30&page=1", s.selected.Owner, s.selected.Name, tip))
		switch {
		case errors.Is(err, ErrTidyNotFound):
			rows = append(rows, TidyTipEvidence{Tip: tip, State: TidyTipUnknown})
			continue
		case err != nil:
			return nil, err
		}
		row, err := s.classifyTipPulls(raw, tip)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	if err = tidyResultBound(rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *TidySession) classifyTipPulls(raw []byte, tip string) (TidyTipEvidence, error) {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil || len(items) > 30 {
		return TidyTipEvidence{}, errors.New("tip pull request list is invalid")
	}
	row := TidyTipEvidence{Tip: tip, State: TidyTipUnknown}
	numbers := map[int]bool{}
	unmerged := false
	merged := 0
	for _, item := range items {
		var p struct {
			Number   *int            `json:"number"`
			State    *string         `json:"state"`
			MergedAt json.RawMessage `json:"merged_at"`
			Draft    *bool           `json:"draft"`
			Merge    json.RawMessage `json:"merge_commit_sha"`
			Head     *tidyPRRef      `json:"head"`
			Base     *tidyPRRef      `json:"base"`
		}
		fail := errors.New("tip pull request list has an invalid item")
		if decodeTidyJSON(item, &p) != nil || p.Number == nil || *p.Number <= 0 || numbers[*p.Number] || p.State == nil || (*p.State != "closed" && *p.State != "open") || p.Draft == nil || p.Head == nil || p.Base == nil {
			return TidyTipEvidence{}, fail
		}
		numbers[*p.Number] = true
		for _, ref := range []*tidyPRRef{p.Head, p.Base} {
			if ref.SHA == nil || !fullSHA(*ref.SHA) || ref.Ref == nil || !tidyBranchName(*ref.Ref) {
				return TidyTipEvidence{}, fail
			}
		}
		head, err := decodeTidyRepoID(p.Head.Repo)
		if err != nil {
			return TidyTipEvidence{}, fail
		}
		base, err := decodeTidyRepoID(p.Base.Repo)
		if err != nil {
			return TidyTipEvidence{}, fail
		}
		mergedAt, err := decodeTidyNullableString(p.MergedAt)
		if err != nil {
			return TidyTipEvidence{}, fail
		}
		mergeOID, err := decodeTidyNullableString(p.Merge)
		if err != nil || (mergeOID != nil && !fullSHA(*mergeOID)) {
			return TidyTipEvidence{}, fail
		}
		// Only a pull request of this repository whose head is exactly the tip is evidence.
		if *p.Head.SHA != tip || head == nil || base == nil || *head != s.repository.TidyRepositoryID || *base != s.repository.TidyRepositoryID || *p.Draft || *p.State != "closed" {
			continue
		}
		switch {
		case mergedAt == nil:
			unmerged = true
		case mergeOID != nil && *p.Base.Ref == s.repository.DefaultBranch:
			if merged == 0 || *p.Number < merged {
				merged = *p.Number
			}
		}
	}
	switch {
	case merged > 0:
		row.State, row.Number = TidyTipMerged, merged
	case unmerged:
		row.State = TidyTipClosedUnmerged
	}
	return row, nil
}
