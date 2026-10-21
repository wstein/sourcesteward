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

const (
	oidOld = "1111111111111111111111111111111111111111"
	oidNew = "2222222222222222222222222222222222222222"
)

func syncRepo(name, kind, result string) stwsync.Repo {
	return stwsync.Repo{Name: name, Path: "/w/" + name, Kind: kind, Branch: syncStr("main"), Remote: syncStr("origin"), Result: result, OldOID: syncStr(oidOld)}
}

func syncCLIResult(repos ...stwsync.Repo) stwsync.Result {
	r := stwsync.Result{SchemaVersion: 1, Command: "sync", Mode: "sync", Repos: repos, Summary: stwsync.Summary{ByResult: map[string]int{}}}
	for _, rp := range repos {
		r.Summary.ByResult[rp.Result]++
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
		{"a", "b"},                        // two paths
		{"--rebase", "--ff-only"},         // contradictory
		{"--rebase", "--no-rebase"},       // contradictory
		{"--autostash", "--no-autostash"}, // contradictory
		{"--prune", "--no-prune"},         // contradictory
		{"--remote", "origin"},            // a repository fetches its own upstream remote
		{"--submodules"},                  // submodules are always included
		{"--apply"},                       // a sync is the apply
		{"--bogus"},
		{"--json=maybe"},
		{"a\x00b"},
		{"\xff"},
		{"restore"}, // reserved for a later slice
	} {
		_, calls, out, err := runSync(syncCLIResult(), args...)
		var failure runtimeFailure
		if err == nil || errors.As(err, &failure) || calls != 0 || out != "" {
			t.Errorf("%q: err=%v calls=%d out=%q", args, err, calls, out)
		}
	}
}

func TestSyncWithoutFlagsIsTheAction(t *testing.T) {
	got, calls, out, err := runSync(syncCLIResult(syncRepo(".", "root", "up-to-date")))
	if err != nil || calls != 1 || out == "" || got.Plan || got.Path != "." || got.Rebase != stwsync.Unset || got.Autostash != stwsync.Unset || got.Prune != stwsync.Unset {
		t.Fatalf("%v %d %+v", err, calls, got)
	}
}

func TestSyncFlagsMapToOptions(t *testing.T) {
	for args, check := range map[string]func(stwsync.Options) bool{
		"--plan":         func(o stwsync.Options) bool { return o.Plan },
		"--dry-run":      func(o stwsync.Options) bool { return o.Plan },
		"--rebase":       func(o stwsync.Options) bool { return o.Rebase == stwsync.On },
		"--ff-only":      func(o stwsync.Options) bool { return o.Rebase == stwsync.Off },
		"--no-rebase":    func(o stwsync.Options) bool { return o.NoRebase },
		"--no-ff":        func(o stwsync.Options) bool { return o.NoFF },
		"--autostash":    func(o stwsync.Options) bool { return o.Autostash == stwsync.On },
		"--no-autostash": func(o stwsync.Options) bool { return o.Autostash == stwsync.Off },
		"--prune":        func(o stwsync.Options) bool { return o.Prune == stwsync.On },
		"--no-prune":     func(o stwsync.Options) bool { return o.Prune == stwsync.Off },
	} {
		got, calls, _, err := runSync(syncCLIResult(), args, "some path")
		if err != nil || calls != 1 || !check(got) || got.Path != "some path" {
			t.Errorf("%s: %v %+v", args, err, got)
		}
	}
}

func TestSyncExitStatusOnlyFailsOnFailures(t *testing.T) {
	var failure runtimeFailure
	for _, tc := range []struct {
		result   string
		wantFail bool
	}{
		{"up-to-date", false}, {"fast-forwarded", false}, {"rebased", false}, {"ahead", false},
		{"skipped-dirty", false}, {"diverged", false}, {"skipped-detached", false}, {"skipped-busy", false},
		{"failed", true}, {"conflict", true}, {"autostash-conflict", true},
	} {
		_, _, out, err := runSync(syncCLIResult(syncRepo(".", "root", tc.result)))
		if tc.wantFail != errors.As(err, &failure) || (!tc.wantFail && err != nil) || out == "" {
			t.Errorf("%s: err=%v", tc.result, err)
		}
	}
	refused := syncCLIResult()
	refused.Error = &stwsync.Failure{Code: "sync-unsupported-backend", Message: "jj-owned"}
	for _, args := range [][]string{{}, {"--json"}} {
		_, _, out, err := runSync(refused, args...)
		if !errors.As(err, &failure) || !strings.Contains(err.Error(), "sync-unsupported-backend") {
			t.Errorf("%q: %v", args, err)
		}
		if (len(args) == 1) != (out != "") {
			t.Errorf("%q: only --json prints the failed result: %q", args, out)
		}
	}
}

