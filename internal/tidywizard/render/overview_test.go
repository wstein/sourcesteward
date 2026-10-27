// SPDX-License-Identifier: EUPL-1.2

package render

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
)

// realFixture is the recorded workspace of 2026-10-26 (testdata/workspace-20261026.rec): the 165
// safe rows unchanged and the 964 retained rows aggregated by what the retain table reads.
type realFixture struct {
	Workspace    string                 `json:"workspace"`
	Repositories []string               `json:"repositories"`
	Derived      []derive.Derived       `json:"derived"`
	Safe         []tidywizard.Candidate `json:"safe"`
	Retained     []realRetained         `json:"retained"`
	Total        int                    `json:"total"`
}

type realRetained struct {
	Kind        tidywizard.Kind    `json:"kind"`
	Verdict     tidywizard.Verdict `json:"verdict"`
	Reason      string             `json:"reason"`
	Proof       tidywizard.Proof   `json:"proof"`
	Scope       owner.Scope        `json:"scope"`
	OwnerSource owner.Source       `json:"owner_source"`
	Count       int                `json:"count"`
}

// realDocument builds the document of the recorded workspace. The activity time is not in the
// recording: activity is the time every safe directory is assumed to have last changed (the zero
// time means unknown, which the ladder treats as recent).
func realDocument(t *testing.T, activity time.Time) Document {
	t.Helper()
	b, err := os.ReadFile("testdata/workspace-20261026.rec")
	if err != nil {
		t.Fatal(err)
	}
	var f realFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	cands := []tidywizard.Candidate{}
	for _, c := range f.Safe {
		c.LastActivity = activity
		cands = append(cands, c)
	}
	n := 0
	for _, r := range f.Retained {
		for i := 0; i < r.Count; i++ {
			n++
			c := tidywizard.Candidate{ID: fmt.Sprintf("%s:.:retained-%04d", r.Kind, n), Repo: ".", Kind: r.Kind, Verdict: r.Verdict, Reason: r.Reason, Proof: r.Proof, Scope: r.Scope, OwnerSource: r.OwnerSource, Effects: []string{}}
			switch r.Kind {
			case tidywizard.KindBranch:
				c.Branch = fmt.Sprintf("retained-%04d", n)
			default:
				c.Path = fmt.Sprintf("/tmp/retained-%04d", n)
			}
			cands = append(cands, c)
		}
	}
	repos := []tidywizard.RepoResult{}
	meta := map[string]RepoMeta{}
	for _, name := range f.Repositories {
		repos = append(repos, tidywizard.RepoResult{Name: name, State: "analysed", Notices: []string{"tracking-requires-online", "Evidence is sampled and nontransactional; nothing was changed and nothing was fetched."}})
		meta[name] = RepoMeta{Path: f.Workspace, Backend: "git", Base: "refs/heads/main", Remote: "origin"}
	}
	if len(cands) != f.Total {
		t.Fatalf("%d candidates, want %d", len(cands), f.Total)
	}
	return Build(Input{Workspace: f.Workspace, Derived: f.Derived, Repos: meta, Result: tidywizard.Result{Repositories: repos, Candidates: cands}, Mode: owner.Evidence{Mode: owner.ModeAgent, Markers: []string{}, ByOwner: []string{}}, Now: runNow})
}

var longAgo = runNow.Add(-30 * 24 * time.Hour)

func lines(s string) int { return strings.Count(strings.TrimRight(s, "\n"), "\n") + 1 }

func TestOverviewOfTheRealWorkspaceGolden(t *testing.T) {
	golden(t, "overview-real-activity-old.txt.golden", []byte(Overview(realDocument(t, longAgo), OverviewOptions{})))
	golden(t, "overview-real-activity-unknown.txt.golden", []byte(Overview(realDocument(t, time.Time{}), OverviewOptions{})))
	golden(t, "retain-real.txt.golden", []byte(RetainTable(realDocument(t, longAgo), OverviewOptions{})))
}

