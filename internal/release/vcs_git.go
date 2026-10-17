// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// This file holds the fixed, read-only Git invocations of the release commands. They run under
// an isolation prefix (DD18): the user's and the system's Git configuration are ignored, replace
// objects are off, nothing is locked or prompted. Only `cat-file` and `ls-tree` are used, which
// read objects and run no hook, filter, textconv or fsmonitor.

const (
	gitListLimit  = 1 << 20 // bytes of an ls-tree listing
	gitBatchLimit = 8 << 20 // bytes of a cat-file --batch-check reply (a missing path echoes its name)
	gitDeadline   = 30 * time.Second
	maxTreeProbes = 20000
)

// gitEnvironment drops every GIT_ variable and pins the isolating ones.
func gitEnvironment() []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			env = append(env, entry)
		}
	}
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_PAGER=cat", "GIT_ATTR_NOSYSTEM=1")
}

func gitCommand(ctx context.Context, root string, args ...string) *exec.Cmd {
	prefix := []string{"--no-replace-objects", "--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null"}
	cmd := exec.CommandContext(ctx, "git", append(prefix, args...)...)
	cmd.Dir = root
	cmd.Env = gitEnvironment()
	cmd.WaitDelay = time.Second
	return cmd
}

// errGitFailed is a Git failure that is neither a missing object nor cancellation.
var errGitFailed = errors.New("git failed")

// gitOutput runs a read and returns its standard output, capped at limit bytes (truncated is
// reported, the process stopped). The exit status is returned; a start failure is an error.
func gitOutput(ctx context.Context, root string, limit int, stdin string, args ...string) (out []byte, truncated bool, status int, err error) {
	ctx, cancel := context.WithTimeout(ctx, gitDeadline)
	defer cancel()
	cmd := gitCommand(ctx, root, args...)
	cmd.Stdin = strings.NewReader(stdin)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, -1, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, -1, err
	}
	out, readErr := io.ReadAll(io.LimitReader(pipe, int64(limit)+1))
	if len(out) > limit {
		out, truncated = out[:limit], true
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil, false, -1, ctx.Err()
	}
	if readErr != nil {
		return nil, false, -1, readErr
	}
	var exit *exec.ExitError
	switch {
	case truncated:
		return out, true, 0, nil
	case waitErr == nil:
		return out, false, 0, nil
	case errors.As(waitErr, &exit):
		return out, false, exit.ExitCode(), nil
	}
	return nil, false, -1, waitErr
}

// gitSource reads the contract files of one commit.
type gitSource struct {
	ctx    context.Context
	root   string
	commit string
}

// newGitSource checks that the commit id names a commit object of the repository.
func newGitSource(ctx context.Context, root, commit string) (*gitSource, error) {
	_, _, status, err := gitOutput(ctx, root, 64, "", "cat-file", "-e", commit+"^{commit}")
	if err != nil {
		return nil, err
	}
	if status != 0 {
		return nil, errCommitUnresolved
	}
	return &gitSource{ctx: ctx, root: root, commit: commit}, nil
}

var errCommitUnresolved = errors.New("commit-unresolved")

// Entries lists the .stw directory of the commit; a missing or non-directory .stw is no entries.
func (g *gitSource) Entries() ([]sourceEntry, bool, error) {
	kind, _, status, err := gitOutput(g.ctx, g.root, 64, "", "cat-file", "-t", g.commit+":.stw")
	if err != nil {
		return nil, false, err
	}
	if status != 0 || strings.TrimSpace(string(kind)) != "tree" {
		return nil, false, nil
	}
	listing, truncated, status, err := gitOutput(g.ctx, g.root, gitListLimit, "", "ls-tree", "-z", g.commit+":.stw")
	if err != nil {
		return nil, false, err
	}
	if status != 0 {
		return nil, false, fmt.Errorf("%w: ls-tree", errGitFailed)
	}
	var entries []sourceEntry
	for _, record := range bytes.Split(listing, []byte{0}) {
		meta, name, ok := bytes.Cut(record, []byte{'\t'})
		if !ok {
			continue
		}
		fields := strings.Fields(string(meta))
		if len(fields) != 3 {
			continue
		}
		entry := sourceEntry{Name: string(name), id: fields[2], Kind: "other"}
		switch {
		case fields[1] == "tree":
			entry.Kind = "dir"
		case fields[0] == "100644" || fields[0] == "100755":
			entry.Kind = "file"
		case fields[0] == "120000":
			entry.Kind = "symlink"
		}
		entries = append(entries, entry)
	}
	return entries, truncated, nil
}

// Read returns a blob, at most limit bytes; tooLarge reports a longer blob.
func (g *gitSource) Read(e sourceEntry, limit int) ([]byte, bool, error) {
	data, truncated, status, err := gitOutput(g.ctx, g.root, limit, "", "cat-file", "blob", e.id)
	if err != nil {
		return nil, false, err
	}
	if status != 0 {
		return nil, false, fmt.Errorf("%w: cat-file", errGitFailed)
	}
	return data, truncated, nil
}

// Kinds reports which of the paths exist in the commit's tree, with one `cat-file --batch-check`.
func (g *gitSource) Kinds(paths []string) (map[string]string, error) {
	if len(paths) > maxTreeProbes {
		return nil, fmt.Errorf("%w: more than %d paths to probe", errGitFailed, maxTreeProbes)
	}
	var stdin strings.Builder
	for _, p := range paths {
		if strings.ContainsAny(p, "\n\x00") {
			return nil, fmt.Errorf("%w: unprobeable path", errGitFailed)
		}
		stdin.WriteString(g.commit + ":" + p + "\n")
	}
	reply, truncated, status, err := gitOutput(g.ctx, g.root, gitBatchLimit, stdin.String(), "cat-file", "--batch-check")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(string(reply), "\n"), "\n")
	if truncated || status != 0 || len(lines) != len(paths) {
		return nil, fmt.Errorf("%w: cat-file --batch-check", errGitFailed)
	}
	out := map[string]string{}
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[1] == "blob" {
			out[paths[i]] = "file"
		} else if len(fields) == 3 && (fields[1] == "tree" || fields[1] == "commit") {
			out[paths[i]] = "dir"
		}
	}
	return out, nil
}
