// SPDX-License-Identifier: EUPL-1.2

package land

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

func sha(c byte) string { return strings.Repeat(string(c), 40) }

const (
	ownerName  = "Werner Stein"
	ownerEmail = "werner.stein@gmail.com"
)

// scenario is a complete fake world: a forge, a Git and the chain 11, 12, 13 on main.
type scenario struct {
	forge  *fakeForge
	git    *fakeGit
	opts   Options
	mode   Mode
	policy Policy
}

var (
	humanMode = Mode{Mode: "human", StdinTTY: true, StdoutTTY: true, Markers: []string{}}
	agentMode = Mode{Mode: "agent", Markers: []string{"CLAUDECODE"}}
)

func newScenario() *scenario {
	m, h1, h2, h3 := sha('0'), sha('1'), sha('2'), sha('3')
	f := &fakeForge{
		repo: RepoFacts{DefaultBranch: "main", DeleteBranchOnMerge: boolp(true)},
		pulls: map[int]PullFacts{
			11: pull(11, h1, "feat-1"), 12: pull(12, h2, "feat-2"), 13: pull(13, h3, "feat-3"),
		},
		checks: map[string]CheckFacts{},
		reviews: map[int][]Review{
			11: {{Reviewer: "rev", State: "APPROVED", CommitID: h1}},
			12: {{Reviewer: "rev", State: "APPROVED", CommitID: h2}},
			13: {{Reviewer: "rev", State: "APPROVED", CommitID: h3}},
		},
		events:   map[int][]Event{},
		required: RequiredFacts{Readable: true, Contexts: []string{"ci/all"}},
		rulesets: RulesetFacts{Readable: true, RuleTypes: []string{"pull_request", "required_status_checks"}, BypassModes: []string{"always"}, Protection: "none"},
		viewer:   "wstein",
	}
	f.checks[h1] = CheckFacts{Contexts: map[string]CheckState{"light": CheckSuccess}}
	f.checks[h2] = CheckFacts{Contexts: map[string]CheckState{"light": CheckSuccess}}
	f.checks[h3] = CheckFacts{Contexts: map[string]CheckState{"light": CheckSuccess, "ci/all": CheckSuccess}}
	g := &fakeGit{
		remoteRepo: "o/r", tip: m, lin: []string{m, h1, h2, h3},
		refs: map[int]string{11: h1, 12: h2, 13: h3},
	}
	g.ident = map[string][4]string{}
	return &scenario{
		forge: f, git: g, mode: humanMode,
		policy: Policy{Loaded: true, OwnerName: ownerName, OwnerEmail: ownerEmail},
		opts: Options{
			Repo: "o/r", Remote: "origin", RangeBound: DefaultRangeBound,
			Chain: []Item{{11, h1}, {12, h2}, {13, h3}},
		},
	}
}

func pull(n int, head, ref string) PullFacts {
	return PullFacts{
		Number: n, State: "open", HeadSHA: head, HeadRef: ref, HeadRepo: "o/r", BaseRef: "main", BaseRepo: "o/r",
		Mergeable: boolp(true), MergeableState: "blocked", Rebaseable: boolp(true),
	}
}

func boolp(b bool) *bool { return &b }

func (s *scenario) plan() Report {
	return Plan(context.Background(), s.opts, s.mode, s.policy, s.forge, s.git)
}

// has reports whether the report carries a failing reason with that code (and number, when not 0).
func has(r Report, code string, number int) bool {
	for _, x := range r.Reasons {
		if x.Code == code && (number == 0 || x.Number == number) {
			return true
		}
	}
	return false
}

func reasonCodes(r Report) []string {
	var out []string
	for _, x := range r.Reasons {
		out = append(out, fmt.Sprintf("%s#%d", x.Code, x.Number))
	}
	return out
}

type fakeForge struct {
	mu        sync.Mutex
	repo      RepoFacts
	repoErr   error
	pulls     map[int]PullFacts
	pullErr   map[int]error
	open      []OpenPull
	openTrunc bool
	checks    map[string]CheckFacts
	checkErr  map[string]error
	reviews   map[int][]Review
	revErr    map[int]error
	events    map[int][]Event
	required  RequiredFacts
	rulesets  RulesetFacts
	viewer    string
	calls     []string
}

func (f *fakeForge) rec(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }

