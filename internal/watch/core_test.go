// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFirstRunIsASilentBaselineAndWritesState(t *testing.T) {
	r := newRig(t)
	none(t, r.must(ok(mk(1), mk(2, draft()))))
	if r.out.Len() != 0 || len(r.journal()) != 0 {
		t.Fatal("a baseline prints and journals nothing")
	}
	s := r.state()
	if len(s.Repo["o/r"].PRs) != 2 || !s.Repo["o/r"].Baselined || s.SchemaVersion != 1 {
		t.Fatalf("%+v", s)
	}
	st, _ := os.Stat(filepath.Join(r.dir, "state.json"))
	if st.Mode().Perm() != 0o600 {
		t.Fatal(st.Mode())
	}
	hb, _ := os.ReadFile(filepath.Join(r.dir, "heartbeat"))
	if !strings.HasSuffix(string(hb), " once\n") {
		t.Fatalf("heartbeat %q", hb)
	}
	// second run: only the change is printed
	ev := one(t, r.must(ok(mk(1), mk(2, draft()), mk(3))), KindNewPR)
	if ev.Seq != 1 || *ev.PR != 3 {
		t.Fatalf("%+v", ev)
	}
	if got := r.stdoutIDs(); !reflect.DeepEqual(got, []string{"o/r#3:new-pr:g0"}) {
		t.Fatal(got)
	}
}

func TestForcedBaselineAfterStateExistsIsSilentAndReplacesState(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1)))
	one(t, r.must(ok(mk(1), mk(2))), KindNewPR)
	r.base = true
	r.mustOpen()
	none(t, r.must(ok(mk(2, head("beef")), mk(5)))) // 1 vanished, 2 force-pushed, 5 appeared: all silent
	s := r.state()
	if len(s.Repo["o/r"].PRs) != 2 || s.Repo["o/r"].PRs[2].Head != sha("beef") || s.Repo["o/r"].PRs[1] != nil {
		t.Fatalf("%+v", s.Repo["o/r"].PRs)
	}
	if len(r.journal()) != 1 {
		t.Fatal("the journal is never rewritten")
	}
	// counters survive the baseline so ids stay unique
	if s.Repo["o/r"].PRs[2].Generation != 0 {
		t.Fatal("generation")
	}
	r.base = false
	r.mustOpen()
	one(t, r.must(ok(mk(2, head("cafe")), mk(5))), KindHeadChanged)
}

func TestNewPRDetailsAndKeys(t *testing.T) {
	r := newRig(t)
	r.must(ok())
	ev := one(t, r.must(ok(mk(41, draft(), base("dev")))), KindNewPR)
	d := ev.Details.(*NewPRDetails)
	want := &NewPRDetails{Title: "PR 41", Author: "alice", Draft: true, Base: "dev", HeadRef: "feat/41", Head: sha("291")}
	if !reflect.DeepEqual(d, want) || ev.ID != "o/r#41:new-pr:g0" || *ev.Repo != "o/r" || ev.Time != "2026-10-22T09:02:00Z" {
		t.Fatalf("%+v %+v", ev, d)
	}
}

func TestHeadChangedFiresOnEveryForcePushIncludingAReturn(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1, head("aa"))))
	e1 := one(t, r.must(ok(mk(1, head("bb")))), KindHeadChanged)
	e2 := one(t, r.must(ok(mk(1, head("aa")))), KindHeadChanged)
	e3 := one(t, r.must(ok(mk(1, head("bb")))), KindHeadChanged)
	if e1.ID != "o/r#1:head-changed:"+sha("bb")+":h1" || e2.ID != "o/r#1:head-changed:"+sha("aa")+":h2" || e3.ID != "o/r#1:head-changed:"+sha("bb")+":h3" {
		t.Fatal(e1.ID, e2.ID, e3.ID)
	}
	if d := e2.Details.(*HeadChangedDetails); d.Old != sha("bb") || d.New != sha("aa") {
		t.Fatalf("%+v", d)
	}
	uniq(t, r.journalIDs())
}

