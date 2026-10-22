// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/release"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

func runDispatchCLI(t *testing.T, run func(context.Context, release.DispatchOptions) release.DispatchResult, args ...string) (int, string, string) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	cmd := releaseDispatchCommandWithRunner(run)
	cmd.SetOut(&out)
	cmd.SetErr(&diagnostic)
	cmd.SetArgs(args)
	err := cmd.Execute()
	if err == nil {
		return 0, out.String(), diagnostic.String()
	}
	return exitStatus(&diagnostic, err), out.String(), diagnostic.String()
}
func TestReleaseDispatchUsageBeforeOperation(t *testing.T) {
	c := strings.Repeat("a", 40)
	id := strings.Repeat("b", 64)
	for _, args := range [][]string{
		{}, {"--commit", c}, {"--version", "v1"}, {"--commit", "abc123", "--version", "v1"}, {"--tag", "bad\nname"}, {"--tag", "v1", "--version", ""}, {"--commit", c, "--tag", "v1", "--version", "v1"}, {"--commit", c, "--version", "v1", "--apply", ""}, {"--tag", "v1", "--apply", "bad"}, {"--tag", "v1", "--host", "https://github.com"}, {"--tag", "v1", "--repo", "o/r/other"}, {"--tag", "v1", ""}, {"--tag", "v1", "a", "b"}, {"--commit", c, "--version", "v1", "--apply", id, "--online"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, out, _ := runDispatchCLI(t, func(context.Context, release.DispatchOptions) release.DispatchResult {
				t.Fatal("usage reached operation")
				return release.DispatchResult{}
			}, args...)
			if code != 2 || out != "" {
				t.Fatalf("args=%q code=%d out=%s", args, code, out)
			}
		})
	}
}
func TestReleaseDispatchRoutesSelectorsAndRendersEffects(t *testing.T) {
	c := strings.Repeat("a", 40)
	id := strings.Repeat("b", 64)
	for _, tag := range []bool{false, true} {
		args := []string{"--commit", c, "--version", "v1.0.0"}
		if tag {
			args = []string{"--tag", "v1.0.0"}
		}
		args = append(args, "/repo", "--product", "cli", "--repo", "owner/repo", "--host", "github.example.com", "--apply", id, "--json")
		calls := 0
		code, out, diagnostic := runDispatchCLI(t, func(_ context.Context, o release.DispatchOptions) release.DispatchResult {
			calls++
			if o.Path != "/repo" || o.Product != "cli" || o.Repo != "owner/repo" || o.Host != "github.example.com" || o.Apply != id || (o.Tag != "") != tag || (o.Commit == c) == tag {
				t.Fatalf("wrong selector/options: %+v", o)
			}
			return release.DispatchResult{PlanResult: release.PlanResult{SchemaVersion: 1, Command: "release-dispatch", Status: "ok", Verdict: "pass"}, PlanID: id, Applied: true, Approval: "a build was not authorized by this plan"}
		}, args...)
		var result map[string]any
		if code != 0 || calls != 1 || json.Unmarshal([]byte(out), &result) != nil || result["command"] != "release-dispatch" || result["applied"] != true {
			t.Fatalf("code=%d out=%s diagnostic=%s", code, out, diagnostic)
		}
	}
	result := release.DispatchResult{PlanResult: release.PlanResult{Status: "ok", Verdict: "pass", WouldBuild: []release.PlanBuild{{ID: "fetch-build", Network: "fetch", NetworkHosts: []string{"example.org"}, LockFiles: []string{"deps.lock"}}}}, Approval: "a build was not authorized by this plan", PlanID: id, Request: &release.DispatchRequest{Method: "POST", Endpoint: "repos/o/r/actions/workflows/release.yml/dispatches"}}
	code, out, _ := runDispatchCLI(t, func(context.Context, release.DispatchOptions) release.DispatchResult { return result }, "--commit", c, "--version", "v1.0.0")
	for _, text := range []string{"a build was not authorized by this plan", "network write on --apply", "draft release", "no tag, no publication", "example.org", "deps.lock", id, "POST"} {
		if code != 0 || !strings.Contains(out, text) {
			t.Fatalf("missing %q: code=%d out=%s", text, code, out)
		}
	}
}
func TestReleaseDispatchExitPrecedence(t *testing.T) {
	for _, tc := range []struct {
		status, verdict string
		want            int
	}{{"ok", "pass", 0}, {"ok", "fail", 3}, {"ok", "incomplete", 1}, {"refused", "fail", 1}} {
		code, out, _ := runDispatchCLI(t, func(context.Context, release.DispatchOptions) release.DispatchResult {
			return release.DispatchResult{PlanResult: release.PlanResult{Status: tc.status, Verdict: tc.verdict, Error: &release.CheckError{Code: "plan-stale", Message: "changed"}}}
		}, "--tag", "v1.0.0", "--json")
		if code != tc.want || out == "" {
			t.Fatalf("%+v code=%d out=%s", tc, code, out)
		}
	}
}

func TestReleaseDispatchRegisteredRefusalUsesVersionedJSON(t *testing.T) {
	code, out, diagnostic := runRelease(t, "dispatch", "--commit", strings.Repeat("a", 40), "--version", "v1.0.0", t.TempDir(), "--json")
	var result map[string]any
	if code != 1 || json.Unmarshal([]byte(out), &result) != nil || result["command"] != "release-dispatch" || result["applied"] != false {
		t.Fatalf("code=%d out=%s diagnostic=%s", code, out, diagnostic)
	}
}

func TestReleaseDispatchExplicitHexTagsKeepSelectorMeaning(t *testing.T) {
	for _, tag := range []string{"deadbeef1", strings.Repeat("a", 39) + "1", strings.Repeat("a", 63) + "1"} {
		t.Run(tag, func(t *testing.T) {
			calls := 0
			code, out, diagnostic := runDispatchCLI(t, func(_ context.Context, o release.DispatchOptions) release.DispatchResult {
				calls++
				if o.Tag != tag || o.Commit != "" || o.Version != "" {
					t.Fatalf("explicit tag mode lost: %+v", o)
				}
				return release.DispatchResult{PlanResult: release.PlanResult{Status: "ok", Verdict: "pass"}}
			}, "--tag", tag)
			if code != 0 || calls != 1 {
				t.Fatalf("explicit tag rejected: code=%d calls=%d out=%q diagnostic=%q", code, calls, out, diagnostic)
			}
		})
	}
	if untrusted.ValidReleaseTag("deadbeef1") || untrusted.ValidReleaseTag(strings.Repeat("a", 40)) {
		t.Fatal("existing positional selector validation changed")
	}
}
