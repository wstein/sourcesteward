// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/pulls"
)

const slug = "wstein/sourcesteward"

var sha40 = strings.Repeat("c", 40)

func remoteRepo(mod func(*Remote)) Repo {
	r := &Remote{Sources: map[Source]SourceState{}}
	if mod != nil {
		mod(r)
	}
	return Repo{Local: repoOK("sourcesteward", nil), Slug: slug, Remote: r}
}

func remote(mod func(*Remote)) []Item { return remoteItems(remoteRepo(mod)) }

func pr(n int, group, reason string, mod func(*PR)) PR {
	p := PR{Number: n, Title: "title " + strings.Repeat("x", 1), URL: "https://github.com/wstein/sourcesteward/pull/" + string(rune('0'+n)), Author: "alice", Group: group, Reason: reason}
	if mod != nil {
		mod(&p)
	}
	return p
}

func TestRemoteRules(t *testing.T) {
	failed := &Run{Workflow: "release.yml", Conclusion: "failure", Status: "completed", URL: "https://github.com/wstein/sourcesteward/actions/runs/9"}
	yes := true
	cases := []struct {
		name     string
		mod      func(*Remote)
		id       string
		priority Priority
		cat      Category
		command  string
		effect   Effect
		url      string
		hint     bool
	}{
		{"3 main red", func(r *Remote) { r.Main = &Main{Branch: "main", Head: sha40, Class: "red"} },
			"main-red:wstein/sourcesteward:main", Red, MainRed, "gh run list --repo wstein/sourcesteward --branch main", Read, "https://github.com/wstein/sourcesteward/actions", false},
		{"4 release run failed (unreleased tag)", func(r *Remote) { r.Tags = []ReleaseTag{{Tag: "v1.0.0", SHA: sha40, Unreleased: true, Run: failed}} },
			"release-run-failed:wstein/sourcesteward:v1.0.0", Red, ReleaseRunFailed, "gh run list --repo wstein/sourcesteward --commit " + sha40, Read, failed.URL, false},
		{"4 release run failed (released tag)", func(r *Remote) { r.Tags = []ReleaseTag{{Tag: "v1.0.0", SHA: sha40, Run: failed}} },
			"release-run-failed:wstein/sourcesteward:v1.0.0", Red, ReleaseRunFailed, "gh run list --repo wstein/sourcesteward --commit " + sha40, Read, failed.URL, false},
		{"5 read error pulls", func(r *Remote) { r.Sources[SourcePulls] = SourceState{Outcome: OutcomeError, Code: "forbidden"} },
			"read-error:wstein/sourcesteward:pulls", Yellow, ReadError, "stw pulls --repo wstein/sourcesteward", Read, "", false},
		{"5 read error releases", func(r *Remote) { r.Sources[SourceReleases] = SourceState{Outcome: OutcomeError, Code: CodeRateLimited} },
			"read-error:wstein/sourcesteward:releases", Yellow, ReadError, "", "", "", false},
		{"11 main behind origin", func(r *Remote) { r.Main = &Main{Branch: "main", Class: "green", BehindOrigin: &yes} },
			"main-behind-origin:wstein/sourcesteward:main", Yellow, MainBehindOrigin, "stw sync --plan sourcesteward", Read, "https://github.com/wstein/sourcesteward", false},
		{"14 pr conflict", func(r *Remote) { r.Pulls = []PR{pr(1, pulls.GroupConflict, "", nil)} },
			"pr-conflict:wstein/sourcesteward:1", Yellow, PRConflict, "stw pulls --repo wstein/sourcesteward", Read, "https://github.com/wstein/sourcesteward/pull/1", false},
		{"15 pr blocked", func(r *Remote) { r.Pulls = []PR{pr(2, pulls.GroupBlocked, pulls.ReasonChecksRed, nil)} },
			"pr-blocked:wstein/sourcesteward:2", Yellow, PRBlocked, "stw pulls --repo wstein/sourcesteward", Read, "https://github.com/wstein/sourcesteward/pull/2", false},
		{"15 pr blocked unknown", func(r *Remote) { r.Pulls = []PR{pr(2, pulls.GroupBlocked, pulls.ReasonUnknown, nil)} },
			"pr-blocked:wstein/sourcesteward:2", Yellow, PRBlocked, "stw pulls --repo wstein/sourcesteward", Read, "https://github.com/wstein/sourcesteward/pull/2", false},
		{"16 tag without release", func(r *Remote) { r.Tags = []ReleaseTag{{Tag: "v1.1.0", SHA: sha40, Unreleased: true}} },
			"tag-without-release:wstein/sourcesteward:v1.1.0", Yellow, TagWithoutRelease, "", "", "", true},
		{"16 tag without release, run in progress", func(r *Remote) {
			r.Tags = []ReleaseTag{{Tag: "v1.1.0", SHA: sha40, Unreleased: true, Run: &Run{Status: "in_progress"}}}
		}, "tag-without-release:wstein/sourcesteward:v1.1.0", Yellow, TagWithoutRelease, "", "", "", true},
		{"17 pr ready", func(r *Remote) { r.Pulls = []PR{pr(3, pulls.GroupReady, "", nil)} },
			"pr-ready:wstein/sourcesteward:3", Green, PRReady, "gh pr merge 3 -R wstein/sourcesteward --rebase", Owner, "https://github.com/wstein/sourcesteward/pull/3", false},
		{"18 release draft", func(r *Remote) {
			r.Visibility = WriteAccess
			r.Drafts = []Draft{{Name: "First", Tag: "v0.1.0", URL: "https://github.com/wstein/sourcesteward/releases/tag/untagged-1", Assets: 4}}
		}, "release-draft:wstein/sourcesteward:v0.1.0", Green, ReleaseDraft, "gh release edit v0.1.0 -R wstein/sourcesteward --draft=false", Owner, "https://github.com/wstein/sourcesteward/releases/tag/untagged-1", false},
		{"19 pr checks pending", func(r *Remote) { r.Pulls = []PR{pr(4, pulls.GroupChecks, "", nil)} },
			"pr-checks-pending:wstein/sourcesteward:4", Blue, PRChecksPending, "stw pulls --repo wstein/sourcesteward", Read, "https://github.com/wstein/sourcesteward/pull/4", false},
		{"20 pr unread", func(r *Remote) {
			r.Pulls = []PR{pr(5, pulls.GroupBlocked, pulls.ReasonNotRead, func(p *PR) { p.PastBudget = true })}
		}, "pr-unread:wstein/sourcesteward:5", Blue, PRUnread, "stw pulls --repo wstein/sourcesteward", Read, "https://github.com/wstein/sourcesteward/pull/5", false},
		{"25 pr bot", func(r *Remote) {
			r.Pulls = []PR{pr(6, pulls.GroupReady, "", func(p *PR) { p.Bot = true; p.Author = "dependabot" })}
		},
			"pr-bot:wstein/sourcesteward:6", Violet, PRBot, "stw pulls --repo wstein/sourcesteward --bots-only", Read, "https://github.com/wstein/sourcesteward/pull/6", false},
	}
	covered := map[Category]bool{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			items := remote(c.mod)
			if len(items) != 1 {
				t.Fatalf("items = %v", ids(items))
			}
			it := items[0]
			if it.ID != c.id || it.Priority != c.priority || it.Category != c.cat || it.Command != c.command || it.Effect != c.effect || it.URL != c.url {
				t.Errorf("item = %+v", it)
			}
			if it.Repo != slug || it.Path != "sourcesteward" || it.Title == "" || (it.Hint != "") != c.hint {
				t.Errorf("repo %q path %q title %q hint %q", it.Repo, it.Path, it.Title, it.Hint)
			}
			if (it.Command == "") != (it.Effect == "") {
				t.Errorf("effect %q does not pair with command %q", it.Effect, it.Command)
			}
		})
		covered[c.cat] = true
	}
	// Together with the local rules, the remote rows cover the rest of the table.
	for _, c := range []Category{MainRed, ReleaseRunFailed, ReadError, MainBehindOrigin, PRConflict, PRBlocked, TagWithoutRelease, PRReady, ReleaseDraft, PRChecksPending, PRUnread, PRBot} {
		if !covered[c] {
			t.Errorf("no fixture for %s", c)
		}
	}
}

