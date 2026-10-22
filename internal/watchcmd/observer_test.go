// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watch"
)

func sha40(seed string) string { return strings.Repeat(seed, 40)[:40] }

// fakeReader serves canned documents and records every call.
type fakeReader struct {
	items    []hosting.PullItem
	listErr  error
	details  map[int]string // pulls/N body
	detailET map[int]string
	tips     map[string]string // base -> sha
	runs     map[string]string // sha -> check_runs body
	stats    map[string]string // sha -> statuses body
	commits  map[string]string
	pulls    map[int]string // pulls/N/commits body
	rate     hosting.RateInfo
	calls    []string
	etags    map[string]string // call -> validator sent
	errAt    map[string]error
}

func newFake() *fakeReader {
	return &fakeReader{details: map[int]string{}, detailET: map[int]string{}, tips: map[string]string{}, runs: map[string]string{},
		stats: map[string]string{}, commits: map[string]string{}, pulls: map[int]string{}, etags: map[string]string{}, errAt: map[string]error{}}
}

func (f *fakeReader) note(call, etag string) error {
	f.calls = append(f.calls, call)
	f.etags[call] = etag
	return f.errAt[call]
}

func (f *fakeReader) count(prefix string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeReader) ListOpenPulls(_ context.Context, _ string, prior hosting.PullListState, _ int) (hosting.PullListResult, error) {
	f.calls = append(f.calls, "list")
	if f.listErr != nil {
		return hosting.PullListResult{}, f.listErr
	}
	st := hosting.PullListState{PerPage: 25, Pages: []hosting.PullPage{{ETag: `"p1"`, Link: "", Items: f.items}}}
	return hosting.PullListResult{State: st, Items: f.items}, nil
}

func (f *fakeReader) PullDetail(_ context.Context, _ string, n int, etag string) (hosting.WatchResponse, error) {
	if err := f.note(fmt.Sprintf("detail/%d", n), etag); err != nil {
		return hosting.WatchResponse{}, err
	}
	if etag != "" && etag == f.detailET[n] {
		return hosting.WatchResponse{Status: 304, NotModified: true, ETag: etag}, nil
	}
	return hosting.WatchResponse{Status: 200, ETag: f.detailET[n], Body: []byte(f.details[n])}, nil
}

func (f *fakeReader) Branch(_ context.Context, _ string, name, etag string) (hosting.WatchResponse, error) {
	if err := f.note("branch/"+name, etag); err != nil {
		return hosting.WatchResponse{}, err
	}
	tag := `"t-` + f.tips[name] + `"`
	if etag == tag {
		return hosting.WatchResponse{Status: 304, NotModified: true, ETag: etag}, nil
	}
	return hosting.WatchResponse{Status: 200, ETag: tag, Body: []byte(`{"commit":{"sha":"` + f.tips[name] + `"}}`)}, nil
}

func (f *fakeReader) Commit(_ context.Context, _ string, sha, etag string) (hosting.WatchResponse, error) {
	if err := f.note("commit/"+sha[:4], etag); err != nil {
		return hosting.WatchResponse{}, err
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(f.commits[sha])}, nil
}

func (f *fakeReader) CheckRuns(_ context.Context, _ string, sha string, page int, etag string) (hosting.WatchResponse, error) {
	if err := f.note("runs/"+sha[:4], etag); err != nil {
		return hosting.WatchResponse{}, err
	}
	tag := `"r-` + fmt.Sprint(len(f.runs[sha])) + `"`
	if etag == tag {
		return hosting.WatchResponse{Status: 304, NotModified: true, ETag: etag}, nil
	}
	body := f.runs[sha]
	if body == "" {
		body = `{"total_count":0,"check_runs":[]}`
	}
	return hosting.WatchResponse{Status: 200, ETag: tag, Body: []byte(body)}, nil
}

func (f *fakeReader) Statuses(_ context.Context, _ string, sha string, page int, etag string) (hosting.WatchResponse, error) {
	if err := f.note("stats/"+sha[:4], etag); err != nil {
		return hosting.WatchResponse{}, err
	}
	tag := `"s-` + fmt.Sprint(len(f.stats[sha])) + `"`
	if etag == tag {
		return hosting.WatchResponse{Status: 304, NotModified: true, ETag: etag}, nil
	}
	body := f.stats[sha]
	if body == "" {
		body = `[]`
	}
	return hosting.WatchResponse{Status: 200, ETag: tag, Body: []byte(body)}, nil
}