func TestChecksAreDebouncedAndEveryAcceptedClassChangeBumpsTheEpoch(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1, runs(running()))))
	// flap: red for one cycle then pending again
	none(t, r.must(ok(mk(1, runs(failing("a", ""))))))
	none(t, r.must(ok(mk(1, runs(running())))))
	// stable red for two cycles
	none(t, r.must(ok(mk(1, runs(failing("b", ""))))))
	red := one(t, r.must(ok(mk(1, runs(failing("b", ""))))), KindChecksRed)
	// green, red, green again: red-green-red announces each time
	none(t, r.must(ok(mk(1, runs(passing())))))
	green := one(t, r.must(ok(mk(1, runs(passing())))), KindChecksGreen)
	none(t, r.must(ok(mk(1, runs(failing("b", ""))))))
	red2 := one(t, r.must(ok(mk(1, runs(failing("b", ""))))), KindChecksRed)
	h := sha("11")
	if red.ID != "o/r#1:checks-red:"+h+":c1" || green.ID != "o/r#1:checks-green:"+h+":c2" || red2.ID != "o/r#1:checks-red:"+h+":c3" {
		t.Fatal(red.ID, green.ID, red2.ID)
	}
	rd := red.Details.(*ChecksRedDetails)
	if rd.Head != h || !reflect.DeepEqual(rd.Failing, []string{"b"}) || rd.FailingTotal != 1 || rd.Shape != ShapeUnknown || rd.Base != "main" || rd.Draft {
		t.Fatalf("%+v", rd)
	}
	if g := green.Details.(*ChecksGreenDetails); g.Total != 1 || g.Head != h || g.Base != "main" {
		t.Fatalf("%+v", g)
	}
}

func TestChecksNoneNeverProducesGreenAndPendingEmitsNothing(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1)))
	none(t, r.must(ok(mk(1, runs()))))
	none(t, r.must(ok(mk(1, runs()))))
	none(t, r.must(ok(mk(1, runs(running())))))
	none(t, r.must(ok(mk(1, runs(running())))))
	if r.state().Repo["o/r"].PRs[1].Class != ClassPending {
		t.Fatal("pending should be accepted silently")
	}
}

func TestChecksReadOnlyWhenObservedLeavesAcceptedClassAlone(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1, runs(passing()))))
	for i := 0; i < 3; i++ {
		none(t, r.must(ok(mk(1)))) // checks not read this cycle
	}
	if r.state().Repo["o/r"].PRs[1].Class != ClassGreen {
		t.Fatal("class lost")
	}
}

func TestChecksRedShapeStack(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1, base("parent"), runs(running()))))
	f := failing("lint", "Unexpected synthetic merge parents")
	none(t, r.must(ok(mk(1, base("parent"), runs(f)))))
	ev := one(t, r.must(ok(mk(1, base("parent"), runs(f)))), KindChecksRed)
	if ev.Details.(*ChecksRedDetails).Shape != ShapeStack {
		t.Fatal("shape")
	}
}

func TestHeadChangeResetsChecksAndCandidates(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1, head("aa"), runs(passing()))))
	none(t, r.must(ok(mk(1, head("aa"), runs(failing("x", "")))))) // candidate red
	one(t, r.must(ok(mk(1, head("bb")))), KindHeadChanged)         // candidate cleared, class unobserved
	none(t, r.must(ok(mk(1, head("bb"), runs(failing("x", "")))))) // first sighting again
	one(t, r.must(ok(mk(1, head("bb"), runs(failing("x", ""))))), KindChecksRed)
}

