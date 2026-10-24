// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// Spec 031 T051: the candidate schemas of the restore plan and result and of
// the history view accept the results of the workspace package and refuse every
// contract violation.

func restoreFixtureResult(mode string) workspace.V3RestoreResult {
	reason := "symbolic-reference"
	oid := strings.Repeat("a", 40)
	entries := []workspace.V3RestoreEntry{
		{ActionID: "ref-1", Ref: "refs/heads/topic", OID: oid, RecoveryRef: "refs/stw/recovery/v3/plan1/" + strings.Repeat("c", 32), State: "restorable", Class: "patch-id", Flags: []string{"accepted-whitespace-differences"},
			Decision: &workspace.V3RestoreDecision{Reviewer: "rev", Kind: "ai", Decision: "integrated", Reason: "done"}},
		{ActionID: "ref-2", Ref: "refs/heads/other", OID: oid, RecoveryRef: "refs/stw/recovery/v3/plan1/" + strings.Repeat("d", 32), State: "name-taken", Reason: &reason, Class: "ancestor", Flags: []string{}},
	}
	origin := "origin"
	entries[0].BranchConfig = &workspace.V3RestoreBranchConfig{Status: "recorded", ReflogTip: oid, BranchOID: oid, Keys: []workspace.V3RestoreConfigKey{
		{Key: "branch.topic.remote", Remote: &origin, State: "reapply"}, {Key: "branch.topic.merge", State: "reapply"}, {Key: "branch.topic.description", State: "skip-not-recorded"}}}
	if mode == "apply" {
		entries[0].BranchConfig.Keys[0].Outcome, entries[0].BranchConfig.Keys[1].Outcome, entries[0].BranchConfig.Keys[2].Outcome = "applied", "failed", "skipped"
		entries[0].Outcome = "created"
		entries[0].RestoreJournal = &workspace.V3RestoreRecordRef{Intent: "i-0001", Outcome: "i-0002"}
		entries[1].Outcome = "symbolic-reference"
	}
	return workspace.V3RestoreResult{SchemaVersion: 1, Command: "tidy-restore", Status: "ok", PlanID: "plan1", Mode: mode, Entries: entries,
		Counts: workspace.V3RestoreCounts{Restorable: 1, NameTaken: 1}, Limits: []string{"tip-and-history-only", "no-config", "no-reflog", "no-worktrees"}, Diagnostics: []workspace.Diagnostic{}}
}

func asDocument(t *testing.T, v any) (raw []byte) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTidyV3RestoreSchemaAcceptsPlanAndApplyAndRefusesViolations(t *testing.T) {
	s := schemaAt(t, "v3", "tidy-restore")
	first := func(v map[string]any) map[string]any { return v["entries"].([]any)[0].(map[string]any) }
	for _, mode := range []string{"plan", "apply"} {
		raw := asDocument(t, restoreFixtureResult(mode))
		valid(t, s, raw)
		refused := restoreFixtureResult(mode)
		refused.Status, refused.Entries = "refused", []workspace.V3RestoreEntry{}
		refused.Diagnostics = []workspace.Diagnostic{{Code: "unsupported-mode", Message: "Only local-reference journals can be restored."}}
		valid(t, s, asDocument(t, refused))
	}
	mutations := map[string]func(map[string]any){
		"unknown top-level field":  func(v map[string]any) { v["extra"] = 1 },
		"schema version":           func(v map[string]any) { v["schema_version"] = 2 },
		"command":                  func(v map[string]any) { v["command"] = "tidy-apply" },
		"status outside the table": func(v map[string]any) { v["status"] = "done" },
		"mode outside the table":   func(v map[string]any) { v["mode"] = "list" },
		"limits changed":           func(v map[string]any) { v["limits"] = []any{"everything"} },
		"state outside the table":  func(v map[string]any) { first(v)["state"] = "restored" },
		"reason outside the table": func(v map[string]any) { first(v)["reason"] = "because" },
		"reason missing":           func(v map[string]any) { delete(first(v), "reason") },
		"decision missing":         func(v map[string]any) { delete(first(v), "decision") },
		"ref outside heads":        func(v map[string]any) { first(v)["ref"] = "refs/tags/x" },
		"short oid":                func(v map[string]any) { first(v)["oid"] = "abc" },
		"zero oid":                 func(v map[string]any) { first(v)["oid"] = strings.Repeat("0", 40) },
		"recovery ref elsewhere":   func(v map[string]any) { first(v)["recovery_ref"] = "refs/heads/x" },
		"decision kind":            func(v map[string]any) { first(v)["decision"].(map[string]any)["kind"] = "robot" },
		"config state outside the table": func(v map[string]any) {
			first(v)["branch_config"].(map[string]any)["keys"].([]any)[0].(map[string]any)["state"] = "force"
		},
		"config key outside branch": func(v map[string]any) {
			first(v)["branch_config"].(map[string]any)["keys"].([]any)[0].(map[string]any)["key"] = "remote.origin.url"
		},
		"config value field": func(v map[string]any) {
			first(v)["branch_config"].(map[string]any)["keys"].([]any)[0].(map[string]any)["value"] = "secret"
		},
		"config outcome outside the table": func(v map[string]any) {
			first(v)["branch_config"].(map[string]any)["keys"].([]any)[0].(map[string]any)["outcome"] = "forced"
		},
		"config reflog tip":      func(v map[string]any) { first(v)["branch_config"].(map[string]any)["reflog_tip"] = "later" },
		"unknown entry field":    func(v map[string]any) { first(v)["verdict"] = "ok" },
		"flag outside the table": func(v map[string]any) { first(v)["flags"] = []any{"loose"} },
		"outcome outside the table": func(v map[string]any) {
			first(v)["outcome"] = "overwritten"
		},
		"record reference field": func(v map[string]any) {
			first(v)["restore_journal"] = map[string]any{"path": "/tmp/x"}
		},
		"negative count": func(v map[string]any) { v["counts"].(map[string]any)["restorable"] = -1 },
		"count missing":  func(v map[string]any) { delete(v["counts"].(map[string]any), "name-taken") },
		"diagnostic without a message": func(v map[string]any) {
			v["diagnostics"] = []any{map[string]any{"code": "x"}}
		},
	}
	raw := asDocument(t, restoreFixtureResult("apply"))
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			v := value(t, raw).(map[string]any)
			mutate(v)
			if s.Validate(v) == nil {
				t.Fatal("the schema admitted a violation")
			}
		})
	}
}