// SC-012: with 960 or more retained rows and 165 choosable rows the first screen has at most 24
// lines and no row, no code of a retained row.
func TestFirstScreenIsShortAndShowsNoRetainedRowOrCode(t *testing.T) {
	for name, at := range map[string]time.Time{"old": longAgo, "unknown": {}} {
		d := realDocument(t, at)
		out := Overview(d, OverviewOptions{})
		if n := lines(out); n > 24 {
			t.Errorf("%s: %d lines:\n%s", name, n, out)
		}
		if strings.Contains(out, "retained-0") {
			t.Errorf("%s: a retained row is shown", name)
		}
		for _, code := range append(tidywizard.AllReasons(), risk.AllReasons()...) {
			if strings.Contains(out, code) {
				t.Errorf("%s: the code %q is in the first screen", name, code)
			}
		}
		for _, p := range tidywizard.AllProofs {
			if p != tidywizard.ProofNone && strings.Contains(out, string(p)) {
				t.Errorf("%s: the proof %q is in the first screen", name, p)
			}
		}
		if !strings.Contains(out, "165 of 1,129 candidates") {
			t.Errorf("%s: counts missing:\n%s", name, out)
		}
	}
}

// The group counts of the first screen are the counts of the groups: every choosable row is in
// exactly one line, the level counts of a line add up to its size, and the worst level is the
// level shown.
func TestOverviewLinesAddUpToTheChoosableRows(t *testing.T) {
	d := realDocument(t, longAgo)
	groups := overviewGroups(d)
	total := 0
	for _, g := range groups {
		c := g.Counts
		n := c.Green + c.Yellow + c.Orange
		if n != len(g.Rows) || c.Red != 0 || c.Kept != 0 {
			t.Errorf("group %s: counts %+v for %d rows", g.Label(), c, len(g.Rows))
		}
		worst := risk.Green
		switch {
		case c.Orange > 0:
			worst = risk.Orange
		case c.Yellow > 0:
			worst = risk.Yellow
		}
		if g.Level != worst {
			t.Errorf("group %s: level %s, worst row %s", g.Label(), g.Level, worst)
		}
		total += n
	}
	if total != 165 {
		t.Errorf("%d choosable rows in the groups, want 165", total)
	}
	levels := d.Summary.Risk
	if levels == nil || levels.Green+levels.Yellow+levels.Orange != 165 || levels.Red+levels.Kept != 964 {
		t.Errorf("summary %+v", levels)
	}
}

// The retain table lists every reason with its count, level, sentence and action; the counts add
// up to the retained total and the red rows are counted apart from the kept ones.
func TestRetainTableCountsAddUpToTheRetainedRows(t *testing.T) {
	d := realDocument(t, longAgo)
	rows := retainRows(d)
	sum, red, kept := 0, 0, 0
	for _, r := range rows {
		sum += r.Count
		if r.Level == risk.Red {
			red += r.Count
		} else {
			kept += r.Count
		}
		if r.Why == "" || r.Action == "" {
			t.Errorf("row without wording: %+v", r)
		}
	}
	if sum != 964 || red != 394 || kept != 570 {
		t.Errorf("sum %d red %d kept %d, want 964, 394, 570", sum, red, kept)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1].Count < rows[i].Count {
			t.Errorf("rows are not ordered by count: %+v", rows)
		}
	}
	out := RetainTable(d, OverviewOptions{})
	for _, code := range tidywizard.AllReasons() {
		if strings.Contains(out, code) {
			t.Errorf("the retain table prints the code %q", code)
		}
	}
	if !strings.Contains(out, "stw tidy --rows") {
		t.Error("the table does not say how to see the rows")
	}
}

// A retained row of a code the table does not know is still counted and named (escaped).
func TestRetainTableNamesAnUnknownReasonEscaped(t *testing.T) {
	in := riskInput()
	in.Result.Candidates = []tidywizard.Candidate{cand("r", tidywizard.KindBranch, "x", tidywizard.VerdictRetain, "future-reason\x1b[2J", owner.ScopeOwn)}
	in.Result.Repositories = []tidywizard.RepoResult{{Name: "r", State: "analysed", Notices: []string{}}}
	out := RetainTable(Build(in), OverviewOptions{})
	if strings.ContainsRune(out, 0x1b) || !strings.Contains(out, "future-reason") || !strings.Contains(out, "1") {
		t.Fatalf("%q", out)
	}
}

