// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"regexp"
	"strconv"
)

// This file holds the three fixed read-only shapes of `stw issues` and of the issue
// source of `stw status` (Spec 038, amendment of Spec 022 FR-005): the per-repository
// open issue list, the authenticated user and the issue owner search. Each is produced
// by one generator and matched exactly, the match being re-derived through the generator.

// UserEndpoint is the one fixed read of the authenticated user (no query).
const UserEndpoint = "user"

// issuesMaxPages bounds the issue list: 10 pages of 100 are 1000 entries, pull requests included.
const issuesMaxPages = 10

// IssuesEndpoint is the one generator of the open issue list of repo (path and query, no
// API prefix). The page is typed: 1..10.
func IssuesEndpoint(repo string, page int) (string, error) {
	base, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	if page < 1 || page > issuesMaxPages {
		return "", malformed("issues page is outside 1..10")
	}
	return base + "/issues?state=open&per_page=100&sort=created&direction=asc&page=" + strconv.Itoa(page), nil
}

var issuesRoute = regexp.MustCompile(`\Arepos/([A-Za-z0-9_-][A-Za-z0-9_.-]*/[A-Za-z0-9_-][A-Za-z0-9_.-]*)/issues\?state=open&per_page=100&sort=created&direction=asc&page=([1-9][0-9]?)\z`)

// issuesListReadEndpoint matches exactly what IssuesEndpoint generates.
func issuesListReadEndpoint(p string) bool {
	m := issuesRoute.FindStringSubmatch(p)
	if m == nil {
		return false
	}
	page, err := strconv.Atoi(m[2])
	if err != nil {
		return false
	}
	want, err := IssuesEndpoint(m[1], page)
	return err == nil && want == p
}

// issueReadEndpoint matches the two reads of a repository and the viewer: the issue list and `user`.
func issueReadEndpoint(p string) bool { return issuesListReadEndpoint(p) || p == UserEndpoint }

// IssueSearchEndpoint is the one generator of the issue owner search request. It is the
// shape of SearchEndpoint with `is:issue` instead of `is:pr`.
func IssueSearchEndpoint(owner string, page int) (string, error) {
	return ownerSearchEndpoint("issue", owner, page)
}

// issueSearchReadEndpoint matches exactly what IssueSearchEndpoint generates.
func issueSearchReadEndpoint(p string) bool { return matchOwnerSearch(issueSearchRoute, "issue", p) }
