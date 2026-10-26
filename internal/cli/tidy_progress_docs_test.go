// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// docsProgressLine is the live line of the long-run section of docs/tidy.md.
func docsProgressLine() string {
	clk := newClock()
	var out bytes.Buffer
	p := newTidyProgress(&out, tidyProgressPlain, clk.now)
	p.Event(ev("sourcesteward", 3, 7, workspace.ObservePhaseWorktrees, 0, 1098))
	clk.advance(38 * time.Second)
	p.Event(ev("sourcesteward", 3, 7, workspace.ObservePhaseWorktrees, 412, 1098))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	return lines[len(lines)-1]
}

// docsVerboseSummary is the --verbose report of the long-run section of docs/tidy.md.
func docsVerboseSummary() string {
	clk := newClock()
	var out bytes.Buffer
	p := newTidyProgress(&out, tidyProgressOff, clk.now)
	p.Prepared(1200 * time.Millisecond)
	p.Event(ev("sourcesteward", 1, 1, workspace.ObservePhaseStart, 0, 0))
	p.Event(ev("sourcesteward", 1, 1, workspace.ObservePhaseWorktrees, 0, 40))
	clk.advance(21 * time.Second)
	p.Event(workspace.ObserveEvent{Repo: "sourcesteward", Index: 1, Count: 1, Phase: workspace.ObservePhaseWorktrees, Done: 40, Total: 40, Item: "/tmp/wt-037", Took: 1800 * time.Millisecond})
	p.Event(ev("sourcesteward", 1, 1, workspace.ObservePhaseBranches, 0, 12))
	clk.advance(2 * time.Second)
	p.Event(workspace.ObserveEvent{Repo: "sourcesteward", Index: 1, Count: 1, Phase: workspace.ObservePhaseBranches, Done: 12, Total: 12, Item: "refs/heads/codex/old-1", Took: 150 * time.Millisecond})
	p.Event(ev("sourcesteward", 1, 1, workspace.ObservePhaseProofs, 0, 30))
	clk.advance(40 * time.Second)
	p.Event(workspace.ObserveEvent{Repo: "sourcesteward", Index: 1, Count: 1, Phase: workspace.ObservePhaseProofs, Done: 30, Total: 30, Item: "refs/heads/codex/wide", Took: 4200 * time.Millisecond})
	p.Event(workspace.ObserveEvent{Repo: "sourcesteward", Index: 1, Count: 1, Phase: workspace.ObservePhaseWindow, Note: "proofs"})
	p.Event(workspace.ObserveEvent{Repo: "sourcesteward", Index: 1, Count: 1, Phase: workspace.ObservePhaseDone, Note: "analysed"})
	for i := 0; i < 880; i++ {
		p.Git(workspace.GitCall{Repo: "sourcesteward", Argv: []string{"rev-list"}, Duration: 70 * time.Millisecond})
	}
	p.Summary()
	return out.String()
}

func TestTidyLongRunExamplesMatchTheDocs(t *testing.T) {
	b, err := os.ReadFile("../../docs/tidy.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{docsProgressLine(), docsVerboseSummary()} {
		if !strings.Contains(string(b), want) {
			t.Errorf("docs/tidy.md does not contain the real sample:\n%s", want)
		}
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
