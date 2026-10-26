// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func oidOf(n int) string { return fmt.Sprintf("%040x", n) }

func onlineCand(repo string, kind Kind, name string, v Verdict, proof Proof, reason string, tip string) Candidate {
	c := Candidate{ID: string(kind) + ":" + repo + ":" + name, Repo: repo, Kind: kind, Verdict: v, Reason: reason, Proof: proof, Effects: []string{}, Tip: tip}
	if kind == KindBranch {
		c.Branch = name
	} else {
		c.Path = "/w/" + name
	}
	return c
}

// A branch row that is safe by an equivalent proof.
func equivalentBranch(repo, name string, n int) Candidate {
	return onlineCand(repo, KindBranch, name, VerdictSafe, ProofPatchID, "", oidOf(n))
}

func unmergedBranch(repo, name string, n int) Candidate {
	return onlineCand(repo, KindBranch, name, VerdictRetain, ProofNone, "unmerged", oidOf(n))
}

func TestSelectTipsOrderCapAndEligibility(t *testing.T) {
	cands := []Candidate{
		unmergedBranch("r", "b-unmerged", 5),
		equivalentBranch("r", "z-equivalent", 3),
		equivalentBranch("r", "a-equivalent", 1),
		onlineCand("r", KindWorktree, "w-patch", VerdictSafe, ProofMergeNoop, "", oidOf(2)),
		// A strong proof is green already: nothing to ask.
		onlineCand("r", KindBranch, "strong", VerdictSafe, ProofAncestor, "", oidOf(9)),
		onlineCand("r", KindBranch, "strong2", VerdictSafe, ProofNoNetChange, "", oidOf(10)),
		// Other retain reasons, decisions, tracking references, stale worktrees and rows without a usable tip are not asked.
		onlineCand("r", KindBranch, "dirty", VerdictRetain, ProofNone, "dirty", oidOf(11)),
		onlineCand("r", KindWorktree, "dec", VerdictDecision, ProofNone, "locked-stw", oidOf(12)),
		onlineCand("r", KindTrackingRef, "refs/remotes/origin/x", VerdictSafe, ProofBranchAbsent, "", oidOf(13)),
		onlineCand("r", KindStaleWorktree, "gone", VerdictRetain, ProofNone, "unmerged", oidOf(14)),
		onlineCand("r", KindBranch, "no-tip", VerdictRetain, ProofNone, "unmerged", ""),
		onlineCand("r", KindBranch, "short-tip", VerdictRetain, ProofNone, "unmerged", "abc123"),
		onlineCand("r", KindBranch, "upper-tip", VerdictRetain, ProofNone, "unmerged", strings.ToUpper(oidOf(255))),
		// The same tip twice is asked once.
		unmergedBranch("r", "dup", 5),
		// Another repository.
		equivalentBranch("other", "x", 7),
	}
	got := selectTips("r", cands, 64)
	if want := []string{oidOf(1), oidOf(3), oidOf(2), oidOf(5)}; !reflect.DeepEqual(got.Tips, want) || got.Eligible != 4 {
		t.Fatalf("tips %v eligible %d, want %v", got.Tips, got.Eligible, want)
	}
	capped := selectTips("r", cands, 2)
	if !reflect.DeepEqual(capped.Tips, []string{oidOf(1), oidOf(3)}) || capped.Eligible != 4 {
		t.Fatalf("capped %v %d", capped.Tips, capped.Eligible)
	}
	// Equivalent rows come first whatever their names, then the unmerged ones.
	many := []Candidate{}
	for i := 1; i <= 100; i++ {
		many = append(many, unmergedBranch("r", fmt.Sprintf("a-u%03d", i), 1000+i))
		many = append(many, equivalentBranch("r", fmt.Sprintf("z-e%03d", i), i))
	}
	big := selectTips("r", many, 64)
	if len(big.Tips) != 64 || big.Eligible != 200 || big.Tips[0] != oidOf(1) || big.Tips[63] != oidOf(64) {
		t.Fatalf("%d tips, eligible %d, first %s last %s", len(big.Tips), big.Eligible, big.Tips[0], big.Tips[len(big.Tips)-1])
	}
}

