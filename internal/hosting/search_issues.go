// SPDX-License-Identifier: EUPL-1.2
package hosting

import "context"

// This file is the owner discovery of `stw issues` (Spec 038, US5): the issue twin of the
// PR owner search in search_owner.go. Exactly one fixed `search/issues` request shape,
// admitted only in guardIssues, used only to discover the repositories of an owner that have
// open issues. The items are decoded by ParseOwnerSearch, which keeps only the repository
// name: the issue data itself is always read through IssuesPage, never from the search.

// IssueSearchPage reads one page (1..10) of the issue owner search. It needs a reader built
// for issues mode; any other reader refuses before a request is made.
func (r *WatchReader) IssueSearchPage(ctx context.Context, owner string, page int) (WatchResponse, error) {
	if !admitsIssueSearch(r.mode) {
		return WatchResponse{}, malformed("issue search is not allowed for this reader")
	}
	ep, err := IssueSearchEndpoint(owner, page)
	if err != nil {
		return WatchResponse{}, err
	}
	return r.fetch(ctx, KindSearch, ep, "", true)
}

// DiscoverIssueOwner pages through the issue owner search (at most 10 requests) with the
// semantics of DiscoverOwner: it never returns an error, every failure is a Code.
func (r *WatchReader) DiscoverIssueOwner(ctx context.Context, owner string) OwnerDiscovery {
	return r.discoverOwner(ctx, owner, admitsIssueSearch(r.mode), r.IssueSearchPage)
}

// DiscoverIssueOwners discovers each distinct owner (case-insensitively) in order, with the
// throttle behaviour of DiscoverOwners.
func (r *WatchReader) DiscoverIssueOwners(ctx context.Context, owners []string) []OwnerDiscovery {
	return r.discoverOwners(ctx, owners, r.DiscoverIssueOwner)
}
