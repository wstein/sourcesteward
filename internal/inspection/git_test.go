// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestBoundedCapture(t *testing.T) {
	var c capture
	p := make([]byte, outputLimit+10)
	n, e := c.Write(p)
	if e != nil || n != len(p) || c.Len() != outputLimit || !c.overflow {
		t.Fatalf("%d %v", n, e)
	}
}
func TestCancellationAndMissingGit(t *testing.T) {
	d := repo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Inspect(ctx, d)
	if r.Diagnostics[0].Code != "canceled" {
		t.Fatalf("%+v", r)
	}
	r = inspect(context.Background(), d, func(context.Context, string, ...string) (string, int, error) { return "", -1, exec.ErrNotFound })
	if r.Diagnostics[0].Code != "git-unavailable" {
		t.Fatalf("%+v", r)
	}
}
func TestConcurrentHead(t *testing.T) {
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	n := 0
	run := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		if len(args) > 0 && args[0] == "symbolic-ref" {
			n++
			if n == 2 {
				git(t, d, "checkout", "--detach", "-q")
			}
		}
		return runGit(ctx, dir, args...)
	}
	r := inspect(context.Background(), d, run)
	if r.Diagnostics[0].Code != "changed-repository" {
		t.Fatalf("%+v", r)
	}
}
func TestGitFailureDoesNotLeakStderr(t *testing.T) {
	d := repo(t)
	r := inspect(context.Background(), d, func(context.Context, string, ...string) (string, int, error) {
		return "sensitive", -1, errors.New("sensitive")
	})
	if strings.Contains(r.Error(), "sensitive") {
		t.Fatal(r)
	}
}
func TestInvalidUTF8(t *testing.T) {
	r := Inspect(context.Background(), string([]byte{255}))
	if r.InputPath != "" || r.Diagnostics[0].Code != "invalid-path" {
		t.Fatalf("%+v", r)
	}
}
func TestOwnershipFailureNotBypassed(t *testing.T) {
	d := repo(t)
	run := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		for _, arg := range args {
			if strings.Contains(arg, "safe.directory") {
				t.Fatal("ownership argv bypass")
			}
		}
		if args[0] == "version" {
			return "git version 2.54.0\n", 0, nil
		}
		return "", 128, nil
	}
	r := inspect(context.Background(), d, run)
	if r.Status != "error" || r.Diagnostics[0].Code != "git-failed" {
		t.Fatalf("%+v", r)
	}
	for _, e := range gitEnvironment() {
		if strings.Contains(e, "safe.directory") {
			t.Fatal("ownership bypass")
		}
	}
}
func TestCancellationDuringGit(t *testing.T) {
	d := repo(t)
	ctx, cancel := context.WithCancel(context.Background())
	run := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		cancel()
		return "", -1, ctx.Err()
	}
	r := inspect(ctx, d, run)
	if r.Diagnostics[0].Code != "canceled" {
		t.Fatalf("%+v", r)
	}
}
func TestCancellationAtEachQuery(t *testing.T) {
	for _, query := range []string{"--git-common-dir", "--is-bare-repository", "--show-toplevel", "--show-object-format"} {
		t.Run(query, func(t *testing.T) {
			d := repo(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			run := func(ctx context.Context, dir string, args ...string) (string, int, error) {
				if args[len(args)-1] == query {
					cancel()
					return "", -1, ctx.Err()
				}
				return runGit(ctx, dir, args...)
			}
			r := inspect(ctx, d, run)
			if r.Diagnostics[0].Code != "canceled" {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func TestInvalidUTF8HeadRef(t *testing.T) {
	run := func(context.Context, string, ...string) (string, int, error) {
		return "refs/heads/" + string([]byte{255}) + "\n", 0, nil
	}
	if _, err := readHead(context.Background(), run, t.TempDir(), "sha1"); err == nil {
		t.Fatal("accepted invalidUTF8 ref")
	}
}
