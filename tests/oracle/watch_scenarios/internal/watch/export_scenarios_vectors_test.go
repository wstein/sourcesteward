// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/watchexport"
)

type vRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	Text       string `json:"text"`
}

type vClassifyIn struct {
	Runs          []vRun   `json:"runs"`
	Statuses      []xStat  `json:"statuses"`
	Base          string   `json:"base"`
	DefaultBranch string   `json:"default_branch"`
	Patterns      []string `json:"stack_patterns"`
}

type vClassifyOut struct {
	Class        string   `json:"class"`
	Total        int      `json:"total"`
	Failing      []string `json:"failing"`
	FailingTotal int      `json:"failing_total"`
	Pending      []string `json:"pending"`
	Shape        string   `json:"shape"`
}

func vClassify(in vClassifyIn) vClassifyOut {
	var runs []CheckRun
	for _, r := range in.Runs {
		runs = append(runs, CheckRun{Name: r.Name, Status: r.Status, Conclusion: r.Conclusion, Title: r.Title, Summary: r.Summary, Text: r.Text})
	}
	var stats []StatusContext
	for _, s := range in.Statuses {
		stats = append(stats, StatusContext{Context: s.Context, State: s.State, Description: s.Description})
	}
	res := ClassifyChecksWith(runs, stats, in.Base, in.DefaultBranch, in.Patterns)
	return vClassifyOut{Class: res.Class, Total: res.Total, Failing: res.Failing, FailingTotal: res.FailingTotal, Pending: res.Pending, Shape: res.Shape}
}

func vin(runs []vRun, stats []xStat, base, deflt string, patterns []string) vClassifyIn {
	if runs == nil {
		runs = []vRun{}
	}
	if stats == nil {
		stats = []xStat{}
	}
	return vClassifyIn{Runs: runs, Statuses: stats, Base: base, DefaultBranch: deflt, Patterns: nonNil(patterns)}
}

