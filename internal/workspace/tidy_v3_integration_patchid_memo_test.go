// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// Spec 031 FR-003 and FR-008: the in-run memo of base-side patch-ids. The ids,
// their order and every bound are those of the range stream; only the
// recomputation of a commit already seen in the run (same commit, same pinned
// attribute source) is saved.

// fiScript builds fast-import input for the memo history.
type fiScript struct{ b strings.Builder }

func fiFile(mode, path, content string) string {
	return fmt.Sprintf("M %s inline %s\ndata %d\n%s\n", mode, path, len(content), content)
}

func (s *fiScript) commit(ref string, mark int, when int64, from, merge string, ops ...string) {
	fmt.Fprintf(&s.b, "commit %s\nmark :%d\ncommitter T <t@t> %d +0000\ndata 2\nc\n", ref, mark, when)
	if from != "" {
		fmt.Fprintf(&s.b, "from %s\n", from)
	}
	if merge != "" {
		fmt.Fprintf(&s.b, "merge %s\n", merge)
	}
	for _, op := range ops {
		s.b.WriteString(op)
	}
	s.b.WriteString("\n")
}

// memoKind is the kind of main commit i of the memo history.
func memoKind(i int) string {
	switch {
	case i%7 == 0:
		return "empty"
	case i%5 == 0:
		return "binary"
	case i%13 == 0:
		return "mode"
	case i%11 == 0 && memoKind(i-1) == "text":
		return "rename"
	}
	return "text"
}

func memoWhen(i int) int64 {
	when := int64(1700000000 + 60*i)
	if i%9 == 0 {
		when -= 7200 // a clock that runs behind its parent
	}
	return when
}

const memoMain = 48

// memoHistory imports a main branch of 48 commits with empty commits, binary
// files, mode changes, renames, two merges and a skewed clock, plus topic
// branches forked at different points (different merge bases against main).
// It returns the topic names.
func (f *proofFix) memoHistory() []string {
	f.t.Helper()
	main := "refs/heads/" + f.main
	var s fiScript
	text := func(i int, trailing string) string {
		return fiFile("100644", fmt.Sprintf("p%d", i), fmt.Sprintf("content %d%s\n", i, trailing))
	}
	for i := 1; i <= memoMain; i++ {
		from, merge := "", ""
		if i == 1 {
			from = f.rev(f.main)
		}
		var ops []string
		switch memoKind(i) {
		case "text":
			ops = append(ops, text(i, ""))
		case "binary":
			ops = append(ops, fiFile("100644", "bin.dat", fmt.Sprintf("\x00\x01bin %d\x00\xff\n", i)))
		case "mode":
			mode := "100755"
			if i%26 == 0 {
				mode = "100644"
			}
			ops = append(ops, fiFile(mode, "exec.sh", "run\n"))
		case "rename":
			ops = append(ops, fmt.Sprintf("R p%d q%d\n", i-1, i))
		}
		switch i {
		case 10:
			// The side branches are forked after their parent exists.
		case 24:
			merge = ":102"
		case 36:
			merge = ":103"
		}
		if i == 24 {
			s.commit("refs/heads/side1", 101, memoWhen(100), ":10", "", fiFile("100644", "side1", "one\n"))
			s.commit("refs/heads/side1", 102, memoWhen(101), "", "", fiFile("100644", "side2", "two\n"))
		}
		if i == 36 {
			s.commit("refs/heads/side2", 103, memoWhen(102), ":30", "", fiFile("100644", "side3", "three\n"))
		}
		s.commit(main, i, memoWhen(i), from, merge, ops...)
	}
	var tips []string
	for k, fork := range []int{5, 12, 18, 25, 33, 40} {
		name := fmt.Sprintf("t%d", k)
		ref := "refs/heads/" + name
		mark := 1000 + 20*k
		first, spaced := true, false
		add := func(when int64, merge string, ops ...string) {
			from := ""
			if first {
				from = fmt.Sprintf(":%d", fork)
			}
			first = false
			s.commit(ref, mark, when, from, merge, ops...)
			mark++
		}
		for j := fork + 1; j <= min(fork+6, memoMain); j++ {
			if memoKind(j) != "text" {
				continue
			}
			trailing := ""
			if k%2 == 1 && !spaced {
				trailing, spaced = " ", true // a whitespace-only difference to the main commit
			}
			add(memoWhen(j)+5, "", text(j, trailing))
		}
		add(memoWhen(49), "")
		add(memoWhen(50), "", fiFile("100644", fmt.Sprintf("own%d", k), "own\n"))
		if k == 3 {
			add(memoWhen(51), ":102")
		}
		tips = append(tips, name)
	}
	// A branch with many commits.
	for n := 0; n < 120; n++ {
		from := ""
		if n == 0 {
			from = ":1"
		}
		s.commit("refs/heads/many", 2000+n, memoWhen(n)+1, from, "", fiFile("100644", fmt.Sprintf("m%d", n), fmt.Sprintf("many %d\n", n)))
	}
	tips = append(tips, "many")
	f.fastImport(s.b.String())
	return tips
}

