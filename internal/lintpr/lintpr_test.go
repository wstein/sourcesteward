// SPDX-License-Identifier: EUPL-1.2
package lintpr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Claude session patterns are assembled so that this file does not contain them in plain text.
var (
	sessionTrailer = "Claude" + "-Session: " + "https://example.invalid/x"
	sessionURL     = "see https://claude" + ".ai/code/session_" + "01ABC for details"
)

const (
	headSHA = "0123456789abcdef0123456789abcdef01234567"
	patchID = "89abcdef0123456789abcdef0123456789abcdef"
	owner   = "Octo Owner"
	ownerM  = "octo@example.com"
)

func cfg() Config { return Config{Owners: []Identity{{Name: owner, Email: ownerM}}} }

func goodCommit() Commit {
	return Commit{SHA: strings.Repeat("a", 40), Message: "feat(lint): add the thing\n\nBody.\n\nCo-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>",
		AuthorName: owner, AuthorEmail: ownerM, CommitterName: owner, CommitterEmail: ownerM}
}

func goodBody() string {
	return "Adds the thing.\n\nChanges\n- one\n\nTests: 3 passed\n\nNot verified: nothing\nBlocker: Independent review pending\nCloses #1\n\nAuthor agent: Claude"
}

func goodInput() Input {
	return Input{PR: 7, Title: "feat: x", Body: goodBody(), Commits: []Commit{goodCommit()}}
}

func ids(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	return out
}

