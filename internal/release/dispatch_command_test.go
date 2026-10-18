// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
)

func dispatchNativeFixture(t *testing.T) (string, string) {
	t.Helper()
	d := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	sha := commitAll(t, d)
	git(t, d, "remote", "add", "origin", "https://github.com/example/test.git")
	git(t, d, "update-ref", "refs/remotes/origin/main", sha)
	return d, sha
}
func dispatchOnlineFixture(commit, main string) hosting.ReleaseEvidence {
	success := "success"
	return hosting.ReleaseEvidence{MainSHA: main, Runs: []hosting.ReleaseRun{{Workflow: "ci.yml", HeadSHA: commit, Event: "push", Branch: "main", Status: "completed", Conclusion: &success}}, Network: []hosting.ReleaseRequest{{Method: "GET", Endpoint: "repos/example/test/git/ref/heads/main"}}}
}

func TestDispatchCommandPlansThenAppliesWithoutLocalWrites(t *testing.T) {
	d, sha := dispatchNativeFixture(t)
	before := snapshot(t, d)
	reads, writes := 0, 0
	reader := func(_ context.Context, root string, repo hosting.Repository, commit string, _ []string) (hosting.ReleaseEvidence, error) {
		reads++
		if root != d || repo.Owner != "example" || commit != sha {
			t.Fatalf("unqualified read: %s %+v %s", root, repo, commit)
		}
		return dispatchOnlineFixture(sha, sha), nil
	}
	writer := func(_ context.Context, root string, repo hosting.Repository, workflow string, inputs hosting.WorkflowDispatchInputs) (hosting.DispatchResponse, error) {
		writes++
		if root != d || repo.Name != "test" || workflow != "release-build.yml" || inputs.Commit != sha || inputs.Mode != "commit" || inputs.Version != "v0.2.0" {
			t.Fatalf("wrong write: %s %+v %s %+v", root, repo, workflow, inputs)
		}
		return hosting.DispatchResponse{Requests: 1, StatusCode: 204}, nil
	}
	o := DispatchOptions{Path: d, Commit: sha, Version: "v0.2.0"}
	plan := dispatchWithAdapters(context.Background(), o, reader, writer)
	if plan.Status != "ok" || plan.Verdict != "pass" || len(plan.PlanID) != 64 || plan.Applied || writes != 0 || reads != 1 || plan.Request == nil || plan.Request.Method != "POST" || plan.Request.Body.Ref != "main" || plan.Boundary.Effects.Writes {
		t.Fatalf("plan=%+v reads=%d writes=%d", plan, reads, writes)
	}
	expectedID, err := (DispatchPlan{Repository: "example/test", Host: "github.com", Workflow: "release-build.yml", Ref: "main", Inputs: DispatchInputs{Mode: "commit", Commit: sha, Version: "v0.2.0"}, ResolvedCommit: sha, MainCommit: sha, ConfigSHA256: hashBytes(exampleBytes(t, "go-cli")), Verdict: "pass"}).ID()
	if err != nil || plan.PlanID != expectedID {
		t.Fatalf("identity did not bind exact committed contract: got %s want %s err=%v", plan.PlanID, expectedID, err)
	}
	if plan.Approval != "a build was not authorized by this plan" || plan.WorkflowURL != "https://github.com/example/test/actions/workflows/release-build.yml" {
		t.Fatalf("disclosure=%+v", plan)
	}
	o.Apply = plan.PlanID
	applied := dispatchWithAdapters(context.Background(), o, reader, writer)
	if applied.Status != "ok" || !applied.Applied || writes != 1 || reads != 2 || !applied.Boundary.Effects.Writes || len(applied.Network) != 2 || applied.Network[1].Method != "POST" {
		t.Fatalf("apply=%+v reads=%d writes=%d", applied, reads, writes)
	}
	if changed := differences(before, snapshot(t, d)); len(changed) != 0 {
		t.Fatalf("dispatch changed local state: %v", changed)
	}
}