// memoRun builds a run like verdictWith does, with the memo enabled or not.
func (f *proofFix) memoRun(memo bool, hook func(integrationCommand)) (context.Context, *integrationRun) {
	f.t.Helper()
	ctx, _, closeSession, err := withPlanningSession(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(closeSession)
	observation, err := observeTidyV3Git(ctx, f.dir, f.rev(f.main))
	if err != nil {
		f.t.Fatalf("observe: %v", err)
	}
	engine := defaultIntegrationEngine()
	engine.command = hook
	run, cleanup, err := engine.begin(ctx, observation.Expected.Identity, observation.ConfigNotes)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(cleanup)
	run.ensureObjectDir()
	if f.bin != "" {
		run.bin = f.bin
	}
	if memo {
		run.enablePatchMemo()
	}
	return ctx, run
}

// rangeOf resolves what patchID resolves for the tip: merge base, trees, range.
func (f *proofFix) rangeOf(ctx context.Context, run *integrationRun, base, tip string) (mb, tipTree, baseTree, spec string) {
	f.t.Helper()
	bases, err := run.mergeBases(ctx, base, tip)
	if err != nil || len(bases) != 1 {
		f.t.Fatalf("merge base of %s: %v %v", tip, bases, err)
	}
	tipTree, _, baseTree, err = run.trees(ctx, tip, bases[0], base)
	if err != nil {
		f.t.Fatal(err)
	}
	return bases[0], tipTree, baseTree, bases[0] + ".." + base
}

func orderOf(names []string, reverse bool) []string {
	out := append([]string{}, names...)
	if reverse {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out
}

// The memo returns exactly the rows of the range stream: same ids, same order,
// merges skipped and empty patches absent, for every tip, in either order of
// evaluation (a later tip with an older merge base reuses commits of an
// earlier one and the other way around).
func TestPatchMemoRowsEqualTheRangeStreams(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%t", reverse), func(t *testing.T) {
			f := newProofFix(t)
			tips := f.memoHistory()
			ctx, run := f.memoRun(true, nil)
			base := f.rev(f.main)
			for _, tip := range orderOf(tips, reverse) {
				_, _, baseTree, spec := f.rangeOf(ctx, run, base, f.rev(tip))
				wantStable, wantVerbatim, err := run.patchIDs(ctx, baseTree, spec)
				if err != nil {
					t.Fatal(err)
				}
				count, err := run.count(ctx, "rev-list", "--count", "--no-merges", spec, "--")
				if err != nil {
					t.Fatal(err)
				}
				gotStable, gotVerbatim, err := run.basePatchIDs(ctx, baseTree, spec, count)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(gotStable, wantStable) || !reflect.DeepEqual(gotVerbatim, wantVerbatim) {
					t.Fatalf("%s: memo rows differ from the range stream\nstable %v\nwant   %v\nverbatim %v\nwant     %v", tip, gotStable, wantStable, gotVerbatim, wantVerbatim)
				}
				if tip == "t0" && len(wantStable) >= count {
					t.Fatalf("the fixture has no empty patch on the base side: %d rows for %d commits", len(wantStable), count)
				}
			}
			if run.patchMemo.size() == 0 {
				t.Fatal("the memo stayed empty")
			}
		})
	}
}

