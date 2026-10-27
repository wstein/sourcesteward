// SPDX-License-Identifier: EUPL-1.2

package prompt

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// golden compares a transcript with its file. A question ends in ": " and the next line follows
// it in the captured text (the test input is not echoed); trailing spaces are trimmed so the
// file carries none.
func golden(t *testing.T, name, got string) {
	t.Helper()
	lines := strings.Split(got, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	got = strings.Join(lines, "\n")
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file (run with -update to see the diff):\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

// groupsFixture is three groups of the three levels: two green branches, three yellow directories
// of codex, and an orange group of one safe directory and one decision row.
func groupsFixture() []Group {
	return []Group{
		{
			Title: "2 branches in a", NounOne: "branch", NounMany: "branches", Place: "in a", Level: risk.Green, Counts: risk.Counts{Green: 2},
			Lines: []string{"  Owner     codex (1), claude (1)", "  Effect    2 branches are deleted (restorable)"},
			Rows: []Row{
				{Number: 1, ID: "b1", Verdict: Safe, Level: risk.Green, Kind: "branch", Repo: "a", Branch: "codex/old", Owner: "codex", OwnerShown: "codex", Word: "codex", Branches: 1},
				{Number: 2, ID: "b2", Verdict: Safe, Level: risk.Green, Kind: "branch", Repo: "a", Branch: "claude/old", Owner: "claude", OwnerShown: "claude", Word: "claude", Branches: 1},
			},
		},
		{
			Title: "3 directories /tmp/whr-*", NounOne: "directory", NounMany: "directories", Place: "/tmp/whr-*", Level: risk.Yellow, Counts: risk.Counts{Green: 1, Yellow: 2},
			Lines: []string{"  Owner     codex (3)", "  Effect    3 directories are deleted for good (their branches stay)"},
			Rows: []Row{
				{Number: 3, ID: "w3", Verdict: Safe, Level: risk.Green, Kind: "worktree", Repo: "a", Path: "/tmp/whr-1", Branch: "codex/1", Owner: "codex", OwnerShown: "codex", Word: "codex", Dirs: 1},
				{Number: 4, ID: "w4", Verdict: Safe, Level: risk.Yellow, Kind: "worktree", Repo: "a", Path: "/tmp/whr-2", Branch: "codex/2", Owner: "codex", OwnerShown: "codex", Word: "codex", Dirs: 1},
				{Number: 5, ID: "w5", Verdict: Safe, Level: risk.Yellow, Kind: "worktree", Repo: "b", Path: "/tmp/whr-3", Branch: "codex/3", Owner: "codex", OwnerShown: "codex", Word: "codex", Dirs: 1},
			},
		},
		{
			Title: "2 directories in /w", NounOne: "directory", NounMany: "directories", Place: "in /w", Level: risk.Orange, Counts: risk.Counts{Orange: 2},
			Lines: []string{"  Owner     unknown (2)"},
			Rows: []Row{
				{Number: 6, ID: "w6", Verdict: Safe, Level: risk.Orange, Kind: "worktree", Repo: "b", Path: "/w/x", Owner: "", OwnerShown: "unknown", Word: "unknown", Dirs: 1},
				{Number: 7, ID: "d7", Verdict: Decision, Level: risk.Orange, Kind: "worktree", Repo: "b", Path: "/w/lock", Owner: "", OwnerShown: "unknown", Word: "unknown", Dirs: 1, Detail: "stw lock whose record does not match [lock stw:t9; path /w/lock]"},
			},
		},
	}
}

func flow(t *testing.T, input string, groups []Group, opt Options) (FlowResult, string, error) {
	t.Helper()
	var out bytes.Buffer
	opt.Interactive = true
	res, err := RunFlow(bufio.NewReader(strings.NewReader(input)), &out, groups, opt)
	return res, out.String(), err
}

func chosenIDs(r FlowResult) []string {
	out := []string{}
	for _, row := range r.Rows {
		out = append(out, row.ID)
	}
	return out
}

// Enter takes a green group and skips a yellow and an orange one (D1, D5); the question names the
// default of each group.
func TestFlowDefaultsFollowTheLevel(t *testing.T) {
	// overview, g1, g2, g3, summary: Enter everywhere.
	res, out, err := flow(t, "\n\n\n\n\n", groupsFixture(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeChosen || !reflect.DeepEqual(chosenIDs(res), []string{"b1", "b2"}) {
		t.Fatalf("%+v\n%s", res, out)
	}
	for _, want := range []string{"Step 1 of 3", "(Enter = take)", "Step 2 of 3", "(Enter = skip)", "Step 3 of 3", "risk green", "risk yellow", "risk orange"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing:\n%s", want, out)
		}
	}
}

// `y` takes every takeable row of the group, also yellow and orange, and never a decision row.
func TestFlowYTakesTheGroupButNeverADecisionRow(t *testing.T) {
	res, out, err := flow(t, "\ns\ny\ny\n\n", groupsFixture(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(chosenIDs(res), []string{"w3", "w4", "w5", "w6"}) || len(res.Confirmed) != 0 {
		t.Fatalf("%v\n%s", chosenIDs(res), out)
	}
}

// `s` skips; `q` quits and chooses nothing even after earlier takes; the end of input is no answer
// and chooses nothing, wherever it comes.
func TestFlowSkipQuitAndEndOfInput(t *testing.T) {
	res, _, err := flow(t, "\ny\ns\ns\n\n", groupsFixture(), Options{})
	if err != nil || !reflect.DeepEqual(chosenIDs(res), []string{"b1", "b2"}) {
		t.Fatalf("%v %v", chosenIDs(res), err)
	}
	res, out, err := flow(t, "\ny\nq\n", groupsFixture(), Options{})
	if err != nil || res.Outcome != OutcomeQuit || len(res.Rows) != 0 || !strings.Contains(out, "nothing was stored") {
		t.Fatalf("%+v %v\n%s", res, err, out)
	}
	full := "\ny\ny\ns\n\n"
	for cut := 0; cut < len(full); cut++ {
		res, _, err = flow(t, full[:cut], groupsFixture(), Options{})
		if err != nil || res.Outcome != OutcomeNone || len(res.Rows) != 0 {
			t.Errorf("end of input after %q: %+v %v", full[:cut], res, err)
		}
	}
	// Everything answered up to the summary and then no more input: still nothing chosen.
	res, _, err = flow(t, "\n\ns\ns\n", groupsFixture(), Options{})
	if err != nil || res.Outcome != OutcomeNone {
		t.Errorf("%+v %v", res, err)
	}
}

// Three invalid answers to one question end the run with nothing chosen (exit 1 in the caller).
func TestFlowThreeInvalidAnswers(t *testing.T) {
	for name, input := range map[string]string{
		"overview": "x\nx\nx\n",
		"step":     "\nzz\n??\nzz\n",
		"summary":  "\ny\ns\ns\nzz\nzz\nzz\n",
		"edit":     "\ny\ns\ns\ne\nzz\nzz\nzz\n",
		"review":   "\ns\nr\nm\nm\nm\n",
	} {
		res, out, err := flow(t, input, groupsFixture(), Options{})
		if !errors.Is(err, ErrTooManyInvalid) || len(res.Rows) != 0 {
			t.Errorf("%s: %+v %v\n%s", name, res, err, out)
		}
	}
	// Two invalid answers and then a valid one go on.
	res, _, err := flow(t, "\nzz\nzz\ny\ns\ns\n\n", groupsFixture(), Options{})
	if err != nil || !reflect.DeepEqual(chosenIDs(res), []string{"b1", "b2"}) {
		t.Errorf("%v %v", chosenIDs(res), err)
	}
}

// `l` lists the rows of the group 20 at a time with their numbers; `d N` shows a row's details.
func TestFlowListAndDetails(t *testing.T) {
	var rows []Row
	for n := 1; n <= 45; n++ {
		rows = append(rows, Row{Number: n, ID: fmt.Sprintf("w%d", n), Verdict: Safe, Level: risk.Yellow, Kind: "worktree", Repo: "a", Path: fmt.Sprintf("/tmp/whr-%02d", n), Owner: "codex", OwnerShown: "codex", Dirs: 1, Detail: fmt.Sprintf("detail %d proof patch-id", n)})
	}
	groups := []Group{{Title: "45 directories /tmp/whr-*", NounOne: "directory", NounMany: "directories", Level: risk.Yellow, Counts: risk.Counts{Yellow: 45}, Rows: rows}}
	res, out, err := flow(t, "\nl\n\n\nd 7\ns\n\n", groups, Options{})
	if err != nil || res.Outcome != OutcomeNone {
		t.Fatalf("%+v %v\n%s", res, err, out)
	}
	for _, want := range []string{"/tmp/whr-01", "/tmp/whr-20", "/tmp/whr-21", "/tmp/whr-40", "/tmp/whr-41", "/tmp/whr-45", "more", "detail 7 proof patch-id"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing:\n%s", want, out)
		}
	}
	// `q` stops the listing and the question is asked again.
	_, out, err = flow(t, "\nl\nq\ns\n\n", groups, Options{})
	if err != nil || strings.Contains(out, "/tmp/whr-21") || strings.Count(out, "Take this group?") != 2 {
		t.Errorf("%v\n%s", err, out)
	}
	// d with a number that is not offered is an invalid answer.
	_, _, err = flow(t, "\nd 99\nd x\nd 1000\n", groups, Options{})
	if !errors.Is(err, ErrTooManyInvalid) {
		t.Errorf("%v", err)
	}
}

// `r` asks per row (y, n, q); a decision row is never asked there, only named.
func TestFlowReviewEachRow(t *testing.T) {
	// Step 2: w3 y, w4 n, w5 q (the review stops); step 3: skip.
	res, out, err := flow(t, "\ns\nr\ny\nn\nq\ns\n\n", groupsFixture(), Options{})
	if err != nil || !reflect.DeepEqual(chosenIDs(res), []string{"w3"}) {
		t.Fatalf("%v %v\n%s", chosenIDs(res), err, out)
	}
	if !strings.Contains(out, "Take it?") {
		t.Errorf("no per-row question:\n%s", out)
	}
	// Step 3: the safe row is asked, the decision row is only named and never asked.
	res, out, err = flow(t, "\ns\ns\nr\ny\n\n", groupsFixture(), Options{})
	if err != nil || !reflect.DeepEqual(chosenIDs(res), []string{"w6"}) || !strings.Contains(out, "decision row") || strings.Count(out, "Take it?") != 1 {
		t.Fatalf("%v %v\n%s", chosenIDs(res), err, out)
	}
}

// A term line at a step adds rows by the numbers of the whole run; a decision row asks its own
// confirmation, "y" takes it and anything else leaves it out.
func TestFlowTermLineAndDecisionConfirmation(t *testing.T) {
	res, out, err := flow(t, "\n4-5\ns\ns\ns\n\n", groupsFixture(), Options{})
	if err != nil || !reflect.DeepEqual(chosenIDs(res), []string{"w4", "w5"}) || !strings.Contains(out, "Added 2 rows") {
		t.Fatalf("%v %v\n%s", chosenIDs(res), err, out)
	}
	res, out, err = flow(t, "\n7\ny\ns\ns\ns\n\n", groupsFixture(), Options{})
	if err != nil || !reflect.DeepEqual(chosenIDs(res), []string{"d7"}) || !res.Confirmed["d7"] {
		t.Fatalf("%v %v %v\n%s", chosenIDs(res), res.Confirmed, err, out)
	}
	if !strings.Contains(out, "decision row") || !strings.Contains(out, "lock stw:t9") {
		t.Errorf("the confirmation does not name the lock:\n%s", out)
	}
	res, _, err = flow(t, "\n7\nn\ns\ns\ns\n\n", groupsFixture(), Options{})
	if err != nil || len(res.Rows) != 0 || len(res.Confirmed) != 0 {
		t.Errorf("%+v %v", res, err)
	}
	// End of input inside the confirmation chooses nothing.
	res, _, err = flow(t, "\n7\n", groupsFixture(), Options{})
	if err != nil || res.Outcome != OutcomeNone || len(res.Rows) != 0 {
		t.Errorf("%+v %v", res, err)
	}
	// A confirmed decision row that an edit then leaves out is not confirmed any more.
	res, _, err = flow(t, "\n7\ny\ns\ns\ns\ne\n1\n\n", groupsFixture(), Options{})
	if err != nil || !reflect.DeepEqual(chosenIDs(res), []string{"b1"}) || len(res.Confirmed) != 0 {
		t.Errorf("%+v %v", res, err)
	}
}

// `a` at the first question takes all green rows of every owner and goes to the summary; `a` at
// the summary adds them; `e` replaces the selection with a line of terms.
func TestFlowAllGreenAndEdit(t *testing.T) {
	res, out, err := flow(t, "a\n\n", groupsFixture(), Options{})
	if err != nil || !reflect.DeepEqual(chosenIDs(res), []string{"b1", "b2", "w3"}) {
		t.Fatalf("%v %v\n%s", chosenIDs(res), err, out)
	}
	res, _, err = flow(t, "\ns\ny\ns\na\n\n", groupsFixture(), Options{})
	if err != nil || !reflect.DeepEqual(chosenIDs(res), []string{"b1", "b2", "w3", "w4", "w5"}) {
		t.Fatalf("%v %v", chosenIDs(res), err)
	}
	res, out, err = flow(t, "a\ne\nw except 3\n\n", groupsFixture(), Options{})
	if err != nil || !reflect.DeepEqual(chosenIDs(res), []string{"w4", "w5", "w6"}) {
		t.Fatalf("%v %v\n%s", chosenIDs(res), err, out)
	}
	if strings.Count(out, "Summary: nothing has been changed yet") != 2 {
		t.Errorf("the summary is shown again after an edit:\n%s", out)
	}
	// An invalid line at the edit question is asked again; the selection stays.
	res, _, err = flow(t, "a\ne\nnope\n1\n\n", groupsFixture(), Options{})
	if err != nil || !reflect.DeepEqual(chosenIDs(res), []string{"b1"}) {
		t.Fatalf("%v %v", chosenIDs(res), err)
	}
}

// An empty selection at the summary is nothing chosen, not a plan.
func TestFlowNothingSelected(t *testing.T) {
	res, out, err := flow(t, "\ns\ns\ns\n", groupsFixture(), Options{})
	if err != nil || res.Outcome != OutcomeNone || len(res.Rows) != 0 || !strings.Contains(out, "Nothing was chosen") {
		t.Errorf("%+v %v\n%s", res, err, out)
	}
	res, _, err = flow(t, "", nil, Options{})
	if err != nil || res.Outcome != OutcomeNone {
		t.Errorf("no groups: %+v %v", res, err)
	}
}

// SC-008: the summary names the selection by group, the levels, the undo lines, the sorted typed
// words, and its last line states the directories deleted for good and the branches.
func TestFlowSummaryLastLineStatesTheDeletions(t *testing.T) {
	res, out, err := flow(t, "\ny\ny\ny\n\n", groupsFixture(), Options{Kept: 964})
	if err != nil || len(res.Rows) != 6 {
		t.Fatalf("%+v %v\n%s", res, err, out)
	}
	// The test input is not echoed, so the line follows the prompt on the same row; nothing comes after it.
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "This deletes 4 directories for good and 2 branches (restorable).") {
		t.Errorf("the last line is not the deletion line:\n%s", out)
	}
	for _, want := range []string{
		"Summary: nothing has been changed yet", "3 directories /tmp/whr-*", "yellow", "total: 6 items in 2 repositories; 964 kept rows untouched",
		"Undo (branches only): stw tidy restore", "Typed words that will be asked (on /dev/tty): claude, codex, unknown",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing:\n%s", want, out)
		}
	}
	// An own-only selection asks no typed word and says so by listing none.
	groups := groupsFixture()
	for gi := range groups {
		for ri := range groups[gi].Rows {
			groups[gi].Rows[ri].Word = ""
		}
	}
	_, out, err = flow(t, "a\n\n", groups, Options{})
	if err != nil || strings.Contains(out, "Typed words") || !strings.HasSuffix(strings.TrimRight(out, "\n"), "This deletes 1 directory for good and 2 branches (restorable).") {
		t.Errorf("%v\n%s", err, out)
	}
	// A skipped group is counted.
	_, out, _ = flow(t, "\ns\ny\ns\n\n", groupsFixture(), Options{})
	if !strings.Contains(out, "skipped: 2 groups (2 directories, 2 branches)") {
		t.Errorf("%s", out)
	}
}

// SC-009: hostile names reach no terminal byte unescaped in any screen: the example rows, the list,
// the review, the details, the decision confirmation and the summary.
func TestFlowHostileNamesAreEscapedEverywhere(t *testing.T) {
	hostile := "/tmp/whr\x1b[2J\x07\r\n‮-evil"
	groups := groupsFixture()
	for gi := range groups {
		for ri := range groups[gi].Rows {
			r := &groups[gi].Rows[ri]
			r.Path, r.Branch, r.Owner, r.OwnerShown, r.Repo = hostile, "br\x1b]0;x\x07", "o\x1b[31m", "o\x1b[31m", "re\x00po"
			r.Detail = "detail \x1b[31m\x00"
		}
	}
	// Walk every question: list, details, review, a decision number, edit, summary.
	input := "\nl\nd 1\nr\ny\ny\nr\ny\ny\nq\n7\ny\nd 7\ns\ne\n1-7\n\n"
	_, out, _ := flow(t, input, groups, Options{})
	for _, bad := range []string{"\x1b", "\x07", "\x00", "\r", "‮"} {
		if strings.Contains(out, bad) {
			t.Errorf("raw %q in the output:\n%s", bad, out)
		}
	}
	if !strings.Contains(out, `\x1b[2J`) {
		t.Errorf("the escape is not shown as text:\n%s", out)
	}
}

// Narrow terminal: below 60 columns the branch column goes, below 40 the example rows go; the
// level word and the counts always stay.
func TestFlowNarrowTerminal(t *testing.T) {
	step := func(width int) string {
		_, out, _ := flow(t, "\ns\ns\ns\n\n", groupsFixture(), Options{Width: width})
		return out
	}
	wide, narrow, tiny := step(100), step(50), step(30)
	if !strings.Contains(wide, "codex/1") || strings.Contains(narrow, "codex/1") || !strings.Contains(narrow, "/tmp/whr-1") || strings.Contains(tiny, "/tmp/whr-1") {
		t.Errorf("wide:\n%s\nnarrow:\n%s\ntiny:\n%s", wide, narrow, tiny)
	}
	// With no example rows the step still says how many rows it holds and how to list them.
	if !strings.Contains(tiny, "... 3 more (l = list)") || strings.Contains(wide, "more (l = list)") {
		t.Errorf("tiny:\n%s", tiny)
	}
	for _, out := range []string{wide, narrow, tiny} {
		for _, want := range []string{"risk yellow", "1 green, 2 yellow", "Take this group?"} {
			if !strings.Contains(out, want) {
				t.Errorf("%q missing:\n%s", want, out)
			}
		}
	}
	for _, line := range strings.Split(step(50), "\n") {
		if strings.HasPrefix(line, "  ") && len([]rune(line)) > 50 && strings.Contains(line, "/tmp/whr") {
			t.Errorf("a row line is wider than the terminal: %q", line)
		}
	}
	// Unknown width is 80.
	if a, b := step(0), step(80); a != b {
		t.Errorf("width 0 is not 80")
	}
}

// Without a terminal nothing is read and nothing is printed.
func TestFlowWithoutATerminalAsksNothing(t *testing.T) {
	var out bytes.Buffer
	res, err := RunFlow(bufio.NewReader(neverReader{}), &out, groupsFixture(), Options{Interactive: false})
	if err != nil || res.Outcome != OutcomeNoTTY || out.Len() != 0 {
		t.Errorf("%+v %v %q", res, err, out.String())
	}
}

type neverReader struct{}

func (neverReader) Read([]byte) (int, error) { panic("read without a terminal") }

// The transcript of a whole run is the golden (SC-007).
func TestFlowTranscriptGolden(t *testing.T) {
	_, out, err := flow(t, "\n\ns\ny\n\n", groupsFixture(), Options{Kept: 964, Width: 100})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "flow-transcript.txt.golden", out)
}

// RunFlow offers only choosable rows, whoever built the groups: a red or kept row in a group is
// never shown, counted or taken.
func TestFlowDropsRowsThatAreNotChoosable(t *testing.T) {
	groups := groupsFixture()
	groups[0].Rows = append(groups[0].Rows,
		Row{Number: 20, ID: "red", Verdict: Safe, Level: risk.Red, Kind: "branch", Repo: "a", Branch: "red", Branches: 1},
		Row{Number: 21, ID: "kept", Verdict: Safe, Level: risk.Kept, Kind: "branch", Repo: "a", Branch: "kept", Branches: 1})
	res, out, err := flow(t, "\ny\ny\ny\n20\n\n", groups, Options{})
	if err != nil || strings.Contains(out, "red") && strings.Contains(out, "  20 ") || len(res.Rows) == 0 {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, r := range res.Rows {
		if r.ID == "red" || r.ID == "kept" {
			t.Errorf("took %s", r.ID)
		}
	}
	// A group of only such rows is no step.
	res, _, err = flow(t, "", []Group{{Title: "x", Level: risk.Green, Rows: []Row{{Number: 1, ID: "r", Verdict: Safe, Level: risk.Red}}}}, Options{})
	if err != nil || res.Outcome != OutcomeNone {
		t.Errorf("%+v %v", res, err)
	}
}

// Tracking references: counted in the summary, named in its effect column and in the line above the
// deletion line, which stays the last line.
func TestFlowSummaryCountsTrackingReferences(t *testing.T) {
	groups := append(groupsFixture()[:1], Group{
		Title: "2 tracking references in a", NounOne: "tracking reference", NounMany: "tracking references", Place: "in a", Level: risk.Green, Counts: risk.Counts{Green: 2},
		Rows: []Row{
			{Number: 3, ID: "t3", Verdict: Safe, Level: risk.Green, Kind: "tracking-ref", Repo: "a", Branch: "origin/x", Tracking: 1},
			{Number: 4, ID: "t4", Verdict: Safe, Level: risk.Green, Kind: "tracking-ref", Repo: "a", Branch: "origin/y", Tracking: 1},
		}})
	_, out, err := flow(t, "a\n\n", groups, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"delete 2 tracking references", "It also deletes 2 local tracking references.\nThis deletes 0 directories for good and 2 branches (restorable)."} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing:\n%s", want, out)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "(restorable).") {
		t.Errorf("the deletion line is not last:\n%s", out)
	}
}

// FR-013: no line of a screen is longer than 120 characters, however long a name is or however
// many owners and typed words there are; a cut shows "...".
func TestFlowLinesStayWithinTheBound(t *testing.T) {
	long := strings.Repeat("n", 300)
	groups := groupsFixture()
	groups[1].Title = "3 directories " + long
	groups[1].Place = long
	for gi := range groups {
		for ri := range groups[gi].Rows {
			r := &groups[gi].Rows[ri]
			r.Path, r.Branch, r.Repo = "/tmp/"+long, long, long
			r.Word = fmt.Sprintf("word%d%s", ri+gi*3, long)
		}
	}
	for _, width := range []int{0, 500} {
		_, out, _ := flow(t, "\ny\ny\ny\n\n", groups, Options{Width: width})
		// The input is not echoed, so the deletion line follows the prompt on one row.
		out = strings.Replace(out, "quit: This deletes", "quit:\nThis deletes", 1)
		for _, line := range strings.Split(out, "\n") {
			if n := len([]rune(line)); n > 120 {
				t.Errorf("width %d: a line of %d characters: %.140q", width, n, line)
			}
		}
		if !strings.Contains(out, "...") || !strings.Contains(out, "more") {
			t.Errorf("no visible cut:\n%s", out)
		}
	}
}

// `q` quits at the decision confirmation.
func TestFlowQuitAtTheDecisionConfirmation(t *testing.T) {
	res, out, err := flow(t, "\n7\nq\n", groupsFixture(), Options{})
	if err != nil || res.Outcome != OutcomeQuit || len(res.Rows) != 0 || !strings.Contains(out, "[y/N/q]") {
		t.Errorf("%+v %v\n%s", res, err, out)
	}
}