func historyFixtureResult() workspace.V3HistoryResult {
	return workspace.V3HistoryResult{SchemaVersion: 1, Command: "tidy-history", Status: "ok", Total: 4096, ScanTruncated: true,
		Entries: []workspace.V3HistoryEntry{
			{PlanID: "p1", Time: "2026-10-01T12:00:00Z", Mode: "local-refs", Deleted: 3, Retained: 1, Restorable: 2, RecoveryRefs: "partial", DecisionKinds: workspace.V3HistoryDecisions{Human: 1, AI: 2}, JournalStatus: "complete",
				RunMode: &workspace.V3RunMode{Mode: "agent", StdinTTY: false, StdoutTTY: true, Markers: []string{"CLAUDECODE"}, Mine: true, ByOwner: []string{"codex"}, Caller: "claude", CallerSource: "env"}},
			{PlanID: "p2", Time: "2026-09-30T12:00:00.5Z", Mode: "tracking-refs", Deleted: 1, RecoveryRefs: "present", JournalStatus: "complete"},
			{PlanID: "p3", Time: "2026-09-29T12:00:00Z", Mode: "unknown", RecoveryRefs: "absent", JournalStatus: "unreadable"},
		},
		Truncated:   []workspace.V3HistoryTruncation{{Unit: "journals", Shown: 3, Total: 4096}},
		Diagnostics: []workspace.Diagnostic{}}
}

func TestTidyV3HistorySchemaAcceptsTheViewAndRefusesViolations(t *testing.T) {
	s := schemaAt(t, "v3", "tidy-history")
	raw := asDocument(t, historyFixtureResult())
	valid(t, s, raw)
	list := historyFixtureResult()
	list.Command = "tidy-restore"
	valid(t, s, asDocument(t, list))
	first := func(v map[string]any) map[string]any { return v["entries"].([]any)[0].(map[string]any) }
	runMode := func(e map[string]any) map[string]any { return e["run_mode"].(map[string]any) }
	mutations := map[string]func(map[string]any){
		"unknown top-level field":      func(v map[string]any) { v["extra"] = 1 },
		"command":                      func(v map[string]any) { v["command"] = "tidy-plan" },
		"scan flag missing":            func(v map[string]any) { delete(v, "scan_truncated") },
		"total over the scan bound":    func(v map[string]any) { v["total"] = 4097 },
		"truncation unit":              func(v map[string]any) { v["truncated"].([]any)[0].(map[string]any)["unit"] = "pages" },
		"truncation without shown":     func(v map[string]any) { delete(v["truncated"].([]any)[0].(map[string]any), "shown") },
		"journal status":               func(v map[string]any) { first(v)["journal_status"] = "lost" },
		"mode":                         func(v map[string]any) { first(v)["mode"] = "other" },
		"recovery presence":            func(v map[string]any) { first(v)["recovery_refs"] = "some" },
		"negative restorable":          func(v map[string]any) { first(v)["restorable"] = -1 },
		"unknown entry field":          func(v map[string]any) { first(v)["branches"] = []any{} },
		"decision kind field":          func(v map[string]any) { first(v)["decision_kinds"].(map[string]any)["robot"] = 1 },
		"time missing":                 func(v map[string]any) { delete(first(v), "time") },
		"time not a time":              func(v map[string]any) { first(v)["time"] = "yesterday" },
		"other mode with restorable":   func(v map[string]any) { v["entries"].([]any)[1].(map[string]any)["restorable"] = 1 },
		"unreadable with restorable":   func(v map[string]any) { v["entries"].([]any)[2].(map[string]any)["restorable"] = 1 },
		"unreadable with a known mode": func(v map[string]any) { v["entries"].([]any)[2].(map[string]any)["mode"] = "local-refs" },
		"plan id with a path":          func(v map[string]any) { first(v)["plan_id"] = "../x" },
		"run mode value":               func(v map[string]any) { runMode(first(v))["mode"] = "root" },
		"run mode field":               func(v map[string]any) { runMode(first(v))["token"] = "x" },
		"run mode missing field":       func(v map[string]any) { delete(runMode(first(v)), "caller") },
		"run mode markers type":        func(v map[string]any) { runMode(first(v))["markers"] = "CLAUDECODE" },
		"run mode too many markers":    func(v map[string]any) { runMode(first(v))["markers"] = make([]any, 33) },
		"run mode long caller":         func(v map[string]any) { runMode(first(v))["caller"] = strings.Repeat("a", 129) },
		"run mode null":                func(v map[string]any) { first(v)["run_mode"] = nil },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			v := value(t, raw).(map[string]any)
			mutate(v)
			if s.Validate(v) == nil {
				t.Fatal("the schema admitted a violation")
			}
		})
	}
}
