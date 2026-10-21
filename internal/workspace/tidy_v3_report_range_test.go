// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Spec 031 T031: consolidation ranges, range-diff pairing and its limits.

// rangeFixture has a branch of three commits and a named range on the base
// holding an identical copy of the first, a changed copy of the second and one
// commit the branch does not have.
func rangeFixture(t *testing.T) (*proofFix, string) { return rangeFixtureWith(t, "") }

// rangeFixtureWith commits the given .gitattributes before the branch point.
func rangeFixtureWith(t *testing.T, attributes string) (*proofFix, string) {
	f := newProofFix(t)
	if attributes != "" {
		f.commit("attributes", map[string]string{".gitattributes": attributes})
	}
	lines := func(first, last string) string {
		out := first + "\n"
		for i := 2; i < 20; i++ {
			out += "line\n"
		}
		return out + last + "\n"
	}
	f.commit("b file", map[string]string{"b.txt": lines("b1", "b20")})
	m0 := f.rev("HEAD")
	f.branch("topic", m0)
	c1 := f.commit("one", map[string]string{"a.txt": "a\n"})
	f.commit("two", map[string]string{"b.txt": lines("B1", "b20")})
	f.commit("three", map[string]string{"c.txt": "c\n"})
	f.checkout(f.main)
	f.commit("main moves", map[string]string{"o.txt": "o\n"})
	start := f.rev("HEAD")
	f.git("cherry-pick", c1)
	f.commit("two, reworked", map[string]string{"b.txt": lines("B1", "B20")})
	f.commit("extra", map[string]string{"e.txt": "e\n"})
	return f, start + ".." + f.rev("HEAD")
}

func (f *proofFix) reportWithRanges(tip string, ranges []string) (V3ReportEntry, error) {
	f.t.Helper()
	ctx, _, closeSession, err := withPlanningSession(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	defer closeSession()
	observation, err := observeTidyV3Git(ctx, f.dir, f.rev(f.main))
	if err != nil {
		f.t.Fatal(err)
	}
	run, cleanup, err := defaultIntegrationEngine().begin(ctx, observation.Expected.Identity, observation.ConfigNotes)
	if err != nil {
		f.t.Fatal(err)
	}
	defer cleanup()
	resolved, err := run.resolveConsolidationRanges(ctx, ranges)
	if err != nil {
		return V3ReportEntry{}, err
	}
	policy := fullPolicy()
	if err = run.beginTip(); err != nil {
		f.t.Fatal(err)
	}
	v, err := run.evaluate(ctx, f.rev(f.main), f.rev(tip), policy)
	if err != nil {
		f.t.Fatal(err)
	}
	return run.collectReportEntry(ctx, reportRequest{Base: f.rev(f.main), Tip: f.rev(tip), Verdict: v, Policy: policy, Bounds: defaultReportBounds(), Ranges: resolved})
}

func TestRangePairingReportsEqualChangedAndUnpairedRows(t *testing.T) {
	f, rng := rangeFixture(t)
	e, err := f.reportWithRanges("topic", []string{rng})
	if err != nil {
		t.Fatal(err)
	}
	p := e.RangePairing
	if p == nil || p.Range != rng || p.Evidence != "superseded-candidate" || p.Note != "" || p.Truncated != nil {
		t.Fatalf("pairing %+v", p)
	}
	byBranch := map[string]string{}
	for _, pair := range p.Pairs {
		if pair.BranchCommit != "" {
			byBranch[pair.BranchCommit] = pair.Status
		} else if pair.Status != ">" {
			t.Errorf("a range-only row must be >: %+v", pair)
		}
	}
	short := func(rev string) string { return f.rev(rev)[:12] }
	want := map[string]string{short("topic~2"): "=", short("topic~1"): "!", short("topic"): "<"}
	if !reflect.DeepEqual(byBranch, want) {
		t.Errorf("branch rows %v, want %v (all rows %+v)", byBranch, want, p.Pairs)
	}
	extra := 0
	for _, pair := range p.Pairs {
		if pair.Status == ">" {
			extra++
		}
	}
	if extra != 1 {
		t.Errorf("range-only rows: %d", extra)
	}
	if e.Verdict.Verdict != "has-real-unmerged-content" {
		t.Errorf("a branch commit without a pair must keep the content verdict: %+v", e.Verdict)
	}
}

func TestRangePairingPicksTheRangeThatPairsMost(t *testing.T) {
	f, rng := rangeFixture(t)
	unrelated := f.rev(f.main) + "~1.." + f.rev(f.main)
	e, err := f.reportWithRanges("topic", []string{unrelated, rng})
	if err != nil {
		t.Fatal(err)
	}
	if e.RangePairing == nil || e.RangePairing.Range != rng {
		t.Fatalf("pairing %+v", e.RangePairing)
	}
	// On a tie the first range named is reported, in either order.
	same := rng[:strings.Index(rng, "..")] + ".." + f.main
	if same == rng {
		t.Fatal("fixture: the alias must differ in spelling")
	}
	for _, order := range [][]string{{rng, same}, {same, rng}} {
		e, err = f.reportWithRanges("topic", order)
		if err != nil || e.RangePairing == nil || e.RangePairing.Range != order[0] {
			t.Fatalf("tie order %v: %+v %v", order, e.RangePairing, err)
		}
	}
	e, err = f.reportWithRanges("topic", nil)
	if err != nil || e.RangePairing != nil {
		t.Fatalf("no range named: %+v %v", e.RangePairing, err)
	}
}

func TestRangePairingIsSkippedWhenTextconvIsConfigured(t *testing.T) {
	// The attribute names a path only the named range touches, so no
	// diff-attribute reason hides the textconv rule.
	f, rng := rangeFixtureWith(t, "e.txt diff=d\n")
	marker := filepath.Join(gitTempDir(t), "marker")
	f.git("config", "diff.d.textconv", helperScript(t, marker))
	// Control: range-diff itself runs the textconv helper.
	gitIn(f.dir, "", "range-diff", "topic~3..topic", rng)
	if _, err := os.Stat(marker); err != nil {
		t.Skip("the fixture does not trigger textconv on this Git")
	}
	os.Remove(marker)
	e, err := f.reportWithRanges("topic", []string{rng})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(marker); err == nil {
		t.Fatal("a textconv helper ran during the report")
	}
	if containsAll(e.Reasons, "diff-attribute") || e.RangePairing == nil || e.RangePairing.Note != "range pairing skipped: textconv or diff attribute" || len(e.RangePairing.Pairs) != 0 || e.Verdict.Verdict == "looks-superseded" {
		t.Fatalf("pairing %+v", e.RangePairing)
	}
}

func TestRangePairingIsSkippedForADiffAttribute(t *testing.T) {
	f, rng := rangeFixtureWith(t, "*.txt diff=d\n")
	e, err := f.reportWithRanges("topic", []string{rng})
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(e.Reasons, "diff-attribute") || e.RangePairing == nil || e.RangePairing.Note != "range pairing skipped: textconv or diff attribute" || len(e.RangePairing.Pairs) != 0 {
		t.Fatalf("reasons %v pairing %+v", e.Reasons, e.RangePairing)
	}
	if e.Verdict.Verdict != "unclear" {
		t.Errorf("verdict %+v", e.Verdict)
	}
}

func TestConsolidationRangeUsageErrorsNameTheSelector(t *testing.T) {
	f, rng := rangeFixture(t)
	many := func(n int) string { // a chain of n commits forms the range
		ref := fmt.Sprintf("refs/heads/long%d", n)
		f.fastImport(importChain("long", n, ref, f.rev(f.main), 1800000000))
		return f.rev(f.main) + ".." + f.rev(ref)
	}
	cases := map[string][]string{
		"no separator":        {"main"},
		"two separators":      {"a..b..c"},
		"three dots":          {"main...topic"},
		"empty left":          {"..main"},
		"empty right":         {"main.."},
		"option-like":         {"-x..main"},
		"unresolvable":        {"nosuchref..main"},
		"too many":            {rng, rng, rng, rng, rng},
		"more than 256":       {many(257)},
		"an invalid selector": {"main..\x00"},
	}
	for name, ranges := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := f.reportWithRanges("topic", ranges)
			var oe operationError
			if err == nil || !errorsAs(err, &oe) || oe.Code != "invalid-options" {
				t.Fatalf("accepted or wrong error: %v", err)
			}
			if name != "too many" && !strings.Contains(oe.Message, "--consolidation-range") {
				t.Errorf("message does not name the flag: %q", oe.Message)
			}
			if name != "too many" && name != "an invalid selector" && !strings.Contains(oe.Message, strings.Split(ranges[0], "\x00")[0][:min(len(ranges[0]), 8)]) {
				t.Errorf("message does not name the selector: %q", oe.Message)
			}
		})
	}
	if _, err := f.reportWithRanges("topic", []string{rng, rng, rng, rng}); err != nil {
		t.Fatalf("four ranges are allowed: %v", err)
	}
	exactly := many(256)
	if _, err := f.reportWithRanges("topic", []string{exactly}); err != nil {
		t.Fatalf("256 commits are allowed: %v", err)
	}
}

