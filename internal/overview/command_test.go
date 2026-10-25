// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"strings"
	"testing"
)

func TestShellWord(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"plain/path-1.2_x", "plain/path-1.2_x", true},
		{"with space", "'with space'", true},
		{"it's", `'it'\''s'`, true},
		{"$(rm -rf ~)", "'$(rm -rf ~)'", true},
		{"a;b|c&d`e`", "'a;b|c&d`e`'", true},
		{"ünï", "'ünï'", true},
		{"", "", false},
		{"-rf", "", false},
		{"--upload-pack=x", "", false},
		{"line\nbreak", "", false},
		{"tab\there", "", false},
		{"nul\x00byte", "", false},
		{"esc\x1b[31m", "", false},
		{"bidi‮evil", "", false},
		{"line" + string(rune(0x2028)) + "sep", "", false},
		{"bad\xffutf8", "", false},
	}
	for _, c := range cases {
		got, ok := shellWord(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("shellWord(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestRefWord(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"main", "main", true},
		{"bdock/v0.1.0", "bdock/v0.1.0", true},
		{"v1.0.0+meta", "'v1.0.0+meta'", true},
		{"semi;colon", "'semi;colon'", true},
		{"$(rm)", "'$(rm)'", true},
		{"it's", `'it'\''s'`, true},
		{"ünï", "'ünï'", true},
		{"", "", false},
		{"-x", "", false},
		{".hidden/x", "", false},
		{"a..b", "", false},
		{"a b", "", false},
		{"a:b", "", false},
		{"a\nb", "", false},
		{"deadbeef1", "", false},
		{strings.Repeat("a", 256), "", false},
	}
	for _, c := range cases {
		got, ok := refWord(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("refWord(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestIsSHA(t *testing.T) {
	if !isSHA(strings.Repeat("a", 40)) || !isSHA(strings.Repeat("0", 64)) {
		t.Error("valid object ids refused")
	}
	for _, s := range []string{"", "abc", strings.Repeat("A", 40), strings.Repeat("g", 40), strings.Repeat("a", 41)} {
		if isSHA(s) {
			t.Errorf("isSHA(%q) = true", s)
		}
	}
}

func TestHTTPSURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/o/r/pull/1":          "https://github.com/o/r/pull/1",
		"http://github.com/o/r":                  "",
		"javascript:alert(1)":                    "",
		"https://x/\x1b[31m":                     "",
		"https://x/a b":                          "",
		"https://x/\n":                           "",
		"":                                       "",
		"https://x/" + strings.Repeat("a", 2048): "",
	}
	for in, want := range cases {
		if got := HTTPSURL(in); got != want {
			t.Errorf("HTTPSURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildCommand(t *testing.T) {
	sha := strings.Repeat("a", 40)
	const repo = "wstein/sourcesteward"
	base := target{Slug: repo, Path: "crewbook", Root: "/ws", Number: 7, Tag: "v1.0.0", Branch: "main", SHA: sha, Worktree: "/ws/wt"}
	cases := []struct {
		cat    Category
		target target
		cmd    string
		effect Effect
	}{
		{RepoError, base, "stw status --offline crewbook", Read},
		{LocalOperation, base, "git -C crewbook status", Read},
		{MainRed, base, "gh run list --repo wstein/sourcesteward --branch main", Read},
		{ReleaseRunFailed, base, "gh run list --repo wstein/sourcesteward --commit " + sha, Read},
		{ReadError, withSource(base, SourcePulls), "stw pulls --repo wstein/sourcesteward", Read},
		{ReadError, withSource(base, SourceTags), "", ""},
		{LocalChanges, base, "git -C crewbook status", Read},
		{LocalDetached, base, "git -C crewbook status", Read},
		{LocalUntracked, base, "git -C crewbook status", Read},
		{LocalBehind, base, "stw sync --plan crewbook", Read},
		{LocalNoUpstream, base, "git -C crewbook branch -vv", Read},
		{MainBehindOrigin, base, "stw sync --plan crewbook", Read},
		{GitlinkDrift, base, "stw status --offline crewbook", Read},
		{WorktreeDirty, base, "git -C /ws/wt status", Read},
		{PRConflict, base, "stw pulls --repo wstein/sourcesteward", Read},
		{PRBlocked, base, "stw pulls --repo wstein/sourcesteward", Read},
		{TagWithoutRelease, base, "", ""},
		{PRReady, base, "gh pr merge 7 -R wstein/sourcesteward --rebase", Owner},
		{ReleaseDraft, base, "gh release edit v1.0.0 -R wstein/sourcesteward --draft=false", Owner},
		{PRChecksPending, base, "stw pulls --repo wstein/sourcesteward", Read},
		{PRUnread, base, "stw pulls --repo wstein/sourcesteward", Read},
		{LocalStash, base, "git -C crewbook stash list", Read},
		{GitlinkAhead, base, "git -C /ws add crewbook", Owner},
		{BranchesUnmerged, base, "stw tidy", Owner},
		{WorktreeStale, base, "stw tidy", Owner},
		{PRBot, base, "stw pulls --repo wstein/sourcesteward --bots-only", Read},
		{IssueSecurity, base, "gh issue view 7 -R wstein/sourcesteward", Read},
		{IssueAssigned, base, "gh issue view 7 -R wstein/sourcesteward", Read},
		{IssueBug, base, "gh issue view 7 -R wstein/sourcesteward", Read},
	}
	if len(cases) != len(Categories())+1 { // read-error appears twice
		t.Fatalf("%d cases for %d categories", len(cases), len(Categories()))
	}
	for _, c := range cases {
		cmd, eff := buildCommand(c.cat, c.target)
		if cmd != c.cmd || eff != c.effect {
			t.Errorf("%s: %q (%q), want %q (%q)", c.cat, cmd, eff, c.cmd, c.effect)
		}
	}
}

func withSource(t target, s Source) target { t.Source = s; return t }

// Every hostile value in any one position removes the command (and its effect), never injects.
func TestBuildCommandRefusesHostileTokens(t *testing.T) {
	sha := strings.Repeat("a", 40)
	good := target{Slug: "o/r", Path: "p", Root: "/ws", Number: 1, Tag: "v1", Branch: "main", SHA: sha, Worktree: "/w"}
	hostile := []string{"", "-x", "a b", "a;b", "$(x)", "x\ny", "`x`", "x'y", "x\x00", "../..", "o/r extra", "o/r;rm"}
	uses := map[Category][]string{
		RepoError: {"Path"}, LocalOperation: {"Path"}, MainRed: {"Slug", "Branch"}, ReleaseRunFailed: {"Slug", "SHA"},
		ReadError: {"Slug"}, LocalBehind: {"Path"}, PRReady: {"Slug"}, ReleaseDraft: {"Slug", "Tag"},
		IssueSecurity: {"Slug"}, GitlinkAhead: {"Path", "Root"}, WorktreeDirty: {"Worktree"}, PRBot: {"Slug"},
	}
	for cat, fields := range uses {
		for _, f := range fields {
			for _, h := range hostile {
				tg := good
				if cat == ReadError {
					tg.Source = SourcePulls
				}
				switch f {
				case "Path":
					tg.Path = h
				case "Slug":
					tg.Slug = h
				case "Branch":
					tg.Branch = h
				case "SHA":
					tg.SHA = h
				case "Tag":
					tg.Tag = h
				case "Root":
					tg.Root = h
				case "Worktree":
					tg.Worktree = h
				}
				cmd, eff := buildCommand(cat, tg)
				// A path with spaces or shell metacharacters is quoted, not refused.
				quoted := (f == "Path" || f == "Root" || f == "Worktree") && h != "" && !strings.HasPrefix(h, "-") && !strings.ContainsAny(h, "\n\x00")
				if f == "Tag" || f == "Branch" {
					_, quoted = refWord(h) // a valid ref with shell characters is quoted, never refused or injected
				}
				if quoted {
					if cmd == "" || eff == "" || (strings.ContainsAny(h, ";$`'") && !strings.Contains(cmd, "'")) {
						t.Errorf("%s %s=%q: quotable value refused or left unquoted: %q", cat, f, h, cmd)
					}
					continue
				}
				if cmd != "" || eff != "" {
					t.Errorf("%s %s=%q: command %q (%q)", cat, f, h, cmd, eff)
				}
			}
		}
	}
	if cmd, _ := buildCommand(PRReady, target{Slug: "o/r", Number: 0}); cmd != "" {
		t.Errorf("PR number 0 accepted: %q", cmd)
	}
	if cmd, _ := buildCommand(Category("nope"), good); cmd != "" {
		t.Errorf("unknown category produced %q", cmd)
	}
}

func TestBuildCommandQuotesPath(t *testing.T) {
	cmd, _ := buildCommand(LocalChanges, target{Path: "my repo/it's"})
	if cmd != `git -C 'my repo/it'\''s' status` {
		t.Fatal(cmd)
	}
}

// Rows 23 and 24 suggest the workspace-wide wizard (Spec 041 OQ11): it takes no value from the target,
// so a hostile path can neither reach the string nor suppress it.
func TestTidyHintIgnoresTheTarget(t *testing.T) {
	for _, c := range []Category{BranchesUnmerged, WorktreeStale} {
		for _, path := range []string{"", "-x", "a;b", "$(x)", "x\ny"} {
			if cmd, eff := buildCommand(c, target{Path: path}); cmd != "stw tidy" || eff != Owner {
				t.Errorf("%s Path=%q: %q (%q)", c, path, cmd, eff)
			}
		}
	}
}