func TestDispatchCommandStaleAndFailedGatesNeverPost(t *testing.T) {
	for _, change := range []string{"main-moved", "gate-changed", "contract-changed", "main-ref-stale", "gate-failed"} {
		t.Run(change, func(t *testing.T) {
			d, sha := dispatchNativeFixture(t)
			main := sha
			green := true
			writes := 0
			reader := func(context.Context, string, hosting.Repository, string, []string) (hosting.ReleaseEvidence, error) {
				e := dispatchOnlineFixture(sha, main)
				if !green {
					e.Runs = nil
				}
				return e, nil
			}
			writer := func(context.Context, string, hosting.Repository, string, hosting.WorkflowDispatchInputs) (hosting.DispatchResponse, error) {
				writes++
				return hosting.DispatchResponse{Requests: 1, StatusCode: 204}, nil
			}
			o := DispatchOptions{Path: d, Commit: sha, Version: "v0.2.0"}
			original := dispatchWithAdapters(context.Background(), o, reader, writer)
			if original.Status != "ok" || original.Verdict != "pass" {
				t.Fatalf("baseline=%+v", original)
			}
			o.Apply = original.PlanID
			want := "plan-stale"
			switch change {
			case "main-moved", "contract-changed":
				if change == "contract-changed" {
					write(t, d, ".stw/release.toml", append(exampleBytes(t, "go-cli"), []byte("\n# contract bytes changed\n")...))
				} else {
					write(t, d, "new.txt", []byte("new main"))
				}
				main = commitAll(t, d)
				git(t, d, "update-ref", "refs/remotes/origin/main", main)
				if change == "contract-changed" {
					o.Commit = main
					sha = main
				}
			case "gate-changed":
				green = false
			case "main-ref-stale":
				main = strings.Repeat("b", 40)
				want = "main-ref-stale"
			case "gate-failed":
				green = false
				o.Apply = ""
				failed := dispatchWithAdapters(context.Background(), o, reader, writer)
				if failed.Status != "ok" || failed.Verdict != "fail" || len(failed.PlanID) != 64 {
					t.Fatalf("failed plan=%+v", failed)
				}
				o.Apply = failed.PlanID
				want = "gate-failed"
			}
			result := dispatchWithAdapters(context.Background(), o, reader, writer)
			if result.Error == nil || result.Error.Code != want || writes != 0 || result.Applied {
				t.Fatalf("change=%s result=%+v writes=%d", change, result, writes)
			}
			if want == "plan-stale" && (result.PlanID == "" || result.PlanID == original.PlanID) {
				t.Fatalf("new plan identity missing: %+v", result)
			}
		})
	}
}

func TestDispatchCommandFailuresDiscloseOnlyActualAttempts(t *testing.T) {
	for _, which := range []string{"read-error", "read-auth", "read-canceled", "auth", "http", "canceled"} {
		t.Run(which, func(t *testing.T) {
			d, sha := dispatchNativeFixture(t)
			reader := func(context.Context, string, hosting.Repository, string, []string) (hosting.ReleaseEvidence, error) {
				if which == "read-error" {
					return hosting.ReleaseEvidence{}, errors.New("unavailable")
				}
				if which == "read-auth" {
					return hosting.ReleaseEvidence{}, hosting.ErrAuthentication
				}
				if which == "read-canceled" {
					return hosting.ReleaseEvidence{}, context.Canceled
				}
				return dispatchOnlineFixture(sha, sha), nil
			}
			writer := func(context.Context, string, hosting.Repository, string, hosting.WorkflowDispatchInputs) (hosting.DispatchResponse, error) {
				if which == "auth" {
					return hosting.DispatchResponse{}, hosting.ErrDispatchAuth
				}
				return hosting.DispatchResponse{Requests: 1, StatusCode: 403}, errors.New("secret sensitive response body")
			}
			o := DispatchOptions{Path: d, Commit: sha, Version: "v0.2.0"}
			plan := dispatchWithAdapters(context.Background(), o, reader, writer)
			o.Apply = plan.PlanID
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if which == "canceled" {
				cancel()
			}
			result := dispatchWithAdapters(ctx, o, reader, writer)
			want := "online-unavailable"
			if which == "auth" || which == "read-auth" {
				want = "auth-unavailable"
			}
			if which == "canceled" || which == "read-canceled" {
				want = "canceled"
			}
			if result.Error == nil || result.Error.Code != want || result.Applied {
				t.Fatalf("%s: %+v", which, result)
			}
			if strings.Contains(result.Text(), "secret") {
				t.Fatalf("write body leaked: %s", result.Text())
			}
			if result.Boundary.Effects.Writes != (which == "http") {
				t.Fatalf("incorrect write disclosure: %+v", result)
			}
		})
	}
}