func errorsAs(err error, target *operationError) bool {
	for err != nil {
		if oe, ok := err.(operationError); ok {
			*target = oe
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestRangePairingKeepsAtMost256RowsAndCountsTheRest(t *testing.T) {
	f, _ := rangeFixture(t)
	f.fastImport(importChain("rows", 256, "refs/heads/rows", f.rev(f.main), 1900000000))
	rng := f.rev(f.main) + ".." + f.rev("refs/heads/rows")
	e, err := f.reportWithRanges("topic", []string{rng})
	if err != nil {
		t.Fatal(err)
	}
	p := e.RangePairing
	// 256 range-only rows and the three branch commits.
	if p == nil || len(p.Pairs) != 256 || p.Truncated == nil || *p.Truncated != (V3ReportTruncation{Unit: "rows", Shown: 256, Total: 259}) {
		t.Fatalf("pairing %+v", p)
	}
	if e.Verdict.Verdict != "unclear" {
		t.Errorf("a truncated pairing must give an unclear verdict: %+v", e.Verdict)
	}
}

func TestRangePairingMarksOneRowOverTheBoundAndNotTheBound(t *testing.T) {
	for rows, want := range map[int]*V3ReportTruncation{253: nil, 254: {Unit: "rows", Shown: 256, Total: 257}} {
		f, _ := rangeFixture(t)
		f.fastImport(importChain("edge", rows, "refs/heads/edge", f.rev(f.main), 1900000000))
		e, err := f.reportWithRanges("topic", []string{f.rev(f.main) + ".." + f.rev("refs/heads/edge")})
		if err != nil || e.RangePairing == nil {
			t.Fatalf("%d: %v", rows, err)
		}
		got := e.RangePairing.Truncated
		if (got == nil) != (want == nil) || got != nil && *got != *want {
			t.Errorf("%d range commits and 3 branch commits: marker %+v, want %+v", rows, got, want)
		}
	}
}
