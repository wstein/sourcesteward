// SPDX-License-Identifier: EUPL-1.2
package readycheck

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

var t0 = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)

// fakeForge serves one pull request and records every call. The nil embedded Reader makes any
// method the command must not use panic, which includes every method that could write.
type fakeForge struct {
	watchcmd.Reader
	pull    string
	pullErr error
	runs    string
	repoErr error
	branch  string
	calls   []string
}

func (f *fakeForge) PullDetail(_ context.Context, repo string, n int, etag string) (hosting.WatchResponse, error) {
	f.calls = append(f.calls, fmt.Sprintf("pull/%s#%d", repo, n))
	if f.pullErr != nil {
		return hosting.WatchResponse{}, f.pullErr
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(f.pull)}, nil
}
func (f *fakeForge) CheckRuns(_ context.Context, _ string, s string, _ int, _ string) (hosting.WatchResponse, error) {
	f.calls = append(f.calls, "runs/"+s[:4])
	return hosting.WatchResponse{Status: 200, Body: []byte(f.runs)}, nil
}
func (f *fakeForge) Statuses(_ context.Context, _ string, s string, _ int, _ string) (hosting.WatchResponse, error) {
	f.calls = append(f.calls, "stats/"+s[:4])
	return hosting.WatchResponse{Status: 200, Body: []byte(`[]`)}, nil
}
func (f *fakeForge) RepoInfo(_ context.Context, repo string) (hosting.RepoInfo, error) {
	f.calls = append(f.calls, "repo/"+repo)
	if f.repoErr != nil {
		return hosting.RepoInfo{}, f.repoErr
	}
	return hosting.RepoInfo{DefaultBranch: f.branch, HTMLURL: "https://github.com/o/r"}, nil
}

const green = `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{}}]}`

func pullBody(draft bool, base, body, head string) string {
	return fmt.Sprintf(`{"state":"open","draft":%t,"body":%q,"mergeable":true,"mergeable_state":"draft","rebaseable":true,"head":{"sha":%q,"repo":{"full_name":"o/r"}},"base":{"ref":%q,"repo":{"full_name":"o/r"}}}`, draft, body, head, base)
}

func newForge() *fakeForge {
	return &fakeForge{pull: pullBody(true, "main", "Blocker: none", head), runs: green, branch: "main"}
}

func clearFor(h string) ReviewFunc {
	return func(context.Context, string, int, string) (Review, error) {
		return Review{State: "clear", Head: h}, nil
	}
}

func doRun(f *fakeForge, r ReviewFunc) Document {
	return Run(context.Background(), f, r, Input{Repo: "o/r", Number: 7, Host: "github.com"}, func() time.Time { return t0 })
}

func status(d Document, id string) string {
	for _, c := range d.Conditions {
		if c.ID == id {
			return c.Status
		}
	}
	return "missing"
}

func TestRunReadyAndOnlyReads(t *testing.T) {
	f := newForge()
	d := doRun(f, clearFor(head))
	if !d.Ready || len(d.Errors) != 0 || d.Head == nil || *d.Head != head || d.Draft == nil || !*d.Draft {
		t.Fatalf("%+v", d)
	}
	if d.SchemaVersion != 1 || d.Command != "ready-check" || d.Repo != "o/r" || d.Number != 7 || d.Time != "2026-11-01T09:00:00Z" {
		t.Fatalf("%+v", d)
	}
	for _, c := range d.Conditions {
		if c.Status != StatusPass {
			t.Errorf("%+v", c)
		}
	}
	want := "pull/o/r#7 repo/o/r runs/3f2a stats/3f2a"
	if got := strings.Join(f.calls, " "); got != want {
		t.Errorf("calls = %q, want %q", got, want)
	}
}

