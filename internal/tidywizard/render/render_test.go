// SPDX-License-Identifier: EUPL-1.2

package render

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
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
