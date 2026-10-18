// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"errors"
)

func gitDiff(ctx context.Context, root, from, to string, patterns []string, recordPath string) (DiffResult, error) {
	ctx, cancel := context.WithTimeout(ctx, gitDeadline)
	defer cancel()
	cmd := gitCommand(ctx, root, "diff-tree", "-r", "--raw", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", from, to, "--")
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		return DiffResult{}, e
	}
	if e = cmd.Start(); e != nil {
		return DiffResult{}, e
	}
	r, readErr := parseRawDiff(pipe, patterns, recordPath)
	if readErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return r, ctx.Err()
	}
	if readErr != nil {
		return r, readErr
	}
	if waitErr != nil {
		return r, errors.Join(errGitFailed, waitErr)
	}
	return r, nil
}
