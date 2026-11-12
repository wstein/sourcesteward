// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"io"
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
func TestBoundedCaptureThroughIOCopy(t *testing.T) {
	var c capture
	source := io.LimitReader(strings.NewReader(strings.Repeat("x", outputLimit+10)), outputLimit+10)
	n, e := io.Copy(&c, source)
	if e != nil || n != outputLimit+10 || c.Len() != outputLimit || !c.overflow {
		t.Fatalf("copy bypassed capture bound: n=%d length=%d overflow=%t err=%v", n, c.Len(), c.overflow, e)
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
func TestVersionAtLeast(t *testing.T) {
	for _, tc := range []struct {
		v     string
		minor int
		want  bool
	}{
		{"2.42.9", 43, false}, {"2.43.0", 43, true}, {"2.43.9", 43, true}, {"2.46.0", 43, true}, {"3.0", 43, true}, {"3.x", 43, false},
		{"2.45.9", 46, false}, {"2.46.0", 46, true}, {"2.47.3", 46, true}, {"2.56.0", 46, true}, {"3.0", 46, true}, {"3.0.0", 46, true},
		{"1.99.0", 43, false}, {"0.0", 43, false}, {"", 43, false}, {"2", 43, false}, {"x.y.z", 43, false}, {"2.x", 43, false},
		{"v2.46.0", 43, false}, {"-2.50", 43, false}, {"+2.50", 43, false}, {"+3.0", 43, false}, {"3.+1", 43, false}, {"2. 50", 43, false},
	} {
		if got := VersionAtLeast(tc.v, 2, tc.minor); got != tc.want {
			t.Errorf("VersionAtLeast(%q, 2, %d) = %t, want %t", tc.v, tc.minor, got, tc.want)
		}
	}
}

// Two floors: the identity commands (inspect, sync, status) accept Git 2.43; every other caller needs
// 2.46, because older Git fetches missing objects of a partial clone and ignores GIT_NO_LAZY_FETCH.
func TestVersionFloors(t *testing.T) {
	d := repo(t)
	versionRunner := func(version string) runner {
		return func(ctx context.Context, dir string, args ...string) (string, int, error) {
			if args[0] == "version" {
				return "git version " + version + "\n", 0, nil
			}
			return "", 128, nil
		}
	}
	const identity = "inspection requires Git 2.43 or newer"
	const strict = "inspection requires Git 2.46 or newer (older Git fetches missing objects of a partial clone and ignores GIT_NO_LAZY_FETCH)"
	run := map[string]func(string) Result{
		"identity": func(v string) Result {
			return inspectAt(context.Background(), d, versionRunner(v), false, floorIdentity)
		},
		"strict": func(v string) Result { return inspect(context.Background(), d, versionRunner(v)) },
	}
	for _, tc := range []struct{ floor, version, want string }{
		{"identity", "2.42.0", identity}, {"identity", "2.40.1", identity},
		{"identity", "2.43.0", ""}, {"identity", "2.43.9", ""}, {"identity", "2.45.0", ""}, {"identity", "2.46.0", ""}, {"identity", "3.0", ""},
		{"strict", "2.43.0", strict}, {"strict", "2.45.9", strict}, {"strict", "2.42.0", strict},
		{"strict", "2.46.0", ""}, {"strict", "2.47.3", ""}, {"strict", "3.0", ""},
	} {
		r := run[tc.floor](tc.version)
		if tc.want != "" {
			if r.Status != "error" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "git-failed" || r.Diagnostics[0].Message != tc.want {
				t.Errorf("%s %s: %+v", tc.floor, tc.version, r)
			}
			continue
		}
		if len(r.Diagnostics) > 0 && strings.Contains(r.Diagnostics[0].Message, "inspection requires Git") {
			t.Errorf("%s %s rejected: %+v", tc.floor, tc.version, r)
		}
	}
}

// Inspect (inspect, sync, status) reads the identity of a real repository with every supported Git,
// from 2.43. InspectStrict refuses Git before 2.46 and names the reason. This test is in the run that
// TestMain limits on Git before 2.46.
func TestIdentityInspectionOnTheInstalledGit(t *testing.T) {
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	r := Inspect(context.Background(), d)
	if r.Status != "ok" || r.Backend != "git" || r.Repository == nil || r.Repository.Head.State != "attached" {
		t.Fatalf("Inspect: %+v", r)
	}
	s := InspectStrict(context.Background(), d)
	if installedGitAtLeast(t, 2, 46) {
		if s.Status != "ok" {
			t.Fatalf("InspectStrict: %+v", s)
		}
		return
	}
	if s.Status != "error" || len(s.Diagnostics) != 1 || !strings.Contains(s.Diagnostics[0].Message, "Git 2.46") {
		t.Fatalf("InspectStrict on Git before 2.46: %+v", s)
	}
}
