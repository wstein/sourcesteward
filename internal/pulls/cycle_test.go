// SPDX-License-Identifier: EUPL-1.2
package pulls

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/hosting"
)

func order(r *Result) string {
	var s []string
	for _, x := range r.Rows {
		s = append(s, x.Repo+"#"+strconv.Itoa(x.Number))
	}
	return strings.Join(s, " ")
}

func TestOrderIsNewestFirstThenRepoThenNumber(t *testing.T) {
	f := newForge()
	mk := func(repo string, n int, age time.Duration) {
		p := okPR(repo, n)
		p.age = age
		f.add(p)
	}
	mk("a/x", 3, 48*time.Hour)
	mk("a/x", 1, 24*time.Hour)
	mk("b/y", 2, 24*time.Hour)
	mk("a/x", 2, 24*time.Hour)
	mk("b/y", 9, 72*time.Hour)
	res := run(t, f, Options{Repos: []string{"b/y", "a/x"}})
	if got, want := order(res), "a/x#1 a/x#2 b/y#2 a/x#3 b/y#9"; got != want {
		t.Fatalf("%s != %s", got, want)
	}
}

func TestUnknownCreatedAtIsSortedLastAndHasNoAge(t *testing.T) {
	f := newForge()
	zero := okPR("o/r", 1)
	zero.noTime = true
	old := okPR("o/r", 2)
	old.age = 4000 * 24 * time.Hour
	f.add(zero)
	f.add(old)
	res := run(t, f, Options{Repos: []string{"o/r"}})
	if got := order(res); got != "o/r#2 o/r#1" {
		t.Fatalf("zero created_at must not be the oldest: %s", got)
	}
	if r := row(t, res, "o/r", 1); r.AgeDays != nil || !r.CreatedAt.IsZero() {
		t.Fatalf("%+v", r)
	}
	// With a budget of one, the unknown date is the one left unread.
	res = run(t, f, Options{Repos: []string{"o/r"}, Limit: 1})
	if r := row(t, res, "o/r", 1); r.Detail || r.Reason != "not-read" {
		t.Fatalf("%+v", r)
	}
}

func TestAgeDaysAreWholeDaysAndNeverNegative(t *testing.T) {
	f := newForge()
	for i, age := range []time.Duration{0, 23*time.Hour + 59*time.Minute, 24 * time.Hour, 49 * time.Hour, -50 * time.Hour} {
		p := okPR("o/r", i+1)
		p.age = age
		f.add(p)
	}
	res := run(t, f, Options{Repos: []string{"o/r"}})
	for n, want := range map[int]int{1: 0, 2: 0, 3: 1, 4: 2, 5: 0} {
		if r := row(t, res, "o/r", n); r.AgeDays == nil || *r.AgeDays != want {
			t.Fatalf("#%d age %v want %d", n, r.AgeDays, want)
		}
	}
}

func TestBudgetIsNewestFirstOverTheWholeSetAndDraftsAreFree(t *testing.T) {
	f := newForge()
	for n := 1; n <= 4; n++ {
		p := okPR("a/x", n)
		p.age = time.Duration(n) * 24 * time.Hour // #1 newest
		f.add(p)
	}
	d := okPR("a/x", 9)
	d.draft = true
	d.age = time.Hour // newest of all, but a draft consumes no budget
	f.add(d)
	p := okPR("b/y", 1)
	p.age = 36 * time.Hour // between #1 and #2 of a/x
	f.add(p)
	res := run(t, f, Options{Repos: []string{"a/x", "b/y"}, Limit: 2})
	read := map[string]bool{}
	for _, r := range res.Rows {
		read[r.Repo+"#"+strconv.Itoa(r.Number)] = r.Detail
	}
	want := map[string]bool{"a/x#1": true, "b/y#1": true, "a/x#2": false, "a/x#3": false, "a/x#4": false, "a/x#9": false}
	for k, v := range want {
		if read[k] != v {
			t.Fatalf("%s detail=%v want %v (%v)", k, read[k], v, read)
		}
	}
	if res.Summary.NotRead != 3 {
		t.Fatalf("not_read %d", res.Summary.NotRead)
	}
	if r := row(t, res, "a/x", 9); r.Group != "draft" || r.Checks != nil || r.Mergeable != nil || r.MergeableState != nil || r.Rebaseable != nil {
		t.Fatalf("draft without detail: %+v", r)
	}
	if n := f.count("detail/a/x#9"); n != 0 {
		t.Fatalf("draft read %d times", n)
	}
}