func TestTipFactOfAnAnswer(t *testing.T) {
	for name, tc := range map[string]struct {
		row  hosting.TidyTipEvidence
		want PREvidence
	}{
		"merged head":           {hosting.TidyTipEvidence{State: hosting.TidyTipMerged, Number: 41}, PREvidence{State: PRMerged, Number: 41}},
		"merged without number": {hosting.TidyTipEvidence{State: hosting.TidyTipMerged}, PREvidence{State: PRUnknown}},
		"closed unmerged":       {hosting.TidyTipEvidence{State: hosting.TidyTipClosedUnmerged, Number: 3}, PREvidence{State: PRClosedUnmerged}},
		"unknown":               {hosting.TidyTipEvidence{State: hosting.TidyTipUnknown}, PREvidence{State: PRUnknown}},
		"unknown word":          {hosting.TidyTipEvidence{State: "something-new"}, PREvidence{State: PRUnknown}},
		"empty":                 {hosting.TidyTipEvidence{}, PREvidence{State: PRUnknown}},
	} {
		if got := tipFact(tc.row); got != tc.want {
			t.Errorf("%s: %+v, want %+v", name, got, tc.want)
		}
	}
}

// The note says what the evidence means for a row; it never changes the row (D8).
func TestPREvidenceNoteAndNoProof(t *testing.T) {
	safe := equivalentBranch("r", "s", 1)
	unmerged := unmergedBranch("r", "u", 2)
	m := PREvidence{State: PRMerged, Number: 12}
	if got := m.Note(safe); got != "merged as #12" {
		t.Errorf("%q", got)
	}
	if got := m.Note(unmerged); got != "merged as #12, but the local proofs disagree: review it" {
		t.Errorf("%q", got)
	}
	for _, f := range []PREvidence{{State: PRClosedUnmerged}, {State: PRNone}, {State: PRUnknown}, {}} {
		if f.Note(safe) != "" || f.Note(unmerged) != "" {
			t.Errorf("%+v has a note", f)
		}
	}
	// Only a safe row is raised: a retained, a decision and a red row are never.
	if !m.RaisesToStrong(safe) || m.RaisesToStrong(unmerged) || m.RaisesToStrong(onlineCand("r", KindWorktree, "d", VerdictDecision, ProofNone, "locked-stw", oidOf(3))) {
		t.Error("RaisesToStrong")
	}
	if (PREvidence{State: PRNone}).RaisesToStrong(safe) || (PREvidence{State: PRClosedUnmerged}).RaisesToStrong(safe) || (PREvidence{}).RaisesToStrong(safe) {
		t.Error("only a merged head raises")
	}
}

// fakeGitHub is a request-counting double of the hosting layer: it answers every GET and fails the
// test on any other method. A member session of the double sends the requests the real discovery
// would: one repository read and one read per tip.
type fakeGitHub struct {
	t      *testing.T
	mu     sync.Mutex
	gets   int
	byRepo map[string]int
	status map[string]int // repository -> status of every request after `after`
	after  map[string]int
	// exhausted makes a 403 report an exhausted rate limit.
	exhausted bool
	writes    atomic.Int64
}

func (g *fakeGitHub) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		g.writes.Add(1)
		g.t.Errorf("write request %s %s", req.Method, req.URL)
		return nil, errors.New("write")
	}
	repo := strings.Split(req.URL.Path, "/")[3]
	g.mu.Lock()
	g.gets++
	g.byRepo[repo]++
	n := g.byRepo[repo]
	status := 200
	if s, ok := g.status[repo]; ok && n > g.after[repo] {
		status = s
	}
	exhausted := g.exhausted
	g.mu.Unlock()
	h := http.Header{}
	if status == 403 && exhausted {
		h.Set("X-RateLimit-Remaining", "0")
	}
	return &http.Response{StatusCode: status, Header: h, Body: http.NoBody}, nil
}

type fakeSession struct {
	rt    http.RoundTripper
	repo  string
	merge func(oid string) hosting.TidyTipEvidence
	// extra rows are appended to the answer (a row for a tip that was not asked), drop rows are left out.
	extra []hosting.TidyTipEvidence
	drop  int
}

func (s *fakeSession) get(ctx context.Context, path string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+s.repo+path, nil)
	res, err := s.rt.RoundTrip(req)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", res.StatusCode)
	}
	return nil
}