func TestDispatchCommandTagAnnotationAndProductSelection(t *testing.T) {
	for _, annotated := range []bool{false, true} {
		t.Run(map[bool]string{false: "lightweight", true: "annotated"}[annotated], func(t *testing.T) {
			d, sha := dispatchNativeFixture(t)
			args := []string{"tag", "v0.2.0"}
			if annotated {
				args = []string{"tag", "-a", "v0.2.0", "-m", "release"}
			}
			git(t, d, args...)
			reader := func(context.Context, string, hosting.Repository, string, []string) (hosting.ReleaseEvidence, error) {
				e := dispatchOnlineFixture(sha, sha)
				e.Tags = []string{"v0.2.0"}
				return e, nil
			}
			writer := func(context.Context, string, hosting.Repository, string, hosting.WorkflowDispatchInputs) (hosting.DispatchResponse, error) {
				t.Fatal("tag plan wrote")
				return hosting.DispatchResponse{}, nil
			}
			result := dispatchWithAdapters(context.Background(), DispatchOptions{Path: d, Tag: "v0.2.0"}, reader, writer)
			if result.Status != "ok" || result.Request == nil || result.Request.Body.Inputs.Mode != "tag" || result.Request.Body.Inputs.Commit != "" || result.Request.Body.Inputs.Tag != "v0.2.0" || result.Request.Body.Inputs.Version != "v0.2.0" || (result.Verdict == "pass") != annotated {
				t.Fatalf("tag result=%+v", result)
			}
		})
	}
}

func TestDispatchCommandNamedProductAndFetchDisclosure(t *testing.T) {
	d := repoWith(t, map[string][]byte{".stw/release.zlib.toml": exampleBytes(t, "mirror-zlib"), ".stw/release.busybox.toml": exampleBytes(t, "mirror-busybox")})
	sha := commitAll(t, d)
	git(t, d, "remote", "add", "origin", "https://github.com/example/test.git")
	git(t, d, "update-ref", "refs/remotes/origin/main", sha)
	git(t, d, "tag", "-a", "zlib-v1.3.2", "-m", "release")
	reads := 0
	reader := func(context.Context, string, hosting.Repository, string, []string) (hosting.ReleaseEvidence, error) {
		reads++
		return dispatchOnlineFixture(sha, sha), nil
	}
	writer := func(context.Context, string, hosting.Repository, string, hosting.WorkflowDispatchInputs) (hosting.DispatchResponse, error) {
		t.Fatal("planning wrote")
		return hosting.DispatchResponse{}, nil
	}
	for _, tc := range []struct{ product, code string }{{"", "product-required"}, {"unknown", "product-unknown"}} {
		r := dispatchWithAdapters(context.Background(), DispatchOptions{Path: d, Commit: sha, Version: "1.3.2", Product: tc.product}, reader, writer)
		if r.Error == nil || r.Error.Code != tc.code || reads != 0 {
			t.Fatalf("selection=%+v result=%+v reads=%d", tc, r, reads)
		}
	}
	result := dispatchWithAdapters(context.Background(), DispatchOptions{Path: d, Tag: "zlib-v1.3.2"}, reader, writer)
	if result.Status != "ok" || result.Verdict != "pass" || result.Request == nil || result.Request.Body.Inputs.Product != "zlib" || len(result.PlanID) != 64 {
		t.Fatalf("inferred product=%+v", result)
	}
	for _, expected := range []string{"zlib.net", "sources/zlib.lock.json", "network: fetch"} {
		if !strings.Contains(result.Text(), expected) {
			t.Fatalf("missing fetch disclosure %q: %s", expected, result.Text())
		}
	}
}

