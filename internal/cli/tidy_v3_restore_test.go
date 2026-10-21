// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// Spec 031 T050: the restore, restore --list and history commands.

type restoreSpy struct {
	restoreCalls, historyCalls int
	restore                    workspace.V3RestoreOptions
	history                    workspace.V3HistoryOptions
	restoreResult              workspace.V3RestoreResult
	historyResult              workspace.V3HistoryResult
}

func (s *restoreSpy) run(args []string) (stdout string, err error) {
	cmd := tidyCommandWithRestoreOperations(nil, nil, nil, nil,
		func(_ context.Context, o workspace.V3RestoreOptions) workspace.V3RestoreResult {
			s.restoreCalls++
			s.restore = o
			return s.restoreResult
		},
		func(_ context.Context, o workspace.V3HistoryOptions) workspace.V3HistoryResult {
			s.historyCalls++
			s.history = o
			return s.historyResult
		})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), err
}

func okRestore() workspace.V3RestoreResult {
	reason := "symbolic-reference"
	return workspace.V3RestoreResult{SchemaVersion: 1, Command: "tidy-restore", Status: "ok", PlanID: "plan1", Mode: "plan",
		Entries: []workspace.V3RestoreEntry{
			{ActionID: "ref-1", Ref: "refs/heads/topic", OID: strings.Repeat("a", 40), RecoveryRef: "refs/stw/recovery/v3/plan1/x", State: "restorable", Class: "patch-id", Flags: []string{}, Decision: &workspace.V3RestoreDecision{Reviewer: "rev", Kind: "ai", Decision: "integrated", Reason: "done\nline two"}},
			{ActionID: "ref-2", Ref: "refs/heads/we\x1b[31mird", OID: strings.Repeat("b", 40), RecoveryRef: "refs/stw/recovery/v3/plan1/y", State: "name-taken", Reason: &reason, Class: "ancestor", Flags: []string{}},
		},
		Counts: workspace.V3RestoreCounts{Restorable: 1, NameTaken: 1}, Limits: []string{"tip-and-history-only"}, Diagnostics: []workspace.Diagnostic{}}
}

const stateArg = "/tmp/private-state"

func TestTidyRestoreArgumentsReachTheWorkspace(t *testing.T) {
	var s restoreSpy
	s.restoreResult = okRestore()
	out, err := s.run([]string{"restore", "/tmp/repo", "--contract", "3", "--plan", "plan1", "--state-dir", stateArg, "--action", "ref-1", "--action", "ref-2"})
	if err != nil || s.restoreCalls != 1 || s.historyCalls != 0 {
		t.Fatalf("err=%v calls=%d/%d", err, s.restoreCalls, s.historyCalls)
	}
	want := workspace.V3RestoreOptions{Path: "/tmp/repo", StateDir: stateArg, PlanID: "plan1", Actions: []string{"ref-1", "ref-2"}}
	if !reflect.DeepEqual(s.restore, want) || out == "" {
		t.Fatalf("%+v\n%s", s.restore, out)
	}
	if _, err = s.run([]string{"restore", "--contract", "3", "--plan", "plan1", "--state-dir", stateArg, "--action", "ref-1", "--as", "refs/heads/again", "--apply", "--authorize"}); err != nil {
		t.Fatal(err)
	}
	want = workspace.V3RestoreOptions{Path: ".", StateDir: stateArg, PlanID: "plan1", Actions: []string{"ref-1"}, As: "refs/heads/again", Apply: true, Authorize: true}
	if !reflect.DeepEqual(s.restore, want) {
		t.Fatalf("%+v", s.restore)
	}
}

func TestTidyListAndHistoryReachTheSameReader(t *testing.T) {
	var s restoreSpy
	s.historyResult = workspace.V3HistoryResult{SchemaVersion: 1, Command: "tidy-history", Status: "ok", Entries: []workspace.V3HistoryEntry{}, Truncated: []workspace.V3HistoryTruncation{}, Diagnostics: []workspace.Diagnostic{}}
	if _, err := s.run([]string{"history", "/tmp/repo", "--contract", "3", "--state-dir", stateArg, "--limit", "7"}); err != nil {
		t.Fatal(err)
	}
	if want := (workspace.V3HistoryOptions{Path: "/tmp/repo", StateDir: stateArg, Limit: 7}); s.history != want || s.restoreCalls != 0 {
		t.Fatalf("%+v", s.history)
	}
	if _, err := s.run([]string{"restore", "--contract", "3", "--list", "--state-dir", stateArg, "--limit", "1024"}); err != nil {
		t.Fatal(err)
	}
	if want := (workspace.V3HistoryOptions{Path: ".", StateDir: stateArg, Limit: 1024, List: true}); s.history != want || s.restoreCalls != 0 {
		t.Fatalf("%+v calls=%d", s.history, s.restoreCalls)
	}
}

