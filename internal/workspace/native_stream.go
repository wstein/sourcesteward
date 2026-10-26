// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
)

var errNativeStreamLimit = errors.New("native stream exceeds its complete-evidence bound")
var errNativeStreamCommandFailed = errors.New("native stream command failed")

// Each stream starts only an already admitted command constructed by a fixed
// typed Git operation. This executor does not select a binary, argv or grants.
// The caller must create cmd with prepareSandboxed and this cancel's context.
type nativeStreamStats struct{ Stdout, Stderr int64 }

type nativeCountReader struct {
	r            io.Reader
	limit, count int64
}

func (r *nativeCountReader) Read(p []byte) (int, error) {
	remaining := r.limit - r.count
	if remaining < 0 {
		return 0, errNativeStreamLimit
	}
	if int64(len(p)) > remaining+1 {
		p = p[:remaining+1]
	}
	n, e := r.r.Read(p)
	r.count += int64(n)
	if r.count > r.limit {
		return n, errNativeStreamLimit
	}
	return n, e
}

func executeNativeStream(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, input []byte, limit int64, parse func(io.Reader) error) (nativeStreamStats, error) {
	return executeNativeStreamBudgeted(ctx, cancel, cmd, input, limit, parse, nil)
}

func executeNativeStreamBudgeted(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, input []byte, limit int64, parse func(io.Reader) error, budget *tidyV3ProofBudget) (nativeStreamStats, error) {
	stats, _, e := executeNativeStreamExit(ctx, cancel, cmd, bytes.NewReader(input), limit, parse, budget, false)
	return stats, e
}

// executeNativeStreamExit is the shared executor. A nonzero exit is the typed
// errNativeStreamCommandFailed unless tolerateExit is set, in which case the
// exit code is returned for commands whose exit status is part of the result
// (merge-base, merge-tree). stdin is streamed, never buffered.
func executeNativeStreamExit(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, stdin io.Reader, limit int64, parse func(io.Reader) error, budget *tidyV3ProofBudget, tolerateExit bool) (nativeStreamStats, int, error) {
	var stats nativeStreamStats
	defer traceCommand(ctx, cmd)()
	out, e := cmd.StdoutPipe()
	if e != nil {
		return stats, -1, e
	}
	defer out.Close()
	stderrPipe, e := cmd.StderrPipe()
	if e != nil {
		return stats, -1, e
	}
	defer stderrPipe.Close()
	var stderr nativeCapture
	stderrCount := &nativeCountReader{r: stderrPipe, limit: nativeOutputLimit}
	stderrDone := make(chan error, 1)
	cmd.Stdin = stdin
	if e = cmd.Start(); e != nil {
		_, e = nativeExitResult(e, nil)
		return stats, -1, e
	}
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			out.Close()
			stderrPipe.Close()
		case <-finished:
		}
	}()
	go func() {
		var buffer [32 * 1024]byte
		var source io.Reader = stderrCount
		if budget != nil {
			source = budget.reader(ctx, proofStderr, source)
		}
		_, readErr := io.CopyBuffer(&stderr, source, buffer[:])
		if readErr != nil {
			cancel()
		}
		stderrDone <- readErr
	}()
	counted := &nativeCountReader{r: out, limit: limit}
	parseErr := parse(counted)
	// Even a parser which returned successfully must consume the complete stream.
	// Exactly one extra byte suffices to reject a prefix result.
	if parseErr == nil {
		var tail [1]byte
		n, readErr := counted.Read(tail[:])
		if n != 0 {
			parseErr = errors.New("native stream has unexpected trailing output")
		} else if readErr != io.EOF {
			parseErr = readErr
			if parseErr == nil {
				parseErr = io.ErrNoProgress
			}
		}
	}
	if parseErr != nil {
		cancel()
		out.Close()
		stderrPipe.Close()
	}
	stderrErr := <-stderrDone
	waitErr := cmd.Wait()
	stats.Stdout = counted.count
	stats.Stderr = stderrCount.count
	if ctx.Err() != nil {
		return stats, -1, ctx.Err()
	}
	if errors.Is(stderrErr, errTidyV3ProofLimit) {
		return stats, -1, stderrErr
	}
	if errors.Is(stderrErr, errNativeStreamLimit) {
		return stats, -1, errNativeOutputLimit
	}
	code, e := nativeExitResult(waitErr, stderr.data)
	if errors.Is(e, errNativeSandboxUnavailable) {
		return stats, -1, e
	}
	if parseErr != nil {
		return stats, -1, parseErr
	}
	if stderrErr != nil {
		return stats, -1, stderrErr
	}
	if e != nil {
		return stats, -1, e
	}
	if code != 0 && !tolerateExit {
		return stats, code, errNativeStreamCommandFailed
	}
	return stats, code, nil
}
