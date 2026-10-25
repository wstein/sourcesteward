// SPDX-License-Identifier: EUPL-1.2

package render

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden file (run with -update to see the diff):\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func cand(repo string, kind tidywizard.Kind, subject string, v tidywizard.Verdict, reason string, scope owner.Scope) tidywizard.Candidate {
	c := tidywizard.Candidate{
		ID: string(kind) + ":" + repo + ":" + subject, Repo: repo, Kind: kind, Verdict: v, Reason: reason,
		Owner: "claude", OwnerSource: owner.SourceTaskRecord, Scope: scope, Proof: tidywizard.ProofNone, Effects: []string{},
	}
	switch kind {
	case tidywizard.KindWorktree, tidywizard.KindStaleWorktree:
		c.Path = subject
	default:
		c.Branch = subject
	}
	return c
}

// input is a small overview with every shape: safe own, safe other (not preselected), decision,
// retain, a skipped and an erroring repository, and hostile text in every field that comes from outside.
func input() Input {
	safe := cand("sourcesteward", tidywizard.KindWorktree, "/tmp/wt-037", tidywizard.VerdictSafe, "", owner.ScopeOwn)
	safe.Task, safe.Branch, safe.Proof = "spec037-x", "codex/037-x", tidywizard.ProofPatchID
	safe.Effects = []string{"remove-registration", "remove-files", "delete-branch:codex/037-x"}
	other := cand("sourcesteward", tidywizard.KindWorktree, "/tmp/whr010-a", tidywizard.VerdictSafe, "unregistered", owner.ScopeOther)
	other.Owner, other.OwnerSource, other.Proof, other.Evidence = "codex", owner.SourcePathConvention, tidywizard.ProofAncestor, "no-work"
	other.Effects = []string{"remove-registration", "remove-files"}
	dec := cand("sourcesteward", tidywizard.KindWorktree, "/tmp/wt-dec", tidywizard.VerdictDecision, "locked-stw", owner.ScopeUnknown)
	dec.Evidence, dec.Effects = "stw lock whose creation record does not match", []string{"remove-registration", "remove-files"}
	dirty := cand("sourcesteward", tidywizard.KindWorktree, "/tmp/wt-041", tidywizard.VerdictRetain, "dirty", owner.ScopeOwn)
	dirty.Evidence = "uncommitted changes"
	branch := cand("bundledock", tidywizard.KindBranch, "codex/old-1", tidywizard.VerdictSafe, "", owner.ScopeOwn)
	branch.Proof, branch.Effects = tidywizard.ProofMergeNoop, []string{"delete-branch:codex/old-1"}
	hostile := cand("bundledock", tidywizard.KindWorktree, "/tmp/\x1b[2Jevil\r\x00\u202egnp.exe", tidywizard.VerdictRetain, "dirty", owner.ScopeUnknown)
	hostile.Owner, hostile.OwnerSource = "ow\x1bner\n", owner.SourceCommitAuthor
	hostile.Branch, hostile.Task = "br\x1b]0;title\x07anch", "ta​sk"
	hostile.Evidence = "ev\x1b[31mid\nence " + strings.Repeat("e", 600)
	long := cand("bundledock", tidywizard.KindBranch, strings.Repeat("n", 10000), tidywizard.VerdictRetain, "unmerged", owner.ScopeOwn)
	res := tidywizard.Result{
		Repositories: []tidywizard.RepoResult{
			{Name: "bundledock", State: "analysed", Notices: []string{"tracking-requires-online", "limit \x1b[31mreached\r\nnow\u202e " + strings.Repeat("ä", 200)}},
			{Name: "chartscribe", State: "skipped", Reason: "base-ambiguous", Notices: []string{}},
			{Name: "sheetmill", State: "error", Reason: "boom\x1b[0m\n" + strings.Repeat("x", 500), Notices: []string{}},
			{Name: "sourcesteward", State: "analysed", Notices: []string{}},
		},
		Candidates: []tidywizard.Candidate{branch, hostile, long, safe, other, dec, dirty},
	}
	return Input{
		Workspace: "/ws/\x1b[31mAnchorage",
		Derived:   []derive.Derived{{Name: "remote", Value: "origin", Source: "default"}, {Name: "state-dirs", Value: "/s (2 records)\x1b[0m", Source: "--state-dir", Overridden: true}},
		Repos: map[string]RepoMeta{
			"bundledock":    {Path: "/ws/bundledock", Backend: "git", Base: "refs/heads/main", Remote: "origin"},
			"chartscribe":   {Path: "/ws/chartscribe", Backend: "git"},
			"sheetmill":     {Path: "/ws/sheetmill", Backend: "jj"},
			"sourcesteward": {Path: "/ws/sourcesteward", Backend: "git", Base: "refs/heads/main\x1b[0m", Remote: "origin"},
		},
		Result:  res,
		Mode:    owner.Evidence{Mode: owner.ModeHuman, StdinTTY: true, StdoutTTY: true, Markers: []string{}, ByOwner: []string{}, Caller: "werner", CallerSource: "os-user"},
		Notices: []string{"run notice \x1b[1mbold\x1b[0m"},
	}
}

