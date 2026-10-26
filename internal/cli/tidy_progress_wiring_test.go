// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/analysis"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// progressWorld is a wizard world whose analysis reports observation events and whose stderr is a
// buffer that is, or is not, a terminal.
func progressWorld(t *testing.T, stderrTTY bool, stdinTTY, stdoutTTY bool) *wizardWorld {
	t.Helper()
	w := newWizardWorld(t, stdinTTY, stdoutTTY)
	clk := newClock()
	oldNow, oldTTY, oldAnalyse := tidyProgressNow, tidyStderrIsTerminal, tidyAnalyse
	t.Cleanup(func() { tidyProgressNow, tidyStderrIsTerminal, tidyAnalyse = oldNow, oldTTY, oldAnalyse })
	tidyProgressNow = clk.now
	tidyStderrIsTerminal = func(any) bool { return stderrTTY }
	// An ordinary terminal: it takes escape sequences and the locale is UTF-8.
	oldEnv := tidyEnviron
	t.Cleanup(func() { tidyEnviron = oldEnv })
	tidyEnviron = func() []string { return append(oldEnv(), "TERM=xterm-256color", "LANG=C.UTF-8") }
	tidyAnalyse = func(ctx context.Context, _ analysis.Prepared, _ tidywizard.Config) tidywizard.Result {
		for i, name := range []string{"a", "b"} {
			rc := workspace.WithObserveRepo(ctx, name, i+1, 2)
			workspace.ReportObserve(rc, workspace.ObserveEvent{Phase: workspace.ObservePhaseStart})
			clk.advance(time1s)
			workspace.ReportObserve(rc, workspace.ObserveEvent{Phase: workspace.ObservePhaseWorktrees, Done: 1, Total: 2})
			clk.advance(time1s)
			workspace.ReportObserve(rc, workspace.ObserveEvent{Phase: workspace.ObservePhaseDone, Note: "analysed"})
		}
		return w.res
	}
	return w
}

const time1s = 1_000_000_000

// runTidyErr is runTidy with the standard error captured.
func runTidyErr(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := tidyCommand()
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errb.String(), err
}

func TestTidyPlanShowsLiveProgressOnAnErrorTerminalOnly(t *testing.T) {
	progressWorld(t, true, false, false)
	out, errText, err := runTidyErr(t, "plan")
	if err != nil {
		t.Fatal(err)
	}
	screen := newFakeScreen(80, 24)
	screen.feed(errText)
	// The final block stays: one line per repository, the cursor on the clean line below it.
	want := []string{
		"stw tidy: 2/2 repos, 4.0s, slowest a 2.0s",
		"✓ a analysed            2.0s",
		"✓ b analysed            2.0s",
	}
	if got := screen.lines(); !equalLines(got, want) || screen.col != 0 || screen.row != 3 {
		t.Fatalf("screen %q, cursor %d,%d\nstderr %q", got, screen.row, screen.col, errText)
	}
	if strings.Contains(errText, "Timings") {
		t.Fatalf("stderr %q", errText)
	}
	// The same run without a terminal on stderr prints the same stdout and nothing on stderr.
	progressWorld(t, false, false, false)
	out2, errText2, err := runTidyErr(t, "plan")
	if err != nil || errText2 != "" || out2 != out {
		t.Fatalf("stdout changed or stderr not empty: %v %q", err, errText2)
	}
}

func TestTidyPlanJSONHasNoProgress(t *testing.T) {
	progressWorld(t, true, true, true)
	out, errText, err := runTidyErr(t, "plan", "--json")
	if err != nil || errText != "" {
		t.Fatalf("%v %q", err, errText)
	}
	progressWorld(t, false, true, true)
	out2, _, err := runTidyErr(t, "plan", "--json")
	if err != nil || out != out2 {
		t.Fatalf("the JSON changed: %v", err)
	}
	progressWorld(t, true, true, true)
	if _, errText, err = runTidyErr(t, "--json"); err != nil || errText != "" {
		t.Fatalf("bare --json: %v %q", err, errText)
	}
}