func TestDetailDraftsConsumeTheBudgetAndStayDrafts(t *testing.T) {
	f := newForge()
	d := okPR("o/r", 1)
	d.draft = true
	d.age = time.Hour
	f.add(d)
	p := okPR("o/r", 2)
	p.age = 48 * time.Hour
	f.add(p)
	res := run(t, f, Options{Repos: []string{"o/r"}, DetailDrafts: true, Limit: 1})
	if r := row(t, res, "o/r", 1); !r.Detail || r.Group != "draft" || r.Checks == nil || *r.Checks != "green" {
		t.Fatalf("%+v", r)
	}
	if r := row(t, res, "o/r", 2); r.Detail || r.Reason != "not-read" {
		t.Fatalf("%+v", r)
	}
}

func TestDefaultAndMaximumLimit(t *testing.T) {
	f := newForge()
	for n := 1; n <= 205; n++ {
		p := okPR("o/r", n)
		p.age = time.Duration(n) * time.Hour
		f.add(p)
	}
	res := run(t, f, Options{Repos: []string{"o/r"}})
	if res.Limit != 200 || res.Summary.NotRead != 5 || f.count("detail/") != 200 {
		t.Fatalf("limit=%d not_read=%d details=%d", res.Limit, res.Summary.NotRead, f.count("detail/"))
	}
	res = run(t, newForge(), Options{Repos: []string{"o/r"}, Limit: 500})
	if res.Limit != 500 {
		t.Fatal(res.Limit)
	}
	for _, bad := range []int{-1, 501} {
		if _, err := Run(context.Background(), newForge(), Options{Repos: []string{"o/r"}, Limit: bad}); err == nil {
			t.Fatalf("limit %d accepted", bad)
		}
	}
}

func TestBotsAndFilters(t *testing.T) {
	f := newForge()
	add := func(n int, user, typ string) {
		p := okPR("o/r", n)
		p.user, p.utype = user, typ
		f.add(p)
	}
	add(1, "alice", "User")
	add(2, "dependabot[bot]", "Bot")
	add(3, "upgrade-bot[bot]", "") // marker by suffix
	add(4, "machine-user", "User") // unmarked
	add(5, "Carol", "User")
	nums := func(o Options) string {
		o.Repos = []string{"o/r"}
		res := run(t, f, o)
		var s []string
		for _, r := range res.Rows {
			s = append(s, strconv.Itoa(r.Number))
		}
		sortStrings(s)
		return strings.Join(s, "")
	}
	for name, c := range map[string]struct {
		o    Options
		want string
	}{
		"default hides nothing": {Options{}, "12345"},
		"no bots":               {Options{NoBots: true}, "145"},
		"bots only":             {Options{BotsOnly: true}, "23"},
		"author":                {Options{Authors: []string{"alice"}}, "1"},
		"author case":           {Options{Authors: []string{"carol"}}, "5"},
		"authors union":         {Options{Authors: []string{"alice", "dependabot[bot]"}}, "12"},
		"bots-only and author":  {Options{BotsOnly: true, Authors: []string{"alice"}}, ""},
		"bots-only and bot":     {Options{BotsOnly: true, Authors: []string{"dependabot[bot]"}}, "2"},
	} {
		if got := nums(c.o); got != c.want {
			t.Errorf("%s: %q want %q", name, got, c.want)
		}
	}
	res := run(t, f, Options{Repos: []string{"o/r"}})
	for n, want := range map[int]bool{1: false, 2: true, 3: true, 4: false} {
		if r := row(t, res, "o/r", n); r.Bot != want {
			t.Errorf("#%d bot=%v", n, r.Bot)
		}
	}
	if res.Summary.Bots != 2 {
		t.Fatalf("bots %d", res.Summary.Bots)
	}
}

func sortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

