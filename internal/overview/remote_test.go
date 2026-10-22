// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/wstein/sourcesteward/internal/pulls"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
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
		{"not-read without the flag is a read-error of the repository", pr(1, pulls.GroupBlocked, pulls.ReasonNotRead, nil), ReadError},
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

var hostileStrings = map[string]string{
	"escape":  "x\x1b[31m\n; rm -rf /",
	"newline": "line\nbreak",
	"colon":   "a:b:c",
	"10kB":    strings.Repeat("long/", 2048),
	"unicode": "v\u00fc\u202e\u2028x",
	"empty":   "",
	"dash":    "-rf",
}

var fingerprintRE = regexp.MustCompile(`^h-[0-9a-f]{12}$`)

// idParts splits an id into CATEGORY, REPO and SUBJECT; a valid repository and the validated parts hold no ':'.
func idParts(t *testing.T, id string) [3]string {
	t.Helper()
	p := strings.Split(id, ":")
	if len(p) != 3 {
		t.Fatalf("id %q has %d parts", id, len(p))
	}
	return [3]string{p[0], p[1], p[2]}
}

func TestRemoteHostileForgeStrings(t *testing.T) {
	seen := map[string]string{}
	for name, evil := range hostileStrings {
		build := func() []Item {
			return remote(func(r *Remote) {
				r.Visibility = WriteAccess
				r.Drafts = []Draft{{Name: evil, Tag: evil, URL: "javascript:alert(1)", Assets: 1}}
				r.Pulls = []PR{pr(1, pulls.GroupReady, "", func(p *PR) { p.Title, p.URL, p.Author = evil, "http://insecure", evil })}
				r.Tags = []ReleaseTag{{Tag: evil, SHA: evil, Unreleased: true}}
				r.Main = &Main{Branch: evil, Head: evil, Class: "red"}
			})
		}
		items := build()
		if len(items) != 4 {
			t.Fatalf("%s: %v", name, ids(items))
		}
		if !reflect.DeepEqual(ids(items), ids(build())) {
			t.Errorf("%s: ids are not stable", name)
		}
		for _, it := range items {
			parts := idParts(t, it.ID)
			if strings.ContainsAny(it.ID, "\n\x1b\x00") || len(it.ID) > 300 {
				t.Errorf("%s: id %q", name, it.ID)
			}
			if parts[1] != slug {
				t.Errorf("%s: repo part %q", name, parts[1])
			}
			if it.Category != PRReady {
				if validRef(evil) && parts[2] != evil {
					t.Errorf("%s: %s: valid subject %q was replaced by %q", name, it.Category, evil, parts[2])
				}
				if !validRef(evil) && !fingerprintRE.MatchString(parts[2]) {
					t.Errorf("%s: %s: invalid component is not a fingerprint: %q", name, it.Category, parts[2])
				}
			}
			if strings.ContainsAny(it.Title, "\n\x1b") || (strings.ContainsAny(it.Command, "\n\x1b;") && it.Category != PRReady && !strings.Contains(it.Command, "'")) {
				t.Errorf("%s: %s: title %q command %q", name, it.ID, it.Title, it.Command)
			}
			if it.Category == ReleaseDraft && it.URL != "" {
				t.Errorf("%s: draft url %q", name, it.URL)
			}
			if it.Category == PRReady && (it.URL != "" || !strings.HasPrefix(it.Command, "gh pr merge 1 -R")) {
				t.Errorf("%s: pr: url %q command %q", name, it.URL, it.Command)
			}
			if !validRef(evil) && (it.Category == MainRed || it.Category == ReleaseDraft) && it.Command != "" {
				t.Errorf("%s: %s command %q for an invalid ref", name, it.Category, it.Command)
			}
			if prev, dup := seen[it.ID]; dup && it.Category != PRReady && prev != name+string(it.Category) {
				// the same category with different hostile values must not share an id
				t.Errorf("%s: id %q already used by %s", name, it.ID, prev)
			}
			seen[it.ID] = name + string(it.Category)
		}
	}
}

func TestInvalidComponentsGetAStableFingerprint(t *testing.T) {
	got := idToken("a:b", validRef)
	again := idToken("a:b", validRef)
	if got != again || !fingerprintRE.MatchString(got) || got == idToken("a:c", validRef) {
		t.Fatalf("%q %q", got, again)
	}
	if idToken("v1.0.0", validRef) != "v1.0.0" {
		t.Error("a valid component stays")
	}
	// sha256("a:b") starts with ...: pins the algorithm, not a time or a counter.
	if want := "h-" + sha12("a:b"); got != want {
		t.Errorf("%q, want %q", got, want)
	}
}

