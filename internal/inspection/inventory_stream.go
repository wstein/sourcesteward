// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// inventoryBounds holds the Git inventory limits of Spec 027 (contracts/inventory-limits.md). The
// session carries one value so limit rows can be tested with small numbers.
type inventoryBounds struct {
	Scan               int // LIM-3: stdout plus stderr of one streamed invocation
	Record             int // LIM-4: one delimited record
	IndexEntries       int // LIM-7: entries read from one index
	IndexPathBytes     int // LIM-7: path bytes read from one index
	Candidates         int // LIM-8: retained file candidates per pass
	CandidatePathBytes int // LIM-8: retained candidate path bytes per pass
	References         int // LIM-9: scanned references
	FileBytes          int // LIM-10: content hashed per file
	ContentBudget      int // LIM-11: content bytes per run
}

func defaultInventoryBounds() inventoryBounds {
	return inventoryBounds{
		Scan: 64 << 20, Record: outputLimit, IndexEntries: 100000, IndexPathBytes: 8 << 20,
		Candidates: 20000, CandidatePathBytes: 8 << 20, References: 20000,
		FileBytes: 16 << 20, ContentBudget: 256 << 20,
	}
}

// streamLimitError reports an exhausted streaming bound; Kind is "scan" or "record".
type streamLimitError struct{ Kind string }

func (e *streamLimitError) Error() string {
	if e.Kind == "record" {
		return "a Git output record exceeded its length limit"
	}
	return "Git output exceeded the scan allowance"
}

var (
	// errStreamStop is returned by a record callback to end the command early without an error.
	errStreamStop = errors.New("stream stopped by the caller")
	// errStreamUnterminated reports output that ends inside a record.
	errStreamUnterminated = errors.New("invalid NUL-delimited native output")
)

// processError reports that the Git process could not be started or waited for. Its message names only the
// operating system condition: the underlying error carries the executable and repository paths, which must
// not reach a result.
type processError struct{ msg string }

func (e *processError) Error() string { return e.msg }

func newProcessError(err error) error {
	var errno syscall.Errno
	switch {
	case errors.As(err, &errno):
		return &processError{"git process failed: " + errno.Error()}
	case errors.Is(err, exec.ErrNotFound):
		return &processError{"git process failed: executable file not found"}
	}
	return &processError{"git process failed"}
}

// gitStream runs one Git command (arguments after the session prefix) and calls each for every
// delimited stdout record. record is valid only during the call. stopped is true when each returned
// errStreamStop.
type gitStream func(ctx context.Context, dir string, stdin io.Reader, delim string, args []string, each func(record []byte) error) (exit int, stopped bool, err error)

type streamCounter struct {
	mu     sync.Mutex
	total  int
	limit  int
	keep   bytes.Buffer
	over   bool
	cancel context.CancelFunc
}

func (s *streamCounter) add(n int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total += n
	if s.total > s.limit {
		s.over = true
		s.cancel()
	}
	return s.over
}

func (s *streamCounter) Write(p []byte) (int, error) {
	s.mu.Lock()
	if room := 4096 - s.keep.Len(); room > 0 {
		s.keep.Write(p[:min(room, len(p))])
	}
	s.mu.Unlock()
	s.add(len(p))
	return len(p), nil
}

