// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

const (
	// pinsRetainBudget bounds the bytes an audit keeps from all Git output together (Spec 021, FR-011).
	pinsRetainBudget = 1 << 20
	// pinsScanAllowance bounds the bytes one invocation may produce on stdout and stderr together,
	// including records that are read and dropped. It is consumed incrementally: exceeding it stops the
	// process and is reported, never silently truncated.
	pinsScanAllowance = 64 << 20
	// pinsRecordLimit bounds one NUL-terminated record (a path is at most 4096 bytes in practice).
	pinsRecordLimit = 64 << 10
	pinsStderrKeep  = 4096
)

// pinsLimitError reports an exhausted bound; callers turn it into the pins-limit finding.
type pinsLimitError struct{ Kind string } // "retained" or "scan"

func (e *pinsLimitError) Error() string {
	if e.Kind == "scan" {
		return "Git output exceeded the scan allowance"
	}
	return "Git output exceeded the retained-output budget"
}

// pinsBudget is the retained-bytes budget shared by every invocation of one audit.
type pinsBudget struct{ retained, limit int }

func (b *pinsBudget) take(n int) error {
	if b.retained+n > b.limit {
		return &pinsLimitError{Kind: "retained"}
	}
	b.retained += n
	return nil
}

// pinsGitRepo names a repository explicitly. Git never discovers a repository for these invocations, so
// repository content cannot select another one and Git's own ownership check does not apply: the caller
// validates ownership first (pins_safe.go).
type pinsGitRepo struct{ GitDir, WorkTree string }

// pinsCall is one Git invocation of the normative process allowlist (specs/021-submodule-pin-audit/research.md).
type pinsCall struct {
	Repo  pinsGitRepo
	Dir   string   // working directory for a call without a repository (bootstrap discovery of the superproject)
	Args  []string // arguments after the isolation prefix
	Stdin []byte
	// Keep selects which NUL-terminated stdout records are retained; nil retains everything. A dropped
	// record still counts toward the scan allowance.
	Keep func(record []byte) bool
}

type pinsOutput struct {
	Stdout []byte
	Stderr string // first bytes only, for diagnostics
	Exit   int
}

type pinsRunner struct {
	ctx    context.Context
	budget *pinsBudget
	scan   int
}

func newPinsRunner(ctx context.Context) *pinsRunner {
	return &pinsRunner{ctx: ctx, budget: &pinsBudget{limit: pinsRetainBudget}, scan: pinsScanAllowance}
}

// pinsIsolation neutralizes hooks, fsmonitor, optional locks, prompts, credential helpers, remote
// transports, replace objects and inherited repository selection (the GIT_* variables are dropped by
// gitEnvironment). Clean and process filters, external diff and textconv are not reachable through the
// allowlisted commands, which never read or compare worktree content (diff-index runs with --no-ext-diff
// --no-textconv); pins_hostile_test.go proves this with planted filters on stat-dirty files.
func pinsIsolation() []string {
	return []string{
		"--no-replace-objects", "--no-optional-locks", "--literal-pathspecs",
		"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "core.untrackedCache=false",
		"-c", "credential.helper=", "-c", "protocol.allow=never", "-c", "core.sshCommand=/bin/false",
		"-c", "gc.auto=0", "-c", "maintenance.auto=false",
	}
}

func pinsEnvironment() []string {
	return append(gitEnvironment(), "LC_ALL=C", "GIT_PROTOCOL_FROM_USER=0")
}

// scanCounter counts stderr toward the same scan allowance as stdout and keeps its first bytes.
type scanCounter struct {
	mu     sync.Mutex
	total  *int
	limit  int
	keep   bytes.Buffer
	cancel context.CancelFunc
	over   bool
}

func (s *scanCounter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	*s.total += len(p)
	if room := pinsStderrKeep - s.keep.Len(); room > 0 {
		s.keep.Write(p[:min(room, len(p))])
	}
	if *s.total > s.limit {
		s.over = true
		s.cancel()
	}
	return len(p), nil
}

