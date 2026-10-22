// SPDX-License-Identifier: EUPL-1.2

package statusremote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/pullscmd"
	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

func TestSelectDerivesTheRepositoryFromOrigin(t *testing.T) {
	cases := []struct {
		name   string
		origin Origin
		slug   string
		skip   string
	}{
		{"https", Origin{Name: "a", Remote: "https://github.com/wstein/a.git", Readable: true}, "wstein/a", ""},
		{"scp", Origin{Name: "a", Remote: "git@github.com:wstein/a", Readable: true}, "wstein/a", ""},
		{"ssh", Origin{Name: "a", Remote: "ssh://git@github.com/wstein/a.git", Readable: true}, "wstein/a", ""},
		{"upper-case host", Origin{Name: "a", Remote: "https://GitHub.com/wstein/a", Readable: true}, "wstein/a", ""},
		{"not read locally wins", Origin{Name: "a", Remote: "https://github.com/wstein/a", Readable: false}, "", overview.ReasonNotReadLocally},
		{"no origin", Origin{Name: "a", Readable: true}, "", overview.ReasonNotGitHub},
		{"local path", Origin{Name: "a", Remote: "/srv/git/a.git", Readable: true}, "", overview.ReasonNotGitHub},
		{"unsupported scheme", Origin{Name: "a", Remote: "file:///github.com/wstein/a", Readable: true}, "", overview.ReasonNotGitHub},
		{"other host", Origin{Name: "a", Remote: "git@gitlab.com:wstein/a.git", Readable: true}, "", overview.ReasonOtherHost},
		{"look-alike host", Origin{Name: "a", Remote: "https://github.com.evil.example/wstein/a", Readable: true}, "", overview.ReasonOtherHost},
		{"too many segments", Origin{Name: "a", Remote: "https://github.com/wstein/a/b", Readable: true}, "", overview.ReasonInvalidRepo},
		{"owner only", Origin{Name: "a", Remote: "https://github.com/wstein", Readable: true}, "", overview.ReasonInvalidRepo},
		{"parent segment", Origin{Name: "a", Remote: "https://github.com/wstein/..", Readable: true}, "", overview.ReasonInvalidRepo},
		{"encoded space", Origin{Name: "a", Remote: "https://github.com/wstein/a%20b", Readable: true}, "", overview.ReasonInvalidRepo},
		{"encoded slash", Origin{Name: "a", Remote: "https://github.com/wstein/a%2Fb", Readable: true}, "", overview.ReasonInvalidRepo},
		{"dot segment", Origin{Name: "a", Remote: "https://github.com/wstein/../a", Readable: true}, "", overview.ReasonInvalidRepo},
	}
	for _, c := range cases {
		got := Select([]Origin{c.origin})
		want := []Target{{Name: "a", Slug: c.slug, Skip: c.skip}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, want)
		}
	}
}

func TestSelectKeepsTheOrderOfTheInput(t *testing.T) {
	got := Select([]Origin{
		{Name: "root", Remote: "https://github.com/wstein/anchorage", Readable: true},
		{Name: "b", Remote: "https://gitlab.com/x/b", Readable: true},
		{Name: "a", Remote: "https://github.com/wstein/a", Readable: true},
	})
	var names []string
	for _, tg := range got {
		names = append(names, tg.Name)
	}
	if !reflect.DeepEqual(names, []string{"root", "b", "a"}) {
		t.Fatalf("order %v", names)
	}
}

const (
	repoA = "wstein/a"
	repoB = "wstein/b"
	repoC = "wstein/c"
)

func targets(slugs ...string) []Target {
	var out []Target
	for _, s := range slugs {
		out = append(out, Target{Name: s, Slug: s})
	}
	return out
}

