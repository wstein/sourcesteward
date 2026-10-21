// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/stwsync"
)

func syncStr(s string) *string { return &s }

func syncCLIResult(results ...string) stwsync.Result {
	r := stwsync.Result{
		SchemaVersion: 1, Command: "sync", Mode: "plan", Remote: "origin",
		Fetch: stwsync.Fetch{Performed: true, PrunedRefs: []string{"refs/remotes/origin/old"}, PrunedTotal: 1},
		Items: []stwsync.Item{}, Submodules: []stwsync.Submodule{}, Summary: stwsync.Summary{ByResult: map[string]int{}},
	}
	for i, result := range results {
		r.Items = append(r.Items, stwsync.Item{Branch: "b" + string(rune('0'+i)), Result: result, OldOID: syncStr("1111111111111111111111111111111111111111"), NewOID: syncStr("2222222222222222222222222222222222222222")})
		r.Summary.ByResult[result]++
		r.Summary.Total++
	}
	return r
}

func runSync(result stwsync.Result, args ...string) (stwsync.Options, int, string, error) {
	var got stwsync.Options
	calls := 0
	cmd := syncCommandWith(func(_ context.Context, o stwsync.Options) stwsync.Result {
		calls++
		got = o
		return result
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return got, calls, out.String(), err
}

func TestSyncUsageErrorsExitTwoBeforeTheOperation(t *testing.T) {
	for _, args := range [][]string{
		{},                            // a real run is a later slice
		{"--json"},                    // still no plan
		{"--plan", "a", "b"},          // two paths
		{"--plan", "--remote", ""},    // empty remote
		{"--plan", "--remote", "-x"},  // option-like remote
		{"--plan", "--remote", "a b"}, // not a remote name
		{"--plan", "--rebase"},        // later slices
		{"--plan", "--autostash"},     // later slices
		{"--plan", "--submodules"},    // later slices
		{"--plan", "--bogus"},         // unknown flag
		{"--plan", "--json=maybe"},    // invalid flag syntax
		{"--plan", "a\x00b"},          // NUL in the path
		{"--plan", "\xff"},            // invalid UTF-8
		{"--plan", "--dry-run=maybe"}, // invalid flag syntax
		{"restore"},                   // reserved for a later slice, must not run a plan
	} {
		_, calls, out, err := runSync(syncCLIResult(), args...)
		var failure runtimeFailure
		if err == nil || errors.As(err, &failure) || calls != 0 || out != "" {
			t.Errorf("%q: err=%v calls=%d out=%q", args, err, calls, out)
		}
	}
}

func TestSyncPlanAndDryRunAreAliases(t *testing.T) {
	for _, flag := range []string{"--plan", "--dry-run"} {
		got, calls, out, err := runSync(syncCLIResult("up-to-date"), flag, "--remote", "upstream", "--prune", "some path")
		if err != nil || calls != 1 || out == "" {
			t.Fatalf("%s: %v %d", flag, err, calls)
		}
		if got.Path != "some path" || got.Remote != "upstream" || !got.Prune {
			t.Fatalf("%s: %+v", flag, got)
		}
	}
	got, _, _, _ := runSync(syncCLIResult(), "--plan")
	if got.Path != "." || got.Remote != "" || got.Prune {
		t.Fatalf("defaults: %+v", got)
	}
}

func TestSyncExitStatusFollowsTheResult(t *testing.T) {
	var failure runtimeFailure
	for results, wantFail := range map[string]bool{
		"fast-forward,up-to-date,ahead,upstream-gone": false,
		"fast-forward,skipped-dirty":                  true,
		"diverged":                                    true,
	} {
		_, _, out, err := runSync(syncCLIResult(strings.Split(results, ",")...), "--plan")
		if wantFail != errors.As(err, &failure) || (!wantFail && err != nil) || out == "" {
			t.Errorf("%s: err=%v", results, err)
		}
	}
	refused := syncCLIResult()
	refused.Error = &stwsync.Failure{Code: "sync-unsupported-backend", Message: "jj-owned"}
	for _, args := range [][]string{{"--plan"}, {"--plan", "--json"}} {
		_, _, out, err := runSync(refused, args...)
		if !errors.As(err, &failure) || !strings.Contains(err.Error(), "sync-unsupported-backend") {
			t.Errorf("%q: %v", args, err)
		}
		if (len(args) == 2) != (out != "") {
			t.Errorf("%q: only --json prints the failed result: %q", args, out)
		}
	}
}

func TestSyncJSONIsOneObjectWithExplicitNulls(t *testing.T) {
	_, _, out, _ := runSync(syncCLIResult("up-to-date"), "--plan", "--json")
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("not one line: %q", out)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "command", "mode", "run_id", "remote", "options", "fetch", "items", "submodules", "summary", "error"} {
		if _, ok := v[key]; !ok {
			t.Errorf("missing key %s", key)
		}
	}
	if v["run_id"] != nil || v["error"] != nil || v["mode"] != "plan" || v["command"] != "sync" {
		t.Fatalf("%v", v)
	}
	item := v["items"].([]any)[0].(map[string]any)
	for _, key := range []string{"branch", "worktree", "result", "old_oid", "new_oid", "reason"} {
		if _, ok := item[key]; !ok {
			t.Errorf("item lacks %s", key)
		}
	}
}

func TestSyncHumanOutputIsEscapedAndBounded(t *testing.T) {
	r := syncCLIResult("fast-forward")
	r.Items[0].Branch = "evil\x1b[31m\nbranch"
	r.Items[0].Worktree = syncStr("/w/‮")
	r.Items[0].Reason = syncStr("why\r\n")
	_, _, out, _ := runSync(r, "--plan")
	if strings.ContainsAny(out, "\x1b\r‮") || strings.Count(out, "\n") > 8 {
		t.Fatalf("unsafe human output: %q", out)
	}
	for _, want := range []string{"fast-forward", "1111111..2222222", "pruned"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	long := syncCLIResult()
	for i := 0; i < 1024; i++ {
		long.Items = append(long.Items, stwsync.Item{Branch: strings.Repeat("x", 600), Result: "up-to-date"})
	}
	_, _, out, _ = runSync(long, "--plan")
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 520 {
			t.Fatalf("line of %d bytes", len(line))
		}
	}
}

func TestSyncWriterFailureIsARuntimeFailure(t *testing.T) {
	for _, args := range [][]string{{"--plan"}, {"--plan", "--json"}} {
		cmd := syncCommandWith(func(context.Context, stwsync.Options) stwsync.Result { return syncCLIResult("up-to-date") })
		cmd.SetOut(&failOnWrite{fail: 1})
		cmd.SetArgs(args)
		var failure runtimeFailure
		if err := cmd.Execute(); !errors.As(err, &failure) {
			t.Errorf("%q: %v", args, err)
		}
	}
}

func TestSyncIsRegisteredAndDocumentedInHelp(t *testing.T) {
	var out, diagnostic bytes.Buffer
	if code := Run(context.Background(), "dev", []string{"sync", "--help"}, &out, &diagnostic); code != 0 {
		t.Fatalf("%d %s", code, diagnostic.String())
	}
	for _, want := range []string{"--plan", "--dry-run", "--remote", "--prune", "--json", "Exit statuses"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help lacks %s", want)
		}
	}
}