// PR evaluation order of the spec: draft, bot, past budget, group.
func TestPullRequestPrecedence(t *testing.T) {
	cases := []struct {
		name string
		row  PR
		want Category // empty: no item
	}{
		{"draft is no item", pr(1, pulls.GroupDraft, "", func(p *PR) { p.Draft = true }), ""},
		{"bot draft is no item", pr(1, pulls.GroupDraft, "", func(p *PR) { p.Draft, p.Bot = true, true }), ""},
		{"draft flag wins over a ready group", pr(1, pulls.GroupReady, "", func(p *PR) { p.Draft = true }), ""},
		{"bot ready is pr-bot only", pr(1, pulls.GroupReady, "", func(p *PR) { p.Bot = true }), PRBot},
		{"bot conflict is pr-bot only", pr(1, pulls.GroupConflict, "", func(p *PR) { p.Bot = true }), PRBot},
		{"bot blocked is pr-bot only", pr(1, pulls.GroupBlocked, pulls.ReasonChecksRed, func(p *PR) { p.Bot = true }), PRBot},
		{"bot past budget is pr-bot only", pr(1, pulls.GroupBlocked, pulls.ReasonNotRead, func(p *PR) { p.Bot, p.PastBudget = true, true }), PRBot},
		{"past budget human is pr-unread", pr(1, pulls.GroupBlocked, pulls.ReasonNotRead, func(p *PR) { p.PastBudget = true }), PRUnread},
		{"past budget wins over the group", pr(1, pulls.GroupReady, "", func(p *PR) { p.PastBudget = true }), PRUnread},
		{"not-read without the flag is no item (deadline or failed read)", pr(1, pulls.GroupBlocked, pulls.ReasonNotRead, nil), ""},
		{"unknown group is no item", pr(1, "other", "", nil), ""},
		{"ready", pr(1, pulls.GroupReady, "", nil), PRReady},
		{"conflict", pr(1, pulls.GroupConflict, "", nil), PRConflict},
		{"checks", pr(1, pulls.GroupChecks, "", nil), PRChecksPending},
		{"blocked no-checks", pr(1, pulls.GroupBlocked, pulls.ReasonNoChecks, nil), PRBlocked},
		{"blocked review", pr(1, pulls.GroupBlocked, pulls.ReasonReview, nil), PRBlocked},
	}
	for _, c := range cases {
		items := remote(func(r *Remote) { r.Pulls = []PR{c.row} })
		switch {
		case c.want == "" && len(items) != 0:
			t.Errorf("%s: %v", c.name, ids(items))
		case c.want != "" && (len(items) != 1 || items[0].Category != c.want):
			t.Errorf("%s: %v, want %s", c.name, ids(items), c.want)
		}
	}
}