func read(t *testing.T, f *forge, ts []Target) map[string]Pulls {
	t.Helper()
	got, err := ReadPulls(context.Background(), f, ts)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func prByNumber(t *testing.T, p Pulls, n int) overview.PR {
	t.Helper()
	for _, row := range p.PRs {
		if row.Number == n {
			return row
		}
	}
	t.Fatalf("no row for #%d in %+v", n, p.PRs)
	return overview.PR{}
}

func TestReadPullsSendsNoRequestForSkippedRepositories(t *testing.T) {
	f := newForge()
	got := read(t, f, []Target{{Name: "x", Skip: overview.ReasonNotGitHub}, {Name: "y", Skip: overview.ReasonNotReadLocally}})
	if len(f.calls) != 0 {
		t.Fatalf("requests for skipped repositories: %v", f.calls)
	}
	want := map[string]Pulls{
		"x": {State: overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonNotGitHub}},
		"y": {State: overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonNotReadLocally}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
}

func TestReadPullsMapsRowsToTheOverviewInputs(t *testing.T) {
	f := newForge()
	f.add(human(repoA, 1))
	conflict := human(repoA, 2)
	conflict.mergeable, conflict.state = "false", "dirty"
	f.add(conflict)
	pending := human(repoA, 3)
	pending.class = "pending"
	f.add(pending)
	red := human(repoA, 4)
	red.class = "red"
	f.add(red)
	draft := human(repoA, 5)
	draft.draft = true
	f.add(draft)
	bot := human(repoA, 6)
	bot.user, bot.utype = "dependabot[bot]", "Bot"
	f.add(bot)
	got := read(t, f, targets(repoA))[repoA]
	if got.State != (overview.SourceState{Outcome: overview.OutcomeOK}) || got.Open == nil || *got.Open != 6 {
		t.Fatalf("state %+v open %v", got.State, got.Open)
	}
	for n, want := range map[int]overview.PR{
		1: {Number: 1, Title: "title 1", URL: "https://github.com/wstein/a/pull/1", Author: "alice", Group: overview.GroupReady},
		2: {Number: 2, Title: "title 2", URL: "https://github.com/wstein/a/pull/2", Author: "alice", Group: overview.GroupConflict},
		3: {Number: 3, Title: "title 3", URL: "https://github.com/wstein/a/pull/3", Author: "alice", Group: overview.GroupChecks},
		4: {Number: 4, Title: "title 4", URL: "https://github.com/wstein/a/pull/4", Author: "alice", Group: overview.GroupBlocked, Reason: "checks-red"},
		5: {Number: 5, Title: "title 5", URL: "https://github.com/wstein/a/pull/5", Author: "alice", Group: overview.GroupDraft, Draft: true},
		6: {Number: 6, Title: "title 6", URL: "https://github.com/wstein/a/pull/6", Author: "dependabot[bot]", Group: overview.GroupReady, Bot: true},
	} {
		if row := prByNumber(t, got, n); row != want {
			t.Errorf("#%d: %+v, want %+v", n, row, want)
		}
	}
	if c := got.Counts; c.Total != 6 || c.Ready != 2 || c.Checks != 1 || c.Conflict != 1 || c.Draft != 1 || c.Blocked != 1 || c.Bots != 1 {
		t.Fatalf("counts %+v", c)
	}
	if f.count("detail/"+repoA+"#5") != 0 {
		t.Fatal("a draft was read in detail")
	}
}

func TestReadPullsSharesOneCycleAcrossRepositoriesAndSpellings(t *testing.T) {
	f := newForge()
	f.add(human(repoA, 1))
	f.add(human(repoB, 2))
	got := read(t, f, []Target{{Name: "root", Slug: repoA}, {Name: "copy", Slug: "WSTEIN/a"}, {Name: "b", Slug: repoB}, {Name: "skip", Skip: overview.ReasonOtherHost}})
	if f.count("list/") != 2 {
		t.Fatalf("lists %v", f.calls)
	}
	if !reflect.DeepEqual(got["root"], got["copy"]) || len(got["root"].PRs) != 1 || len(got["b"].PRs) != 1 {
		t.Fatalf("%+v", got)
	}
	if got["skip"].State.Reason != overview.ReasonOtherHost {
		t.Fatalf("%+v", got["skip"])
	}
}

func rereadFixture(first, second string) *forge {
	f := newForge()
	p := human(repoA, 1)
	p.mergeable, p.state = "null", "unknown"
	f.add(p)
	f.seq[repoA+"#1"] = []string{first, second}
	return f
}

func TestReadPullsRereadsUnknownMergeabilityOnce(t *testing.T) {
	p := human(repoA, 1)
	unknown, known := detail(p, "null", "unknown"), detail(p, "true", "clean")

	f := rereadFixture(unknown, known)
	row := prByNumber(t, read(t, f, targets(repoA))[repoA], 1)
	if f.count("detail/") != 2 || row.Group != overview.GroupReady {
		t.Fatalf("unknown then known: %d reads, %+v", f.count("detail/"), row)
	}

	f = rereadFixture(unknown, unknown)
	f.details[repoA+"#1"] = known // a third read would turn the row ready
	row = prByNumber(t, read(t, f, targets(repoA))[repoA], 1)
	if f.count("detail/") != 2 || row.Group != overview.GroupBlocked || row.Reason != "unknown" {
		t.Fatalf("unknown twice: %d reads, %+v", f.count("detail/"), row)
	}

	f = newForge()
	f.add(human(repoA, 1))
	row = prByNumber(t, read(t, f, targets(repoA))[repoA], 1)
	if f.count("detail/") != 1 || row.Group != overview.GroupReady {
		t.Fatalf("known: %d reads, %+v", f.count("detail/"), row)
	}
}

func budgetForge() *forge {
	f := newForge()
	for n := 1; n <= 5; n++ { // the newest five are drafts and spend no budget
		d := human(repoA, n)
		d.draft = true
		f.add(d)
	}
	bot := human(repoA, 6)
	bot.user, bot.utype = "renovate[bot]", "Bot"
	f.add(bot)
	for n := 7; n <= 206; n++ {
		f.add(human(repoA, n))
	}
	return f
}

func TestReadPullsMarksOnlyRowsPastTheBudget(t *testing.T) {
	f := budgetForge()
	got := read(t, f, targets(repoA))[repoA]
	var past []int
	for _, row := range got.PRs {
		if row.PastBudget {
			past = append(past, row.Number)
		}
	}
	// 201 non-drafts compete for 200 reads, the oldest loses; drafts and the bot are not it.
	if !reflect.DeepEqual(past, []int{206}) {
		t.Fatalf("past budget: %v", past)
	}
	if row := prByNumber(t, got, 206); row.Group != overview.GroupBlocked || row.Reason != "not-read" || row.Bot {
		t.Fatalf("%+v", row)
	}
	if row := prByNumber(t, got, 6); row.PastBudget || !row.Bot {
		t.Fatalf("%+v", row)
	}
	if got.Counts.NotRead != 1 {
		t.Fatalf("counts %+v", got.Counts)
	}
}

func TestReadPullsTurnsFailedReadsIntoErrorsOfThatRepositoryOnly(t *testing.T) {
	f := newForge()
	f.add(human(repoA, 1))
	f.add(human(repoB, 2))
	f.errAt["list/"+repoB] = errors.New("connection reset")
	got := read(t, f, targets(repoA, repoB))
	if got[repoA].State.Outcome != overview.OutcomeOK || len(got[repoA].PRs) != 1 {
		t.Fatalf("a: %+v", got[repoA])
	}
	if b := got[repoB]; b.State != (overview.SourceState{Outcome: overview.OutcomeError, Code: hosting.CodeNetwork}) || b.Open != nil || len(b.PRs) != 0 {
		t.Fatalf("b: %+v", b)
	}
}

func TestReadPullsKeepsTheRowsOfAFailedDetailReadAsNotRead(t *testing.T) {
	f := newForge()
	f.add(human(repoA, 1))
	f.add(human(repoA, 2))
	f.errAt["detail/"+repoA+"#1"] = &hosting.WatchError{Code: hosting.CodeRateLimited, Message: "rate limit"}
	got := read(t, f, targets(repoA))[repoA]
	if got.State != (overview.SourceState{Outcome: overview.OutcomeError, Code: hosting.CodeRateLimited}) {
		t.Fatalf("%+v", got.State)
	}
	if len(got.PRs) != 2 || got.PRs[0].Number != 1 || got.PRs[1].Number != 2 {
		t.Fatalf("the rows of a failed read must stay: %+v", got.PRs)
	}
	for _, row := range got.PRs {
		if row.Group != overview.GroupBlocked || row.Reason != "not-read" || row.PastBudget {
			t.Fatalf("a lost read must be not-read without PastBudget: %+v", row)
		}
	}
	if f.count("detail/") != 1 {
		t.Fatalf("reads continued after the rate limit: %v", f.calls)
	}
}

func TestReadPullsTurnsTheDeadlineIntoASkip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newForge()
	f.add(human(repoA, 1))
	f.add(human(repoA, 2))
	f.onRead = func(call string) {
		if strings.HasPrefix(call, "detail/") {
			cancel() // the deadline passes while the first row is read
		}
	}
	got, err := ReadPulls(ctx, f, targets(repoA))
	if err != nil {
		t.Fatalf("a deadline is a skip, not an error: %v", err)
	}
	a := got[repoA]
	if a.State != (overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline}) {
		t.Fatalf("%+v", a.State)
	}
	if len(a.PRs) != 2 || f.count("detail/") != 1 {
		t.Fatalf("rows %d, reads %v", len(a.PRs), f.calls)
	}
}

