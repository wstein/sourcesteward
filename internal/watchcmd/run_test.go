// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watch"
)

type runRig struct {
	t      *testing.T
	f      *fakeReader
	out    *bytes.Buffer
	now    time.Time
	sleeps []time.Duration
	script func(i int) // runs before the (i+1)th poll, after the ith sleep
	p      Params
}

func newRunRig(t *testing.T, mode string) *runRig {
	t.Helper()
	r := &runRig{t: t, f: newFake(), out: &bytes.Buffer{}, now: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}
	r.f.tips["main"] = sha40("a")
	for n := 1; n <= 9; n++ {
		r.f.details[n] = detailJSON
	}
	r.p = Params{Mode: mode, Interval: time.Minute, DetailEvery: 10, PendingTimeout: 45 * time.Minute, MaxOpenPRs: 2000, Host: "github.com",
		StateDir: filepath.Join(t.TempDir(), "state"), Repos: []string{"o/r"}, Out: r.out,
		Now:    func() time.Time { return r.now },
		Jitter: func() time.Duration { return 0 },
		Sleep: func(ctx context.Context, d time.Duration) error {
			r.sleeps = append(r.sleeps, d)
			r.now = r.now.Add(d)
			if r.script != nil {
				r.script(len(r.sleeps))
			}
			return ctx.Err()
		},
		OpenReader: func(context.Context, string, string) (Reader, func(), error) { return r.f, func() {}, nil }}
	return r
}

func (r *runRig) run(ctx context.Context) error { return Run(ctx, r.p) }

func (r *runRig) lines() []map[string]any {
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(r.out.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			r.t.Fatalf("not JSON: %q", l)
		}
		out = append(out, m)
	}
	return out
}

func code(err error) int {
	var f *Failure
	if errors.As(err, &f) {
		return f.Code
	}
	if err == nil {
		return 0
	}
	return -1
}

func (r *runRig) open(items ...hosting.PullItem) { r.f.items = items }