func TestRunEachConditionFailsFromTheForgeData(t *testing.T) {
	cases := []struct {
		name string
		id   string
		mut  func(*fakeForge)
		rev  ReviewFunc
	}{
		{"closed", CondOpen, func(f *fakeForge) { f.pull = strings.Replace(f.pull, `"state":"open"`, `"state":"closed"`, 1) }, clearFor(head)},
		{"review stale", CondReview, func(*fakeForge) {}, func(context.Context, string, int, string) (Review, error) {
			return Review{State: "clear-stale", Head: oldHead}, nil
		}},
		{"checks red", CondChecks, func(f *fakeForge) { f.runs = strings.Replace(green, "success", "failure", 1) }, clearFor(head)},
		{"base", CondBase, func(f *fakeForge) { f.pull = pullBody(true, "feat/x", "Blocker: none", head) }, clearFor(head)},
		{"blocker", CondBlocker, func(f *fakeForge) { f.pull = pullBody(true, "main", "Blocker: Independent review pending", head) }, clearFor(head)},
		{"conflict", CondConflict, func(f *fakeForge) {
			f.pull = strings.Replace(f.pull, `"mergeable":true`, `"mergeable":false`, 1)
		}, clearFor(head)},
		{"rebaseable", CondRebaseable, func(f *fakeForge) {
			f.pull = strings.Replace(f.pull, `"rebaseable":true`, `"rebaseable":false`, 1)
		}, clearFor(head)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newForge()
			tc.mut(f)
			d := doRun(f, tc.rev)
			if d.Ready || status(d, tc.id) != StatusFail {
				t.Fatalf("%s = %s, ready %t", tc.id, status(d, tc.id), d.Ready)
			}
		})
	}
}

func TestRunReadFailuresAreUnknownNotPass(t *testing.T) {
	f := newForge()
	f.pullErr = errors.New("boom")
	d := doRun(f, clearFor(head))
	if d.Ready || d.Head != nil || d.Draft != nil || len(d.Errors) != 1 || d.Errors[0].Source != "pull" {
		t.Fatalf("%+v", d)
	}
	for _, c := range d.Conditions {
		if c.Status != StatusUnknown {
			t.Errorf("%+v", c)
		}
	}
	if len(f.calls) != 2 || f.calls[1] != "repo/o/r" {
		t.Errorf("without a head no checks or review are read: %v", f.calls)
	}

	f = newForge()
	f.repoErr = &hosting.WatchError{Code: hosting.CodeRateLimited, Message: "limit"}
	d = doRun(f, clearFor(head))
	if status(d, CondBase) != StatusUnknown || d.Ready || len(d.Errors) != 1 || d.Errors[0].Source != "repo" || d.Errors[0].Code != hosting.CodeRateLimited {
		t.Fatalf("%+v", d)
	}

	f = newForge()
	d = doRun(f, func(context.Context, string, int, string) (Review, error) { return Review{}, errors.New("no comments") })
	if status(d, CondReview) != StatusUnknown || d.Ready || d.Errors[0].Source != "review" {
		t.Fatalf("%+v", d)
	}

	f = newForge()
	d = doRun(f, nil)
	if status(d, CondReview) != StatusUnknown || d.Ready || d.Errors[0].Source != "review" {
		t.Fatalf("a missing review source never passes: %+v", d)
	}
}

func TestRunRejectsAMalformedPullRequest(t *testing.T) {
	for _, body := range []string{`not json`, `{"state":"open","head":{"sha":"short"},"base":{"ref":"main"}}`, `{"state":"open","head":{"sha":"` + head + `"},"base":{}}`} {
		f := newForge()
		f.pull = body
		d := doRun(f, clearFor(head))
		if d.Ready || len(d.Errors) == 0 || d.Errors[0].Source != "pull" {
			t.Errorf("%q: %+v", body, d)
		}
	}
}

func TestMergedPullRequestIsNotOpen(t *testing.T) {
	f := newForge()
	f.pull = strings.Replace(f.pull, `"state":"open"`, `"state":"closed","merged":true`, 1)
	d := doRun(f, clearFor(head))
	for _, c := range d.Conditions {
		if c.ID == CondOpen && (c.Status != StatusFail || !strings.Contains(c.Evidence, "merged")) {
			t.Fatalf("%+v", c)
		}
	}
}
