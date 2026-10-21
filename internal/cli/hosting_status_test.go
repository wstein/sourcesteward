// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/inspection"
)

type hostingCalls struct {
	legacy, status, lookup int
	options                hosting.Options
}

func statusCommand(jj bool, c *hostingCalls, st hosting.PRStatusResult, lk hosting.LookupResult) (*bytesCommand, func(args ...string) error) {
	cmd := hostingCommandWithOps(jj,
		func(_ context.Context, _ string, o hosting.Options) hosting.Result {
			c.legacy++
			c.options = o
			return hosting.Result{SchemaVersion: 1, Kind: o.Kind, Status: "ok"}
		},
		func(_ context.Context, _ string, o hosting.Options) hosting.PRStatusResult {
			c.status++
			c.options = o
			return st
		},
		func(_ context.Context, _ string, o hosting.Options) hosting.LookupResult {
			c.lookup++
			c.options = o
			return lk
		})
	var out, errOut bytes.Buffer
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	return &bytesCommand{out: &out}, func(args ...string) error { cmd.SetArgs(args); return cmd.ExecuteContext(context.Background()) }
}

type bytesCommand struct{ out *bytes.Buffer }

func okStatus(title, ref string) hosting.PRStatusResult {
	head := "a"
	r := hosting.PRStatusResult{SchemaVersion: 2, Kind: "pr", Status: "ok", Backend: "git", Candidates: []hosting.Candidate{}, Diagnostics: []inspection.Diagnostic{}, ObservedAt: "2026-10-18T12:00:00Z"}
	r.PullRequest = &hosting.PRStatusPullRequest{Number: 42, Title: title, URL: "https://github.com/a/b/pull/42", State: "open", HeadSHA: strings.Repeat(head, 40), BaseSHA: strings.Repeat("b", 40), HeadRef: ref, BaseRef: "main", BaseRepository: "a/b", Lifecycle: "open"}
	r.Observation = hosting.PRStatusObservation{Mode: "once", Stop: "sampled", Samples: 1, Expectation: "not-requested"}
	r.Boundary = hosting.PRStatusBoundary{Complete: true, Network: true, Requests: 1, MaxRequests: 1}
	return r
}

func TestHostingStatusFlagsAreUsageErrors(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for _, args := range [][]string{
		{"pr", "3", "--json-version", "2"},
		{"pr", "3", "--json", "--json-version", "3"},
		{"pr", "3", "--json", "--json-version", "2", "--compact"},
		{"pr", "3", "--expect-head", "abc"},
		{"pr", "3", "--expect-head", ""},
		{"pr", "3", "--expect-head", strings.Repeat("A", 40)},
		{"pr", "3", "--json", "--expect-head", sha},
		{"pr", "3", "--watch"},
		{"pr", "3", "--json", "--json-version", "2", "--watch"},
		{"ci", "3", "--compact"},
		{"ci", "3", "--expect-head", sha},
		{"repo", "--json-version", "2"},
		{"prs", "short"},
		{"prs", sha, "--expect-head", sha},
		{"prs", sha, "--json-version", "2"},
		{"prs"},
		{"prs", sha, "p", "extra"},
		{"prs", sha, "--json", "--compact"},
	} {
		for _, jj := range []bool{false, true} {
			var c hostingCalls
			_, run := statusCommand(jj, &c, okStatus("t", "r"), hosting.LookupResult{})
			if e := run(args...); e == nil || c.legacy+c.status+c.lookup != 0 {
				t.Fatalf("%q jj=%v err=%v calls=%+v", args, jj, e, c)
			}
		}
	}
}

