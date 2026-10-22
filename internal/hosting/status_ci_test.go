// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func statusCIReader(t *testing.T, f *fakeForge) *WatchReader {
	t.Helper()
	client, err := newSDKClientGuardMode("github.com", "fixturetoken", t.TempDir(), f, guardStatus)
	if err != nil {
		t.Fatal(err)
	}
	r := newWatchReader(&ghAPI{client: client, host: "github.com"})
	r.mode = guardStatus
	return r
}

const ciSHA = "0123456789abcdef0123456789abcdef01234567"

func runsPath(workflow string) string {
	return "repos/o/r/actions/workflows/" + workflow + "/runs?head_sha=" + ciSHA + "&per_page=100&page=1"
}

func TestWorkflowRunsReadsPageOneOfTheFixedShape(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[runsPath("release.yml")] = func(*http.Request) *http.Response {
		return response(200, `{"total_count":1,"workflow_runs":[{"status":"completed","conclusion":"failure","html_url":"https://github.com/o/r/actions/runs/9","head_sha":"`+ciSHA+`","created_at":"2026-10-22T09:00:00Z"}]}`)
	}
	r := statusCIReader(t, f)
	res, err := r.WorkflowRuns(context.Background(), "o/r", "release.yml", ciSHA)
	if err != nil || res.Status != 200 {
		t.Fatalf("%+v %v", res, err)
	}
	if len(f.log) != 1 || f.log[0] != runsPath("release.yml") {
		t.Fatalf("requests %v", f.log)
	}
	if c := r.TakeCalls()[KindRuns]; c.Requests != 1 {
		t.Fatalf("accounting %+v", c)
	}
	runs, err := DecodeWorkflowRuns(res.Body)
	if err != nil || len(runs) != 1 {
		t.Fatalf("%+v %v", runs, err)
	}
	want := WorkflowRun{Status: "completed", Conclusion: "failure", URL: "https://github.com/o/r/actions/runs/9", CreatedAt: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC), HeadSHA: ciSHA}
	if runs[0] != want {
		t.Fatalf("%+v", runs[0])
	}
}

func TestWorkflowRunsRefusesWhatTheGuardDoesNotAdmit(t *testing.T) {
	for name, tc := range map[string]struct{ repo, workflow, sha string }{
		"path in workflow":  {"o/r", ".github/workflows/release.yml", ciSHA},
		"not a yaml file":   {"o/r", "release.sh", ciSHA},
		"traversal":         {"o/r", "../x.yml", ciSHA},
		"short sha":         {"o/r", "release.yml", ciSHA[:39]},
		"upper case sha":    {"o/r", "release.yml", strings.ToUpper(ciSHA)},
		"invalid repo":      {"o", "release.yml", ciSHA},
		"query in workflow": {"o/r", "release.yml?x=1", ciSHA},
	} {
		f := newFakeForge(t)
		_, err := statusCIReader(t, f).WorkflowRuns(context.Background(), tc.repo, tc.workflow, tc.sha)
		assertWatchCode(t, err, CodeMalformed)
		if len(f.log) != 0 {
			t.Errorf("%s: sent %v", name, f.log)
		}
	}
}

func TestWorkflowRunsAnswersMapToWatchErrors(t *testing.T) {
	f := newFakeForge(t) // no handler: the forge answers 404
	_, err := statusCIReader(t, f).WorkflowRuns(context.Background(), "o/r", "release.yml", ciSHA)
	if we := assertWatchCode(t, err, CodeForbidden); we.Status != http.StatusNotFound {
		t.Fatalf("a missing workflow must keep status 404: %+v", we)
	}
	f.handlers[runsPath("release.yml")] = func(*http.Request) *http.Response {
		return response(200, strings.Repeat("x", maxBytes+1))
	}
	_, err = statusCIReader(t, f).WorkflowRuns(context.Background(), "o/r", "release.yml", ciSHA)
	assertWatchCode(t, err, CodePageTooLarge)
}

func TestDecodeWorkflowRuns(t *testing.T) {
	runs, err := DecodeWorkflowRuns([]byte(`{"total_count":2,"workflow_runs":[
		{"status":"in_progress","conclusion":null,"html_url":"https://github.com/o/r/actions/runs/2","created_at":"2026-10-22T10:00:00Z"},
		{"status":"completed","conclusion":"success","html_url":"https://github.com/o/r/actions/runs/1","created_at":"2026-10-22T09:00:00Z"}]}`))
	if err != nil || len(runs) != 2 || runs[0].Conclusion != "" || runs[0].Status != "in_progress" || runs[1].Conclusion != "success" {
		t.Fatalf("%+v %v", runs, err)
	}
	empty, err := DecodeWorkflowRuns([]byte(`{"total_count":0,"workflow_runs":[]}`))
	if err != nil || len(empty) != 0 {
		t.Fatalf("%+v %v", empty, err)
	}
	for name, body := range map[string]string{
		"not json":          `<html>`,
		"no array":          `{"total_count":0}`,
		"array is null":     `{"workflow_runs":null}`,
		"no status":         `{"workflow_runs":[{"conclusion":"success","html_url":"u","created_at":"2026-10-22T09:00:00Z"}]}`,
		"bad time":          `{"workflow_runs":[{"status":"completed","conclusion":"success","html_url":"u","created_at":"yesterday"}]}`,
		"more than a page":  `{"workflow_runs":[` + strings.Repeat(`{"status":"completed","conclusion":"success","html_url":"u","created_at":"2026-10-22T09:00:00Z"},`, 100) + `{"status":"completed","conclusion":"success","html_url":"u","created_at":"2026-10-22T09:00:00Z"}]}`,
		"conclusion number": `{"workflow_runs":[{"status":"completed","conclusion":3,"html_url":"u","created_at":"2026-10-22T09:00:00Z"}]}`,
	} {
		if _, err := DecodeWorkflowRuns([]byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		} else {
			assertWatchCode(t, err, CodeMalformed)
		}
	}
}

func TestDecodeBranchTip(t *testing.T) {
	sha, err := DecodeBranchTip([]byte(`{"name":"main","commit":{"sha":"` + ciSHA + `"}}`))
	if err != nil || sha != ciSHA {
		t.Fatalf("%q %v", sha, err)
	}
	for _, body := range []string{`x`, `{}`, `{"commit":{"sha":"abc"}}`, `{"commit":{"sha":"` + strings.ToUpper(ciSHA) + `"}}`, `{"commit":null}`} {
		_, err := DecodeBranchTip([]byte(body))
		assertWatchCode(t, err, CodeMalformed)
	}
}
