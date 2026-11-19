// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watch"
	"github.com/wstein/sourcesteward/internal/watchexport"
)

var obsClock = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

// oStep is one Observe call of an observer session.
type oStep struct {
	Note        string `json:"note"`
	World       wWorld `json:"world"`
	NewProcess  bool   `json:"new_process"`
	Hints       oHints `json:"hints"`
	AfterCycle  bool   `json:"after_cycle_first"` // the previous cycle ended: the rate limit is read and may arm a hold
	CtxCanceled bool   `json:"context_canceled"`
}

type oStepOut struct {
	Step        int      `json:"step"`
	Observation *oObs    `json:"observation"`
	Error       *oError  `json:"error"`
	Calls       []oCall  `json:"calls"`
	Diagnostics []string `json:"diagnostics"`
	Slow        *bool    `json:"slow_after_cycle"`
}

type oConfig struct {
	MaxOpenPRs    int      `json:"max_open_prs"`
	IgnoreAuthors []string `json:"ignore_authors"`
	IgnoreLabels  []string `json:"ignore_labels"`
	Clock         string   `json:"clock"`
}

type oDoc struct {
	watchexport.Common
	Config   oConfig    `json:"config"`
	Steps    []oStep    `json:"steps"`
	Expected []oStepOut `json:"expected"`
}

// swapReader forwards to the fake of the current step.
type swapReader struct{ cur *fakeReader }

func (s *swapReader) ListOpenPulls(ctx context.Context, repo string, prior hosting.PullListState, max int) (hosting.PullListResult, error) {
	return s.cur.ListOpenPulls(ctx, repo, prior, max)
}
func (s *swapReader) PullDetail(ctx context.Context, repo string, n int, etag string) (hosting.WatchResponse, error) {
	return s.cur.PullDetail(ctx, repo, n, etag)
}
func (s *swapReader) Branch(ctx context.Context, repo, name, etag string) (hosting.WatchResponse, error) {
	return s.cur.Branch(ctx, repo, name, etag)
}
func (s *swapReader) Commit(ctx context.Context, repo, sha, etag string) (hosting.WatchResponse, error) {
	return s.cur.Commit(ctx, repo, sha, etag)
}
func (s *swapReader) PullCommits(ctx context.Context, repo string, n, page int, etag string) (hosting.WatchResponse, error) {
	return s.cur.PullCommits(ctx, repo, n, page, etag)
}
func (s *swapReader) CheckRuns(ctx context.Context, repo, sha string, page int, etag string) (hosting.WatchResponse, error) {
	return s.cur.CheckRuns(ctx, repo, sha, page, etag)
}
func (s *swapReader) Statuses(ctx context.Context, repo, sha string, page int, etag string) (hosting.WatchResponse, error) {
	return s.cur.Statuses(ctx, repo, sha, page, etag)
}
func (s *swapReader) RateLimit(ctx context.Context) (hosting.RateInfo, error) {
	return s.cur.RateLimit(ctx)
}

// oSession drives a HostObserver live while the scenario is written; the recorded steps carry the full world.
type oSession struct {
	t     *testing.T
	doc   *oDoc
	f     *fakeReader // the fake the author edits between steps (a Go-test style fake)
	rd    *swapReader
	o     *HostObserver
	cond  *watch.Conditional
	cyc   int
	know  []oKnown
	last  watch.RepoObservation
	diags []string
}

func newOSession(t *testing.T, name, desc string, tests ...string) *oSession {
	s := &oSession{t: t, f: newFake(), rd: &swapReader{}}
	s.doc = &oDoc{Common: watchexport.NewCommon(name, watchexport.KindObserver, desc, tests...), Steps: []oStep{}, Expected: []oStepOut{},
		Config: oConfig{MaxOpenPRs: 2000, IgnoreAuthors: []string{}, IgnoreLabels: []string{}, Clock: obsClock.Format(time.RFC3339)}}
	return s
}

func (s *oSession) ignore(authors, labels []string) *oSession {
	s.doc.Config.IgnoreAuthors, s.doc.Config.IgnoreLabels = authors, labels
	return s
}

func (s *oSession) newObserver() {
	s.o = NewHostObserver(s.rd, func() time.Time { return obsClock }, s.doc.Config.MaxOpenPRs, s.doc.Config.IgnoreAuthors, s.doc.Config.IgnoreLabels)
	s.o.Diag = func(l string) { s.diags = append(s.diags, l) }
}

