// SPDX-License-Identifier: EUPL-1.2

package prompt

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
)

func items() []Item {
	return []Item{
		{Number: 1, ID: "w1", Verdict: Safe, Scope: owner.ScopeOwn, Owner: "claude", Detail: "worktree one"},
		{Number: 2, ID: "w2", Verdict: Safe, Scope: owner.ScopeOwn, Owner: "claude", Detail: "worktree two"},
		{Number: 3, ID: "d3", Verdict: Decision, Scope: owner.ScopeUnknown, Owner: "", Detail: "stw lock whose creation record does not match"},
		{Number: 4, ID: "o4", Verdict: Safe, Scope: owner.ScopeOther, Owner: "codex", Detail: "codex worktree"},
		{Number: 5, ID: "o5", Verdict: Safe, Scope: owner.ScopeOther, Owner: "codex", Detail: "second codex worktree"},
		{Number: 6, ID: "b6", Verdict: Safe, Scope: owner.ScopeOwn, Owner: "claude", Detail: "branch six"},
	}
}

func run(t *testing.T, input string, interactive bool) (Result, string, error) {
	t.Helper()
	var out bytes.Buffer
	res, err := Select(bufio.NewReader(strings.NewReader(input)), &out, items(), Options{Interactive: interactive})
	return res, out.String(), err
}

func ids(r Result) []string { return r.Chosen }

// The default answer is none: an empty line, "n" and an end of input all choose nothing.
func TestDefaultIsNone(t *testing.T) {
	for _, in := range []string{"\n", "n\n", "  \n", "", "n"} {
		res, _, err := run(t, in, true)
		if err != nil || res.Outcome != OutcomeNone || len(res.Chosen) != 0 {
			t.Errorf("%q: %+v %v", in, res, err)
		}
	}
}

