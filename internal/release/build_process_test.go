// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This is trusted source-test machinery, not a project script or product build.
// Only the already-compiled Go test executable runs, with temporary marker files.
func TestBuildSyntheticProcessHelper(t *testing.T) {
	mode := os.Getenv("STW_SYNTHETIC_MODE")
	if mode == "" {
		return
	}
	if mode == "print-argv0" {
		fmt.Fprint(os.Stdout, os.Args[0])
		os.Exit(0)
	}
	pidfile := os.Getenv("STW_SYNTHETIC_PID")
	if mode == "descendant" {
		io.WriteString(os.Stdout, strings.Repeat("o", (1<<20)+31))
		io.WriteString(os.Stderr, strings.Repeat("e", (1<<20)+37))
		if err := os.WriteFile(pidfile, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			os.Exit(21)
		}
		io.WriteString(os.Stderr, syntheticReadyMarker)
		for {
			time.Sleep(time.Hour)
		}
	}
	binary, err := os.Executable()
	if err != nil {
		os.Exit(22)
	}
	child := exec.Command(binary, "-test.run=^TestBuildSyntheticProcessHelper$")
	child.Env = []string{"STW_SYNTHETIC_MODE=descendant", "STW_SYNTHETIC_PID=" + pidfile}
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		os.Exit(23)
	}
	// No wall-clock bound here: the caller's timeout kills this process group.
	for {
		if _, err := os.Stat(pidfile); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if mode == "exit-leader" {
		os.Exit(0)
	}
	for {
		time.Sleep(time.Hour)
	}
}

// syntheticReadyMarker is written to stderr after the descendant has written the
// pid file and filled both bounded streams, so a test can fire its deadline on
// an observed event instead of racing process start-up against a wall clock.
const syntheticReadyMarker = "\nSTW-SYNTHETIC-READY\n"

// readyDeadline reports context.DeadlineExceeded once ready is closed. It stands
// in for a wall-clock deadline that begins when the descendant is known to run.
type readyDeadline struct {
	context.Context
	ready <-chan struct{}
}

func (c readyDeadline) Done() <-chan struct{} { return c.ready }
func (c readyDeadline) Err() error {
	select {
	case <-c.ready:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// markerWatcher closes ready once the marker has been seen on the stream, also
// when the marker is split across two writes.
type markerWatcher struct {
	next  io.Writer
	tail  []byte
	ready chan struct{}
	fired bool
}

func (w *markerWatcher) Write(p []byte) (int, error) {
	if !w.fired {
		w.tail = append(w.tail, p...)
		if bytes.Contains(w.tail, []byte(syntheticReadyMarker)) {
			w.fired = true
			close(w.ready)
		} else if keep := len(syntheticReadyMarker) - 1; len(w.tail) > keep {
			w.tail = append([]byte(nil), w.tail[len(w.tail)-keep:]...)
		}
	}
	return w.next.Write(p)
}

type markerRunner struct {
	inner buildRunner
	ready chan struct{}
}

func (r markerRunner) Run(ctx context.Context, c BuildCommand, stdout, stderr io.Writer) (int, error) {
	return r.inner.Run(ctx, c, stdout, &markerWatcher{next: stderr, ready: r.ready})
}

func syntheticChildRunning(pid int) bool {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		return true
	}
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		return true
	}
	fields := strings.Fields(string(raw[end+1:]))
	return len(fields) == 0 || (fields[0] != "Z" && fields[0] != "X")
}

// runSyntheticBuildLifecycle runs the helper under the production runner. With
// timeoutOnReady the deadline fires when the descendant reports readiness and
// timeout is only a safety net; otherwise timeout is a plain wall-clock bound.
func runSyntheticBuildLifecycle(t *testing.T, mode string, timeout time.Duration, timeoutOnReady bool) BuildInvocationResult {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc distinguishes a killed orphan from an unreaped zombie; other platform runtime evidence remains unverified")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	pidfile := filepath.Join(root, "child.pid")
	ctx, runner := context.Background(), buildRunner(projectRunner{})
	if timeoutOnReady {
		ready := make(chan struct{})
		ctx, runner = readyDeadline{Context: ctx, ready: ready}, markerRunner{inner: runner, ready: ready}
	}
	result := runBuildCommand(ctx, runner, BuildCommand{Argv: []string{binary, "-test.run=^TestBuildSyntheticProcessHelper$"}, Env: []string{"STW_SYNTHETIC_MODE=" + mode, "STW_SYNTHETIC_PID=" + pidfile}, Dir: root, Timeout: timeout})
	raw, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("synthetic descendant did not start: %v (exit%d,err%v)", err, result.Exit, result.err)
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(pid, syscall.SIGKILL) // also clean the intentionally failing RED fixture
	deadline := time.Now().Add(time.Second)
	for syntheticChildRunning(pid) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if syntheticChildRunning(pid) {
		t.Fatalf("synthetic descendant %d survived runner exit (exit%d,err%v)", pid, result.Exit, result.err)
	}
	if !result.StdoutTruncated || !result.StderrTruncated || len(result.Stdout) != 1<<20 || len(result.Stderr) != 1<<20 {
		t.Fatal("synthetic streams were not bounded")
	}
	return result
}
func TestBuildWaitDelayKillsSyntheticDescendantGroup(t *testing.T) {
	r := runSyntheticBuildLifecycle(t, "exit-leader", 10*time.Second, false)
	if r.err == nil {
		t.Fatal("inherited pipe deadline unexpectedly succeeded")
	}
}
func TestBuildTimeoutKillsSyntheticDescendantGroup(t *testing.T) {
	r := runSyntheticBuildLifecycle(t, "hold-leader", time.Minute, true)
	if r.err != context.DeadlineExceeded {
		t.Fatalf("%v", r.err)
	}
}

func TestBuildProductionPreservesDeclaredArgvZero(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	name := "synthetic-test-tool"
	if err := os.Symlink(binary, filepath.Join(root, name)); err != nil {
		t.Fatal(err)
	}
	result := runBuildCommand(context.Background(), projectRunner{}, BuildCommand{Argv: []string{name, "-test.run=^TestBuildSyntheticProcessHelper$"}, Env: []string{"PATH=" + root, "STW_SYNTHETIC_MODE=print-argv0"}, Dir: root, Timeout: 5 * time.Second})
	if result.err != nil || result.Exit != 0 || result.Stdout != name {
		t.Fatalf("argv0 %q, expected%q (exit%d,err%v)", result.Stdout, name, result.Exit, result.err)
	}
}