func TestOverviewEdgeCases(t *testing.T) {
	empty := riskInput()
	empty.Result = tidywizard.Result{Repositories: []tidywizard.RepoResult{{Name: "r", State: "analysed", Notices: []string{}}}, Candidates: []tidywizard.Candidate{}}
	if out := Overview(Build(empty), OverviewOptions{}); !strings.Contains(out, "Nothing to clean.") || strings.Contains(out, "Kept") {
		t.Errorf("empty:\n%s", out)
	}
	if out := RetainTable(Build(empty), OverviewOptions{}); out != "" {
		t.Errorf("an empty retain table is nothing: %q", out)
	}
	allKept := riskInput()
	allKept.Result.Candidates = []tidywizard.Candidate{cand("r", tidywizard.KindBranch, "x", tidywizard.VerdictRetain, "unmerged", owner.ScopeOwn)}
	allKept.Result.Repositories = []tidywizard.RepoResult{{Name: "r", State: "analysed", Notices: []string{}}}
	out := Overview(Build(allKept), OverviewOptions{})
	if !strings.Contains(out, "Nothing can be cleaned") || !strings.Contains(out, "may hold unique work") || strings.Contains(out, "Can be cleaned") {
		t.Errorf("all red:\n%s", out)
	}
	cut := riskInput()
	cut.Result.Truncated = true
	if out := Overview(Build(cut), OverviewOptions{}); !strings.Contains(out, "truncated:") {
		t.Errorf("a cut result must say so:\n%s", out)
	}
	one := riskInput()
	one.Result.Candidates = []tidywizard.Candidate{func() tidywizard.Candidate {
		c := cand("bundledock", tidywizard.KindBranch, "old", tidywizard.VerdictSafe, "", owner.ScopeOwn)
		c.Proof, c.Effects = tidywizard.ProofAncestor, []string{"delete-branch:old"}
		return c
	}()}
	if out := Overview(Build(one), OverviewOptions{}); !strings.Contains(out, "1 branch ") || !strings.Contains(out, "green") {
		t.Errorf("a group of one:\n%s", out)
	}
}

func TestOverviewReportsRepositoriesThatWereNotAnalyzed(t *testing.T) {
	out := Overview(Build(riskInput()), OverviewOptions{})
	for _, want := range []string{"chartscribe", "sheetmill"} {
		if !strings.Contains(out, want) {
			t.Errorf("%s is not named:\n%s", want, out)
		}
	}
	if strings.Contains(out, "base-ambiguous") {
		t.Errorf("the skip reason is a code:\n%s", out)
	}
}

// SC-009: hostile text reaches no terminal byte unescaped, whatever the field or the screen.
func TestOverviewAndRetainTableNeverPrintRawControlCharacters(t *testing.T) {
	in := riskInput()
	hostileDir := cand("bundledock", tidywizard.KindWorktree, "/tmp/\x1b[2Jevil\r\x00‮/x", tidywizard.VerdictSafe, "", owner.ScopeOther)
	hostileDir.Owner, hostileDir.OwnerSource, hostileDir.Proof = "ow\x1bner\n", owner.SourceCommitAuthor, tidywizard.ProofAncestor
	hostileDir.Effects = []string{"remove-registration", "remove-files"}
	ws := cand("bundledock", tidywizard.KindWorktree, "/ws/\x1b]0;title\x07dir/child", tidywizard.VerdictSafe, "", owner.ScopeOther)
	ws.Proof, ws.Effects = tidywizard.ProofAncestor, []string{"remove-registration", "remove-files"}
	in.Result.Candidates = append(in.Result.Candidates, hostileDir, ws)
	in.Result.Repositories = append(in.Result.Repositories, tidywizard.RepoResult{Name: "r\x1b[0m", State: "skipped", Reason: "x\x1b", Notices: []string{"n\x1b[31m"}})
	d := Build(in)
	for name, out := range map[string]string{"overview": Overview(d, OverviewOptions{}), "retain": RetainTable(d, OverviewOptions{})} {
		for i, line := range strings.Split(out, "\n") {
			for _, r := range line {
				if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) || r == unicode.ReplacementChar {
					t.Errorf("%s line %d has U+%04X: %q", name, i, r, line)
				}
			}
			if n := len([]rune(line)); n > 1500 {
				t.Errorf("%s line %d is %d characters", name, i, n)
			}
		}
	}
}