func TestHostingStatusSelectsOperationAndOptions(t *testing.T) {
	sha := strings.Repeat("c", 40)
	for _, tc := range []struct {
		args                   []string
		legacy, status, lookup int
		wantStatus             bool
		expect                 string
	}{
		{[]string{"pr", "3"}, 1, 0, 0, false, ""},
		{[]string{"pr", "3", "--json"}, 1, 0, 0, false, ""},
		{[]string{"pr", "3", "--json", "--json-version", "1"}, 1, 0, 0, false, ""},
		{[]string{"pr", "3", "--json", "--json-version", "2"}, 0, 1, 0, true, ""},
		{[]string{"pr", "3", "--json", "--json-version", "2", "--expect-head", sha}, 0, 1, 0, true, sha},
		{[]string{"pr", "3", "--compact"}, 0, 1, 0, true, ""},
		{[]string{"pr", "3", "--expect-head", sha}, 0, 1, 0, true, sha},
		{[]string{"ci", "3", "--json"}, 1, 0, 0, false, ""},
		{[]string{"prs", sha, "--json"}, 0, 0, 1, false, ""},
	} {
		for _, jj := range []bool{false, true} {
			var c hostingCalls
			_, run := statusCommand(jj, &c, okStatus("t", "r"), hosting.LookupResult{SchemaVersion: 2, Kind: "prs", Status: "ok"})
			if e := run(tc.args...); e != nil || c.legacy != tc.legacy || c.status != tc.status || c.lookup != tc.lookup || c.options.Status != tc.wantStatus || c.options.ExpectHead != tc.expect || c.options.RequireJJ != jj {
				t.Fatalf("%q jj=%v err=%v %+v", tc.args, jj, e, c)
			}
		}
	}
	var c hostingCalls
	_, run := statusCommand(false, &c, okStatus("t", "r"), hosting.LookupResult{Status: "ok"})
	if e := run("prs", sha, "some path", "--repo", "a/b"); e != nil || c.options.HeadSHA != sha || c.options.Kind != "prs" || c.options.Repo != "a/b" {
		t.Fatalf("%v %+v", e, c)
	}
}

func TestHostingStatusHumanOutputIsEscapedAndCompactIsOneLine(t *testing.T) {
	hostile := "t\x1b[31m\nline2‮\u0085\rend"
	for _, compact := range []bool{true, false} {
		var c hostingCalls
		out, run := statusCommand(false, &c, okStatus(hostile, "ref\x1b]0;x\x07\nz"), hosting.LookupResult{})
		args := []string{"pr", "42", "--expect-head", strings.Repeat("a", 40)}
		if compact {
			args = []string{"pr", "42", "--compact"}
		}
		if e := run(args...); e != nil {
			t.Fatal(e)
		}
		text := out.out.String()
		for _, r := range text {
			if r == '\x1b' || r == '\r' || r == '\x07' || r == '‮' || r == '\u0085' {
				t.Fatalf("control byte leaked: %q", text)
			}
		}
		if compact && (strings.Count(text, "\n") != 1 || !strings.HasSuffix(text, "\n")) {
			t.Fatalf("compact must be exactly one line: %q", text)
		}
		if !compact && !strings.Contains(text, "Lifecycle:") {
			t.Fatalf("detailed output lost merge evidence: %q", text)
		}
	}
}

func TestHostingStatusJSONAndExit(t *testing.T) {
	var c hostingCalls
	st := okStatus("title", "r")
	out, run := statusCommand(false, &c, st, hosting.LookupResult{})
	if e := run("pr", "42", "--json", "--json-version", "2"); e != nil {
		t.Fatal(e)
	}
	var got hosting.PRStatusResult
	if e := json.Unmarshal(out.out.Bytes(), &got); e != nil || got.SchemaVersion != 2 || got.Observation.Mode != "once" || got.PullRequest.Number != 42 {
		t.Fatalf("%v %s", e, out.out)
	}
	st.Status = "error"
	for _, args := range [][]string{{"pr", "42", "--json", "--json-version", "2"}, {"pr", "42", "--compact"}, {"pr", "42", "--expect-head", strings.Repeat("a", 40)}} {
		_, run = statusCommand(false, &c, st, hosting.LookupResult{})
		var failure runtimeFailure
		if e := run(args...); e == nil || !asRuntimeFailure(e, &failure) {
			t.Fatalf("%q must be exit 1 (runtime failure), got %v", args, e)
		}
	}
}

func asRuntimeFailure(err error, target *runtimeFailure) bool {
	f, ok := err.(runtimeFailure)
	if ok {
		*target = f
	}
	return ok
}