func (r *pinsRunner) run(call pinsCall) (pinsOutput, error) {
	if err := r.ctx.Err(); err != nil {
		return pinsOutput{}, err
	}
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	args := pinsIsolation()
	if call.Repo.GitDir != "" {
		args = append(args, "--git-dir="+call.Repo.GitDir)
		if call.Repo.WorkTree != "" {
			args = append(args, "--work-tree="+call.Repo.WorkTree)
		}
	}
	c := exec.CommandContext(ctx, "git", append(args, call.Args...)...)
	c.Dir = call.Dir
	if c.Dir == "" {
		c.Dir = call.Repo.WorkTree
	}
	c.Env = pinsEnvironment()
	c.Stdin = bytes.NewReader(call.Stdin)
	c.WaitDelay = time.Second
	scanned := 0
	errs := &scanCounter{total: &scanned, limit: r.scan, cancel: cancel}
	c.Stderr = errs
	pipe, err := c.StdoutPipe()
	if err != nil {
		return pinsOutput{}, err
	}
	if err := c.Start(); err != nil {
		return pinsOutput{}, err
	}
	kept, readErr := r.consume(pipe, call.Keep, &scanned, errs, cancel)
	waitErr := c.Wait()
	if err := r.ctx.Err(); err != nil {
		return pinsOutput{}, err
	}
	errs.mu.Lock()
	over := errs.over
	stderr := errs.keep.String()
	errs.mu.Unlock()
	if over {
		return pinsOutput{}, &pinsLimitError{Kind: "scan"}
	}
	if readErr != nil {
		return pinsOutput{}, readErr
	}
	out := pinsOutput{Stdout: kept, Stderr: stderr}
	if waitErr == nil {
		return out, nil
	}
	var exit *exec.ExitError
	if errors.As(waitErr, &exit) {
		out.Exit = exit.ExitCode()
		return out, nil
	}
	return pinsOutput{}, fmt.Errorf("git did not run: %w", waitErr)
}

// consume reads stdout incrementally. It stops the process as soon as the scan allowance or the shared
// retained budget is exhausted.
func (r *pinsRunner) consume(pipe io.Reader, keep func([]byte) bool, scanned *int, errs *scanCounter, cancel context.CancelFunc) ([]byte, error) {
	var kept, pending []byte
	buf := make([]byte, 32<<10)
	retain := func(b []byte) error {
		if err := r.budget.take(len(b)); err != nil {
			return err
		}
		kept = append(kept, b...)
		return nil
	}
	fail := func(err error) ([]byte, error) {
		cancel()
		_, _ = io.Copy(io.Discard, pipe)
		return nil, err
	}
	for {
		n, err := pipe.Read(buf)
		if n > 0 {
			errs.mu.Lock()
			*scanned += n
			over := *scanned > errs.limit
			errs.mu.Unlock()
			if over {
				return fail(&pinsLimitError{Kind: "scan"})
			}
			if keep == nil {
				if e := retain(buf[:n]); e != nil {
					return fail(e)
				}
			} else {
				pending = append(pending, buf[:n]...)
				for {
					i := bytes.IndexByte(pending, 0)
					if i < 0 {
						break
					}
					if keep(pending[:i]) {
						if e := retain(pending[:i+1]); e != nil {
							return fail(e)
						}
					}
					pending = pending[i+1:]
				}
				if len(pending) > pinsRecordLimit {
					return fail(&pinsLimitError{Kind: "retained"})
				}
			}
		}
		if err != nil {
			if len(pending) > 0 && keep != nil && keep(pending) {
				if e := retain(pending); e != nil {
					return fail(e)
				}
			}
			if errors.Is(err, io.EOF) {
				return kept, nil
			}
			return fail(err)
		}
	}
}
