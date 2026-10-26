// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
)

// Spec 044: the text of `tidy plan` is the grouped overview and the retain table; --rows keeps the
// row view of Spec 041, and --json is the same document either way.
func TestWizardPlanTextIsTheGroupedOverviewAndRowsKeepsTheRowView(t *testing.T) {
	newWizardWorld(t, true, true)
	grouped, err := runTidy(t, "", "plan")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Workspace /ws", "Derived:   workspace /ws (discovery)", "Mode:      human (stdin tty, stdout tty, no agent marker)", "Can be cleaned (4 of 6 candidates in 3 groups)", "risk", "Kept (2)", "stw tidy --rows"} {
		if !strings.Contains(grouped, want) {
			t.Errorf("%q missing:\n%s", want, grouped)
		}
	}
	for _, row := range []string{"[1] safe", "[-] retain", "summary:", "own1", "whr1", "owner claude"} {
		if strings.Contains(grouped, row) {
			t.Errorf("the grouped overview shows the row text %q:\n%s", row, grouped)
		}
	}
	rows, err := runTidy(t, "", "plan", "--rows")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Workspace: /ws", "Analysed 2 repositories", "own1", "[-] retain", "summary: 4 safe"} {
		if !strings.Contains(rows, want) {
			t.Errorf("--rows lost %q:\n%s", want, rows)
		}
	}
	if strings.Contains(rows, "Can be cleaned") {
		t.Errorf("--rows shows the grouped overview:\n%s", rows)
	}
}

// --rows is a text view: with --json the data is the document, unchanged.
func TestRowsDoesNotChangeTheJSON(t *testing.T) {
	newWizardWorld(t, false, false)
	plain, err := runTidy(t, "", "plan", "--json")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := runTidy(t, "", "plan", "--json", "--rows")
	if err != nil || rows != plain {
		t.Fatalf("--rows changed the JSON: %v", err)
	}
}

func TestRowsIsAWizardFlag(t *testing.T) {
	if tidyFlagClasses["rows"] != flagWizard {
		t.Fatalf("--rows is classified %v", tidyFlagClasses["rows"])
	}
	w := newWizardWorld(t, true, true)
	for name, args := range map[string][]string{
		"with a PATH":         {"plan", "/some/path", "--rows"},
		"with an expert flag": {"plan", "--rows", "--task", "T"},
	} {
		_, err := runTidy(t, "", args...)
		if !isUsage(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if w.prepared != 0 {
		t.Error("a usage error analyzed the workspace")
	}
	// The bare wizard takes it too.
	newInteractive(t, false, false)
	if _, err := runTidy(t, "", "--rows"); err != nil {
		t.Errorf("bare tidy --rows: %v", err)
	}
}

// Without a terminal the bare wizard prints the grouped overview, asks nothing and stores nothing.
func TestBareTidyWithoutATerminalPrintsTheGroupedOverview(t *testing.T) {
	i := newInteractive(t, false, false)
	out, err := runTidy(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Can be cleaned (4 of 6 candidates in 3 groups)") || !strings.Contains(out, "No terminal (or an agent run)") || strings.Contains(out, "[1] safe") {
		t.Errorf("%s", out)
	}
	if len(i.applied) != 0 || len(storedSets(t, i.prep.PlanStore)) != 0 {
		t.Error("stored or applied something")
	}
	rows, err := runTidy(t, "", "--rows")
	if err != nil || !strings.Contains(rows, "[-] retain") || strings.Contains(rows, "Can be cleaned") {
		t.Errorf("--rows: %v\n%s", err, rows)
	}
}

// The numbered choice of the interactive wizard still needs the numbers of the row view until the
// group steps replace it, so a human on a terminal keeps seeing the rows.
func TestBareTidyOnATerminalKeepsTheNumberedRows(t *testing.T) {
	newInteractive(t, true, true)
	out, err := runTidy(t, "\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[1] safe") || !strings.Contains(out, "Apply which?") {
		t.Errorf("%s", out)
	}
}

// A refused state directory is on the first screen of the default text, not only on stderr.
func TestGroupedOverviewShowsARefusedStateDirectory(t *testing.T) {
	w := newWizardWorld(t, true, true)
	w.prep.Derived = append(w.prep.Derived, derive.Derived{Name: "state-dirs", Value: "/s (skipped: state-dir-unsafe)", Source: "--state-dir"})
	w.prep.Stores = []derive.Store{{Path: "/s", Issue: &derive.Issue{Code: "state-dir-unsafe", Reason: "mode"}}}
	out, err := runTidy(t, "", "plan")
	if err == nil || !strings.Contains(out, "state-dirs /s (skipped: state-dir-unsafe) (--state-dir)") {
		t.Errorf("%v\n%s", err, out)
	}
}