func TestHostingLookupOutput(t *testing.T) {
	sha := strings.Repeat("a", 40)
	base := okStatus("evil\x1b[2J\ntitle", "r\nx").PullRequest
	lk := hosting.LookupResult{SchemaVersion: 2, Kind: "prs", Status: "ok", Backend: "git", Candidates: []hosting.Candidate{}, Diagnostics: []inspection.Diagnostic{}, Query: hosting.LookupQuery{HeadSHA: sha}, Count: 2, Truncated: true,
		PullRequests: []hosting.LookupPullRequest{{PRStatusPullRequest: *base, HeadMatch: true}, {PRStatusPullRequest: *base, HeadMatch: false}}}
	lk.Boundary = hosting.PRStatusBoundary{Complete: true, Network: true, Requests: 1, MaxRequests: 1, Limitations: []string{"lookup-single-page"}}
	for _, compact := range []bool{true, false} {
		var c hostingCalls
		out, run := statusCommand(false, &c, hosting.PRStatusResult{}, lk)
		args := []string{"prs", sha}
		if compact {
			args = append(args, "--compact")
		}
		if e := run(args...); e != nil {
			t.Fatal(e)
		}
		text := out.out.String()
		if strings.ContainsAny(text, "\x1b\r") || !strings.Contains(text, "head_match=true") || !strings.Contains(text, "head_match=false") || !strings.Contains(text, "truncated") {
			t.Fatalf("%q", text)
		}
		if compact && strings.Count(text, "\n") != 3 {
			t.Fatalf("compact: one header line plus one line per PR: %q", text)
		}
	}
	var c hostingCalls
	out, run := statusCommand(false, &c, hosting.PRStatusResult{}, lk)
	if e := run("prs", sha, "--json"); e != nil {
		t.Fatal(e)
	}
	var got hosting.LookupResult
	if e := json.Unmarshal(out.out.Bytes(), &got); e != nil || got.Count != 2 || got.Kind != "prs" {
		t.Fatalf("%v %s", e, out.out)
	}
	lk.Status = "error"
	_, run = statusCommand(false, &c, hosting.PRStatusResult{}, lk)
	var failure runtimeFailure
	if e := run("prs", sha); e == nil || !asRuntimeFailure(e, &failure) {
		t.Fatalf("lookup error must exit 1: %v", e)
	}
}

type shortWriter struct{ n int }

func (w shortWriter) Write(p []byte) (int, error) { return w.n, nil }

// Human output is bounded like JSON (escape expansion counts), and a failing or short
// writer is a runtime failure, never silent success.
func TestHostingHumanOutputBoundAndWriterFailure(t *testing.T) {
	huge := strings.Repeat("\u0085", 400000)
	lk := hosting.LookupResult{SchemaVersion: 2, Kind: "prs", Status: "ok", Candidates: []hosting.Candidate{}, Diagnostics: []inspection.Diagnostic{}, Count: 1}
	lk.PullRequests = []hosting.LookupPullRequest{{PRStatusPullRequest: *okStatus(huge, "r").PullRequest}}
	for _, args := range [][]string{{"pr", "42", "--compact"}, {"pr", "42", "--expect-head", strings.Repeat("a", 40)}, {"prs", strings.Repeat("a", 40)}, {"prs", strings.Repeat("a", 40), "--compact"}} {
		var c hostingCalls
		out, run := statusCommand(false, &c, okStatus(huge, "r"), lk)
		var failure runtimeFailure
		if e := run(args...); e == nil || !asRuntimeFailure(e, &failure) || out.out.Len() > 1024 || strings.Contains(out.out.String(), "\\u0085") || !strings.Contains(out.out.String(), "hosting-output-limit") {
			t.Fatalf("%q: err=%v len=%d", args, e, out.out.Len())
		}
	}
	for _, w := range []io.Writer{shortWriter{3}, closedPipeWriter{}} {
		var c hostingCalls
		cmd := hostingCommandWithOps(false, nil,
			func(context.Context, string, hosting.Options) hosting.PRStatusResult {
				c.status++
				return okStatus("t", "r")
			},
			func(context.Context, string, hosting.Options) hosting.LookupResult { return lk })
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		cmd.SetOut(w)
		cmd.SetArgs([]string{"pr", "42", "--compact"})
		var failure runtimeFailure
		if e := cmd.ExecuteContext(context.Background()); e == nil || !asRuntimeFailure(e, &failure) {
			t.Fatalf("writer failure must exit 1: %v", e)
		}
	}
}

type closedPipeWriter struct{}

func (closedPipeWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