// Color is decoration only: the plain bytes are the colored text without its sequences.
func TestOverviewColorChangesNoByteOfThePlainText(t *testing.T) {
	d := realDocument(t, longAgo)
	sgr := regexp.MustCompile("\x1b\\[[0-9;]*m")
	plain := Overview(d, OverviewOptions{}) + RetainTable(d, OverviewOptions{})
	colored := Overview(d, OverviewOptions{Color: true}) + RetainTable(d, OverviewOptions{Color: true})
	if colored == plain || sgr.ReplaceAllString(colored, "") != plain {
		t.Fatal("color changed the text")
	}
	if strings.Contains(plain, "\x1b") {
		t.Fatal("escape in the plain text")
	}
}

// The Derived block and the Mode line stay on the first screen (Spec 041 FR-010): a refused state
// directory and an agent marker explain why a run exits 1 or asks nothing; both are escaped.
func TestOverviewKeepsTheDerivedBlockAndTheModeLine(t *testing.T) {
	in := riskInput()
	in.Derived = append(in.Derived, derive.Derived{Name: "state-dirs", Value: "/s\x1b[2J (skipped: state-dir-unsafe)", Source: "--state-dir", Overridden: true})
	in.Mode = owner.Evidence{Mode: owner.ModeAgent, StdinTTY: true, StdoutTTY: true, Markers: []string{"CLAUDE_CODE_SSE_PORT", "X\x1b[2J"}}
	out := Overview(Build(in), OverviewOptions{})
	for _, want := range []string{", state-dirs /s\\x1b[2J (skipped: state-dir-unsafe) (--state-dir)\n", "Mode:      agent (stdin tty, stdout tty, agent marker CLAUDE_CODE_SSE_PORT, X\\x1b[2J)\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q is missing:\n%s", want, out)
		}
	}
	in.Mode.Markers = []string{}
	in.Derived = nil
	out = Overview(Build(in), OverviewOptions{})
	if !strings.Contains(out, "Mode:      agent (stdin tty, stdout tty, no agent marker)\n") || strings.Contains(out, "Derived:") {
		t.Errorf("without a marker and derived values:\n%s", out)
	}
	// The line is the one of the row view, byte for byte.
	rows := Text(Build(riskInput()), TextOptions{})
	line := ""
	for _, l := range strings.Split(rows, "\n") {
		if strings.HasPrefix(l, "Derived:") {
			line = l
		}
	}
	if line == "" || !strings.Contains(Overview(Build(riskInput()), OverviewOptions{}), line+"\n") {
		t.Errorf("the derived line differs from the row view: %q", line)
	}
}

// The consequence line and the undo hint follow the effect classes: a branch group, and a directory
// that also deletes its branch, have the undo hint; directories only and tracking references do not.
func TestOverviewUndoHintFollowsTheEffectClass(t *testing.T) {
	mk := func(kind tidywizard.Kind, subject string, effects ...string) tidywizard.Candidate {
		c := cand("r", kind, subject, tidywizard.VerdictSafe, "", owner.ScopeOwn)
		c.Proof, c.Effects = tidywizard.ProofAncestor, effects
		return c
	}
	run := func(c tidywizard.Candidate) string {
		in := riskInput()
		in.Result.Candidates = []tidywizard.Candidate{c}
		in.Result.Repositories = []tidywizard.RepoResult{{Name: "r", State: "analysed", Notices: []string{}}}
		return Overview(Build(in), OverviewOptions{})
	}
	undo := "stw tidy restore PATH --contract 3 --plan PLAN_ID --state-dir DIR --apply --authorize"
	history := "stw tidy history --contract 3 --state-dir DIR"
	for name, tc := range map[string]struct {
		c           tidywizard.Candidate
		undo        bool
		consequence string
	}{
		"branch":                    {mk(tidywizard.KindBranch, "b", "delete-branch:b"), true, "reversible"},
		"directory with its branch": {mk(tidywizard.KindWorktree, "/tmp/x", "remove-registration", "remove-files", "delete-branch:b"), true, "files deleted, branch too"},
		"directory only":            {mk(tidywizard.KindWorktree, "/tmp/x", "remove-registration", "remove-files"), false, "files deleted"},
		"tracking reference":        {mk(tidywizard.KindTrackingRef, "refs/remotes/origin/x", "delete-tracking-ref:refs/remotes/origin/x"), false, "reference deleted"},
	} {
		out := run(tc.c)
		if strings.Contains(out, undo) != tc.undo || strings.Contains(out, history) != tc.undo || !strings.Contains(out, tc.consequence) {
			t.Errorf("%s: undo %v, want consequence %q:\n%s", name, strings.Contains(out, undo), tc.consequence, out)
		}
	}
}