func (f *fakeReader) PullCommits(_ context.Context, _ string, n, page int, etag string) (hosting.WatchResponse, error) {
	if err := f.note(fmt.Sprintf("commits/%d/%d", n, page), etag); err != nil {
		return hosting.WatchResponse{}, err
	}
	body := f.pulls[n]
	if body == "" || page > 1 {
		body = `[]`
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(body)}, nil
}

func (f *fakeReader) RateLimit(context.Context) (hosting.RateInfo, error) { return f.rate, nil }

func item(n int, base string) hosting.PullItem {
	return hosting.PullItem{Number: n, Title: fmt.Sprintf("PR %d", n), User: "alice", Draft: false, HeadSHA: sha40(fmt.Sprintf("%x", n)),
		HeadRef: fmt.Sprintf("feat/%d", n), BaseRef: base, Labels: []string{"x"}}
}

const detailJSON = `{"mergeable":true,"mergeable_state":"clean","head":{"repo":{"full_name":"o/r"}},"base":{"repo":{"full_name":"o/r"}}}`

func newObserver(f *fakeReader) *HostObserver {
	return NewHostObserver(f, func() time.Time { return time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC) }, 2000, nil, nil)
}

// observe runs one Observe and carries the stored conditional into the next call, as the core does.
type session struct {
	o    *HostObserver
	f    *fakeReader
	cond *watch.Conditional
	know []watch.KnownPR
	cyc  int
}

func (s *session) observe(t *testing.T) watch.RepoObservation {
	t.Helper()
	s.f.calls = nil
	s.cyc++
	obs, err := s.o.Observe(context.Background(), "o/r", watch.Hints{Cycle: s.cyc, DetailEvery: 10, Known: s.know, Cond: s.cond})
	if err != nil {
		t.Fatal(err)
	}
	if obs.Cond != nil {
		s.cond = obs.Cond
	}
	return obs
}

func (s *session) accept(obs watch.RepoObservation) {
	s.know = nil
	for _, p := range obs.PRs {
		class := ""
		if p.Checks != nil {
			class = watch.ClassifyChecks(p.Checks.Runs, p.Checks.Statuses, "", "").Class
		}
		for _, k := range s.know {
			_ = k
		}
		s.know = append(s.know, watch.KnownPR{Number: p.Number, Head: p.Head, Class: class})
	}
}

func TestObserveBaselineReadsEveryPRFully(t *testing.T) {
	f := newFake()
	f.items = []hosting.PullItem{item(1, "main"), item(2, "feat/1")}
	f.tips["main"], f.tips["feat/1"] = sha40("a"), sha40("b")
	f.details[1], f.details[2] = detailJSON, `{"mergeable":null,"mergeable_state":"unknown","head":{"repo":null},"base":{"repo":{"full_name":"o/r"}}}`
	f.detailET[1], f.detailET[2] = `"d1"`, `"d2"`
	f.runs[item(1, "").HeadSHA] = `{"total_count":2,"check_runs":[{"name":"ci","status":"completed","conclusion":"failure","output":{"title":"t","summary":"--- FAIL: TestX","text":""}},{"name":"lint","status":"in_progress","conclusion":null,"output":{}}]}`
	f.stats[item(1, "").HeadSHA] = `[{"context":"legacy","state":"success","description":"new"},{"context":"legacy","state":"failure","description":"old"}]`
	s := &session{o: newObserver(f), f: f}
	obs := s.observe(t)
	if len(obs.PRs) != 2 || obs.DefaultBranch != "main" || obs.Cond == nil {
		t.Fatalf("%+v", obs)
	}
	p1, p2 := obs.PRs[0], obs.PRs[1]
	if p1.Number != 1 || p1.Author != "alice" || p1.HeadRef != "feat/1" || p1.Base != "main" || len(p1.Labels) != 1 {
		t.Fatalf("%+v", p1)
	}
	if p1.Detail == nil || p1.Detail.Mergeable == nil || !*p1.Detail.Mergeable || p1.Detail.State != "clean" {
		t.Fatalf("detail %+v", p1.Detail)
	}
	if p2.Detail == nil || p2.Detail.Mergeable != nil || p2.Detail.State != "unknown" {
		t.Fatalf("a null mergeable is not computed: %+v", p2.Detail)
	}
	if !p2.Fork || p1.Fork {
		t.Fatalf("fork from pulls/N: %v %v", p1.Fork, p2.Fork)
	}
	if p1.Checks == nil || len(p1.Checks.Runs) != 2 || p1.Checks.Runs[0].Summary != "--- FAIL: TestX" || len(p1.Checks.Statuses) != 1 || p1.Checks.Statuses[0].State != "success" {
		t.Fatalf("checks %+v", p1.Checks)
	}
	if res := watch.ClassifyChecks(p1.Checks.Runs, p1.Checks.Statuses, "main", "main"); res.Class != watch.ClassRed || res.Shape != watch.ShapeReal {
		t.Fatalf("%+v", res)
	}
	if obs.Cond.Tips["main"].SHA != sha40("a") || obs.Cond.Details["pulls/1"] != `"d1"` || len(obs.Cond.Pages) != 1 {
		t.Fatalf("%+v", obs.Cond)
	}
}

