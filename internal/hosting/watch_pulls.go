// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"errors"
)

// PullItem is the part of a pulls list item that the watcher keeps (Spec 035).
// The field tags are the stored form in state.json.
type PullItem struct {
	Number  int      `json:"number"`
	Title   string   `json:"title"`
	User    string   `json:"user"`
	Labels  []string `json:"labels"`
	Draft   bool     `json:"draft"`
	HeadSHA string   `json:"head_sha"`
	HeadRef string   `json:"head_ref"`
	BaseRef string   `json:"base_ref"`
	BaseSHA string   `json:"base_sha"`
}

// PullPage is one stored list page: the ETag and Link header of the response and
// the parsed items, so a 304 needs no body. The caller keeps it in state.json.
type PullPage struct {
	ETag  string     `json:"etag"`
	Link  string     `json:"link"`
	Items []PullItem `json:"items"`
}

// PullListState is everything that must be stored between two listings of one
// repository. PerPage is 0 (none stored), 25 or 12. Pages are those of a listing at
// PerPage, in order. PerPage only says what size the stored pages have, so that their
// ETags are reused only for that size; it never selects the size of the next listing. The halved size is kept in memory by
// the WatchReader until restart, so the stored state must not be used to pin it.
type PullListState struct {
	PerPage int        `json:"per_page"`
	Pages   []PullPage `json:"pages"`
}

// PullListResult is a complete listing. Unchanged is true when every page was a 304.
// State must be stored by the caller (and passed to the next call).
type PullListResult struct {
	State     PullListState
	Items     []PullItem
	Unchanged bool
}

func listPageLimit(perPage int) int {
	if perPage == 12 {
		return watchMaxPages12
	}
	return watchMaxPages25
}

// ListOpenPulls lists the open PRs of repo with conditional requests. It follows the
// Link rel="next" header (the next page must be exactly the current one plus one),
// halves the page size to 12 on a body-limit error and restarts at page 1, and stops
// with CodeTooManyPRs above maxOpen (1..WatchMaxOpenPRs) PRs. A refused 301 is
// CodeRepoMoved. Nothing sleeps and nothing is retried except the halving.
func (r *WatchReader) ListOpenPulls(ctx context.Context, repo string, prior PullListState, maxOpen int) (PullListResult, error) {
	if maxOpen < 1 || maxOpen > WatchMaxOpenPRs {
		return PullListResult{}, malformed("max_open_prs is outside 1..2000")
	}
	perPage := 25
	if r.isHalved(repo) {
		perPage = 12
	}
	for {
		res, err := r.listAt(ctx, repo, perPage, prior, maxOpen)
		var we *WatchError
		if errors.As(err, &we) && we.Code == CodePageTooLarge && perPage == 25 {
			r.markHalved(repo)
			perPage = 12 // restart at page 1: page numbers of different sizes are never mixed
			continue
		}
		return res, err
	}
}

func (r *WatchReader) listAt(ctx context.Context, repo string, perPage int, prior PullListState, maxOpen int) (PullListResult, error) {
	usable := prior.PerPage == perPage
	maxPages := (maxOpen + perPage - 1) / perPage
	if limit := listPageLimit(perPage); maxPages > limit {
		maxPages = limit
	}
	out := PullListResult{State: PullListState{PerPage: perPage}, Unchanged: true}
	for page := 1; ; page++ {
		var stored *PullPage
		etag := ""
		if usable && page <= len(prior.Pages) && prior.Pages[page-1].ETag != "" {
			stored = &prior.Pages[page-1]
			etag = stored.ETag
		}
		res, err := r.PullListPage(ctx, repo, perPage, page, etag)
		if err != nil {
			return PullListResult{}, err
		}
		var entry PullPage
		if res.NotModified {
			if stored == nil {
				return PullListResult{}, malformed("304 for a page without stored data")
			}
			entry = PullPage{ETag: res.ETag, Link: stored.Link, Items: stored.Items}
		} else {
			items, perr := parsePullItems(res.Body)
			if perr != nil {
				return PullListResult{}, perr
			}
			entry = PullPage{ETag: res.ETag, Link: res.Link, Items: items}
			if len(items) > 0 || page <= len(prior.Pages) || !usable {
				out.Unchanged = false
			}
		}
		out.State.Pages = append(out.State.Pages, entry)
		out.Items = append(out.Items, entry.Items...)
		if len(out.Items) > maxOpen {
			return PullListResult{}, &WatchError{Code: CodeTooManyPRs, Message: "more open pull requests than max_open_prs"}
		}
		next, more := nextPageFromLink(entry.Link)
		if entry.Link != "" && !more && hasNextRel(entry.Link) {
			return PullListResult{}, malformed("unusable Link header")
		}
		if !more {
			// A 304 reuses the stored Link, so an exactly full last page may hide an older PR
			// that was reopened behind it: probe the next page once (conditional, allowlisted).
			if res.NotModified && len(entry.Items) == perPage && page < listPageLimit(perPage) {
				continue
			}
			if contentPages(prior.Pages) != contentPages(out.State.Pages) || prior.PerPage != perPage {
				out.Unchanged = false
			}
			return out, nil
		}
		if next != page+1 {
			return PullListResult{}, malformed("Link next is not the following page")
		}
		if page >= maxPages {
			return PullListResult{}, &WatchError{Code: CodeTooManyPRs, Message: "more open pull requests than max_open_prs"}
		}
	}
}

// contentPages counts the pages up to the last non-empty one; a trailing empty
// page is only the stored result of a probe past a full last page.
func contentPages(pages []PullPage) int {
	n := len(pages)
	for n > 0 && len(pages[n-1].Items) == 0 {
		n--
	}
	return n
}

func hasNextRel(link string) bool {
	for _, p := range splitRel(link) {
		if p == "next" {
			return true
		}
	}
	return false
}

func parsePullItems(body []byte) ([]PullItem, error) {
	var raw []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		Draft bool `json:"draft"`
		Head  struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, malformed("list page is not a JSON array of pull requests")
	}
	items := make([]PullItem, 0, len(raw))
	for _, p := range raw {
		if p.Number < 1 || !validSHA(p.Head.SHA) || (p.Base.SHA != "" && !validSHA(p.Base.SHA)) {
			return nil, malformed("list item without a valid number or head SHA")
		}
		it := PullItem{Number: p.Number, Title: p.Title, User: p.User.Login, Draft: p.Draft, HeadSHA: p.Head.SHA, HeadRef: p.Head.Ref, BaseRef: p.Base.Ref, BaseSHA: p.Base.SHA}
		for _, l := range p.Labels {
			it.Labels = append(it.Labels, l.Name)
		}
		items = append(items, it)
	}
	return items, nil
}
