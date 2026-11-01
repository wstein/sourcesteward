// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// This file is the read side of `stw land --stack` (Spec 043 FR-080, an amendment of the Spec 022
// allowlist): fixed, bounded GET shapes behind their own guard mode. The command's writes are not
// here: the landing push is a Git process and no forge write endpoint exists in chain mode (FR-070).

// landMaxPages bounds the open pull request list read for the side-child check (5 pages of 100).
const landMaxPages = 5

// landRoute lists the shapes added for the land reader. pulls/N, repos/O/R, the check runs and
// the statuses are part of the one-shot set; rulesets listings are part of the release set.
var landRoute = regexp.MustCompile(`\A(?:user|repos/[A-Za-z0-9_-][A-Za-z0-9_.-]*/[A-Za-z0-9_-][A-Za-z0-9_.-]*/(?:` +
	`pulls\?state=open&per_page=100&page=([1-5])|` +
	`pulls/[1-9][0-9]{0,8}/reviews\?per_page=100&page=1|` +
	`issues/[1-9][0-9]{0,8}/events\?per_page=100&page=1|` +
	`rules/branches/([^/?#]+)|` +
	`branches/([^/?#]+)/protection|` +
	`rulesets/[1-9][0-9]{0,11}))\z`)

func admitsLand(m guardMode) bool { return m == guardLand }

func landReadEndpoint(p string) bool {
	m := landRoute.FindStringSubmatch(p)
	if m == nil {
		return false
	}
	if p != "user" {
		parts := strings.SplitN(p, "/", 4)
		if !validRepo(parts[1]+"/"+parts[2]) || parts[1] == ".." || parts[2] == ".." {
			return false
		}
	}
	for _, seg := range m[2:] {
		if seg != "" && !branchSegmentValid(seg) {
			return false
		}
	}
	return true
}

func landRepo(repo string) (string, error) {
	if !validRepo(repo) || strings.HasPrefix(repo, "..") || strings.Contains(repo, "/..") {
		return "", fmt.Errorf("invalid repository name")
	}
	return "repos/" + repo, nil
}

func landNumber(n int) (string, error) {
	if n < 1 || n > 999999999 {
		return "", fmt.Errorf("invalid number")
	}
	return strconv.Itoa(n), nil
}

// LandOpenPullsEndpoint is the open pull request page 1..5.
func LandOpenPullsEndpoint(repo string, page int) (string, error) {
	base, err := landRepo(repo)
	if err != nil || page < 1 || page > landMaxPages {
		return "", fmt.Errorf("invalid land read")
	}
	return base + "/pulls?state=open&per_page=100&page=" + strconv.Itoa(page), nil
}

// LandReviewsEndpoint is the first page of the reviews of a pull request.
func LandReviewsEndpoint(repo string, number int) (string, error) {
	base, err := landRepo(repo)
	n, e2 := landNumber(number)
	if err != nil || e2 != nil {
		return "", fmt.Errorf("invalid land read")
	}
	return base + "/pulls/" + n + "/reviews?per_page=100&page=1", nil
}

// LandEventsEndpoint is the first page of the issue events of a pull request.
func LandEventsEndpoint(repo string, number int) (string, error) {
	base, err := landRepo(repo)
	n, e2 := landNumber(number)
	if err != nil || e2 != nil {
		return "", fmt.Errorf("invalid land read")
	}
	return base + "/issues/" + n + "/events?per_page=100&page=1", nil
}

// LandRulesEndpoint is the rules in force for a branch.
func LandRulesEndpoint(repo, branch string) (string, error) {
	base, err := landRepo(repo)
	seg, e2 := EncodeBranchSegment(branch)
	if err != nil || e2 != nil {
		return "", fmt.Errorf("invalid land read")
	}
	return base + "/rules/branches/" + seg, nil
}

// LandProtectionEndpoint is the classic branch protection of a branch.
func LandProtectionEndpoint(repo, branch string) (string, error) {
	base, err := landRepo(repo)
	seg, e2 := EncodeBranchSegment(branch)
	if err != nil || e2 != nil {
		return "", fmt.Errorf("invalid land read")
	}
	return base + "/branches/" + seg + "/protection", nil
}

// LandRulesetEndpoint is one ruleset (its bypass actors).
func LandRulesetEndpoint(repo string, id int) (string, error) {
	base, err := landRepo(repo)
	n, e2 := landNumber(id)
	if err != nil || e2 != nil {
		return "", fmt.Errorf("invalid land read")
	}
	return base + "/rulesets/" + n, nil
}

// NewLandReader qualifies gh like the other readers and builds a client whose guard admits the
// land read shapes only.
func NewLandReader(ctx context.Context, target, host string) (*WatchReader, func(), error) {
	return NewLandReaderWithTransport(ctx, target, host, nil)
}

// NewLandReaderWithTransport is the in-process injection seam for fixtures.
func NewLandReaderWithTransport(ctx context.Context, target, host string, rt http.RoundTripper) (*WatchReader, func(), error) {
	api, cleanup, err := newGHMode(ctx, target, host, rt, guardLand)
	if err != nil {
		return nil, cleanup, err
	}
	r := newWatchReader(api)
	r.mode = guardLand
	return r, cleanup, nil
}

// LandRead reads one endpoint of the land set. It needs a reader built by NewLandReader; any other
// reader refuses before a request is made. A 403 or 404 is a *WatchError carrying its Status, so
// the caller can tell an unreadable ruleset (403) from an absent protection (404).
func (r *WatchReader) LandRead(ctx context.Context, endpoint string) (WatchResponse, error) {
	if !admitsLand(r.mode) {
		return WatchResponse{}, malformed("land reads are not allowed for this reader")
	}
	return r.fetch(ctx, landKind(endpoint), endpoint, "", strings.Contains(endpoint, "per_page"))
}

func landKind(endpoint string) WatchKind {
	switch {
	case strings.HasPrefix(endpoint, "user"):
		return KindUser
	case strings.Contains(endpoint, "/pulls?"):
		return KindList
	}
	return KindDetail
}

// LandEndpointAllowed reports whether an endpoint is readable in land mode: the land shapes plus
// the one-shot set (repository, pull request, check runs, statuses).
func LandEndpointAllowed(p string) bool { return landReadEndpoint(p) || transportReadEndpoint(p) }