func TestObserveSteadyStateReadsNothingBeyondTheConditionalBasics(t *testing.T) {
	f := newFake()
	f.items = []hosting.PullItem{item(1, "main"), item(2, "main")}
	f.tips["main"] = sha40("a")
	f.details[1], f.details[2] = detailJSON, detailJSON
	for _, it := range f.items {
		f.runs[it.HeadSHA] = `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{}}]}`
	}
	s := &session{o: newObserver(f), f: f}
	s.accept(s.observe(t))
	s.cyc = 1 // cycles 2..: (cycle+number) % 10 never 0 for 2..8
	obs := s.observe(t)
	if f.count("detail") != 0 || f.count("runs") != 0 || f.count("stats") != 0 {
		t.Fatalf("settled PRs must not be re-read: %v", f.calls)
	}
	if f.count("branch/main") != 1 || f.etags["branch/main"] == "" {
		t.Fatalf("one conditional tip read per base: %v %v", f.calls, f.etags)
	}
	for _, p := range obs.PRs {
		if p.Detail != nil || p.Checks != nil {
			t.Fatalf("nothing read, nothing reported: %+v", p)
		}
	}
}

func TestObserveReadsDetailWhenItIsNeeded(t *testing.T) {
	setup := func() (*session, *fakeReader) {
		f := newFake()
		f.items = []hosting.PullItem{item(1, "main"), item(2, "main"), item(3, "feat/1")}
		f.tips["main"], f.tips["feat/1"] = sha40("a"), sha40("b")
		for n := 1; n <= 3; n++ {
			f.details[n] = detailJSON
			f.detailET[n] = fmt.Sprintf(`"d%d"`, n)
		}
		for _, it := range f.items {
			f.runs[it.HeadSHA] = `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{}}]}`
		}
		s := &session{o: newObserver(f), f: f}
		s.accept(s.observe(t))
		s.cyc = 1
		return s, f
	}
	t.Run("head changed", func(t *testing.T) {
		s, f := setup()
		f.items[0].HeadSHA = sha40("f")
		f.runs[sha40("f")] = f.runs[item(2, "").HeadSHA]
		s.observe(t)
		if f.count("detail/1") != 1 || f.count("detail/2") != 0 || f.count("runs/ffff") != 1 {
			t.Fatalf("%v", f.calls)
		}
	})
	t.Run("base tip moved", func(t *testing.T) {
		s, f := setup()
		f.tips["main"] = sha40("c")
		s.observe(t)
		if f.count("detail/1") != 1 || f.count("detail/2") != 1 || f.count("detail/3") != 0 {
			t.Fatalf("only the PRs on the moved base: %v", f.calls)
		}
		if f.etags["detail/1"] != "" {
			t.Fatal("a moved base must force a fresh read, not a conditional one")
		}
	})
	t.Run("a merge in the same cycle", func(t *testing.T) {
		s, f := setup()
		s.know = append(s.know, watch.KnownPR{Number: 9, Head: sha40("9"), Class: watch.ClassGreen})
		f.details[9] = `{"state":"closed","merged":true,"merge_commit_sha":"` + sha40("e") + `","merged_at":"2026-10-22T08:00:00Z","base":{"ref":"main"},"head":{"sha":"` + sha40("9") + `","ref":"feat/9"}}`
		f.commits[sha40("e")] = `{"parents":[{"sha":"x"}]}`
		obs := s.observe(t)
		if f.count("detail/1") != 1 || f.count("detail/2") != 1 || f.count("detail/3") != 1 {
			t.Fatalf("every open PR is re-read after a merge: %v", f.calls)
		}
		g, ok := obs.Gone[9]
		if !ok || !g.Merged || g.MergeCommit == nil || *g.MergeCommit != sha40("e") || g.Head != sha40("9") || g.HeadRef != "feat/9" || g.Base != "main" ||
			g.MethodHint != "rebase-or-squash" || g.MergedAt != "2026-10-22T08:00:00Z" {
			t.Fatalf("%+v", g)
		}
	})
	t.Run("due flag and the staggered slow refresh", func(t *testing.T) {
		s, f := setup()
		s.know[1].DetailDue = true
		s.cyc = 6 // the next cycle is 7: (7+3) % 10 == 0, PR 3 is refreshed
		s.observe(t)
		if f.count("detail/2") != 1 || f.count("detail/3") != 1 || f.count("detail/1") != 0 {
			t.Fatalf("%v", f.calls)
		}
		if f.etags["detail/3"] != `"d3"` {
			t.Fatalf("the slow refresh is conditional: %q", f.etags["detail/3"])
		}
		if f.etags["detail/2"] != "" {
			t.Fatal("a due PR is read fresh")
		}
	})
}