func TestPendingTimeoutFiresOncePerPendingPeriod(t *testing.T) {
	r := newRig(t)
	r.timeout = 10 * time.Minute
	r.must(ok(mk(1, runs(running()))))
	// the baseline at 09:01 accepted pending; the timeout fires after more than 10 minutes, at 09:12
	for i := 0; i < 10; i++ {
		none(t, r.must(ok(mk(1, runs(running())))))
	}
	ev := one(t, r.must(ok(mk(1, runs(running())))), KindChecksPendingTimout)
	d := ev.Details.(*PendingTimeoutDetails)
	if d.Head != sha("11") || !reflect.DeepEqual(d.Pending, []string{"ci"}) || d.Since != "2026-10-22T09:01:00Z" {
		t.Fatalf("%+v", d)
	}
	if ev.ID != "o/r#1:checks-pending-timeout:"+sha("11")+":c0:t" {
		t.Fatal(ev.ID)
	}
	none(t, r.must(ok(mk(1, runs(running())))))
	// a class change starts a new period
	r.must(ok(mk(1, runs(passing()))))
	r.must(ok(mk(1, runs(passing()))))
	r.must(ok(mk(1, runs(running()))))
	r.must(ok(mk(1, runs(running()))))
	for i := 0; i < 12; i++ {
		r.must(ok(mk(1, runs(running()))))
	}
	got := r.journalIDs()
	if got[len(got)-1] != "o/r#1:checks-pending-timeout:"+sha("11")+":c2:t" {
		t.Fatal(got)
	}
}

func TestPendingTimeoutDisabledWithZero(t *testing.T) {
	r := newRig(t)
	r.timeout = 0
	r.must(ok(mk(1, runs(running()))))
	for i := 0; i < 80; i++ {
		none(t, r.must(ok(mk(1, runs(running())))))
	}
}

func TestMergeableIsDebouncedAndNullKeepsThePreviousValue(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1, detail(tt(), "clean"))))
	none(t, r.must(ok(mk(1, detail(nil, "unknown"))))) // not computed
	none(t, r.must(ok(mk(1, detail(ff(), "dirty")))))  // candidate
	none(t, r.must(ok(mk(1, detail(tt(), "clean")))))  // flapped back: silent
	none(t, r.must(ok(mk(1, detail(ff(), "dirty")))))
	ev := one(t, r.must(ok(mk(1, detail(ff(), "dirty")))), KindMergeableChanged)
	d := ev.Details.(*MergeableDetails)
	if d.Mergeable == nil || *d.Mergeable || d.State != "dirty" || !d.Conflicting || d.Behind || d.Base != "main" {
		t.Fatalf("%+v", d)
	}
	if ev.ID != "o/r#1:mergeable-changed:"+sha("11")+":m1" {
		t.Fatal(ev.ID)
	}
}

func TestMergeableBehindEntersAndLeaves(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1, detail(tt(), "clean"))))
	r.must(ok(mk(1, detail(tt(), "behind"))))
	in := one(t, r.must(ok(mk(1, detail(tt(), "behind")))), KindMergeableChanged)
	if d := in.Details.(*MergeableDetails); !d.Behind || d.Conflicting {
		t.Fatalf("%+v", d)
	}
	r.must(ok(mk(1, detail(tt(), "clean"))))
	out := one(t, r.must(ok(mk(1, detail(tt(), "clean")))), KindMergeableChanged)
	if out.Details.(*MergeableDetails).Behind || !strings.HasSuffix(out.ID, ":m2") {
		t.Fatal(out.ID)
	}
	// state text changing alone is silent
	none(t, r.must(ok(mk(1, detail(tt(), "unstable")))))
}

func TestReadyStateAndBaseChangedAreImmediate(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1, draft())))
	ev := one(t, r.must(ok(mk(1))), KindReadyStateChanged)
	if d := ev.Details.(*ReadyDetails); d.From != "draft" || d.To != "ready" || ev.ID != "o/r#1:ready-state-changed:r1" {
		t.Fatalf("%+v %s", d, ev.ID)
	}
	ev = one(t, r.must(ok(mk(1, draft()))), KindReadyStateChanged)
	if ev.ID != "o/r#1:ready-state-changed:r2" || ev.Details.(*ReadyDetails).To != "draft" {
		t.Fatal(ev.ID)
	}
	b := one(t, r.must(ok(mk(1, draft(), base("dev")))), KindBaseChanged)
	if d := b.Details.(*BaseChangedDetails); d.Old != "main" || d.New != "dev" || b.ID != "o/r#1:base-changed:b1" {
		t.Fatalf("%+v", b)
	}
}

