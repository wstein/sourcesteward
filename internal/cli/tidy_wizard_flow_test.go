// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
	"github.com/wstein/sourcesteward/internal/tidywizard/render"
)

// janitor is the world of the janitor wizard tests: integrated rows of every owner at every level,
// a decision row, a red row and a kept row.
type janitor struct {
	*interactive
	long time.Time
}

func newJanitor(t *testing.T) *janitor {
	t.Helper()
	i := newInteractive(t, true, true)
	now := tidyNow()
	long := now.Add(-30 * 24 * time.Hour)
	mk := func(repo string, kind tidywizard.Kind, subject string, v tidywizard.Verdict, ownerLabel string, src owner.Source, scope owner.Scope, proof tidywizard.Proof) tidywizard.Candidate {
		c := wcand(repo, kind, subject, v, ownerLabel, src, scope)
		c.Proof = proof
		if kind == tidywizard.KindBranch {
			c.Effects = []string{"delete-branch:" + subject}
		} else {
			c.Effects = []string{"remove-registration", "remove-files"}
			c.LastActivity = long
		}
		return c
	}
	ownBranch := mk("a", tidywizard.KindBranch, "claude/old", tidywizard.VerdictSafe, "claude", owner.SourceTaskRecord, owner.ScopeOwn, tidywizard.ProofAncestor)
	codexBranch := mk("a", tidywizard.KindBranch, "codex/old", tidywizard.VerdictSafe, "codex", owner.SourcePathConvention, owner.ScopeOther, tidywizard.ProofAncestor)
	hintBranch := mk("b", tidywizard.KindBranch, "feat/x", tidywizard.VerdictSafe, "author:Werner Stein", owner.SourceCommitAuthor, owner.ScopeUnknown, tidywizard.ProofPatchID)
	ownWT := mk("a", tidywizard.KindWorktree, "own1", tidywizard.VerdictSafe, "claude", owner.SourceTaskRecord, owner.ScopeOwn, tidywizard.ProofAncestor)
	ownWT.Task = "t1"
	codexWT := mk("a", tidywizard.KindWorktree, "whr-1", tidywizard.VerdictSafe, "codex", owner.SourcePathConvention, owner.ScopeOther, tidywizard.ProofAncestor)
	codexWT.Path = "/tmp/whr-1"
	unkWT := mk("b", tidywizard.KindWorktree, "unk", tidywizard.VerdictSafe, "", owner.SourceUnknown, owner.ScopeUnknown, tidywizard.ProofPatchID)
	dec := mk("b", tidywizard.KindWorktree, "lock", tidywizard.VerdictDecision, "", owner.SourceUnknown, owner.ScopeUnknown, tidywizard.ProofAncestor)
	dec.Reason, dec.Evidence, dec.Task = "lock-mismatch", "stw lock whose creation record does not match", "t9"
	red := wcand("a", tidywizard.KindBranch, "feat/unmerged", tidywizard.VerdictRetain, "codex", owner.SourcePathConvention, owner.ScopeOther)
	red.Reason = "unmerged"
	kept := wcand("a", tidywizard.KindWorktree, "primary", tidywizard.VerdictRetain, "", owner.SourceUnknown, owner.ScopeUnknown)
	kept.Reason, kept.Path = "primary-checkout", "/ws/a"
	i.res.Candidates = []tidywizard.Candidate{ownBranch, codexBranch, hintBranch, ownWT, codexWT, unkWT, dec, red, kept}
	return &janitor{interactive: i, long: long}
}

func (j *janitor) doc() render.Document {
	return render.Build(render.Input{Workspace: j.prep.Workspace, Derived: j.prep.Derived, Repos: j.prep.Meta, Result: j.res, Mode: owner.Evidence{Mode: owner.ModeHuman}, Now: tidyNow()})
}

func (j *janitor) plan() janitorPlan { return buildJanitorPlan(j.doc()) }

// number is the number the wizard gives to a candidate, or 0 when it is not offered.
func (j *janitor) number(id string) int {
	for _, g := range j.plan().groups {
		for _, r := range g.Rows {
			if r.ID == id {
				return r.Number
			}
		}
	}
	return 0
}

// answeringTTY plays the controlling terminal: it types the confirmation the prompt asks for (or a
// wrong word) and remembers what stdout held when it was first written to.
type answeringTTY struct {
	out      *bytes.Buffer
	reply    func(prompt string) string
	snapshot string
	prompts  []string
	pending  *strings.Reader
}

