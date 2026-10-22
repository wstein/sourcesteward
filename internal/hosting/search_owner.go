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
)

// This file is the owner mode of `stw pulls` (Spec 036, amendment of Spec 022 FR-005):
// exactly one fixed `search/issues` request shape, admitted only in guardPulls, used
// only to discover the repositories of an owner that have open pull requests.

// Codes added by the search read (the other codes are in watch_reader.go).
const (
	CodeSearchIncomplete = "search-incomplete"
	CodeSearchTooMany    = "search-too-many"
)

const (
	searchPerPage   = 100
	searchMaxPages  = 10
	searchResultCap = 1000
)

// ValidOwner reports whether s follows the GitHub login rules: 1 to 39 ASCII letters,
// digits and single interior hyphens (none leading, trailing or doubled). The check is
// byte-wise on purpose, so no encoded or Unicode form passes.
func ValidOwner(s string) bool {
	if len(s) < 1 || len(s) > 39 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '-' && s[i-1] == '-' {
			return false
		}
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// SearchEndpoint is the one generator of the PR search request (path and query, no API prefix).
func SearchEndpoint(owner string, page int) (string, error) {
	return ownerSearchEndpoint("pr", owner, page)
}

// ownerSearchEndpoint generates the fixed owner search of one item kind: "pr" for
// `stw pulls` (Spec 036) and "issue" for `stw issues` (Spec 038). Nothing else is generated.
func ownerSearchEndpoint(kind, owner string, page int) (string, error) {
	if !ValidOwner(owner) {
		return "", malformed("invalid owner")
	}
	if page < 1 || page > searchMaxPages {
		return "", malformed("search page is outside 1..10")
	}
	return "search/issues?q=user%3A" + owner + "+is%3A" + kind + "+is%3Aopen+archived%3Afalse&per_page=100&page=" + strconv.Itoa(page) + "&sort=created&order=asc", nil
}

func ownerSearchRoute(kind string) *regexp.Regexp {
	return regexp.MustCompile(`\Asearch/issues\?q=user%3A([A-Za-z0-9-]+)\+is%3A` + kind + `\+is%3Aopen\+archived%3Afalse&per_page=100&page=([1-9][0-9]?)&sort=created&order=asc\z`)
}

var (
	searchRoute      = ownerSearchRoute("pr")
	issueSearchRoute = ownerSearchRoute("issue")
)

// matchOwnerSearch matches exactly what ownerSearchEndpoint generates: the match is
// re-derived through the generator, so the two can never drift apart.
func matchOwnerSearch(route *regexp.Regexp, kind, p string) bool {
	m := route.FindStringSubmatch(p)
	if m == nil {
		return false
	}
	page, err := strconv.Atoi(m[2])
	if err != nil {
		return false
	}
	want, err := ownerSearchEndpoint(kind, m[1], page)
	return err == nil && want == p
}

// searchReadEndpoint matches the PR owner search.
func searchReadEndpoint(p string) bool { return matchOwnerSearch(searchRoute, "pr", p) }

// admitsSearch is true for the pulls guard only: it is the admission of the PR owner search.
// The issue owner search (Spec 038) has its own predicate, admitsIssueSearch, so the two
// guards refuse each other's search. Watch, status and one-shot reach neither.
func admitsSearch(m guardMode) bool { return m == guardPulls }

// SearchOwnerPage reads one page (1..10) of the owner search. It needs a reader built
// for pulls mode; any other reader refuses before a request is made.
func (r *WatchReader) SearchOwnerPage(ctx context.Context, owner string, page int) (WatchResponse, error) {
	if !admitsSearch(r.mode) {
		return WatchResponse{}, malformed("search is not allowed for this reader")
	}
	ep, err := SearchEndpoint(owner, page)
	if err != nil {
		return WatchResponse{}, err
	}
	return r.fetch(ctx, KindSearch, ep, "", true)
}

// OwnerSearchPage is one decoded search page.
type OwnerSearchPage struct {
	Repos      []string // owner/name in the order of first appearance, de-duplicated case-insensitively
	Ignored    int      // results dropped because their repository_url is unusable or foreign
	TotalCount int
	Incomplete bool
}

// ParseOwnerSearch decodes one search page. Each result's repository_url must name a
// repository of owner (compared case-insensitively) on the API host of host; anything
// else is ignored and counted. A page that is not the documented document is malformed.
func ParseOwnerSearch(body []byte, owner, host string) (OwnerSearchPage, error) {
	var doc struct {
		Total      *int             `json:"total_count"`
		Incomplete *bool            `json:"incomplete_results"`
		Items      *json.RawMessage `json:"items"`
	}
	if json.Unmarshal(body, &doc) != nil || doc.Total == nil || *doc.Total < 0 || doc.Incomplete == nil || doc.Items == nil {
		return OwnerSearchPage{}, malformed("search response is not a search document")
	}
	var items []struct {
		Number  int    `json:"number"`
		RepoURL string `json:"repository_url"`
	}
	if json.Unmarshal(*doc.Items, &items) != nil || items == nil || len(items) > searchPerPage {
		return OwnerSearchPage{}, malformed("search items are not a bounded array")
	}
	apiHost, prefix := apiDestination(host)
	out := OwnerSearchPage{TotalCount: *doc.Total, Incomplete: *doc.Incomplete}
	seen := map[string]bool{}
	for _, it := range items {
		repo, ok := repoFromURL(it.RepoURL, apiHost, prefix, owner)
		if !ok || it.Number < 1 {
			out.Ignored++
			continue
		}
		if k := strings.ToLower(repo); !seen[k] {
			seen[k] = true
			out.Repos = append(out.Repos, repo)
		}
	}
	return out, nil
}

// repoFromURL extracts owner/name from https://APIHOST/PREFIX repos/owner/name.
func repoFromURL(raw, apiHost, prefix, owner string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != apiHost || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" {
		return "", false
	}
	rest, ok := strings.CutPrefix(u.Path, prefix+"repos/")
	if !ok || !validRepo(rest) {
		return "", false
	}
	if !strings.EqualFold(strings.SplitN(rest, "/", 2)[0], owner) {
		return "", false
	}
	return rest, true
}

