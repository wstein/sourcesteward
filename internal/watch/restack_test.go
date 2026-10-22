// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// rebaseFixture is data recorded from a real git rebase-merge of a stacked pair (testdata/rebase_merge.json).
type rebaseFixture struct {
	Base   string `json:"base"`
	Parent struct {
		Number      int      `json:"number"`
		HeadRef     string   `json:"head_ref"`
		HeadSHA     string   `json:"head_sha"`
		MergeCommit string   `json:"merge_commit_sha"`
		Rewritten   []string `json:"rewritten_commits"`
	} `json:"parent"`
	ChildOnParent   fixturePR `json:"child_on_parent"`
	ChildRetargeted fixturePR `json:"child_retargeted"`
}

type fixturePR struct {
	Number  int    `json:"number"`
	Base    string `json:"base"`
	HeadRef string `json:"head_ref"`
	Commits []struct {
		SHA string `json:"sha"`
	} `json:"commits"`
}

func (p fixturePR) shas() []string {
	var out []string
	for _, c := range p.Commits {
		out = append(out, c.SHA)
	}
	return out
}

func loadRebaseFixture(t *testing.T) rebaseFixture {
	t.Helper()
	b, err := os.ReadFile("testdata/rebase_merge.json")
	if err != nil {
		t.Fatal(err)
	}
	var f rebaseFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// fakeCommits serves commit lists per PR in pages of pageSize and records every read.
type fakeCommits struct {
	lists    map[int][]string
	errs     map[int]error
	pageSize int
	reads    []string
}

func (f *fakeCommits) PullCommits(ctx context.Context, repo string, number, page int) ([]string, bool, error) {
	f.reads = append(f.reads, fmt.Sprintf("%d:%d", number, page))
	if err := f.errs[number]; err != nil {
		return nil, false, err
	}
	size := f.pageSize
	if size == 0 {
		size = 100
	}
	all := f.lists[number]
	lo, hi := (page-1)*size, page*size
	if lo > len(all) {
		lo = len(all)
	}
	more := hi < len(all)
	if hi > len(all) {
		hi = len(all)
	}
	return all[lo:hi], more, nil
}

func wantArgv(newParent, oldParent, branch string) []string {
	return []string{"git", "-c", "rebase.updateRefs=false", "-c", "rerere.enabled=false", "rebase", "--onto", newParent, oldParent, branch}
}

// TestRestackRebaseMergeFixture is US3 on recorded data: the rewrite changed every SHA, the old head is
// found only in the retargeted child's commit list and the class-1 child is found by its base ref alone.
func TestRestackRebaseMergeFixture(t *testing.T) {
	f := loadRebaseFixture(t)
	if f.Parent.MergeCommit == f.Parent.HeadSHA {
		t.Fatal("fixture is not a rewrite")
	}
	// Every rewritten commit differs from every original parent commit.
	orig := map[string]bool{}
	for _, c := range f.ChildRetargeted.shas() {
		orig[c] = true
	}
	for _, r := range f.Parent.Rewritten {
		if orig[r] {
			t.Fatalf("rewritten commit %s is still an original", r)
		}
	}
	if len(f.Parent.Rewritten) != 2 || len(f.ChildRetargeted.shas()) != 4 || !orig[f.Parent.HeadSHA] {
		t.Fatal("fixture shape changed")
	}
	for _, s := range f.ChildRetargeted.shas() {
		if s == f.Parent.MergeCommit {
			t.Fatal("rewritten tip must not be in the retargeted list")
		}
	}
	lc := &fakeCommits{lists: map[int][]string{f.ChildRetargeted.Number: f.ChildRetargeted.shas(), f.ChildOnParent.Number: f.ChildOnParent.shas(), 99: {sha("ee")}}}
	var diag []string
	rs := &GitRestacker{Commits: lc, Diag: func(s string) { diag = append(diag, s) }}
	mc := f.Parent.MergeCommit
	due := RestackDue{MergedPR: f.Parent.Number, MergeCommit: &mc, OldParent: f.Parent.HeadSHA, Base: f.Base, HeadRef: f.Parent.HeadRef}
	open := []OpenPR{
		{Number: f.ChildOnParent.Number, Base: f.ChildOnParent.Base, HeadRef: "child-a", Head: sha("c1")},
		{Number: f.ChildRetargeted.Number, Base: f.ChildRetargeted.Base, HeadRef: f.ChildRetargeted.HeadRef, Head: sha("c2")},
		{Number: 99, Base: "main", HeadRef: "unrelated", Head: sha("ee")},
		{Number: 98, Base: "other", HeadRef: "elsewhere", Head: sha("ff")},
	}
	got, err := rs.Candidates(context.Background(), "o/r", due, open)
	if err != nil {
		t.Fatal(err)
	}
	want := []RestackCandidate{
		{PR: f.ChildOnParent.Number, Reason: ReasonChildOfMerged, Branch: "child-a", Argv: wantArgv(mc, f.Parent.HeadSHA, "child-a")},
		{PR: f.ChildRetargeted.Number, Reason: ReasonContains, Branch: f.ChildRetargeted.HeadRef, Argv: wantArgv(mc, f.Parent.HeadSHA, f.ChildRetargeted.HeadRef)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates\n got %+v\nwant %+v", got, want)
	}
	// Class 1 never reads commits; the unrelated PR on the base is read, the PR on another base is not.
	if !reflect.DeepEqual(lc.reads, []string{"12:1", "99:1"}) {
		t.Fatalf("reads %v", lc.reads)
	}
	if len(diag) != 0 {
		t.Fatalf("diag %v", diag)
	}
}

func TestRestackNullsAndFork(t *testing.T) {
	old := sha("aa")
	lc := &fakeCommits{lists: map[int][]string{12: {old}}}
	rs := &GitRestacker{Commits: lc}
	open := []OpenPR{{Number: 11, Base: "feat/p", HeadRef: "c11", Fork: true}, {Number: 12, Base: "main", HeadRef: "c12"}}
	due := RestackDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "feat/p"}
	got, err := rs.Candidates(context.Background(), "o/r", due, open)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Argv != nil || got[1].Argv != nil {
		t.Fatalf("unknown merge commit must give null argv: %+v", got)
	}
	nc := sha("bb")
	due.MergeCommit = &nc
	got, _ = rs.Candidates(context.Background(), "o/r", due, open)
	if got[0].Argv != nil || got[0].PR != 11 {
		t.Fatalf("fork child must give null argv: %+v", got[0])
	}
	if !reflect.DeepEqual(got[1].Argv, wantArgv(nc, old, "c12")) {
		t.Fatalf("argv %v", got[1].Argv)
	}
}

func TestRestackPagingAndCap(t *testing.T) {
	old := sha("aa")
	var list []string
	for i := 0; i < 5; i++ {
		list = append(list, sha(fmt.Sprintf("%02x", i+1)))
	}
	list = append(list, old) // sixth: page 3 at page size 2
	lc := &fakeCommits{lists: map[int][]string{12: list}, pageSize: 2}
	rs := &GitRestacker{Commits: lc}
	due := RestackDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "p"}
	got, err := rs.Candidates(context.Background(), "o/r", due, []OpenPR{{Number: 12, Base: "main", HeadRef: "c"}})
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	// Beyond the 3-page cap the SHA is not found: no candidate, no fourth read.
	lc = &fakeCommits{lists: map[int][]string{12: append([]string{sha("01"), sha("02"), sha("03"), sha("04"), sha("05"), sha("06")}, old)}, pageSize: 2}
	rs = &GitRestacker{Commits: lc}
	got, err = rs.Candidates(context.Background(), "o/r", due, []OpenPR{{Number: 12, Base: "main", HeadRef: "c"}})
	if err != nil || len(got) != 0 || len(lc.reads) != 3 {
		t.Fatalf("%v %+v %v", err, got, lc.reads)
	}
}