var typedWord = regexp.MustCompile(`Type "([^"]*)"`)

func (a *answeringTTY) Write(p []byte) (int, error) {
	if len(a.prompts) == 0 {
		a.snapshot = a.out.String()
	}
	a.prompts = append(a.prompts, string(p))
	a.pending = strings.NewReader(a.reply(string(p)) + "\n")
	return len(p), nil
}

func (a *answeringTTY) Read(p []byte) (int, error) {
	if a.pending == nil {
		return 0, io.EOF
	}
	return a.pending.Read(p)
}

func (a *answeringTTY) Close() error { return nil }

func rightWord(prompt string) string {
	if m := typedWord.FindStringSubmatch(prompt); m != nil {
		return m[1]
	}
	return ""
}

// run executes bare `stw tidy` with the given stdin and a terminal that types the right words.
func (j *janitor) run(stdin string, reply func(string) string, args ...string) (string, *answeringTTY, error) {
	var out bytes.Buffer
	tty := &answeringTTY{out: &out, reply: reply}
	tidyConfirmOpener = func() (io.ReadWriteCloser, error) {
		j.confirmOpened++
		return tty, nil
	}
	cmd := tidyCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), tty, err
}

// storedIDs are the candidate ids of the one stored plan set.
func (j *janitor) storedIDs(t *testing.T) []string {
	t.Helper()
	files := storedSets(t, j.prep.PlanStore)
	if len(files) != 1 {
		t.Fatalf("stored files: %v", files)
	}
	id := strings.TrimSuffix(files[0][strings.LastIndex(files[0], "/")+1:], ".json")
	set, err := planset.Load(j.prep.PlanStore, id)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range set.Items {
		ids = append(ids, it.ID)
	}
	sort.Strings(ids)
	return ids
}

func sorted(ids ...string) []string { sort.Strings(ids); return ids }

// SC-001: in janitor mode every integrated row is choosable whoever owns it, and `a` takes exactly
// the green rows of all owners; yellow, orange, decision, red and kept rows stay out.
func TestJanitorAllTakesTheGreenRowsOfEveryOwnerOnly(t *testing.T) {
	j := newJanitor(t)
	out, tty, err := j.run("a\n\n", rightWord)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := sorted("branch:a:claude/old", "branch:a:codex/old", "worktree:a:own1", "worktree:a:whr-1")
	if got := j.storedIDs(t); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("stored %v want %v", got, want)
	}
	// The one foreign word is asked once, on the terminal, after the summary's last line.
	if len(tty.prompts) != 1 || !strings.Contains(tty.prompts[0], "Foreign items (codex). Type ") {
		t.Errorf("prompts %q", tty.prompts)
	}
	if !strings.HasSuffix(strings.TrimRight(tty.snapshot, "\n"), "This deletes 2 directories for good and 2 branches (restorable).") {
		t.Errorf("the summary's last line is not directly before the typed prompt:\n%s", tty.snapshot)
	}
	if len(j.applied) != 0 {
		t.Errorf("applied %d", len(j.applied))
	}
}

// Every integrated row has a number, whoever owns it; red and kept rows have none (SC-004).
func TestJanitorNumbersTheChoosableRowsOnly(t *testing.T) {
	j := newJanitor(t)
	for _, id := range []string{"branch:a:claude/old", "branch:a:codex/old", "branch:b:feat/x", "worktree:a:own1", "worktree:a:whr-1", "worktree:b:unk", "worktree:b:lock"} {
		if j.number(id) == 0 {
			t.Errorf("%s has no number", id)
		}
	}
	for _, id := range []string{"branch:a:feat/unmerged", "worktree:a:primary"} {
		if j.number(id) != 0 {
			t.Errorf("%s has a number", id)
		}
	}
	// A number past the last row is not offered: three tries end the run, nothing is stored.
	n := fmt.Sprint(len(j.plan().rows()) + 1)
	out, _, err := j.run("\n"+n+"\n"+n+"\n"+n+"\n", rightWord)
	var failure runtimeFailure
	if !errors.As(err, &failure) || len(storedSets(t, j.prep.PlanStore)) != 0 {
		t.Errorf("%v\n%s", err, out)
	}
}

