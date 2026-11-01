// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/statusremote"
)

func reviewRepo(read bool, rv pulls.ReviewCounts, h pulls.HintCounts) statusremote.RepoResult {
	return statusremote.RepoResult{
		Name: "r", Slug: "o/r",
		Remote: &overview.Remote{Sources: onlineSources(onlineOK), Main: &overview.Main{Class: "green"}, Visibility: overview.WriteAccess},
		Issues: &statusremote.Issues{State: onlineOK},
		Pulls: &statusremote.Pulls{State: onlineOK, Open: statusPtrTo(3), ReviewRead: read,
			Counts: pulls.Summary{Total: 3, Ready: 1, Draft: 1, Blocked: 1, Review: rv, Hints: h}},
	}
}

func TestStatusRemoteLineNamesTheReviewStatesAndHints(t *testing.T) {
	line := statusRemoteLine(reviewRepo(true, pulls.ReviewCounts{Clear: 1, ClearCarried: 1, None: 1}, pulls.HintCounts{ClearNotReady: 1, NoFinalReview: 1, ReadyMergeable: 1}))
	if !strings.Contains(line, "3 open PRs (1 ready, 1 draft, 1 other blocked), reviews: 1 clear, 1 clear-carried, 1 none (hints: 1 clear-not-ready, 1 no-final-review, 1 ready-mergeable)") {
		t.Fatalf("%s", line)
	}
}

func TestStatusRemoteLineWithoutReviewReadsStaysAsItWas(t *testing.T) {
	line := statusRemoteLine(reviewRepo(false, pulls.ReviewCounts{NotRead: 3}, pulls.HintCounts{}))
	if strings.Contains(line, "review") || strings.Contains(line, "hints") {
		t.Fatalf("%s", line)
	}
	// reviews read but nothing to say: no clause
	if line := statusRemoteLine(reviewRepo(true, pulls.ReviewCounts{}, pulls.HintCounts{})); strings.Contains(line, "review") {
		t.Fatalf("%s", line)
	}
	// only unread reviews: say so
	if line := statusRemoteLine(reviewRepo(true, pulls.ReviewCounts{NotRead: 2}, pulls.HintCounts{})); !strings.Contains(line, "reviews: 2 not read") {
		t.Fatalf("%s", line)
	}
}