// ReadTipPullRequests sends what the real session sends: the repository read, the revalidation
// and one read per tip.
func (s *fakeSession) ReadTipPullRequests(ctx context.Context, targets []string) ([]hosting.TidyTipEvidence, error) {
	if err := s.get(ctx, ""); err != nil {
		return nil, err
	}
	rows := []hosting.TidyTipEvidence{}
	sorted := append([]string{}, targets...)
	sort.Strings(sorted)
	for _, oid := range sorted {
		if err := s.get(ctx, "/commits/"+oid+"/pulls"); err != nil {
			return nil, err
		}
		row := s.merge(oid)
		row.Tip = oid
		rows = append(rows, row)
	}
	rows = rows[:len(rows)-min(s.drop, len(rows))]
	return append(rows, s.extra...), nil
}

func (s *fakeSession) Close() {}

func mergedRow(oid string) hosting.TidyTipEvidence {
	return hosting.TidyTipEvidence{State: hosting.TidyTipMerged, Number: 100 + int(oid[len(oid)-1])}
}

// workspaceDouble is the 7-member, 466-tip workspace of Spec 044 SC-005: 165 safe tips with an
// equivalent proof and 301 unmerged tips spread over seven repositories.
func workspaceDouble() ([]OnlineMember, []Candidate, map[string]int) {
	members := []OnlineMember{}
	cands := []Candidate{}
	perRepo := map[string]int{}
	n := 0
	for i := 0; i < 7; i++ {
		name := fmt.Sprintf("m%d", i)
		members = append(members, OnlineMember{Name: name, Path: "/ws/" + name, Remote: "origin"})
		count := 66
		if i < 4 {
			count = 67
		}
		for j := 0; j < count; j++ {
			n++
			if n <= 165 {
				cands = append(cands, equivalentBranch(name, fmt.Sprintf("e%03d", n), n))
			} else {
				cands = append(cands, unmergedBranch(name, fmt.Sprintf("u%03d", n), n))
			}
			perRepo[name]++
		}
	}
	return members, cands, perRepo
}

func openFake(g *fakeGitHub, merge func(string) hosting.TidyTipEvidence) PROpener {
	return func(_ context.Context, path, _ string, rt http.RoundTripper) (PRSession, error) {
		return &fakeSession{rt: rt, repo: "x/" + strings.TrimPrefix(path, "/ws/"), merge: merge}, nil
	}
}

func newDouble(t *testing.T) *fakeGitHub {
	return &fakeGitHub{t: t, byRepo: map[string]int{}, status: map[string]int{}, after: map[string]int{}}
}

func TestFetchSevenMembersAbout466TipsRespectsEveryBudget(t *testing.T) {
	members, cands, perRepo := workspaceDouble()
	if len(cands) != 466 {
		t.Fatalf("%d tips", len(cands))
	}
	before := append([]Candidate{}, cands...)
	g := newDouble(t)
	// A member with 64 tips needs 1 + 64 = 65 requests; with a run cap of 400 the seventh is cut.
	out := FetchPREvidence(context.Background(), members, cands, OnlineOptions{Open: openFake(g, mergedRow), Transport: g, Width: 1, RunCap: 400})
	if g.gets > 400 || out.Requests != g.gets || g.writes.Load() != 0 {
		t.Fatalf("requests %d counted %d writes %d", g.gets, out.Requests, g.writes.Load())
	}
	checked, capped := 0, 0
	for _, m := range out.Members {
		switch m.State {
		case MemberChecked:
			checked++
			if m.Asked != 64 || m.Eligible != perRepo[m.Name] || m.NotChecked != perRepo[m.Name]-64 {
				t.Errorf("%+v", m)
			}
		case MemberOffline:
			if m.Reason != ReasonRunCap {
				t.Errorf("%+v", m)
			}
			capped++
		}
	}
	if checked != 6 || capped != 1 || len(out.Facts) != 6*64 {
		t.Fatalf("checked %d offline %d facts %d", checked, capped, len(out.Facts))
	}
	if !reflect.DeepEqual(before, cands) {
		t.Fatal("the fetch changed a candidate: online evidence never changes a row")
	}
	for _, c := range cands[:1] {
		if f, ok := out.Lookup(c); !ok || f.State != PRMerged || f.Number == 0 {
			t.Errorf("%+v %v", f, ok)
		}
	}
	// The member that hit the run cap has no fact at all: judged offline, never "unknown".
	last := out.Members[len(out.Members)-1]
	if _, ok := out.Lookup(cands[len(cands)-1]); ok || last.State != MemberOffline {
		t.Errorf("the capped member keeps online facts: %+v", last)
	}
	if out.Notice() != "GitHub: checked 6 of 7 repositories; the others are judged offline (per-run request cap reached)" {
		t.Errorf("%q", out.Notice())
	}
}