// A decision row is chosen by its number only: not by `a`, a group answer or a selector; with its
// number it asks its own confirmation naming the lock, and then the typed word.
func TestJanitorDecisionRowOnlyByItsNumber(t *testing.T) {
	j := newJanitor(t)
	n := j.number("worktree:b:lock")
	script := "\n"
	for _, g := range j.plan().groups {
		hit := false
		for _, r := range g.Rows {
			hit = hit || r.ID == "worktree:b:lock"
		}
		if hit {
			script += fmt.Sprintf("%d\ny\n", n)
		}
		script += "s\n"
	}
	script += "\n"
	out, tty, err := j.run(script, rightWord)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := j.storedIDs(t); strings.Join(got, ",") != "worktree:b:lock" {
		t.Errorf("stored %v\n%s", got, out)
	}
	for _, want := range []string{"is a decision row", "stw lock whose creation record does not match", "lock stw:t9"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing:\n%s", want, out)
		}
	}
	if len(tty.prompts) != 1 || !strings.Contains(tty.prompts[0], "Foreign items (unknown)") {
		t.Errorf("prompts %q", tty.prompts)
	}
	// `y` on the group that holds it, `w`, `a` and `o:unknown` never take it.
	for _, line := range []string{"w", "a", "o:unknown", "b"} {
		j2 := newJanitor(t)
		out, _, err = j2.run("\n"+line+"\ns\ns\ns\ns\ns\n\n", rightWord)
		for _, s := range storedSets(t, j2.prep.PlanStore) {
			if strings.Contains(s, "lock") {
				t.Errorf("%s stored %v", line, s)
			}
		}
		_ = err
	}
}

// The typed words: the wrong word, no terminal and a word given on stdin all store nothing.
func TestJanitorTypedWordRefusalsStoreNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		stdin string
		reply func(string) string
		args  []string
	}{
		"wrong word":    {"a\n\n", func(string) string { return "codex 000000" }, nil},
		"bare y":        {"a\n\n", func(string) string { return "y" }, nil},
		"word on stdin": {"a\n\ncodex 000000\nyes\n", func(string) string { return "" }, nil},
	} {
		j := newJanitor(t)
		out, _, err := j.run(tc.stdin, tc.reply, tc.args...)
		var failure runtimeFailure
		if !errors.As(err, &failure) || len(storedSets(t, j.prep.PlanStore)) != 0 || len(j.applied) != 0 {
			t.Errorf("%s: %v\n%s", name, err, out)
		}
	}
	// No terminal: the build fails before anything is planned.
	j := newJanitor(t)
	var out bytes.Buffer
	cmd := tidyCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader("a\n\n"))
	cmd.SetArgs(nil)
	err := cmd.Execute()
	var failure runtimeFailure
	if !errors.As(err, &failure) || len(storedSets(t, j.prep.PlanStore)) != 0 || j.confirmOpened == 0 {
		t.Errorf("%v\n%s", err, out.String())
	}
}

// An own-only selection asks "Store this plan set?" (default no) and no typed word.
func TestJanitorOwnOnlySelectionAsksToStore(t *testing.T) {
	for name, tc := range map[string]struct {
		stdin  string
		stored bool
	}{
		"y": {"a\n\ny\n", true}, "default": {"a\n\n\n", false}, "n": {"a\n\nn\n", false}, "end of input": {"a\n\n", false}, "yes is not y": {"a\n\nyes\nyes\nyes\n", false},
	} {
		j := newJanitor(t)
		var own []tidywizard.Candidate
		for _, c := range j.res.Candidates {
			if c.Scope == owner.ScopeOwn && c.Verdict == tidywizard.VerdictSafe {
				own = append(own, c)
			}
		}
		j.res.Candidates = own
		out, tty, err := j.run(tc.stdin, rightWord)
		stored := len(storedSets(t, j.prep.PlanStore)) == 1
		if stored != tc.stored || len(tty.prompts) != 0 || j.confirmOpened != 0 || len(j.applied) != 0 {
			t.Errorf("%s: stored %v err %v prompts %q\n%s", name, stored, err, tty.prompts, out)
		}
		if !strings.Contains(out, "Store this plan set?") {
			t.Errorf("%s: no store question\n%s", name, out)
		}
	}
}