func exportCoreVectors(t *testing.T, dir string) {
	write := func(v *watchexport.VectorDoc) {
		if err := watchexport.Write(dir, v.Name, v); err != nil {
			t.Fatal(err)
		}
	}
	// ---- checks classification ----
	{
		v := watchexport.NewVectors("checks-classify-classes", "classify_checks", "The check class and the total of a head from its check runs and commit statuses (none, green, pending, red; skipped and neutral count green; a failure wins over pending).", nil,
			"checks_test.go:TestClassifyChecksClasses")
		run := func(name, status, conclusion string) vRun {
			return vRun{Name: name, Status: status, Conclusion: conclusion}
		}
		type c struct {
			name  string
			runs  []vRun
			stats []xStat
		}
		for _, k := range []c{
			{"none", nil, nil},
			{"green", []vRun{run("a", "completed", "success")}, nil},
			{"skipped and neutral are green", []vRun{run("a", "completed", "skipped"), run("b", "completed", "neutral")}, nil},
			{"pending run", []vRun{run("a", "in_progress", ""), run("b", "completed", "success")}, nil},
			{"queued run", []vRun{run("a", "queued", "")}, nil},
			{"pending status", nil, []xStat{{Context: "ci", State: "pending"}}},
			{"failure wins over pending", []vRun{run("a", "in_progress", ""), run("b", "completed", "failure")}, nil},
			{"cancelled", []vRun{run("a", "completed", "cancelled")}, nil},
			{"timed_out", []vRun{run("a", "completed", "timed_out")}, nil},
			{"action_required", []vRun{run("a", "completed", "action_required")}, nil},
			{"startup_failure", []vRun{run("a", "completed", "startup_failure")}, nil},
			{"status failure", nil, []xStat{{Context: "ci", State: "failure"}}},
			{"status error", nil, []xStat{{Context: "ci", State: "error"}}},
			{"status success", nil, []xStat{{Context: "ci", State: "success"}}},
			{"completed run with an unknown conclusion counts green", []vRun{run("a", "completed", "stale")}, nil},
			{"failing status and pending run: red", []vRun{run("a", "queued", "")}, []xStat{{Context: "s", State: "failure"}}},
		} {
			in := vin(k.runs, k.stats, "main", "main", nil)
			v.Add(k.name, in, vClassify(in))
		}
		write(v)
	}
	{
		v := watchexport.NewVectors("checks-classify-failing-names", "classify_checks", "The failing names are sorted, at most 20, and counted in full.", nil,
			"checks_test.go:TestClassifyFailingNamesAreSortedBoundedAndCounted")
		var runs []vRun
		for i := 29; i >= 0; i-- {
			runs = append(runs, vRun{Name: fmt.Sprintf("job-%02d", i), Status: "completed", Conclusion: "failure"})
		}
		in := vin(runs, nil, "main", "main", nil)
		v.Add("30 failing runs given in descending order", in, vClassify(in))
		var pend []vRun
		for i := 24; i >= 0; i-- {
			pend = append(pend, vRun{Name: fmt.Sprintf("p-%02d", i), Status: "queued"})
		}
		in = vin(pend, nil, "main", "main", nil)
		v.Add("25 pending runs: the pending names are sorted and bounded too", in, vClassify(in))
		write(v)
	}
	{
		v := watchexport.NewVectors("checks-shape-rules", "classify_checks", "The failure shape of a red head: real, stack-merge-shape or unknown, by the ordered rules (real markers first, stack patterns only on a stacked base and only if every failure is a plain failure with a pattern).", nil,
			"checks_test.go:TestShapeRules")
		fail := func(name, text string) vRun {
			return vRun{Name: name, Status: "completed", Conclusion: "failure", Summary: text}
		}
		stack := "Unexpected synthetic merge parents"
		target := "PR must target repository main"
		type c struct {
			name        string
			runs        []vRun
			stats       []xStat
			base, deflt string
			patterns    []string
		}
		for _, k := range []c{
			{"stack on stacked base", []vRun{fail("a", stack)}, nil, "feature", "main", nil},
			{"stack text on default branch is unknown", []vRun{fail("a", stack)}, nil, "main", "main", nil},
			{"both patterns", []vRun{fail("a", stack), fail("b", target)}, nil, "feature", "main", nil},
			{"mixed shape and other is unknown", []vRun{fail("a", stack), fail("b", "runner shutdown")}, nil, "feature", "main", nil},
			{"real wins over stack", []vRun{fail("a", stack), fail("b", "--- FAIL: TestX (0.00s)")}, nil, "feature", "main", nil},
			{"real FAIL line", []vRun{fail("a", "ok\nFAIL\tpkg 0.1s")}, nil, "main", "main", nil},
			{"real panic", []vRun{fail("a", "panic: boom")}, nil, "main", "main", nil},
			{"real Error", []vRun{fail("a", "Error: nope")}, nil, "main", "main", nil},
			{"real _test.go line", []vRun{fail("a", "x_test.go:42: bad")}, nil, "main", "main", nil},
			{"_test.go without digits is not real", []vRun{fail("a", "x_test.go: bad")}, nil, "main", "main", nil},
			{"FAIL inside a word is not real", []vRun{fail("a", "FAILURE reported")}, nil, "main", "main", nil},
			{"empty text is unknown", []vRun{fail("a", "")}, nil, "feature", "main", nil},
			{"name alone never qualifies", []vRun{fail(stack, "")}, nil, "feature", "main", nil},
			{"cancelled is unknown even with text", []vRun{{Name: "a", Status: "completed", Conclusion: "cancelled", Summary: stack}}, nil, "feature", "main", nil},
			{"extra pattern", []vRun{fail("a", "custom marker")}, nil, "feature", "main", []string{"custom marker"}},
			{"patterns are case sensitive", []vRun{fail("a", "unexpected synthetic merge parents")}, nil, "feature", "main", nil},
			{"status failure with description", nil, []xStat{{Context: "ci", State: "failure", Description: target}}, "feature", "main", nil},
			{"status error is unknown", nil, []xStat{{Context: "ci", State: "error", Description: target}}, "feature", "main", nil},
			{"unknown default branch is unknown", []vRun{fail("a", stack)}, nil, "feature", "", nil},
			{"stack pattern in the title", []vRun{{Name: "a", Status: "completed", Conclusion: "failure", Title: stack}}, nil, "feature", "main", nil},
			{"stack pattern in the text field", []vRun{{Name: "a", Status: "completed", Conclusion: "failure", Text: stack}}, nil, "feature", "main", nil},
			{"a pending run next to a stack failure does not matter", []vRun{fail("a", stack), {Name: "p", Status: "queued"}}, nil, "feature", "main", nil},
		} {
			in := vin(k.runs, k.stats, k.base, k.deflt, k.patterns)
			v.Add(k.name, in, vClassify(in))
		}
		write(v)
	}
	{
		v := watchexport.NewVectors("checks-shape-text-bound", "classify_checks", "Only the first 4 KiB of the summary and of the text count for the shape.", nil,
			"checks_test.go:TestShapeReadsOnlyFirst4KiBOfSummaryAndText")
		long := strings.Repeat("x", 4096) + "--- FAIL: TestLate"
		in := vin([]vRun{{Name: "a", Status: "completed", Conclusion: "failure", Summary: long}}, nil, "main", "main", nil)
		v.Add("marker after 4096 bytes of summary", in, vClassify(in))
		in = vin([]vRun{{Name: "a", Status: "completed", Conclusion: "failure", Text: long}}, nil, "main", "main", nil)
		v.Add("marker after 4096 bytes of text", in, vClassify(in))
		in = vin([]vRun{{Name: "a", Status: "completed", Conclusion: "failure", Summary: strings.Repeat("x", 4000) + "--- FAIL: TestEarly"}}, nil, "main", "main", nil)
		v.Add("marker inside the first 4096 bytes", in, vClassify(in))
		write(v)
	}

	// ---- backoff ----
	{
		v := watchexport.NewVectors("backoff-doubles-to-the-cap", "backoff_seconds", "The wait after the nth consecutive failure of a repository: the interval, doubled with each failure, capped at the smaller of 10 minutes and 10 times the interval.", nil,
			"health_test.go:TestBackoffDoublesToTheCap")
		for _, iv := range []int{60, 10, 120, 600} {
			for f := 1; f <= 9; f++ {
				c := &cycle{w: &Watcher{o: Options{Interval: time.Duration(iv) * time.Second}}}
				v.Add(fmt.Sprintf("interval %ds, failure %d", iv, f), map[string]int{"interval_seconds": iv, "failures": f}, map[string]int{"seconds": int(c.backoff(f) / time.Second)})
			}
		}
		write(v)
	}
	{
		v := watchexport.NewVectors("status-freshness-window", "status_freshness_seconds", "How old the heartbeat may be for `--status` to report fresh: the larger of 3 intervals and 3 minutes.", nil,
			"status_test.go:TestStatusFreshnessWindow")
		for _, iv := range []int{10, 60, 61, 120, 600} {
			v.Add(fmt.Sprintf("interval %ds", iv), map[string]int{"interval_seconds": iv}, map[string]int{"seconds": int(StatusFreshness(time.Duration(iv)*time.Second) / time.Second)})
		}
		write(v)
	}

	// ---- format dependent: ids, counters, state directory ----
	{
		v := watchexport.NewVectors("event-id-keys", "event_id", "How Go builds the event id (repo#pr:kind:key and the key of every kind). Format-dependent: a Rust port may use other ids as long as they are unique and stable per transition; the scenarios assert uniqueness, not these strings.",
			[]string{"expected"}, "event_test.go:TestEventIDs")
		pr41 := 41
		type in struct {
			Fn   string         `json:"fn"`
			Args map[string]any `json:"args"`
		}
		a := func(m map[string]any) map[string]any { return m }
		c := sha("cc")
		d := sha("dd")
		add := func(name, fn string, args map[string]any, got string) {
			v.Add(name, in{fn, args}, map[string]string{"id": got})
		}
		add("new-pr", "event_id", a(map[string]any{"repo": "o/r", "pr": 41, "kind": KindNewPR, "key": "g0"}), eventID("o/r", &pr41, KindNewPR, "g0"))
		add("watch-recovered without pr", "event_id", a(map[string]any{"repo": "o/r", "pr": nil, "kind": KindWatchRecovered, "key": "e1"}), eventID("o/r", nil, KindWatchRecovered, "e1"))
		add("run-level watch-error", "event_id", a(map[string]any{"repo": "", "pr": nil, "kind": KindWatchError, "key": "rate-limited:e1:f1"}), eventID("", nil, KindWatchError, "rate-limited:e1:f1"))
		add("repos-changed", "event_id", a(map[string]any{"repo": "", "pr": nil, "kind": KindReposChanged, "key": "p3"}), eventID("", nil, KindReposChanged, "p3"))
		add("key head", "key_head", a(map[string]any{"head": d, "epoch": 4}), keyHead(d, 4))
		add("key checks", "key_checks", a(map[string]any{"head": c, "epoch": 3}), keyChecks(c, 3))
		add("key timeout", "key_timeout", a(map[string]any{"head": c, "epoch": 3}), keyTimeout(c, 3))
		add("key mergeable", "key_mergeable", a(map[string]any{"head": c, "epoch": 2}), keyMergeable(c, 2))
		add("key ready", "key_ready", a(map[string]any{"epoch": 5}), keyReady(5))
		add("key base", "key_base", a(map[string]any{"epoch": 6}), keyBase(6))
		add("key generation", "key_generation", a(map[string]any{"generation": 1}), keyGeneration(1))
		add("key merged", "key_merged", a(map[string]any{"merge_commit": d}), keyMerged(&d))
		add("key merged unknown", "key_merged", a(map[string]any{"merge_commit": nil}), keyMerged(nil))
		add("key restack", "key_restack", a(map[string]any{"merged_pr": 41, "merge_commit": d}), keyRestack(41, &d))
		add("key restack unknown", "key_restack", a(map[string]any{"merged_pr": 41, "merge_commit": nil}), keyRestack(41, nil))
		add("key repos", "key_repos", a(map[string]any{"epoch": 2}), keyRepos(2))
		add("key error", "key_error", a(map[string]any{"code": "network", "episode": 2, "failures": 10}), keyError("network", 2, 10))
		add("key recovered", "key_recovered_error", a(map[string]any{"episode": 2}), keyRecoveredError(2))
		add("key stall", "key_stall", a(map[string]any{"episode": 3}), keyStall(3))
		write(v)
	}
	{
		v := watchexport.NewVectors("event-id-counter", "id_counter", "How Go reads a counter (epoch, episode, generation) back from an event id when it rebuilds a lost state: the colon-separated part made of one letter and 1 to 8 digits, never a hex SHA. Format-dependent.",
			[]string{"input", "expected"}, "event_test.go:TestParseIDCounters")
		sha1 := "3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182"
		type in struct {
			ID     string `json:"id"`
			Letter string `json:"letter"`
		}
		for _, k := range []struct {
			id string
			l  byte
		}{
			{"o/r#4:head-changed:" + sha1 + ":h12", 'h'},
			{"o/r#4:checks-pending-timeout:" + sha1 + ":c7:t", 'c'},
			{"-:watch-error:rate-limited:e3:f10", 'e'},
			{"o/r#4:new-pr:g0", 'h'},
			{"o/r#4:head-changed:" + sha1 + ":h12", 'c'},
			{"o/r#4:checks-green:" + strings.Repeat("c1", 20) + ":c2", 'c'},
			{"o/r#4:head-changed:" + sha1 + ":h123456789", 'h'},
			{"o/r#4:head-changed:" + sha1 + ":h12345678", 'h'},
			{"o/r#4:head-changed:" + sha1 + ":hx", 'h'},
			{"o/r#4:new-pr:g12", 'g'},
		} {
			n, ok := idCounter(k.id, k.l)
			v.Add(strings.ReplaceAll(k.id, sha1, "SHA")+" letter "+string(k.l), in{k.id, string(k.l)}, map[string]any{"found": ok, "counter": n})
		}
		write(v)
	}
	{
		v := watchexport.NewVectors("workspace-id", "workspace_id", "The state directory id: the first 16 hex digits of SHA-256 over host and workspace root or the sorted, de-duplicated, lowercased repository list. Format-dependent (F3: a Rust port may use another id).",
			[]string{"expected"}, "statedir_test.go:TestWorkspaceID")
		type in struct {
			Host          string   `json:"host"`
			WorkspaceRoot string   `json:"workspace_root"`
			Repos         []string `json:"repos"`
		}
		for _, k := range []in{
			{"github.com", "", []string{"C/D", "a/b", "A/B"}},
			{"github.com", "/w/root", []string{"a/b"}},
			{"github.com", "", []string{"a/b"}},
			{"ghe.example", "", []string{"a/b"}},
			{"github.com", "", []string{"a/b", "c/d"}},
			{"github.com", "", []string{"c/d", "a/b"}},
		} {
			v.Add(fmt.Sprintf("%s %q %v", k.Host, k.WorkspaceRoot, k.Repos), k, map[string]string{"id": WorkspaceID(k.Host, k.WorkspaceRoot, k.Repos)})
		}
		write(v)
	}
	{
		v := watchexport.NewVectors("default-state-dir", "default_state_dir", "The default state directory from the environment. Format-dependent (F3).", []string{"expected"}, "statedir_test.go:TestDefaultStateDir")
		type in struct {
			XDGStateHome string `json:"xdg_state_home"`
			Home         string `json:"home"`
			ID           string `json:"id"`
		}
		for _, k := range []in{{"/x/state", "/h", "abc"}, {"", "/h", "abc"}} {
			old, had := os.LookupEnv("XDG_STATE_HOME")
			oldHome := os.Getenv("HOME")
			os.Setenv("XDG_STATE_HOME", k.XDGStateHome)
			os.Setenv("HOME", k.Home)
			got := DefaultStateDir(k.ID)
			if had {
				os.Setenv("XDG_STATE_HOME", old)
			} else {
				os.Unsetenv("XDG_STATE_HOME")
			}
			os.Setenv("HOME", oldHome)
			v.Add(fmt.Sprintf("XDG_STATE_HOME=%q HOME=%q", k.XDGStateHome, k.Home), k, map[string]string{"path": got})
		}
		write(v)
	}
	exportEventJSON(t, dir)
	exportRestackVectors(t, dir)
}

