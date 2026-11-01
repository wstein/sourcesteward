// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// This file holds the reads of the review state (Spec 046, amendment of Spec 022 FR-005): the
// comments of one pull request, one page of recently closed pull requests, and the decoders of
// both and of the head commit. The two request shapes are admitted by the pulls and status guards
// only; each is produced by one generator and matched exactly.

// Kinds of the review reads, for the per-kind call accounting of the reader.
const (
	KindComments WatchKind = "comments"
	KindClosed   WatchKind = "closed"
)

const (
	commentsPerPage = 50
	// commentsMaxPage is the highest page number the shape admits (999 pages of 50 comments).
	commentsMaxPage = 999
	// commentsReadPages bounds the pages read for one pull request: the first and the last five.
	commentsReadPages = 6
	// maxCommentBody bounds the part of a comment body that is kept: the first line is what matters.
	maxCommentBody = 4096
)

// IssueCommentsEndpoint is the one generator of the comment list of a pull request (path and query,
// no API prefix). number is the pull request number, page 1..999.
func IssueCommentsEndpoint(repo string, number, page int) (string, error) {
	base, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	if number < 1 {
		return "", malformed("invalid pull request number")
	}
	if page < 1 || page > commentsMaxPage {
		return "", malformed("comments page is outside 1..999")
	}
	return base + "/issues/" + strconv.Itoa(number) + "/comments?per_page=" + strconv.Itoa(commentsPerPage) + "&page=" + strconv.Itoa(page), nil
}

// ClosedPullsEndpoint is the one generator of the closed pull request list: the 100 most recently
// updated, page 1 only.
func ClosedPullsEndpoint(repo string) (string, error) {
	base, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	return base + "/pulls?state=closed&sort=updated&direction=desc&per_page=100&page=1", nil
}

var (
	commentsRoute = regexp.MustCompile(`\Arepos/([A-Za-z0-9_-][A-Za-z0-9_.-]*/[A-Za-z0-9_-][A-Za-z0-9_.-]*)/issues/([1-9][0-9]{0,9})/comments\?per_page=50&page=([1-9][0-9]{0,2})\z`)
	closedRoute   = regexp.MustCompile(`\Arepos/([A-Za-z0-9_-][A-Za-z0-9_.-]*/[A-Za-z0-9_-][A-Za-z0-9_.-]*)/pulls\?state=closed&sort=updated&direction=desc&per_page=100&page=1\z`)
)

// reviewReadEndpoint matches exactly what the two generators produce: the match is re-derived
// through the generator, so the two can never drift apart.
func reviewReadEndpoint(p string) bool {
	if m := commentsRoute.FindStringSubmatch(p); m != nil {
		n, e1 := strconv.Atoi(m[2])
		page, e2 := strconv.Atoi(m[3])
		if e1 != nil || e2 != nil {
			return false
		}
		want, err := IssueCommentsEndpoint(m[1], n, page)
		return err == nil && want == p
	}
	if m := closedRoute.FindStringSubmatch(p); m != nil {
		want, err := ClosedPullsEndpoint(m[1])
		return err == nil && want == p
	}
	return false
}

// admitsReviewReads is true for the pulls and status guards: the readers of the review state.
func (m guardMode) admitsReviewReads() bool { return m == guardPulls || m == guardStatus }

// IssueComments reads one page (1..999) of the comments of a pull request.
func (r *WatchReader) IssueComments(ctx context.Context, repo string, number, page int) (WatchResponse, error) {
	if !r.mode.admitsReviewReads() {
		return WatchResponse{}, malformed("review reads are not allowed for this reader")
	}
	ep, err := IssueCommentsEndpoint(repo, number, page)
	if err != nil {
		return WatchResponse{}, err
	}
	return r.fetch(ctx, KindComments, ep, "", true)
}

// ClosedPulls reads the 100 most recently updated closed pull requests of a repository.
func (r *WatchReader) ClosedPulls(ctx context.Context, repo string) (WatchResponse, error) {
	if !r.mode.admitsReviewReads() {
		return WatchResponse{}, malformed("review reads are not allowed for this reader")
	}
	ep, err := ClosedPullsEndpoint(repo)
	if err != nil {
		return WatchResponse{}, err
	}
	return r.fetch(ctx, KindClosed, ep, "", true)
}

// IssueComment is one comment of a pull request. URL is empty unless it is an https URL; Body is
// cut at 4096 bytes (only its first line is ever read).
type IssueComment struct {
	ID                   int64
	CreatedAt, UpdatedAt time.Time
	Body, URL            string
}