func TestDraftsFirstThenBots(t *testing.T) {
	items := remote(func(r *Remote) {
		r.Pulls = []PR{
			pr(1, pulls.GroupDraft, "", func(p *PR) { p.Draft, p.Bot = true, true }),
			pr(2, pulls.GroupReady, "", func(p *PR) { p.Bot = true }),
			pr(3, pulls.GroupReady, "", nil),
		}
	})
	if got := ids(items); len(got) != 2 || got[0] != "pr-bot:wstein/sourcesteward:2" || got[1] != "pr-ready:wstein/sourcesteward:3" {
		t.Fatalf("%v", got)
	}
}

func TestOneItemPerTag(t *testing.T) {
	failed := &Run{Conclusion: "failure", URL: "https://github.com/a/b/actions/runs/1"}
	for _, conclusion := range []string{"failure", "cancelled", "timed_out"} {
		items := remote(func(r *Remote) {
			r.Tags = []ReleaseTag{{Tag: "v2.0.0", SHA: sha40, Unreleased: true, Run: &Run{Conclusion: conclusion}}}
		})
		if len(items) != 1 || items[0].Category != ReleaseRunFailed {
			t.Errorf("%s: %v", conclusion, ids(items))
		}
	}
	for _, conclusion := range []string{"success", "skipped", "neutral", ""} {
		items := remote(func(r *Remote) {
			r.Tags = []ReleaseTag{{Tag: "v2.0.0", SHA: sha40, Unreleased: true, Run: &Run{Conclusion: conclusion}}}
		})
		if len(items) != 1 || items[0].Category != TagWithoutRelease {
			t.Errorf("%s: %v", conclusion, ids(items))
		}
	}
	// The latest tag of a product that is released and has a green run is no item.
	if items := remote(func(r *Remote) { r.Tags = []ReleaseTag{{Tag: "v1.0.0", SHA: sha40, Run: &Run{Conclusion: "success"}}} }); len(items) != 0 {
		t.Errorf("released green tag: %v", ids(items))
	}
	// The same tag listed twice (two products, latest and unreleased) is one item, the failed run wins, whatever the order.
	a := ReleaseTag{Tag: "v3.0.0", SHA: sha40, Unreleased: true}
	b := ReleaseTag{Tag: "v3.0.0", SHA: sha40, Run: failed}
	for _, order := range [][]ReleaseTag{{a, b}, {b, a}} {
		items := remote(func(r *Remote) { r.Tags = order })
		if len(items) != 1 || items[0].Category != ReleaseRunFailed || items[0].URL != failed.URL {
			t.Errorf("duplicate tag: %v", ids(items))
		}
	}
}

