// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
)

// This file holds the reads of `stw lint pr` (Spec 050, amendment of Spec 022 FR-005): the conversation
// comments of one pull request, 30 per page, pages 1..10. The lint guard admits them, the commit pages of
// the watch set (pulls/N/commits, 100 per page, pages 1..3) and the one-shot set (pulls/N); it admits no
// other watch shape. The comment shape differs from every other comment shape on purpose (30 per page).

// KindLintComments is the call-accounting kind of the lint comment pages.
const KindLintComments WatchKind = "lint-comments"

const (
	lintCommentsPerPage = 30
	// LintCommentsMaxPage is the highest comment page the shape admits (300 comments).
	LintCommentsMaxPage = 10
)

// LintCommentsEndpoint is the one generator of the comment list of a pull request for the lint reader
// (path and query, no API prefix). number is the pull request number, page 1..10.
func LintCommentsEndpoint(repo string, number, page int) (string, error) {
	base, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	if number < 1 {
		return "", malformed("invalid pull request number")
	}
	if page < 1 || page > LintCommentsMaxPage {
		return "", malformed("comments page is outside 1..10")
	}
	return base + "/issues/" + strconv.Itoa(number) + "/comments?per_page=" + strconv.Itoa(lintCommentsPerPage) + "&page=" + strconv.Itoa(page), nil
}

var lintRoute = regexp.MustCompile(`\Arepos/([A-Za-z0-9_-][A-Za-z0-9_.-]*/[A-Za-z0-9_-][A-Za-z0-9_.-]*)/(?:issues/([1-9][0-9]{0,9})/comments\?per_page=30&page=([1-9][0-9]?)|pulls/[1-9][0-9]{0,9}/commits\?per_page=100&page=[1-3])\z`)

// lintReadEndpoint matches the comment generator's output and the commit pages. The comment match is
// re-derived through the generator, so the two can never drift apart.
func lintReadEndpoint(p string) bool {
	m := lintRoute.FindStringSubmatch(p)
	if m == nil || !validRepo(m[1]) {
		return false
	}
	if m[2] == "" {
		return true
	}
	n, e1 := strconv.Atoi(m[2])
	page, e2 := strconv.Atoi(m[3])
	if e1 != nil || e2 != nil {
		return false
	}
	want, err := LintCommentsEndpoint(m[1], n, page)
	return err == nil && want == p
}

// admitsLintReads is true for the lint guard only.
func (m guardMode) admitsLintReads() bool { return m == guardLint }

// NewLintReader is the reader of the stateless `stw lint pr` (Spec 050): guard mode lint and the
// DefaultRequestTimeout per request.
func NewLintReader(ctx context.Context, target, host string) (*WatchReader, func(), error) {
	return NewLintReaderWithTransport(ctx, target, host, nil)
}

// NewLintReaderWithTransport is the in-process injection seam for fixtures.
func NewLintReaderWithTransport(ctx context.Context, target, host string, rt http.RoundTripper) (*WatchReader, func(), error) {
	api, cleanup, err := newGHMode(ctx, target, host, rt, guardLint)
	if err != nil {
		return nil, cleanup, err
	}
	r := newWatchReader(api)
	r.mode = guardLint
	return r, cleanup, nil
}

// LintComments reads one page (1..10) of the comments of a pull request. Only the lint reader may read it;
// the request is refused for any other reader before it is made. The `Link` header is only returned.
func (r *WatchReader) LintComments(ctx context.Context, repo string, number, page int) (WatchResponse, error) {
	if !r.mode.admitsLintReads() {
		return WatchResponse{}, malformed("lint reads are not allowed for this reader")
	}
	ep, err := LintCommentsEndpoint(repo, number, page)
	if err != nil {
		return WatchResponse{}, err
	}
	return r.fetch(ctx, KindLintComments, ep, "", true)
}