func TestMergedAndRestackNeeded(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(10, head("aa")), mk(11, base("feat/10")), mk(12)))
	c := sha("cc")
	nc := c
	r.rs.byMerge[10] = []RestackCandidate{
		{PR: 12, Reason: ReasonContains, Branch: "feat/12", Argv: []string{"git", "rebase"}},
		{PR: 11, Reason: ReasonChildOfMerged, Branch: "feat/11"},
	}
	evs := r.must(ok(mk(11, base("feat/10")), mk(12)).gone(10, merged(c, sha("aa"))))
	if got := kinds(evs); !reflect.DeepEqual(got, []string{KindMerged, KindRestackNeeded, KindRestackNeeded}) {
		t.Fatal(got)
	}
	m := evs[0].Details.(*MergedDetails)
	if m.MergeCommit == nil || *m.MergeCommit != nc || m.Head != sha("aa") || m.Base != "main" || m.MethodHint != "rebase-or-squash" || evs[0].ID != "o/r#10:merged:"+c {
		t.Fatalf("%+v %s", m, evs[0].ID)
	}
	// ascending child number, ids carry the merged PR and the merge commit
	if evs[1].ID != "o/r#11:restack-needed:10:"+c || evs[2].ID != "o/r#12:restack-needed:10:"+c {
		t.Fatal(ids(evs))
	}
	rd := evs[1].Details.(*RestackDetails)
	want := &RestackDetails{MergedPR: 10, Base: "main", Reason: ReasonChildOfMerged, OldParent: sha("aa"), NewParent: &nc, NewParentSource: "merge_commit_sha", Branch: "feat/11", Argv: nil}
	if !reflect.DeepEqual(rd, want) {
		t.Fatalf("%+v", rd)
	}
	none(t, r.must(ok(mk(11, base("feat/10")), mk(12))))
	if due := r.state().Repo["o/r"].RestackDue; len(due) != 0 {
		t.Fatalf("restack_due must be cleared at the end of the cycle: %+v", due)
	}
}

func TestMergedWithUnreadableCommitHasUnknownKeyAndNullParent(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(10, head("aa")), mk(12)))
	r.rs.byMerge[10] = []RestackCandidate{{PR: 12, Reason: ReasonContains, Branch: "feat/12"}}
	evs := r.must(ok(mk(12)).gone(10, merged("", sha("aa"))))
	if evs[0].ID != "o/r#10:merged:unknown" || evs[1].ID != "o/r#12:restack-needed:10:unknown" {
		t.Fatal(ids(evs))
	}
	if rd := evs[1].Details.(*RestackDetails); rd.NewParent != nil || rd.Argv != nil {
		t.Fatalf("%+v", rd)
	}
}

func TestGoneButUnreadablePRIsRetriedAndNeverGuessed(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(10, head("aa")), mk(12)))
	r.rs.byMerge[10] = []RestackCandidate{{PR: 12, Reason: ReasonContains, Branch: "feat/12"}}
	none(t, r.must(ok(mk(12)))) // 10 left the list, pulls/10 not readable
	none(t, r.must(ok(mk(12))))
	if r.rs.calls != 0 {
		t.Fatal("no restack work without a merged event")
	}
	evs := r.must(ok(mk(12)).gone(10, merged(sha("cc"), sha("aa"))))
	if got := kinds(evs); !reflect.DeepEqual(got, []string{KindMerged, KindRestackNeeded}) {
		t.Fatal(got)
	}
}