func TestSyncJSONIsOneObjectWithExplicitNulls(t *testing.T) {
	r := syncCLIResult(syncRepo(".", "root", "up-to-date"))
	_, _, out, _ := runSync(r, "--json")
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("not one line: %q", out)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "command", "mode", "run_id", "options", "repos", "summary", "error"} {
		if _, ok := v[key]; !ok {
			t.Errorf("missing key %s", key)
		}
	}
	item := v["repos"].([]any)[0].(map[string]any)
	for _, key := range []string{"name", "path", "kind", "branch", "remote", "result", "old_oid", "new_oid", "ahead", "behind", "pruned", "reason", "detail"} {
		if _, ok := item[key]; !ok {
			t.Errorf("repo lacks %s", key)
		}
	}
}

func TestSyncHumanOutputIsOneLinePerRepositoryAndReadable(t *testing.T) {
	ff := syncRepo("a", "submodule", "fast-forwarded")
	ff.NewOID, ff.Behind = syncStr(oidNew), 3
	rb := syncRepo("b", "submodule", "rebased")
	rb.NewOID, rb.Ahead, rb.Behind = syncStr(oidNew), 2, 5
	dv := syncRepo("c", "submodule", "diverged")
	dv.Ahead, dv.Behind, dv.Reason = 2, 5, syncStr("needs a choice: use --rebase or set pull.rebase")
	dt := syncRepo("d", "submodule", "skipped-dirty")
	dt.Reason = syncStr("local changes would be overwritten: x.txt")
	det := syncRepo("e", "submodule", "skipped-detached")
	det.Branch = nil
	cf := syncRepo("f", "submodule", "conflict")
	cf.Reason, cf.Detail = syncStr("rebase stopped on a conflict; run git rebase --continue"), syncStr("CONFLICT (content): x\nhint: git rebase --abort")
	fl := syncRepo("g", "submodule", "failed")
	fl.Reason = syncStr("git fetch failed")
	r := syncCLIResult(syncRepo(".", "root", "up-to-date"), ff, rb, dv, dt, det, cf, fl)
	r.RunID = syncStr("20261021T101500Z-0a1b2c3d")
	_, _, out, err := runSync(r)
	var failure runtimeFailure
	if !errors.As(err, &failure) {
		t.Fatalf("failed repos exit 1: %v", err)
	}
	for _, want := range []string{
		"up to date",
		"fast-forwarded 3 commits 1111111..2222222",
		"rebased 2 local commits onto 5 new 1111111..2222222",
		"skipped: diverged (2 ahead, 5 behind)",
		"skipped: dirty",
		"skipped: detached HEAD",
		"conflict: rebase stopped",
		"    CONFLICT (content): x",
		"failed: git fetch failed",
		"8 repositories",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.ContainsAny(out, "\x1b") {
		t.Error("colour only on a terminal")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 8+1+2+1+2 { // header, repos, detail, summary
		t.Errorf("too many lines (%d):\n%s", len(lines), out)
	}
	if strings.Contains(out, "refs/heads") || strings.Contains(out, "worktree") {
		t.Errorf("no branch or worktree lists:\n%s", out)
	}
}

func TestSyncPlanHumanOutputSaysWouldAndChangedNothing(t *testing.T) {
	ff := syncRepo(".", "root", "fast-forward")
	ff.NewOID, ff.Behind = syncStr(oidNew), 1
	r := syncCLIResult(ff)
	r.Mode = "plan"
	_, _, out, _ := runSync(r, "--plan")
	if !strings.Contains(out, "would fast-forward 1 commit 1111111..2222222") || !strings.Contains(strings.ToLower(out), "plan") {
		t.Fatalf("%s", out)
	}
}

func TestSyncHumanOutputIsEscaped(t *testing.T) {
	rp := syncRepo("evil\x1b[31m\nname", "submodule", "failed")
	rp.Reason = syncStr("why\r\n\x1b]0;x\x07")
	_, _, out, _ := runSync(syncCLIResult(rp))
	if strings.ContainsAny(out, "\x1b\r\x07") || strings.Count(out, "\n") > 4 {
		t.Fatalf("unsafe: %q", out)
	}
}

func TestSyncWriterFailureIsARuntimeFailure(t *testing.T) {
	for _, args := range [][]string{{}, {"--json"}} {
		cmd := syncCommandWith(func(context.Context, stwsync.Options) stwsync.Result {
			return syncCLIResult(syncRepo(".", "root", "up-to-date"))
		})
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
	for _, want := range []string{"--plan", "--dry-run", "--rebase", "--ff-only", "--autostash", "--prune", "--json", "Exit statuses", "git pull"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help lacks %s", want)
		}
	}
}

func TestSyncColourOnlyWhenRequestedAndNeverFromRepositoryText(t *testing.T) {
	ff := syncRepo("a\x1b[31m", "submodule", "fast-forwarded")
	ff.NewOID, ff.Behind = syncStr(oidNew), 1
	r := syncCLIResult(ff)
	var plain, colored bytes.Buffer
	if err := renderSyncText(&plain, r, false); err != nil || strings.Contains(plain.String(), "\x1b") {
		t.Fatalf("%v %q", err, plain.String())
	}
	if err := renderSyncText(&colored, r, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(colored.String(), "\x1b[32mfast-forwarded") || strings.Contains(colored.String(), "\x1b[31m") {
		t.Fatalf("%q", colored.String())
	}
}
