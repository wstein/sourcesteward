// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"regexp"
	"sort"
	"strings"
)

// Check classes of one PR head (Spec 035, "Check class"). The empty class means "not observed yet".
const (
	ClassNone    = "none"
	ClassPending = "pending"
	ClassGreen   = "green"
	ClassRed     = "red"
)

// Failure shapes of a red head (Spec 035, "shape classification").
const (
	ShapeStack   = "stack-merge-shape"
	ShapeReal    = "real"
	ShapeUnknown = "unknown"
)

const (
	maxNames     = 20
	maxCheckText = 4096
)

// CheckRun is the bounded data of one check run: no log is ever read.
type CheckRun struct {
	Name       string
	Status     string // queued, in_progress, completed
	Conclusion string
	Title      string
	Summary    string
	Text       string
}

// StatusContext is one commit status context.
type StatusContext struct {
	Context     string
	State       string // pending, success, failure, error
	Description string
}

// ChecksResult is the classification of one head.
type ChecksResult struct {
	Class        string
	Total        int
	Failing      []string // sorted, at most 20
	FailingTotal int
	Pending      []string // sorted, at most 20
	Shape        string   // set for red only
}

var stackDefaults = []string{"Unexpected synthetic merge parents", "PR must target repository main"}

var (
	failLine = regexp.MustCompile(`(?m)^FAIL\s`)
	testLine = regexp.MustCompile(`_test\.go:\d+`)
)

func isFailureConclusion(c string) bool {
	switch c {
	case "failure", "cancelled", "timed_out", "action_required", "startup_failure":
		return true
	}
	return false
}

func bounded(s string) string {
	if len(s) > maxCheckText {
		return s[:maxCheckText]
	}
	return s
}

func (r CheckRun) text() string {
	return r.Title + "\n" + bounded(r.Summary) + "\n" + bounded(r.Text)
}

func realMarker(text string) bool {
	return strings.Contains(text, "--- FAIL: ") || failLine.MatchString(text) || strings.Contains(text, "panic:") ||
		strings.Contains(text, "Error:") || testLine.MatchString(text)
}

func topNames(names []string) []string {
	sort.Strings(names)
	if len(names) > maxNames {
		names = names[:maxNames]
	}
	if names == nil {
		return []string{}
	}
	return names
}

// ClassifyChecks classifies a head with the default stack patterns.
func ClassifyChecks(runs []CheckRun, stats []StatusContext, base, defaultBranch string) ChecksResult {
	return ClassifyChecksWith(runs, stats, base, defaultBranch, nil)
}

// ClassifyChecksWith classifies a head; extra are the [shape] stack_patterns of the config.
func ClassifyChecksWith(runs []CheckRun, stats []StatusContext, base, defaultBranch string, extra []string) ChecksResult {
	res := ChecksResult{Total: len(runs) + len(stats), Failing: []string{}, Pending: []string{}}
	var failing []failItem
	var pending, failNames []string
	for _, r := range runs {
		switch {
		case r.Status == "completed" && isFailureConclusion(r.Conclusion):
			failing = append(failing, failItem{r.text(), r.Conclusion == "failure"})
			failNames = append(failNames, r.Name)
		case r.Status != "completed":
			pending = append(pending, r.Name)
		}
	}
	for _, s := range stats {
		switch s.State {
		case "failure", "error":
			failing = append(failing, failItem{s.Context + "\n" + s.Description, s.State == "failure"})
			failNames = append(failNames, s.Context)
		case "pending":
			pending = append(pending, s.Context)
		}
	}
	switch {
	case len(failing) > 0:
		res.Class = ClassRed
		res.FailingTotal = len(failNames)
		res.Failing = topNames(failNames)
		res.Shape = shapeOf(failing, base, defaultBranch, extra)
	case len(pending) > 0:
		res.Class = ClassPending
	case res.Total > 0:
		res.Class = ClassGreen
	default:
		res.Class = ClassNone
	}
	res.Pending = topNames(pending)
	return res
}

// failItem is the shape input of one failing check: whether its conclusion is exactly "failure" and its bounded text.
type failItem struct {
	text    string
	failure bool
}

// shapeOf applies the three ordered rules to the whole failing set of one head.
func shapeOf(items []failItem, base, defaultBranch string, extra []string) string {
	for _, i := range items { // rule 1
		if i.failure && realMarker(i.text) {
			return ShapeReal
		}
	}
	if defaultBranch == "" || base == defaultBranch { // rule 2: only a stacked PR
		return ShapeUnknown
	}
	patterns := append(append([]string{}, stackDefaults...), extra...)
	for _, i := range items {
		if !i.failure {
			return ShapeUnknown
		}
		matched := false
		for _, p := range patterns {
			if p != "" && strings.Contains(i.text, p) {
				matched = true
				break
			}
		}
		if !matched {
			return ShapeUnknown
		}
	}
	return ShapeStack
}