// ---- event line shapes ----

func exportEventJSON(t *testing.T, dir string) {
	v := watchexport.NewVectors("event-json-shapes", "event_json", "The JSON object of an event as Go writes it (every kind, no key omitted, lists never null) and the objects Go refuses to decode (unknown key, unknown kind, wrong schema version). The lines are format-dependent in key order and escaping; the key sets per kind and the refusals are the contract.",
		[]string{"line"}, "event_test.go:TestEveryKindHasExactlyTheContractKeysAndNeverOmitsOne", "event_test.go:TestEventJSONRoundTripAndStrictDetails")
	for _, e := range sampleEvents() {
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		var back Event
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatal(err)
		}
		var top map[string]json.RawMessage
		json.Unmarshal(raw, &top)
		var d map[string]json.RawMessage
		json.Unmarshal(top["details"], &d)
		keys := []string{}
		for k := range d {
			keys = append(keys, k)
		}
		sortStrings(keys)
		v.Add("encode "+e.Kind, map[string]any{"op": "encode", "event": json.RawMessage(raw)}, map[string]any{"accepted": true, "detail_keys": keys, "line": string(raw)})
	}
	for name, bad := range map[string]string{
		"unknown detail key":   `{"schema_version":1,"seq":1,"id":"x","time":"t","repo":null,"pr":null,"kind":"head-changed","details":{"old":"a","new":"b","extra":1}}`,
		"unknown kind":         `{"schema_version":1,"seq":1,"id":"x","time":"t","repo":null,"pr":null,"kind":"nope","details":{}}`,
		"schema version 2":     `{"schema_version":2,"seq":1,"id":"x","time":"t","repo":null,"pr":null,"kind":"closed","details":{"head":"a","base":"b"}}`,
		"missing detail key":   `{"schema_version":1,"seq":1,"id":"x","time":"t","repo":null,"pr":null,"kind":"closed","details":{"head":"a"}}`,
		"trailing data":        `{"schema_version":1,"seq":1,"id":"x","time":"t","repo":null,"pr":null,"kind":"closed","details":{"head":"a","base":"b"}} {}`,
		"reserved review kind": `{"schema_version":1,"seq":1,"id":"x","time":"t","repo":null,"pr":null,"kind":"review-state-changed","details":{}}`,
		"unknown envelope key": `{"schema_version":1,"seq":1,"id":"x","time":"t","repo":null,"pr":null,"kind":"closed","details":{"head":"a","base":"b"},"extra":1}`,
	} {
		var e Event
		err := e.UnmarshalJSON([]byte(bad))
		v.Add("decode "+name, map[string]any{"op": "decode", "line": bad}, map[string]any{"accepted": err == nil})
	}
	// keep the case order stable: Go map iteration is random
	sortCases(v)
	if err := watchexport.Write(dir, v.Name, v); err != nil {
		t.Fatal(err)
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

func sortCases(v *watchexport.VectorDoc) {
	for i := 1; i < len(v.Cases); i++ {
		for j := i; j > 0 && v.Cases[j-1].Name > v.Cases[j].Name; j-- {
			v.Cases[j-1], v.Cases[j] = v.Cases[j], v.Cases[j-1]
		}
	}
}

// ---- restack candidates ----

type vOpen struct {
	Number  int    `json:"number"`
	Base    string `json:"base"`
	HeadRef string `json:"head_ref"`
	Head    string `json:"head"`
	Fork    bool   `json:"fork"`
}

type vDue struct {
	MergedPR    int     `json:"merged_pr"`
	MergeCommit *string `json:"merge_commit"`
	OldParent   string  `json:"old_parent"`
	Base        string  `json:"base"`
	HeadRef     string  `json:"head_ref"`
	HeadFork    bool    `json:"head_fork"`
}

type vRestackIn struct {
	Due         vDue                `json:"due"`
	Open        []vOpen             `json:"open"`
	CommitLists map[string][]string `json:"commit_lists"`
	PageSize    int                 `json:"page_size"`
	Errors      map[string]string   `json:"commit_errors"` // per PR number: "malformed" or "other"
	BranchRule  string              `json:"branch_name_rule"`
}

type vRestackOut struct {
	Candidates    []xcand  `json:"candidates"`
	Reads         []string `json:"commit_reads"`
	Diagnostics   int      `json:"diagnostics"`
	DiagnosticPRs []int    `json:"diagnostic_prs"`
	Error         string   `json:"error"`
}

var errOtherRead = errors.New("network")

func vRestack(t *testing.T, in vRestackIn) vRestackOut {
	lists := map[int][]string{}
	errs := map[int]error{}
	for k, l := range in.CommitLists {
		var n int
		fmt.Sscan(k, &n)
		lists[n] = l
	}
	for k, kind := range in.Errors {
		var n int
		fmt.Sscan(k, &n)
		if kind == "malformed" {
			errs[n] = fmt.Errorf("bad: %w", ErrMalformed)
		} else {
			errs[n] = errOtherRead
		}
	}
	lc := &fakeCommits{lists: lists, errs: errs, pageSize: in.PageSize}
	var diag []string
	rs := &GitRestacker{Commits: lc, Diag: func(s string) { diag = append(diag, s) }}
	switch in.BranchRule {
	case "", "default":
	case "reject-percent":
		rs.BranchOK = func(n string) error {
			if strings.Contains(n, "%") {
				return errors.New("percent")
			}
			return defaultBranchOK(n)
		}
	default:
		t.Fatalf("branch rule %q", in.BranchRule)
	}
	var open []OpenPR
	for _, o := range in.Open {
		open = append(open, OpenPR{Number: o.Number, Base: o.Base, HeadRef: o.HeadRef, Head: o.Head, Fork: o.Fork})
	}
	due := RestackDue{MergedPR: in.Due.MergedPR, MergeCommit: in.Due.MergeCommit, OldParent: in.Due.OldParent, Base: in.Due.Base, HeadRef: in.Due.HeadRef, HeadFork: in.Due.HeadFork}
	got, err := rs.Candidates(context.Background(), "o/r", due, open)
	out := vRestackOut{Candidates: []xcand{}, Reads: nonNil(lc.reads), DiagnosticPRs: []int{}, Diagnostics: len(diag)}
	for _, g := range got {
		out.Candidates = append(out.Candidates, xcand{PR: g.PR, Reason: g.Reason, Branch: g.Branch, Argv: g.Argv})
	}
	for _, d := range diag {
		var n int
		if i := strings.Index(d, "#"); i >= 0 {
			fmt.Sscan(d[i+1:], &n)
		}
		out.DiagnosticPRs = append(out.DiagnosticPRs, n)
	}
	if err != nil {
		out.Error = "commit-read-failed"
	}
	return out
}

func exportRestackVectors(t *testing.T, dir string) {
	v := watchexport.NewVectors("restack-candidates", "restack_candidates", "Which open PRs must be restacked after a merge: class 1 child-of-merged-branch by the base name alone, class 2 contains-merged-commits by the old parent in the commit list (at most 3 pages), ascending by PR number. The `argv` of a candidate is Go's fixed git command; it is null for a fork head or an unknown merge commit (owner decision F1: kept as in Go, an exception to D-20261103-03).",
		nil, "restack_test.go:TestRestackRebaseMergeFixture", "restack_test.go:TestRestackNullsAndFork", "restack_test.go:TestRestackPagingAndCap", "restack_test.go:TestRestackSkipsUnnamableBranch",
		"restack_test.go:TestRestackErrors", "restack_test.go:TestRestackHeadRefEqualsBaseUsesCommitCheck", "restack_test.go:TestRestackForkHeadHasNoClassOne", "restack_test.go:TestRestackStopsAtFirstHit")
	// recorded data
	raw, err := os.ReadFile("testdata/rebase_merge.json")
	if err != nil {
		t.Fatal(err)
	}
	f := loadRebaseFixture(t)
	_ = raw
	mc := f.Parent.MergeCommit
	{
		in := vRestackIn{
			Due: vDue{MergedPR: f.Parent.Number, MergeCommit: &mc, OldParent: f.Parent.HeadSHA, Base: f.Base, HeadRef: f.Parent.HeadRef},
			Open: []vOpen{{f.ChildOnParent.Number, f.ChildOnParent.Base, "child-a", sha("c1"), false}, {f.ChildRetargeted.Number, f.ChildRetargeted.Base, f.ChildRetargeted.HeadRef, sha("c2"), false},
				{99, "main", "unrelated", sha("ee"), false}, {98, "other", "elsewhere", sha("ff"), false}},
			CommitLists: map[string][]string{fmt.Sprint(f.ChildRetargeted.Number): f.ChildRetargeted.shas(), fmt.Sprint(f.ChildOnParent.Number): f.ChildOnParent.shas(), "99": {sha("ee")}},
		}
		v.Add("recorded rebase merge (testdata/rebase_merge.json): class 1 by base name, class 2 by the old head in the child's commits", in, vRestack(t, in))
	}
	old := sha("aa")
	nc := sha("bb")
	{
		in := vRestackIn{Due: vDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "feat/p"},
			Open:        []vOpen{{11, "feat/p", "c11", "", true}, {12, "main", "c12", "", false}},
			CommitLists: map[string][]string{"12": {old}}}
		v.Add("unknown merge commit gives null argv", in, vRestack(t, in))
		in.Due.MergeCommit = &nc
		v.Add("fork child gives null argv, the other child the fixed argv", in, vRestack(t, in))
	}
	{
		var list []string
		for i := 0; i < 5; i++ {
			list = append(list, sha(fmt.Sprintf("%02x", i+1)))
		}
		list = append(list, old)
		in := vRestackIn{Due: vDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "p"}, Open: []vOpen{{12, "main", "c", "", false}},
			CommitLists: map[string][]string{"12": list}, PageSize: 2}
		v.Add("the old parent on page 3 at page size 2 is found", in, vRestack(t, in))
		in.CommitLists = map[string][]string{"12": append([]string{sha("01"), sha("02"), sha("03"), sha("04"), sha("05"), sha("06")}, old)}
		v.Add("beyond the 3-page cap the old parent is not found and no fourth read happens", in, vRestack(t, in))
	}
	{
		in := vRestackIn{Due: vDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "p"},
			Open:        []vOpen{{11, "p", "bad%20name", "", false}, {12, "main", "-rf", "", false}, {13, "main", "ok/branch", "", false}},
			CommitLists: map[string][]string{"12": {old}, "13": {old}}, BranchRule: "reject-percent"}
		v.Add("a head branch that cannot be named is skipped with a diagnostic", in, vRestack(t, in))
	}
	{
		due := vDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "p"}
		open := []vOpen{{12, "main", "c12", "", false}, {13, "main", "c13", "", false}}
		in := vRestackIn{Due: due, Open: open, CommitLists: map[string][]string{"13": {old}}, Errors: map[string]string{"12": "malformed"}}
		v.Add("a malformed commit list skips that PR with a diagnostic", in, vRestack(t, in))
		in = vRestackIn{Due: due, Open: open, Errors: map[string]string{"12": "other"}}
		v.Add("any other read failure is returned so the core retries next cycle", in, vRestack(t, in))
	}
	{
		in := vRestackIn{Due: vDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "main"}, Open: []vOpen{{12, "main", "c", "", false}, {13, "main", "d", "", false}},
			CommitLists: map[string][]string{"12": {sha("01")}, "13": {old}}}
		v.Add("head ref equal to base: no class 1, only the commit check", in, vRestack(t, in))
	}
	{
		in := vRestackIn{Due: vDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "feat/x", HeadFork: true}, Open: []vOpen{{12, "feat/x", "c", "", false}}}
		v.Add("a merged fork head has no class 1", in, vRestack(t, in))
	}
	{
		in := vRestackIn{Due: vDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "p"}, Open: []vOpen{{12, "main", "c", "", false}},
			CommitLists: map[string][]string{"12": {old, sha("01"), sha("02"), sha("03")}}, PageSize: 2}
		v.Add("the first hit ends the reading even when more pages exist", in, vRestack(t, in))
	}
	{
		in := vRestackIn{Due: vDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "p"}, Open: []vOpen{{10, "main", "p", "", false}, {12, "p", "c", "", false}}}
		v.Add("the merged PR itself is never a candidate", in, vRestack(t, in))
	}
	if err := watchexport.Write(dir, v.Name, v); err != nil {
		t.Fatal(err)
	}

	// the head_fork recording of a merge (state apply)
	w := watchexport.NewVectors("merged-records-head-fork", "merged_due_head_fork", "The merge records whether the merged PR's head lives in a fork: false only if the PR was observed open with its head in the base repository; a fork-merged or unobserved PR never yields class-1 candidates.",
		nil, "restack_test.go:TestMergedDueRecordsHeadFork")
	for _, k := range []struct {
		name string
		prs  map[int]*PRState
	}{
		{"same repository", map[int]*PRState{10: {Number: 10}}},
		{"fork", map[int]*PRState{10: {Number: 10, Fork: true}}},
		{"unobserved", map[int]*PRState{}},
	} {
		st := newState()
		st.Repo = map[string]*RepoState{"o/r": {PRs: k.prs}}
		c := sha("cc")
		st.apply(Event{Repo: rp("o/r"), PR: pp(10), Kind: KindMerged, ID: "o/r#10:merged:" + c, Details: &MergedDetails{MergeCommit: &c, Base: "main", Head: sha("aa"), HeadRef: "f"}})
		w.Add(k.name, map[string]any{"observed_open_pr": k.name != "unobserved", "observed_fork": k.name == "fork"}, map[string]any{"head_fork": st.Repo["o/r"].RestackDue[0].HeadFork})
	}
	if err := watchexport.Write(dir, w.Name, w); err != nil {
		t.Fatal(err)
	}
}