func TestDraftsAreOnlyItemsWithWriteAccess(t *testing.T) {
	drafts := []Draft{{Tag: "v1.0.0", URL: "https://github.com/x/y/releases"}}
	for _, v := range []Visibility{NoWriteAccess, Unknown, ""} {
		if items := remote(func(r *Remote) { r.Visibility, r.Drafts = v, drafts }); len(items) != 0 {
			t.Errorf("visibility %q: %v", v, ids(items))
		}
	}
	if items := remote(func(r *Remote) { r.Visibility, r.Drafts = WriteAccess, drafts }); len(items) != 1 {
		t.Errorf("write access: %v", ids(items))
	}
}

func TestMainStateOnlyRedAndBehindOriginMakeItems(t *testing.T) {
	no := false
	for _, class := range []string{"none", "pending", "green", ""} {
		if items := remote(func(r *Remote) { r.Main = &Main{Branch: "main", Class: class, BehindOrigin: &no} }); len(items) != 0 {
			t.Errorf("class %q: %v", class, ids(items))
		}
	}
	// Unknown behind-origin (nil) is no item; red and behind yield both items (distinct facts).
	yes := true
	items := remote(func(r *Remote) { r.Main = &Main{Branch: "main", Class: "red", BehindOrigin: &yes} })
	if len(items) != 2 {
		t.Fatalf("%v", ids(items))
	}
}

func TestReadErrorsOnlyForFailedReads(t *testing.T) {
	items := remote(func(r *Remote) {
		r.Sources = map[Source]SourceState{
			SourceRepo:      {Outcome: OutcomeOK},
			SourcePulls:     {Outcome: OutcomeSkipped, Reason: ReasonDeadline},
			SourceReleases:  {Outcome: OutcomeSkipped, Reason: ReasonNoToken},
			SourceTags:      {Outcome: OutcomeError, Code: "network"},
			SourceCIMain:    {Outcome: OutcomeError, Code: CodeRateLimited},
			SourceCIRelease: {Outcome: OutcomeSkipped, Reason: ReasonNotFound},
		}
	})
	got := ids(items)
	if len(got) != 2 || got[0] != "read-error:wstein/sourcesteward:tags" || got[1] != "read-error:wstein/sourcesteward:ci-main" {
		t.Fatalf("%v", got)
	}
}

