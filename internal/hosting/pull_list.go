// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"encoding/json"
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