// Filters apply before the budget is spent: filtered-out PRs cost nothing.
func TestFiltersApplyBeforeTheBudget(t *testing.T) {
	f := newForge()
	bot := okPR("o/r", 1)
	bot.user, bot.utype = "dependabot[bot]", "Bot"
	bot.age = time.Hour
	f.add(bot)
	h := okPR("o/r", 2)
	h.age = 48 * time.Hour
	f.add(h)
	res := run(t, f, Options{Repos: []string{"o/r"}, NoBots: true, Limit: 1})
	if r := row(t, res, "o/r", 2); !r.Detail {
		t.Fatalf("%+v", r)
	}
	if f.count("detail/o/r#1") != 0 || len(res.Rows) != 1 {
		t.Fatal("filtered PR was read or listed")
	}
	if res.Repos[0].Open == nil || *res.Repos[0].Open != 2 || res.Repos[0].Listed != 1 {
		t.Fatalf("%+v", res.Repos[0])
	}
}

func TestReposAreDeduplicatedCaseInsensitivelyAndSorted(t *testing.T) {
	f := newForge()
	f.add(okPR("a/x", 1))
	f.add(okPR("b/y", 1))
	res := run(t, f, Options{Repos: []string{"b/y", "A/X", "a/x"}})
	if len(res.Repos) != 2 || res.Repos[0].Repo != "A/X" && res.Repos[0].Repo != "a/x" || res.Repos[1].Repo != "b/y" {
		t.Fatalf("%+v", res.Repos)
	}
	if f.count("list/") != 2 {
		t.Fatalf("%v", f.calls)
	}
}

func TestPartialFailureNamesTheRepositoryAndKeepsTheRest(t *testing.T) {
	f := newForge()
	f.add(okPR("a/x", 1))
	f.add(okPR("b/y", 1))
	f.listErr["a/x"] = &hosting.WatchError{Code: hosting.CodeForbidden, Message: "no access"}
	res := run(t, f, Options{Repos: []string{"a/x", "b/y"}})
	if len(res.Rows) != 1 || res.Rows[0].Repo != "b/y" || res.Summary.ReposRead != 1 || res.Summary.ReposFailed != 1 {
		t.Fatalf("%+v", res.Summary)
	}
	if len(res.Errors) != 1 || res.Errors[0].Repo != "a/x" || res.Errors[0].Code != "forbidden" || res.Repos[0].Error != "forbidden" || res.Repos[0].Open != nil {
		t.Fatalf("%+v %+v", res.Errors, res.Repos)
	}
	// A foreign error becomes a bounded network error without its text.
	f.listErr["a/x"] = errors.New("token ghp_secret leaked in message")
	res = run(t, f, Options{Repos: []string{"a/x", "b/y"}})
	if res.Errors[0].Code != "network" || strings.Contains(res.Errors[0].Message, "ghp_") {
		t.Fatalf("%+v", res.Errors[0])
	}
}

func TestDetailFailureFailsClosedAndNamesTheRepository(t *testing.T) {
	for name, key := range map[string]string{"detail": "detail/o/r#1", "checks": "runs/" + sha("2")[:4]} {
		f := newForge()
		a := okPR("o/r", 1)
		a.head = sha("2")
		f.add(a)
		f.add(okPR("o/r", 2))
		f.errAt[key] = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "boom"}
		res := run(t, f, Options{Repos: []string{"o/r"}})
		if r := row(t, res, "o/r", 1); r.Ready || r.Group != "blocked" || r.Reason != "not-read" || r.Detail {
			t.Fatalf("%s: %+v", name, r)
		}
		if r := row(t, res, "o/r", 2); !r.Ready {
			t.Fatalf("%s: the other PR must still be read: %+v", name, r)
		}
		if len(res.Errors) != 1 || res.Errors[0].Code != "http-5xx" || res.Summary.ReposFailed != 1 {
			t.Fatalf("%s: %+v %+v", name, res.Errors, res.Summary)
		}
	}
}

func TestMalformedDetailIsBlockedNotReadAndDoesNotStopTheRun(t *testing.T) {
	f := newForge()
	f.add(okPR("o/r", 1))
	f.details["o/r#1"] = `not json`
	res := run(t, f, Options{Repos: []string{"o/r"}})
	if r := row(t, res, "o/r", 1); r.Group != "blocked" || r.Reason != "not-read" || len(res.Errors) != 1 || res.Errors[0].Code != "malformed-response" {
		t.Fatalf("%+v %+v", r, res.Errors)
	}
}