// newStreamGit returns the production streaming runner. It builds the command exactly like runGit
// (same prefix, environment and WaitDelay) and ends the process by cancelling its context.
func newStreamGit(b inventoryBounds) gitStream {
	return func(ctx context.Context, dir string, stdin io.Reader, delim string, args []string, each func([]byte) error) (int, bool, error) {
		if err := ctx.Err(); err != nil {
			return -1, false, err
		}
		cctx, cancel := context.WithCancel(ctx)
		defer cancel()
		c := exec.CommandContext(cctx, "git", append([]string{"--no-replace-objects", "-c", "core.fsmonitor=false"}, args...)...)
		c.Dir = dir
		c.Env = gitEnvironment()
		if stdin == nil {
			stdin = strings.NewReader("")
		}
		c.Stdin = stdin
		c.WaitDelay = time.Second
		prepareStreamCommand(c)
		counter := &streamCounter{limit: b.Scan, cancel: cancel}
		c.Stderr = counter
		pipe, err := c.StdoutPipe()
		if err != nil {
			return -1, false, newProcessError(err)
		}
		if err := c.Start(); err != nil {
			return -1, false, newProcessError(err)
		}
		stopped, readErr := consumeRecords(pipe, []byte(delim), b.Record, counter, cancel, each)
		waitErr := c.Wait()
		if ctx.Err() != nil {
			return -1, false, ctx.Err()
		}
		counter.mu.Lock()
		over := counter.over
		counter.mu.Unlock()
		if readErr != nil {
			return -1, false, readErr
		}
		if over {
			return -1, false, &streamLimitError{Kind: "scan"}
		}
		if stopped {
			return 0, true, nil
		}
		if waitErr == nil {
			return 0, false, nil
		}
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) {
			return exit.ExitCode(), false, nil
		}
		return -1, false, newProcessError(waitErr)
	}
}

// consumeRecords reads stdout in chunks, splits it on delim and calls each. It cancels the process
// and drains the pipe on overflow, stop or error.
func consumeRecords(pipe io.Reader, delim []byte, recordLimit int, counter *streamCounter, cancel context.CancelFunc, each func([]byte) error) (bool, error) {
	var pending []byte
	buf := make([]byte, 32<<10)
	finish := func(stopped bool, err error) (bool, error) {
		cancel()
		_, _ = io.Copy(io.Discard, pipe)
		return stopped, err
	}
	for {
		n, err := pipe.Read(buf)
		if n > 0 {
			if counter.add(n) {
				return finish(false, nil)
			}
			pending = append(pending, buf[:n]...)
			start := 0
			for {
				i := bytes.Index(pending[start:], delim)
				if i < 0 {
					break
				}
				if i > recordLimit {
					return finish(false, &streamLimitError{Kind: "record"})
				}
				if e := each(pending[start : start+i]); e != nil {
					if errors.Is(e, errStreamStop) {
						return finish(true, nil)
					}
					return finish(false, e)
				}
				start += i + len(delim)
			}
			pending = append(pending[:0], pending[start:]...)
			if len(pending) > recordLimit+len(delim)-1 {
				return finish(false, &streamLimitError{Kind: "record"})
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(pending) > 0 {
					return finish(false, errStreamUnterminated)
				}
				return false, nil
			}
			return finish(false, err)
		}
	}
}

// splitRecords applies the record rules of consumeRecords to complete output.
func splitRecords(out, delim string, recordLimit int, each func([]byte) error) (bool, error) {
	data := []byte(out)
	d := []byte(delim)
	for len(data) > 0 {
		i := bytes.Index(data, d)
		if i < 0 {
			return false, errStreamUnterminated
		}
		if i > recordLimit {
			return false, &streamLimitError{Kind: "record"}
		}
		if e := each(data[:i]); e != nil {
			if errors.Is(e, errStreamStop) {
				return true, nil
			}
			return false, e
		}
		data = data[i+len(d):]
	}
	return false, nil
}

// streamFromRunner adapts an injected buffered runner (used by interposition tests) to a gitStream.
// A command that needs standard input falls back to the real stream, which an interposing runner
// cannot carry.
func streamFromRunner(run runner, real gitStream) gitStream {
	return func(ctx context.Context, dir string, stdin io.Reader, delim string, args []string, each func([]byte) error) (int, bool, error) {
		if stdin != nil {
			return real(ctx, dir, stdin, delim, args, each)
		}
		out, code, err := run(ctx, dir, args...)
		if err != nil {
			return code, false, err
		}
		if code != 0 {
			return code, false, nil
		}
		stopped, e := splitRecords(out, delim, outputLimit, each)
		return 0, stopped, e
	}
}
