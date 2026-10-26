// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/inspection"
)

// GitCall is one Git process of an observation, for `stw tidy --verbose` and `--trace`: the
// repository of the analysis it belongs to, its arguments without the fixed safety prefix, its
// duration and its exit code (-1 when it did not exit normally).
type GitCall struct {
	Repo     string
	Argv     []string
	Duration time.Duration
	Exit     int
}

type gitTraceKey struct{}

// WithGitTrace carries a sink for every Git process of the observation. The sink may be called
// from several goroutines at the same time. It only observes.
func WithGitTrace(ctx context.Context, sink func(GitCall)) context.Context {
	if sink == nil {
		return ctx
	}
	ctx = context.WithValue(ctx, gitTraceKey{}, sink)
	return inspection.WithRunTrace(ctx, func(c context.Context, _ string, args []string, took time.Duration, exit int) {
		repo := ""
		if sc, ok := c.Value(observeScopeKey{}).(observeScope); ok {
			repo = sc.name
		}
		sink(GitCall{Repo: repo, Argv: traceArgv(args), Duration: took, Exit: exit})
	})
}

const (
	traceArgLimit  = 24
	traceArgLength = 160
)

// traceArgv drops the `-c key=value` pairs and the leading options of the fixed prefix, bounds the
// number and the length of the arguments and removes credentials from URLs.
func traceArgv(args []string) []string {
	out := []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-c" {
			i++
			continue
		}
		if len(out) == 0 && strings.HasPrefix(a, "-") {
			continue
		}
		if len(out) == traceArgLimit {
			out = append(out, "...")
			break
		}
		if i := strings.Index(a, "://"); i >= 0 {
			if at := strings.IndexByte(a[i+3:], '@'); at >= 0 && !strings.ContainsRune(a[i+3:i+3+at], '/') {
				a = a[:i+3] + a[i+3+at+1:]
			}
		}
		if len(a) > traceArgLength {
			a = a[:traceArgLength] + "..."
		}
		out = append(out, a)
	}
	return out
}

// traceCommand starts the clock of a prepared native command; the returned function reports it
// once the command has ended. Without a sink it does nothing.
func traceCommand(ctx context.Context, cmd *exec.Cmd) func() {
	sink, ok := ctx.Value(gitTraceKey{}).(func(GitCall))
	if !ok {
		return func() {}
	}
	args := cmd.Args
	// Under the Linux sandbox the Git command follows the "--" of the sandbox arguments.
	if filepath.Base(cmd.Path) != "git" {
		for i, a := range args {
			if a == "--" {
				args = args[i+1:]
				break
			}
		}
	}
	if len(args) > 0 {
		args = args[1:] // the executable
	}
	repo := ""
	if sc, ok := ctx.Value(observeScopeKey{}).(observeScope); ok {
		repo = sc.name
	}
	argv := traceArgv(args)
	start := time.Now()
	return func() {
		exit := -1
		if cmd.ProcessState != nil {
			exit = cmd.ProcessState.ExitCode()
		}
		sink(GitCall{Repo: repo, Argv: argv, Duration: time.Since(start), Exit: exit})
	}
}

// OpenTraceFile creates the file of `--trace` for writing (mode 0600). An existing file is never
// overwritten.
func OpenTraceFile(path string) (io.WriteCloser, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	return f, nil
}