// observe runs one Observe against a fake rebuilt from the recorded world. cyc counts up as in the Go session.
func (s *oSession) observe(note string, mod ...func(*oStep)) *oSession {
	s.cyc++
	st := oStep{Note: note, World: worldOf(s.f, obsClock), Hints: oHints{Cycle: s.cyc, DetailEvery: 10, Known: append([]oKnown{}, s.know...)}}
	for _, m := range mod {
		m(&st)
	}
	s.cyc = st.Hints.Cycle
	return s.run(st)
}

func (s *oSession) run(st oStep) *oSession {
	if s.o == nil || st.NewProcess {
		s.newObserver()
	}
	fake := fakeOf(st.World, obsClock)
	s.rd.cur = fake
	s.diags = nil
	out := oStepOut{Step: len(s.doc.Steps) + 1}
	ctx := context.Background()
	if st.CtxCanceled {
		c, cancel := context.WithCancel(ctx)
		cancel()
		ctx = c
	}
	if st.AfterCycle {
		slow := s.o.AfterCycle(ctx)
		out.Slow = &slow
	}
	h := st.Hints.hints()
	h.Cond = s.cond
	obs, err := s.o.Observe(ctx, "o/r", h)
	if err == nil {
		if obs.Cond != nil {
			s.cond = obs.Cond
		}
		s.last = obs
		v := oObsOf(obs)
		out.Observation = &v
	}
	out.Error = oErrOf(err)
	out.Calls = callsOf(fake)
	out.Diagnostics = nonNilS(s.diags)
	s.doc.Steps = append(s.doc.Steps, st)
	s.doc.Expected = append(s.doc.Expected, out)
	return s
}

// accept mirrors session.accept of the Go tests: the accepted view of the core is the last observed PRs with their class.
func (s *oSession) accept() *oSession {
	s.know = nil
	for _, p := range s.last.PRs {
		class := ""
		if p.Checks != nil {
			class = watch.ClassifyChecks(p.Checks.Runs, p.Checks.Statuses, "", "").Class
		}
		s.know = append(s.know, oKnown{Number: p.Number, Head: p.Head, Class: class})
	}
	return s
}

func (s *oSession) write(dir string) {
	if err := watchexport.Write(dir, s.doc.Name, s.doc); err != nil {
		s.t.Fatal(err)
	}
}

// ---- guards of the translation ----

func (s *oSession) calls(step int, prefix string) int {
	n := 0
	for _, c := range s.doc.Expected[step-1].Calls {
		if strings.HasPrefix(c.Call, prefix) {
			n++
		}
	}
	return n
}

func (s *oSession) must(cond bool, msg string) {
	s.t.Helper()
	if !cond {
		s.t.Fatalf("%s: translation guard: %s", s.doc.Name, msg)
	}
}

const mergedBody = `{"state":"closed","merged":true,"merge_commit_sha":"%s","merged_at":"2026-10-22T08:00:00Z","base":{"ref":"main"},"head":{"sha":"%s","ref":"%s"}}`