func TestTextGolden(t *testing.T) {
	golden(t, "overview.txt.golden", []byte(Text(Build(input()), TextOptions{})))
}

func TestJSONGolden(t *testing.T) {
	b, truncated, err := JSON(Build(input()))
	if err != nil || truncated {
		t.Fatal(err, truncated)
	}
	if !json.Valid(b) {
		t.Fatal("invalid JSON")
	}
	golden(t, "overview.json.golden", b)
}

// No control, format or bidirectional character reaches the terminal, whatever the field: every
// line of the text is printable apart from the newline, the notices pass through EscapedNotices.
func TestTextNeverPrintsRawControlCharacters(t *testing.T) {
	out := Text(Build(input()), TextOptions{})
	for i, line := range strings.Split(out, "\n") {
		for _, r := range line {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) || r == unicode.ReplacementChar {
				t.Errorf("line %d has U+%04X: %q", i, r, line)
			}
		}
		if n := len([]rune(line)); n > 1500 {
			t.Errorf("line %d is %d characters", i, n)
		}
	}
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\r") {
		t.Error("raw ESC or CR in the text")
	}
	for _, want := range []string{`limit \x1b[31mreached\r\nnow\u202e`, `\x1b[2Jevil`, `\u202egnp.exe`, `/ws/\x1b[31mAnchorage`} {
		if !strings.Contains(out, want) {
			t.Errorf("escaped form %q is missing", want)
		}
	}
}

// The text bounds each field to 120 characters before escaping and shows at most 10 rows per
// repository, then says how many more there are.
func TestTextBoundsRowsAndFields(t *testing.T) {
	in := input()
	in.Result.Candidates = nil
	for i := 0; i < 14; i++ {
		c := cand("bundledock", tidywizard.KindBranch, "b"+string(rune('a'+i)), tidywizard.VerdictRetain, "unmerged", owner.ScopeOwn)
		in.Result.Candidates = append(in.Result.Candidates, c)
	}
	out := Text(Build(in), TextOptions{})
	if n := strings.Count(out, "unmerged"); n != 10 {
		t.Errorf("%d rows shown, want 10", n)
	}
	if !strings.Contains(out, "and 4 more (use --json or details)") {
		t.Errorf("missing the more line:\n%s", out)
	}
	if got := Build(in); len(got.Candidates) != 14 {
		t.Errorf("JSON lists every candidate: %d", len(got.Candidates))
	}
}

// Only safe and decision rows get a number; the numbers follow the display order and are what
// the prompts choose from.
func TestNumbersAreForSafeAndDecisionRowsOnly(t *testing.T) {
	doc := Build(input())
	nums := Numbered(doc)
	if len(nums) != 4 {
		t.Fatalf("%+v", nums)
	}
	for i, n := range nums {
		if n.Number != i+1 || (n.Candidate.Verdict != tidywizard.VerdictSafe && n.Candidate.Verdict != tidywizard.VerdictDecision) {
			t.Errorf("%+v", n)
		}
	}
	out := Text(doc, TextOptions{})
	if strings.Count(out, "[1]") != 1 || strings.Contains(out, "[5]") {
		t.Errorf("numbering in the text:\n%s", out)
	}
	if !strings.Contains(out, "[-] retain") {
		t.Errorf("a retain row has no number:\n%s", out)
	}
}

