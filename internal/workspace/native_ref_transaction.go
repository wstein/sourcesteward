// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"time"
)

var errGitRefTransaction = errors.New("prepared Git reference transaction refused")

// errGitRefDescendants is joined into the terminal result when the owned
// process is gone but something still holds its stderr: a sandbox descendant
// (bwrap's git) may still run.
var errGitRefDescendants = errors.New("sandbox descendants of the stopped Git process may still run")

// gitRefDescendantWait bounds how long a finished transaction waits for its
// descendants to release stderr.
var gitRefDescendantWait = 5 * time.Second

type gitRefTransactionOutcome struct{ Prepared, CommitAttempted, Committed, Uncertain bool }
type gitRefStderr struct {
	data []byte
	err  error
}
type gitRefTransaction struct {
	ctx              context.Context
	cancel           context.CancelFunc
	cmd              *exec.Cmd
	input            io.WriteCloser
	stdout, stderr   io.ReadCloser
	output           *nativeCountReader
	stderrDone       chan gitRefStderr
	done             chan struct{}
	bin, ref, oldOID string
	id               NativeIdentity
	out              gitRefTransactionOutcome
	stopped          bool
	terminalErr      error
}

func prepareGitRefDeletion(ctx context.Context, bin string, id NativeIdentity, ref, oldOID string, writeRoots, readOnlyRoots []string) (*gitRefTransaction, error) {
	if id.Backend != "git" || (!strings.HasPrefix(ref, "refs/heads/") && !strings.HasPrefix(ref, "refs/remotes/")) || ref == id.HeadRef || !fullGitOID(oldOID, id.ObjectFormat) || oldOID == strings.Repeat("0", len(oldOID)) {
		return nil, errGitRefTransaction
	}
	if _, e := gitRead(ctx, bin, id.WorkspaceRoot, "check-ref-format", ref); e != nil {
		return nil, errGitRefTransaction
	}
	current, e := gitNativeIdentity(ctx, id.WorkspaceRoot)
	if e != nil {
		return nil, e
	}
	if !reflect.DeepEqual(current, id) {
		return nil, errGitRefTransaction
	}
	if _, e = gitSafety(ctx, bin, id, id.CommitID); e != nil {
		return nil, e
	}
	protection, e := gitTidyWriteProtection(id, []string{ref})
	if e != nil {
		return nil, e
	}
	if !reflect.DeepEqual(writeRoots, []string{id.RepositoryDir}) || !reflect.DeepEqual(readOnlyRoots, protection) {
		return nil, errGitRefTransaction
	}
	runCtx, cancel := context.WithCancel(ctx)
	cmd, e := prepareSandboxed(runCtx, bin, []string{"--no-replace-objects", "update-ref", "--stdin", "-z"}, id.WorkspaceRoot, writeRoots, readOnlyRoots)
	if e != nil {
		cancel()
		return nil, e
	}
	t, e := startGitRefTransaction(ctx, runCtx, cancel, cmd, bin, id, ref, oldOID)
	if e != nil {
		return nil, e
	}
	if e = t.write("start\x00"); e == nil {
		e = t.ack("start: ok\n")
	}
	if e == nil {
		e = t.write(gitRefDeleteCommand(ref, oldOID))
	}
	if e == nil {
		e = t.ack("prepare: ok\n")
	}
	if e != nil {
		t.out.Uncertain = true
		return t, errors.Join(e, t.finish(true))
	}
	t.out.Prepared = true
	if e = t.readback(ctx); e != nil {
		return t, t.abortFor(e)
	}
	return t, nil
}