func TestFetchWithManyFewRequestsChecksEveryMember(t *testing.T) {
	members, cands, _ := workspaceDouble()
	g := newDouble(t)
	// Closed unmerged everywhere: the repository read and one read per tip.
	none := func(string) hosting.TidyTipEvidence {
		return hosting.TidyTipEvidence{State: hosting.TidyTipClosedUnmerged}
	}
	out := FetchPREvidence(context.Background(), members, cands, OnlineOptions{Open: openFake(g, none), Transport: g})
	if g.gets != 7*65 || out.Notice() != "GitHub: checked 7 of 7 repositories" {
		t.Fatalf("%d requests, %q", g.gets, out.Notice())
	}
	for _, m := range out.Members {
		if m.State != MemberChecked || m.Asked != 64 {
			t.Errorf("%+v", m)
		}
	}
	if f, ok := out.Lookup(cands[0]); !ok || f.State != PRClosedUnmerged {
		t.Errorf("%+v %v", f, ok)
	}
	// A tip beyond the cap of 64 is not checked: no fact (absent), which is not "none".
	if _, ok := out.Lookup(cands[66]); ok && out.Members[0].NotChecked != 3 {
		t.Errorf("a tip beyond the cap has a fact")
	}
}

func twoMembers() ([]OnlineMember, []Candidate) {
	return []OnlineMember{{Name: "a", Path: "/ws/a", Remote: "origin"}, {Name: "b", Path: "/ws/b", Remote: "origin"}, {Name: "c", Path: "/ws/c", Remote: "origin"}},
		[]Candidate{equivalentBranch("a", "x", 1), unmergedBranch("b", "y", 2), unmergedBranch("c", "z", 3)}
}