// "a" chooses every safe item of scope own, and never a decision row or an other/unknown owner.
func TestAllChoosesOwnSafeItemsOnly(t *testing.T) {
	res, _, err := run(t, "a\n", true)
	if err != nil || res.Outcome != OutcomeChosen || !reflect.DeepEqual(ids(res), []string{"w1", "w2", "b6"}) || len(res.Confirmed) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

// Numbers, comma lists and ranges choose the numbered items, once each, in number order.
func TestNumbersAndRanges(t *testing.T) {
	for in, want := range map[string][]string{
		"1\n": {"w1"}, "1,2,6\n": {"w1", "w2", "b6"}, "1-2,6\n": {"w1", "w2", "b6"}, " 6 , 1 \n": {"w1", "b6"},
		"2,2,1-2\n": {"w1", "w2"}, "4\n": {"o4"}, "4-5\n": {"o4", "o5"},
	} {
		res, _, err := run(t, in, true)
		if err != nil || !reflect.DeepEqual(ids(res), want) {
			t.Errorf("%q: %+v %v", in, res, err)
		}
	}
}

// A decision row asks a second confirmation naming the lock; only a "y" keeps it.
func TestDecisionRowAsksASecondConfirmation(t *testing.T) {
	res, out, err := run(t, "1,3\ny\n", true)
	if err != nil || !reflect.DeepEqual(ids(res), []string{"w1", "d3"}) || !res.Confirmed["d3"] {
		t.Fatalf("%+v %v", res, err)
	}
	if !strings.Contains(out, "stw lock whose creation record does not match") || !strings.Contains(out, "[y/N]") {
		t.Errorf("the confirmation does not name the lock:\n%s", out)
	}
	for _, answer := range []string{"\n", "n\n"} {
		res, _, err := run(t, "1,3\n"+answer, true)
		if err != nil || !reflect.DeepEqual(ids(res), []string{"w1"}) || res.Confirmed["d3"] {
			t.Errorf("answer %q: %+v %v", answer, res, err)
		}
	}
	// An end of input inside the question (also after an invalid answer) is the end of the wizard:
	// nothing is chosen, not even the rows taken before the question.
	for _, answer := range []string{"", "yes\n", "Y\n"} {
		res, _, err := run(t, "1,3\n"+answer, true)
		if err != nil || res.Outcome != OutcomeNone || len(res.Chosen) != 0 || len(res.Confirmed) != 0 {
			t.Errorf("answer %q: %+v %v", answer, res, err)
		}
	}
	// A decision row alone that is not confirmed leaves nothing chosen.
	res, _, _ = run(t, "3\nn\n", true)
	if res.Outcome != OutcomeNone || len(res.Chosen) != 0 {
		t.Errorf("%+v", res)
	}
}

// "o" asks per owner label, in sorted order, and takes the safe items of the owners answered "y";
// a decision row is never taken by an owner answer.
func TestOwnerAsksPerOwner(t *testing.T) {
	res, out, err := run(t, "o\nn\ny\n", true)
	if err != nil || !reflect.DeepEqual(ids(res), []string{"o4", "o5"}) {
		t.Fatalf("%+v %v\n%s", res, err, out)
	}
	if !strings.Contains(out, "claude") || !strings.Contains(out, "codex") || strings.Index(out, "claude") > strings.Index(out, "codex") {
		t.Errorf("owners asked in sorted order:\n%s", out)
	}
	res, _, _ = run(t, "o\ny\ny\n", true)
	if !reflect.DeepEqual(ids(res), []string{"w1", "w2", "o4", "o5", "b6"}) {
		t.Errorf("%+v", res)
	}
	res, _, _ = run(t, "o\n\n\n", true)
	if res.Outcome != OutcomeNone {
		t.Errorf("the default of every owner question is no: %+v", res)
	}
}

// "d N" shows the details of the item and asks again; it is not a failed answer.
func TestDetailsShowAndReask(t *testing.T) {
	res, out, err := run(t, "d 4\nd 3\n1\n", true)
	if err != nil || !reflect.DeepEqual(ids(res), []string{"w1"}) {
		t.Fatalf("%+v %v", res, err)
	}
	if !strings.Contains(out, "codex worktree") || !strings.Contains(out, "stw lock whose creation record") {
		t.Errorf("details missing:\n%s", out)
	}
	res, _, err = run(t, "d 9\nd 9\nd 9\n", true)
	if err == nil {
		t.Errorf("an unknown detail number is an invalid answer: %+v", res)
	}
}

// Anything else is re-asked, three times in all, then the run fails with nothing chosen.
func TestStrictParsingRetriesThreeTimes(t *testing.T) {
	for _, bad := range []string{"x", "A", "all", "N", "0", "7", "1-", "-2", "2-1", "1,,2", "1 2", "1;2", "a b", "o x", "1.5", "+1", "2-1,1", "6-4,6", "99999999999999999999", "d", "d x", "n n"} {
		res, out, err := run(t, strings.Repeat(bad+"\n", 3)+"1\n", true)
		if !errors.Is(err, ErrTooManyInvalid) || len(res.Chosen) != 0 || res.Outcome != OutcomeNone {
			t.Errorf("%q: %+v %v", bad, res, err)
		}
		if strings.Count(out, "Apply which?") != 3 {
			t.Errorf("%q: asked %d times", bad, strings.Count(out, "Apply which?"))
		}
	}
	// One or two invalid answers followed by a valid one are fine.
	res, _, err := run(t, "x\nA\n6\n", true)
	if err != nil || !reflect.DeepEqual(ids(res), []string{"b6"}) {
		t.Errorf("%+v %v", res, err)
	}
	// An end of input during the retries is none, not an error.
	res, _, err = run(t, "x\n", true)
	if err != nil || res.Outcome != OutcomeNone {
		t.Errorf("%+v %v", res, err)
	}
}

// Only a safe or decision item can be chosen: numbers of rows without a number do not exist.
func TestOnlyListedNumbersAreChoosable(t *testing.T) {
	var out bytes.Buffer
	few := []Item{{Number: 1, ID: "a", Verdict: Safe, Scope: owner.ScopeOwn, Owner: "claude"}, {Number: 3, ID: "c", Verdict: Safe, Scope: owner.ScopeOwn, Owner: "claude"}}
	res, err := Select(bufio.NewReader(strings.NewReader("2\n1-3\n3\n")), &out, few, Options{Interactive: true})
	if err != nil || !reflect.DeepEqual(res.Chosen, []string{"c"}) {
		t.Errorf("number 2 and the range over the gap are invalid: %+v %v", res, err)
	}
}

type neverRead struct{ t *testing.T }

func (n neverRead) Read([]byte) (int, error) {
	n.t.Error("stdin was read without a terminal")
	return 0, io.EOF
}

// Without a terminal nothing is read and nothing is asked: the outcome is the plan-only degradation
// (exit 0, nothing stored), even when stdin holds a perfectly good answer.
func TestNoTerminalNeverReadsOrBlocks(t *testing.T) {
	var out bytes.Buffer
	res, err := Select(bufio.NewReader(neverRead{t}), &out, items(), Options{Interactive: false})
	if err != nil || res.Outcome != OutcomeNoTTY || len(res.Chosen) != 0 || out.Len() != 0 {
		t.Fatalf("%+v %v %q", res, err, out.String())
	}
	res, _, err = run(t, "a\n", false)
	if err != nil || res.Outcome != OutcomeNoTTY || len(res.Chosen) != 0 {
		t.Errorf("a pipe holding 'a' must not choose: %+v %v", res, err)
	}
	// Nothing choosable: nothing to ask either, with a terminal too.
	var empty bytes.Buffer
	res, err = Select(bufio.NewReader(neverRead{t}), &empty, nil, Options{Interactive: true})
	if err != nil || res.Outcome != OutcomeNone || empty.Len() != 0 {
		t.Errorf("%+v %v", res, err)
	}
}

// A line longer than the bound is an invalid answer, never an unbounded read.
func TestLongLinesAreBounded(t *testing.T) {
	res, _, err := run(t, strings.Repeat("1", 100000)+"\n"+strings.Repeat("2", 100000)+"\n"+strings.Repeat("3", 100000)+"\n", true)
	if !errors.Is(err, ErrTooManyInvalid) || len(res.Chosen) != 0 {
		t.Errorf("%+v %v", res, err)
	}
}

// The question to apply the stored plan set: only an exact "y" is yes, the default is no, anything
// else is re-asked three times and then fails.
func TestYesNo(t *testing.T) {
	for in, want := range map[string]bool{"y\n": true, "n\n": false, "\n": false} {
		var out bytes.Buffer
		got, err := YesNo(bufio.NewReader(strings.NewReader(in)), &out, "Apply it now?", true)
		if err != nil || got != want || !strings.Contains(out.String(), "[y/N]") {
			t.Errorf("%q: %v %v %q", in, got, err, out.String())
		}
	}
	for _, bad := range []string{"yes", "Y", "N", "1", "y y"} {
		var out bytes.Buffer
		got, err := YesNo(bufio.NewReader(strings.NewReader(strings.Repeat(bad+"\n", 3)+"y\n")), &out, "Apply it now?", true)
		if got || !errors.Is(err, ErrTooManyInvalid) {
			t.Errorf("%q: %v %v", bad, got, err)
		}
	}
	// The end of input is reported on its own, so a caller can tell it from a "no".
	for _, in := range []string{"", "zz\n", "zz\nzz\n"} {
		got, err := YesNo(bufio.NewReader(strings.NewReader(in)), io.Discard, "Q?", true)
		if got || !errors.Is(err, ErrEndOfInput) {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	got, err := YesNo(bufio.NewReader(strings.NewReader("zz\ny\n")), io.Discard, "Q?", true)
	if err != nil || !got {
		t.Errorf("%v %v", got, err)
	}
	// No terminal: never read, never yes.
	got, err = YesNo(bufio.NewReader(neverRead{t}), io.Discard, "Q?", false)
	if err != nil || got {
		t.Errorf("%v %v", got, err)
	}
}

// A decision row with an owner label of its own scope is still no "own safe" item and no owner answer
// takes it; a valid answer padded beyond the line bound is not an answer.
func TestDecisionRowsAreOnlyEverChosenByNumber(t *testing.T) {
	list := append(items(), Item{Number: 7, ID: "d7", Verdict: Decision, Scope: owner.ScopeOwn, Owner: "claude", Detail: "own decision"})
	var out bytes.Buffer
	res, err := Select(bufio.NewReader(strings.NewReader("a\ny\ny\n")), &out, list, Options{Interactive: true})
	if err != nil || !reflect.DeepEqual(res.Chosen, []string{"w1", "w2", "b6"}) {
		t.Errorf("a: %+v %v", res, err)
	}
	res, err = Select(bufio.NewReader(strings.NewReader("o\ny\ny\ny\ny\n")), &out, list, Options{Interactive: true})
	if err != nil || !reflect.DeepEqual(res.Chosen, []string{"w1", "w2", "o4", "o5", "b6"}) {
		t.Errorf("o: %+v %v", res, err)
	}
	res, err = Select(bufio.NewReader(strings.NewReader("1"+strings.Repeat(" ", 300)+"\n"+"1"+strings.Repeat(" ", 300)+"\n"+"1"+strings.Repeat(" ", 300)+"\n")), &out, list, Options{Interactive: true})
	if !errors.Is(err, ErrTooManyInvalid) || len(res.Chosen) != 0 {
		t.Errorf("over-long: %+v %v", res, err)
	}
}

// An end of input in any nested question ends the run with nothing chosen (the plan-only
// degradation), also when rows were already taken: the owner questions, the decision confirmation.
func TestEndOfInputInANestedQuestionChoosesNothing(t *testing.T) {
	for _, in := range []string{"o\ny\n", "o\n", "1,3\n", "o\nx\n"} {
		res, _, err := run(t, in, true)
		if err != nil || res.Outcome != OutcomeNone || len(res.Chosen) != 0 || len(res.Confirmed) != 0 {
			t.Errorf("%q: %+v %v", in, res, err)
		}
	}
	// "a" has no nested question: its end of input after the answer is irrelevant.
	res, _, err := run(t, "a\n", true)
	if err != nil || !reflect.DeepEqual(ids(res), []string{"w1", "w2", "b6"}) {
		t.Errorf("%+v %v", res, err)
	}
}

// A repeated range is accepted and de-duplicated; there is no cap on the number of tokens beyond
// the line bound.
func TestRepeatedRangesAreAcceptedOnce(t *testing.T) {
	res, _, err := run(t, "1-6,1-6,1-6,1-6,1-6\nn\n", true)
	if err != nil || !reflect.DeepEqual(ids(res), []string{"w1", "w2", "o4", "o5", "b6"}) {
		t.Errorf("%+v %v", res, err)
	}
}

// The details number is digits only, and the text of a detail or an owner label cannot carry control
// characters into the prompt.
func TestDetailsNumberIsStrictAndTextIsFlattened(t *testing.T) {
	for _, bad := range []string{"d +4", "d 04x", "d -4", "d 4 5", "d 0x4"} {
		_, _, err := run(t, strings.Repeat(bad+"\n", 3), true)
		if !errors.Is(err, ErrTooManyInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	list := []Item{
		{Number: 1, ID: "a", Verdict: Safe, Scope: owner.ScopeOwn, Owner: "cl\x1b[31maude\n", Detail: "det\x1b[2Jail\nline\x00"},
		{Number: 2, ID: "b", Verdict: Decision, Scope: owner.ScopeUnknown, Detail: "lock\rforged\x07"},
	}
	var out bytes.Buffer
	_, err := Select(bufio.NewReader(strings.NewReader("d 1\no\nn\n")), &out, list, Options{Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Select(bufio.NewReader(strings.NewReader("2\nn\n")), &out, list, Options{Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, r := range text {
		if r != '\n' && (r < 0x20 || r == 0x7f) {
			t.Errorf("control character %q in the prompt output", r)
		}
	}
	for _, want := range []string{"det [2Jail line ", "cl [31maude ", "lock forged "} {
		if !strings.Contains(text, want) {
			t.Errorf("flattened text %q missing:\n%q", want, text)
		}
	}
}

// The owner question agrees in number: claude has three safe items, codex two, one-item owners one.
func TestOwnerQuestionAgreesInNumber(t *testing.T) {
	list := append(items(), Item{Number: 7, ID: "x7", Verdict: Safe, Scope: owner.ScopeOther, Owner: "zed", Detail: "one"})
	var out bytes.Buffer
	if _, err := Select(bufio.NewReader(strings.NewReader("o\nn\nn\nn\n")), &out, list, Options{Interactive: true}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Take the 3 safe items of owner claude?", "Take the 2 safe items of owner codex?", "Take the 1 safe item of owner zed?"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("%q missing:\n%s", want, out.String())
		}
	}
}
