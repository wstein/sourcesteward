// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"errors"
	"net/http"
)

// IssuesPage reads one page (1..10) of the open issues of repo. Only the issues and status
// readers may read it; fetch refuses it for any other reader before a request is made. A 410 answer (issues
// are disabled for the repository) is CodeIssuesDisabled, not a failure of the read; every other
// status is classified as for the watch reader. The `Link` header is only returned: the caller
// asks for the following page by number, and a URL in it is never requested.
func (r *WatchReader) IssuesPage(ctx context.Context, repo string, page int) (WatchResponse, error) {
	ep, err := IssuesEndpoint(repo, page)
	if err != nil {
		return WatchResponse{}, err
	}
	res, err := r.fetch(ctx, KindIssues, ep, "", true)
	var we *WatchError
	if errors.As(err, &we) && we.Status == http.StatusGone {
		return WatchResponse{}, &WatchError{Code: CodeIssuesDisabled, Status: we.Status, Message: "issues are disabled for this repository"}
	}
	return res, err
}

// Viewer reads `user` once and returns the login the token resolves to. Only the issues and
// status readers may read it; fetch refuses it for any other reader before a request is made. A failed
// read is classified as for the watch reader (a 403 or 404 is CodeForbidden, a 401
// CodeUnauthorized, a throttle CodeRateLimited) and is never retried here.
func (r *WatchReader) Viewer(ctx context.Context) (string, error) {
	res, err := r.fetch(ctx, KindUser, UserEndpoint, "", false)
	if err != nil {
		return "", err
	}
	return ParseViewer(res.Body)
}