// Every failure judges offline and never fails the run: the reason is the exact one, the other
// repositories keep their own results unless the run itself stopped.
func TestFetchFallbackReasons(t *testing.T) {
	members, cands := twoMembers()
	merged := mergedRow
	for name, tc := range map[string]struct {
		open       func(g *fakeGitHub) PROpener
		prep       func(g *fakeGitHub)
		cancel     bool
		wantA      string // reason of a ("" = checked)
		wantB      string
		wantNotice string
	}{
		"not authenticated": {
			open: func(*fakeGitHub) PROpener {
				return func(context.Context, string, string, http.RoundTripper) (PRSession, error) {
					return nil, hosting.ErrTidyAuthentication
				}
			},
			wantA: ReasonNotAuthenticated, wantB: ReasonNotAuthenticated,
			wantNotice: "GitHub: checked 0 of 3 repositories; the others are judged offline (gh is not authenticated)",
		},
		"remote not github.com": {
			open: func(*fakeGitHub) PROpener {
				return func(context.Context, string, string, http.RoundTripper) (PRSession, error) {
					return nil, fmt.Errorf("wrapped: %w", hosting.ErrTidyUnsupported)
				}
			},
			wantA: ReasonRemoteUnsupported, wantB: ReasonRemoteUnsupported,
			wantNotice: "GitHub: checked 0 of 3 repositories; the others are judged offline (the remote is not a supported github.com repository)",
		},
		"one member's own budget ends only that member": {
			open: func(g *fakeGitHub) PROpener {
				good := openFake(g, merged)
				return func(ctx context.Context, path, remote string, rt http.RoundTripper) (PRSession, error) {
					if path == "/ws/a" {
						return &failingSession{err: hosting.ErrTidyLimit}, nil
					}
					return good(ctx, path, remote, rt)
				}
			},
			wantA: ReasonMemberBudget, wantB: "",
			wantNotice: "GitHub: checked 2 of 3 repositories; the others are judged offline (a repository's request or time budget ran out)",
		},
		"a plain error ends only that member": {
			open: func(g *fakeGitHub) PROpener {
				good := openFake(g, merged)
				return func(ctx context.Context, path, remote string, rt http.RoundTripper) (PRSession, error) {
					if path == "/ws/a" {
						return &failingSession{err: errors.New("boom")}, nil
					}
					return good(ctx, path, remote, rt)
				}
			},
			wantA: ReasonError, wantB: "",
			wantNotice: "GitHub: checked 2 of 3 repositories; the others are judged offline (a GitHub read failed)",
		},
		"rate limit stops every unfinished member": {
			open: func(g *fakeGitHub) PROpener { return openFake(g, merged) },
			prep: func(g *fakeGitHub) { g.status["a"], g.after["a"] = 429, 0 },
			// a is first in order and runs alone (Width 1): it hits the limit, b and c never send.
			wantA: ReasonRateLimit, wantB: ReasonRateLimit,
			wantNotice: "GitHub: checked 0 of 3 repositories; the others are judged offline (GitHub rate limit reached)",
		},
		"exhausted 403": {
			open:  func(g *fakeGitHub) PROpener { return openFake(g, merged) },
			prep:  func(g *fakeGitHub) { g.status["a"], g.after["a"] = 403, 1; g.exhausted = true },
			wantA: ReasonRateLimit, wantB: ReasonRateLimit,
			wantNotice: "GitHub: checked 0 of 3 repositories; the others are judged offline (GitHub rate limit reached)",
		},
		"server error": {
			open:  func(g *fakeGitHub) PROpener { return openFake(g, merged) },
			prep:  func(g *fakeGitHub) { g.status["a"], g.after["a"] = 502, 1 },
			wantA: ReasonServer, wantB: ReasonServer,
			wantNotice: "GitHub: checked 0 of 3 repositories; the others are judged offline (GitHub reported a server error)",
		},
		"a permission 403 is only that member": {
			open:  func(g *fakeGitHub) PROpener { return openFake(g, merged) },
			prep:  func(g *fakeGitHub) { g.status["a"], g.after["a"] = 403, 1 },
			wantA: ReasonError, wantB: "",
			wantNotice: "GitHub: checked 2 of 3 repositories; the others are judged offline (a GitHub read failed)",
		},
		"timeout": {
			open:   func(g *fakeGitHub) PROpener { return openFake(g, merged) },
			cancel: true,
			wantA:  ReasonTimeout, wantB: ReasonTimeout,
			wantNotice: "GitHub: checked 0 of 3 repositories; the others are judged offline (timeout reached)",
		},
	} {
		g := newDouble(t)
		if tc.prep != nil {
			tc.prep(g)
		}
		ctx, cancel := context.WithCancel(context.Background())
		if tc.cancel {
			cancel()
		}
		before := append([]Candidate{}, cands...)
		out := FetchPREvidence(ctx, members, cands, OnlineOptions{Open: tc.open(g), Transport: g, Width: 1})
		cancel()
		byName := map[string]MemberOutcome{}
		for _, m := range out.Members {
			byName[m.Name] = m
		}
		check := func(member, want string) {
			m := byName[member]
			if want == "" && m.State != MemberChecked || want != "" && (m.State != MemberOffline || m.Reason != want) {
				t.Errorf("%s: member %s is %+v, want reason %q", name, member, m, want)
			}
			if _, ok := out.Lookup(cands[map[string]int{"a": 0, "b": 1, "c": 2}[member]]); ok == (want != "") {
				t.Errorf("%s: member %s: a fact exists exactly when it finished (%v)", name, member, ok)
			}
		}
		check("a", tc.wantA)
		check("b", tc.wantB)
		if out.Notice() != tc.wantNotice {
			t.Errorf("%s: notice %q\nwant %q", name, out.Notice(), tc.wantNotice)
		}
		if strings.Contains(out.Notice(), "\n") || g.writes.Load() != 0 || !reflect.DeepEqual(before, cands) {
			t.Errorf("%s: one line, no write, no row changed", name)
		}
	}
}

type failingSession struct{ err error }