func TestTidyVerbosePrintsTheSummaryToStderrOnly(t *testing.T) {
	for _, args := range [][]string{{"plan", "--verbose"}, {"--verbose"}, {"plan", "--json", "--verbose"}, {"--json", "--verbose"}} {
		progressWorld(t, false, false, false)
		out, errText, err := runTidyErr(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		want := "Timings: total 4.0s\n  discovery and references: 0ms\n  [1/2] a: analysed in 2.0s\n    observing worktrees 1/2: 1.0s\n  [2/2] b: analysed in 2.0s\n    observing worktrees 1/2: 1.0s\n  observation window: not reached\n"
		// A non-terminal verbose run also writes plain progress lines first (the start of each repository).
		if !strings.HasSuffix(errText, want) {
			t.Fatalf("%v: stderr %q", args, errText)
		}
		progressWorld(t, false, false, false)
		out2, _, err := runTidyErr(t, args[:len(args)-1]...)
		if err != nil || out != out2 {
			t.Fatalf("%v: --verbose changed stdout", args)
		}
	}
}

func TestTidyVerboseReportsTheWindow(t *testing.T) {
	w := progressWorld(t, false, false, false)
	inner := tidyAnalyse
	tidyAnalyse = func(ctx context.Context, p analysis.Prepared, c tidywizard.Config) tidywizard.Result {
		rc := workspace.WithObserveRepo(ctx, "b", 2, 2)
		workspace.ReportObserve(rc, workspace.ObserveEvent{Phase: workspace.ObservePhaseWindow, Note: "proofs"})
		return inner(ctx, p, c)
	}
	_ = w
	_, errText, err := runTidyErr(t, "plan", "--verbose")
	if err != nil || !strings.Contains(errText, "observation window reached: b (proofs); raise --timeout") {
		t.Fatalf("%v %q", err, errText)
	}
}

// --verbose belongs to the wizard layer: it never selects another layer, and the run mode is
// decided by stdin and stdout alone (stderr being a terminal changes nothing about the prompts).
func TestTidyVerboseAndStderrDoNotChangeTheMode(t *testing.T) {
	progressWorld(t, true, false, false)
	out, _, err := runTidyErr(t, "plan", "--json")
	if err != nil || docOf(t, out).Mode.Mode != owner.ModeAgent {
		t.Fatalf("%v %s", err, out)
	}
	progressWorld(t, true, true, true)
	out, _, err = runTidyErr(t, "plan", "--json")
	if err != nil || docOf(t, out).Mode.Mode != owner.ModeHuman {
		t.Fatalf("%v %s", err, out)
	}
	if _, _, err = runTidyErr(t, "plan", "--verbose", "--base", "main"); !isUsage(err) {
		t.Fatalf("--verbose with an expert flag is a usage error: %v", err)
	}
}

func TestTidyQuietAndProgressFlags(t *testing.T) {
	// --quiet silences a terminal; --progress writes plain lines without one; they exclude each other.
	progressWorld(t, true, false, false)
	if _, errText, err := runTidyErr(t, "plan", "--quiet"); err != nil || errText != "" {
		t.Fatalf("--quiet: %v %q", err, errText)
	}
	progressWorld(t, false, false, false)
	_, errText, err := runTidyErr(t, "plan", "--progress")
	if err != nil || !strings.Contains(errText, "[1/2] a: starting") || strings.ContainsAny(errText, "\r\x1b") {
		t.Fatalf("--progress: %v %q", err, errText)
	}
	if _, errText, err = runTidyErr(t, "plan", "--progress", "--json"); err != nil || errText != "" {
		t.Fatalf("--progress with --json: %v %q", err, errText)
	}
	if _, _, err = runTidyErr(t, "plan", "--quiet", "--progress"); !isUsage(err) {
		t.Fatalf("both flags: %v", err)
	}
}

type memFile struct{ bytes.Buffer }

func (*memFile) Close() error { return nil }

func TestTidyTraceWritesOneJSONLinePerGitProcess(t *testing.T) {
	w := progressWorld(t, false, false, false)
	_ = w
	var f memFile
	old := tidyOpenTrace
	t.Cleanup(func() { tidyOpenTrace = old })
	var gotPath string
	tidyOpenTrace = func(p string) (io.WriteCloser, error) { gotPath = p; return &f, nil }
	inner := tidyAnalyse
	tidyAnalyse = func(ctx context.Context, p analysis.Prepared, c tidywizard.Config) tidywizard.Result {
		rc := workspace.WithObserveRepo(ctx, "a", 1, 2)
		_ = rc
		return inner(ctx, p, c)
	}
	out, errText, err := runTidyErr(t, "plan", "--json", "--trace", "/t/git.jsonl")
	if err != nil || gotPath != "/t/git.jsonl" || errText != "" || out == "" {
		t.Fatalf("%v %q %q", err, gotPath, errText)
	}
	// An unusable file is a refusal before the analysis.
	tidyOpenTrace = func(string) (io.WriteCloser, error) { return nil, errors.New("exists\x1b[2J") }
	if _, _, err = runTidyErr(t, "plan", "--trace", "/t/git.jsonl"); err == nil || strings.Contains(err.Error(), "\x1b") {
		t.Fatalf("%v", err)
	}
}

func TestTidyProgressGitSinkCountsAndBoundsTheTrace(t *testing.T) {
	clk := newClock()
	var out, trace bytes.Buffer
	p := newTidyProgress(&out, tidyProgressOff, clk.now)
	p.trace = &trace
	p.Event(ev("a", 1, 1, workspace.ObservePhaseStart, 0, 0))
	p.Git(workspace.GitCall{Repo: "a", Argv: []string{"rev-list", "--count", "x"}, Duration: 1500 * time.Microsecond, Exit: 0})
	p.Git(workspace.GitCall{Repo: "a", Argv: []string{"log"}, Duration: 2 * time.Millisecond, Exit: 128})
	if got := trace.String(); got != `{"repo":"a","argv":["rev-list","--count","x"],"ms":1.5,"exit":0}`+"\n"+`{"repo":"a","argv":["log"],"ms":2,"exit":128}`+"\n" {
		t.Fatalf("%q", got)
	}
	p.Event(workspace.ObserveEvent{Repo: "a", Index: 1, Count: 1, Phase: workspace.ObservePhaseWorktrees, Done: 1, Total: 1, Item: "/w/slow", Took: 3 * time.Second})
	p.Summary()
	if !strings.Contains(out.String(), "git processes: 2, 3ms in total") || !strings.Contains(out.String(), "3.0s  a observing worktrees: /w/slow") {
		t.Fatalf("%s", out.String())
	}
	// The trace stops at its bound with one marker line.
	p.traceBytes = tidyTraceLimit - 5
	trace.Reset()
	p.Git(workspace.GitCall{Repo: "a", Argv: []string{"x"}})
	p.Git(workspace.GitCall{Repo: "a", Argv: []string{"y"}})
	if trace.String() != "{\"truncated\":true}\n" {
		t.Fatalf("%q", trace.String())
	}
}

// A run that is stopped (deadline or interrupt) while a repository is observed still leaves the
// final block, with that repository marked as stopped, before the error is reported.
func TestTidyPlanTimeoutLeavesTheFinalBlock(t *testing.T) {
	progressWorld(t, true, false, false)
	tidyAnalyse = func(ctx context.Context, _ analysis.Prepared, _ tidywizard.Config) tidywizard.Result {
		rc := workspace.WithObserveRepo(ctx, "a", 1, 2)
		workspace.ReportObserve(rc, workspace.ObserveEvent{Phase: workspace.ObservePhaseStart})
		workspace.ReportObserve(rc, workspace.ObserveEvent{Phase: workspace.ObservePhaseWorktrees, Done: 3, Total: 8})
		<-ctx.Done()
		return tidywizard.Result{}
	}
	_, errText, err := runTidyErr(t, "plan", "--timeout", "50ms")
	if err == nil {
		t.Fatal("no timeout error")
	}
	screen := newFakeScreen(80, 24)
	screen.feed(errText)
	lines := screen.lines()
	if len(lines) < 2 || !strings.HasPrefix(lines[1], "! a stopped observing worktrees [###-----] 3/8") || screen.col != 0 || len(screen.unknown) != 0 {
		t.Fatalf("%q cursor %d,%d", lines, screen.row, screen.col)
	}
}
