// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
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

func TestStatusRemoteLineNamesTruncatedAndForeignHints(t *testing.T) {
	line := statusRemoteLine(reviewRepo(true, pulls.ReviewCounts{Clear: 2, None: 1}, pulls.HintCounts{TruncatedReviewUnknown: 2, ForeignReviewIgnored: 1}))
	if !strings.Contains(line, "reviews: 2 clear, 1 none (hints: 2 truncated-review-unknown, 1 foreign-review-ignored)") {
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

// reviewStatusForge adds the review reads (Spec 046) to a healthy, empty statusForge.
type reviewStatusForge struct{ *statusForge }

func (f reviewStatusForge) ReadIssueComments(context.Context, string, int) ([]hosting.IssueComment, bool, error) {
	return nil, false, f.read()
}

func (f reviewStatusForge) ClosedPulls(context.Context, string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{Status: 200, Body: []byte(`[]`)}, f.read()
}

func TestStatusReadsReviewsOnlyWithTheReviewFlag(t *testing.T) {
	hasReview := func(args ...string) bool {
		t.Helper()
		run := runOnline(t, rootOnly(), reviewStatusForge{&statusForge{}}, nil, append([]string{"--json"}, args...)...)
		if run.err != nil {
			t.Fatalf("%v: %v", args, run.err)
		}
		var doc struct {
			Repos []struct {
				Remote struct {
					Sources struct {
						Pulls struct {
							Data map[string]any `json:"data"`
						} `json:"pulls"`
					} `json:"sources"`
				} `json:"remote"`
			} `json:"repos"`
		}
		if err := json.Unmarshal([]byte(run.out), &doc); err != nil || len(doc.Repos) == 0 {
			t.Fatalf("%v: %v %s", args, err, run.out)
		}
		_, ok := doc.Repos[0].Remote.Sources.Pulls.Data["review"]
		return ok
	}
	if hasReview() {
		t.Error("a plain run reads no review state")
	}
	if !hasReview("--review") {
		t.Error("--review reads the review state")
	}
}

func TestStatusReviewFlagIsRefusedOffline(t *testing.T) {
	run := runOnline(t, rootOnly(), &statusForge{err: errors.New("no request may be sent")}, nil, "--offline", "--review")
	if run.err == nil || !strings.Contains(run.err.Error(), "--review cannot be used with --offline") || run.opened != 0 || run.out != "" {
		t.Fatalf("%v %+v", run.err, run)
	}
	if isRuntimeFailure(run.err) {
		t.Fatalf("a usage error, not a runtime failure: %v", run.err)
	}
}
