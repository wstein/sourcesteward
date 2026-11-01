// SPDX-License-Identifier: EUPL-1.2
package pulls

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/conflicts"
	"github.com/wstein/sourcesteward/internal/reviewstate"
)

// fakeProber answers by pull request number and records what it was asked.
type fakeProber struct {
	mu       sync.Mutex
	findings map[int]conflicts.Finding
	asked    []conflicts.PR
	repos    []string
}

func (p *fakeProber) Probe(_ context.Context, repo string, prs []conflicts.PR) []conflicts.Finding {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.repos = append(p.repos, repo)
	var out []conflicts.Finding
	for _, pr := range prs {
		p.asked = append(p.asked, pr)
		if f, ok := p.findings[pr.Number]; ok {
			f.Number = pr.Number
			out = append(out, f)
		}
	}
	return out
}

func conflicting(files ...string) conflicts.Finding {
	f := conflicts.Finding{State: conflicts.StateConflicts, Base: "main", BaseSHA: sha("f"), MergeBase: sha("e")}
	for _, p := range files {
		f.Files = append(f.Files, conflicts.FileConflict{Path: p, BaseCommit: sha("b")})
	}
	return f
}

func clean() conflicts.Finding { return conflicts.Finding{State: conflicts.StateClean, Base: "main"} }

// clearPR adds a ready, CLEAR pull request.
func clearPR(f *fakeForge, n int, base, headRef string) {
	p := okPR(repoA, n)
	p.base, p.headRef = base, headRef
	f.add(p)
	f.commits[p.head] = t0
	f.say(repoA, n, 1, 5, reviewLine("CLEAR", p.head, n))
}

func runConflicts(t *testing.T, f *fakeForge, pr *fakeProber, o Options) *Result {
	t.Helper()
	o.Conflicts, o.Remote = pr, "origin"
	return runReview(t, f, o)
}

func TestConflictDemotesReadyAndClear(t *testing.T) {
	f := newForge()
	clearPR(f, 1, "main", "one")
	clearPR(f, 2, "main", "two")
	pr := &fakeProber{findings: map[int]conflicts.Finding{1: conflicting("rules.md"), 2: clean()}}
	res := runConflicts(t, f, pr, Options{})
	c := row(t, res, repoA, 1)
	if c.Group != GroupConflict || c.Ready || c.Reason != "" {
		t.Fatalf("a conflicting pull request must not be ready: %+v", c)
	}
	if c.Review == nil || c.Review.State != reviewstate.StateClear {
		t.Fatalf("the review is still shown as it is: %+v", c.Review)
	}
	for _, h := range c.Hints {
		if h == reviewstate.HintReadyMergeable {
			t.Fatalf("hints %v", c.Hints)
		}
	}
	if c.BaseConflict == nil || c.BaseConflict.State != conflicts.StateConflicts || len(c.BaseConflict.Files) != 1 || c.BaseConflict.Files[0].Path != "rules.md" {
		t.Fatalf("%+v", c.BaseConflict)
	}
	ok := row(t, res, repoA, 2)
	if ok.Group != GroupReady || !ok.Ready || ok.BaseConflict == nil || ok.BaseConflict.State != conflicts.StateClean {
		t.Fatalf("%+v", ok)
	}
	if got := strings.Join(ok.Hints, ","); got != reviewstate.HintReadyMergeable {
		t.Fatalf("hints %q", got)
	}
	s := res.Summary
	if s.Ready != 1 || s.Conflict != 1 || s.BaseConflicts != 1 {
		t.Fatalf("summary %+v", s)
	}
	if !res.ConflictsOn {
		t.Fatal("ConflictsOn")
	}
}

