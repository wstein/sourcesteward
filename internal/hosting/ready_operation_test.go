// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/readiness"
)

func readyOperationFixture(t *testing.T, f *collectionFixture) (readyOperationDeps, http.RoundTripper, *int, *int) {
	auth, closed := new(int), new(int)
	d := readyOperationDeps{resolve: func(context.Context, string, Options) Result {
		native := identity()
		native.InputPath = f.target
		native.Repository.Worktree = &f.target
		return Result{Status: "ok", Backend: "git", Identity: &native, Repository: &Repository{Host: "github.com", Owner: "o", Name: "r"}}
	}, api: func(_ context.Context, _, host string, rt http.RoundTripper) (*ghAPI, func(), error) {
		*auth++
		client, err := newSDKClient(host, "fixture-token", t.TempDir(), rt)
		return &ghAPI{host: host, client: client}, func() { *closed++ }, err
	}}
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		path := r.URL.RequestURI()
		f.calls[path]++
		raw, ok := f.routes[path]
		if !ok {
			t.Fatalf("unexpected HTTP %s", path)
		}
		status := 200
		if f.change != nil {
			status, raw = f.change(path, f.calls[path], raw)
		}
		return response(status, raw), nil
	})
	return d, rt, auth, closed
}
func TestReadyOperationCallerProjectionAndUsage(t *testing.T) {
	f := newCollectionFixture(t)
	d, rt, auth, closed := readyOperationFixture(t, f)
	got := checkReady(context.Background(), f.target, ReadyOptions{Options: Options{Number: 7, Repo: "o/r", Host: "github.com"}, PolicyLock: f.lockPath}, rt, d)
	if got.Decision != "indeterminate" || got.Components.Review.State != "pass" || got.Components.CI.State != "unknown" || got.Components.Constraints.State != "unknown" || got.Components.Deployment.State != "not-required" || got.Policy.Authority != "caller-supplied" {
		t.Fatalf("projection %+v", got)
	}
	if got.GitHubReported.Mergeable == nil || !*got.GitHubReported.Mergeable || got.GitHubReported.Rebaseable == nil || !*got.GitHubReported.Rebaseable || got.GitHubReported.MergeableState != "clean" {
		t.Fatal("observed display metadata discarded")
	}
	if *auth != 1 || *closed != 1 || !got.Boundary.NetworkAttempted || got.Boundary.Usage.Requests != 15 || got.Boundary.Usage.Pages != 5 || got.Boundary.Usage.Records != 5 || got.Boundary.Usage.Bytes <= 0 {
		t.Fatalf("usage %+v auth%d close%d", got.Boundary, *auth, *closed)
	}
	n := 0
	for _, s := range got.Sources {
		n += s.RequestCount
	}
	if n != got.Boundary.Usage.Requests {
		t.Fatalf("double/missing source counts %d", n)
	}
}
func TestReadyOperationRefusesBeforeAuthentication(t *testing.T) {
	for _, kind := range []string{"installed", "bad-lock", "native-failure", "jj-required", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := newCollectionFixture(t)
			d, rt, auth, _ := readyOperationFixture(t, f)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := ReadyOptions{Options: Options{Number: 7, Host: "github.com"}, PolicyLock: f.lockPath}
			switch kind {
			case "installed":
				f.lock.Deployment.Profile = "app"
				name := "candidate-channel"
				id := int64(99)
				f.lock.Deployment.Environment = &name
				f.lock.Deployment.PublisherAppID = &id
				f.lockPath = loaderLockFile(t, loaderJSON(t, f.lock))
				opts.PolicyLock = f.lockPath
			case "bad-lock":
				opts.PolicyLock = "/private-token-path/not-present"
			case "native-failure":
				d.resolve = func(context.Context, string, Options) Result { return Result{Status: "error"} }
			case "jj-required":
				opts.RequireJJ = true
			case "cancel":
				cancel()
			}
			got := checkReady(ctx, f.target, opts, rt, d)
			raw, _ := json.Marshal(got)
			if *auth != 0 || got.Boundary.NetworkAttempted || got.Boundary.Usage.Requests != 0 || got.Decision == "eligible" || got.Components.Review.Complete || strings.Contains(string(raw), "private-token-path") {
				t.Fatalf("refusal auth%d %+v", *auth, got)
			}
		})
	}
}
func TestReadyOperationEarlyShapeAndOpaqueFailure(t *testing.T) {
	f := newCollectionFixture(t)
	d, rt, _, closed := readyOperationFixture(t, f)
	d.api = func(context.Context, string, string, http.RoundTripper) (*ghAPI, func(), error) {
		return nil, func() { *closed++ }, errors.New("private-token /private-path")
	}
	got := checkReady(context.Background(), f.target, ReadyOptions{Options: Options{Number: 7}, PolicyLock: f.lockPath}, rt, d)
	raw, _ := json.Marshal(got)
	if got.Decision != "error" || *closed != 1 || strings.Contains(string(raw), "private-token") {
		t.Fatalf("opaque error %s", raw)
	}
	// The accepted evaluator fixture emits this same public shape for early error,
	// including zero policy binding; no fake parsed policy enters the operation.
	_, policy, _ := policyLoaderFixture(t)
	p, err := readiness.ParsePolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	sample := readiness.Sample{ObservedAt: got.ObservedAt, OperationalError: true, Components: got.Components, Boundary: got.Boundary, Diagnostics: got.Diagnostics}
	expected, err := readiness.Evaluate(p, sample)
	if err != nil {
		t.Fatal(err)
	}
	var a, b map[string]any
	json.Unmarshal(raw, &a)
	json.Unmarshal(expected.RawJSON(), &b)
	for _, key := range []string{"schema_version", "kind", "decision", "policy", "components"} {
		x, _ := json.Marshal(a[key])
		y, _ := json.Marshal(b[key])
		if string(x) != string(y) {
			t.Fatalf("early contract mismatch %s: %s %s", key, x, y)
		}
	}
}