func TestOnceBaselineIsSilentThenPrintsOnlyTransitions(t *testing.T) {
	r := newRunRig(t, ModeOnce)
	r.open(item(1, "main"))
	if err := r.run(context.Background()); err != nil || r.out.Len() != 0 {
		t.Fatalf("first run: %v %q", err, r.out)
	}
	for _, name := range []string{"state.json", "journal.jsonl", "heartbeat", "lock"} {
		if _, err := os.Stat(filepath.Join(r.p.StateDir, name)); name != "journal.jsonl" && err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	r.now = r.now.Add(time.Minute)
	r.open(item(1, "main"), item(2, "main"))
	if err := r.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	ls := r.lines()
	if len(ls) != 1 || ls[0]["kind"] != "new-pr" || ls[0]["pr"] != float64(2) || ls[0]["seq"] != float64(1) || ls[0]["repo"] != "o/r" {
		t.Fatalf("%v", ls)
	}
	// --baseline after state exists prints nothing and replaces the state
	r.out.Reset()
	r.p.Baseline = true
	r.now = r.now.Add(time.Minute)
	r.open(item(1, "main"), item(2, "main"), item(3, "main"))
	if err := r.run(context.Background()); err != nil || r.out.Len() != 0 {
		t.Fatalf("baseline: %v %q", err, r.out)
	}
	r.p.Baseline = false
	r.now = r.now.Add(time.Minute)
	r.out.Reset()
	if err := r.run(context.Background()); err != nil || r.out.Len() != 0 {
		t.Fatalf("PR 3 belongs to the new baseline: %v %q", err, r.out)
	}
}

func TestUntilEventExitsZeroOnAMatchingKind(t *testing.T) {
	r := newRunRig(t, ModeUntil)
	r.p.UntilKinds = map[string]bool{"checks-green": true, "new-pr": true}
	r.p.Timeout = 590 * time.Second
	r.open(item(1, "main"))
	r.script = func(i int) {
		if i == 2 {
			r.open(item(1, "main"), item(2, "main"))
		}
	}
	if err := r.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ls := r.lines(); len(ls) != 1 || ls[0]["kind"] != "new-pr" || len(r.sleeps) != 2 {
		t.Fatalf("%v sleeps %v", ls, r.sleeps)
	}
}

func TestUntilEventTimeoutExitsFourWithoutOutput(t *testing.T) {
	r := newRunRig(t, ModeUntil)
	r.p.UntilKinds = map[string]bool{"merged": true}
	r.p.Timeout = 5 * time.Minute
	r.open(item(1, "main"))
	err := r.run(context.Background())
	if code(err) != ExitTimeout || r.out.Len() != 0 {
		t.Fatalf("%v %q", err, r.out)
	}
	var total time.Duration
	for _, d := range r.sleeps {
		total += d
	}
	if total != 5*time.Minute {
		t.Fatalf("sleeps %v must end exactly at the timeout", r.sleeps)
	}
}

func TestUntilEventMatchesAfterThePollThatPrintedIt(t *testing.T) {
	r := newRunRig(t, ModeUntil)
	r.p.UntilKinds = map[string]bool{"head-changed": true}
	r.p.Timeout = time.Hour
	r.open(item(1, "main"), item(2, "main"))
	r.script = func(i int) {
		if i == 1 {
			a, b := item(1, "main"), item(2, "main")
			a.HeadSHA, b.HeadSHA = sha40("c"), sha40("d")
			r.open(a, b)
			r.f.items = append(r.f.items, item(3, "main"))
		}
	}
	if err := r.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, l := range r.lines() {
		kinds = append(kinds, l["kind"].(string))
	}
	if strings.Join(kinds, " ") != "head-changed head-changed new-pr" {
		t.Fatalf("the whole poll is printed before exiting: %v", kinds)
	}
	if len(r.sleeps) != 1 {
		t.Fatalf("sleeps %v", r.sleeps)
	}
}

func TestStreamEndsOnInterruptAndOnMaxIdle(t *testing.T) {
	r := newRunRig(t, ModeStream)
	r.open(item(1, "main"))
	ctx, cancel := context.WithCancel(context.Background())
	r.script = func(i int) {
		if i == 3 {
			cancel()
		}
	}
	if err := r.run(ctx); err != nil {
		t.Fatalf("SIGINT ends a stream with exit 0: %v", err)
	}
	r = newRunRig(t, ModeStream)
	r.p.MaxIdle = 150 * time.Second
	r.open(item(1, "main"))
	if err := r.run(context.Background()); err != nil {
		t.Fatalf("--max-idle ends a stream with exit 0: %v", err)
	}
	var total time.Duration
	for _, d := range r.sleeps {
		total += d
	}
	if total != 150*time.Second {
		t.Fatalf("sleeps %v", r.sleeps)
	}
}

func TestSecondWatcherExitsThree(t *testing.T) {
	r := newRunRig(t, ModeOnce)
	r.open(item(1, "main"))
	holder, err := watch.Open(watch.Options{Dir: r.p.StateDir, Repos: r.p.Repos, Observer: &counting{inner: NewHostObserver(r.f, nil, 0, nil, nil)}})
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	err = r.run(context.Background())
	if code(err) != ExitLocked || !strings.Contains(err.Error(), "pid") {
		t.Fatalf("%v", err)
	}
}

func TestUsageAndFailureExitCodes(t *testing.T) {
	r := newRunRig(t, ModeOnce)
	r.p.StateDir = "relative/state"
	if err := r.run(context.Background()); code(err) != ExitUsage {
		t.Fatalf("relative state dir: %v", err)
	}
	r = newRunRig(t, ModeOnce)
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	r.p.StateDir = filepath.Join(repo, "state")
	if err := r.run(context.Background()); code(err) != ExitUsage {
		t.Fatalf("state dir inside a work tree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "state")); err == nil {
		t.Fatal("nothing may be created for a usage error")
	}
	r = newRunRig(t, ModeOnce)
	os.MkdirAll(r.p.StateDir, 0o755)
	os.Chmod(r.p.StateDir, 0o755)
	if err := r.run(context.Background()); code(err) != ExitFailure {
		t.Fatalf("wide mode: %v", err)
	}
	r = newRunRig(t, ModeOnce)
	r.p.OpenReader = func(context.Context, string, string) (Reader, func(), error) {
		return nil, func() {}, errors.New("no token")
	}
	if err := r.run(context.Background()); code(err) != ExitFailure || !strings.Contains(err.Error(), "hosting-api-unavailable") {
		t.Fatalf("missing authentication: %v", err)
	}
	r = newRunRig(t, ModeOnce)
	os.MkdirAll(r.p.StateDir, 0o700)
	os.WriteFile(filepath.Join(r.p.StateDir, "state.json"), []byte("{broken"), 0o600)
	if err := r.run(context.Background()); code(err) != ExitFailure || !strings.Contains(err.Error(), "--baseline") {
		t.Fatalf("corrupt state is never silently reset: %v", err)
	}
	r.p.Baseline = true
	r.open(item(1, "main"))
	if err := r.run(context.Background()); err != nil {
		t.Fatalf("--baseline repairs it: %v", err)
	}
}

func TestOnceFailsOnlyWhenNoRepositoryCouldBeRead(t *testing.T) {
	r := newRunRig(t, ModeOnce)
	r.f.listErr = &hosting.WatchError{Code: hosting.CodeNetwork, Message: "HTTP read failed"}
	err := r.run(context.Background())
	if code(err) != ExitFailure {
		t.Fatalf("%v", err)
	}
	ls := r.lines()
	if len(ls) != 1 || ls[0]["kind"] != "watch-error" {
		t.Fatalf("the failure is data on stdout: %v", ls)
	}
	r = newRunRig(t, ModeOnce)
	r.p.Repos = []string{"o/r"}
	r.open(item(1, "main"))
	if err := r.run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHumanOutputAndSlowdown(t *testing.T) {
	r := newRunRig(t, ModeStream)
	r.p.Human = true
	r.p.MaxIdle = 10 * time.Minute
	r.open(item(1, "main"))
	r.f.rate = hosting.RateInfo{Known: true, Limit: 5000, Remaining: 100, Reset: r.now.Add(time.Hour)}
	r.script = func(i int) {
		if i == 1 {
			r.open(item(1, "main"), item(2, "main"))
		}
	}
	if err := r.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.out.String(), "09:02:00 o/r#2 new-pr PR 2 by alice into main") || strings.Count(r.out.String(), "\n") != 1 {
		t.Fatalf("%q", r.out)
	}
	if r.sleeps[0] != 2*time.Minute {
		t.Fatalf("remaining below 10 percent doubles the interval: %v", r.sleeps)
	}
}

func TestRateLimitedHoldsTheWholeRun(t *testing.T) {
	r := newRunRig(t, ModeStream)
	r.p.MaxIdle = 12 * time.Minute
	r.open(item(1, "main"))
	r.f.rate = hosting.RateInfo{Known: true, Limit: 5000, Remaining: 20, Reset: r.now.Add(10 * time.Minute)}
	if err := r.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var held bool
	for _, d := range r.sleeps {
		held = held || d >= 9*time.Minute
	}
	if !held {
		t.Fatalf("an exhausted limit waits for the reset: %v", r.sleeps)
	}
	ls := r.lines()
	if len(ls) == 0 || ls[0]["kind"] != "watch-error" || ls[0]["repo"] != nil {
		t.Fatalf("one run-level rate-limited error: %v", ls)
	}
}

func TestNoTokenInOutputStateOrJournal(t *testing.T) {
	r := newRunRig(t, ModeOnce)
	r.open(item(1, "main"))
	r.run(context.Background())
	r.open(item(1, "main"), item(2, "main"))
	r.run(context.Background())
	filepath.WalkDir(r.p.StateDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			raw, _ := os.ReadFile(path)
			if strings.Contains(string(raw), "token") {
				t.Errorf("%s mentions a token", path)
			}
		}
		return nil
	})
}