func TestDispatchCommandExplicitHostRetainsNativeRemoteIdentity(t *testing.T) {
	d, sha := dispatchNativeFixture(t)
	git(t, d, "remote", "set-url", "origin", "https://github.example.com/example/test.git")
	calls := 0
	reader := func(_ context.Context, _ string, repo hosting.Repository, _ string, _ []string) (hosting.ReleaseEvidence, error) {
		calls++
		if repo.Host != "github.example.com" || repo.Owner != "example" || repo.Name != "test" {
			t.Fatalf("host/native identity lost: %+v", repo)
		}
		return dispatchOnlineFixture(sha, sha), nil
	}
	writer := func(context.Context, string, hosting.Repository, string, hosting.WorkflowDispatchInputs) (hosting.DispatchResponse, error) {
		t.Fatal("planning wrote")
		return hosting.DispatchResponse{}, nil
	}
	result := dispatchWithAdapters(context.Background(), DispatchOptions{Path: d, Commit: sha, Version: "v0.2.0", Host: "github.example.com"}, reader, writer)
	if result.Status != "ok" || calls != 1 || result.WorkflowURL != "https://github.example.com/example/test/actions/workflows/release-build.yml" {
		t.Fatalf("host result=%+v calls=%d", result, calls)
	}
}

func TestDispatchCommandExplicitHexTagPlansAndApplies(t *testing.T) {
	for _, tag := range []string{"deadbeef1", strings.Repeat("a", 39) + "1", strings.Repeat("a", 63) + "1"} {
		t.Run(tag, func(t *testing.T) {
			prefix := strings.TrimSuffix(tag, "1")
			version := "scheme = \"pattern\"\ntag_template = \"" + prefix + "{version}\"\ntag_pattern = '^" + prefix + "(?P<version>(?P<build>[0-9]{1,9}))$'\norder = [\"build\"]"
			raw := []byte(strings.Replace(string(exampleBytes(t, "go-cli")), `scheme = "semver"`, version, 1))
			d := repoWith(t, map[string][]byte{".stw/release.toml": raw})
			sha := commitAll(t, d)
			git(t, d, "remote", "add", "origin", "https://github.com/example/test.git")
			git(t, d, "update-ref", "refs/remotes/origin/main", sha)
			git(t, d, "tag", "-a", tag, "-m", "annotated fixture")
			reads, writes := 0, 0
			reader := func(_ context.Context, _ string, _ hosting.Repository, commit string, _ []string) (hosting.ReleaseEvidence, error) {
				reads++
				if commit != sha {
					t.Fatalf("tag did not resolve to native commit: %q", commit)
				}
				e := dispatchOnlineFixture(sha, sha)
				e.Tags = []string{prefix + "0", tag}
				return e, nil
			}
			writer := func(_ context.Context, _ string, _ hosting.Repository, _ string, inputs hosting.WorkflowDispatchInputs) (hosting.DispatchResponse, error) {
				writes++
				if inputs.Mode != "tag" || inputs.Tag != tag || inputs.Commit != "" || inputs.Version != "1" {
					t.Fatalf("tag request mode lost: %+v", inputs)
				}
				return hosting.DispatchResponse{Requests: 1, StatusCode: 204}, nil
			}
			o := DispatchOptions{Path: d, Tag: tag}
			plan := dispatchWithAdapters(context.Background(), o, reader, writer)
			if plan.Status != "ok" || plan.Verdict != "pass" || plan.Request == nil || plan.Request.Body.Inputs.Tag != tag || len(plan.PlanID) != 64 || reads != 1 || writes != 0 {
				t.Fatalf("explicit annotated tag misclassified: status=%s verdict=%s error=%+v reads=%d writes=%d", plan.Status, plan.Verdict, plan.Error, reads, writes)
			}
			o.Apply = plan.PlanID
			applied := dispatchWithAdapters(context.Background(), o, reader, writer)
			if !applied.Applied || applied.Status != "ok" || reads != 2 || writes != 1 {
				t.Fatalf("tag apply failed: %+v reads=%d writes=%d", applied, reads, writes)
			}
			commit := dispatchWithAdapters(context.Background(), DispatchOptions{Path: d, Commit: sha, Version: "1"}, reader, writer)
			if commit.Status != "ok" || commit.Verdict != "fail" || commit.Request == nil || commit.Request.Body.Inputs.Mode != "commit" || commit.Request.Body.Inputs.Commit != sha || commit.Request.Body.Inputs.Tag != "" || writes != 1 {
				t.Fatalf("commit mode changed existing-tag checks: %+v", commit)
			}
			for _, code := range []string{"release-exists", "version-not-monotonic"} {
				found := false
				for _, gate := range commit.Gates {
					if gate.Code == code && gate.Outcome == "fail" {
						found = true
					}
				}
				if !found {
					t.Fatalf("commit gate %s did not retain existing-tag behavior: %+v", code, commit.Gates)
				}
			}
		})
	}
}