func (s *failingSession) ReadTipPullRequests(context.Context, []string) ([]hosting.TidyTipEvidence, error) {
	return nil, s.err
}
func (s *failingSession) Close() {}

// A repository with nothing to ask opens no session and sends no request; a run with no tip at
// all has no notice.
func TestFetchWithoutTipsMakesNoRequest(t *testing.T) {
	members, _ := twoMembers()
	g := newDouble(t)
	opened := 0
	open := func(ctx context.Context, p, r string, rt http.RoundTripper) (PRSession, error) {
		opened++
		return openFake(g, mergedRow)(ctx, p, r, rt)
	}
	strong := []Candidate{onlineCand("a", KindBranch, "s", VerdictSafe, ProofAncestor, "", oidOf(1)), onlineCand("b", KindBranch, "d", VerdictRetain, ProofNone, "dirty", oidOf(2))}
	out := FetchPREvidence(context.Background(), members, strong, OnlineOptions{Open: open, Transport: g})
	if opened != 0 || g.gets != 0 || out.Requests != 0 || len(out.Facts) != 0 {
		t.Fatalf("opened %d gets %d requests %d", opened, g.gets, out.Requests)
	}
	if out.Notice() != "GitHub: checked 3 of 3 repositories" {
		t.Fatalf("%q", out.Notice())
	}
	if FormatOnlineNotice(0, 0, nil) != "" || (OnlineOutcome{}).Notice() != "" {
		t.Fatal("no repository, no line")
	}
}

// The width of the fan-out never raises a bound: four repositories at the same time still send at
// most 512 requests and never a write.
func TestFetchWidthFourKeepsTheRunCap(t *testing.T) {
	members, cands, _ := workspaceDouble()
	g := newDouble(t)
	out := FetchPREvidence(context.Background(), members, cands, OnlineOptions{Open: openFake(g, mergedRow), Transport: g, Width: 4, RunCap: 400})
	if g.gets > 400 || out.Requests > 400 || g.writes.Load() != 0 {
		t.Fatalf("%d requests", g.gets)
	}
	for _, m := range out.Members {
		if m.State == MemberChecked && len(out.Facts) == 0 {
			t.Fatal("a checked member without facts")
		}
	}
}

// The tip of an observed worktree and branch reaches the row (never the JSON); a tracking reference
// and a stale registration have none.
func TestTipReachesTheRowsButNotTheJSON(t *testing.T) {
	linked := evid("/ws/wt/a", "refs/heads/feat/a")
	linked.HeadOID = oidOf(7)
	obs := workspace.WorktreeObservation{
		Worktrees: []workspace.WorktreeEvidence{linked},
		Branches:  []workspace.BranchEvidence{{Name: "refs/heads/topic", OID: oidOf(8), Proof: "ancestor", AuthorHint: "Ana"}},
	}
	a, _ := testAdapter(obs, nil)
	res := Classify(context.Background(), a, []string{"r"}, Config{Caller: "werner", CWD: "/ws/r", Now: old.Add(RecentWindow * 4)})
	tips := map[Kind]string{}
	for _, c := range res.Candidates {
		tips[c.Kind] = c.Tip
	}
	if tips[KindWorktree] != oidOf(7) || tips[KindBranch] != oidOf(8) {
		t.Fatalf("%v", tips)
	}
	raw, err := json.Marshal(res.Candidates)
	if err != nil || strings.Contains(string(raw), oidOf(7)) || strings.Contains(string(raw), oidOf(8)) || strings.Contains(string(raw), "tip") {
		t.Fatalf("the tip is in the JSON: %s %v", raw, err)
	}
}