func TestSecondMergeForTheSameChildGetsADistinctID(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(10, head("aa")), mk(11, head("bb")), mk(12)))
	r.rs.byMerge[10] = []RestackCandidate{{PR: 12, Reason: ReasonContains, Branch: "feat/12"}}
	r.rs.byMerge[11] = []RestackCandidate{{PR: 12, Reason: ReasonContains, Branch: "feat/12"}}
	e1 := r.must(ok(mk(11, head("bb")), mk(12)).gone(10, merged(sha("c1"), sha("aa"))))
	e2 := r.must(ok(mk(12)).gone(11, merged(sha("c2"), sha("bb"))))
	if e1[1].ID != "o/r#12:restack-needed:10:"+sha("c1") || e2[1].ID != "o/r#12:restack-needed:11:"+sha("c2") {
		t.Fatal(ids(e1), ids(e2))
	}
	uniq(t, r.journalIDs())
}

func TestMergeMarksOtherOpenPRsDueForADetailRead(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(10, head("aa")), mk(12, detail(tt(), "clean"))))
	r.must(ok(mk(12, detail(tt(), "clean"))).gone(10, merged(sha("cc"), sha("aa"))))
	if !r.state().Repo["o/r"].PRs[12].DetailDue {
		t.Fatal("PR 12 must be due for a detail read")
	}
	r.must(ok(mk(12, detail(tt(), "clean"))))
	if r.state().Repo["o/r"].PRs[12].DetailDue {
		t.Fatal("a detail observation clears the mark")
	}
}

func TestClosedReopenedClosedGenerations(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(5), mk(6)))
	c0 := one(t, r.must(ok(mk(6)).gone(5, closedGone(sha("51")))), KindClosed)
	n1 := one(t, r.must(ok(mk(5), mk(6))), KindNewPR)
	c1 := one(t, r.must(ok(mk(6)).gone(5, closedGone(sha("51")))), KindClosed)
	if c0.ID != "o/r#5:closed:g0" || n1.ID != "o/r#5:new-pr:g1" || c1.ID != "o/r#5:closed:g1" {
		t.Fatal(c0.ID, n1.ID, c1.ID)
	}
	if d := c0.Details.(*ClosedDetails); d.Head != sha("51") || d.Base != "main" {
		t.Fatalf("%+v", d)
	}
}

func TestReopenedPRKeepsItsCountersSoIDsStayUnique(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(5, head("aa"))))
	one(t, r.must(ok(mk(5, head("bb")))), KindHeadChanged)
	r.must(ok().gone(5, closedGone(sha("bb"))))
	r.must(ok(mk(5, head("bb"))))
	again := one(t, r.must(ok(mk(5, head("aa")))), KindHeadChanged)
	if !strings.HasSuffix(again.ID, ":h2") {
		t.Fatalf("epoch restarted: %s", again.ID)
	}
	uniq(t, r.journalIDs())
}

func TestIgnoredAuthorsAndLabelsProduceNoEvents(t *testing.T) {
	r := newRig(t)
	r.ignoreA, r.ignoreL = []string{"bot[bot]"}, []string{"no-watch"}
	r.mustOpen()
	r.must(ok())
	none(t, r.must(ok(mk(1, author("bot[bot]")), mk(2, labels("x", "no-watch")))))
	none(t, r.must(ok(mk(1, author("bot[bot]"), head("ee")), mk(2, labels("no-watch"), draft()))))
	ev := one(t, r.must(ok(mk(3))), KindNewPR)
	if *ev.PR != 3 {
		t.Fatal(ev)
	}
	// a PR that becomes ignored vanishes silently
	none(t, r.must(ok(mk(3, labels("no-watch")))))
	if len(r.state().Repo["o/r"].PRs) != 0 {
		t.Fatal("ignored PR kept")
	}
}

