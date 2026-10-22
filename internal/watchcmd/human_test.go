// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/watch"
)

func s(v string) *string { return &v }
func n(v int) *int       { return &v }

func ev(kind string, repo *string, pr *int, details any) watch.Event {
	return watch.Event{SchemaVersion: 1, Seq: 1, ID: "x", Time: "2026-10-22T10:04:10Z", Repo: repo, PR: pr, Kind: kind, Details: details}
}

func TestHumanLinesGolden(t *testing.T) {
	r := s("wstein/sourcesteward")
	a, b := sha40("3"), sha40("9")
	tr := true
	cases := []struct {
		e    watch.Event
		want string
	}{
		{ev("new-pr", r, n(41), &watch.NewPRDetails{Title: "docs(035): stw watch", Author: "wstein", Draft: true, Base: "main", HeadRef: "docs/x", Head: a}),
			"10:04:10 wstein/sourcesteward#41 new-pr docs(035): stw watch by wstein into main (draft)"},
		{ev("head-changed", r, n(41), &watch.HeadChangedDetails{Old: a, New: b}), "10:04:10 wstein/sourcesteward#41 head-changed 3333333 -> 9999999"},
		{ev("checks-green", r, n(41), &watch.ChecksGreenDetails{Head: a, Base: "main", Total: 5}), "10:04:10 wstein/sourcesteward#41 checks-green 5 checks on 3333333"},
		{ev("checks-red", r, n(41), &watch.ChecksRedDetails{Head: a, Failing: []string{"docs-check"}, FailingTotal: 1, Shape: "real", Base: "main"}),
			"10:04:10 wstein/sourcesteward#41 checks-red docs-check (real)"},
		{ev("checks-red", r, n(41), &watch.ChecksRedDetails{Head: a, Failing: []string{"a", "b"}, FailingTotal: 5, Shape: "unknown", Base: "main"}),
			"10:04:10 wstein/sourcesteward#41 checks-red a, b and 3 more (unknown)"},
		{ev("checks-pending-timeout", r, n(41), &watch.PendingTimeoutDetails{Head: a, Pending: []string{"ci"}, Since: "2026-10-22T09:00:00Z"}),
			"10:04:10 wstein/sourcesteward#41 checks-pending-timeout ci pending since 2026-10-22T09:00:00Z"},
		{ev("mergeable-changed", r, n(41), &watch.MergeableDetails{Mergeable: &tr, State: "behind", Behind: true, Base: "main"}),
			"10:04:10 wstein/sourcesteward#41 mergeable-changed mergeable=true state=behind (behind)"},
		{ev("mergeable-changed", r, n(41), &watch.MergeableDetails{Mergeable: new(bool), State: "dirty", Conflicting: true, Base: "main"}),
			"10:04:10 wstein/sourcesteward#41 mergeable-changed mergeable=false state=dirty (conflicting)"},
		{ev("ready-state-changed", r, n(41), &watch.ReadyDetails{From: "draft", To: "ready"}), "10:04:10 wstein/sourcesteward#41 ready-state-changed draft -> ready"},
		{ev("base-changed", r, n(41), &watch.BaseChangedDetails{Old: "feat/x", New: "main"}), "10:04:10 wstein/sourcesteward#41 base-changed feat/x -> main"},
		{ev("merged", r, n(41), &watch.MergedDetails{MergeCommit: &b, Base: "main", Head: a, HeadRef: "docs/x", MethodHint: "rebase-or-squash"}),
			"10:04:10 wstein/sourcesteward#41 merged into main as 9999999 (rebase-or-squash)"},
		{ev("merged", r, n(41), &watch.MergedDetails{Base: "main", Head: a, MethodHint: "unknown"}), "10:04:10 wstein/sourcesteward#41 merged into main as unknown (unknown)"},
		{ev("closed", r, n(41), &watch.ClosedDetails{Head: a, Base: "main"}), "10:04:10 wstein/sourcesteward#41 closed without merge (base main)"},
		{ev("restack-needed", r, n(42), &watch.RestackDetails{MergedPR: 41, Base: "main", Reason: "contains-merged-commits", OldParent: a, NewParent: &b, Branch: "feat/035-impl",
			Argv: []string{"git", "-c", "rebase.updateRefs=false", "-c", "rerere.enabled=false", "rebase", "--onto", b, a, "feat/035-impl"}}),
			"10:04:10 wstein/sourcesteward#42 restack-needed after #41: git rebase --onto 9999999 3333333 feat/035-impl"},
		{ev("restack-needed", r, n(42), &watch.RestackDetails{MergedPR: 41, Base: "main", Reason: "child-of-merged-branch", OldParent: a, Branch: "someone:feat"}),
			"10:04:10 wstein/sourcesteward#42 restack-needed after #41: no command (someone:feat)"},
		{ev("repos-changed", nil, nil, &watch.ReposChangedDetails{Added: []string{"o/a"}, Removed: []string{"o/b", "o/c"}, Skipped: []watch.Skipped{{Path: "x", Reason: "r"}}}),
			"10:04:10 - repos-changed +1 -2 skipped 1"},
		{ev("watch-error", r, nil, &watch.WatchErrorDetails{Code: "network", Message: "HTTP read failed", Failures: 3, RetryInSeconds: 240, Repo: r}),
			"10:04:10 wstein/sourcesteward watch-error network: HTTP read failed (failure 3, retry in 240s)"},
		{ev("watch-error", nil, nil, &watch.WatchErrorDetails{Code: "rate-limited", Message: "wait", Failures: 1, RetryInSeconds: 60}),
			"10:04:10 - watch-error rate-limited: wait (failure 1, retry in 60s)"},
		{ev("watch-recovered", r, nil, &watch.WatchRecoveredDetails{AfterFailures: 3, Repo: r}), "10:04:10 wstein/sourcesteward watch-recovered after 3 failures"},
		{ev("watch-stalled", r, nil, &watch.WatchStalledDetails{Since: "2026-10-22T09:00:00Z", ThresholdSeconds: 600, Repo: "wstein/sourcesteward"}),
			"10:04:10 wstein/sourcesteward watch-stalled no success since 2026-10-22T09:00:00Z"},
	}
	for _, c := range cases {
		got := string(Human(c.e))
		if got != c.want+"\n" {
			t.Errorf("\n got %q\nwant %q", got, c.want)
		}
	}
}