// startGitRefTransaction wires the pipes of an already prepared sandboxed
// command, starts it and arms the cancel watcher and the stderr capture. It
// cancels runCtx itself when it fails.
func startGitRefTransaction(ctx, runCtx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, bin string, id NativeIdentity, ref, oldOID string) (*gitRefTransaction, error) {
	input, e := cmd.StdinPipe()
	if e != nil {
		cancel()
		return nil, e
	}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		input.Close()
		cancel()
		return nil, e
	}
	stderr, e := cmd.StderrPipe()
	if e != nil {
		input.Close()
		stdout.Close()
		cancel()
		return nil, e
	}
	t := &gitRefTransaction{ctx: ctx, cancel: cancel, cmd: cmd, input: input, stdout: stdout, stderr: stderr, output: &nativeCountReader{r: stdout, limit: nativeOutputLimit}, stderrDone: make(chan gitRefStderr, 1), done: make(chan struct{}), bin: bin, id: id, ref: ref, oldOID: oldOID}
	if e = cmd.Start(); e != nil {
		input.Close()
		stdout.Close()
		stderr.Close()
		cancel()
		_, e = nativeExitResult(e, nil)
		return nil, e
	}
	go func() {
		select {
		case <-runCtx.Done():
			input.Close()
			stdout.Close()
			stderr.Close()
		case <-t.done:
		}
	}()
	go func() {
		var capture nativeCapture
		var buffer [32 * 1024]byte
		_, e := io.CopyBuffer(&capture, &nativeCountReader{r: stderr, limit: nativeOutputLimit}, buffer[:])
		if e != nil {
			cancel()
		}
		t.stderrDone <- gitRefStderr{capture.data, e}
	}()
	return t, nil
}

// gitRefDeleteCommand is the exact prepared command: the old object name is
// Git's own compare-and-swap, independent of the later locked readback.
func gitRefDeleteCommand(ref, oldOID string) string {
	return "option no-deref\x00delete " + ref + "\x00" + oldOID + "\x00prepare\x00"
}
func (t *gitRefTransaction) write(command string) error {
	if t.stopped {
		return errGitRefTransaction
	}
	if e := t.ctx.Err(); e != nil {
		return e
	}
	n, e := io.WriteString(t.input, command)
	if e == nil && n != len(command) {
		e = io.ErrShortWrite
	}
	return e
}
func (t *gitRefTransaction) ack(expected string) error {
	var line [128]byte
	for i := range line {
		n, e := t.output.Read(line[i : i+1])
		if e != nil {
			return e
		}
		if n != 1 {
			return io.ErrNoProgress
		}
		if line[i] == '\n' {
			if string(line[:i+1]) != expected {
				return errGitRefTransaction
			}
			return nil
		}
	}
	return errGitRefTransaction
}
func (t *gitRefTransaction) readback(ctx context.Context) error {
	raw, e := gitRead(ctx, t.bin, t.id.WorkspaceRoot, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)%00", t.ref)
	if e != nil {
		return e
	}
	fields := strings.Split(strings.TrimSuffix(raw, "\n"), "\x00")
	if len(fields) != 4 || fields[0] != t.ref || fields[1] != t.oldOID || fields[2] != "" || fields[3] != "" {
		return errGitRefTransaction
	}
	return nil
}
func (t *gitRefTransaction) finish(kill bool) error {
	if t.stopped {
		return t.terminalErr
	}
	t.input.Close()
	var tailErr error
	if kill {
		t.cancel()
	} else {
		var tail [1]byte
		n, e := t.output.Read(tail[:])
		if n != 0 {
			tailErr = errGitRefTransaction
		} else if e != io.EOF {
			tailErr = e
			if tailErr == nil {
				tailErr = io.ErrNoProgress
			}
		}
		if tailErr != nil {
			t.cancel()
		}
	}
	stderr := <-t.stderrDone
	waitErr := t.cmd.Wait()
	t.stdout.Close()
	t.stderr.Close()
	t.cancel()
	close(t.done)
	t.stopped = true
	code, exitErr := nativeExitResult(waitErr, stderr.data)
	e := errors.Join(tailErr, exitErr)
	if !kill && code != 0 {
		e = errors.Join(e, errGitRefTransaction)
	}
	if errors.Is(stderr.err, errNativeStreamLimit) {
		e = errors.Join(e, errNativeOutputLimit)
	} else if !kill && stderr.err != nil {
		e = errors.Join(e, stderr.err)
	}
	if t.ctx.Err() != nil {
		e = errors.Join(e, t.ctx.Err())
	}
	t.terminalErr = e
	return e
}
func (t *gitRefTransaction) abortFor(cause error) error { return errors.Join(cause, t.Abort()) }
func (t *gitRefTransaction) outcome() gitRefTransactionOutcome {
	if t == nil {
		return gitRefTransactionOutcome{}
	}
	return t.out
}

