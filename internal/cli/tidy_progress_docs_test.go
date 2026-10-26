// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// docsProgressBlocks are the two screens of the long-run section of docs/tidy.md: the live block in
// the middle of a run of five repositories, and the final block that stays when the run ends. The
// clock is the test's, so the times are round; the layout is that of a 100 column terminal.
func docsProgressBlocks() (live, final string) {
	p, screen, clk := liveSink(100, 24)
	names := []string{"chartscribe", "crewbook", "sheetmill", "sourcesteward", "workharbor"}
	p.SetRepos(names)
	step := func(d time.Duration, e workspace.ObserveEvent) {
		clk.advance(d)
		p.Event(e)
	}
	at := func(i int, ph workspace.ObservePhase, done, total int) workspace.ObserveEvent {
		return ev(names[i], i+1, 5, ph, done, total)
	}
	for i := 0; i < 4; i++ {
		step(0, at(i, workspace.ObservePhaseStart, 0, 0))
	}
	step(time.Second, at(3, workspace.ObservePhaseWorktrees, 0, 1098))
	step(8*time.Second, at(0, workspace.ObservePhaseBranches, 40, 40))
	step(0, done(names[0], 1, 5))
	step(5*time.Second, at(1, workspace.ObservePhaseProofs, 7, 7))
	step(0, done(names[1], 2, 5))
	step(0, at(4, workspace.ObservePhaseStart, 0, 0))
	step(0, at(4, workspace.ObservePhaseRefs, 0, 0))
	step(10*time.Second, at(2, workspace.ObservePhaseTracking, 2, 9))
	step(0, at(4, workspace.ObservePhaseWorktrees, 0, 469))
	step(14*time.Second, at(3, workspace.ObservePhaseWorktrees, 412, 1098))
	live = strings.Join(screen.lines(), "\n")
	step(8*time.Second, done(names[2], 3, 5))
	step(20*time.Second, done(names[3], 4, 5))
	step(10*time.Second, done(names[4], 5, 5))
	p.Finish()
	return live, strings.Join(screen.lines(), "\n")
}

func TestTidyLongRunExamplesMatchTheDocs(t *testing.T) {
	b, err := os.ReadFile("../../docs/tidy.md")
	if err != nil {
		t.Fatal(err)
	}
	live, final := docsProgressBlocks()
	for _, want := range []string{live, final} {
		if !strings.Contains(string(b), "```text\n"+want+"\n```") {
			t.Errorf("docs/tidy.md does not contain the real sample:\n%s", want)
		}
	}
	// The plain line of --progress.
	clk := newClock()
	var plain strings.Builder
	pp := newTidyProgress(&plain, tidyProgressPlain, clk.now)
	pp.Event(ev("sourcesteward", 3, 7, workspace.ObservePhaseWorktrees, 0, 1098))
	clk.advance(38 * time.Second)
	pp.Event(ev("sourcesteward", 3, 7, workspace.ObservePhaseWorktrees, 412, 1098))
	pl := strings.Split(strings.TrimSpace(plain.String()), "\n")
	if want := pl[len(pl)-1]; !strings.Contains(string(b), "`"+want+"`") {
		t.Errorf("docs/tidy.md does not contain the plain line %q", want)
	}
	// The verbose sample is the report of a real analysis; what depends on the machine is hidden.
	doc := string(b)
	i := strings.Index(doc, "Timings: total")
	j := strings.Index(doc[i:], "```")
	if i < 0 || j < 0 {
		t.Fatal("no verbose sample in docs/tidy.md")
	}
	if got, want := normalizeVerbose(doc[i:i+j]), normalizeVerbose(realVerboseSummary(t)); got != want {
		t.Errorf("the verbose sample of docs/tidy.md differs from a real report:\n--- docs\n%s\n--- real\n%s", got, want)
	}
}

// The agent-marker line of the docs is the real Mode line of a terminal run with the marker.
func TestTidyAgentMarkerLineMatchesTheDocs(t *testing.T) {
	newWizardWorld(t, true, true, "CLAUDE_CODE_SSE_PORT=12345", "HOME=/h")
	out, err := runTidy(t, "", "plan")
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "Mode:") {
			line = l
		}
	}
	b, err := os.ReadFile("../../docs/tidy.md")
	if err != nil {
		t.Fatal(err)
	}
	if line != "Mode:      agent (stdin tty, stdout tty, agent marker CLAUDE_CODE_SSE_PORT)" || !strings.Contains(string(b), line+"\n") {
		t.Fatalf("line %q", line)
	}
}