// The recorded stream bytes of the commits add up to the range stream, so the
// 64 MiB bound of a tip is judged on the same number as before.
func TestPatchMemoByteCountsAddUpToTheRangeStream(t *testing.T) {
	f := newProofFix(t)
	tips := f.memoHistory()
	ctx, run := f.memoRun(true, nil)
	base := f.rev(f.main)
	for _, tip := range tips {
		_, _, baseTree, spec := f.rangeOf(ctx, run, base, f.rev(tip))
		count, _ := run.count(ctx, "rev-list", "--count", "--no-merges", spec, "--")
		if _, _, err := run.basePatchIDs(ctx, baseTree, spec, count); err != nil {
			t.Fatal(err)
		}
		rawLen := f.streamBytes(ctx, run, baseTree, spec)
		pin := strings.Join(run.session.attrSource(baseTree), "\x00")
		var sum int64
		for _, c := range strings.Fields(f.git("rev-list", "--no-merges", spec)) {
			e, ok := run.patchMemo.get(patchMemoKey{pin: pin, commit: c})
			if !ok {
				t.Fatalf("%s: commit %s not memoized", tip, c)
			}
			sum += e.bytes
		}
		if sum != rawLen {
			t.Fatalf("%s: commit segments add up to %d bytes, the range stream has %d", tip, sum, rawLen)
		}
	}
}

// The whole verdict of every tip, including the per-commit CommitMatches and
// BaseMatches of the report, is identical with and without the memo.
func TestPatchMemoVerdictsAreIdentical(t *testing.T) {
	f := newProofFix(t)
	tips := f.memoHistory()
	base := f.rev(f.main)
	for _, accept := range []bool{false, true} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("accept=%t/reverse=%t", accept, reverse), func(t *testing.T) {
				ctx, plain := f.memoRun(false, nil)
				_, memo := f.memoRun(true, nil)
				for _, tip := range orderOf(tips, reverse) {
					oid := f.rev(tip)
					want, err := plain.evaluate(ctx, base, oid, V3ProofPolicy{Version: 1, Kinds: allProofs.Kinds, AcceptWhitespaceDifferences: accept})
					if err != nil {
						t.Fatal(err)
					}
					got, err := memo.evaluate(ctx, base, oid, V3ProofPolicy{Version: 1, Kinds: allProofs.Kinds, AcceptWhitespaceDifferences: accept})
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("%s: verdict with the memo %+v, without %+v", tip, got, want)
					}
					mb, tipTree, baseTree, _ := f.rangeOf(ctx, plain, base, oid)
					wantRes, err1 := plain.patchID(ctx, base, oid, mb, tipTree, baseTree)
					gotRes, err2 := memo.patchID(ctx, base, oid, mb, tipTree, baseTree)
					if err1 != nil || err2 != nil || !reflect.DeepEqual(gotRes, wantRes) {
						t.Fatalf("%s: patchID with the memo %+v %v, without %+v %v", tip, gotRes, err2, wantRes, err1)
					}
				}
			})
		}
	}
}