func TestTidyRestoreUsageErrorsExitTwoBeforeAnyWork(t *testing.T) {
	base := []string{"restore", "--contract", "3", "--state-dir", stateArg}
	with := func(extra ...string) []string { return append(append([]string{}, base...), extra...) }
	cases := map[string][]string{
		"contract omitted":           {"restore", "--plan", "p", "--state-dir", stateArg},
		"contract two":               {"restore", "--contract", "2", "--plan", "p", "--state-dir", stateArg},
		"state dir omitted":          {"restore", "--contract", "3", "--plan", "p"},
		"relative state dir":         {"restore", "--contract", "3", "--plan", "p", "--state-dir", "state"},
		"non-canonical state dir":    {"restore", "--contract", "3", "--plan", "p", "--state-dir", "/tmp/a/../b"},
		"neither plan nor list":      with(),
		"empty plan":                 with("--plan", ""),
		"malformed plan":             with("--plan", "a b"),
		"plan repeated":              with("--plan", "p", "--plan", "q"),
		"duplicate action":           with("--plan", "p", "--action", "a", "--action", "a"),
		"malformed action":           with("--plan", "p", "--action", "a/b"),
		"as without an action":       with("--plan", "p", "--as", "refs/heads/x"),
		"as with two actions":        with("--plan", "p", "--action", "a", "--action", "b", "--as", "refs/heads/x"),
		"as empty":                   with("--plan", "p", "--action", "a", "--as", ""),
		"as outside heads":           with("--plan", "p", "--action", "a", "--as", "refs/tags/x"),
		"as short name":              with("--plan", "p", "--action", "a", "--as", "x"),
		"as with a leading dash":     with("--plan", "p", "--action", "a", "--as", "refs/heads/-x"),
		"as with dots":               with("--plan", "p", "--action", "a", "--as", "refs/heads/a..b"),
		"authorize without apply":    with("--plan", "p", "--authorize"),
		"apply without authorize":    with("--plan", "p", "--apply"),
		"list with plan":             with("--list", "--plan", "p"),
		"list with action":           with("--list", "--action", "a"),
		"list with apply":            with("--list", "--apply", "--authorize"),
		"list with as":               with("--list", "--as", "refs/heads/x"),
		"limit without list":         with("--plan", "p", "--limit", "5"),
		"limit zero":                 with("--list", "--limit", "0"),
		"limit negative":             with("--list", "--limit", "-1"),
		"limit over the ceiling":     with("--list", "--limit", "1025"),
		"limit not a number":         with("--list", "--limit", "many"),
		"limit repeated":             with("--list", "--limit", "1", "--limit", "2"),
		"json repeated":              with("--plan", "p", "--json", "--json"),
		"path relative":              {"restore", "repo", "--contract", "3", "--plan", "p", "--state-dir", stateArg},
		"two paths":                  {"restore", "/a", "/b", "--contract", "3", "--plan", "p", "--state-dir", stateArg},
		"history without contract":   {"history", "--state-dir", stateArg},
		"history with contract two":  {"history", "--contract", "2", "--state-dir", stateArg},
		"history without state dir":  {"history", "--contract", "3"},
		"history limit over":         {"history", "--contract", "3", "--state-dir", stateArg, "--limit", "1025"},
		"history limit zero":         {"history", "--contract", "3", "--state-dir", stateArg, "--limit", "0"},
		"history relative state dir": {"history", "--contract", "3", "--state-dir", "state"},
		"list relative state dir":    {"restore", "--contract", "3", "--list", "--state-dir", "state"},
		"history takes no plan":      {"history", "--contract", "3", "--state-dir", stateArg, "--plan", "p"},
		"history relative path":      {"history", "repo", "--contract", "3", "--state-dir", stateArg},
		"history state dir repeated": {"history", "--contract", "3", "--state-dir", stateArg, "--state-dir", stateArg},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var s restoreSpy
			_, err := s.run(args)
			var runtime runtimeFailure
			if err == nil || errors.As(err, &runtime) || s.restoreCalls+s.historyCalls != 0 {
				t.Fatalf("err=%v calls=%d/%d", err, s.restoreCalls, s.historyCalls)
			}
			if code := exitStatus(io.Discard, err); code != 2 {
				t.Fatalf("exit %d", code)
			}
		})
	}
}

