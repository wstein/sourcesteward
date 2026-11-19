// SPDX-License-Identifier: EUPL-1.2
package watch

// TestExportScenarios writes the cycle scenarios of the core (internal/watch) as JSON. It is skipped unless
// STW_EXPORT_DIR names an absolute directory. Each scenario is a translation of Go tests: the inputs are the
// observations the test feeds, the expectations are what the Go watcher produced (FORMAT.md).

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/watchexport"
)

func tp(b bool) *bool { return &b }

func TestExportScenarios(t *testing.T) {
	dir, err := watchexport.OutDir()
	if err != nil {
		t.Skip("STW_EXPORT_DIR not set: " + err.Error())
	}
	for _, sc := range coreScenarios() {
		sc := sc
		t.Run(sc.name, func(t *testing.T) { sc.run(t, dir) })
	}
	t.Run("crash-matrix", func(t *testing.T) { exportCrashMatrix(t, dir) })
	t.Run("crash-extra", func(t *testing.T) {
		for _, sc := range crashExtraScenarios() {
			sc := sc
			t.Run(sc.name, func(t *testing.T) { sc.run(t, dir) })
		}
	})
	t.Run("fuzz-sequences", func(t *testing.T) { exportFuzzSequences(t, dir) })
	t.Run("vectors", func(t *testing.T) { exportCoreVectors(t, dir) })
}