func TestSummaryAndDocumentShape(t *testing.T) {
	doc := Build(input())
	if doc.Summary.Safe != 3 || doc.Summary.Retain != 3 || doc.Summary.Decision != 1 {
		t.Errorf("%+v", doc.Summary)
	}
	if doc.Command != "tidy-overview" || doc.SchemaVersion != 1 || doc.PlanID != "" {
		t.Errorf("%+v", doc)
	}
	for _, r := range doc.Repositories {
		if r.Name == "sheetmill" && (r.State != "error" || r.Backend != "jj") {
			t.Errorf("%+v", r)
		}
	}
	b, _, _ := JSON(doc)
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"workspace", "derived", "repositories", "candidates", "summary", "mode", "plan_id", "notices", "truncated"} {
		if _, ok := back[k]; !ok {
			t.Errorf("missing %q", k)
		}
	}
	// No array is ever null.
	if strings.Contains(string(b), ": null") {
		t.Errorf("null in the document:\n%s", b)
	}
}

// The document is bounded: a result over 4 MiB is cut to what fits, flagged truncated, and the
// caller learns of it (exit 1 output-limit).
func TestJSONIsBounded(t *testing.T) {
	in := input()
	in.Result.Candidates = nil
	for i := 0; i < tidywizard.MaxCandidates; i++ {
		c := cand("bundledock", tidywizard.KindBranch, strings.Repeat("b", 900)+string(rune('a'+i%26)), tidywizard.VerdictRetain, "unmerged", owner.ScopeOwn)
		c.ID += strings.Repeat("i", 200) + string(rune(0x4e00+i))
		c.Evidence = strings.Repeat("e", 512)
		in.Result.Candidates = append(in.Result.Candidates, c)
	}
	b, truncated, err := JSON(Build(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > MaxDocumentBytes {
		t.Errorf("document of %d bytes", len(b))
	}
	if !truncated {
		t.Fatal("not flagged as truncated")
	}
	var back Document
	if err := json.Unmarshal(b, &back); err != nil || !back.Truncated || len(back.Candidates) == 0 || len(back.Candidates) >= tidywizard.MaxCandidates {
		t.Errorf("%v truncated=%v candidates=%d", err, back.Truncated, len(back.Candidates))
	}
}

// Raw JSON shown on a terminal cannot carry bidirectional or invisible characters either; the
// document decodes to the very same strings.
func TestJSONEscapesInvisibleCharacters(t *testing.T) {
	in := input()
	b, _, err := JSON(Build(in))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range string(b) {
		if unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) || r > 0x7e && unicode.IsControl(r) {
			t.Errorf("raw U+%04X in the JSON", r)
		}
	}
	var back Document
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	want := Build(in)
	for i := range want.Candidates {
		if back.Candidates[i].Path != want.Candidates[i].Path || back.Candidates[i].Owner != want.Candidates[i].Owner {
			t.Errorf("candidate %d changed by the escape: %q", i, back.Candidates[i].Path)
		}
	}
	if back.Workspace != want.Workspace || back.Notices[1] != want.Notices[1] {
		t.Error("strings changed")
	}
}

// More candidates than the bound are cut and flagged, whatever the classification handed over.
func TestBuildBoundsTheCandidates(t *testing.T) {
	in := input()
	in.Result.Candidates = nil
	for i := 0; i < tidywizard.MaxCandidates+5; i++ {
		in.Result.Candidates = append(in.Result.Candidates, cand("bundledock", tidywizard.KindBranch, "b"+strings.Repeat("x", 3)+string(rune(0x4e00+i)), tidywizard.VerdictRetain, "unmerged", owner.ScopeOwn))
	}
	d := Build(in)
	if len(d.Candidates) != tidywizard.MaxCandidates || !d.Truncated {
		t.Errorf("%d candidates, truncated %v", len(d.Candidates), d.Truncated)
	}
}

// An astral format character (a tag character, an invisible musical symbol) is written as a UTF-16
// surrogate pair, so the JSON decodes to exactly the input string.
func TestJSONRoundTripsAstralFormatCharacters(t *testing.T) {
	const astral = "\U000e0041\U000e007f\U0001d173x\u202e"
	in := input()
	in.Workspace = "/ws/" + astral
	c := cand("bundledock", tidywizard.KindWorktree, "/tmp/"+astral, tidywizard.VerdictSafe, "", owner.ScopeOwn)
	c.Branch = "br" + astral
	in.Result.Candidates = append(in.Result.Candidates, c)
	b, _, err := JSON(Build(in))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range string(b) {
		if r > 0xffff {
			t.Errorf("raw astral U+%X in the JSON", r)
		}
	}
	var back Document
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Workspace != in.Workspace {
		t.Errorf("workspace %q", back.Workspace)
	}
	found := false
	for _, got := range back.Candidates {
		if got.ID == c.ID {
			found = true
			if got.Path != c.Path || got.Branch != c.Branch {
				t.Errorf("path %q branch %q", got.Path, got.Branch)
			}
		}
	}
	if !found {
		t.Errorf("the candidate id %q did not round-trip", c.ID)
	}
}

// The summary counts every analysed candidate, before any cut; `truncated` says the list is partial.
func TestSummaryCountsEveryCandidateEvenWhenTheListIsCut(t *testing.T) {
	in := input()
	in.Result.Candidates = nil
	total := tidywizard.MaxCandidates + 5
	for i := 0; i < total; i++ {
		in.Result.Candidates = append(in.Result.Candidates, cand("bundledock", tidywizard.KindBranch, "b"+string(rune(0x4e00+i)), tidywizard.VerdictRetain, "unmerged", owner.ScopeOwn))
	}
	d := Build(in)
	if d.Summary.Retain != total || len(d.Candidates) != tidywizard.MaxCandidates || !d.Truncated {
		t.Errorf("4096 cap: retain %d candidates %d truncated %v", d.Summary.Retain, len(d.Candidates), d.Truncated)
	}
	// The 4 MiB cut keeps the summary of the whole analysis too.
	in.Result.Candidates = nil
	for i := 0; i < tidywizard.MaxCandidates; i++ {
		c := cand("bundledock", tidywizard.KindBranch, strings.Repeat("b", 900)+string(rune(0x4e00+i)), tidywizard.VerdictRetain, "unmerged", owner.ScopeOwn)
		c.Evidence = strings.Repeat("e", 512)
		in.Result.Candidates = append(in.Result.Candidates, c)
	}
	b, truncated, err := JSON(Build(in))
	if err != nil || !truncated {
		t.Fatal(err, truncated)
	}
	var back Document
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Summary.Retain != tidywizard.MaxCandidates || len(back.Candidates) >= tidywizard.MaxCandidates {
		t.Errorf("after the 4 MiB cut: summary %+v, candidates %d", back.Summary, len(back.Candidates))
	}
}

// A run-level notice that itself starts with "<repo>: " is still shown, and once.
func TestRunNoticeLookingLikeARepositoryNoticeIsShown(t *testing.T) {
	in := input()
	in.Notices = []string{"bundledock: a run notice that merely looks like a repository notice"}
	out := Text(Build(in), TextOptions{})
	if n := strings.Count(out, "a run notice that merely looks like a repository notice"); n != 1 {
		t.Errorf("shown %d times:\n%s", n, out)
	}
	if !strings.Contains(out, "notice: bundledock: a run notice") {
		t.Errorf("not shown as a run notice:\n%s", out)
	}
}

// The agent markers are outside text too: they are escaped in the mode line, and a nil list is an
// empty array in the document.
func TestModeMarkersAreEscapedAndNeverNull(t *testing.T) {
	in := input()
	in.Mode.Markers = []string{"CLAUDE_CODE_\x1b[31mX", "CODEX_\u202e"}
	out := Text(Build(in), TextOptions{})
	if !strings.Contains(out, `agent marker CLAUDE_CODE_\x1b[31mX, CODEX_\u202e`) || strings.Contains(out, "\x1b") {
		t.Errorf("markers not escaped:\n%s", out)
	}
	in = input()
	in.Mode.Markers, in.Mode.ByOwner = nil, nil
	b, _, err := JSON(Build(in))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"markers": []`) || !strings.Contains(string(b), `"by_owner": []`) {
		t.Errorf("nil lists in the mode:\n%s", b)
	}
}

// lineIndex is the index of the first line of out that contains sub, or -1.
func lineIndex(out, sub string) int {
	for i, l := range strings.Split(out, "\n") {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

// FR-006a, SC-010: candidates are listed under one heading per owner, own scope first, then the
// other owners sorted by label, `unknown` last; inside a group, under a sub-heading per repository.
func TestTextGroupsCandidatesByOwner(t *testing.T) {
	out := Text(Build(input()), TextOptions{})
	own, codex, unknown := lineIndex(out, "owner claude (own)"), lineIndex(out, "owner codex (other)"), lineIndex(out, "owner unknown")
	if own < 0 || codex < 0 || unknown < 0 || !(own < codex && codex < unknown) {
		t.Fatalf("group headings missing or out of order (%d %d %d):\n%s", own, codex, unknown, out)
	}
	for sub, heading := range map[string]int{"codex/old-1": own, "/tmp/wt-037": own, "/tmp/wt-041": own, "/tmp/whr010-a": codex, "/tmp/wt-dec": unknown, `\u202egnp.exe`: unknown} {
		got := lineIndex(out, sub)
		next := len(strings.Split(out, "\n"))
		for _, h := range []int{own, codex, unknown} {
			if h > heading && h < next {
				next = h
			}
		}
		if got < heading || got >= next {
			t.Errorf("%q at line %d, want inside its group (%d..%d):\n%s", sub, got, heading, next, out)
		}
	}
	// Repository sub-headings inside the own group, indented under the heading.
	if !strings.Contains(out, "\n  bundledock\n") || !strings.Contains(out, "\n  sourcesteward\n") {
		t.Errorf("repository sub-headings missing:\n%s", out)
	}
}

// The per-repository lines (base, notices, skipped, error, no candidates) come in a block before
// the owner groups, one entry per repository, so that coverage stays visible.
func TestTextRepositoriesBlockPrecedesOwnerGroups(t *testing.T) {
	out := Text(Build(input()), TextOptions{})
	block := lineIndex(out, "Repositories:")
	first := lineIndex(out, "owner claude (own)")
	if block < 0 || first < 0 || block > first {
		t.Fatalf("repositories block missing or after the groups:\n%s", out)
	}
	for _, want := range []string{"chartscribe   skipped: base-ambiguous", "sheetmill   error: boom", "  notice: tracking-requires-online"} {
		i := lineIndex(out, want)
		if i < block || i > first {
			t.Errorf("%q at line %d, want between %d and %d:\n%s", want, i, block, first, out)
		}
	}
}

// The order does not depend on the order of the candidates (deterministic output).
func TestTextOrderIsIndependentOfCandidateOrder(t *testing.T) {
	in := input()
	extra := cand("bundledock", tidywizard.KindBranch, "alpha/x", tidywizard.VerdictSafe, "", owner.ScopeOther)
	extra.Owner = "alpha"
	in.Result.Candidates = append(in.Result.Candidates, extra)
	want := Text(Build(in), TextOptions{})
	if a, c := lineIndex(want, "owner alpha (other)"), lineIndex(want, "owner codex (other)"); a < 0 || c < 0 || a > c {
		t.Fatalf("other owners are not sorted by label:\n%s", want)
	}
	n := len(in.Result.Candidates)
	for shift := 1; shift < n; shift++ {
		p := tidywizardResultRotated(in, shift)
		if got := Text(Build(p), TextOptions{}); got != want {
			// Rotation keeps the relative order inside a repository only per rotation, so compare
			// the group headings and the multiset of lines instead of the byte string.
			if sortedLines(got) != sortedLines(want) {
				t.Errorf("rotation %d changes the lines:\n%s", shift, got)
			}
			if headings(got) != headings(want) {
				t.Errorf("rotation %d changes the headings: %q vs %q", shift, headings(got), headings(want))
			}
		}
	}
	if Text(Build(in), TextOptions{}) != want {
		t.Error("two renders of the same input differ")
	}
}

func tidywizardResultRotated(in Input, shift int) Input {
	c := append([]tidywizard.Candidate{}, in.Result.Candidates...)
	c = append(c[shift:], c[:shift]...)
	in.Result.Candidates = c
	return in
}

func sortedLines(s string) string {
	l := strings.Split(s, "\n")
	for i := range l {
		l[i] = strings.TrimLeft(strings.Join(strings.Fields(l[i]), " "), "[0123456789-]")
	}
	sort.Strings(l)
	return strings.Join(l, "\n")
}

func headings(s string) string {
	var h []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "owner ") {
			h = append(h, l)
		}
	}
	return strings.Join(h, "|")
}

// The bound of 10 rows is per repository inside each owner group; the `and N more` line belongs
// to that sub-heading.
func TestTextBoundsRowsPerRepositoryInsideOwnerGroup(t *testing.T) {
	in := input()
	in.Result.Candidates = nil
	for i := 0; i < 12; i++ {
		in.Result.Candidates = append(in.Result.Candidates, cand("bundledock", tidywizard.KindBranch, "own"+string(rune('a'+i)), tidywizard.VerdictRetain, "unmerged", owner.ScopeOwn))
		c := cand("sourcesteward", tidywizard.KindBranch, "own"+string(rune('a'+i)), tidywizard.VerdictRetain, "unmerged", owner.ScopeOwn)
		in.Result.Candidates = append(in.Result.Candidates, c)
		o := cand("bundledock", tidywizard.KindBranch, "oth"+string(rune('a'+i)), tidywizard.VerdictRetain, "unmerged", owner.ScopeOther)
		o.Owner = "codex"
		in.Result.Candidates = append(in.Result.Candidates, o)
	}
	out := Text(Build(in), TextOptions{})
	if n := strings.Count(out, "unmerged"); n != 30 {
		t.Errorf("%d rows shown, want 30 (10 for each of 3 repository groups)", n)
	}
	if n := strings.Count(out, "    and 2 more (use --json or details)"); n != 3 {
		t.Errorf("%d more lines, want 3:\n%s", n, out)
	}
}

// The numbers of the prompts (Numbered) are the numbers in the text, in the order of the text.
func TestNumbersFollowTheTextOrder(t *testing.T) {
	in := input()
	extra := cand("bundledock", tidywizard.KindBranch, "alpha/x", tidywizard.VerdictSafe, "", owner.ScopeOther)
	extra.Owner = "alpha"
	late := cand("bundledock", tidywizard.KindBranch, "late-own", tidywizard.VerdictSafe, "", owner.ScopeOwn)
	in.Result.Candidates = append(in.Result.Candidates, extra, late)
	doc := Build(in)
	out := Text(doc, TextOptions{})
	last := -1
	for _, n := range Numbered(doc) {
		tag := "[" + strconv.Itoa(n.Number) + "] "
		line := lineIndex(out, tag)
		if line < 0 || line <= last {
			t.Fatalf("number %d at line %d after %d:\n%s", n.Number, line, last, out)
		}
		last = line
		subject := n.Candidate.Path
		if subject == "" {
			subject = n.Candidate.Branch
		}
		if !strings.Contains(strings.Split(out, "\n")[line], subject) {
			t.Errorf("number %d is not row %q:\n%s", n.Number, subject, out)
		}
	}
	if lineIndex(out, "late-own") > lineIndex(out, "owner alpha (other)") {
		t.Errorf("an own row after the other owners:\n%s", out)
	}
}

// The JSON lists the candidates in the order of the analysis, not regrouped, whatever the text does.
func TestJSONKeepsTheAnalysisOrder(t *testing.T) {
	in := input()
	doc := Build(in)
	_ = Text(doc, TextOptions{})
	for i, c := range in.Result.Candidates {
		if doc.Candidates[i].ID != c.ID {
			t.Errorf("candidate %d is %s, want %s", i, doc.Candidates[i].ID, c.ID)
		}
	}
}

// The label is escaped and cut alone; the scope suffix is appended afterwards, so a long label
// cannot cut it off and a label cannot fake it.
func TestOwnerHeadingKeepsTheScopeSuffix(t *testing.T) {
	in := input()
	in.Result.Candidates = nil
	long := cand("bundledock", tidywizard.KindBranch, "a", tidywizard.VerdictSafe, "", owner.ScopeOwn)
	long.Owner = strings.Repeat("L", 200)
	fake := cand("bundledock", tidywizard.KindBranch, "b", tidywizard.VerdictSafe, "", owner.ScopeOther)
	fake.Owner = "mallory (own)"
	in.Result.Candidates = append(in.Result.Candidates, long, fake)
	var headings []string
	for _, l := range strings.Split(Text(Build(in), TextOptions{}), "\n") {
		if strings.HasPrefix(l, "owner ") {
			headings = append(headings, l)
		}
	}
	if len(headings) != 2 || !strings.HasSuffix(headings[0], "... (own)") || len(headings[0]) > 140 {
		t.Fatalf("own heading: %q", headings)
	}
	if headings[1] != "owner mallory (own) (other)" {
		t.Errorf("a label with scope text keeps its real scope last: %q", headings[1])
	}
}

// Other owners are sorted by label, not by first appearance; the same label in two scopes gives
// two groups, the own scope first.
func TestOwnerGroupOrderIsByLabelAndScope(t *testing.T) {
	in := input()
	in.Result.Candidates = nil
	for _, l := range []string{"zeta", "mid", "alpha"} {
		c := cand("bundledock", tidywizard.KindBranch, l+"/x", tidywizard.VerdictRetain, "unmerged", owner.ScopeOther)
		c.Owner = l
		in.Result.Candidates = append(in.Result.Candidates, c)
	}
	same := cand("bundledock", tidywizard.KindBranch, "own/x", tidywizard.VerdictRetain, "unmerged", owner.ScopeOwn)
	same.Owner = "mid"
	in.Result.Candidates = append(in.Result.Candidates, same)
	if got := headings(Text(Build(in), TextOptions{})); got != "owner mid (own)|owner alpha (other)|owner mid (other)|owner zeta (other)" {
		t.Errorf("%s", got)
	}
}

// Items that were not judged because of the deadline are stated once as a run notice, in the text
// and in the existing `notices` of the JSON, with a retry hint; none, no notice.
func TestDeadlineNoticeCountsTheItemsNotJudged(t *testing.T) {
	in := input()
	base := len(Build(in).Notices)
	if strings.Contains(strings.Join(Build(in).Notices, "|"), "not judged") {
		t.Fatal("a notice without a deadline")
	}
	for i := 0; i < 2; i++ {
		c := cand("sourcesteward", tidywizard.KindWorktree, "/tmp/wt-late-"+string(rune('a'+i)), tidywizard.VerdictRetain, "evidence-incomplete", owner.ScopeUnknown)
		c.Evidence = "deadline-exceeded"
		in.Result.Candidates = append(in.Result.Candidates, c)
	}
	budget := cand("sourcesteward", tidywizard.KindBranch, "late-proof", tidywizard.VerdictRetain, "evidence-incomplete", owner.ScopeUnknown)
	budget.Evidence = "proof-budget-exhausted" // the proof phase ran out: --timeout widens it, so it counts
	in.Result.Candidates = append(in.Result.Candidates, budget)
	other := cand("sourcesteward", tidywizard.KindWorktree, "/tmp/wt-other", tidywizard.VerdictRetain, "evidence-incomplete", owner.ScopeUnknown)
	other.Evidence = "registration-mismatch" // incomplete for another reason: not counted
	in.Result.Candidates = append(in.Result.Candidates, other)
	doc := Build(in)
	want := "3 of 11 not judged (deadline or proof budget); retry with a larger window, for example --timeout 15m"
	if len(doc.Notices) != base+1 || doc.Notices[len(doc.Notices)-1] != want {
		t.Fatalf("notices %q, want the last to be %q", doc.Notices, want)
	}
	if !strings.Contains(Text(doc, TextOptions{}), "notice: "+want+"\n") {
		t.Errorf("text lacks the notice:\n%s", Text(doc, TextOptions{}))
	}
	b, _, _ := JSON(doc)
	if !strings.Contains(string(b), want) {
		t.Errorf("JSON lacks the notice")
	}
	if doc.Summary.Retain != 3+4 {
		t.Errorf("the verdicts changed: %+v", doc.Summary)
	}
}
