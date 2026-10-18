// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"errors"
	"strings"
)

var errTagUnresolved = errors.New("tag-unresolved")

func resolveGitTag(ctx context.Context, root, tag string) (string, bool, error) {
	ref := "refs/tags/" + tag
	// Resolve the mutable name once. Type and peel must observe this immutable
	// object even if another actor changes the tag reference between reads.
	out, cut, status, e := gitOutput(ctx, root, 128, "", "rev-parse", "--verify", "--quiet", ref)
	if e != nil {
		return "", false, e
	}
	if cut {
		return "", false, errReleaseLimit
	}
	object := strings.TrimSpace(string(out))
	if status != 0 || !IsFullCommitID(object) {
		return "", false, errTagUnresolved
	}
	out, cut, status, e = gitOutput(ctx, root, 128, "", "cat-file", "-t", object)
	if e != nil {
		return "", false, e
	}
	if cut {
		return "", false, errReleaseLimit
	}
	if status != 0 {
		return "", false, errTagUnresolved
	}
	annotated := strings.TrimSpace(string(out)) == "tag"
	out, cut, status, e = gitOutput(ctx, root, 128, "", "rev-parse", "--verify", "--quiet", object+"^{commit}")
	if e != nil {
		return "", false, e
	}
	if cut {
		return "", false, errReleaseLimit
	}
	id := strings.TrimSpace(string(out))
	if status != 0 || !IsFullCommitID(id) {
		return "", false, errTagUnresolved
	}
	return id, annotated, nil
}
func gitAncestor(ctx context.Context, root, a, b string) (bool, error) {
	_, cut, status, e := gitOutput(ctx, root, 64, "", "merge-base", "--is-ancestor", a, b)
	if e != nil {
		return false, e
	}
	if cut {
		return false, errReleaseLimit
	}
	if status > 1 {
		return false, errGitFailed
	}
	return status == 0, nil
}