func TestRateLimitStopsReadingAndMarksTheRest(t *testing.T) {
	f := newForge()
	for _, repo := range []string{"a/x", "b/y", "c/z", "d/w", "e/v"} {
		f.add(okPR(repo, 1))
		f.add(okPR(repo, 2))
	}
	f.errAt["detail/a/x#1"] = &hosting.WatchError{Code: hosting.CodeRateLimited, Message: "limit"}
	res := run(t, f, Options{Repos: []string{"a/x", "b/y", "c/z", "d/w", "e/v"}, Parallel: 1})
	if r := row(t, res, "a/x", 1); r.Group != "blocked" || r.Reason != "not-read" {
		t.Fatalf("%+v", r)
	}
	if n := f.count("detail/"); n != 1 {
		t.Fatalf("reading continued after rate limit: %d detail reads", n)
	}
	limited := 0
	for _, e := range res.Errors {
		if e.Code == "rate-limited" {
			limited++
		}
	}
	if limited == 0 || res.Summary.ReposFailed == 0 {
		t.Fatalf("%+v %+v", res.Errors, res.Summary)
	}
	for _, r := range res.Rows {
		if r.Detail || r.Group != "blocked" || r.Reason != "not-read" {
			t.Fatalf("row read after the limit: %+v", r)
		}
	}
	// Unlisted repositories are reported, not silently dropped.
	if len(res.Repos) != 5 {
		t.Fatalf("%+v", res.Repos)
	}
}

func TestTooManyOpenPullsIsAPerRepositoryError(t *testing.T) {
	f := newForge()
	f.listErr["o/r"] = &hosting.WatchError{Code: hosting.CodeTooManyPRs, Message: "more"}
	res := run(t, f, Options{Repos: []string{"o/r"}})
	if len(res.Errors) != 1 || res.Errors[0].Code != "too-many-open-prs" {
		t.Fatalf("%+v", res.Errors)
	}
}

func TestUnreadableListedHeadFailsClosed(t *testing.T) {
	f := newForge()
	f.add(okPR("o/r", 1))
	f.lists["o/r"][0].HeadSHA = strings.ToUpper(sha("a"))
	if r := row(t, run(t, f, Options{Repos: []string{"o/r"}}), "o/r", 1); r.Ready || r.Reason != "unknown" {
		t.Fatalf("%+v", r)
	}
}

func TestReadsAreUnconditionalAndOnlyTheExpectedKinds(t *testing.T) {
	f := newForge()
	f.add(okPR("o/r", 1))
	run(t, f, Options{Repos: []string{"o/r"}})
	if len(f.etags) != 0 {
		t.Fatalf("validators sent: %v", f.etags)
	}
	for _, c := range f.calls {
		if !strings.HasPrefix(c, "list/") && !strings.HasPrefix(c, "detail/") && !strings.HasPrefix(c, "runs/") && !strings.HasPrefix(c, "stats/") {
			t.Fatalf("unexpected read %q", c)
		}
	}
}

// Spec 036: no write and no state file. The package imports nothing that can write or reach the network.
func TestPackageHasNoFilesystemOrNetworkImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == "os" || strings.HasPrefix(p, "os/") || p == "io/ioutil" || p == "net" || strings.HasPrefix(p, "net/") || p == "syscall" {
				t.Errorf("%s imports %s", name, p)
			}
		}
	}
}

func TestOnlyHTTPSURLsAreKept(t *testing.T) {
	f := newForge()
	f.add(okPR("o/r", 1))
	f.add(okPR("o/r", 2))
	f.lists["o/r"][1].HTMLURL = "http://github.com/o/r/pull/2"
	res := run(t, f, Options{Repos: []string{"o/r"}})
	if row(t, res, "o/r", 1).URL == "" || row(t, res, "o/r", 2).URL != "" {
		t.Fatalf("%+v", res.Rows)
	}
}

func TestLongStringsAreCutOnARuneBoundary(t *testing.T) {
	f := newForge()
	f.add(okPR("o/r", 1))
	f.lists["o/r"][0].Title = strings.Repeat("\u00e9", 2000) // 4000 bytes
	got := row(t, run(t, f, Options{Repos: []string{"o/r"}}), "o/r", 1).Title
	if len(got) != 2048 || !utf8.ValidString(got) {
		t.Fatalf("len %d valid %v", len(got), utf8.ValidString(got))
	}
}
