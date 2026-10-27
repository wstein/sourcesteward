// SPDX-License-Identifier: EUPL-1.2

package prompt

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
)

// selRows is the fixture of the grammar table: every kind of choosable row, a decision row, and a
// gap where a red row would be (a red row has no number, so number 9 is not offered).
func selRows() []Row {
	return []Row{
		{Number: 1, ID: "b1", Verdict: Safe, Level: risk.Green, Kind: "branch", Repo: "a", Owner: "codex", OwnerShown: "codex"},
		{Number: 2, ID: "b2", Verdict: Safe, Level: risk.Green, Kind: "branch", Repo: "a", Owner: "claude", OwnerShown: "claude"},
		{Number: 3, ID: "b3", Verdict: Safe, Level: risk.Yellow, Kind: "branch", Repo: "b", Owner: "author:Werner Stein", OwnerShown: "hint Werner Stein"},
		{Number: 4, ID: "w4", Verdict: Safe, Level: risk.Green, Kind: "worktree", Repo: "a", Path: "/tmp/whr-1", Owner: "codex", OwnerShown: "codex"},
		{Number: 5, ID: "w5", Verdict: Safe, Level: risk.Yellow, Kind: "worktree", Repo: "a", Path: "/tmp/whr-2", Owner: "codex", OwnerShown: "codex"},
		{Number: 6, ID: "w6", Verdict: Safe, Level: risk.Orange, Kind: "worktree", Repo: "b", Path: "/w/ws-x", Owner: "", OwnerShown: "unknown"},
		{Number: 7, ID: "d7", Verdict: Decision, Level: risk.Orange, Kind: "worktree", Repo: "b", Path: "/tmp/dec-1", Owner: "", OwnerShown: "unknown"},
		{Number: 8, ID: "w8", Verdict: Safe, Level: risk.Yellow, Kind: "worktree", Repo: "a", Path: "/tmp/claude/scratchpad/s1", Owner: "claude", OwnerShown: "claude"},
		{Number: 10, ID: "w10", Verdict: Safe, Level: risk.Green, Kind: "worktree", Repo: "a", Path: "/tmp/../home/x", Owner: "codex", OwnerShown: "codex"},
	}
}

func selIDs(rows []Row) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// SC-006: the grammar table. Every selector, the quoted label, `except`, a selector that matches
// nothing, and the decision row that only its own number takes.
func TestParseSelectionGrammar(t *testing.T) {
	for line, want := range map[string][]string{
		"1":                            {"b1"},
		"01":                           {"b1"},
		"1,2":                          {"b1", "b2"},
		"1 2":                          {"b1", "b2"},
		" 2 , 1 ":                      {"b1", "b2"},
		"1-3":                          {"b1", "b2", "b3"},
		"2,2,1-2":                      {"b1", "b2"},
		"a":                            {"b1", "b2", "w4", "w10"},
		"b":                            {"b1", "b2", "b3"},
		"w":                            {"w4", "w5", "w6", "w8", "w10"},
		"t":                            {"w4", "w5", "w8"},
		"o:codex":                      {"b1", "w4", "w5", "w10"},
		`o:"author:Werner Stein"`:      {"b3"},
		`o:"hint Werner Stein"`:        {"b3"},
		"o:unknown":                    {"w6"},
		"m:b":                          {"b3", "w6"},
		"a b":                          {"b1", "b2", "b3", "w4", "w10"},
		"w except 5":                   {"w4", "w6", "w8", "w10"},
		"w t except 5":                 {"w4", "w6", "w8", "w10"},
		"w except t":                   {"w6", "w10"},
		"1-6 except 2-3":               {"b1", "w4", "w5", "w6"},
		"1,2 except 1,2":               {},
		"o:nobody":                     {},
		`o:"a name with spaces"`:       {},
		"b except b":                   {},
		"1-8 except 7":                 {"b1", "b2", "b3", "w4", "w5", "w6", "w8"},
		"7":                            {"d7"},
		"6-8":                          {"w6", "w8"},
		"7 1":                          {"b1", "d7"},
		"w except 7":                   {"w4", "w5", "w6", "w8", "w10"},
		"1" + strings.Repeat(" ", 255): {"b1"},
	} {
		got, err := ParseSelection(line, selRows())
		if err != nil {
			t.Errorf("%q: %v", line, err)
			continue
		}
		if !reflect.DeepEqual(selIDs(got), want) {
			t.Errorf("%q: got %v want %v", line, selIDs(got), want)
		}
	}
}

// A decision row is taken by its own number and by nothing else (FR-001): not by a, w, t, a label,
// a repository, a range, or any selector, and a range skips it.
func TestDecisionRowIsTakenByItsNumberOnly(t *testing.T) {
	for _, line := range []string{"a", "b", "w", "t", "o:unknown", "m:b", "6-8", "1-10 except 9", "w t a b"} {
		got, err := ParseSelection(line, selRows())
		if err != nil && !strings.Contains(line, "1-10") {
			t.Fatalf("%q: %v", line, err)
		}
		for _, r := range got {
			if r.Verdict == Decision {
				t.Errorf("%q took the decision row", line)
			}
		}
	}
	got, err := ParseSelection("7", selRows())
	if err != nil || len(got) != 1 || got[0].Verdict != Decision {
		t.Errorf("number 7: %v %v", got, err)
	}
}