func TestTidyRestoreExitCodesFollowTheStatus(t *testing.T) {
	for status, want := range map[string]int{"ok": 0, "refused": 1, "partial": 1, "error": 1} {
		var s restoreSpy
		s.restoreResult = okRestore()
		s.restoreResult.Status = status
		_, err := s.run([]string{"restore", "--contract", "3", "--plan", "p", "--state-dir", stateArg, "--json"})
		got := 0
		if err != nil {
			got = exitStatus(io.Discard, err)
		}
		if got != want {
			t.Errorf("%s: exit %d, want %d", status, got, want)
		}
	}
}

func TestTidyRestoreHumanAndJSONAgree(t *testing.T) {
	var s restoreSpy
	s.restoreResult = okRestore()
	jsonOut, err := s.run([]string{"restore", "--contract", "3", "--plan", "plan1", "--state-dir", stateArg, "--json"})
	if err != nil {
		t.Fatal(err)
	}
	var got workspace.V3RestoreResult
	if err := json.Unmarshal([]byte(jsonOut), &got); err != nil || !reflect.DeepEqual(got, s.restoreResult) {
		t.Fatalf("JSON: %v\n%s", err, jsonOut)
	}
	text, err := s.run([]string{"restore", "--contract", "3", "--plan", "plan1", "--state-dir", stateArg})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got.Entries {
		for _, want := range []string{"action=\"" + e.ActionID + "\"", "oid=\"" + e.OID + "\"", "state=\"" + e.State + "\"", "class=\"" + e.Class + "\""} {
			if !strings.Contains(text, want) {
				t.Errorf("text lacks %s:\n%s", want, text)
			}
		}
	}
	for _, want := range []string{"Counts: restorable=1 already-present=0 name-taken=1 recovery-missing=0 journal-incomplete=0", `reason="symbolic-reference"`, `Decision: reviewer="rev" kind="ai" decision="integrated"`, `Mode: "plan"`, `Plan: "plan1"`} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %s:\n%s", want, text)
		}
	}
	// Journal text is data: no control character or line break reaches the terminal.
	if strings.ContainsAny(text, "\x1b") || strings.Contains(text, "done\nline two") || strings.Contains(text, "we\x1b") {
		t.Errorf("unescaped journal text:\n%q", text)
	}
}

func TestTidyHistoryHumanAndJSONAgree(t *testing.T) {
	var s restoreSpy
	s.historyResult = workspace.V3HistoryResult{SchemaVersion: 1, Command: "tidy-history", Status: "ok", Total: 5000, ScanTruncated: true,
		Entries:     []workspace.V3HistoryEntry{{PlanID: "p1", Time: "2026-10-01T12:00:00Z", Mode: "local-refs", Deleted: 3, Retained: 1, Restorable: 2, RecoveryRefs: "partial", DecisionKinds: workspace.V3HistoryDecisions{Human: 1, AI: 2}, JournalStatus: "complete"}},
		Truncated:   []workspace.V3HistoryTruncation{{Unit: "journals", Shown: 1, Total: 4096}},
		Diagnostics: []workspace.Diagnostic{}}
	args := []string{"history", "--contract", "3", "--state-dir", stateArg}
	jsonOut, err := s.run(append(append([]string{}, args...), "--json"))
	if err != nil {
		t.Fatal(err)
	}
	var got workspace.V3HistoryResult
	if err := json.Unmarshal([]byte(jsonOut), &got); err != nil || !reflect.DeepEqual(got, s.historyResult) {
		t.Fatalf("JSON: %v\n%s", err, jsonOut)
	}
	text, err := s.run(args)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`Total: 5000 scan_truncated=true`, `Journal: plan="p1" time="2026-10-01T12:00:00Z" mode="local-refs" deleted=3 retained=1 restorable=2 recovery_refs="partial" decisions_human=1 decisions_ai=2 status="complete"`, `Truncated: unit="journals" shown=1 total=4096`} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %s:\n%s", want, text)
		}
	}
}

func TestTidyRestoreCommandsAreListedInHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), "dev", []string{"tidy", "--help"}, &out, &errOut); code != 0 {
		t.Fatalf("%d %s", code, errOut.String())
	}
	for _, want := range []string{"restore", "history"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help lacks %s:\n%s", want, out.String())
		}
	}
}
