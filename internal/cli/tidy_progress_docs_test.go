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

func TestTidyLongRunExamplesMatchTheDocs(t *testing.T) {
	b, err := os.ReadFile("../../docs/tidy.md")
	if err != nil {
		t.Fatal(err)
	}
	if want := docsProgressLine(); !strings.Contains(string(b), want) {
		t.Errorf("docs/tidy.md does not contain the real sample:\n%s", want)
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