func TestFailedReadNeverLeavesACategorylessRow(t *testing.T) {
	// A pulls read that failed after one row: that row is not-read, the repository has exactly one read-error and no PR item.
	items := remote(func(r *Remote) {
		r.Sources[SourcePulls] = SourceState{Outcome: OutcomeError, Code: CodeRateLimited}
		r.Pulls = []PR{pr(1, pulls.GroupBlocked, pulls.ReasonNotRead, nil), pr(2, pulls.GroupBlocked, pulls.ReasonNotRead, nil)}
	})
	if got := ids(items); len(got) != 1 || got[0] != "read-error:wstein/sourcesteward:pulls" {
		t.Fatalf("%v", got)
	}
	for _, it := range items {
		if it.Category == "" || it.Priority == "" {
			t.Errorf("categoryless item %+v", it)
		}
	}
}

func TestRemoteHostileForgeStrings(t *testing.T) {
	evil := "x\x1b[31m\n; rm -rf /"
	items := remote(func(r *Remote) {
		r.Visibility = WriteAccess
		r.Drafts = []Draft{{Name: evil, Tag: evil, URL: "javascript:alert(1)", Assets: 1}}
		r.Pulls = []PR{pr(1, pulls.GroupReady, "", func(p *PR) { p.Title, p.URL, p.Author = evil, "http://insecure", evil })}
		r.Tags = []ReleaseTag{{Tag: evil, SHA: evil, Unreleased: true}}
		r.Main = &Main{Branch: evil, Head: evil, Class: "red"}
	})
	if len(items) != 4 {
		t.Fatalf("%v", ids(items))
	}
	for _, it := range items {
		if strings.ContainsAny(it.Title, "\n\x1b") || strings.ContainsAny(it.Command, "\n\x1b;") && it.Category != PRReady {
			t.Errorf("%s: title %q command %q", it.ID, it.Title, it.Command)
		}
		if it.Category == ReleaseDraft && (it.Command != "" || it.URL != "") {
			t.Errorf("draft with hostile tag: command %q url %q", it.Command, it.URL)
		}
		if it.Category == PRReady && (it.URL != "" || !strings.HasPrefix(it.Command, "gh pr merge 1 -R")) {
			t.Errorf("pr: url %q command %q", it.URL, it.Command)
		}
		if it.Category == MainRed && it.Command != "" {
			t.Errorf("main-red command %q", it.Command)
		}
	}
}

func TestRemoteNeedsAValidRepository(t *testing.T) {
	for _, s := range []string{"", "no-slash", "a/b c", "../x", "o/r;x"} {
		r := remoteRepo(func(r *Remote) { r.Pulls = []PR{pr(1, pulls.GroupReady, "", nil)} })
		r.Slug = s
		if items := remoteItems(r); len(items) != 0 {
			t.Errorf("slug %q: %v", s, ids(items))
		}
	}
	if items := remoteItems(Repo{Slug: slug}); len(items) != 0 {
		t.Errorf("nil remote: %v", ids(items))
	}
}

func TestRemoteEvidence(t *testing.T) {
	items := remote(func(r *Remote) {
		r.Visibility = WriteAccess
		r.Drafts = []Draft{{Tag: "v1", Assets: 4, CreatedAt: time.Unix(0, 0)}}
	})
	ev := items[0].Evidence
	if ev["drafts_visibility"] != "write-access" || ev["assets"] != 4 {
		t.Fatalf("%v", ev)
	}
	items = remote(func(r *Remote) { r.Pulls = []PR{pr(1, pulls.GroupBlocked, pulls.ReasonReview, nil)} })
	if ev := items[0].Evidence; ev["group"] != "blocked" || ev["reason"] != "review-or-protection" {
		t.Fatalf("%v", ev)
	}
}

func TestSubjectIDFallsBackToDash(t *testing.T) {
	if got := subjectID(Subject{Kind: KindRelease}); got != "-" {
		t.Errorf("empty release tag: %q", got)
	}
	if got := subjectID(Subject{Kind: KindPR, Number: 12}); got != "12" {
		t.Errorf("pr: %q", got)
	}
}