func TestReadyOperationCrossBindsPreAuthRawLock(t *testing.T) {
	for _, change := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "whitespace-drift"}[change], func(t *testing.T) {
			f := newCollectionFixture(t)
			d, rt, _, _ := readyOperationFixture(t, f)
			api := d.api
			d.api = func(ctx context.Context, path, host string, rt http.RoundTripper) (*ghAPI, func(), error) {
				if change {
					raw, err := os.ReadFile(f.lockPath)
					if err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(f.lockPath, append(raw, ' '), 0600); err != nil {
						t.Fatal(err)
					}
				}
				return api(ctx, path, host, rt)
			}
			got := checkReady(context.Background(), f.target, ReadyOptions{Options: Options{Number: 7}, PolicyLock: f.lockPath}, rt, d)
			if change {
				if got.Components.Review.State == "pass" || !collectionReason(got.Components.Review, "sample-changed") {
					t.Fatal("pre-auth raw drift passed")
				}
			} else if got.Components.Review.State != "pass" {
				t.Fatal("unchanged raw bytes rejected")
			}
		})
	}
}
func TestReadyOperationFinalCancelAndDeniedAreNonpositive(t *testing.T) {
	for _, kind := range []string{"denied", "late-cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := newCollectionFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.change = func(path string, n int, raw string) (int, string) {
				if path == collectionReport && n == 2 {
					if kind == "late-cancel" {
						cancel()
					} else {
						return 403, `{"private":"private-token"}`
					}
				}
				return 200, raw
			}
			d, rt, _, closed := readyOperationFixture(t, f)
			got := checkReady(ctx, f.target, ReadyOptions{Options: Options{Number: 7}, PolicyLock: f.lockPath}, rt, d)
			raw, _ := json.Marshal(got)
			if got.Components.Review.State == "pass" || got.Components.Review.Complete || got.Boundary.Complete || got.Decision == "eligible" || *closed != 1 || strings.Contains(string(raw), "private-token") {
				t.Fatalf("late failure %+v", got)
			}
			if kind == "late-cancel" && got.Decision != "error" {
				t.Fatal("cancellation lost")
			}
		})
	}
}