func TestReadPullsReportsAFailureEvenWhenTheDeadlinePassedLater(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newForge()
	for n := 1; n <= 3; n++ {
		f.add(human(repoA, n))
	}
	f.errAt["detail/"+repoA+"#1"] = errors.New("connection reset")
	f.onRead = func(call string) {
		if call == "detail/"+repoA+"#2" {
			cancel()
		}
	}
	got, err := ReadPulls(ctx, f, targets(repoA))
	if err != nil || got[repoA].State != (overview.SourceState{Outcome: overview.OutcomeError, Code: hosting.CodeNetwork}) {
		t.Fatalf("%v %+v", err, got[repoA].State)
	}
}

func TestReadPullsSkipsEveryRepositoryWhenTheDeadlinePassedBeforeTheStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := newForge()
	f.add(human(repoA, 1))
	got, err := ReadPulls(ctx, f, targets(repoA, repoB))
	if err != nil || len(f.calls) != 0 {
		t.Fatalf("%v %v", err, f.calls)
	}
	for _, slug := range []string{repoA, repoB} {
		if got[slug].State != (overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline}) {
			t.Fatalf("%s: %+v", slug, got[slug].State)
		}
	}
}

// parityForge builds the same fixture each time it is called: three repositories with every verdict
// group, a bot, a draft, an unknown mergeability that resolves, and one whose detail read fails.
func parityForge() *forge {
	f := newForge()
	f.add(human(repoA, 1))
	c := human(repoA, 2)
	c.mergeable, c.state = "false", "dirty"
	f.add(c)
	r := human(repoA, 3)
	r.class = "red"
	f.add(r)
	d := human(repoA, 4)
	d.draft = true
	f.add(d)
	b := human(repoB, 5)
	b.user, b.utype = "dependabot[bot]", "Bot"
	f.add(b)
	p := human(repoB, 6)
	p.class = "pending"
	f.add(p)
	u := human(repoB, 7)
	u.mergeable, u.state = "null", "unknown"
	f.add(u)
	f.seq[repoB+"#7"] = []string{detail(u, "null", "unknown"), detail(u, "true", "clean")}
	f.add(human(repoC, 8))
	f.add(human(repoC, 9))
	f.errAt["detail/"+repoC+"#8"] = &hosting.WatchError{Code: hosting.CodeForbidden, Message: "forbidden"}
	return f
}

