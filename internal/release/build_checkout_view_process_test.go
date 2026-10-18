// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A trusted source-test compiler helper only. Never a product/project command.
func init() {
	switch os.Getenv("STW_BUILD_VIEW_NATIVE_TEST") {
	case "stdin-exit":
		var one [1]byte
		_, _ = os.Stdin.Read(one[:])
		os.Exit(0)
	case "wait":
		time.Sleep(time.Hour)
		os.Exit(0)
	case "large":
		fmt.Fprint(os.Stdout, string(bytes.Repeat([]byte("o"), 200000)))
		fmt.Fprint(os.Stderr, string(bytes.Repeat([]byte("e"), 180000)))
		os.Exit(0)
	case "dirty":
		os.Exit(1)
	case "normal":
		fmt.Fprint(os.Stdout, "semantic output")
		os.Exit(0)
	}
}
func TestBuildViewNativeSupervisor(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"normal", "dirty", "large", "wait"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			if mode == "wait" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
			}
			defer cancel()
			var output bytes.Buffer
			exit, diagnostic, err := runBuildViewNative(ctx, buildViewCommand{Executable: binary, Dir: t.TempDir(), Env: []string{"STW_BUILD_VIEW_NATIVE_TEST=" + mode, "GORACE=atexit_sleep_ms=0"}}, &output)
			if mode == "wait" {
				if err == nil {
					t.Fatal("canceled native helper succeeded")
				}
				return
			}
			if mode == "large" {
				if !errors.Is(err, errReleaseLimit) || len(diagnostic) > 64<<10 {
					t.Fatal("diagnostic overflow not bounded/refused")
				}
				return
			}
			if mode == "dirty" {
				if err != nil || exit != 1 {
					t.Fatalf("native dirty predicate lost: exit=%d err=%v", exit, err)
				}
				return
			}
			if err != nil || exit != 0 || output.String() != "semantic output" {
				t.Fatalf("native supervisor refused normal source helper: exit=%d err=%v", exit, err)
			}
		})
	}
}

func TestBuildViewNativeKeepsLeaseUntilOutputEOFAndReap(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	release, err := acquireBuildViewLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var held *os.File
	pidCh := make(chan int, 1)
	finished := make(chan error, 1)
	hooks := buildViewNativeHooks{pipes: func(stdout, stderr *os.File) {
		fd, e := syscall.Dup(int(stdout.Fd()))
		if e != nil {
			panic(e)
		}
		syscall.CloseOnExec(fd)
		held = os.NewFile(uintptr(fd), "trusted-fixture-held-output")
	}, started: func(pid int) { pidCh <- pid }}
	go func() {
		_, _, e := runBuildViewNative(ctx, buildViewCommand{Executable: binary, Dir: t.TempDir(), Env: []string{"STW_BUILD_VIEW_NATIVE_TEST=normal", "GORACE=atexit_sleep_ms=0"}}, &bytes.Buffer{}, hooks)
		release()
		finished <- e
	}()
	pid := <-pidCh
	defer func() {
		if held != nil {
			held.Close()
		}
	}()
	deadline := time.Now().Add(time.Second)
	for {
		state, e := observeBuildViewGroup(pid)
		if e == nil && state.leaderTerminal {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("synthetic leader did not become waitable")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-finished:
		t.Fatal("reaped/released with an owned output writer open")
	default:
	}
	admission, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if free, e := acquireBuildViewLease(admission); e == nil {
		free()
		t.Fatal("second job admitted during incomplete output cleanup")
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	held = nil
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("native group did not finish cleanup after EOF")
	}
	check, stopCheck := context.WithTimeout(context.Background(), time.Second)
	defer stopCheck()
	free, e := acquireBuildViewLease(check)
	if e != nil {
		t.Fatal(e)
	}
	free()
}

func TestBuildViewNativeCleansDescendantBeforeReap(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	input, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer write.Close()
	reaped := make(chan struct{})
	var companion *exec.Cmd
	hooks := buildViewNativeHooks{started: func(pid int) {
		companion = exec.Command(binary)
		companion.Env = []string{"STW_BUILD_VIEW_NATIVE_TEST=wait", "GORACE=atexit_sleep_ms=0"}
		companion.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: pid}
		if err := companion.Start(); err != nil {
			panic(err)
		}
		go func() { _ = companion.Wait(); close(reaped) }()
		if _, err := write.Write([]byte{1}); err != nil {
			panic(err)
		}
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err = runBuildViewNative(ctx, buildViewCommand{Executable: binary, Dir: t.TempDir(), Env: []string{"STW_BUILD_VIEW_NATIVE_TEST=stdin-exit", "GORACE=atexit_sleep_ms=0"}, Stdin: input}, &bytes.Buffer{}, hooks)
	if err == nil || !strings.Contains(err.Error(), "outlived its leader") {
		t.Fatalf("orphaned owned group was not explicitly refused/cleaned: %v", err)
	}
	select {
	case <-reaped:
	case <-time.After(time.Second):
		t.Fatal("owned synthetic companion not reaped")
	}
}

func TestBuildViewCanceledAdmissionStartsNoChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if release, err := acquireBuildViewLease(ctx); !errors.Is(err, context.Canceled) {
		if release != nil {
			release()
		}
		t.Fatal("canceled capture job admitted")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	started := false
	_, _, err = runBuildViewNative(ctx, buildViewCommand{Executable: binary, Dir: t.TempDir(), Env: []string{"STW_BUILD_VIEW_NATIVE_TEST=normal", "GORACE=atexit_sleep_ms=0"}}, &bytes.Buffer{}, buildViewNativeHooks{started: func(int) { started = true }})
	if !errors.Is(err, context.Canceled) || started {
		t.Fatal("canceled native job started a child")
	}
}