func sha12(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

func TestDraftIDsAreUniqueAndStable(t *testing.T) {
	created := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		drafts []Draft
		want   int
	}{
		{"distinct valid tags", []Draft{{Tag: "v1.0.0"}, {Tag: "v1.1.0"}}, 2},
		{"empty tags, different names", []Draft{{Name: "a"}, {Name: "b"}}, 2},
		{"empty tags, same name, different dates", []Draft{{Name: "a", CreatedAt: created}, {Name: "a", CreatedAt: created.Add(time.Hour)}}, 2},
		{"same valid tag twice, different names", []Draft{{Tag: "v1.0.0", Name: "a"}, {Tag: "v1.0.0", Name: "b"}}, 2},
		{"identical drafts are one finding", []Draft{{Tag: "v1.0.0", Name: "a", CreatedAt: created}, {Tag: "v1.0.0", Name: "a", CreatedAt: created}}, 1},
		{"hostile tags, different names", []Draft{{Tag: "a:b", Name: "x"}, {Tag: "a:b", Name: "y"}}, 2},
	}
	for _, c := range cases {
		build := func(drafts []Draft) []Item {
			return sortItems(remote(func(r *Remote) { r.Visibility, r.Drafts = WriteAccess, drafts }))
		}
		got := build(c.drafts)
		if len(got) != c.want {
			t.Errorf("%s: %v", c.name, ids(got))
			continue
		}
		reversed := append([]Draft(nil), c.drafts...)
		for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
			reversed[i], reversed[j] = reversed[j], reversed[i]
		}
		if !reflect.DeepEqual(ids(got), ids(build(reversed))) {
			t.Errorf("%s: ids depend on the order: %v", c.name, ids(got))
		}
	}
	// A draft with a unique valid tag keeps the readable id.
	if got := remote(func(r *Remote) { r.Visibility, r.Drafts = WriteAccess, []Draft{{Tag: "v0.1.0"}} }); got[0].ID != "release-draft:wstein/sourcesteward:v0.1.0" {
		t.Errorf("%s", got[0].ID)
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

// A row that was not read for another reason than the budget is a lost read: the model reports it as one
// read-error of the repository even when the source says ok, and never twice.
func TestUnreadRowDerivesTheReadError(t *testing.T) {
	unread := []PR{pr(1, pulls.GroupBlocked, pulls.ReasonNotRead, nil), pr(2, pulls.GroupBlocked, pulls.ReasonNotRead, nil)}
	cases := []struct {
		name  string
		state SourceState
		pulls []PR
		want  []string
	}{
		{"source ok", SourceState{Outcome: OutcomeOK}, unread, []string{"read-error:wstein/sourcesteward:pulls"}},
		{"source state missing", SourceState{}, unread, []string{"read-error:wstein/sourcesteward:pulls"}},
		{"source error: still one item", SourceState{Outcome: OutcomeError, Code: CodeRateLimited}, unread, []string{"read-error:wstein/sourcesteward:pulls"}},
		{"deadline skip is no item", SourceState{Outcome: OutcomeSkipped, Reason: ReasonDeadline}, unread, nil},
		{"past budget is not a read error", SourceState{Outcome: OutcomeOK}, []PR{pr(3, pulls.GroupBlocked, pulls.ReasonNotRead, func(p *PR) { p.PastBudget = true })}, []string{"pr-unread:wstein/sourcesteward:3"}},
		{"bot and draft rows lose nothing", SourceState{Outcome: OutcomeOK}, []PR{
			pr(4, pulls.GroupBlocked, pulls.ReasonNotRead, func(p *PR) { p.Bot = true }),
			pr(5, pulls.GroupDraft, "", func(p *PR) { p.Draft = true }),
		}, []string{"pr-bot:wstein/sourcesteward:4"}},
	}
	for _, c := range cases {
		items := remote(func(r *Remote) {
			if c.state != (SourceState{}) {
				r.Sources[SourcePulls] = c.state
			}
			r.Pulls = c.pulls
		})
		if got := ids(items); !reflect.DeepEqual(got, c.want) && !(len(got) == 0 && len(c.want) == 0) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// Per pattern entries of one tag are one finding, and an entry that is only the latest of its
// pattern is information, never an item.
func TestTagItemsPerPatternEntries(t *testing.T) {
	r := Repo{Local: repoOK("a", nil), Slug: "w/a", Remote: &Remote{Tags: []ReleaseTag{
		{Pattern: "v*.*.*", Tag: "v1.1.0", SHA: "a", Latest: true, Unreleased: true},
		{Pattern: "v*", Tag: "v1.1.0", SHA: "a", Latest: true, Unreleased: true},
		{Pattern: "bdock/*", Tag: "bdock/1.0.0", SHA: "b", Latest: true},
	}}}
	items := tagItems(r)
	if len(items) != 1 || items[0].Category != TagWithoutRelease || items[0].Subject.Tag != "v1.1.0" {
		t.Fatalf("%+v", items)
	}
}