// OwnerDiscovery is the outcome of discovering one owner. Code is empty on full
// success; otherwise it names why the result is partial or empty (a watch-reader code,
// CodeSearchIncomplete or CodeSearchTooMany) and Repos holds what was seen. Message is
// fixed, bounded text.
type OwnerDiscovery struct {
	Owner   string
	Repos   []string
	Ignored int
	Code    string
	Message string
}

// MessageIgnoredResults is the Message of a discovery whose only problem is ignored results.
const MessageIgnoredResults = "search results with an unusable repository were ignored"

// DiscoverOwner pages through the owner search (at most 10 requests) and never panics
// or returns an error: every failure is a Code. Precedence: a failed request stops the
// run (Code is its code), then incomplete_results, then the 1000 result cap, then
// ignored results (malformed-response).
func (r *WatchReader) DiscoverOwner(ctx context.Context, owner string) OwnerDiscovery {
	d := OwnerDiscovery{Owner: owner}
	if !admitsSearch(r.mode) {
		d.Code, d.Message = CodeMalformed, "search is not allowed for this reader"
		return d
	}
	if !ValidOwner(owner) {
		d.Code, d.Message = CodeMalformed, "invalid owner"
		return d
	}
	seen := map[string]bool{}
	incomplete, tooMany := false, false
	for page := 1; ; page++ {
		res, err := r.SearchOwnerPage(ctx, owner, page)
		if err != nil {
			d.Code, d.Message = failureCode(err)
			return d
		}
		pg, err := ParseOwnerSearch(res.Body, owner, r.api.host)
		if err != nil {
			d.Code, d.Message = failureCode(err)
			return d
		}
		d.Ignored += pg.Ignored
		incomplete = incomplete || pg.Incomplete
		tooMany = tooMany || pg.TotalCount > searchResultCap
		for _, repo := range pg.Repos {
			if k := strings.ToLower(repo); !seen[k] {
				seen[k] = true
				d.Repos = append(d.Repos, repo)
			}
		}
		next, more := nextPageFromLink(res.Link)
		if res.Link != "" && !more && hasNextRel(res.Link) {
			d.Code, d.Message = CodeMalformed, "unusable Link header"
			return d
		}
		if !more {
			break
		}
		if next != page+1 {
			d.Code, d.Message = CodeMalformed, "Link next is not the following page"
			return d
		}
		if page >= searchMaxPages {
			tooMany = true
			break
		}
	}
	switch {
	case incomplete:
		d.Code, d.Message = CodeSearchIncomplete, "search results are incomplete"
	case tooMany:
		d.Code, d.Message = CodeSearchTooMany, "more than 1000 search results; some repositories may be missing"
	case d.Ignored > 0:
		d.Code, d.Message = CodeMalformed, MessageIgnoredResults
	}
	return d
}

func failureCode(err error) (string, string) {
	if we, ok := err.(*WatchError); ok {
		return we.Code, we.Message
	}
	return CodeNetwork, "search read failed"
}

// DiscoverOwners discovers each distinct owner (case-insensitively) in order. After a
// throttle, or when the search bucket is known to be empty until a future reset, the
// remaining owners are not sent and report rate-limited.
func (r *WatchReader) DiscoverOwners(ctx context.Context, owners []string) []OwnerDiscovery {
	var out []OwnerDiscovery
	seen := map[string]bool{}
	for _, o := range owners {
		k := strings.ToLower(o)
		if seen[k] {
			continue
		}
		seen[k] = true
		if r.searchExhausted() {
			out = append(out, OwnerDiscovery{Owner: o, Code: CodeRateLimited, Message: "search rate limit reached"})
			continue
		}
		out = append(out, r.DiscoverOwner(ctx, o))
	}
	return out
}

// searchExhausted is true when the last search response showed no budget left
// before its reset time.
func (r *WatchReader) searchExhausted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.searchLast.Known && r.searchLast.Remaining == 0 && time.Now().Before(r.searchLast.Reset)
}
