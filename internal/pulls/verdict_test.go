// SPDX-License-Identifier: EUPL-1.2
package pulls

import (
	"fmt"
	"testing"
	"time"
)

// Every row of the ordered verdict table of Spec 036 and the precedence pairs that the order exists for.
func TestVerdictTable(t *testing.T) {
	type c struct {
		name        string
		mut         func(*pr)
		group, why  string
		noDetail    bool // past the budget (Limit 0 is not allowed, so the case adds a newer PR with Limit 1)
		wantReasonN string
	}
	cases := []c{
		{name: "1 draft", mut: func(p *pr) { p.draft = true }, group: "draft"},
		{name: "1 draft beats red", mut: func(p *pr) { p.draft = true; p.class = "red" }, group: "draft"},
		{name: "2 past budget", noDetail: true, mut: func(p *pr) {}, group: "blocked", why: "not-read"},
		{name: "3 head differs", mut: func(p *pr) { p.detailHead = sha("f") }, group: "blocked", why: "unknown"},
		{name: "3 head differs beats red", mut: func(p *pr) { p.detailHead = sha("f"); p.class = "red" }, group: "blocked", why: "unknown"},
		{name: "3 detail head malformed", mut: func(p *pr) { p.detailHead = "ABC" }, group: "blocked", why: "unknown"},
		{name: "3 detail head uppercase", mut: func(p *pr) { p.detailHead = "ABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD" }, group: "blocked", why: "unknown"},
		{name: "4 red", mut: func(p *pr) { p.class = "red" }, group: "blocked", why: "checks-red"},
		{name: "4 red beats dirty", mut: func(p *pr) { p.class = "red"; p.state = "dirty"; p.mergeable = "false" }, group: "blocked", why: "checks-red"},
		{name: "4 red beats unknown mergeable", mut: func(p *pr) { p.class = "red"; p.mergeable = "null" }, group: "blocked", why: "checks-red"},
		{name: "5 none", mut: func(p *pr) { p.class = "none" }, group: "blocked", why: "no-checks"},
		{name: "5 none beats unknown", mut: func(p *pr) { p.class = "none"; p.mergeable = "null" }, group: "blocked", why: "no-checks"},
		{name: "5 none beats clean", mut: func(p *pr) { p.class = "none"; p.state = "clean" }, group: "blocked", why: "no-checks"},
		{name: "6 mergeable null", mut: func(p *pr) { p.mergeable = "null" }, group: "blocked", why: "unknown"},
		{name: "6 state unknown", mut: func(p *pr) { p.state = "unknown" }, group: "blocked", why: "unknown"},
		{name: "6 unknown beats pending", mut: func(p *pr) { p.class = "pending"; p.mergeable = "null" }, group: "blocked", why: "unknown"},
		{name: "6 state unknown beats pending", mut: func(p *pr) { p.class = "pending"; p.state = "unknown" }, group: "blocked", why: "unknown"},
		{name: "7 pending", mut: func(p *pr) { p.class = "pending" }, group: "checks"},
		{name: "7 pending beats dirty", mut: func(p *pr) { p.class = "pending"; p.state = "dirty"; p.mergeable = "false"; p.rebaseable = "false" }, group: "checks"},
		{name: "7 pending beats blocked", mut: func(p *pr) { p.class = "pending"; p.state = "blocked" }, group: "checks"},
		{name: "8 dirty", mut: func(p *pr) { p.state = "dirty" }, group: "conflict"},
		{name: "8 mergeable false", mut: func(p *pr) { p.mergeable = "false" }, group: "conflict"},
		{name: "8 behind", mut: func(p *pr) { p.state = "behind" }, group: "conflict"},
		{name: "8 dirty beats not-rebaseable", mut: func(p *pr) { p.state = "dirty"; p.rebaseable = "false" }, group: "conflict"},
		{name: "8 mergeable false beats blocked", mut: func(p *pr) { p.mergeable = "false"; p.state = "blocked" }, group: "conflict"},
		{name: "9 blocked", mut: func(p *pr) { p.state = "blocked" }, group: "blocked", why: "review-or-protection"},
		{name: "9 blocked beats not-rebaseable", mut: func(p *pr) { p.state = "blocked"; p.rebaseable = "false" }, group: "blocked", why: "review-or-protection"},
		{name: "10 unstable", mut: func(p *pr) { p.state = "unstable" }, group: "blocked", why: "unstable"},
		{name: "10 unstable beats not-rebaseable", mut: func(p *pr) { p.state = "unstable"; p.rebaseable = "false" }, group: "blocked", why: "unstable"},
		{name: "11 not rebaseable", mut: func(p *pr) { p.rebaseable = "false" }, group: "blocked", why: "not-rebaseable"},
		{name: "11 not rebaseable beats has_hooks", mut: func(p *pr) { p.rebaseable = "false"; p.state = "has_hooks" }, group: "blocked", why: "not-rebaseable"},
		{name: "12 has_hooks", mut: func(p *pr) { p.state = "has_hooks" }, group: "blocked", why: "unknown"},
		{name: "13 ready", mut: func(p *pr) {}, group: "ready"},
		{name: "14 rebaseable null", mut: func(p *pr) { p.rebaseable = "null" }, group: "blocked", why: "unknown"},
		{name: "14 other state", mut: func(p *pr) { p.state = "draft" }, group: "blocked", why: "unknown"},
		{name: "14 empty state", mut: func(p *pr) { p.state = "" }, group: "blocked", why: "unknown"},
		{name: "14 state not a known word", mut: func(p *pr) { p.state = "CLEAN" }, group: "blocked", why: "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newForge()
			p := okPR("o/r", 5)
			tc.mut(&p)
			f.add(p)
			o := Options{Repos: []string{"o/r"}}
			if tc.noDetail {
				newer := okPR("o/r", 6)
				newer.age = time.Hour
				f.add(newer)
				o.Limit = 1
			}
			res := run(t, f, o)
			got := row(t, res, "o/r", 5)
			if got.Group != tc.group || got.Reason != tc.why || got.Ready != (tc.group == "ready") {
				t.Fatalf("group=%q reason=%q ready=%v, want %q %q", got.Group, got.Reason, got.Ready, tc.group, tc.why)
			}
			if got.Ready && (got.Reason != "") {
				t.Fatal("ready row with a reason")
			}
			if tc.group != "blocked" && got.Reason != "" {
				t.Fatalf("reason %q outside blocked", got.Reason)
			}
		})
	}
}