func TestRestackSkipsUnnamableBranch(t *testing.T) {
	old := sha("aa")
	lc := &fakeCommits{lists: map[int][]string{12: {old}, 13: {old}}}
	var diag []string
	rs := &GitRestacker{Commits: lc, Diag: func(s string) { diag = append(diag, s) }, BranchOK: func(n string) error {
		if strings.Contains(n, "%") {
			return errors.New("percent")
		}
		return defaultBranchOK(n)
	}}
	due := RestackDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "p"}
	open := []OpenPR{
		{Number: 11, Base: "p", HeadRef: "bad%20name"},
		{Number: 12, Base: "main", HeadRef: "-rf"},
		{Number: 13, Base: "main", HeadRef: "ok/branch"},
	}
	got, err := rs.Candidates(context.Background(), "o/r", due, open)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PR != 13 {
		t.Fatalf("%+v", got)
	}
	if len(diag) != 2 || !strings.Contains(diag[0], "#11") || !strings.Contains(diag[1], "#12") {
		t.Fatalf("diag %v", diag)
	}
}

func TestRestackErrors(t *testing.T) {
	old := sha("aa")
	due := RestackDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "p"}
	open := []OpenPR{{Number: 12, Base: "main", HeadRef: "c12"}, {Number: 13, Base: "main", HeadRef: "c13"}}
	// A malformed commit list skips that PR with a diagnostic; the cycle goes on.
	var diag []string
	lc := &fakeCommits{lists: map[int][]string{13: {old}}, errs: map[int]error{12: fmt.Errorf("bad: %w", ErrMalformed)}}
	rs := &GitRestacker{Commits: lc, Diag: func(s string) { diag = append(diag, s) }}
	got, err := rs.Candidates(context.Background(), "o/r", due, open)
	if err != nil || len(got) != 1 || got[0].PR != 13 || len(diag) != 1 {
		t.Fatalf("%v %+v %v", err, got, diag)
	}
	// Any other failure is returned so that the core retries the work in the next cycle.
	boom := errors.New("network")
	lc = &fakeCommits{errs: map[int]error{12: boom}}
	rs = &GitRestacker{Commits: lc}
	if _, err := rs.Candidates(context.Background(), "o/r", due, open); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
}