// Until the confirmation receipt exists the wizard only prints the exact next command and never
// applies; the seam gets the stored set and nothing else (Spec 044 P4b implements it).
func TestJanitorHandsOverToTheApplyStepAndNeverApplies(t *testing.T) {
	j := newJanitor(t)
	out, _, err := j.run("a\n\n", rightWord)
	if err != nil {
		t.Fatal(err)
	}
	ids := j.storedIDs(t)
	files := storedSets(t, j.prep.PlanStore)
	id := strings.TrimSuffix(files[0][strings.LastIndex(files[0], "/")+1:], ".json")
	if len(ids) == 0 || !strings.Contains(out, "Nothing has been changed") || !strings.Contains(out, "Apply it with: stw tidy apply "+id+" --state-dir "+j.prep.PlanStore) || len(j.applied) != 0 {
		t.Errorf("%s", out)
	}
	if strings.Contains(out, "Apply it now?") {
		t.Errorf("the wizard asks to apply before the receipt exists:\n%s", out)
	}
	// The seam is an interface: a double sees the stored set and the same streams.
	j = newJanitor(t)
	spy := &applySpy{}
	old := tidyApplyStep
	tidyApplyStep = spy
	t.Cleanup(func() { tidyApplyStep = old })
	if _, _, err = j.run("a\n\n", rightWord); err != nil {
		t.Fatal(err)
	}
	if len(spy.got) != 1 || spy.got[0].Set.PlanID == "" || spy.got[0].Store != j.prep.PlanStore || len(spy.got[0].Set.Items) != 4 || len(j.applied) != 0 {
		t.Errorf("%+v", spy.got)
	}
}

type applySpy struct{ got []wizardHandoff }

func (s *applySpy) Confirm(h wizardHandoff) error { s.got = append(s.got, h); return nil }

// `q`, the end of input and three invalid answers store nothing.
func TestJanitorQuitEndOfInputAndInvalidAnswers(t *testing.T) {
	for name, tc := range map[string]struct {
		stdin string
		fails bool
	}{
		"q": {"q\n", false}, "q at a step": {"\ny\nq\n", false}, "end of input": {"", false}, "end of input in a step": {"\ny\n", false},
		"invalid": {"x\ny\nz\n", true}, "invalid at a step": {"\nzz\n??\nzz\n", true}, "all skipped": {"\ns\ns\ns\ns\ns\n", false},
	} {
		j := newJanitor(t)
		out, _, err := j.run(tc.stdin, rightWord)
		var failure runtimeFailure
		if errors.As(err, &failure) != tc.fails || (err != nil) != tc.fails || len(storedSets(t, j.prep.PlanStore)) != 0 || len(j.applied) != 0 {
			t.Errorf("%s: %v\n%s", name, err, out)
		}
		if !tc.fails && !strings.Contains(out, "nothing was stored") {
			t.Errorf("%s: no statement that nothing was stored\n%s", name, out)
		}
	}
}

// An interrupt at a janitor question is exit 1 naming the interruption.
func TestJanitorInterruptIsReportedAsAnInterruption(t *testing.T) {
	j := newJanitor(t)
	ctx, cancel := context.WithCancel(context.Background())
	in := &cancelOnRead{r: strings.NewReader("a\n\n"), cancel: cancel, after: 0}
	cmd := tidyCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(in)
	cmd.SetArgs(nil)
	err := cmd.ExecuteContext(ctx)
	var failure runtimeFailure
	if !errors.As(err, &failure) || !strings.Contains(err.Error(), "interrupted") || strings.Contains(err.Error(), "context canceled") || len(storedSets(t, j.prep.PlanStore)) != 0 {
		t.Errorf("%v", err)
	}
}

// --rows keeps the numbered row view and its question of Spec 041.
func TestJanitorRowsFlagKeepsTheRowQuestion(t *testing.T) {
	j := newJanitor(t)
	out, _, err := j.run("n\n", rightWord, "--rows")
	if err != nil || !strings.Contains(out, "Apply which?") || strings.Contains(out, "Step 1 of") {
		t.Errorf("%v\n%s", err, out)
	}
}