// refuse ends a prepared transaction without COMMIT. When the operation was
// cancelled, the cancel watcher and a clean abort acknowledgment race, so a
// cancelled refusal never trusts an abort: it kills the owned process now and
// reports the outcome as uncertain, whichever side would have won.
func (t *gitRefTransaction) refuse(ctx context.Context, cause error) (gitRefTransactionOutcome, error) {
	if ctx.Err() != nil && !t.stopped {
		t.out.Uncertain = true
		return t.out, errors.Join(cause, t.finish(true))
	}
	e := t.abortFor(cause)
	return t.out, e
}
func (t *gitRefTransaction) Abort() error {
	if t == nil {
		return nil
	}
	if t.stopped {
		if t.out.CommitAttempted || t.out.Uncertain {
			return errors.Join(errGitRefTransaction, t.terminalErr)
		}
		return t.terminalErr
	}
	e := t.write("abort\x00")
	if e == nil {
		e = t.ack("abort: ok\n")
	}
	if e != nil {
		t.out.Uncertain = true
		return errors.Join(e, t.finish(true))
	}
	e = t.finish(false)
	if e != nil {
		t.out.Uncertain = true
	}
	return e
}
func (t *gitRefTransaction) Commit(ctx context.Context, revalidate func(context.Context) error) (gitRefTransactionOutcome, error) {
	if t == nil {
		return gitRefTransactionOutcome{}, errGitRefTransaction
	}
	callerCtx := ctx
	operationCtx, stopOperation := context.WithCancelCause(t.ctx)
	stopCaller := context.AfterFunc(callerCtx, func() { stopOperation(context.Cause(callerCtx)) })
	defer stopCaller()
	defer stopOperation(context.Canceled)
	if ctx.Err() != nil {
		stopOperation(context.Cause(ctx))
	}
	ctx = operationCtx
	if t.stopped || !t.out.Prepared {
		return t.out, errors.Join(errGitRefTransaction, t.terminalErr)
	}
	if e := ctx.Err(); e != nil {
		return t.refuse(ctx, errors.Join(e, context.Cause(ctx)))
	}
	commitDone := make(chan struct{})
	defer close(commitDone)
	go func() {
		select {
		case <-ctx.Done():
			t.cancel()
		case <-commitDone:
		}
	}()
	if revalidate == nil {
		return t.refuse(ctx, errGitRefTransaction)
	}
	if e := t.readback(ctx); e != nil {
		return t.refuse(ctx, errors.Join(e, context.Cause(ctx)))
	}
	if e := revalidate(ctx); e != nil {
		return t.refuse(ctx, errors.Join(e, context.Cause(ctx)))
	}
	if e := ctx.Err(); e != nil {
		return t.refuse(ctx, errors.Join(e, context.Cause(ctx)))
	}
	t.out.CommitAttempted = true
	e := t.write("commit\x00")
	if e == nil {
		e = t.ack("commit: ok\n")
	}
	if e != nil {
		t.out.Uncertain = true
		finishErr := t.finish(true)
		return t.out, errors.Join(e, finishErr, ctx.Err(), context.Cause(ctx))
	}
	if e = t.finish(false); e != nil {
		t.out.Uncertain = true
		return t.out, errors.Join(e, ctx.Err(), context.Cause(ctx))
	}
	t.out.Committed = true
	return t.out, nil
}