func has(fs []Finding, rule string) bool {
	for _, f := range fs {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func TestCleanInputHasNoFindings(t *testing.T) {
	if fs := Lint(goodInput(), cfg()); len(fs) != 0 {
		t.Fatalf("findings on clean input: %+v", fs)
	}
}

func TestSessionLinkEveryLocation(t *testing.T) {
	for _, bad := range []string{sessionTrailer, sessionURL, strings.ToUpper(sessionTrailer)} {
		in := goodInput()
		in.Commits[0].Message += "\n" + bad
		in.Body += "\n" + bad
		in.Title += " " + bad
		in.Comments = []Comment{{ID: 99, Body: "Review ok\n" + bad}}
		fs := Lint(in, cfg())
		kinds := map[string]string{}
		for _, f := range fs {
			if f.Rule == RuleSessionLink {
				kinds[f.Location.Kind] = f.Location.ID
			}
		}
		if kinds[LocCommit] != strings.Repeat("a", 40) || kinds[LocComment] != "99" || kinds[LocPRBody] != "" || len(kinds) != 4 {
			t.Errorf("%q: locations %v", bad, kinds)
		}
	}
}

func TestSessionLinkReportsLine(t *testing.T) {
	in := goodInput()
	in.Body = "a\nb\n" + sessionURL
	for _, f := range Lint(in, cfg()) {
		if f.Rule == RuleSessionLink && f.Location.Line != 3 {
			t.Fatalf("line %d, want 3", f.Location.Line)
		}
	}
}

func TestIdentity(t *testing.T) {
	in := goodInput()
	in.Commits[0].AuthorName = "Someone Else"
	fs := Lint(in, cfg())
	if !has(fs, RuleAuthorIdentity) || has(fs, RuleCommitterIdentity) {
		t.Fatalf("author only: %v", ids(fs))
	}
	in = goodInput()
	in.Commits[0].CommitterEmail = "bot@example.com"
	fs = Lint(in, cfg())
	if has(fs, RuleAuthorIdentity) || !has(fs, RuleCommitterIdentity) {
		t.Fatalf("committer only: %v", ids(fs))
	}
	in = goodInput()
	in.Commits[0].AuthorEmail = strings.ToUpper(ownerM)
	if fs := Lint(in, cfg()); has(fs, RuleAuthorIdentity) {
		t.Fatalf("email is case-insensitive: %v", ids(fs))
	}
	in = goodInput()
	in.Commits[0].AuthorName = strings.ToUpper(owner)
	if fs := Lint(in, cfg()); !has(fs, RuleAuthorIdentity) {
		t.Fatalf("name is exact")
	}
	// A second configured owner identity is accepted.
	in = goodInput()
	in.Commits[0].AuthorName, in.Commits[0].AuthorEmail = "Alt", "alt@example.com"
	c := cfg()
	c.Owners = append(c.Owners, Identity{Name: "Alt", Email: "alt@example.com"})
	if fs := Lint(in, c); has(fs, RuleAuthorIdentity) {
		t.Fatalf("second identity: %v", ids(fs))
	}
}

func TestNoOwnerConfiguredSkipsIdentityRules(t *testing.T) {
	if fs := Lint(goodInput(), Config{}); has(fs, RuleAuthorIdentity) || has(fs, RuleCommitterIdentity) {
		t.Fatalf("identity rules ran without an owner: %v", ids(fs))
	}
}

func TestCoAuthoredBy(t *testing.T) {
	for _, msg := range []string{
		"feat: x\n\nBody only.",
		"feat: x\n\nCo-Authored-By: no email here",
		"feat: x\n\nsee Co-Authored-By: Name <a@b.c> inline",
	} {
		in := goodInput()
		in.Commits[0].Message = msg
		if !has(Lint(in, cfg()), RuleCoAuthoredBy) {
			t.Errorf("%q passed", msg)
		}
	}
	in := goodInput()
	in.Commits[0].Message = "feat: x\n\nco-authored-by: Name <a@b.c>\r\n"
	if has(Lint(in, cfg()), RuleCoAuthoredBy) {
		t.Errorf("lower-case trailer with CRLF refused")
	}
}

func TestConventionalCommit(t *testing.T) {
	bad := []string{"Add the thing", "feat add: x", "feat(): x", "feature: x", "feat:x", "fix(scope) x", "feat: ", "Feat: x", "Add feat: x", "WIP fix(a): x"}
	for _, subject := range bad {
		in := goodInput()
		in.Commits[0].Message = subject + "\n\nCo-Authored-By: C <c@d.e>"
		if !has(Lint(in, cfg()), RuleConventionalCommit) {
			t.Errorf("%q passed", subject)
		}
	}
	for _, subject := range []string{"fix: x", "docs(spec): x", "test(ci)!: x", "refactor(a/b-c.d): x", "chore: x"} {
		in := goodInput()
		in.Commits[0].Message = subject + "\n\nCo-Authored-By: C <c@d.e>"
		if has(Lint(in, cfg()), RuleConventionalCommit) {
			t.Errorf("%q refused", subject)
		}
	}
}

func lines(n int) string {
	var b []string
	for i := 0; i < n; i++ {
		b = append(b, fmt.Sprintf("line %d", i))
	}
	return strings.Join(b, "\n")
}

func TestBodyLength(t *testing.T) {
	base := "Blocker: x\nAuthor agent: Claude\n"
	check := func(total int, want Severity) {
		t.Helper()
		in := goodInput()
		in.Body = base + lines(total-2)
		var got Severity
		for _, f := range Lint(in, cfg()) {
			if f.Rule == RuleBodyLength {
				got = f.Severity
			}
		}
		if got != want {
			t.Errorf("%d lines: severity %q, want %q", total, got, want)
		}
	}
	check(25, "")
	check(26, SeverityWarning)
	check(30, SeverityWarning)
	check(31, SeverityError)
	// Trailing blank lines and CRLF do not count.
	in := goodInput()
	in.Body = strings.ReplaceAll(base+lines(23), "\n", "\r\n") + "\r\n\r\n\r\n"
	if has(Lint(in, cfg()), RuleBodyLength) {
		t.Errorf("trailing blank lines counted")
	}
}

func TestBlockerAndAuthorAgentLines(t *testing.T) {
	in := goodInput()
	in.Body = "Purpose.\nAuthor agent: Claude"
	fs := Lint(in, cfg())
	if !has(fs, RuleBlockerLine) || has(fs, RuleAuthorAgentLine) {
		t.Errorf("blocker missing: %v", ids(fs))
	}
	in.Body = "Purpose.\nBlocker: keiner"
	fs = Lint(in, cfg())
	if has(fs, RuleBlockerLine) || !has(fs, RuleAuthorAgentLine) {
		t.Errorf("author agent missing: %v", ids(fs))
	}
	in.Body = "Purpose mentions blocker in prose.\nan Author agent: later in a line"
	fs = Lint(in, cfg())
	if !has(fs, RuleBlockerLine) || !has(fs, RuleAuthorAgentLine) {
		t.Errorf("mid-line mentions counted: %v", ids(fs))
	}
	in.Body = "**Blocker**: none\n- Author agent: Claude"
	if fs = Lint(in, cfg()); has(fs, RuleBlockerLine) || has(fs, RuleAuthorAgentLine) {
		t.Errorf("markdown prefixes refused: %v", ids(fs))
	}
}

func TestReviewLineFormat(t *testing.T) {
	good := fmt.Sprintf("Review: CLEAR head=%s patch-id=%s pr=7", headSHA, patchID)
	carried := fmt.Sprintf("Carried-over: from=%s to=%s patch-id=%s pr=7", headSHA, strings.Repeat("b", 40), patchID)
	for _, body := range []string{good, good + "\nrest", carried, "Review: FINDINGS" + good[len("Review: CLEAR"):]} {
		in := goodInput()
		in.Comments = []Comment{{ID: 1, Body: body}}
		if fs := Lint(in, cfg()); len(fs) != 0 {
			t.Errorf("%q: %+v", body, fs)
		}
	}
	bad := map[string]string{
		"markdown":       "**" + good + "**",
		"colon only":     "Review: CLEAR",
		"short sha":      "Review: CLEAR head=abc patch-id=" + patchID + " pr=7",
		"upper hex":      "Review: CLEAR head=" + strings.ToUpper(headSHA) + " patch-id=" + patchID + " pr=7",
		"order":          fmt.Sprintf("Review: CLEAR patch-id=%s head=%s pr=7", patchID, headSHA),
		"extra":          good + " extra",
		"crlf":           good + "\r\nrest",
		"wrong pr":       strings.Replace(good, "pr=7", "pr=8", 1),
		"verdict":        strings.Replace(good, "CLEAR", "OK", 1),
		"patch short":    fmt.Sprintf("Review: CLEAR head=%s patch-id=%s pr=7", headSHA, patchID[:20]),
		"carried order":  fmt.Sprintf("Carried-over: to=%s from=%s patch-id=%s pr=7", headSHA, headSHA, patchID),
		"long sha":       fmt.Sprintf("Review: CLEAR head=%s0 patch-id=%s pr=7", headSHA, patchID),
		"long patch":     fmt.Sprintf("Review: CLEAR head=%s patch-id=%s0 pr=7", headSHA, patchID),
		"bold key":       "**Review**: CLEAR head=" + headSHA,
		"no colon":       "Review CLEAR head=" + headSHA,
		"no colon FIND":  "review findings",
		"heading only":   "## Review",
		"heading colon":  "### Review:",
		"carried bold":   "**Carried-over**: from=" + headSHA,
		"leading blank":  "\n" + good,
		"leading space":  " " + good,
		"lowercase word": strings.Replace(good, "Review", "review", 1),
	}
	for name, body := range bad {
		in := goodInput()
		in.Comments = []Comment{{ID: 5, Body: body}}
		var found bool
		for _, f := range Lint(in, cfg()) {
			if f.Rule == RuleReviewLine && f.Location.Kind == LocComment && f.Location.ID == "5" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: %q passed", name, body)
		}
	}
	// A comment that is not a review is not checked.
	in := goodInput()
	in.Comments = []Comment{{ID: 2, Body: "Thanks, looks fine\nReview: later"}, {ID: 3, Body: "Review of the approach is below."}}
	if fs := Lint(in, cfg()); len(fs) != 0 {
		t.Errorf("non-review comments: %+v", fs)
	}
}

func TestReviewCommentLength(t *testing.T) {
	first := fmt.Sprintf("Review: CLEAR head=%s patch-id=%s pr=7\n", headSHA, patchID)
	for n, want := range map[int]bool{15: false, 16: true, 58: true} {
		in := goodInput()
		in.Comments = []Comment{{ID: 4, Body: first + lines(n-1)}}
		if got := has(Lint(in, cfg()), RuleReviewLength); got != want {
			t.Errorf("%d lines: finding=%v want %v", n, got, want)
		}
	}
	// A long comment that is not a review is not limited.
	in := goodInput()
	in.Comments = []Comment{{ID: 4, Body: lines(60)}}
	if has(Lint(in, cfg()), RuleReviewLength) {
		t.Errorf("non-review comment limited")
	}
	// The other spellings of a review comment are limited too.
	for _, key := range []string{"**Review**: CLEAR", "Review CLEAR", "## Review", "review findings", "**Carried-over**: x"} {
		in.Comments = []Comment{{ID: 4, Body: key + "\n" + lines(20)}}
		if !has(Lint(in, cfg()), RuleReviewLength) {
			t.Errorf("%q: long review not limited", key)
		}
	}
	// A malformed review line is still a review comment for the length rule.
	in.Comments = []Comment{{ID: 4, Body: "Review: CLEAR\n" + lines(20)}}
	if fs := Lint(in, cfg()); !has(fs, RuleReviewLength) || !has(fs, RuleReviewLine) {
		t.Errorf("malformed long review: %v", ids(fs))
	}
}

func TestParseReviewLine(t *testing.T) {
	l, ok := ParseReviewLine(fmt.Sprintf("Review: FINDINGS head=%s patch-id=%s pr=12", headSHA, patchID))
	if !ok || l.Verdict != "FINDINGS" || l.Head != headSHA || l.PatchID != patchID || l.PR != 12 || l.Carried {
		t.Fatalf("%+v %v", l, ok)
	}
	l, ok = ParseReviewLine(fmt.Sprintf("Carried-over: from=%s to=%s patch-id=%s pr=3", headSHA, strings.Repeat("c", 40), patchID))
	if !ok || !l.Carried || l.From != headSHA || l.To != strings.Repeat("c", 40) || l.PR != 3 {
		t.Fatalf("%+v %v", l, ok)
	}
	if _, ok := ParseReviewLine("Review: CLEAR head=x patch-id=y pr=1"); ok {
		t.Fatal("accepted")
	}
	if _, ok := ParseReviewLine("Review: CLEAR head=" + headSHA + " patch-id=" + patchID + " pr=0"); ok {
		t.Fatal("pr=0 accepted")
	}
}

func TestFindingsAreOrderedByRuleTable(t *testing.T) {
	in := goodInput()
	in.Body = "x"
	in.Commits[0].Message = "bad subject"
	in.Commits[0].AuthorName = "Other"
	order := map[string]int{}
	for i, r := range Rules {
		order[r.ID] = i
	}
	last := -1
	for _, f := range Lint(in, cfg()) {
		if order[f.Rule] < last {
			t.Fatalf("not ordered: %v", ids(Lint(in, cfg())))
		}
		last = order[f.Rule]
	}
}

// The rule table follows the agent-rules core. Every rule names its source file and an exact anchor phrase of
// that file. With AGENT_RULES_DIR (a checkout of agent-rules) the anchors are looked up; without it the
// check is skipped, so a change of the core is noticed on the next run that has the checkout.
func TestRulesTableShape(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Rules {
		if r.ID == "" || r.Summary == "" || r.Source == "" || seen[r.ID] {
			t.Errorf("incomplete or duplicate rule %+v", r)
		}
		seen[r.ID] = true
		if r.Severity != SeverityError && r.Severity != SeverityWarning {
			t.Errorf("%s: severity %q", r.ID, r.Severity)
		}
		if r.Source != SourceRepo && r.Anchor == "" {
			t.Errorf("%s: a core rule needs an anchor", r.ID)
		}
	}
	if ByID(RuleReviewLength).Limit != 15 || ByID(RuleBodyLength).Limit != 25 || ByID(RuleBodyLength).Hard != 30 {
		t.Errorf("limits not in the table")
	}
}

func TestRulesTableAnchorsInAgentRules(t *testing.T) {
	dir := os.Getenv("AGENT_RULES_DIR")
	if dir == "" {
		t.Skip("AGENT_RULES_DIR is not set; set it to a checkout of agent-rules to verify the rule anchors")
	}
	for _, r := range Rules {
		if r.Source == SourceRepo {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, r.Source))
		if err != nil {
			t.Fatalf("%s: %v", r.ID, err)
		}
		if !strings.Contains(string(raw), r.Anchor) {
			t.Errorf("%s: anchor %q not found in %s", r.ID, r.Anchor, r.Source)
		}
	}
}
