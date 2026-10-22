// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// PullListItem is one open pull request of a list page with the extra fields that
// `stw pulls` reports (Spec 036). It is a separate type on purpose: PullItem is the
// stored form of watch state.json and is not widened. Optional fields that GitHub
// omits or sets to null stay empty (CreatedAt zero).
type PullListItem struct {
	Number    int
	Title     string
	User      string
	UserType  string // GitHub user.type: User, Bot, Organization, ...
	Draft     bool
	HTMLURL   string
	CreatedAt time.Time
	HeadSHA   string
	HeadRef   string
	BaseRef   string
	Labels    []string
	Reviewers []string // requested_reviewers logins
	Teams     []string // requested_teams slugs
}

// ParsePullList decodes the body of one `pulls?state=open` list page into
// PullListItem values. Number and head SHA are required exactly as in the watch
// decoder; created_at must parse as RFC 3339 when present.
func ParsePullList(body []byte) ([]PullListItem, error) {
	var raw []struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		HTMLURL   string `json:"html_url"`
		CreatedAt string `json:"created_at"`
		User      *struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"user"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		Draft     bool `json:"draft"`
		Reviewers []struct {
			Login string `json:"login"`
		} `json:"requested_reviewers"`
		Teams []struct {
			Slug string `json:"slug"`
		} `json:"requested_teams"`
		Head struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, malformed("list page is not a JSON array of pull requests")
	}
	items := make([]PullListItem, 0, len(raw))
	for _, p := range raw {
		if p.Number < 1 || !validSHA(p.Head.SHA) {
			return nil, malformed("list item without a valid number or head SHA")
		}
		it := PullListItem{Number: p.Number, Title: p.Title, Draft: p.Draft, HTMLURL: p.HTMLURL, HeadSHA: p.Head.SHA, HeadRef: p.Head.Ref, BaseRef: p.Base.Ref}
		if p.CreatedAt != "" {
			t, err := time.Parse(time.RFC3339, p.CreatedAt)
			if err != nil {
				return nil, malformed("list item with an unusable created_at")
			}
			it.CreatedAt = t
		}
		if p.User != nil {
			it.User, it.UserType = p.User.Login, p.User.Type
		}
		for _, l := range p.Labels {
			it.Labels = append(it.Labels, l.Name)
		}
		for _, r := range p.Reviewers {
			it.Reviewers = append(it.Reviewers, r.Login)
		}
		for _, tm := range p.Teams {
			it.Teams = append(it.Teams, tm.Slug)
		}
		items = append(items, it)
	}
	return items, nil
}

// ListOpenPullItems lists every open PR of repo for the stateless `stw pulls` reader:
// the same pages, Link rules and halving as ListOpenPulls, but no validators and the
// extra fields of PullListItem. It stops with CodeTooManyPRs above maxOpen
// (1..WatchMaxOpenPRs) PRs.
func (r *WatchReader) ListOpenPullItems(ctx context.Context, repo string, maxOpen int) ([]PullListItem, error) {
	if maxOpen < 1 || maxOpen > WatchMaxOpenPRs {
		return nil, malformed("max_open_prs is outside 1..2000")
	}
	perPage := 25
	if r.isHalved(repo) {
		perPage = 12
	}
	for {
		items, err := r.listItemsAt(ctx, repo, perPage, maxOpen)
		var we *WatchError
		if errors.As(err, &we) && we.Code == CodePageTooLarge && perPage == 25 {
			r.markHalved(repo)
			perPage = 12 // restart at page 1: page numbers of different sizes are never mixed
			continue
		}
		return items, err
	}
}

func (r *WatchReader) listItemsAt(ctx context.Context, repo string, perPage, maxOpen int) ([]PullListItem, error) {
	maxPages := (maxOpen + perPage - 1) / perPage
	if limit := listPageLimit(perPage); maxPages > limit {
		maxPages = limit
	}
	var out []PullListItem
	for page := 1; ; page++ {
		res, err := r.PullListPage(ctx, repo, perPage, page, "")
		if err != nil {
			return nil, err
		}
		if res.NotModified {
			return nil, malformed("unexpected 304 for an unconditional read")
		}
		items, err := ParsePullList(res.Body)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(out) > maxOpen {
			return nil, &WatchError{Code: CodeTooManyPRs, Message: "more open pull requests than max_open_prs"}
		}
		next, more := nextPageFromLink(res.Link)
		if res.Link != "" && !more && hasNextRel(res.Link) {
			return nil, malformed("unusable Link header")
		}
		if !more {
			return out, nil
		}
		if next != page+1 {
			return nil, malformed("Link next is not the following page")
		}
		if page >= maxPages {
			return nil, &WatchError{Code: CodeTooManyPRs, Message: "more open pull requests than max_open_prs"}
		}
	}
}