func TestRunStatus(t *testing.T) {
	r := newRunRig(t, ModeOnce)
	r.open(item(1, "main"))
	if err := r.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := RunStatus(StatusParams{StateDir: r.p.StateDir, Out: &out, Now: func() time.Time { return r.now.Add(time.Minute) }}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"state directory: " + r.p.StateDir, "status: fresh", "lock: free", "o/r", "failures 0"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in %s", want, out.String())
		}
	}
	out.Reset()
	err := RunStatus(StatusParams{StateDir: r.p.StateDir, JSON: true, Out: &out, Now: func() time.Time { return r.now.Add(4 * time.Minute) }})
	if code(err) != ExitStale {
		t.Fatalf("stale: %v", err)
	}
	var st map[string]any
	if json.Unmarshal(out.Bytes(), &st) != nil || st["fresh"] != false || st["schema_version"] != float64(1) {
		t.Fatalf("%s", out.String())
	}
	out.Reset()
	if err := RunStatus(StatusParams{StateDir: filepath.Join(t.TempDir(), "none"), Out: &out}); code(err) != ExitStale || !strings.Contains(out.String(), "heartbeat: missing") {
		t.Fatalf("missing heartbeat: %v %s", err, out.String())
	}
	if err := RunStatus(StatusParams{StateDir: "rel", Out: &out}); code(err) != ExitUsage {
		t.Fatalf("%v", err)
	}
}