func TestObserveGoneClosedMergedMethodAndStillOpen(t *testing.T) {
	f := newFake()
	f.tips["main"] = sha40("a")
	f.items = []hosting.PullItem{item(1, "main")}
	f.details[1] = detailJSON
	f.details[5] = `{"state":"closed","merged":false,"base":{"ref":"main"},"head":{"sha":"` + sha40("5") + `","ref":"feat/5"}}`
	f.details[6] = `{"state":"closed","merged":true,"merge_commit_sha":"` + sha40("d") + `","merged_at":"2026-10-22T08:00:00Z","base":{"ref":"main"},"head":{"sha":"` + sha40("6") + `","ref":"feat/6"}}`
	f.details[7] = `{"state":"open","merged":false,"base":{"ref":"main"},"head":{"sha":"` + sha40("7") + `","ref":"feat/7"}}`
	f.details[8] = `{"state":"closed","merged":true,"merge_commit_sha":null,"merged_at":"2026-10-22T08:00:00Z","base":{"ref":"main"},"head":{"sha":"` + sha40("8") + `","ref":"feat/8"}}`
	f.commits[sha40("d")] = `{"parents":[{"sha":"1"},{"sha":"2"}]}`
	s := &session{o: newObserver(f), f: f}
	s.know = []watch.KnownPR{{Number: 5}, {Number: 6}, {Number: 7}, {Number: 8}}
	obs := s.observe(t)
	if g := obs.Gone[5]; g.Merged || g.Head != sha40("5") || g.Base != "main" {
		t.Fatalf("closed: %+v", g)
	}
	if g := obs.Gone[6]; !g.Merged || g.MethodHint != "merge" {
		t.Fatalf("merge commit with two parents: %+v", g)
	}
	if _, ok := obs.Gone[7]; ok {
		t.Fatal("a PR that is still open stays unresolved")
	}
	if g := obs.Gone[8]; !g.Merged || g.MergeCommit != nil || g.MethodHint != "unknown" {
		t.Fatalf("unreadable merge commit: %+v", g)
	}
	// the commit read failing only makes the hint unknown
	f.errAt["commit/dddd"] = &hosting.WatchError{Code: hosting.CodeNetwork, Message: "x"}
	if g := s.observe(t).Gone[6]; g.MethodHint != "unknown" || g.MergeCommit == nil {
		t.Fatalf("%+v", g)
	}
	// but an unreadable pulls/N fails the repository: no merged event on a guess
	f.errAt["detail/6"] = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "boom"}
	if _, err := s.o.Observe(context.Background(), "o/r", watch.Hints{Cycle: 3, DetailEvery: 10, Known: s.know, Cond: s.cond}); err == nil {
		t.Fatal("expected a failure")
	}
}

