// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"fmt"
	"strings"
	"testing"
)

func TestClassifyChecksClasses(t *testing.T) {
	run := func(name, status, conclusion string) CheckRun {
		return CheckRun{Name: name, Status: status, Conclusion: conclusion}
	}
	cases := []struct {
		name  string
		runs  []CheckRun
		stats []StatusContext
		want  string
		total int
	}{
		{"none", nil, nil, ClassNone, 0},
		{"green", []CheckRun{run("a", "completed", "success")}, nil, ClassGreen, 1},
		{"skipped and neutral are green", []CheckRun{run("a", "completed", "skipped"), run("b", "completed", "neutral")}, nil, ClassGreen, 2},
		{"pending run", []CheckRun{run("a", "in_progress", ""), run("b", "completed", "success")}, nil, ClassPending, 2},
		{"queued run", []CheckRun{run("a", "queued", "")}, nil, ClassPending, 1},
		{"pending status", nil, []StatusContext{{Context: "ci", State: "pending"}}, ClassPending, 1},
		{"failure wins over pending", []CheckRun{run("a", "in_progress", ""), run("b", "completed", "failure")}, nil, ClassRed, 2},
		{"cancelled", []CheckRun{run("a", "completed", "cancelled")}, nil, ClassRed, 1},
		{"timed_out", []CheckRun{run("a", "completed", "timed_out")}, nil, ClassRed, 1},
		{"action_required", []CheckRun{run("a", "completed", "action_required")}, nil, ClassRed, 1},
		{"startup_failure", []CheckRun{run("a", "completed", "startup_failure")}, nil, ClassRed, 1},
		{"status failure", nil, []StatusContext{{Context: "ci", State: "failure"}}, ClassRed, 1},
		{"status error", nil, []StatusContext{{Context: "ci", State: "error"}}, ClassRed, 1},
		{"status success", nil, []StatusContext{{Context: "ci", State: "success"}}, ClassGreen, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClassifyChecks(c.runs, c.stats, "main", "main")
			if got.Class != c.want || got.Total != c.total {
				t.Fatalf("got %s/%d want %s/%d", got.Class, got.Total, c.want, c.total)
			}
		})
	}
}

func TestClassifyFailingNamesAreSortedBoundedAndCounted(t *testing.T) {
	var runs []CheckRun
	for i := 29; i >= 0; i-- {
		runs = append(runs, CheckRun{Name: fmt.Sprintf("job-%02d", i), Status: "completed", Conclusion: "failure"})
	}
	got := ClassifyChecks(runs, nil, "main", "main")
	if got.FailingTotal != 30 || len(got.Failing) != 20 || got.Failing[0] != "job-00" || got.Failing[19] != "job-19" {
		t.Fatalf("failing %v total %d", got.Failing, got.FailingTotal)
	}
}

func TestShapeRules(t *testing.T) {
	fail := func(name, text string) CheckRun {
		return CheckRun{Name: name, Status: "completed", Conclusion: "failure", Summary: text}
	}
	stack := "Unexpected synthetic merge parents"
	target := "PR must target repository main"
	cases := []struct {
		name        string
		runs        []CheckRun
		stats       []StatusContext
		base, deflt string
		patterns    []string
		want        string
	}{
		{"stack on stacked base", []CheckRun{fail("a", stack)}, nil, "feature", "main", nil, ShapeStack},
		{"stack text on default branch is unknown", []CheckRun{fail("a", stack)}, nil, "main", "main", nil, ShapeUnknown},
		{"both patterns", []CheckRun{fail("a", stack), fail("b", target)}, nil, "feature", "main", nil, ShapeStack},
		{"mixed shape and other is unknown", []CheckRun{fail("a", stack), fail("b", "runner shutdown")}, nil, "feature", "main", nil, ShapeUnknown},
		{"real wins over stack", []CheckRun{fail("a", stack), fail("b", "--- FAIL: TestX (0.00s)")}, nil, "feature", "main", nil, ShapeReal},
		{"real FAIL line", []CheckRun{fail("a", "ok\nFAIL\tpkg 0.1s")}, nil, "main", "main", nil, ShapeReal},
		{"real panic", []CheckRun{fail("a", "panic: boom")}, nil, "main", "main", nil, ShapeReal},
		{"real Error", []CheckRun{fail("a", "Error: nope")}, nil, "main", "main", nil, ShapeReal},
		{"real _test.go line", []CheckRun{fail("a", "x_test.go:42: bad")}, nil, "main", "main", nil, ShapeReal},
		{"_test.go without digits is not real", []CheckRun{fail("a", "x_test.go: bad")}, nil, "main", "main", nil, ShapeUnknown},
		{"FAIL inside a word is not real", []CheckRun{fail("a", "FAILURE reported")}, nil, "main", "main", nil, ShapeUnknown},
		{"empty text is unknown", []CheckRun{fail("a", "")}, nil, "feature", "main", nil, ShapeUnknown},
		{"name alone never qualifies", []CheckRun{fail(stack, "")}, nil, "feature", "main", nil, ShapeUnknown},
		{"cancelled is unknown even with text", []CheckRun{{Name: "a", Status: "completed", Conclusion: "cancelled", Summary: stack}}, nil, "feature", "main", nil, ShapeUnknown},
		{"extra pattern", []CheckRun{fail("a", "custom marker")}, nil, "feature", "main", []string{"custom marker"}, ShapeStack},
		{"patterns are case sensitive", []CheckRun{fail("a", "unexpected synthetic merge parents")}, nil, "feature", "main", nil, ShapeUnknown},
		{"status failure with description", nil, []StatusContext{{Context: "ci", State: "failure", Description: target}}, "feature", "main", nil, ShapeStack},
		{"status error is unknown", nil, []StatusContext{{Context: "ci", State: "error", Description: target}}, "feature", "main", nil, ShapeUnknown},
		{"unknown default branch is unknown", []CheckRun{fail("a", stack)}, nil, "feature", "", nil, ShapeUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClassifyChecksWith(c.runs, c.stats, c.base, c.deflt, c.patterns)
			if got.Class != ClassRed || got.Shape != c.want {
				t.Fatalf("got %s/%s want red/%s", got.Class, got.Shape, c.want)
			}
		})
	}
}

func TestShapeReadsOnlyFirst4KiBOfSummaryAndText(t *testing.T) {
	long := strings.Repeat("x", 4096) + "--- FAIL: TestLate"
	got := ClassifyChecks([]CheckRun{{Name: "a", Status: "completed", Conclusion: "failure", Summary: long}}, nil, "main", "main")
	if got.Shape != ShapeUnknown {
		t.Fatalf("marker beyond 4 KiB must not count: %s", got.Shape)
	}
}