func TestFormatOnlineNotice(t *testing.T) {
	for name, tc := range map[string]struct {
		checked, of int
		reasons     []string
		want        string
	}{
		"nothing":         {0, 0, nil, ""},
		"all":             {3, 3, nil, "GitHub: checked 3 of 3 repositories"},
		"one reason once": {1, 3, []string{ReasonRateLimit, ReasonRateLimit}, "GitHub: checked 1 of 3 repositories; the others are judged offline (GitHub rate limit reached)"},
		"fixed order":     {0, 2, []string{ReasonTimeout, ReasonNotAuthenticated}, "GitHub: checked 0 of 2 repositories; the others are judged offline (gh is not authenticated; timeout reached)"},
		"generic hidden":  {0, 2, []string{ReasonError, ReasonNotAuthenticated}, "GitHub: checked 0 of 2 repositories; the others are judged offline (gh is not authenticated)"},
		"generic alone":   {0, 2, []string{ReasonError}, "GitHub: checked 0 of 2 repositories; the others are judged offline (a GitHub read failed)"},
		"too large":       {0, 2, []string{ReasonResponseTooLarge, ReasonError}, "GitHub: checked 0 of 2 repositories; the others are judged offline (a GitHub response exceeded its size bound)"},
		"unknown word":    {0, 1, []string{"surprise"}, "GitHub: checked 0 of 1 repository; the others are judged offline (a GitHub read failed)"},
	} {
		if got := FormatOnlineNotice(tc.checked, tc.of, tc.reasons); got != tc.want {
			t.Errorf("%s: %q", name, got)
		}
	}
}

// When the run stopped and the context ended too, the stop is the reason (the cause comes first);
// after a stop no later repository opens a session at all.
func TestFetchAStopOfTheRunComesBeforeTheTimeoutAndOpensNothingMore(t *testing.T) {
	members, cands := twoMembers()
	g := newDouble(t)
	g.status["a"], g.after["a"] = 429, 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened := 0
	good := openFake(g, mergedRow)
	open := func(c context.Context, path, remote string, rt http.RoundTripper) (PRSession, error) {
		opened++
		s, err := good(c, path, remote, rt)
		if err != nil {
			return nil, err
		}
		return &cancelAfter{PRSession: s, cancel: cancel}, nil
	}
	out := FetchPREvidence(ctx, members, cands, OnlineOptions{Open: open, Transport: g, Width: 1})
	for _, m := range out.Members {
		if m.State != MemberOffline || m.Reason != ReasonRateLimit {
			t.Errorf("%+v", m)
		}
	}
	if opened != 1 || out.Stopped != hosting.TidyStopRateLimit {
		t.Errorf("opened %d stopped %q", opened, out.Stopped)
	}
}

// cancelAfter ends the run's context once the session has made its reads.
type cancelAfter struct {
	PRSession
	cancel context.CancelFunc
}

func (c *cancelAfter) ReadTipPullRequests(ctx context.Context, targets []string) ([]hosting.TidyTipEvidence, error) {
	rows, err := c.PRSession.ReadTipPullRequests(ctx, targets)
	c.cancel()
	return rows, err
}

// After a 429 no later repository opens a session or sends a request.
func TestFetchAfterAStopNoSessionIsOpened(t *testing.T) {
	members, cands := twoMembers()
	g := newDouble(t)
	g.status["a"], g.after["a"] = 429, 0
	opened := 0
	good := openFake(g, mergedRow)
	open := func(c context.Context, path, remote string, rt http.RoundTripper) (PRSession, error) {
		opened++
		return good(c, path, remote, rt)
	}
	out := FetchPREvidence(context.Background(), members, cands, OnlineOptions{Open: open, Transport: g, Width: 1})
	if opened != 1 || g.gets != 1 || out.Requests != 1 || out.Notice() != "GitHub: checked 0 of 3 repositories; the others are judged offline (GitHub rate limit reached)" {
		t.Errorf("opened %d gets %d requests %d %q", opened, g.gets, out.Requests, out.Notice())
	}
}