func TestReadyOperationRealNativeAndFakeHTTPPreservesFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GH_CONFIG_DIR", filepath.Join(home, "gh"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_TEMPLATE_DIR", t.TempDir())
	fake := filepath.Join(t.TempDir(), "gh")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\ncase \"$*\" in\n --version) echo 'gh version 2.102.0';;\n 'auth token --hostname github.com') echo fixture-token;;\n *) exit 1;;\nesac\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_PATH", fake)
	f := newCollectionFixture(t)
	if out, err := exec.Command("git", "init", "--initial-branch=topic", f.target).CombinedOutput(); err != nil {
		t.Fatalf("native fixture: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(f.target, "dirty.txt"), []byte("caller data"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]string {
		m := map[string]string{}
		err := filepath.WalkDir(f.target, func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !e.IsDir() {
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				m[strings.TrimPrefix(path, f.target)] = string(raw)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	before := snapshot()
	_, rt, _, _ := readyOperationFixture(t, f)
	got := CheckReadyWithTransport(context.Background(), f.target, ReadyOptions{Options: Options{Number: 7, Repo: "o/r", Host: "github.com"}, PolicyLock: f.lockPath}, rt)
	if got.Backend == nil || *got.Backend != "git" || got.Components.Review.State != "pass" || got.Decision != "indeterminate" || !reflect.DeepEqual(before, snapshot()) {
		t.Fatalf("native/effect contract %+v", got)
	}
}

func TestReadyOperationDeadlineIncludesNativeAuthAndAllReads(t *testing.T) {
	f := newCollectionFixture(t)
	d, rt, _, _ := readyOperationFixture(t, f)
	resolve, api := d.resolve, d.api
	var deadline time.Time
	d.resolve = func(ctx context.Context, path string, o Options) Result {
		var ok bool
		deadline, ok = ctx.Deadline()
		if !ok || time.Until(deadline) > 30*time.Second {
			t.Fatal("unbounded native phase")
		}
		return resolve(ctx, path, o)
	}
	d.api = func(ctx context.Context, path, host string, rt http.RoundTripper) (*ghAPI, func(), error) {
		end, ok := ctx.Deadline()
		meter := rt.(readyMeterTransport)
		if !ok || !end.Equal(deadline) || !meter.budget.started.Add(30*time.Second).Equal(deadline) {
			t.Fatal("authentication reset operation deadline")
		}
		return api(ctx, path, host, rt)
	}
	reads := 0
	inner := rt
	rt = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		reads++
		end, ok := req.Context().Deadline()
		if !ok || !end.Equal(deadline) {
			t.Fatal("reader reset operation deadline")
		}
		return inner.RoundTrip(req)
	})
	got := checkReady(context.Background(), f.target, ReadyOptions{Options: Options{Number: 7}, PolicyLock: f.lockPath}, rt, d)
	if reads != 15 || got.Boundary.Usage.Requests != reads {
		t.Fatalf("actual requests %d usage%d", reads, got.Boundary.Usage.Requests)
	}
}

func TestReadyOperationRawMergeabilityIsDisplayOnly(t *testing.T) {
	for _, value := range []string{"true", "false", "null"} {
		t.Run(value, func(t *testing.T) {
			f := newCollectionFixture(t)
			f.routes[collectionPR] = strings.ReplaceAll(strings.ReplaceAll(f.routes[collectionPR], `"mergeable":true`, `"mergeable":`+value), `"rebaseable":true`, `"rebaseable":`+value)
			f.change = func(path string, n int, raw string) (int, string) {
				if path == collectionPR && n == 2 {
					return 200, strings.ReplaceAll(raw, `"mergeable_state":"clean"`, `"mergeable_state":"blocked"`)
				}
				return 200, raw
			}
			d, rt, _, _ := readyOperationFixture(t, f)
			got := checkReady(context.Background(), f.target, ReadyOptions{Options: Options{Number: 7}, PolicyLock: f.lockPath}, rt, d)
			if got.Decision != "indeterminate" || got.Components.Review.State != "pass" || got.Components.Constraints.State != "unknown" || got.GitHubReported.MergeableState != "clean" {
				t.Fatal("display changed decision or lost sampled identity")
			}
			if value == "null" {
				if got.GitHubReported.Mergeable != nil || got.GitHubReported.Rebaseable != nil {
					t.Fatal("nullable field invented")
				}
			} else if got.GitHubReported.Mergeable == nil || *got.GitHubReported.Mergeable != (value == "true") || got.GitHubReported.Rebaseable == nil || *got.GitHubReported.Rebaseable != (value == "true") {
				t.Fatal("raw field changed")
			}
		})
	}
}
