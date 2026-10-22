// SPDX-License-Identifier: EUPL-1.2
package issues

import (
	"context"
	"errors"

	"github.com/wstein/sourcesteward/internal/hosting"
)

// readViewer reads `user` once, with no retry, and records the outcome. Without a repository
// there is nothing to compare an assignee with, so no request is made. A throttle (403 or 429
// with rate-limit evidence) is an error that stops the run; a 403 or 404 without that
// evidence is the user-lookup exception, a skip, because the token class withholds the
// identity; any other failure is an error. In every case but ok the login stays unknown and
// only the assigned rule is off.
func (c *cycle) readViewer(ctx context.Context, anyRepo bool) {
	if !anyRepo {
		c.res.Viewer = Viewer{State: ViewerSkipped, Reason: ReasonNotGitHub}
		return
	}
	login, err := c.r.Viewer(ctx)
	if err == nil {
		c.viewer = login
		c.res.Viewer = Viewer{State: ViewerOK, Login: login}
		return
	}
	code, msg := hosting.CodeNetwork, "read failed"
	var we *hosting.WatchError
	if errors.As(err, &we) {
		code, msg = we.Code, we.Message
	}
	if code == hosting.CodeForbidden {
		c.res.Viewer = Viewer{State: ViewerSkipped, Reason: ReasonNoIdentity}
		return
	}
	c.res.Viewer = Viewer{State: ViewerError, Error: code}
	c.res.Errors = append(c.res.Errors, Error{Code: code, Message: msg})
	if code == hosting.CodeRateLimited {
		c.limited.Store(true)
	}
}