func TestStatusCountsEqualTheStwPullsDocument(t *testing.T) {
	var out bytes.Buffer
	err := pullscmd.Execute(context.Background(), pullscmd.Params{
		Host: Host, Repos: []string{repoA, repoB, repoC}, Out: &out,
		Open: func(context.Context, string, string) (pullscmd.Source, func(), error) {
			return parityForge(), func() {}, nil
		},
	})
	var failure *watchcmd.Failure // the failed repository makes `stw pulls` exit 1 after printing
	if !errors.As(err, &failure) || failure.Code != watchcmd.ExitFailure {
		t.Fatalf("stw pulls: %v", err)
	}
	var doc pullscmd.Document
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	got := read(t, parityForge(), targets(repoA, repoB, repoC))

	groups := map[string][]pullscmd.DocRow{overview.GroupReady: doc.Groups.Ready, overview.GroupChecks: doc.Groups.Checks, overview.GroupConflict: doc.Groups.Conflict, overview.GroupDraft: doc.Groups.Draft, overview.GroupBlocked: doc.Groups.Blocked}
	want := map[string][]string{}
	for group, rows := range groups {
		for _, row := range rows {
			want[group] = append(want[group], fmt.Sprintf("%s#%d", row.Repo, row.Number))
		}
	}
	have := map[string][]string{}
	for _, slug := range []string{repoA, repoB, repoC} {
		for _, row := range got[slug].PRs {
			have[row.Group] = append(have[row.Group], fmt.Sprintf("%s#%d", slug, row.Number))
		}
	}
	for _, m := range []map[string][]string{want, have} {
		for _, v := range m {
			sort.Strings(v)
		}
	}
	if !reflect.DeepEqual(have, want) {
		t.Fatalf("groups of status %v, of stw pulls %v", have, want)
	}

	var sum pulls.Summary
	for _, slug := range []string{repoA, repoB, repoC} {
		c := got[slug].Counts
		sum.Total, sum.Ready, sum.Checks, sum.Conflict, sum.Draft, sum.Blocked, sum.Bots, sum.NotRead = sum.Total+c.Total, sum.Ready+c.Ready, sum.Checks+c.Checks, sum.Conflict+c.Conflict, sum.Draft+c.Draft, sum.Blocked+c.Blocked, sum.Bots+c.Bots, sum.NotRead+c.NotRead
	}
	s := doc.Summary
	if sum.Total != s.Total || sum.Ready != s.Ready || sum.Checks != s.Checks || sum.Conflict != s.Conflict || sum.Draft != s.Draft || sum.Blocked != s.Blocked || sum.Bots != s.Bots || sum.NotRead != s.NotRead {
		t.Fatalf("counts of status %+v, of stw pulls %+v", sum, s)
	}
	for _, dr := range doc.Repos {
		g := got[dr.Repo]
		if (g.Open == nil) != (dr.Open == nil) || (g.Open != nil && *g.Open != *dr.Open) {
			t.Fatalf("%s: open %v, stw pulls %v", dr.Repo, g.Open, dr.Open)
		}
		if (dr.Error != nil) != (g.State.Outcome == overview.OutcomeError) || (dr.Error != nil && *dr.Error != g.State.Code) {
			t.Fatalf("%s: state %+v, stw pulls error %v", dr.Repo, g.State, dr.Error)
		}
	}
	if c := got[repoC]; c.State.Code != hosting.CodeForbidden || len(c.PRs) != 2 {
		t.Fatalf("the failed repository: %+v", c)
	}
	if s.Ready != 4 || s.Blocked != 2 || s.Bots != 1 || s.Draft != 1 {
		t.Fatalf("the fixture lost its variety: %+v", s)
	}
}