// ParseIssueComments decodes one comment page. id and created_at are required.
func ParseIssueComments(body []byte) ([]IssueComment, error) {
	var raw []struct {
		ID        int64   `json:"id"`
		HTMLURL   string  `json:"html_url"`
		CreatedAt string  `json:"created_at"`
		UpdatedAt string  `json:"updated_at"`
		Body      *string `json:"body"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, malformed("comment page is not a JSON array")
	}
	out := make([]IssueComment, 0, len(raw))
	for _, c := range raw {
		created, err := time.Parse(time.RFC3339, c.CreatedAt)
		if c.ID < 1 || err != nil {
			return nil, malformed("comment without a valid id or created_at")
		}
		updated := created
		if c.UpdatedAt != "" {
			if updated, err = time.Parse(time.RFC3339, c.UpdatedAt); err != nil {
				return nil, malformed("comment with an unusable updated_at")
			}
		}
		ic := IssueComment{ID: c.ID, CreatedAt: created, UpdatedAt: updated}
		if c.Body != nil {
			ic.Body = cutUTF8(*c.Body, maxCommentBody)
		}
		if strings.HasPrefix(c.HTMLURL, "https://") {
			ic.URL = cutUTF8(c.HTMLURL, 2048)
		}
		out = append(out, ic)
	}
	return out, nil
}

func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// ReadIssueComments reads the comments of a pull request, oldest first. A long thread is read
// partially: the first page and the last five pages, so that the newest comments (where a verdict
// is) are never missed; truncated is then true. Without a rel="last" link the pages are followed
// one by one up to six.
func (r *WatchReader) ReadIssueComments(ctx context.Context, repo string, number int) (comments []IssueComment, truncated bool, err error) {
	read := func(page int) (WatchResponse, error) {
		res, err := r.IssueComments(ctx, repo, number, page)
		if err != nil {
			return res, err
		}
		if res.NotModified {
			return res, malformed("unexpected 304 for an unconditional read")
		}
		items, err := ParseIssueComments(res.Body)
		if err != nil {
			return res, err
		}
		comments = append(comments, items...)
		return res, nil
	}
	first, err := read(1)
	if err != nil {
		return nil, false, err
	}
	next, more := nextPageFromLink(first.Link)
	if first.Link != "" && !more && hasNextRel(first.Link) {
		return nil, false, malformed("unusable Link header")
	}
	if !more {
		return comments, false, nil
	}
	if next != 2 {
		return nil, false, malformed("Link next is not the following page")
	}
	last := lastPageFromLink(first.Link)
	switch {
	case last > commentsMaxPage:
		return nil, false, malformed("comment list is too long")
	case last == 0:
		for page := 2; ; page++ {
			res, err := read(page)
			if err != nil {
				return nil, false, err
			}
			n, more := nextPageFromLink(res.Link)
			if !more {
				return comments, false, nil
			}
			if n != page+1 {
				return nil, false, malformed("Link next is not the following page")
			}
			if page >= commentsReadPages {
				return comments, true, nil
			}
		}
	}
	from := 2
	if last > commentsReadPages {
		from, truncated = last-commentsReadPages+2, true
	}
	for page := from; page <= last; page++ {
		if _, err := read(page); err != nil {
			return nil, false, err
		}
	}
	return comments, truncated, nil
}

// lastPageFromLink returns the page number of the rel="last" entry of a Link header, 0 if none.
func lastPageFromLink(link string) int {
	for _, entry := range strings.Split(link, ",") {
		entry = strings.TrimSpace(entry)
		end := strings.Index(entry, ">")
		if !strings.HasPrefix(entry, "<") || end < 0 {
			continue
		}
		for _, param := range strings.Split(entry[end+1:], ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(param), "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(k), "rel") {
				continue
			}
			for _, rel := range strings.Fields(strings.Trim(strings.TrimSpace(v), `"`)) {
				if !strings.EqualFold(rel, "last") {
					continue
				}
				u, err := url.Parse(entry[1:end])
				if err != nil {
					return 0
				}
				page, err := strconv.Atoi(u.Query().Get("page"))
				if err != nil || page < 1 || page > 1000000 {
					return 0
				}
				return page
			}
		}
	}
	return 0
}

// ClosedPull is one closed pull request of the closed list. Fork is true when the head repository is
// missing or differs from the base repository.
type ClosedPull struct {
	Number  int
	HeadRef string
	Merged  bool
	Fork    bool
}

// ParseClosedPulls decodes the closed list. A pull request is merged when merged_at is set.
func ParseClosedPulls(body []byte) ([]ClosedPull, error) {
	var raw []struct {
		Number   int     `json:"number"`
		MergedAt *string `json:"merged_at"`
		Head     struct {
			Ref  string `json:"ref"`
			Repo *struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Repo *struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"base"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, malformed("closed list is not a JSON array")
	}
	out := make([]ClosedPull, 0, len(raw))
	for _, p := range raw {
		if p.Number < 1 {
			return nil, malformed("closed list item without a number")
		}
		fork := p.Head.Repo == nil || p.Base.Repo == nil || !strings.EqualFold(p.Head.Repo.FullName, p.Base.Repo.FullName)
		out = append(out, ClosedPull{Number: p.Number, HeadRef: p.Head.Ref, Merged: p.MergedAt != nil && *p.MergedAt != "", Fork: fork})
	}
	return out, nil
}

// ParseCommitDate decodes the committer date of a `commits/SHA` document: the time of the head commit.
func ParseCommitDate(body []byte) (time.Time, error) {
	var doc struct {
		Commit struct {
			Committer *struct {
				Date string `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Commit.Committer == nil {
		return time.Time{}, malformed("commit response has no committer date")
	}
	t, err := time.Parse(time.RFC3339, doc.Commit.Committer.Date)
	if err != nil {
		return time.Time{}, malformed("commit response has an unusable committer date")
	}
	return t, nil
}
