// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bufio"
	"context"
	"errors"
	"io"
)

// allowlistMatches enumerates only leaf names; output volume is not a repository-size cut.
// Counters become public only after clean EOF and successful process completion.
func (g *gitSource) allowlistMatches(patterns []string) ([]AllowlistMatch, error) {
	ctx, cancel := context.WithTimeout(g.ctx, gitDeadline)
	defer cancel()
	cmd := gitCommand(ctx, g.root, "ls-tree", "-r", "--name-only", "-z", g.commit)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	rows := newAllowlistMatches(patterns)
	err = countAllowlistPaths(pipe, rows)
	if err != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if waitErr != nil {
		return nil, errGitFailed
	}
	return rows, nil
}

func countAllowlistPaths(input io.Reader, rows []AllowlistMatch) error {
	reader := bufio.NewReaderSize(input, maxPathBytes+1)
	for {
		path, err := reader.ReadSlice(0)
		if errors.Is(err, io.EOF) && len(path) == 0 {
			return nil
		}
		if errors.Is(err, bufio.ErrBufferFull) || len(path) > maxPathBytes+1 {
			return errReleaseLimit
		}
		if err != nil {
			return errGitFailed
		}
		if len(path) < 2 {
			return errGitFailed
		}
		countAllowlistPath(rows, string(path[:len(path)-1]))
	}
}