func coreScenarios() []*xsc {
	var out []*xsc
	add := func(s *xsc) *xsc { out = append(out, s); return s }
	h := func(s string) string { return sha(s) }
	f := failing("lint", "Unexpected synthetic merge parents")

	add(newXSC("first-run-silent-baseline", "The first successful read is a silent baseline; the next cycle announces only the change.",
		"core_test.go:TestFirstRunIsASilentBaselineAndWritesState").
		cyc(ok(mk(1), mk(2, draft()))).
		cyc(ok(mk(1), mk(2, draft()), mk(3)), KindNewPR))

	add(newXSC("forced-baseline-after-state", "A forced baseline after a state exists is silent and replaces the accepted view; counters survive so ids stay unique.",
		"core_test.go:TestForcedBaselineAfterStateExistsIsSilentAndReplacesState").
		cyc(ok(mk(1))).
		cyc(ok(mk(1), mk(2)), KindNewPR).
		add(xstep{baseline: tp(true), restart: true, obs: map[string]snap{"o/r": ok(mk(2, head("beef")), mk(5))}, guard: true}).
		add(xstep{baseline: tp(false), restart: true, obs: map[string]snap{"o/r": ok(mk(2, head("cafe")), mk(5))}, guard: true, want: []string{KindHeadChanged}}))

	add(newXSC("new-pr-details", "The details of new-pr and its time.", "core_test.go:TestNewPRDetailsAndKeys").
		cyc(ok()).cyc(ok(mk(41, draft(), base("dev"))), KindNewPR))

	add(newXSC("head-changed-every-force-push", "head-changed fires on every force push including a return to an earlier head; every transition is a distinct event.",
		"core_test.go:TestHeadChangedFiresOnEveryForcePushIncludingAReturn").
		cyc(ok(mk(1, head("aa")))).
		cyc(ok(mk(1, head("bb"))), KindHeadChanged).cyc(ok(mk(1, head("aa"))), KindHeadChanged).cyc(ok(mk(1, head("bb"))), KindHeadChanged))

	add(newXSC("checks-debounce-and-epochs", "Check classes need two consecutive sightings; every accepted class change (also pending) bumps the checks epoch.",
		"core_test.go:TestChecksAreDebouncedAndEveryAcceptedClassChangeBumpsTheEpoch").
		any(ok(mk(1, runs(running())))).
		cyc(ok(mk(1, runs(failing("a", ""))))).cyc(ok(mk(1, runs(running())))).
		cyc(ok(mk(1, runs(failing("b", ""))))).cyc(ok(mk(1, runs(failing("b", "")))), KindChecksRed).
		cyc(ok(mk(1, runs(passing())))).cyc(ok(mk(1, runs(passing()))), KindChecksGreen).
		cyc(ok(mk(1, runs(failing("b", ""))))).cyc(ok(mk(1, runs(failing("b", "")))), KindChecksRed))

	add(newXSC("checks-none-never-green", "An empty check list (class none) never produces checks-green; an accepted pending emits nothing.",
		"core_test.go:TestChecksNoneNeverProducesGreenAndPendingEmitsNothing").
		any(ok(mk(1))).cyc(ok(mk(1, runs()))).cyc(ok(mk(1, runs()))).cyc(ok(mk(1, runs(running())))).cyc(ok(mk(1, runs(running())))))

	add(newXSC("checks-not-read-keeps-class", "A cycle that did not read checks leaves the accepted class alone.",
		"core_test.go:TestChecksReadOnlyWhenObservedLeavesAcceptedClassAlone").
		any(ok(mk(1, runs(passing())))).cyc(ok(mk(1))).cyc(ok(mk(1))).cyc(ok(mk(1))))

	add(newXSC("checks-red-shape-stack", "A red head on a stacked base with the stack message has shape stack-merge-shape.",
		"core_test.go:TestChecksRedShapeStack").
		any(ok(mk(1, base("parent"), runs(running())))).
		cyc(ok(mk(1, base("parent"), runs(f)))).cyc(ok(mk(1, base("parent"), runs(f))), KindChecksRed))

	add(newXSC("head-change-resets-checks-and-candidates", "A head change clears the class and the stored candidates.",
		"core_test.go:TestHeadChangeResetsChecksAndCandidates").
		any(ok(mk(1, head("aa"), runs(passing())))).
		cyc(ok(mk(1, head("aa"), runs(failing("x", ""))))).
		cyc(ok(mk(1, head("bb"))), KindHeadChanged).
		cyc(ok(mk(1, head("bb"), runs(failing("x", ""))))).
		cyc(ok(mk(1, head("bb"), runs(failing("x", "")))), KindChecksRed))

	{
		s := newXSC("pending-timeout-once-per-period", "checks-pending-timeout fires once per pending period (limit 10 minutes) and again after a class change.",
			"core_test.go:TestPendingTimeoutFiresOncePerPendingPeriod")
		s.timeout, s.timeoutSet = 10*time.Minute, true
		s.any(ok(mk(1, runs(running()))))
		for i := 0; i < 10; i++ {
			s.cyc(ok(mk(1, runs(running()))))
		}
		s.cyc(ok(mk(1, runs(running()))), KindChecksPendingTimout).cyc(ok(mk(1, runs(running()))))
		s.any(ok(mk(1, runs(passing())))).any(ok(mk(1, runs(passing())))).any(ok(mk(1, runs(running())))).any(ok(mk(1, runs(running()))))
		s.times(12, ok(mk(1, runs(running()))))
		s.total = map[string]int{KindChecksPendingTimout: 2}
		add(s)
	}

	{
		s := newXSC("pending-timeout-disabled-with-zero", "A pending timeout of 0 disables checks-pending-timeout.", "core_test.go:TestPendingTimeoutDisabledWithZero")
		s.timeout, s.timeoutSet, s.lean = 0, true, true
		s.any(ok(mk(1, runs(running()))))
		for i := 0; i < 80; i++ {
			s.cyc(ok(mk(1, runs(running()))))
		}
		add(s)
	}

	add(newXSC("mergeable-debounced-null-keeps", "mergeable-changed needs two sightings; a null mergeable keeps the previous value; a flap back is silent.",
		"core_test.go:TestMergeableIsDebouncedAndNullKeepsThePreviousValue").
		any(ok(mk(1, detail(tt(), "clean")))).
		cyc(ok(mk(1, detail(nil, "unknown")))).cyc(ok(mk(1, detail(ff(), "dirty")))).cyc(ok(mk(1, detail(tt(), "clean")))).
		cyc(ok(mk(1, detail(ff(), "dirty")))).cyc(ok(mk(1, detail(ff(), "dirty"))), KindMergeableChanged))

	add(newXSC("mergeable-behind-enters-and-leaves", "Entering and leaving behind is a mergeable-changed; a change of the state text alone is silent.",
		"core_test.go:TestMergeableBehindEntersAndLeaves").
		any(ok(mk(1, detail(tt(), "clean")))).any(ok(mk(1, detail(tt(), "behind")))).
		cyc(ok(mk(1, detail(tt(), "behind"))), KindMergeableChanged).
		any(ok(mk(1, detail(tt(), "clean")))).cyc(ok(mk(1, detail(tt(), "clean"))), KindMergeableChanged).
		cyc(ok(mk(1, detail(tt(), "unstable")))))

	add(newXSC("ready-state-and-base-changed", "ready-state-changed and base-changed are immediate.", "core_test.go:TestReadyStateAndBaseChangedAreImmediate").
		any(ok(mk(1, draft()))).cyc(ok(mk(1)), KindReadyStateChanged).cyc(ok(mk(1, draft())), KindReadyStateChanged).
		cyc(ok(mk(1, draft(), base("dev"))), KindBaseChanged))

	{
		c := h("cc")
		s := newXSC("merged-and-restack-needed", "A merge emits merged, then restack-needed for the children in ascending PR order; restack_due is cleared at the end of the cycle.",
			"core_test.go:TestMergedAndRestackNeeded")
		s.byMerge[10] = []RestackCandidate{
			{PR: 12, Reason: ReasonContains, Branch: "feat/12", Argv: []string{"git", "rebase"}},
			{PR: 11, Reason: ReasonChildOfMerged, Branch: "feat/11"}}
		s.any(ok(mk(10, head("aa")), mk(11, base("feat/10")), mk(12)))
		s.cyc(ok(mk(11, base("feat/10")), mk(12)).gone(10, merged(c, h("aa"))), KindMerged, KindRestackNeeded, KindRestackNeeded)
		s.cyc(ok(mk(11, base("feat/10")), mk(12)))
		add(s)
	}

	{
		s := newXSC("merged-unreadable-commit", "A merge whose commit is unreadable has the key unknown and a null new parent.",
			"core_test.go:TestMergedWithUnreadableCommitHasUnknownKeyAndNullParent")
		s.byMerge[10] = []RestackCandidate{{PR: 12, Reason: ReasonContains, Branch: "feat/12"}}
		s.any(ok(mk(10, head("aa")), mk(12)))
		s.cyc(ok(mk(12)).gone(10, merged("", h("aa"))), KindMerged, KindRestackNeeded)
		add(s)
	}

	{
		s := newXSC("gone-unreadable-retried", "A PR that left the list with an unreadable pulls/N is retried every cycle and never guessed.",
			"core_test.go:TestGoneButUnreadablePRIsRetriedAndNeverGuessed")
		s.byMerge[10] = []RestackCandidate{{PR: 12, Reason: ReasonContains, Branch: "feat/12"}}
		s.any(ok(mk(10, head("aa")), mk(12)))
		s.cyc(ok(mk(12))).cyc(ok(mk(12)))
		s.cyc(ok(mk(12)).gone(10, merged(h("cc"), h("aa"))), KindMerged, KindRestackNeeded)
		add(s)
	}

	{
		s := newXSC("second-merge-same-child-distinct-ids", "A second merge for the same child is a distinct restack-needed transition.",
			"core_test.go:TestSecondMergeForTheSameChildGetsADistinctID")
		s.byMerge[10] = []RestackCandidate{{PR: 12, Reason: ReasonContains, Branch: "feat/12"}}
		s.byMerge[11] = []RestackCandidate{{PR: 12, Reason: ReasonContains, Branch: "feat/12"}}
		s.any(ok(mk(10, head("aa")), mk(11, head("bb")), mk(12)))
		s.cyc(ok(mk(11, head("bb")), mk(12)).gone(10, merged(h("c1"), h("aa"))), KindMerged, KindRestackNeeded)
		s.cyc(ok(mk(12)).gone(11, merged(h("c2"), h("bb"))), KindMerged, KindRestackNeeded)
		add(s)
	}

	add(newXSC("merge-marks-open-prs-detail-due", "A merge marks the other open PRs due for a detail read; a detail observation clears the mark.",
		"core_test.go:TestMergeMarksOtherOpenPRsDueForADetailRead").
		any(ok(mk(10, head("aa")), mk(12, detail(tt(), "clean")))).
		any(ok(mk(12, detail(tt(), "clean"))).gone(10, merged(h("cc"), h("aa")))).
		any(ok(mk(12, detail(tt(), "clean")))))

	add(newXSC("closed-reopened-closed-generations", "A closed PR that reopens gets the next generation; closed and new-pr ids carry it.",
		"core_test.go:TestClosedReopenedClosedGenerations").
		any(ok(mk(5), mk(6))).
		cyc(ok(mk(6)).gone(5, closedGone(h("51"))), KindClosed).cyc(ok(mk(5), mk(6)), KindNewPR).
		cyc(ok(mk(6)).gone(5, closedGone(h("51"))), KindClosed))

	add(newXSC("reopened-pr-keeps-counters", "A reopened PR keeps its epoch counters so event ids stay unique.", "core_test.go:TestReopenedPRKeepsItsCountersSoIDsStayUnique").
		any(ok(mk(5, head("aa")))).cyc(ok(mk(5, head("bb"))), KindHeadChanged).
		any(ok().gone(5, closedGone(h("bb")))).any(ok(mk(5, head("bb")))).cyc(ok(mk(5, head("aa"))), KindHeadChanged))

	{
		s := newXSC("ignored-authors-and-labels", "Ignored authors and labels produce no events; a PR that becomes ignored vanishes silently.",
			"core_test.go:TestIgnoredAuthorsAndLabelsProduceNoEvents")
		s.ignoreA, s.ignoreL = []string{"bot[bot]"}, []string{"no-watch"}
		s.any(ok())
		s.cyc(ok(mk(1, author("bot[bot]")), mk(2, labels("x", "no-watch"))))
		s.cyc(ok(mk(1, author("bot[bot]"), head("ee")), mk(2, labels("no-watch"), draft())))
		s.cyc(ok(mk(3)), KindNewPR)
		s.cyc(ok(mk(3, labels("no-watch"))))
		add(s)
	}

	add(newXSC("lost-state-silent-baseline-seq-gapless", "A lost state with a non-empty journal is a silent baseline and the journal continues without a gap.",
		"core_test.go:TestSeqIsGaplessFromTheJournalTailEvenWithoutState").
		any(ok(mk(1))).cyc(ok(mk(1), mk(2)), KindNewPR).cyc(ok(mk(1), mk(2), mk(3)), KindNewPR).
		add(xstep{fileOp: "delete-state", obs: map[string]snap{"o/r": ok(mk(1), mk(2), mk(3))}, guard: true}).
		cyc(ok(mk(1), mk(2), mk(3), mk(4)), KindNewPR))

	add(newXSC("lost-state-rebuilds-counters", "A lost state rebuilds the epoch counters from the journal so a later head-changed gets the next epoch; the old journal is not reprinted.",
		"core_test.go:TestLostStateKeepsIDsUniqueByRebuildingCountersFromTheJournal").
		any(ok(mk(1, head("aa")))).cyc(ok(mk(1, head("bb"))), KindHeadChanged).
		add(xstep{fileOp: "delete-state", obs: map[string]snap{"o/r": ok(mk(1, head("bb")))}, guard: true}).
		cyc(ok(mk(1, head("aa"))), KindHeadChanged))

	add(newXSC("corrupt-state-is-an-error-unless-baseline", "A corrupt state is an error when opening, never silently reset; a forced baseline repairs it.",
		"core_test.go:TestCorruptStateIsAnErrorUnlessBaseline").
		any(ok(mk(1))).
		add(xstep{action: "open", fileOp: "corrupt-state"}).
		add(xstep{baseline: tp(true), restart: true, obs: map[string]snap{"o/r": ok(mk(1))}, guard: true}))

	{
		s := newXSC("repos-changed-added-removed-abab", "A repository added gets a silent baseline; every change of the repository set is a distinct repos-changed (ABAB).",
			"core_test.go:TestReposChangedAddedRepoGetsASilentBaselineAndAbaAbIDsAreDistinct")
		s.repos = []string{"o/a"}
		s.add(xstep{obs: map[string]snap{"o/a": ok(mk(1)), "o/b": ok(mk(7))}, guard: true})
		for _, set := range [][]string{{"o/a", "o/b"}, {"o/a"}, {"o/a", "o/b"}, {"o/a"}} {
			s.add(xstep{repos: set, guard: true, want: []string{KindReposChanged}})
		}
		add(s)
	}

	{
		s := newXSC("repos-changed-skipped-submodule", "A skipped submodule is reported once in repos-changed.", "core_test.go:TestReposChangedReportsASkippedSubmodule")
		s.any(ok(mk(1)))
		sk := []Skipped{{Path: "vendor/x", Reason: "no GitHub remote"}}
		s.add(xstep{skipped: &sk, guard: true, want: []string{KindReposChanged}})
		s.add(xstep{guard: true})
		add(s)
	}

	// health
	{
		s := newXSC("watch-error-episodes", "watch-error per episode and code; a long outage also stalls; recovery closes the episode; a second episode gets a new id.",
			"health_test.go:TestWatchErrorEpisodes")
		s.any(ok(mk(1)))
		s.cyc(failWith("network"), KindWatchError)
		s.cyc(failWith("network")).cyc(failWith("network")).cyc(failWith("network"))
		s.add(xstep{adv: dur(11 * time.Minute), obs: map[string]snap{"o/r": failWith("http-5xx")}})
		s.add(xstep{adv: dur(11 * time.Minute), obs: map[string]snap{"o/r": ok(mk(1))}})
		s.add(xstep{adv: dur(11 * time.Minute), obs: map[string]snap{"o/r": failWith("network")}})
		add(s)
	}

	{
		s := newXSC("watch-error-every-tenth-failure", "watch-error repeats at the 1st, 10th and 20th failure of an episode.", "health_test.go:TestWatchErrorRepeatsEveryTenthFailure")
		s.lean = true
		s.any(ok(mk(1)))
		s.repeatUntil(xstep{obs: map[string]snap{"o/r": failWith("network")}}, func(_ []xEvent, all []xEvent) bool {
			n := 0
			for _, e := range all {
				if e.Kind == KindWatchError {
					n++
				}
			}
			return n >= 3
		})
		add(s)
	}

	add(newXSC("repository-in-backoff-not-polled", "A repository in backoff is not polled; recovery comes with the first successful poll after it.",
		"health_test.go:TestRepositoryInBackoffIsNotPolled").
		any(ok(mk(1))).cyc(failWith("network"), KindWatchError).cyc(failWith("network")).cyc(ok(mk(1))).cyc(ok(mk(1)), KindWatchRecovered))

	{
		s := newXSC("watch-stalled-once-both-episodes-recover", "watch-stalled fires once per stall; recovery emits watch-recovered for the error episode first, then the stall.",
			"health_test.go:TestWatchStalledOncePerStallAndBothEpisodesRecover")
		s.any(ok(mk(1)))
		s.cycAfter(12*time.Minute, failWith("network"), KindWatchError, KindWatchStalled)
		for i := 0; i < 5; i++ {
			s.any(failWith("network"))
		}
		s.repeatUntil(xstep{obs: map[string]snap{"o/r": ok(mk(1))}}, func(step []xEvent, _ []xEvent) bool { return len(step) > 0 })
		s.total = map[string]int{KindWatchStalled: 1, KindWatchRecovered: 2}
		add(s)
	}

	add(newXSC("run-wide-rate-limit-holds-the-poll", "A run-wide failure (rate-limited) holds the whole poll, is reported without a repo, and recovers without one.",
		"health_test.go:TestRunWideRateLimitHoldsThePollAndRecoversWithoutARepo").
		any(ok(mk(1))).
		add(xstep{adv: dur(0), restart: true, guard: true, want: []string{KindWatchError},
			obs: map[string]snap{"o/r": {err: &ObserveError{Code: "rate-limited", Message: "reset at 10:00", RunWide: true, RetryIn: 5 * time.Minute}}}}).
		add(xstep{adv: dur(2 * time.Minute), guard: true, obs: map[string]snap{"o/r": ok(mk(1), mk(2))}}).
		add(xstep{adv: dur(6 * time.Minute), guard: true, want: []string{KindNewPR, KindWatchRecovered}, obs: map[string]snap{"o/r": ok(mk(1), mk(2))}}))

	add(newXSC("watch-error-message-sanitized-and-bounded", "The error message is sanitized (no control characters) and bounded to 512 bytes.",
		"health_test.go:TestMessageIsSanitizedAndBounded").
		any(ok(mk(1))).
		add(xstep{obs: map[string]snap{"o/r": {err: &ObserveError{Code: "network", Message: "bad\x00\n" + strings.Repeat("é", 600)}}}, guard: true, want: []string{KindWatchError}}))

	// review / replay of lost state
	add(newXSC("lost-state-rebuild-raises-checks-epoch", "A lost state rebuild raises the checks epoch of a baseline PR from the journal so later ids stay unique.",
		"review_test.go:TestLostStateRebuildRaisesTheChecksEpochOfABaselinePR").
		any(ok(mk(1, runs(running())))).any(ok(mk(1, runs(passing())))).cyc(ok(mk(1, runs(passing()))), KindChecksGreen).
		add(xstep{fileOp: "delete-state", obs: map[string]snap{"o/r": ok(mk(1, runs(running())))}}).
		any(ok(mk(1, runs(passing())))).any(ok(mk(1, runs(passing())))))

	firedPrefix := func(s *xsc) {
		s.timeout, s.timeoutSet = 10*time.Minute, true
		s.any(ok(mk(1, runs(running()))))
		for i := 0; i < 11; i++ {
			s.any(ok(mk(1, runs(running()))))
		}
	}
	{
		s := newXSC("timeout-no-refire-after-forced-baseline", "A pending timeout that already fired is not fired again after a forced baseline.",
			"review_test.go:TestTimeoutDoesNotRefireAfterForcedBaseline")
		firedPrefix(s)
		s.add(xstep{baseline: tp(true), restart: true, obs: map[string]snap{"o/r": ok(mk(1, runs(running())))}})
		s.times(13, ok(mk(1, runs(running()))))
		s.total = map[string]int{KindChecksPendingTimout: 1}
		add(s)
	}
	{
		s := newXSC("timeout-no-refire-after-lost-state", "A pending timeout that already fired is not fired again after a lost state.",
			"review_test.go:TestTimeoutDoesNotRefireAfterLostState")
		firedPrefix(s)
		s.add(xstep{fileOp: "delete-state", obs: map[string]snap{"o/r": ok(mk(1, runs(running())))}})
		s.times(13, ok(mk(1, runs(running()))))
		s.total = map[string]int{KindChecksPendingTimout: 1}
		add(s)
	}
	{
		s := newXSC("timeout-no-refire-after-repo-remove-readd", "A pending timeout that already fired is not fired again after the repository was removed and added again.",
			"review_test.go:TestTimeoutDoesNotRefireAfterRepositoryRemoveAndReAdd")
		s.repos = []string{"o/r", "o/x"}
		s.timeout, s.timeoutSet = 10*time.Minute, true
		both := map[string]snap{"o/x": ok(), "o/r": ok(mk(1, runs(running())))}
		s.add(xstep{obs: both})
		for i := 1; i < 12; i++ {
			s.add(xstep{})
		}
		s.add(xstep{repos: []string{"o/x"}, guard: true, want: []string{KindReposChanged}})
		s.add(xstep{repos: []string{"o/r", "o/x"}})
		for i := 1; i < 14; i++ {
			s.add(xstep{})
		}
		s.total = map[string]int{KindChecksPendingTimout: 1}
		add(s)
	}
	{
		s := newXSC("lost-state-keeps-counters-of-unconfigured-repos", "A lost state keeps the epoch counters of repositories that are no longer configured; they return with the next epoch.",
			"review_test.go:TestLostStateKeepsCountersOfRepositoriesNoLongerConfigured")
		s.repos = []string{"o/r", "o/x"}
		s.add(xstep{obs: map[string]snap{"o/x": ok(mk(3, head("aa"))), "o/r": ok()}})
		s.add(xstep{obs: map[string]snap{"o/x": ok(mk(3, head("bb")))}})
		s.add(xstep{fileOp: "delete-state", repos: []string{"o/r"}})
		s.add(xstep{repos: []string{"o/r", "o/x"}})
		s.add(xstep{obs: map[string]snap{"o/x": ok(mk(3, head("aa")))}, guard: true, want: []string{KindHeadChanged}})
		add(s)
	}
	{
		s := newXSC("lost-state-replay-timeout-at-higher-epoch", "The replay of a lost state restores the checks epoch and the fired flag of a timeout journalled at epoch 2.",
			"review_test.go:TestLostStateReplayOfATimeoutAtAHigherEpoch")
		s.timeout, s.timeoutSet = 3*time.Minute, true
		s.any(ok(mk(1, runs(running())))).any(ok(mk(1, runs(passing())))).cyc(ok(mk(1, runs(passing()))), KindChecksGreen)
		s.any(ok(mk(1, runs(running())))).any(ok(mk(1, runs(running()))))
		s.repeatUntil(xstep{obs: map[string]snap{"o/r": ok(mk(1, runs(running())))}}, func(step []xEvent, _ []xEvent) bool { return len(step) > 0 })
		s.total = map[string]int{KindChecksPendingTimout: 1}
		s.add(xstep{action: "open", fileOp: "delete-state"})
		add(s)
	}
	{
		s := newXSC("closed-replay-pins-generation", "The replay records the generation of a closed line so a later reopen gets generation 2.",
			"review_test.go:TestClosedReplayPinsTheGenerationAcrossASecondReopen")
		s.any(ok(mk(5), mk(6)))
		s.cyc(ok(mk(6)).gone(5, closedGone(h("51"))), KindClosed).cyc(ok(mk(5), mk(6)), KindNewPR)
		s.add(xstep{crash: &xcrash{"append", 1}, obs: map[string]snap{"o/r": ok(mk(6)).gone(5, closedGone(h("51")))}})
		s.add(xstep{action: "open"})
		s.cyc(ok(mk(5), mk(6)), KindNewPR)
		add(s)
	}
	{
		s := newXSC("fork-flag-travels-in-new-pr-and-replay", "The fork flag travels in the new-pr event and is restored by the replay after a crash.",
			"review_test.go:TestForkFlagTravelsInTheNewPREventAndIsRestoredByReplay")
		fork := func(p *PRObs) { p.Fork = true }
		s.any(ok())
		s.add(xstep{crash: &xcrash{"append", 1}, obs: map[string]snap{"o/r": ok(mk(7, fork))}})
		s.add(xstep{action: "open"})
		add(s)
	}

	// status and fork
	{
		s := newXSC("status-fresh-stale-after-a-failing-cycle", "The heartbeat freshness window (3 minutes at a 60 s interval) and the per-repository status after a failing cycle.",
			"status_test.go:TestReadStatusFreshStaleAndMissing")
		s.any(ok(mk(1))).any(failWith("network"))
		s.status = []time.Duration{100 * time.Second, 179 * time.Second, 181 * time.Second}
		add(s)
	}
	add(newXSC("unknown-fork-never-overwrites-persisted", "A fork flag read without pulls/N is not an answer: it never overwrites a persisted fork flag, also not through a baseline.",
		"status_test.go:TestUnknownForkNeverOverwritesAPersistedFork").
		any(ok(mk(1))).
		any(ok(mk(1, func(p *PRObs) { p.Fork, p.ForkKnown = true, true }))).
		any(ok(mk(1))).
		any(ok(mk(1, func(p *PRObs) { p.ForkKnown = true }))).
		any(ok(mk(1, func(p *PRObs) { p.Fork, p.ForkKnown = true, true }))).
		any(ok(mk(1))).
		add(xstep{baseline: tp(true), restart: true, obs: map[string]snap{"o/r": ok(mk(1))}}).
		any(ok(mk(1))))

	// observer hints (candidate_test.go)
	{
		clean := ok(mk(1, detail(tt(), "clean"), runs(passing())))
		add(newXSC("stored-candidates-visible-in-hints", "A stored class candidate and a stored mergeability candidate show in the hints for the observer (detail read due).",
			"candidate_test.go:TestStoredCandidatesAreVisibleToTheObserver").
			any(clean).any(ok(mk(1, detail(ff(), "dirty"), runs(failing("x", ""))))).any(clean))
	}
	add(newXSC("null-mergeable-keeps-detail-due", "A confirming read that finds mergeability still being computed keeps the detail read due until a non-null read resolves the candidate.",
		"candidate_test.go:TestNullMergeableKeepsTheDetailReadDueWhileACandidateIsStored").
		any(ok(mk(1, detail(tt(), "clean")))).any(ok(mk(1, detail(ff(), "dirty")))).any(ok(mk(1, detail(nil, "unknown")))).any(ok(mk(1, detail(nil, "unknown")))).
		cyc(ok(mk(1, detail(ff(), "dirty"))), KindMergeableChanged).any(ok(mk(1, detail(ff(), "dirty")))))

	_ = fmt.Sprint
	return out
}