func TestObserveConfirmsADebounceCandidateWithAFreshRead(t *testing.T) {
	f := newFake()
	f.items = []hosting.PullItem{item(1, "main")}
	f.tips["main"] = sha40("a")
	f.details[1] = detailJSON
	h := f.items[0].HeadSHA
	f.runs[h] = `{"total_count":1,"check_runs":[{"name":"ci","status":"in_progress","conclusion":null,"output":{}}]}`
	s := &session{o: newObserver(f), f: f}
	s.accept(s.observe(t)) // accepted class: pending
	s.cyc = 1
	// unchanged pending: a conditional read, answered 304
	obs := s.observe(t)
	if f.count("runs") != 1 || f.etags["runs/"+h[:4]] == "" || obs.PRs[0].Checks != nil {
		t.Fatalf("%v %v", f.calls, f.etags)
	}
	// the run completes: a new body
	f.runs[h] = `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{}}]}`
	obs = s.observe(t)
	if obs.PRs[0].Checks == nil {
		t.Fatal("the change must be reported")
	}
	// the core has only a candidate (accepted class still pending): the next read must be fresh even
	// though nothing changed upstream, so the second consecutive sighting reaches the core
	obs = s.observe(t)
	if obs.PRs[0].Checks == nil || f.etags["runs/"+h[:4]] != "" {
		t.Fatalf("candidate not confirmed: %+v etag %q", obs.PRs[0].Checks, f.etags["runs/"+h[:4]])
	}
	// once the core has accepted green, the PR is settled and is not read any more
	s.know[0].Class = watch.ClassGreen
	s.observe(t)
	if f.count("runs") != 0 {
		t.Fatalf("%v", f.calls)
	}
}

func TestObserveSkipsIgnoredPRs(t *testing.T) {
	f := newFake()
	bot := item(1, "main")
	bot.User = "dependabot[bot]"
	labelled := item(2, "main")
	labelled.Labels = []string{"no-watch"}
	f.items = []hosting.PullItem{bot, labelled, item(3, "main")}
	f.tips["main"] = sha40("a")
	f.details[3] = detailJSON
	o := NewHostObserver(f, nil, 2000, []string{"dependabot[bot]"}, []string{"no-watch"})
	obs, err := o.Observe(context.Background(), "o/r", watch.Hints{Cycle: 1, DetailEvery: 10})
	if err != nil || len(obs.PRs) != 3 {
		t.Fatalf("the core still needs the list to see an ignored PR: %v %+v", err, obs)
	}
	if f.count("detail/1")+f.count("detail/2")+f.count("runs/1")+f.count("runs/2") != 0 || f.count("detail/3") != 1 {
		t.Fatalf("%v", f.calls)
	}
}

