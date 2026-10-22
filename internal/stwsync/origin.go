// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
)

// BehindOrigin reports whether origin has commits the local default branch lacks, from the head SHA
// that origin reported (Spec 037, `main-behind-origin`). It reads the local object store only and
// never fetches: behind is true when the commit is absent locally or is not an ancestor of the tip of
// the local branch. known is false when it cannot be decided: remoteHead is not a full lower-case SHA,
// branch is not an existing local branch of the repository at dir, or Git is unavailable.
func BehindOrigin(ctx context.Context, dir, branch, remoteHead string) (behind, known bool) {
	if !fullSHA(remoteHead) {
		return false, false
	}
	bin, err := exec.LookPath("git")
	if err != nil {
		return false, false
	}
	if bin, err = filepath.Abs(bin); err != nil {
		return false, false
	}
	e := env{bin, dir}
	// `show-ref --verify` takes an exact ref name, so a revision expression in branch finds nothing.
	out, err := e.g(ctx, "show-ref", "--verify", "--hash", "--", "refs/heads/"+branch)
	tip := strings.TrimSpace(out.Out)
	if err != nil || out.Code != 0 || !fullSHA(tip) {
		return false, false
	}
	// `^{commit}` makes rev-parse look the object up: a full SHA alone is echoed even when absent.
	if e.revParse(ctx, dir, remoteHead+"^{commit}") == "" {
		return true, true
	}
	out, err = e.g(ctx, "merge-base", "--is-ancestor", remoteHead, tip)
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