// Hostile text in the owner and the repository of a group reaches the screen escaped, also where the
// group has one owner or one repository (the label is then named, not counted).
func TestOverviewEscapesTheOwnerAndTheRepositoryOfASingleGroup(t *testing.T) {
	in := riskInput()
	b := cand("re\x1bpo", tidywizard.KindBranch, "x", tidywizard.VerdictSafe, "", owner.ScopeOwn)
	b.Owner, b.OwnerSource, b.Proof, b.Effects = "ow\x1bner", owner.SourceTaskRecord, tidywizard.ProofAncestor, []string{"delete-branch:x"}
	w := cand("repo2", tidywizard.KindWorktree, "/tmp/y", tidywizard.VerdictSafe, "", owner.ScopeOwn)
	w.Owner, w.Proof, w.Effects = "o2\x1bx", tidywizard.ProofAncestor, []string{"remove-registration", "remove-files"}
	w2 := w
	w2.ID, w2.Path, w2.Owner = "worktree:repo2:/tmp/z", "/tmp/z", "o3\x1by"
	in.Result.Candidates = []tidywizard.Candidate{b, w, w2}
	in.Result.Repositories = []tidywizard.RepoResult{{Name: "re\x1bpo", State: "analysed", Notices: []string{}}, {Name: "repo2", State: "analysed", Notices: []string{}}}
	out := Overview(Build(in), OverviewOptions{})
	for _, want := range []string{"in re\\x1bpo", "ow\\x1bner", "o2\\x1bx and o3\\x1by"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q is missing:\n%s", want, out)
		}
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Error("raw escape")
	}
}

// Past the candidate cap the grouped text reads the whole result: owners and the kept counts are
// not those of the cut list.
func TestOverviewCountsTheWholeResultPastTheCandidateCap(t *testing.T) {
	in := riskInput()
	in.Result.Repositories = []tidywizard.RepoResult{{Name: "r", State: "analysed", Notices: []string{}}}
	in.Result.Candidates = nil
	for i := 0; i < tidywizard.MaxCandidates+10; i++ {
		c := cand("r", tidywizard.KindBranch, fmt.Sprintf("b%05d", i), tidywizard.VerdictSafe, "", owner.ScopeOwn)
		c.Owner, c.Proof, c.Effects = "claude", tidywizard.ProofAncestor, []string{"delete-branch:x"}
		if i%2 == 1 {
			c = cand("r", tidywizard.KindBranch, fmt.Sprintf("b%05d", i), tidywizard.VerdictRetain, "unmerged", owner.ScopeOwn)
		}
		in.Result.Candidates = append(in.Result.Candidates, c)
	}
	d := Build(in)
	if len(d.Candidates) != tidywizard.MaxCandidates {
		t.Fatalf("the list is not cut: %d", len(d.Candidates))
	}
	out := Overview(d, OverviewOptions{})
	want := (tidywizard.MaxCandidates + 10) / 2
	if !strings.Contains(out, fmt.Sprintf("Kept (%s)", thousands(want))) || strings.Contains(out, "unknown") || !strings.Contains(out, "reversible, claude") {
		t.Errorf("counts of the cut list:\n%s", out)
	}
	if rt := RetainTable(d, OverviewOptions{}); !strings.Contains(rt, fmt.Sprintf("Kept: %s candidates", thousands(want))) {
		t.Errorf("retain table:\n%s", rt)
	}
}