// Junk is an invalid answer: nothing is guessed. A number that is not offered (a red row has no
// number), a range over such a gap, an empty except, an unterminated quote, an over-long line.
func TestParseSelectionRejectsJunk(t *testing.T) {
	for _, line := range []string{
		"", "   ", "x", "9", "0", "11", "99", "4-9", "3-1", "1-", "-1", "1--2", "1 - 3", "1,,2", ",1", "1,", "1 ,, 2",
		"except 1", "1 except", "1 except 2 except 3", "except", "a except", "w t except", "o:", "m:", `o:"x`, `o:"x"y`, `"a"`, `o:""`,
		`o:a"b"`, "1;2", "1\t2", "a1", "0x1", "1234567890", "o:codex extra:", "1 except 99", "a except 0", "1-0000000001", "1 except x", "A", "Y", "y", "n", "all", "o", "m", "o:co\x00dex", "1\x00", "\xff", "1\n2",
	} {
		_, err := ParseSelection(line, selRows())
		if !errors.Is(err, ErrSelection) {
			t.Errorf("%q: error %v", line, err)
		}
	}
	// 257 bytes is over the bound even when it would parse; 256 is not.
	_, err := ParseSelection("1"+strings.Repeat(" ", 256), selRows())
	if !errors.Is(err, ErrLineTooLong) || !errors.Is(err, ErrSelection) {
		t.Errorf("257 bytes: %v", err)
	}
	_, err = ParseSelection(strings.Repeat("1,", 200), selRows())
	if !errors.Is(err, ErrLineTooLong) {
		t.Errorf("400 bytes: %v", err)
	}
}

// The error text is a fixed sentence; it never repeats the input (which may be hostile).
func TestParseSelectionErrorDoesNotEchoTheInput(t *testing.T) {
	_, err := ParseSelection("\x1b[31mevil", selRows())
	if err == nil || strings.Contains(err.Error(), "evil") || strings.Contains(err.Error(), "\x1b") {
		t.Errorf("%q", err)
	}
}

// The example of the specification: `w t except 85-86` over 90 rows.
func TestParseSelectionSpecExample(t *testing.T) {
	var rows []Row
	for n := 1; n <= 90; n++ {
		r := Row{Number: n, ID: fmt.Sprintf("r%d", n), Verdict: Safe, Level: risk.Yellow, Kind: "branch", Repo: "a"}
		switch {
		case n >= 80 && n <= 89:
			r.Kind, r.Path = "worktree", fmt.Sprintf("/tmp/whr-%d", n)
		case n == 90:
			r.Kind, r.Path = "worktree", "/home/x/ws"
		}
		rows = append(rows, r)
	}
	got, err := ParseSelection("w t except 85-86", rows)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"r80", "r81", "r82", "r83", "r84", "r87", "r88", "r89", "r90"}
	if !reflect.DeepEqual(selIDs(got), want) {
		t.Errorf("got %v", selIDs(got))
	}
}

// Choosable is the janitor rule (FR-001, FR-003): a safe or decision row whose level is green,
// yellow or orange, whoever owns it; red and kept rows are never choosable, whatever the verdict.
func TestChoosable(t *testing.T) {
	for _, tc := range []struct {
		verdict string
		level   risk.Level
		want    bool
	}{
		{Safe, risk.Green, true}, {Safe, risk.Yellow, true}, {Safe, risk.Orange, true}, {Decision, risk.Orange, true},
		{Safe, risk.Red, false}, {Safe, risk.Kept, false}, {Decision, risk.Red, false}, {"retain", risk.Green, false}, {"", risk.Green, false}, {Safe, "", false}, {Safe, "purple", false},
	} {
		if got := Choosable(tc.verdict, tc.level); got != tc.want {
			t.Errorf("%s/%s: %v", tc.verdict, tc.level, got)
		}
	}
}

// A row that is not choosable has no number, whatever the caller passes: a red and a kept row are
// never selected by a number, a range or a selector.
func TestParseSelectionIgnoresRowsThatAreNotChoosable(t *testing.T) {
	rows := append(selRows(),
		Row{Number: 11, ID: "red", Verdict: Safe, Level: risk.Red, Kind: "worktree", Path: "/tmp/red", Owner: "codex", OwnerShown: "codex"},
		Row{Number: 12, ID: "kept", Verdict: Safe, Level: risk.Kept, Kind: "branch", Repo: "a", Owner: "codex", OwnerShown: "codex"},
		Row{Number: 13, ID: "retain", Verdict: "retain", Level: risk.Green, Kind: "branch", Repo: "a", Owner: "codex", OwnerShown: "codex"})
	for _, line := range []string{"11", "12", "13", "10-13", "11-12"} {
		if _, err := ParseSelection(line, rows); !errors.Is(err, ErrSelection) {
			t.Errorf("%q: %v", line, err)
		}
	}
	for _, line := range []string{"a", "b", "w", "t", "o:codex", "m:a"} {
		got, err := ParseSelection(line, rows)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range got {
			if r.ID == "red" || r.ID == "kept" || r.ID == "retain" {
				t.Errorf("%q took %s", line, r.ID)
			}
		}
	}
}
