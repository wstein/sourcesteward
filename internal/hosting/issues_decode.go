// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"encoding/json"
	"time"
)

// CodeIssuesDisabled is the code of a 410 answer to the issue list: the repository has
// issues disabled. It is not a failure; the caller reports the repository as skipped.
const CodeIssuesDisabled = "issues-disabled"

// issuesPageEntries is the size of one issue list page (per_page=100).
const issuesPageEntries = 100

// GhostLogin is the author reported for an issue whose user GitHub no longer knows
// (`user` is null). It is a placeholder, not a bot.
const GhostLogin = "ghost"

// IssueListItem is one open issue of a list page with the fields `stw issues` reports
// (Spec 038). Optional fields that GitHub omits or sets to null stay empty (CreatedAt zero,
// Comments nil). Labels and Assignees keep the API order and hold only entries that carry a
// string name or login.
type IssueListItem struct {
	Number     int
	Title      string
	Author     string // user.login; GhostLogin when the user is null
	AuthorType string // GitHub user.type: User, Bot, Organization, ...; empty for the ghost
	HTMLURL    string
	CreatedAt  time.Time
	Comments   *int
	Labels     []string
	Assignees  []string
}

// ParseIssueList decodes the body of one open issue list page. GitHub returns pull requests
// in this list: every entry that has a `pull_request` key (whatever its value, `null`
// included) is dropped and counted in pullsDropped, without looking at its other fields.
// The page is a JSON array of at most 100 entries (more is CodePageTooLarge); a kept entry
// needs a number of at least 1 and a created_at that parses as RFC 3339 when present.
func ParseIssueList(body []byte) (items []IssueListItem, pullsDropped int, err error) {
	var entries []json.RawMessage
	if json.Unmarshal(body, &entries) != nil || entries == nil {
		return nil, 0, malformed("issue list page is not a JSON array")
	}
	if len(entries) > issuesPageEntries {
		return nil, 0, &WatchError{Code: CodePageTooLarge, Message: "issue list page has more than 100 entries"}
	}
	items = make([]IssueListItem, 0, len(entries))
	for _, entry := range entries {
		var probe struct {
			PullRequest json.RawMessage `json:"pull_request"`
		}
		if json.Unmarshal(entry, &probe) != nil {
			return nil, 0, malformed("issue list entry is not an object")
		}
		if probe.PullRequest != nil {
			pullsDropped++
			continue
		}
		item, err := parseIssueEntry(entry)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, pullsDropped, nil
}

func parseIssueEntry(entry json.RawMessage) (IssueListItem, error) {
	var raw struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		HTMLURL   string `json:"html_url"`
		CreatedAt string `json:"created_at"`
		Comments  *int   `json:"comments"`
		User      *struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"user"`
		Labels    []json.RawMessage `json:"labels"`
		Assignees []json.RawMessage `json:"assignees"`
	}
	if json.Unmarshal(entry, &raw) != nil {
		return IssueListItem{}, malformed("issue list entry has a field of the wrong type")
	}
	if raw.Number < 1 || (raw.Comments != nil && *raw.Comments < 0) {
		return IssueListItem{}, malformed("issue list entry without a valid number or comment count")
	}
	item := IssueListItem{Number: raw.Number, Title: raw.Title, HTMLURL: raw.HTMLURL, Comments: raw.Comments, Author: GhostLogin}
	if raw.CreatedAt != "" {
		t, err := time.Parse(time.RFC3339, raw.CreatedAt)
		if err != nil {
			return IssueListItem{}, malformed("issue list entry with an unusable created_at")
		}
		item.CreatedAt = t
	}
	if raw.User != nil && raw.User.Login != "" {
		item.Author, item.AuthorType = raw.User.Login, raw.User.Type
	}
	for _, l := range raw.Labels {
		var label struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(l, &label) == nil && label.Name != "" {
			item.Labels = append(item.Labels, label.Name)
		}
	}
	for _, a := range raw.Assignees {
		var assignee struct {
			Login string `json:"login"`
		}
		if json.Unmarshal(a, &assignee) == nil && assignee.Login != "" {
			item.Assignees = append(item.Assignees, assignee.Login)
		}
	}
	return item, nil
}

// ParseViewer decodes the answer of `user` and returns the login the token resolves to. The
// login must follow the GitHub login rules (ValidOwner); anything else is malformed, so no
// hostile text reaches a comparison or an output.
func ParseViewer(body []byte) (string, error) {
	var doc struct {
		Login string `json:"login"`
	}
	if json.Unmarshal(body, &doc) != nil || !ValidOwner(doc.Login) {
		return "", malformed("user response has no valid login")
	}
	return doc.Login, nil
}