// Spec 044 SC-012 and "Overview first": a green group is never folded, only groups of one effect
// class are folded together, and a folded line still says what happens to its rows.
func TestFoldingKeepsGreenGroupsAndEffectClasses(t *testing.T) {
	for _, at := range []time.Time{longAgo, {}} {
		d := realDocument(t, at)
		all := overviewGroups(d)
		shown := foldGroups(all, maxGroupLines)
		if len(shown) > maxGroupLines {
			t.Fatalf("%d lines", len(shown))
		}
		rows, greens := 0, 0
		for _, g := range shown {
			rows += len(g.Rows)
			if g.merged > 0 && g.Level == risk.Green {
				t.Errorf("a green group was folded: %+v", g.Counts)
			}
			if g.merged > 0 {
				for _, r := range g.Rows {
					if tidywizard.Kind(r.Kind) != g.Kind {
						t.Errorf("a folded line mixes kinds: %s in %s", r.Kind, g.Kind)
					}
				}
			}
		}
		for _, g := range all {
			if g.Level == risk.Green {
				greens++
			}
		}
		for _, g := range shown {
			if g.Level == risk.Green && g.merged == 0 {
				greens--
			}
		}
		if rows != 165 || greens != 0 {
			t.Errorf("rows %d, green groups not shown alone: %d", rows, greens)
		}
	}
	out := Overview(realDocument(t, longAgo), OverviewOptions{})
	if !strings.Contains(out, "2 branches      in .") || !strings.Contains(out, "+ 4 smaller groups  reversible") || !strings.Contains(out, "files deleted, branch too") {
		t.Errorf("folded lines do not say what happens:\n%s", out)
	}
	// Nothing to fold: green groups stay, the list may be longer than the limit.
	var many []OverviewGroup
	for i := 0; i < 12; i++ {
		many = append(many, OverviewGroup{Kind: tidywizard.KindBranch, Repo: fmt.Sprint("r", i), Level: risk.Green, Rows: make([]risk.Row, 1)})
	}
	if got := foldGroups(many, maxGroupLines); len(got) != 12 {
		t.Errorf("green groups were folded: %d", len(got))
	}
}

// A merged pull request of the tip makes a row green in the group and the JSON carries its number;
// the group line counts it as green (Spec 044 C2).
func TestMergedPullRequestTurnsAGroupGreenInTheOverview(t *testing.T) {
	in := riskInput()
	c := cand("r", tidywizard.KindBranch, "old", tidywizard.VerdictSafe, "", owner.ScopeOwn)
	c.Proof, c.Effects, c.PR = tidywizard.ProofPatchID, []string{"delete-branch:old"}, 17
	in.Result.Candidates = []tidywizard.Candidate{c}
	in.Result.Repositories = []tidywizard.RepoResult{{Name: "r", State: "analysed", Notices: []string{}}}
	d := Build(in)
	if out := Overview(d, OverviewOptions{}); !strings.Contains(out, "1 branch") || !strings.Contains(out, " green") || strings.Contains(out, "yellow") {
		t.Errorf("overview:\n%s", out)
	}
	if got := d.Candidates[0]; got.PR != 17 || got.Risk != "green" || got.RiskReasons[0] != risk.ReasonPRMergedHead {
		t.Errorf("%+v", got)
	}
	in.Result.Candidates[0].PR = 0
	if out := Overview(Build(in), OverviewOptions{}); !strings.Contains(out, "yellow") {
		t.Errorf("without the number:\n%s", out)
	}
}

// The title of the first screen agrees in number: one candidate in one group.
func TestOverviewTitleAgreesInNumberForOne(t *testing.T) {
	one := riskInput()
	c := cand("bundledock", tidywizard.KindBranch, "old", tidywizard.VerdictSafe, "", owner.ScopeOwn)
	c.Proof, c.Effects = tidywizard.ProofAncestor, []string{"delete-branch:old"}
	one.Result.Candidates = []tidywizard.Candidate{c}
	if out := Overview(Build(one), OverviewOptions{}); !strings.Contains(out, "Can be cleaned (1 of 1 candidate in 1 group)") {
		t.Errorf("title:\n%s", out)
	}
}