func (f *fakeForge) Repository(context.Context) (RepoFacts, error) {
	f.rec("repo")
	return f.repo, f.repoErr
}
func (f *fakeForge) Pull(_ context.Context, n int) (PullFacts, error) {
	f.rec(fmt.Sprint("pull ", n))
	if e := f.pullErr[n]; e != nil {
		return PullFacts{}, e
	}
	p, ok := f.pulls[n]
	if !ok {
		return PullFacts{}, errors.New("no such pull request")
	}
	return p, nil
}
func (f *fakeForge) OpenPulls(context.Context) ([]OpenPull, bool, error) {
	f.rec("open")
	return f.open, f.openTrunc, nil
}
func (f *fakeForge) Checks(_ context.Context, sha string) (CheckFacts, error) {
	f.rec("checks " + sha[:1])
	if e := f.checkErr[sha]; e != nil {
		return CheckFacts{}, e
	}
	return f.checks[sha], nil
}
func (f *fakeForge) Reviews(_ context.Context, n int) ([]Review, error) {
	f.rec(fmt.Sprint("reviews ", n))
	return f.reviews[n], f.revErr[n]
}
func (f *fakeForge) Events(_ context.Context, n int) ([]Event, error) {
	f.rec(fmt.Sprint("events ", n))
	return f.events[n], nil
}
func (f *fakeForge) Required(context.Context, string) (RequiredFacts, error) {
	f.rec("required")
	return f.required, nil
}
func (f *fakeForge) Rulesets(context.Context, string) (RulesetFacts, error) {
	f.rec("rulesets")
	return f.rulesets, nil
}
func (f *fakeForge) Viewer(context.Context) (string, error) { f.rec("viewer"); return f.viewer, nil }

type fakeGit struct {
	remoteRepo string
	tip        string
	tipErr     error
	lin        []string // linear history, oldest first; element 0 is the default branch tip
	refs       map[int]string
	ident      map[string][4]string // sha -> author name, email, committer name, email overrides
	fetched    int
	calls      []string
	extra      int                // extra commits appended to the range (range bound tests)
	diverged   map[[2]string]bool // pairs (older, newer) that are not ancestors although both are known
	rangeHook  func(from, to string, out []Commit) []Commit
}

func (g *fakeGit) rec(s string) { g.calls = append(g.calls, s) }

func (g *fakeGit) RemoteRepo(context.Context, string) (string, error) {
	g.rec("remote-repo")
	return g.remoteRepo, nil
}
func (g *fakeGit) DefaultTip(context.Context, string, string) (string, error) {
	g.rec("ls-remote")
	return g.tip, g.tipErr
}
func (g *fakeGit) Fetch(context.Context, string, string, []int) error {
	g.rec("fetch")
	g.fetched++
	return nil
}
func (g *fakeGit) Resolve(_ context.Context, n int) (string, error) {
	g.rec(fmt.Sprint("resolve ", n))
	return g.refs[n], nil
}
func (g *fakeGit) idx(s string) int {
	for i, x := range g.lin {
		if x == s {
			return i
		}
	}
	return -1
}
func (g *fakeGit) IsAncestor(_ context.Context, a, b string) (Ancestry, error) {
	g.rec("is-ancestor")
	ia, ib := g.idx(a), g.idx(b)
	if g.diverged[[2]string{a, b}] {
		return AncestorNo, nil
	}
	switch {
	case ia < 0 || ib < 0:
		return AncestorUnknown, nil
	case ia <= ib:
		return AncestorYes, nil
	}
	return AncestorNo, nil
}
func (g *fakeGit) Range(_ context.Context, from, to string, limit int) ([]Commit, error) {
	g.rec("range")
	i, j := g.idx(from), g.idx(to)
	if i < 0 || j < 0 || i > j {
		return nil, errors.New("bad range")
	}
	var out []Commit
	for _, s := range g.lin[i+1 : j+1] {
		c := Commit{SHA: s, AuthorName: ownerName, AuthorEmail: ownerEmail, CommitterName: ownerName, CommitterEmail: ownerEmail}
		if o, ok := g.ident[s]; ok {
			c.AuthorName, c.AuthorEmail, c.CommitterName, c.CommitterEmail = o[0], o[1], o[2], o[3]
		}
		out = append(out, c)
	}
	for k := 0; k < g.extra; k++ {
		out = append(out, Commit{SHA: fmt.Sprintf("%040d", k), AuthorName: ownerName, AuthorEmail: ownerEmail, CommitterName: ownerName, CommitterEmail: ownerEmail})
	}
	if g.rangeHook != nil {
		out = g.rangeHook(from, to, out)
	}
	if len(out) > limit+1 {
		out = out[:limit+1]
	}
	return out, nil
}