// build runs the action model over what ReadPulls read for one repository.
func build(p Pulls) []overview.Item {
	local := stwsync.RepoStatus{Name: "a", Path: "a", Kind: "root", State: "ok", Reasons: []string{}}
	remote := &overview.Remote{Sources: map[overview.Source]overview.SourceState{overview.SourcePulls: p.State}, Pulls: p.PRs}
	return overview.Build(overview.Input{Root: "/ws", Repos: []overview.Repo{{Local: local, Slug: repoA, Remote: remote}}}).Items
}

func categories(items []overview.Item) map[overview.Category]int {
	out := map[overview.Category]int{}
	for _, it := range items {
		out[it.Category]++
	}
	return out
}

func TestOnlyPastBudgetRowsBecomeUnreadItems(t *testing.T) {
	got := categories(build(read(t, budgetForge(), targets(repoA))[repoA]))
	if got[overview.PRUnread] != 1 || got[overview.ReadError] != 0 || got[overview.PRBot] != 1 || got[overview.PRReady] != 199 || len(got) != 3 {
		t.Fatalf("%v", got)
	}
}

func TestAFailedReadGivesOneReadErrorAndNoPullRequestItem(t *testing.T) {
	f := newForge()
	f.add(human(repoA, 1))
	f.add(human(repoA, 2))
	f.errAt["detail/"+repoA+"#1"] = &hosting.WatchError{Code: hosting.CodeRateLimited, Message: "rate limit"}
	items := build(read(t, f, targets(repoA))[repoA])
	if got := categories(items); len(got) != 1 || got[overview.ReadError] != 1 {
		t.Fatalf("%v", got)
	}
}
