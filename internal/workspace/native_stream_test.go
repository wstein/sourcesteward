// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNativeStreamHelper(t *testing.T) {
	for i, arg := range os.Args {
		if arg != "stw-stream-helper" {
			continue
		}
		args := os.Args[i+1:]
		n, _ := strconv.Atoi(args[1])
		switch args[0] {
		case "duplex":
			done := make(chan struct{})
			go func() { io.CopyN(os.Stderr, strings.NewReader(strings.Repeat("e", n)), int64(n)); close(done) }()
			io.Copy(os.Stdout, os.Stdin)
			<-done
		case "stdout":
			io.CopyN(os.Stdout, strings.NewReader(strings.Repeat("x", n)), int64(n))
		case "stderr":
			io.CopyN(os.Stderr, strings.NewReader(strings.Repeat("e", n)), int64(n))
		case "wait":
			time.Sleep(time.Minute)
		case "endless":
			for {
				fmt.Print(strings.Repeat("x", 4096))
			}
		}
		os.Exit(0)
	}
}

func streamTestCommand(t *testing.T, ctx context.Context, mode string, n int) *exec.Cmd {
	t.Helper()
	bin, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	bin, e = filepath.EvalSymlinks(bin)
	if e != nil {
		t.Fatal(e)
	}
	cmd, e := prepareSandboxed(ctx, bin, []string{"-test.run=^TestNativeStreamHelper$", "--", "stw-stream-helper", mode, strconv.Itoa(n)}, filepath.Dir(bin), nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	return cmd
}

func TestNativeStreamConcurrentChannelsAndBounds(t *testing.T) {
	for _, n := range []int{0, 65536, 65537} {
		ctx, deadlineCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer deadlineCancel()
		streamCtx, cancel := context.WithCancel(ctx)
		cmd := streamTestCommand(t, streamCtx, "duplex", n)
		input := bytes.Repeat([]byte{'i'}, 65536)
		var got bytes.Buffer
		stats, e := executeNativeStream(ctx, cancel, cmd, input, 65536, func(r io.Reader) error { _, e := io.Copy(&got, r); return e })
		cancel()
		if n <= 65536 {
			if e != nil || !bytes.Equal(got.Bytes(), input) || stats.Stdout != 65536 || stats.Stderr != int64(n) {
				t.Fatalf("n=%d stats=%+v error=%v", n, stats, e)
			}
		} else if !errors.Is(e, errNativeOutputLimit) || stats.Stderr != 65537 {
			t.Fatalf("stderr detection byte: %+v %v", stats, e)
		}
		if cmd.ProcessState == nil {
			t.Fatal("child not reaped")
		}
	}
	for _, n := range []int{65536, 65537, 1000000} {
		ctx, deadlineCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer deadlineCancel()
		streamCtx, cancel := context.WithCancel(ctx)
		cmd := streamTestCommand(t, streamCtx, "stdout", n)
		stats, e := executeNativeStream(ctx, cancel, cmd, nil, 65536, func(r io.Reader) error { _, e := io.Copy(io.Discard, r); return e })
		cancel()
		if n == 65536 && e != nil {
			t.Fatal(e)
		}
		if n > 65536 && (!errors.Is(e, errNativeStreamLimit) || stats.Stdout != 65537) {
			t.Fatalf("stdout detection: %+v %v", stats, e)
		}
	}
}

func TestNativeStreamEarlyFailureCancellationAndReaping(t *testing.T) {
	for _, mode := range []string{"endless", "wait"} {
		ctx, deadlineCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer deadlineCancel()
		streamCtx, cancel := context.WithCancel(ctx)
		cmd := streamTestCommand(t, streamCtx, mode, 0)
		sentinel := errors.New("parser refused")
		start := time.Now()
		_, e := executeNativeStream(ctx, cancel, cmd, bytes.Repeat([]byte{'i'}, 65536), 65536, func(r io.Reader) error {
			if mode == "endless" {
				var b [1]byte
				_, e := r.Read(b[:])
				if e != nil {
					return e
				}
				return sentinel
			}
			_, e := io.Copy(io.Discard, r)
			return e
		})
		cancel()
		if mode == "endless" && !errors.Is(e, sentinel) {
			t.Fatalf("parser failure: %v", e)
		}
		if mode == "wait" && !errors.Is(e, context.DeadlineExceeded) {
			t.Fatalf("cancellation: %v", e)
		}
		if time.Since(start) > 2*time.Second || cmd.ProcessState == nil {
			t.Fatal("child/blocked stdin not reaped promptly")
		}
		if e := cmd.Process.Signal(os.Signal(syscall.Signal(0))); !errors.Is(e, os.ErrProcessDone) {
			t.Fatalf("child remains: %v", e)
		}
	}
}

func TestNativeStreamUsesCanonicalLaunchAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bin, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	bin, e = filepath.EvalSymlinks(bin)
	if e != nil {
		t.Fatal(e)
	}
	root := gitTempDir(t)
	alias := filepath.Join(root, "alias")
	if e = os.Symlink(bin, alias); e != nil {
		t.Fatal(e)
	}
	if _, e = prepareSandboxed(ctx, alias, nil, root, nil, nil); e == nil {
		t.Fatal("aliased executable admitted")
	}
	if _, e = prepareSandboxed(ctx, bin, nil, root, []string{"/"}, nil); e == nil {
		t.Fatal("root write grant admitted")
	}
	if _, e = prepareSandboxed(ctx, bin, []string{"bad\x00argument"}, root, nil, nil); e == nil {
		t.Fatal("NUL argument admitted")
	}
	if _, e = prepareSandboxed(ctx, bin, []string{strings.Repeat("x", 65536)}, root, nil, nil); e == nil {
		t.Fatal("oversized command input admitted")
	}
}

func TestNativeStreamKeepsReadOnlyAndMinimalEnvironment(t *testing.T) {
	for _, key := range []string{"GIT_CONFIG_COUNT", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "SECRET_TOKEN"} {
		t.Setenv(key, "hostile")
	}
	bin, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	bin, e = filepath.EvalSymlinks(bin)
	if e != nil {
		t.Fatal(e)
	}
	root := gitTempDir(t)
	forbidden := filepath.Join(root, "forbidden")
	for _, args := range [][]string{{"env"}, {"files", forbidden}} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		streamCtx, stop := context.WithCancel(ctx)
		cmd, e := prepareSandboxed(streamCtx, bin, append([]string{"-test.run=^TestNativeSandboxHelper$", "--", "stw-sandbox-helper"}, args...), root, nil, nil)
		if e != nil {
			t.Fatal(e)
		}
		_, e = executeNativeStream(ctx, stop, cmd, nil, 65536, func(r io.Reader) error { _, e := io.Copy(io.Discard, r); return e })
		stop()
		cancel()
		if args[0] == "env" && e != nil {
			t.Fatalf("minimal environment: %v", e)
		}
		if args[0] == "files" && e == nil {
			t.Fatal("undeclared write admitted")
		}
	}
	if _, e = os.Stat(forbidden); !os.IsNotExist(e) {
		t.Fatal("undeclared write changed filesystem")
	}
}