func TestHumanEscapesControlCharactersAndBoundsText(t *testing.T) {
	r := s("o/r")
	line := string(Human(ev("new-pr", r, n(1), &watch.NewPRDetails{Title: "a\x1b[31mred\nline‮\x00", Author: "al\rice", Base: "main", Head: sha40("1")})))
	if strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, "\n") || strings.ContainsAny(line[:len(line)-1], "\x1b\r\x00‮") {
		t.Fatalf("%q", line)
	}
	if !strings.Contains(line, `\x1b`) || !strings.Contains(line, `\n`) {
		t.Fatalf("%q", line)
	}
	long := string(Human(ev("new-pr", r, n(1), &watch.NewPRDetails{Title: strings.Repeat("é", 500), Author: "a", Base: "main", Head: sha40("1")})))
	if len([]rune(long)) > 400 {
		t.Fatalf("unbounded line of %d runes", len([]rune(long)))
	}
	if got := string(Human(watch.Event{Kind: "future-kind", Time: "garbage", Details: nil})); !strings.HasSuffix(got, " future-kind\n") {
		t.Fatalf("unknown kinds are shown without a summary: %q", got)
	}
}

func TestHumanEscapesEveryTextField(t *testing.T) {
	bad := "x\x1b[31m\u009b\u2028\u202e\x7f"
	r, a := s("o/"+bad), bad+strings.Repeat("0", 40)
	tr := true
	events := []watch.Event{
		ev("new-pr", r, n(1), &watch.NewPRDetails{Title: bad, Author: bad, Base: bad, HeadRef: bad, Head: a}),
		ev("head-changed", r, n(1), &watch.HeadChangedDetails{Old: a, New: a}),
		ev("checks-green", r, n(1), &watch.ChecksGreenDetails{Head: a, Base: bad}),
		ev("checks-red", r, n(1), &watch.ChecksRedDetails{Head: a, Failing: []string{bad}, FailingTotal: 1, Shape: bad, Base: bad}),
		ev("checks-pending-timeout", r, n(1), &watch.PendingTimeoutDetails{Head: a, Pending: []string{bad}, Since: bad}),
		ev("mergeable-changed", r, n(1), &watch.MergeableDetails{Mergeable: &tr, State: bad, Base: bad}),
		ev("ready-state-changed", r, n(1), &watch.ReadyDetails{From: bad, To: bad}),
		ev("base-changed", r, n(1), &watch.BaseChangedDetails{Old: bad, New: bad}),
		ev("merged", r, n(1), &watch.MergedDetails{MergeCommit: &a, Base: bad, Head: a, HeadRef: bad, MethodHint: bad}),
		ev("closed", r, n(1), &watch.ClosedDetails{Head: a, Base: bad}),
		ev("restack-needed", r, n(2), &watch.RestackDetails{MergedPR: 1, Base: bad, Reason: bad, OldParent: a, NewParent: &a, Branch: bad, Argv: []string{"git"}}),
		ev("repos-changed", nil, nil, &watch.ReposChangedDetails{}),
		ev("watch-error", r, nil, &watch.WatchErrorDetails{Code: bad, Message: bad}),
		ev("watch-recovered", r, nil, &watch.WatchRecoveredDetails{}),
		ev("watch-stalled", r, nil, &watch.WatchStalledDetails{Since: bad, Repo: bad}),
	}
	for _, e := range events {
		line := string(Human(e))
		body := strings.TrimSuffix(line, "\n")
		for _, r := range body {
			if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == 0x2028 || r == 0x202e {
				t.Errorf("%s: unescaped %U in %q", e.Kind, r, line)
				break
			}
		}
	}
}