// The fixture reaches the interesting classes: whitespace-only differences,
// matches, partial matches, merges in the range and empty patches.
func TestPatchMemoHistoryCoversTheEdgeCases(t *testing.T) {
	f := newProofFix(t)
	tips := f.memoHistory()
	ctx, run := f.memoRun(true, nil)
	base := f.rev(f.main)
	var whitespace, matched, empty, merges, binary bool
	for _, tip := range tips {
		v, err := run.evaluate(ctx, base, f.rev(tip), patchPolicy(false))
		if err != nil {
			t.Fatal(err)
		}
		whitespace = whitespace || contains(v.Reasons, "whitespace-differs")
		matched = matched || v.Matched > 0
		empty = empty || v.Empty > 0
		merges = merges || v.SkippedMerges > 0
		binary = binary || len(v.BinaryPaths) > 0
	}
	if !whitespace || !matched || !empty || !merges {
		t.Fatalf("the fixture misses a case: whitespace=%t matched=%t empty=%t merges=%t binary=%t", whitespace, matched, empty, merges, binary)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// stdinCommits is the list of commits a `log --stdin` command was given.
func stdinCommits(c integrationCommand) []string {
	r, ok := c.stdin.(*bytes.Reader)
	if !ok {
		return nil
	}
	b := make([]byte, r.Size())
	if _, err := r.ReadAt(b, 0); err != nil && len(b) > 0 {
		return nil
	}
	return strings.Fields(string(b))
}

func isMemoStream(c integrationCommand) bool {
	return len(c.args) > 0 && c.args[0] == "log" && contains(c.args, "--stdin")
}

// Each distinct (commit, pinned attribute source) is computed once per run,
// however many tips share it, and a run without the memo computes the same
// commits again and again.
func TestPatchMemoComputesEachDistinctCommitOncePerRun(t *testing.T) {
	f := newProofFix(t)
	tips := f.memoHistory()
	base := f.rev(f.main)
	var computed, rangeStreams []string
	ctx, memo := f.memoRun(true, func(c integrationCommand) {
		if isMemoStream(c) {
			computed = append(computed, stdinCommits(c)...)
		}
	})
	_, plain := f.memoRun(false, func(c integrationCommand) {
		if len(c.args) > 0 && c.args[0] == "log" && contains(c.args, "-p") && c.attrTree != "" {
			rangeStreams = append(rangeStreams, c.args[len(c.args)-2])
		}
	})
	distinct := map[string]bool{}
	for _, tip := range tips {
		oid := f.rev(tip)
		if _, err := memo.evaluate(ctx, base, oid, patchPolicy(false)); err != nil {
			t.Fatal(err)
		}
		if _, err := plain.evaluate(ctx, base, oid, patchPolicy(false)); err != nil {
			t.Fatal(err)
		}
		_, _, _, spec := f.rangeOf(ctx, plain, base, oid)
		for _, c := range strings.Fields(f.git("rev-list", "--no-merges", spec)) {
			distinct[c] = true
		}
	}
	seen := map[string]int{}
	for _, c := range computed {
		seen[c]++
		if seen[c] > 1 {
			t.Fatalf("commit %s computed %d times in one run", c, seen[c])
		}
	}
	if len(seen) != len(distinct) || memo.patchMemo.size() != len(distinct) {
		t.Fatalf("computed %d commits, memo holds %d, the ranges hold %d distinct", len(seen), memo.patchMemo.size(), len(distinct))
	}
	if len(rangeStreams) < len(tips)*2 {
		t.Fatalf("the plain run streamed %d ranges for %d tips", len(rangeStreams), len(tips))
	}
}

// A different pinned attribute source is a different key: the same commits are
// computed again for another base tree, and the rows still equal the range's.
func TestPatchMemoKeyIncludesThePinnedAttributeSource(t *testing.T) {
	f := newProofFix(t)
	f.memoHistory()
	var computed []string
	ctx, run := f.memoRun(true, func(c integrationCommand) {
		if isMemoStream(c) {
			computed = append(computed, stdinCommits(c)...)
		}
	})
	tip := f.rev("t0")
	for _, base := range []string{f.rev(f.main), f.rev(f.main + "~8")} {
		mb, _, baseTree, spec := f.rangeOf(ctx, run, base, tip)
		_ = mb
		count, _ := run.count(ctx, "rev-list", "--count", "--no-merges", spec, "--")
		wantS, wantV, err := run.patchIDs(ctx, baseTree, spec)
		if err != nil {
			t.Fatal(err)
		}
		gotS, gotV, err := run.basePatchIDs(ctx, baseTree, spec, count)
		if err != nil || !reflect.DeepEqual(gotS, wantS) || !reflect.DeepEqual(gotV, wantV) {
			t.Fatalf("base %s: rows differ: %v", base, err)
		}
	}
	seen := map[string]int{}
	for _, c := range computed {
		seen[c]++
	}
	again := 0
	for _, n := range seen {
		if n == 2 {
			again++
		}
		if n > 2 {
			t.Fatalf("a commit computed %d times for two pins", n)
		}
	}
	if again == 0 {
		t.Fatal("two base trees shared one memo entry")
	}
	// The same pin computed again changes nothing.
	before := len(computed)
	mb, _, baseTree, spec := f.rangeOf(ctx, run, f.rev(f.main), tip)
	_ = mb
	count, _ := run.count(ctx, "rev-list", "--count", "--no-merges", spec, "--")
	if _, _, err := run.basePatchIDs(ctx, baseTree, spec, count); err != nil || len(computed) != before {
		t.Fatalf("a repeated range computed %d more commits: %v", len(computed)-before, err)
	}
}

func realGit(t *testing.T) string {
	t.Helper()
	real, err := gitExecutable()
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// failingGit is Git except that every command given --stdin runs the fragment.
func failingGit(t *testing.T, fragment string) string {
	t.Helper()
	real, err := gitExecutable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(gitTempDir(t), "git")
	script := "#!/bin/sh\nfor a in \"$@\"; do if [ \"$a\" = --stdin ]; then " + fragment + "; fi; done\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// An error, a truncated stream and a cancellation are never stored as results;
// the same range computed afterwards by a working Git gives the range's rows.
func TestPatchMemoNeverStoresAFailedOrInterruptedComputation(t *testing.T) {
	cases := map[string]string{
		"exit status":      "exit 3",
		"truncated stream": "\"" + realGit(t) + "\" \"$@\" | head -c 200; exit 0",
	}
	for name, fragment := range cases {
		t.Run(name, func(t *testing.T) {
			f := newProofFix(t)
			f.memoHistory()
			f.bin = failingGit(t, fragment)
			ctx, run := f.memoRun(true, nil)
			base, tip := f.rev(f.main), f.rev("t0")
			mb, tipTree, baseTree, spec := f.rangeOf(ctx, run, base, tip)
			count, _ := run.count(ctx, "rev-list", "--count", "--no-merges", spec, "--")
			_, _, err := run.basePatchIDs(ctx, baseTree, spec, count)
			if run.patchMemo.size() != 0 {
				t.Fatalf("a failed computation left %d entries (error %v)", run.patchMemo.size(), err)
			}
			// With the failing Git the verdict is the range stream's or a limit, never a partial proof.
			res, err := run.patchID(ctx, base, tip, mb, tipTree, baseTree)
			if err != nil {
				t.Fatal(err)
			}
			run.bin = realGit(t)
			ref, err := f.refRun().patchID(ctx, base, tip, mb, tipTree, baseTree)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(res, ref) && !(res.Limit != nil && !res.Proven && reflect.DeepEqual(res.Reasons, []string{"proof-limit"})) {
				t.Fatalf("a failed Git gave %+v, the range stream %+v", res, ref)
			}
			got, err := run.patchID(ctx, base, tip, mb, tipTree, baseTree)
			if err != nil || !reflect.DeepEqual(got, ref) {
				t.Fatalf("after the failure: %+v %v, want %+v", got, err, ref)
			}
			if run.patchMemo.size() == 0 {
				t.Fatal("the working Git stored nothing")
			}
		})
	}
}

// refRun is a run without the memo on the same repository.
func (f *proofFix) refRun() *integrationRun {
	f.t.Helper()
	saved := f.bin
	f.bin = ""
	_, run := f.memoRun(false, nil)
	f.bin = saved
	return run
}

func TestPatchMemoStoresNothingWhenTheOperationIsCanceled(t *testing.T) {
	f := newProofFix(t)
	f.memoHistory()
	cancelCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, run := f.memoRun(true, func(c integrationCommand) {
		if isMemoStream(c) {
			cancel()
		}
	})
	base, tip := f.rev(f.main), f.rev("t0")
	_, _, baseTree, spec := f.rangeOf(cancelCtx, run, base, tip)
	count, _ := run.count(cancelCtx, "rev-list", "--count", "--no-merges", spec, "--")
	_, _, err := run.basePatchIDs(cancelCtx, baseTree, spec, count)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v, want the cancellation", err)
	}
	if n := run.patchMemo.size(); n != 0 {
		t.Fatalf("a canceled computation left %d entries", n)
	}
}

// An expired phase budget stops a tip with the budget error, as before.
func TestPatchMemoHonorsTheProofBudget(t *testing.T) {
	f := newProofFix(t)
	f.memoHistory()
	ctx, run := f.memoRun(true, nil)
	base, tip := f.rev(f.main), f.rev("t0")
	_, _, baseTree, spec := f.rangeOf(ctx, run, base, tip)
	count, _ := run.count(ctx, "rev-list", "--count", "--no-merges", spec, "--")
	if _, _, err := run.basePatchIDs(ctx, baseTree, spec, count); err != nil {
		t.Fatal(err)
	}
	// All commits are memoized now; an exhausted budget must still stop the tip.
	now := run.engine.now()
	run.engine.now = func() time.Time { return now.Add(time.Hour) }
	run.phaseStart, run.phaseStarted, run.tipStart = now, true, now
	if _, _, err := run.basePatchIDs(ctx, baseTree, spec, count); !errors.Is(err, errIntegrationBudget) {
		t.Fatalf("error %v, want the budget error", err)
	}
}

// The 64 MiB bound is judged on the whole range stream: commits that were
// memoized by an earlier tip still count toward it.
func TestPatchMemoKeepsTheStreamBoundOfTheWholeRange(t *testing.T) {
	f := newProofFix(t)
	f.memoHistory()
	base := f.rev(f.main)
	ctx, plain := f.memoRun(false, nil)
	_, memo := f.memoRun(true, nil)
	size := func(tip string) int {
		_, _, baseTree, spec := f.rangeOf(ctx, plain, base, f.rev(tip))
		return int(f.streamBytes(ctx, plain, baseTree, spec))
	}
	small, large := "t5", "t0"
	if size(small) >= size(large) {
		t.Fatalf("fixture: %d bytes for %s, %d for %s", size(small), small, size(large), large)
	}
	limit := int64(size(large) - 1)
	plain.patchStreamLimit, memo.patchStreamLimit = limit, limit
	for _, tip := range []string{small, large} {
		oid := f.rev(tip)
		mb, tipTree, baseTree, _ := f.rangeOf(ctx, plain, base, oid)
		want, err1 := plain.patchID(ctx, base, oid, mb, tipTree, baseTree)
		got, err2 := memo.patchID(ctx, base, oid, mb, tipTree, baseTree)
		if err1 != nil || err2 != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: with the memo %+v %v, without %+v %v", tip, got, err2, want, err1)
		}
		if tip == large && (want.Limit == nil || want.Limit.Unit != "bytes" || !reflect.DeepEqual(got.Reasons, []string{"proof-limit"})) {
			t.Fatalf("the large range must hit the bound: %+v", want)
		}
		if tip == small && want.Limit != nil {
			t.Fatalf("the small range must fit: %+v", want)
		}
	}
}

// The memo is bounded: it never holds more entries than its cap.
func TestPatchMemoIsBounded(t *testing.T) {
	m := newPatchIDMemo()
	for i := 0; i < patchMemoMaxEntries+100; i++ {
		m.put(patchMemoKey{pin: "p", commit: fmt.Sprintf("%040d", i)}, patchMemoEntry{bytes: 1})
	}
	if m.size() != patchMemoMaxEntries {
		t.Fatalf("size %d, want the cap %d", m.size(), patchMemoMaxEntries)
	}
	if _, ok := m.get(patchMemoKey{pin: "p", commit: fmt.Sprintf("%040d", 0)}); !ok {
		t.Fatal("an early entry was lost")
	}
	if _, ok := m.get(patchMemoKey{pin: "p", commit: fmt.Sprintf("%040d", patchMemoMaxEntries+1)}); ok {
		t.Fatal("an entry beyond the cap was stored")
	}
	// An existing key is never replaced.
	m.put(patchMemoKey{pin: "p", commit: fmt.Sprintf("%040d", 0)}, patchMemoEntry{bytes: 99})
	if e, _ := m.get(patchMemoKey{pin: "p", commit: fmt.Sprintf("%040d", 0)}); e.bytes != 1 {
		t.Fatal("an entry was replaced")
	}
}

// Concurrent tips of one run share the memo without a race (run with -race).
func TestPatchMemoIsSafeForConcurrentTips(t *testing.T) {
	f := newProofFix(t)
	tips := f.memoHistory()
	base := f.rev(f.main)
	ctx, run := f.memoRun(true, nil)
	_, plain := f.memoRun(false, nil)
	run.phaseStart, run.phaseStarted = run.engine.now(), true
	run.reporting = true // no per-tip clock is shared between the goroutines
	type result struct {
		v   integrationVerdict
		err error
	}
	out := make([]result, len(tips))
	done := make(chan int)
	for i, tip := range tips {
		go func() {
			tipRun := *run
			v, err := tipRun.evaluate(ctx, base, f.rev(tip), patchPolicy(false))
			out[i] = result{v, err}
			done <- i
		}()
	}
	for range tips {
		<-done
	}
	for i, tip := range tips {
		want, err := plain.evaluate(ctx, base, f.rev(tip), patchPolicy(false))
		if err != nil || out[i].err != nil || !reflect.DeepEqual(out[i].v, want) {
			t.Fatalf("%s: concurrent %+v %v, sequential %+v %v", tip, out[i].v, out[i].err, want, err)
		}
	}
}

// The plan computes every distinct base commit once for tips that share a base
// range; apply never owns a memo and recomputes as before.
func TestPlanUsesTheMemoAndApplyDoesNot(t *testing.T) {
	f := classFix(t)
	var planned []string
	c := defaultTidyV3RefsCoordinator()
	c.integration.command = func(ic integrationCommand) {
		if isMemoStream(ic) {
			planned = append(planned, stdinCommits(ic)...)
		}
	}
	plan := f.planWith(c, V3PlanOptions{Refs: []string{"refs/heads/rebased"}, IntegrationProofs: []string{"patch-id"}})
	if plan.Status != "ok" || candidateByName(t, plan.Plan, "rebased").Decision != "delete" {
		t.Fatalf("plan: %+v", plan.Diagnostics)
	}
	if len(planned) == 0 {
		t.Fatal("the plan did not use the memo")
	}
	seen := map[string]bool{}
	for _, oid := range planned {
		if seen[oid] {
			t.Fatalf("plan computed %s twice", oid)
		}
		seen[oid] = true
	}

	var applied []string
	var memoSeen []bool
	a := defaultTidyV3RefsApplyCoordinator()
	a.refs.integration.command = func(ic integrationCommand) {
		if isMemoStream(ic) {
			applied = append(applied, stdinCommits(ic)...)
		}
	}
	a.refs.proofRunner = func(ctx context.Context, run *integrationRun, base, tip string, policy V3ProofPolicy) (integrationVerdict, error) {
		memoSeen = append(memoSeen, run.patchMemo != nil)
		return run.evaluate(ctx, base, tip, policy)
	}
	res := a.apply(context.Background(), V3ApplyOptions{StateDir: plan.Plan.StateDir, PlanID: plan.Plan.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if res.Status != "ok" {
		t.Fatalf("apply failed: %+v", res.Diagnostics)
	}
	if len(memoSeen) == 0 {
		t.Fatal("apply did not re-evaluate the proof")
	}
	for _, has := range memoSeen {
		if has {
			t.Fatal("apply ran with a patch-id memo")
		}
	}
	if len(applied) != 0 {
		t.Fatalf("apply used the memo stream for %v", applied)
	}
}

// Every other run starts without a memo.
func TestRunsStartWithoutAMemo(t *testing.T) {
	f := newProofFix(t)
	_, run := f.memoRun(false, nil)
	if run.patchMemo != nil {
		t.Fatal("begin must not enable the memo")
	}
}

// patchSegmenter attributes every byte of the stream to the commit whose
// header line precedes it, whatever the chunking of the writes.
func TestPatchSegmenterSizesAreIndependentOfChunking(t *testing.T) {
	a, b, c := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	stream := a + "\n\ndiff --git x y\n+" + b + "\n" + b + "\n" + c + "\n"
	for _, chunk := range []int{1, 2, 7, 41, 1000} {
		s := newPatchSegmenter(40, []string{a, b, c})
		for i := 0; i < len(stream); i += chunk {
			s.Write([]byte(stream[i:min(len(stream), i+chunk)]))
		}
		sizes, ok := s.finish()
		want := []int64{int64(len(a + "\n\ndiff --git x y\n+" + b + "\n")), 41, 41}
		if !ok || !reflect.DeepEqual(sizes, want) {
			t.Fatalf("chunk %d: %v %t, want %v", chunk, sizes, ok, want)
		}
	}
	bad := map[string]string{
		"a missing header":     a + "\n" + c + "\n",
		"data before a header": "x\n" + a + "\n" + b + "\n" + c + "\n",
		"headers out of order": b + "\n" + a + "\n" + c + "\n",
		"an extra header":      a + "\n" + b + "\n" + c + "\n" + c + "\n",
	}
	names := make([]string, 0, len(bad))
	for name := range bad {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		s := newPatchSegmenter(40, []string{a, b, c})
		s.Write([]byte(bad[name]))
		if _, ok := s.finish(); ok {
			t.Errorf("%s was accepted", name)
		}
	}
}

// The worktree observation proves its tips against one base in one run: they
// share the memo, so a commit of the base side is computed once (the tips are
// read one after the other here; concurrent tips may compute a commit that is
// still in flight in another tip, never one that has finished).
func TestObserveWorktreesShareTheMemoAcrossTips(t *testing.T) {
	repo := observeRepo(t)
	topics := make([]string, 3)
	for i := range topics {
		topics[i] = observeAdd(t, repo, fmt.Sprintf("topic%d", i))
		commitIn(t, topics[i], fmt.Sprintf("own%d", i), "own\n")
	}
	for i := 0; i < 5; i++ {
		commitIn(t, repo, fmt.Sprintf("main%d", i), "main\n")
	}
	var mu sync.Mutex
	var computed []string
	obs := defaultWorktreeObserver()
	obs.refs.integration.command = func(c integrationCommand) {
		if isMemoStream(c) {
			mu.Lock()
			computed = append(computed, stdinCommits(c)...)
			mu.Unlock()
		}
	}
	if _, err := obs.observe(context.Background(), ObserveOptions{Path: repo, Base: observeBase, Width: 1}); err != nil {
		t.Fatal(err)
	}
	if len(computed) == 0 {
		t.Fatal("the observation did not use the memo")
	}
	seen := map[string]bool{}
	for _, c := range computed {
		if seen[c] {
			t.Fatalf("commit %s computed twice in one observation", c)
		}
		seen[c] = true
	}
}

// countWriter counts the bytes of a stream.
type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// streamBytes is the size of the range stream under the production session,
// environment and pinned attribute source.
func (f *proofFix) streamBytes(ctx context.Context, run *integrationRun, baseTree, spec string) int64 {
	f.t.Helper()
	var n countWriter
	if _, _, err := run.patchStreams(ctx, integrationCommand{attrTree: baseTree, args: logPatchArgs(spec), limit: 1 << 40}, &n); err != nil {
		f.t.Fatal(err)
	}
	return n.n
}

// shortListGit is Git except that `rev-list` without --count loses its first line.
func shortListGit(t *testing.T) string {
	t.Helper()
	real := realGit(t)
	path := filepath.Join(gitTempDir(t), "git")
	script := "#!/bin/sh\nlist=0; count=0\nfor a in \"$@\"; do [ \"$a\" = rev-list ] && list=1; [ \"$a\" = --count ] && count=1; done\nif [ $list = 1 ] && [ $count = 0 ]; then \"" + real + "\" \"$@\" | tail -n +2; exit 0; fi\nexec \"" + real + "\" \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAssignPatchRowsRefusesUnexpectedAndDuplicateRows(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	p1, p2 := strings.Repeat("1", 40), strings.Repeat("2", 40)
	row := func(p, c string) patchIDRow { return patchIDRow{patch: p, commit: c} }
	if out, ok, _ := assignPatchRows([]string{a, b}, []int64{1, 2}, []patchIDRow{row(p1, a)}, []patchIDRow{row(p2, a)}); !ok || !out[0].hasStable || out[1].hasStable || out[1].bytes != 2 {
		t.Fatalf("a good stream was refused: %+v %t", out, ok)
	}
	bad := map[string][2][]patchIDRow{
		"duplicate stable":   {{row(p1, a), row(p1, a)}, nil},
		"duplicate verbatim": {nil, {row(p1, b), row(p2, b)}},
		"unknown stable":     {{row(p1, strings.Repeat("c", 40))}, nil},
		"unknown verbatim":   {nil, {row(p1, strings.Repeat("c", 40))}},
	}
	for name, rows := range bad {
		if _, ok, _ := assignPatchRows([]string{a, b}, []int64{1, 2}, rows[0], rows[1]); ok {
			t.Errorf("%s was accepted", name)
		}
	}
}

// A range list that does not match the count falls back to the range stream
// and stores nothing.
func TestPatchMemoFallsBackWhenTheRangeListDisagreesWithTheCount(t *testing.T) {
	f := newProofFix(t)
	f.memoHistory()
	f.bin = shortListGit(t)
	ctx, run := f.memoRun(true, nil)
	_, _, baseTree, spec := f.rangeOf(ctx, run, f.rev(f.main), f.rev("t0"))
	count, err := run.count(ctx, "rev-list", "--count", "--no-merges", spec, "--")
	if err != nil {
		t.Fatal(err)
	}
	want, wantV, err := f.refRun().patchIDs(ctx, baseTree, spec)
	if err != nil {
		t.Fatal(err)
	}
	got, gotV, err := run.basePatchIDs(ctx, baseTree, spec, count)
	if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotV, wantV) || run.patchMemo.size() != 0 {
		t.Fatalf("rows differ or the memo holds %d entries: %v", run.patchMemo.size(), err)
	}
}