// A repository without any CI is never ready, however clean GitHub reports the PR.
func TestNoCIIsNeverReady(t *testing.T) {
	f := newForge()
	p := okPR("o/r", 1)
	p.class = "none"
	f.add(p)
	if r := row(t, run(t, f, Options{Repos: []string{"o/r"}}), "o/r", 1); r.Ready || r.Group != "blocked" || r.Reason != "no-checks" {
		t.Fatalf("%+v", r)
	}
}

// Status-only checks (no runs) classify the same way.
func TestStatusOnlyChecksDriveTheVerdict(t *testing.T) {
	for state, want := range map[string]string{"success": "ready", "pending": "checks"} {
		f := newForge()
		p := okPR("o/r", 1)
		p.class = "none"
		f.add(p)
		f.stats[p.head] = fmt.Sprintf(`[{"context":"legacy","state":%q,"description":"x"}]`, state)
		if r := row(t, run(t, f, Options{Repos: []string{"o/r"}}), "o/r", 1); r.Group != want {
			t.Fatalf("%s: %+v", state, r)
		}
	}
}

func TestRowFieldsFromListAndDetail(t *testing.T) {
	f := newForge()
	p := okPR("o/r", 7)
	p.class = "red"
	f.add(p)
	f.lists["o/r"][0].Labels = []string{"dependencies"}
	f.lists["o/r"][0].Reviewers = []string{"bob"}
	f.lists["o/r"][0].Teams = []string{"core"}
	r := row(t, run(t, f, Options{Repos: []string{"o/r"}}), "o/r", 7)
	if r.Title != "title 7" || r.URL != "https://github.com/o/r/pull/7" || r.Author != "alice" || r.Bot || r.Base != "main" || r.HeadRef != "feat" || r.Head != p.head || r.Fork ||
		len(r.Labels) != 1 || r.Labels[0] != "dependencies" || len(r.Reviewers) != 2 || r.Reviewers[0] != "bob" || r.Reviewers[1] != "core" ||
		r.Checks == nil || *r.Checks != "red" || len(r.Failing) != 1 || r.Failing[0] != "ci" || r.Mergeable == nil || !*r.Mergeable || r.MergeableState == nil || *r.MergeableState != "clean" ||
		r.Rebaseable == nil || !*r.Rebaseable || !r.Detail || r.AgeDays == nil || *r.AgeDays != 1 {
		t.Fatalf("%+v", r)
	}
	// Failing names only for red.
	g := newForge()
	g.add(okPR("o/r", 1))
	if r := row(t, run(t, g, Options{Repos: []string{"o/r"}}), "o/r", 1); r.Failing == nil || len(r.Failing) != 0 {
		t.Fatalf("failing must be an empty array for a green PR: %#v", r.Failing)
	}
}

// A class outside the four known ones never reaches ready, and has_hooks is explicitly unknown.
func TestVerdictFailsClosedOnUnknownInput(t *testing.T) {
	yes := true
	base := facts{detail: true, headOK: true, class: "green", mergeable: &yes, state: "clean", rebaseable: &yes}
	if g, _ := verdict(base); g != GroupReady {
		t.Fatalf("base %q", g)
	}
	for name, mut := range map[string]func(*facts){
		"class weird":     func(f *facts) { f.class = "weird" },
		"class empty":     func(f *facts) { f.class = "" },
		"state has_hooks": func(f *facts) { f.state = "has_hooks" },
	} {
		f := base
		mut(&f)
		if g, r := verdict(f); g != GroupBlocked || r != ReasonUnknown {
			t.Errorf("%s: %q %q", name, g, r)
		}
	}
}