// A session that answers for a tip that was not asked, or leaves one out, judges only that
// repository offline: a partial or foreign answer is never used.
func TestFetchRefusesAForeignOrMissingRow(t *testing.T) {
	members, cands := twoMembers()
	for name, session := range map[string]func(*fakeSession){
		"foreign row": func(s *fakeSession) {
			s.extra = []hosting.TidyTipEvidence{{Tip: oidOf(99), State: hosting.TidyTipMerged, Number: 7}}
		},
		"missing row": func(s *fakeSession) { s.drop = 1 },
		"swapped row": func(s *fakeSession) {
			s.drop, s.extra = 1, []hosting.TidyTipEvidence{{Tip: oidOf(99), State: hosting.TidyTipMerged, Number: 7}}
		},
	} {
		g := newDouble(t)
		good := openFake(g, mergedRow)
		open := func(c context.Context, path, remote string, rt hosting.TidyTransport) (PRSession, error) {
			s, err := good(c, path, remote, rt)
			if err == nil && path == "/ws/a" {
				session(s.(*fakeSession))
			}
			return s, err
		}
		out := FetchPREvidence(context.Background(), members, cands, OnlineOptions{Open: open, Transport: g, Width: 1})
		byName := map[string]MemberOutcome{}
		for _, m := range out.Members {
			byName[m.Name] = m
		}
		if m := byName["a"]; m.State != MemberOffline || m.Reason != ReasonError {
			t.Errorf("%s: %+v", name, m)
		}
		if _, ok := out.Lookup(cands[0]); ok {
			t.Errorf("%s: a fact of a rejected answer is used", name)
		}
		if _, ok := out.Facts[TipKey{Repo: "a", Tip: oidOf(99)}]; ok {
			t.Errorf("%s: the foreign tip became a fact", name)
		}
		if byName["b"].State != MemberChecked || byName["c"].State != MemberChecked {
			t.Errorf("%s: the others are unaffected: %+v", name, byName)
		}
	}
}

// A response over its size bound gets its own reason (not a generic failed read).
func TestFetchNamesAnOversizedResponse(t *testing.T) {
	members, cands := twoMembers()
	g := newDouble(t)
	open := func(context.Context, string, string, hosting.TidyTransport) (PRSession, error) {
		return &failingSession{err: fmt.Errorf("wrapped: %w", hosting.ErrTidyResponseTooLarge)}, nil
	}
	out := FetchPREvidence(context.Background(), members, cands, OnlineOptions{Open: open, Transport: g})
	if out.Notice() != "GitHub: checked 0 of 3 repositories; the others are judged offline (a GitHub response exceeded its size bound)" {
		t.Errorf("%q", out.Notice())
	}
}

// The run budget is shared with whoever else reads in the run (the tracking reads of the
// observation): a stop already seen means no session is opened and no request is sent, and
// requests already counted leave less for the pull request reads.
func TestFetchSharesTheRunBudgetWithEarlierReads(t *testing.T) {
	members, cands := twoMembers()
	g := newDouble(t)
	g.status["tracking"], g.after["tracking"] = 429, 0
	budget := hosting.NewTidyRunBudget(g, hosting.TidyRunMaxRequests)
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/x/tracking", nil)
	if res, err := budget.RoundTrip(req); err != nil || res.StatusCode != 429 {
		t.Fatalf("%v", err)
	}
	opened := 0
	good := openFake(g, mergedRow)
	open := func(c context.Context, path, remote string, rt hosting.TidyTransport) (PRSession, error) {
		opened++
		return good(c, path, remote, rt)
	}
	out := FetchPREvidence(context.Background(), members, cands, OnlineOptions{Open: open, Budget: budget})
	if opened != 0 || g.gets != 1 || out.Requests != 1 || out.Stopped != hosting.TidyStopRateLimit || len(out.Facts) != 0 {
		t.Fatalf("opened %d gets %d requests %d stopped %q", opened, g.gets, out.Requests, out.Stopped)
	}
	if out.Notice() != "GitHub: checked 0 of 3 repositories; the others are judged offline (GitHub rate limit reached)" {
		t.Errorf("%q", out.Notice())
	}

	// Requests of earlier reads count against the cap of 512 of the same run.
	g = newDouble(t)
	budget = hosting.NewTidyRunBudget(g, hosting.TidyRunMaxRequests)
	for i := 0; i < hosting.TidyRunMaxRequests-3; i++ {
		req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/x/tracking", nil)
		if res, err := budget.RoundTrip(req); err != nil {
			t.Fatal(err)
		} else {
			res.Body.Close()
		}
	}
	out = FetchPREvidence(context.Background(), members, cands, OnlineOptions{Open: openFake(g, mergedRow), Budget: budget, Width: 1})
	if g.gets != hosting.TidyRunMaxRequests || out.Requests != hosting.TidyRunMaxRequests || out.Stopped != hosting.TidyStopRunCap {
		t.Fatalf("gets %d requests %d stopped %q", g.gets, out.Requests, out.Stopped)
	}
}