func exportObserverScenarios(t *testing.T, dir string) {
	runs1 := `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{}}]}`

	// baseline
	{
		s := newOSession(t, "observe-baseline-reads-every-pr-fully", "The first observation of a repository reads every PR fully: list, pulls/N, branch tips, check runs and commit statuses; a null mergeable is `not computed`; the fork flag comes from pulls/N; check data keeps title, summary and text; a status context is taken once (the newest).",
			"watchcmd/observer_test.go:TestObserveBaselineReadsEveryPRFully")
		f := s.f
		f.items = []hosting.PullItem{item(1, "main"), item(2, "feat/1")}
		f.tips["main"], f.tips["feat/1"] = sha40("a"), sha40("b")
		f.details[1], f.details[2] = detailJSON, `{"mergeable":null,"mergeable_state":"unknown","head":{"repo":null},"base":{"repo":{"full_name":"o/r"}}}`
		f.detailET[1], f.detailET[2] = `"d1"`, `"d2"`
		f.runs[item(1, "").HeadSHA] = `{"total_count":2,"check_runs":[{"name":"ci","status":"completed","conclusion":"failure","output":{"title":"t","summary":"--- FAIL: TestX","text":""}},{"name":"lint","status":"in_progress","conclusion":null,"output":{}}]}`
		f.stats[item(1, "").HeadSHA] = `[{"context":"legacy","state":"success","description":"new"},{"context":"legacy","state":"failure","description":"old"}]`
		s.observe("baseline")
		o := s.doc.Expected[0].Observation
		s.must(o != nil && len(o.PRs) == 2 && o.DefaultBranch == "main" && o.PRs[1].Fork && !o.PRs[0].Fork && o.PRs[0].Checks != nil && len(o.PRs[0].Checks.Runs) == 2, "baseline content")
		s.write(dir)
	}
	// steady state
	{
		s := newOSession(t, "observe-steady-state-reads-only-the-basics", "In the steady state (all PRs accepted green, no due flag, no moved base) a cycle reads the list and one conditional branch tip per base; no detail, no checks, nothing reported for PRs.",
			"watchcmd/observer_test.go:TestObserveSteadyStateReadsNothingBeyondTheConditionalBasics")
		f := s.f
		f.items = []hosting.PullItem{item(1, "main"), item(2, "main")}
		f.tips["main"] = sha40("a")
		f.details[1], f.details[2] = detailJSON, detailJSON
		for _, it := range f.items {
			f.runs[it.HeadSHA] = runs1
		}
		s.observe("baseline").accept()
		s.cyc = 1
		s.observe("steady state, cycle 2")
		s.must(s.calls(2, "detail") == 0 && s.calls(2, "runs") == 0 && s.calls(2, "stats") == 0 && s.calls(2, "branch/main") == 1, "steady state")
		s.write(dir)
	}
	// detail when needed
	setup := func(name, desc, test string) *oSession {
		s := newOSession(t, name, desc, test)
		f := s.f
		f.items = []hosting.PullItem{item(1, "main"), item(2, "main"), item(3, "feat/1")}
		f.tips["main"], f.tips["feat/1"] = sha40("a"), sha40("b")
		for n := 1; n <= 3; n++ {
			f.details[n] = detailJSON
			f.detailET[n] = fmt.Sprintf(`"d%d"`, n)
		}
		for _, it := range f.items {
			f.runs[it.HeadSHA] = runs1
		}
		s.observe("baseline").accept()
		s.cyc = 1
		return s
	}
	{
		s := setup("observe-detail-when-head-changed", "A PR whose head changed gets a fresh pulls/N read and its checks read; the other PRs are left alone.", "watchcmd/observer_test.go:TestObserveReadsDetailWhenItIsNeeded/head_changed")
		s.f.items[0].HeadSHA = sha40("f")
		s.f.runs[sha40("f")] = s.f.runs[item(2, "").HeadSHA]
		s.observe("head of PR 1 changed")
		s.must(s.calls(2, "detail/1") == 1 && s.calls(2, "detail/2") == 0 && s.calls(2, "runs/ffff") == 1, "head changed")
		s.write(dir)
	}
	{
		s := setup("observe-detail-when-base-tip-moved", "A moved base tip (the only evidence that the base moved) makes the PRs on that base due for a fresh, unconditional pulls/N read; PRs on other bases are not read.", "watchcmd/observer_test.go:TestObserveReadsDetailWhenItIsNeeded/base_tip_moved")
		s.f.tips["main"] = sha40("c")
		s.observe("tip of main moved")
		fresh := false
		for _, c := range s.doc.Expected[1].Calls {
			if c.Call == "detail/1" {
				fresh = !c.Conditional
			}
		}
		s.must(s.calls(2, "detail/1") == 1 && s.calls(2, "detail/2") == 1 && s.calls(2, "detail/3") == 0 && fresh, "base moved")
		s.write(dir)
	}
	{
		s := setup("observe-detail-after-a-merge-in-the-same-cycle", "When a known PR left the open list and pulls/N says merged, every open PR is re-read; the merged PR is reported as gone with merge commit, head, base, merged_at and method hint (one parent: rebase-or-squash).", "watchcmd/observer_test.go:TestObserveReadsDetailWhenItIsNeeded/a_merge_in_the_same_cycle")
		s.know = append(s.know, oKnown{Number: 9, Head: sha40("9"), Class: "green"})
		s.f.details[9] = fmt.Sprintf(mergedBody, sha40("e"), sha40("9"), "feat/9")
		s.f.commits[sha40("e")] = `{"parents":[{"sha":"x"}]}`
		s.observe("PR 9 merged")
		s.must(s.calls(2, "detail/1") == 1 && s.calls(2, "detail/2") == 1 && s.calls(2, "detail/3") == 1 && s.doc.Expected[1].Observation.Gone["9"].Merged, "merge")
		s.write(dir)
	}
	{
		s := setup("observe-detail-due-flag-and-slow-refresh", "A PR marked due gets a fresh read; every detail_every cycles the PR numbered so that (cycle + number) mod 10 = 0 gets a conditional refresh (staggered by number).", "watchcmd/observer_test.go:TestObserveReadsDetailWhenItIsNeeded/due_flag_and_the_staggered_slow_refresh")
		s.know[1].DetailDue = true
		s.cyc = 6
		s.observe("cycle 7: PR 2 due, PR 3 slow refresh")
		s.must(s.calls(2, "detail/2") == 1 && s.calls(2, "detail/3") == 1 && s.calls(2, "detail/1") == 0, "due flag")
		s.write(dir)
	}
	// gone
	{
		s := newOSession(t, "observe-gone-closed-merged-and-still-open", "A PR that left the list is read once via pulls/N: closed (no merge), merged with two parents (merge), merged with an unreadable commit (null commit, unknown), still open (stays unresolved); an unreadable commit only makes the hint unknown; an unreadable pulls/N fails the repository.",
			"watchcmd/observer_test.go:TestObserveGoneClosedMergedMethodAndStillOpen")
		f := s.f
		f.tips["main"] = sha40("a")
		f.items = []hosting.PullItem{item(1, "main")}
		f.details[1] = detailJSON
		f.details[5] = `{"state":"closed","merged":false,"base":{"ref":"main"},"head":{"sha":"` + sha40("5") + `","ref":"feat/5"}}`
		f.details[6] = fmt.Sprintf(mergedBody, sha40("d"), sha40("6"), "feat/6")
		f.details[7] = `{"state":"open","merged":false,"base":{"ref":"main"},"head":{"sha":"` + sha40("7") + `","ref":"feat/7"}}`
		f.details[8] = `{"state":"closed","merged":true,"merge_commit_sha":null,"merged_at":"2026-10-22T08:00:00Z","base":{"ref":"main"},"head":{"sha":"` + sha40("8") + `","ref":"feat/8"}}`
		f.commits[sha40("d")] = `{"parents":[{"sha":"1"},{"sha":"2"}]}`
		s.know = []oKnown{{Number: 5}, {Number: 6}, {Number: 7}, {Number: 8}}
		s.observe("gone PRs")
		g := s.doc.Expected[0].Observation.Gone
		s.must(!g["5"].Merged && g["6"].MethodHint == "merge" && g["7"].Head == "" && g["8"].MergeCommit == nil && g["8"].MethodHint == "unknown", "gone")
		f.errAt["commit/dddd"] = &hosting.WatchError{Code: hosting.CodeNetwork, Message: "x"}
		s.observe("the commit read fails")
		s.must(s.doc.Expected[1].Observation.Gone["6"].MethodHint == "unknown", "commit failure")
		f.errAt["detail/6"] = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "boom"}
		s.observe("pulls/6 is unreadable")
		s.must(s.doc.Expected[2].Error != nil, "unreadable")
		s.write(dir)
	}
	// debounce candidate
	{
		s := newOSession(t, "observe-confirms-a-debounce-candidate", "A class that changed upstream is reported; the stored candidate (accepted class still differs) makes the next read fresh, not conditional, so the second sighting reaches the core; once the core accepted the class the PR is settled and not read.",
			"watchcmd/observer_test.go:TestObserveConfirmsADebounceCandidateWithAFreshRead")
		f := s.f
		f.items = []hosting.PullItem{item(1, "main")}
		f.tips["main"] = sha40("a")
		f.details[1] = detailJSON
		h := f.items[0].HeadSHA
		f.runs[h] = `{"total_count":1,"check_runs":[{"name":"ci","status":"in_progress","conclusion":null,"output":{}}]}`
		s.observe("baseline: pending").accept()
		s.cyc = 1
		s.observe("unchanged pending: a conditional read, answered 304")
		s.must(s.calls(2, "runs") == 1 && s.doc.Expected[1].Observation.PRs[0].Checks == nil, "304")
		f.runs[h] = `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{}}]}`
		s.observe("the run completed: the change is reported")
		s.must(s.doc.Expected[2].Observation.PRs[0].Checks != nil, "change reported")
		s.observe("the core has only a candidate: a fresh read")
		s.must(s.doc.Expected[3].Observation.PRs[0].Checks != nil, "fresh read")
		s.know[0].Class = "green"
		s.observe("the core accepted green: not read")
		s.must(s.calls(5, "runs") == 0, "settled")
		s.write(dir)
	}
	{
		s := newOSession(t, "observe-confirms-a-stored-candidate-in-a-new-process", "A candidate stored by the core survives a restart: the memory of the observer is gone, so the known PR itself (class candidate) makes a fresh read due.",
			"watchcmd/observer_test.go:TestObserveConfirmsAStoredCandidateInANewProcess")
		f := s.f
		f.items = []hosting.PullItem{item(1, "main")}
		f.tips["main"] = sha40("a")
		f.details[1] = detailJSON
		h := f.items[0].HeadSHA
		f.runs[h] = `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"failure","output":{}}]}`
		s.observe("first sighting").accept()
		s.know[0].ClassCandidate = "red"
		s.cyc = 1
		s.observe("new process", func(st *oStep) { st.NewProcess = true })
		s.must(s.doc.Expected[1].Observation.PRs[0].Checks != nil, "fresh read")
		s.write(dir)
	}
	{
		s := newOSession(t, "observe-skips-ignored-prs", "Ignored PRs (author or label) stay in the list for the core but cost no detail and no checks read.",
			"watchcmd/observer_test.go:TestObserveSkipsIgnoredPRs").ignore([]string{"dependabot[bot]"}, []string{"no-watch"})
		f := s.f
		bot := item(1, "main")
		bot.User = "dependabot[bot]"
		labelled := item(2, "main")
		labelled.Labels = []string{"no-watch"}
		f.items = []hosting.PullItem{bot, labelled, item(3, "main")}
		f.tips["main"] = sha40("a")
		f.details[3] = detailJSON
		s.observe("list with two ignored PRs")
		s.must(len(s.doc.Expected[0].Observation.PRs) == 3 && s.calls(1, "detail/1")+s.calls(1, "detail/2")+s.calls(1, "runs/1")+s.calls(1, "runs/2") == 0 && s.calls(1, "detail/3") == 1, "ignored")
		s.write(dir)
	}
	{
		s := newOSession(t, "observe-maps-failures-and-the-rate-limit", "Read failures map to watch-error codes; a rate limit below 10 percent asks for a slower interval; below 50 remaining arms a hold that the next Observe reports once as a run-wide rate-limited error (wait until reset + 5 s); a canceled run is not a repository failure.",
			"watchcmd/observer_test.go:TestObserveMapsFailuresAndTheRateLimit")
		f := s.f
		f.listErr = &hosting.WatchError{Code: hosting.CodeTooManyPRs, Message: "more open pull requests than max_open_prs"}
		s.observe("too many open PRs")
		f.listErr = &hosting.WatchError{Code: hosting.CodeRateLimited, Message: "rate limit reached", RetryAfter: 30 * time.Second}
		s.observe("rate limited with retry-after 30 s")
		f.listErr = fmt.Errorf("something else")
		s.observe("an unclassified error is network")
		f.listErr = nil
		f.rate = hosting.RateInfo{Known: true, Limit: 5000, Remaining: 400, Reset: obsClock.Add(time.Hour)}
		s.observe("remaining 400 of 5000: slow, the read still works", func(st *oStep) { st.AfterCycle = true })
		f.rate = hosting.RateInfo{Known: true, Limit: 5000, Remaining: 10, Reset: obsClock.Add(10 * time.Minute)}
		s.observe("remaining 10: the hold is reported", func(st *oStep) { st.AfterCycle = true })
		s.observe("the hold is reported once")
		f.listErr = &hosting.WatchError{Code: hosting.CodeNetwork, Message: "canceled"}
		s.observe("a canceled context is not a repository failure", func(st *oStep) { st.CtxCanceled = true })
		e := s.doc.Expected
		s.must(e[0].Error.Code == "too-many-open-prs" && !e[0].Error.RunWide && e[1].Error.RunWide && e[1].Error.RetryInSeconds == 35 && e[2].Error.Code == "network" &&
			*e[3].Slow && e[3].Error == nil && e[4].Error.RetryInSeconds == 605 && e[5].Error == nil && e[6].Error.Canceled, "failures")
		s.write(dir)
	}
	{
		s := newOSession(t, "observe-refuses-invalid-values-of-a-gone-pr", "pulls/N of a PR that left the list is validated: a merge commit with an escape or in uppercase, a short head, an unparsable or missing merged_at, a closed PR with a bad head are malformed responses.",
			"watchcmd/observer_test.go:TestObserveRefusesInvalidValuesOfAGonePR")
		good := fmt.Sprintf(mergedBody, sha40("d"), sha40("6"), "feat/6")
		bad := map[string]string{
			"merge commit with an escape": strings.Replace(good, sha40("d"), "\u001b[31m"+sha40("d")[:30], 1),
			"uppercase merge commit":      strings.Replace(good, sha40("d"), strings.ToUpper(sha40("d")), 1),
			"short head":                  strings.Replace(good, sha40("6"), "abc", 1),
			"merged_at garbage":           strings.Replace(good, "2026-10-22T08:00:00Z", "yesterday", 1),
			"merged_at missing":           strings.Replace(good, `"merged_at":"2026-10-22T08:00:00Z",`, "", 1),
			"closed with a bad head":      `{"state":"closed","merged":false,"base":{"ref":"main"},"head":{"sha":"x","ref":"f"}}`,
		}
		for _, name := range sortedKeysOf(bad) {
			s.f.tips["main"] = sha40("a")
			s.f.details[6] = bad[name]
			s.know = []oKnown{{Number: 6}}
			s.cyc = 0
			s.observe(name)
			s.must(s.doc.Expected[len(s.doc.Expected)-1].Error != nil && s.doc.Expected[len(s.doc.Expected)-1].Error.Code == hosting.CodeMalformed, name)
		}
		s.write(dir)
	}
	{
		s := newOSession(t, "observe-keeps-watching-when-a-base-name-is-refused", "A base branch name that cannot be encoded for a URL is skipped with one diagnostic per run; the PRs on it are still observed.",
			"watchcmd/observer_test.go:TestObserveKeepsWatchingWhenABaseNameIsRefused")
		f := s.f
		f.items = []hosting.PullItem{item(1, "main"), item(2, "we%2Fird")}
		f.tips["main"] = sha40("a")
		f.details[1], f.details[2] = detailJSON, detailJSON
		s.observe("first cycle")
		s.observe("second cycle")
		s.must(s.calls(1, "branch/we") == 0 && s.calls(1, "branch/main") == 1 && len(s.doc.Expected[0].Diagnostics) == 1 && len(s.doc.Expected[1].Diagnostics) == 0, "refused base")
		s.write(dir)
	}
	{
		s := newOSession(t, "observe-fork-flag-only-when-detail-was-read", "The fork flag is reported as an answer (fork_known) only when pulls/N was read with a body in this observation; an unread or 304-answered detail reports no answer.",
			"watchcmd/observer_test.go:TestObserveReportsTheForkFlagOnlyWhenPullDetailWasRead")
		f := s.f
		f.items = []hosting.PullItem{item(1, "main")}
		f.tips["main"] = sha40("a")
		f.details[1] = `{"mergeable":true,"mergeable_state":"clean","head":{"repo":{"full_name":"x/fork"}},"base":{"repo":{"full_name":"o/r"}}}`
		f.detailET[1] = `"d1"`
		s.observe("baseline: detail read").accept()
		s.cyc = 1
		s.observe("nothing read")
		s.cyc = 8
		s.observe("slow refresh answered 304")
		p := func(i int) oPR { return s.doc.Expected[i].Observation.PRs[0] }
		s.must(p(0).Fork && p(0).ForkKnown && !p(1).ForkKnown && !p(2).ForkKnown && !p(2).Fork && s.calls(3, "detail/1") == 1, "fork")
		s.write(dir)
	}
	// default branch guess
	{
		v := watchexport.NewVectors("observe-default-branch-guess", "observe_default_branch", "The default branch is guessed from the bases of the open PRs (main, then master; stacked bases and unknown names give nothing); the allowlist has no repository read.", nil,
			"watchcmd/observer_test.go:TestObserveGuessesTheDefaultBranch")
		type in struct {
			Bases []string `json:"pr_bases"`
		}
		for _, k := range []struct {
			name  string
			bases []string
		}{{"main", []string{"main"}}, {"master", []string{"master"}}, {"main over master", []string{"master", "main"}}, {"stacked and main", []string{"feat/1", "main"}},
			{"only stacked", []string{"feat/1"}}, {"no open PR", nil}, {"develop is unknown", []string{"develop"}}} {
			f := newFake()
			for i, b := range k.bases {
				f.items = append(f.items, item(i+1, b))
				f.tips[b] = sha40("a")
				f.details[i+1] = detailJSON
			}
			obs, err := newObserver(f).Observe(context.Background(), "o/r", watch.Hints{Cycle: 1, DetailEvery: 10})
			if err != nil {
				t.Fatal(err)
			}
			v.Add(k.name, in{nonNilS(k.bases)}, map[string]string{"default_branch": obs.DefaultBranch})
		}
		if err := watchexport.Write(dir, v.Name, v); err != nil {
			t.Fatal(err)
		}
	}
}