// All red or kept, and nothing at all: no question is asked, stdin is never read, exit 0.
func TestJanitorNothingToCleanAsksNothing(t *testing.T) {
	for name, keep := range map[string]func(c tidywizard.Candidate) bool{
		"all red or kept": func(c tidywizard.Candidate) bool { return c.Verdict == tidywizard.VerdictRetain },
		"nothing":         func(tidywizard.Candidate) bool { return false },
	} {
		j := newJanitor(t)
		var kept []tidywizard.Candidate
		for _, c := range j.res.Candidates {
			if keep(c) {
				kept = append(kept, c)
			}
		}
		j.res.Candidates = kept
		var out bytes.Buffer
		cmd := tidyCommand()
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(neverRead{t})
		cmd.SetArgs(nil)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out.String())
		}
		want := "Nothing can be cleaned"
		if name == "nothing" {
			want = "Nothing to clean."
		}
		if !strings.Contains(out.String(), want) || strings.Contains(out.String(), "Continue?") {
			t.Errorf("%s:\n%s", name, out.String())
		}
	}
}

// SC-009: hostile names in a branch, a path, a repository and an owner reach no terminal byte in
// any screen of the run: the overview, the steps, the list, the details, the review, the summary.
func TestJanitorHostileNamesAreEscapedOnEveryScreen(t *testing.T) {
	j := newJanitor(t)
	for k := range j.res.Candidates {
		c := &j.res.Candidates[k]
		if c.Verdict != tidywizard.VerdictSafe {
			continue
		}
		c.Owner = "o\x1b[31m" + c.Owner
		if c.Kind == tidywizard.KindBranch {
			c.Branch = "b\x1b]0;x\x07" + c.Branch
			c.Effects = []string{"delete-branch:" + c.Branch}
		} else {
			c.Path = "/tmp/whr\x1b[2J\r\n\u202e" + c.Path
			c.Branch = "br\x1b[1m"
		}
	}
	out, _, _ := j.run("\nl\nd 1\nr\ny\ny\ny\nl\nr\ny\ny\ny\ny\ny\ny\nq\n", func(string) string { return "nope" })
	for _, bad := range []string{"\x1b", "\x07", "\r", "\u202e", "\x00"} {
		if strings.Contains(out, bad) {
			t.Errorf("raw %q in the output:\n%s", bad, out)
		}
	}
	if !strings.Contains(out, `\x1b`) {
		t.Errorf("no escaped name shown:\n%s", out)
	}
	// The same through the summary. The typed prompt of owner.Confirm prints the word as it is (a
	// label of scope other that is one word); its text stays byte for byte, so it is not checked
	// here (a finding for the security review T012c).
	out, tty, _ := j.run("a\n\n", func(string) string { return "nope" })
	for _, s := range []string{out, tty.snapshot} {
		for _, bad := range []string{"\x1b", "\x07", "\r", "\u202e"} {
			if strings.Contains(s, bad) {
				t.Errorf("raw %q:\n%s", bad, s)
			}
		}
	}
}

// Narrow terminal: the width of the terminal reaches the steps; below 60 columns the branch
// column goes.
func TestJanitorNarrowTerminalDropsTheBranchColumn(t *testing.T) {
	for width, wantBranch := range map[int]bool{120: true, 50: false} {
		j := newJanitor(t)
		for k := range j.res.Candidates {
			if c := &j.res.Candidates[k]; c.Path == "/tmp/whr-1" {
				c.Branch = "codex/whr-1"
			}
		}
		old := tidyTerminalWidth
		tidyTerminalWidth = func(io.Writer) int { return width }
		out, _, _ := j.run("\ns\ns\ns\ns\ns\n", rightWord)
		tidyTerminalWidth = old
		if got := strings.Contains(out, "codex/whr-1"); got != wantBranch || !strings.Contains(out, "risk green") {
			t.Errorf("width %d: branch column %v\n%s", width, got, out)
		}
	}
}

// The counts and numbers of the groups are those of the grouped overview: every choosable row once.
func TestJanitorPlanGroupsCoverTheChoosableRowsOnce(t *testing.T) {
	j := newJanitor(t)
	p := j.plan()
	seen := map[string]bool{}
	n := 0
	for _, g := range p.groups {
		for _, r := range g.Rows {
			n++
			if seen[r.ID] || r.Number != n {
				t.Errorf("%s: number %d, position %d", r.ID, r.Number, n)
			}
			seen[r.ID] = true
		}
	}
	if n != 7 || len(p.rows()) != 7 {
		t.Errorf("%d rows", n)
	}
}