func TestChildOfConflictIsBlockedByIt(t *testing.T) {
	f := newForge()
	clearPR(f, 1, "main", "one")
	clearPR(f, 2, "one", "two")
	clearPR(f, 3, "two", "three")
	clearPR(f, 4, "main", "four")
	pr := &fakeProber{findings: map[int]conflicts.Finding{1: conflicting("a"), 2: clean(), 3: clean(), 4: clean()}}
	res := runConflicts(t, f, pr, Options{})
	for _, n := range []int{2, 3} {
		r := row(t, res, repoA, n)
		if r.Group != GroupBlocked || r.Reason != ReasonParentConflict || r.Ready || r.BaseConflict.BlockedBy != 1 {
			t.Errorf("#%d: %+v / %+v", n, r, r.BaseConflict)
		}
		for _, h := range r.Hints {
			if h == reviewstate.HintReadyMergeable {
				t.Errorf("#%d keeps ready-mergeable", n)
			}
		}
	}
	if r := row(t, res, repoA, 4); !r.Ready || r.BaseConflict.BlockedBy != 0 {
		t.Fatalf("an unrelated pull request is untouched: %+v", r)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Repo != repoA || len(res.Conflicts[0].Stacks) != 1 || len(res.Conflicts[0].Stacks[0].Levels) != 3 {
		t.Fatalf("stacks: %+v", res.Conflicts)
	}
	if len(res.Conflicts[0].Plans) != 0 {
		t.Fatal("no plan without RestackPlan")
	}
}

func TestDraftAndUnknownKeepTheirGroup(t *testing.T) {
	f := newForge()
	d := okPR(repoA, 1)
	d.draft = true
	f.add(d)
	clearPR(f, 2, "main", "two")
	pr := &fakeProber{findings: map[int]conflicts.Finding{1: conflicting("a"), 2: {State: conflicts.StateUnknown, Reason: conflicts.ReasonFetchFailed}}}
	res := runConflicts(t, f, pr, Options{})
	if r := row(t, res, repoA, 1); r.Group != GroupDraft || r.BaseConflict.State != conflicts.StateConflicts {
		t.Fatalf("a draft stays a draft and shows the conflict: %+v", r)
	}
	if r := row(t, res, repoA, 2); r.Group != GroupReady || r.BaseConflict.State != conflicts.StateUnknown || r.BaseConflict.Reason != conflicts.ReasonFetchFailed {
		t.Fatalf("unknown is shown as unknown: %+v", r)
	}
}

func TestMissingFindingIsUnknown(t *testing.T) {
	f := newForge()
	clearPR(f, 1, "main", "one")
	res := runConflicts(t, f, &fakeProber{findings: map[int]conflicts.Finding{}}, Options{})
	if r := row(t, res, repoA, 1); r.BaseConflict == nil || r.BaseConflict.State != conflicts.StateUnknown || r.BaseConflict.Reason != conflicts.ReasonProbeFailed {
		t.Fatalf("%+v", r.BaseConflict)
	}
}

func TestProberAskedWithTheListedPullRequests(t *testing.T) {
	f := newForge()
	clearPR(f, 1, "main", "one")
	bad := okPR(repoA, 2)
	bad.head = "short"
	f.add(bad)
	pr := &fakeProber{findings: map[int]conflicts.Finding{1: clean()}}
	res := runConflicts(t, f, pr, Options{})
	if len(pr.asked) != 1 || pr.asked[0].Number != 1 || pr.asked[0].Base != "main" || pr.asked[0].HeadRef != "one" || !conflicts.ValidSHA(pr.asked[0].Head) {
		t.Fatalf("asked %+v", pr.asked)
	}
	if r := row(t, res, repoA, 2); r.BaseConflict == nil || r.BaseConflict.Reason != conflicts.ReasonRefInvalid {
		t.Fatalf("a head that is no SHA is never probed: %+v", r.BaseConflict)
	}
}

func TestNoProberNoChange(t *testing.T) {
	f := newForge()
	clearPR(f, 1, "main", "one")
	res := runReview(t, f, Options{})
	if r := row(t, res, repoA, 1); r.BaseConflict != nil || !r.Ready {
		t.Fatalf("%+v", r)
	}
	if res.ConflictsOn || len(res.Conflicts) != 0 || res.Summary.BaseConflicts != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestRestackPlanOnlyOnRequest(t *testing.T) {
	f := newForge()
	clearPR(f, 1, "main", "one")
	clearPR(f, 2, "one", "two")
	pr := &fakeProber{findings: map[int]conflicts.Finding{1: conflicting("a"), 2: clean()}}
	res := runConflicts(t, f, pr, Options{RestackPlan: true})
	if len(res.Conflicts) != 1 || len(res.Conflicts[0].Plans) != 1 || len(res.Conflicts[0].Plans[0].Steps) != 2 || res.Conflicts[0].Plans[0].Remote != "origin" {
		t.Fatalf("%+v", res.Conflicts)
	}
}

func TestFilteredRowsStillFormTheStacks(t *testing.T) {
	f := newForge()
	clearPR(f, 1, "main", "one")
	clearPR(f, 2, "one", "two")
	pr := &fakeProber{findings: map[int]conflicts.Finding{1: conflicting("a"), 2: clean()}}
	res := runConflicts(t, f, pr, Options{Hints: []string{reviewstate.HintReadyMergeable}})
	if len(res.Rows) != 0 {
		t.Fatalf("both rows are blocked, so the filter drops them: %+v", res.Rows)
	}
	if len(res.Conflicts) != 1 || len(res.Conflicts[0].Stacks) != 1 {
		t.Fatalf("the stack view is built before the filter: %+v", res.Conflicts)
	}
}

func TestForkAndReviewReachTheStackView(t *testing.T) {
	f := newForge()
	clearPR(f, 1, "main", "one")
	clearPR(f, 2, "one", "two")
	f.details[repoA+"#2"] = strings.Replace(f.details[repoA+"#2"], `"repo":{"full_name":"o/a"}},"base"`, `"repo":{"full_name":"x/fork"}},"base"`, 1)
	pr := &fakeProber{findings: map[int]conflicts.Finding{1: conflicting("a"), 2: clean()}}
	res := runConflicts(t, f, pr, Options{})
	var forkAsked bool
	for _, a := range pr.asked {
		if a.Number == 2 {
			forkAsked = a.Fork
		}
	}
	if !forkAsked || !row(t, res, repoA, 2).Fork {
		t.Fatalf("the fork flag must reach the prober: %+v", pr.asked)
	}
	if r := row(t, res, repoA, 2); r.BaseConflict.BlockedBy != 0 {
		t.Fatalf("a fork pull request is not a child in the stack: %+v", r.BaseConflict)
	}
	lv := res.Conflicts[0].Stacks[0].Levels[0]
	if lv.Number != 1 || lv.Clear != "valid" || lv.CI != "green" {
		t.Fatalf("the review and the checks reach the level: %+v", lv)
	}
}

func TestNoProbeAfterTheContextExpired(t *testing.T) {
	f := newForge()
	for _, repo := range []string{"o/a", "o/b"} {
		p := okPR(repo, 1)
		f.add(p)
	}
	ctx, cancel := context.WithCancel(context.Background())
	pr := &cancelingProber{fakeProber: fakeProber{findings: map[int]conflicts.Finding{}}, cancel: cancel}
	res, err := Run(ctx, f, Options{Repos: []string{"o/a", "o/b"}, Conflicts: pr, Now: func() time.Time { return t0 }, Parallel: 1})
	if err == nil || res == nil {
		t.Fatalf("%v %v", res, err)
	}
	if len(pr.repos) != 1 {
		t.Fatalf("the second repository was probed after the context expired: %v", pr.repos)
	}
}

type cancelingProber struct {
	fakeProber
	cancel func()
}

func (p *cancelingProber) Probe(ctx context.Context, repo string, prs []conflicts.PR) []conflicts.Finding {
	out := p.fakeProber.Probe(ctx, repo, prs)
	p.cancel()
	return out
}

func TestRestackPlanNeedsTheProbe(t *testing.T) {
	f := newForge()
	clearPR(f, 1, "main", "one")
	if _, err := Run(context.Background(), f, Options{Repos: []string{repoA}, RestackPlan: true}); err == nil {
		t.Fatal("a plan without a probe was accepted")
	}
}
