// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeSandboxHelper(t *testing.T) {
	for i, v := range os.Args {
		if v != "stw-sandbox-helper" {
			continue
		}
		a := os.Args[i+1:]
		switch a[0] {
		case "files":
			if err := os.WriteFile(a[1], []byte("allowed"), 0600); err != nil {
				os.Exit(40)
			}
			for _, p := range a[2:] {
				if err := os.WriteFile(p, []byte("escaped"), 0600); err == nil {
					os.Exit(41)
				}
			}
		case "null":
			f, e := os.OpenFile("/dev/null", os.O_RDWR, 0)
			if e != nil {
				os.Exit(44)
			}
			if _, e = f.Write([]byte("discard")); e != nil {
				os.Exit(45)
			}
			f.Close()
			u, e := os.Open("/dev/urandom")
			if e != nil {
				os.Exit(46)
			}
			buf := make([]byte, 16)
			if n, e := u.Read(buf); e != nil || n != 16 {
				os.Exit(47)
			}
			u.Close()
		case "network":
			c, e := net.DialTimeout("tcp", a[1], time.Second)
			if e == nil {
				c.Close()
				os.Exit(42)
			}
		case "output":
			fmt.Print(strings.Repeat("x", 70000))
		case "spawn", "spawn-exit", "spawn-flood":
			// A parent that starts a descendant sharing its stderr and tells the test.
			// spawn and spawn-flood then live until killed; spawn-exit ends when its
			// stdin does. spawn-flood first writes more stderr than the limit.
			child := exec.Command(os.Args[0], append([]string{"-test.run=^TestNativeSandboxHelper$", "--", "stw-sandbox-helper", "hold"}, a[1:]...)...)
			child.Stderr = os.Stderr
			control, alive := os.NewFile(3, "control"), os.NewFile(4, "alive")
			child.ExtraFiles = []*os.File{control, alive}
			if e := child.Start(); e != nil {
				os.Exit(48)
			}
			// Only the descendant keeps the test's pipes.
			control.Close()
			alive.Close()
			if a[0] == "spawn-flood" {
				fmt.Fprint(os.Stderr, strings.Repeat("x", 70000))
			}
			fmt.Print("ready\n")
			if a[0] == "spawn-exit" {
				io.Copy(io.Discard, os.Stdin)
			} else {
				time.Sleep(time.Minute)
			}
		case "hold":
			// The descendant lives until the test closes the control pipe.
			io.Copy(io.Discard, os.NewFile(3, "control"))
		case "wait":
			time.Sleep(time.Minute)
		case "env":
			for _, k := range []string{"GIT_CONFIG_COUNT", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "SECRET_TOKEN"} {
				if os.Getenv(k) != "" {
					os.Exit(43)
				}
			}
		}
		os.Exit(0)
	}
}

func sandboxTestRun(t *testing.T, ctx context.Context, args []string, grants, protected []string) (string, int, error) {
	t.Helper()
	bin, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	bin, e = filepath.EvalSymlinks(bin)
	if e != nil {
		t.Fatal(e)
	}
	out, code, e := runSandboxed(ctx, bin, append([]string{"-test.run=^TestNativeSandboxHelper$", "--", "stw-sandbox-helper"}, args...), filepath.Dir(bin), grants, protected)
	if errors.Is(e, errNativeSandboxUnavailable) && os.Getenv("STW_REQUIRE_NATIVE_SANDBOX") == "" {
		t.Skip("required native sandbox unavailable: ", e)
	}
	return out, code, e
}

func TestNativeSandboxWriteConfinement(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	grant := filepath.Join(root, "grant")
	protected := filepath.Join(grant, "existing")
	outside := filepath.Join(root, "source")
	for _, p := range []string{grant, protected, outside} {
		if e := os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	_, code, e := sandboxTestRun(t, context.Background(), []string{"files", filepath.Join(grant, "new"), filepath.Join(protected, "index"), filepath.Join(outside, "index")}, []string{grant}, []string{protected})
	if e != nil || code != 0 {
		t.Fatalf("sandbox: code=%d error=%v", code, e)
	}
	if b, e := os.ReadFile(filepath.Join(grant, "new")); e != nil || string(b) != "allowed" {
		t.Fatal("declared write missing", e)
	}
	for _, p := range []string{filepath.Join(protected, "index"), filepath.Join(outside, "index")} {
		if _, e := os.Stat(p); !os.IsNotExist(e) {
			t.Fatal("undeclared file", p, e)
		}
	}
}
func TestNativeSandboxDeniesNetwork(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		if os.Getenv("STW_REQUIRE_NATIVE_SANDBOX") == "" && errors.Is(e, os.ErrPermission) {
			t.Skip("outer host policy denies network probe listener")
		}
		t.Fatal(e)
	}
	defer l.Close()
	_, code, e := sandboxTestRun(t, context.Background(), []string{"network", l.Addr().String()}, nil, nil)
	if e != nil || code != 0 {
		t.Fatalf("network confinement: %d %v", code, e)
	}
}
func TestNativeSandboxBoundsAndEnvironment(t *testing.T) {
	for _, k := range []string{"GIT_CONFIG_COUNT", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "SECRET_TOKEN"} {
		t.Setenv(k, "hostile")
	}
	_, code, e := sandboxTestRun(t, context.Background(), []string{"env"}, nil, nil)
	if e != nil || code != 0 {
		t.Fatalf("environment: %d %v", code, e)
	}
	_, _, e = sandboxTestRun(t, context.Background(), []string{"output"}, nil, nil)
	if !errors.Is(e, errNativeOutputLimit) {
		t.Fatalf("output: %v", e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _, e = sandboxTestRun(t, ctx, []string{"wait"}, nil, nil)
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", e)
	}
}

func TestNativeSandboxRejectsAliasedGrant(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	alias := filepath.Join(root, "alias")
	if e := os.Symlink(root, alias); e != nil {
		t.Fatal(e)
	}
	bin, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	bin, e = filepath.EvalSymlinks(bin)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e := runSandboxed(context.Background(), bin, nil, root, []string{alias}, nil); e == nil {
		t.Fatal("aliased write grant admitted")
	}
	if _, _, e := runSandboxed(context.Background(), bin, nil, root, []string{"/"}, nil); e == nil {
		t.Fatal("root write grant admitted")
	}
}

func TestNativeSandboxAllowsOnlyNullDeviceWrite(t *testing.T) {
	_, code, e := sandboxTestRun(t, context.Background(), []string{"null"}, nil, nil)
	if e != nil || code != 0 {
		t.Fatalf("null device: code=%d error=%v", code, e)
	}
}