// A fork PR fork:main into main has head ref == base. It has no children by branch name: only the commit check applies.
func TestRestackHeadRefEqualsBaseUsesCommitCheck(t *testing.T) {
	old := sha("aa")
	lc := &fakeCommits{lists: map[int][]string{12: {sha("01")}, 13: {old}}}
	rs := &GitRestacker{Commits: lc}
	due := RestackDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "main"}
	got, err := rs.Candidates(context.Background(), "o/r", due, []OpenPR{{Number: 12, Base: "main", HeadRef: "c"}, {Number: 13, Base: "main", HeadRef: "d"}})
	if err != nil || len(got) != 1 || got[0].PR != 13 || got[0].Reason != ReasonContains || len(lc.reads) != 2 {
		t.Fatalf("%v %+v %v", err, got, lc.reads)
	}
}

// A merged fork PR must not match an upstream PR whose base is a branch with the fork head's name.
func TestRestackForkHeadHasNoClassOne(t *testing.T) {
	old := sha("aa")
	lc := &fakeCommits{}
	rs := &GitRestacker{Commits: lc}
	due := RestackDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "feat/x", HeadFork: true}
	got, err := rs.Candidates(context.Background(), "o/r", due, []OpenPR{{Number: 12, Base: "feat/x", HeadRef: "c"}})
	if err != nil || len(got) != 0 || len(lc.reads) != 0 {
		t.Fatalf("%v %+v %v", err, got, lc.reads)
	}
}

// The first hit ends the reading, even when the lister reports more pages.
func TestRestackStopsAtFirstHit(t *testing.T) {
	old := sha("aa")
	lc := &fakeCommits{lists: map[int][]string{12: {old, sha("01"), sha("02"), sha("03")}}, pageSize: 2}
	rs := &GitRestacker{Commits: lc}
	due := RestackDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "p"}
	got, err := rs.Candidates(context.Background(), "o/r", due, []OpenPR{{Number: 12, Base: "main", HeadRef: "c"}})
	if err != nil || len(got) != 1 || len(lc.reads) != 1 {
		t.Fatalf("%v %+v %v", err, got, lc.reads)
	}
}

// The merged PR's head repository is taken from the open PR in state, in a live cycle and in a replay.
func TestMergedDueRecordsHeadFork(t *testing.T) {
	for _, tc := range []struct {
		name string
		prs  map[int]*PRState
		want bool
	}{
		{"same repository", map[int]*PRState{10: {Number: 10}}, false},
		{"fork", map[int]*PRState{10: {Number: 10, Fork: true}}, true},
		{"unobserved", map[int]*PRState{}, true},
	} {
		st := newState()
		st.Repo = map[string]*RepoState{"o/r": {PRs: tc.prs}}
		c := sha("cc")
		st.apply(Event{Repo: rp("o/r"), PR: pp(10), Kind: KindMerged, ID: "o/r#10:merged:" + c,
			Details: &MergedDetails{MergeCommit: &c, Base: "main", Head: sha("aa"), HeadRef: "f"}})
		if got := st.Repo["o/r"].RestackDue[0].HeadFork; got != tc.want {
			t.Errorf("%s: HeadFork %v", tc.name, got)
		}
	}
}