func TestObserveMapsFailuresAndTheRateLimit(t *testing.T) {
	f := newFake()
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	o := NewHostObserver(f, func() time.Time { return now }, 2000, nil, nil)
	f.listErr = &hosting.WatchError{Code: hosting.CodeTooManyPRs, Message: "more open pull requests than max_open_prs"}
	_, err := o.Observe(context.Background(), "o/r", watch.Hints{})
	var oe *watch.ObserveError
	if !errors.As(err, &oe) || oe.Code != "too-many-open-prs" || oe.RunWide {
		t.Fatalf("%v", err)
	}
	f.listErr = &hosting.WatchError{Code: hosting.CodeRateLimited, Message: "rate limit reached", RetryAfter: 30 * time.Second}
	_, err = o.Observe(context.Background(), "o/r", watch.Hints{})
	if !errors.As(err, &oe) || oe.Code != "rate-limited" || !oe.RunWide || oe.RetryIn != 35*time.Second {
		t.Fatalf("%+v", oe)
	}
	f.listErr = errors.New("something else")
	if _, err = o.Observe(context.Background(), "o/r", watch.Hints{}); !errors.As(err, &oe) || oe.Code != "network" {
		t.Fatalf("%v", err)
	}
	f.listErr = nil
	// slow: remaining below 10 percent; wait: below 50
	f.rate = hosting.RateInfo{Known: true, Limit: 5000, Remaining: 400, Reset: now.Add(time.Hour)}
	if !o.AfterCycle(context.Background()) {
		t.Fatal("expected slow")
	}
	if _, err = o.Observe(context.Background(), "o/r", watch.Hints{}); err != nil {
		t.Fatal("slow only doubles the interval")
	}
	f.rate = hosting.RateInfo{Known: true, Limit: 5000, Remaining: 10, Reset: now.Add(10 * time.Minute)}
	o.AfterCycle(context.Background())
	_, err = o.Observe(context.Background(), "o/r", watch.Hints{})
	if !errors.As(err, &oe) || oe.Code != "rate-limited" || !oe.RunWide || oe.RetryIn != 10*time.Minute+5*time.Second {
		t.Fatalf("%v", err)
	}
	if _, err = o.Observe(context.Background(), "o/r", watch.Hints{}); err != nil {
		t.Fatal("the hold is reported once")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.listErr = &hosting.WatchError{Code: hosting.CodeNetwork, Message: "canceled"}
	if _, err = o.Observe(ctx, "o/r", watch.Hints{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled run is not a repository failure: %v", err)
	}
}

func TestStoredListRoundTrip(t *testing.T) {
	st := hosting.PullListState{PerPage: 12, Pages: []hosting.PullPage{
		{ETag: `"a"`, Link: `<x>; rel="next"`, Items: []hosting.PullItem{item(1, "main")}}, {ETag: `"b"`, Items: []hosting.PullItem{item(2, "main")}}}}
	raw, _ := json.Marshal(st)
	back := storedList(&watch.Conditional{Pages: storeList(st)})
	got, _ := json.Marshal(back)
	if string(raw) != string(got) {
		t.Fatalf("%s != %s", raw, got)
	}
	if got := storedList(&watch.Conditional{Pages: map[string]watch.PageEntry{"25/2": {ETag: `"x"`}}}); got.PerPage != 0 || len(got.Pages) != 0 {
		t.Fatalf("a gap in the pages discards the store: %+v", got)
	}
}

func TestPruneCondDropsClosedPRs(t *testing.T) {
	f := newFake()
	f.items = []hosting.PullItem{item(1, "main")}
	f.tips["main"] = sha40("a")
	f.details[1] = detailJSON
	f.detailET[1] = `"d1"`
	s := &session{o: newObserver(f), f: f}
	s.cond = &watch.Conditional{Tips: map[string]watch.TipEntry{"old": {SHA: sha40("1")}}, Details: map[string]string{"pulls/9": `"x"`, "checks/" + sha40("9") + "/runs": `"y"`}}
	obs := s.observe(t)
	if _, ok := obs.Cond.Details["pulls/9"]; ok || len(obs.Cond.Tips) != 1 || obs.Cond.Details["pulls/1"] == "" {
		t.Fatalf("%+v", obs.Cond)
	}
	for k := range obs.Cond.Details {
		if strings.Contains(k, sha40("9")) {
			t.Fatal(k)
		}
	}
}

func TestObserveRefusesInvalidValuesOfAGonePR(t *testing.T) {
	good := `{"state":"closed","merged":true,"merge_commit_sha":"` + sha40("d") + `","merged_at":"2026-10-22T08:00:00Z","base":{"ref":"main"},"head":{"sha":"` + sha40("6") + `","ref":"feat/6"}}`
	for name, body := range map[string]string{
		"merge commit with an escape": strings.Replace(good, sha40("d"), "\u001b[31m"+sha40("d")[:30], 1),
		"uppercase merge commit":      strings.Replace(good, sha40("d"), strings.ToUpper(sha40("d")), 1),
		"short head":                  strings.Replace(good, sha40("6"), "abc", 1),
		"merged_at garbage":           strings.Replace(good, "2026-10-22T08:00:00Z", "yesterday", 1),
		"merged_at missing":           strings.Replace(good, `"merged_at":"2026-10-22T08:00:00Z",`, "", 1),
		"closed with a bad head":      `{"state":"closed","merged":false,"base":{"ref":"main"},"head":{"sha":"x","ref":"f"}}`,
	} {
		f := newFake()
		f.tips["main"] = sha40("a")
		f.details[6] = body
		o := newObserver(f)
		_, err := o.Observe(context.Background(), "o/r", watch.Hints{Cycle: 1, DetailEvery: 10, Known: []watch.KnownPR{{Number: 6}}})
		var oe *watch.ObserveError
		if !errors.As(err, &oe) || oe.Code != hosting.CodeMalformed {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestObserveKeepsWatchingWhenABaseNameIsRefused(t *testing.T) {
	f := newFake()
	f.items = []hosting.PullItem{item(1, "main"), item(2, "we%2Fird")}
	f.tips["main"] = sha40("a")
	f.details[1], f.details[2] = detailJSON, detailJSON
	o := newObserver(f)
	var diags []string
	o.Diag = func(s string) { diags = append(diags, s) }
	for i := 0; i < 2; i++ {
		obs, err := o.Observe(context.Background(), "o/r", watch.Hints{Cycle: i + 1, DetailEvery: 10})
		if err != nil || len(obs.PRs) != 2 || obs.PRs[1].Detail == nil {
			t.Fatalf("%v %+v", err, obs)
		}
	}
	if f.count("branch/we") != 0 || f.count("branch/main") != 2 || len(diags) != 1 || !strings.Contains(diags[0], `"we%2Fird"`) {
		t.Fatalf("%v %v", f.calls, diags)
	}
}

func TestObserveReportsTheForkFlagOnlyWhenPullDetailWasRead(t *testing.T) {
	f := newFake()
	f.items = []hosting.PullItem{item(1, "main")}
	f.tips["main"] = sha40("a")
	f.details[1] = `{"mergeable":true,"mergeable_state":"clean","head":{"repo":{"full_name":"x/fork"}},"base":{"repo":{"full_name":"o/r"}}}`
	f.detailET[1] = `"d1"`
	s := &session{o: newObserver(f), f: f}
	obs := s.observe(t)
	if !obs.PRs[0].Fork || !obs.PRs[0].ForkKnown {
		t.Fatalf("%+v", obs.PRs[0])
	}
	s.accept(obs)
	s.cyc = 1
	obs = s.observe(t) // nothing read
	if obs.PRs[0].ForkKnown || obs.PRs[0].Fork {
		t.Fatalf("an unread fork must not be reported as an answer: %+v", obs.PRs[0])
	}
	s.cyc = 8 // the next cycle is 9: (9+1) % 10 == 0 is the slow refresh, whose 304 reads no body
	obs = s.observe(t)
	if f.count("detail/1") != 1 || f.etags["detail/1"] != `"d1"` || obs.PRs[0].ForkKnown || obs.PRs[0].Fork {
		t.Fatalf("304 on the slow refresh: %v %v %+v", f.calls, f.etags, obs.PRs[0])
	}
}

func TestObserveGuessesTheDefaultBranch(t *testing.T) {
	for name, tc := range map[string]struct {
		bases []string
		want  string
	}{
		"main":               {[]string{"main"}, "main"},
		"master":             {[]string{"master"}, "master"},
		"main over master":   {[]string{"master", "main"}, "main"},
		"stacked and main":   {[]string{"feat/1", "main"}, "main"},
		"only stacked":       {[]string{"feat/1"}, ""},
		"no open PR":         {nil, ""},
		"develop is unknown": {[]string{"develop"}, ""},
	} {
		f := newFake()
		for i, b := range tc.bases {
			f.items = append(f.items, item(i+1, b))
			f.tips[b] = sha40("a")
			f.details[i+1] = detailJSON
		}
		obs, err := newObserver(f).Observe(context.Background(), "o/r", watch.Hints{Cycle: 1, DetailEvery: 10})
		if err != nil || obs.DefaultBranch != tc.want {
			t.Errorf("%s: %q %v", name, obs.DefaultBranch, err)
		}
	}
}