func TestInterruptedUntilEventIsAFailureAndNeverAMatch(t *testing.T) {
	r := newRunRig(t, ModeUntil)
	r.p.UntilKinds = map[string]bool{"merged": true}
	r.p.Timeout = time.Hour
	r.open(item(1, "main"))
	ctx, cancel := context.WithCancel(context.Background())
	r.script = func(i int) {
		if i == 2 {
			cancel()
		}
	}
	err := r.run(ctx)
	if code(err) != ExitFailure || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("an interrupted --until-event must not look like a match: %v", err)
	}
	if got := interrupted(ModeStream); got != nil {
		t.Fatalf("stream: %v", got)
	}
	if got := interrupted(ModeOnce); code(got) != ExitFailure {
		t.Fatalf("once: %v", got)
	}
}

func TestRestackNeededEventsAreProducedEndToEnd(t *testing.T) {
	r := newRunRig(t, ModeOnce)
	r.f.tips["feat/1"] = sha40("f")
	r.open(item(1, "main"), item(2, "feat/1"), item(3, "main"), item(4, "main"))
	if err := r.run(context.Background()); err != nil || r.out.Len() != 0 {
		t.Fatalf("baseline: %v %q", err, r.out)
	}
	merge := sha40("e")
	r.f.details[1] = `{"state":"closed","merged":true,"merge_commit_sha":"` + merge + `","merged_at":"2026-10-22T08:00:00Z","base":{"ref":"main"},"head":{"sha":"` + sha40("1") + `","ref":"feat/1"}}`
	r.f.commits[merge] = `{"parents":[{"sha":"x"}]}`
	r.f.pulls[3] = `[{"sha":"` + sha40("1") + `"},{"sha":"` + sha40("3") + `"}]`
	r.f.pulls[4] = `[{"sha":"` + sha40("4") + `"}]`
	r.now = r.now.Add(time.Minute)
	r.open(item(2, "feat/1"), item(3, "main"), item(4, "main"))
	if err := r.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]any{}
	for _, l := range r.lines() {
		if l["kind"] == "restack-needed" {
			got[fmt.Sprint(l["pr"])] = l["details"].(map[string]any)
		}
	}
	if len(got) != 2 || got["2"]["reason"] != "child-of-merged-branch" || got["3"]["reason"] != "contains-merged-commits" || got["4"] != nil {
		t.Fatalf("%v\n%s", got, r.out)
	}
	if d := got["3"]; d["old_parent"] != sha40("1") || d["new_parent"] != merge || d["merged_pr"] != float64(1) || len(d["argv"].([]any)) != 10 {
		t.Fatalf("%v", d)
	}
}