func TestSeqIsGaplessFromTheJournalTailEvenWithoutState(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1)))
	r.must(ok(mk(1), mk(2)))
	r.must(ok(mk(1), mk(2), mk(3)))
	os.Remove(filepath.Join(r.dir, "state.json"))
	r.mustOpen()
	r.must(ok(mk(1), mk(2), mk(3))) // the lost state is a silent baseline
	ev := one(t, r.must(ok(mk(1), mk(2), mk(3), mk(4))), KindNewPR)
	if ev.Seq != 3 {
		t.Fatalf("seq %d", ev.Seq)
	}
	for i, e := range r.journal() {
		if e.Seq != int64(i+1) {
			t.Fatal("gap")
		}
	}
}

func TestLostStateKeepsIDsUniqueByRebuildingCountersFromTheJournal(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1, head("aa"))))
	one(t, r.must(ok(mk(1, head("bb")))), KindHeadChanged)
	os.Remove(filepath.Join(r.dir, "state.json"))
	r.mustOpen()
	none(t, r.must(ok(mk(1, head("bb")))))
	ev := one(t, r.must(ok(mk(1, head("aa")))), KindHeadChanged)
	if !strings.HasSuffix(ev.ID, ":h2") {
		t.Fatal(ev.ID)
	}
	if got := strings.Count(r.out.String(), "\n"); got != 2 {
		t.Fatalf("a lost state must not reprint the old journal: %d lines", got)
	}
}

func TestCorruptStateIsAnErrorUnlessBaseline(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1)))
	r.w.Close()
	r.w = nil
	os.WriteFile(filepath.Join(r.dir, "state.json"), []byte("{broken"), 0o600)
	if err := r.open(); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("%v", err)
	}
	r.base = true
	r.mustOpen()
	none(t, r.must(ok(mk(1))))
}

func TestSecondWatcherOnTheSameDirectoryIsLockedOut(t *testing.T) {
	r := newRig(t)
	r.mustOpen()
	_, err := Open(r.options())
	var held *LockedError
	if !errorsAs(err, &held) {
		t.Fatalf("%v", err)
	}
}

func TestReposChangedAddedRepoGetsASilentBaselineAndAbaAbIDsAreDistinct(t *testing.T) {
	r := newRig(t, "o/a")
	r.set("o/a", ok(mk(1)))
	r.set("o/b", ok(mk(7)))
	r.mustOpen()
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	var seen []string
	for _, set := range [][]string{{"o/a", "o/b"}, {"o/a"}, {"o/a", "o/b"}, {"o/a"}} {
		r.repos = set
		r.mustOpen()
		evs, err := r.run()
		if err != nil {
			t.Fatal(err)
		}
		one(t, evs, KindReposChanged)
		seen = append(seen, evs[0].ID)
	}
	if !reflect.DeepEqual(seen, []string{"-:repos-changed:p1", "-:repos-changed:p2", "-:repos-changed:p3", "-:repos-changed:p4"}) {
		t.Fatal(seen)
	}
	// the re-added repo o/b announced nothing for its existing PR 7 (silent baseline)
	for _, e := range r.journal() {
		if e.Kind == KindNewPR {
			t.Fatalf("baseline leaked: %s", e.ID)
		}
	}
	d := r.journal()[0].Details.(*ReposChangedDetails)
	if !reflect.DeepEqual(d.Added, []string{"o/b"}) || len(d.Removed) != 0 || d.Skipped == nil {
		t.Fatalf("%+v", d)
	}
	if e := r.journal()[0]; e.Repo != nil || e.PR != nil {
		t.Fatal("repos-changed is run-level")
	}
}

func TestReposChangedReportsASkippedSubmodule(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1)))
	r.skipped = []Skipped{{Path: "vendor/x", Reason: "no GitHub remote"}}
	r.mustOpen()
	evs, _ := r.run()
	d := one(t, evs, KindReposChanged).Details.(*ReposChangedDetails)
	if len(d.Added) != 0 || len(d.Removed) != 0 || !reflect.DeepEqual(d.Skipped, r.skipped) {
		t.Fatalf("%+v", d)
	}
	evs, _ = r.run()
	none(t, evs)
}
