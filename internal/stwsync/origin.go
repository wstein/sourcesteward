// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
)

// BehindOrigin reports whether origin has commits the local default branch lacks, from the head SHA
// that origin reported (Spec 037, `main-behind-origin`). It looks in the local object store only: every
// Git call carries --no-lazy-fetch, so even a partial clone never fetches the missing commit. behind is
// true when the commit is absent locally or is not an ancestor of the tip of the local branch. known is
// false when it cannot be decided: remoteHead is not a full lower-case SHA, branch is not an existing
// local branch of the repository at dir, Git is unavailable, or Git is older than 2.44 and refuses
// --no-lazy-fetch.
func BehindOrigin(ctx context.Context, dir, branch, remoteHead string) (behind, known bool) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return false, false
	}
	if bin, err = filepath.Abs(bin); err != nil {
		return false, false
	}
	return behindOrigin(ctx, func(ctx context.Context, args ...string) (gitOutput, error) {
		return git(ctx, bin, dir, args...)
	}, branch, remoteHead)
}

// behindOrigin is BehindOrigin over a Git runner; run receives the arguments after the global options.
func behindOrigin(ctx context.Context, run func(context.Context, ...string) (gitOutput, error), branch, remoteHead string) (behind, known bool) {
	if !fullSHA(remoteHead) {
		return false, false
	}
	g := func(args ...string) (gitOutput, error) {
		return run(ctx, append([]string{"--no-lazy-fetch"}, args...)...)
	}
	// `show-ref --verify` takes an exact ref name, so a revision expression in branch finds nothing.
	out, err := g("show-ref", "--verify", "--hash", "--", "refs/heads/"+branch)
	tip := strings.TrimSpace(out.Out)
	if err != nil || out.Code != 0 || !fullSHA(tip) {
		return false, false
	}
	// `^{commit}` makes rev-parse look the object up: a full SHA alone is echoed even when absent.
	out, err = g("rev-parse", "--verify", "--quiet", remoteHead+"^{commit}")
	if err != nil || out.Code > 1 {
		return false, false
	}
	if out.Code == 1 {
		return true, true
	}
	out, err = g("merge-base", "--is-ancestor", remoteHead, tip)
	if err != nil || out.Code > 1 {
		return false, false
	}
	return out.Code == 1, true
}

// fullSHA reports whether s is 40 lower-case hexadecimal digits.
func fullSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
