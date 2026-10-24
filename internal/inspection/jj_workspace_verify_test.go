// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// A stopped inspection says nothing about ownership. VerifyJJWorkspace must report
// a deadline or cancellation as such, so callers can retry or stay uncertain,
// and keep "foreign or ambiguous" for a destination that was observed and differs.
func TestVerifyJJWorkspaceReportsStoppedInspectionAsDeadline(t *testing.T) {
	const repo = "/repo/.jj/repo"
	source := Result{Status: "ok", Backend: "jj", JJRepository: &JJRepository{RepositoryDir: repo}}
	stopped := func(code string) Result {
		return Result{Status: "error", Backend: "jj", Diagnostics: []Diagnostic{{Code: code, Message: "stopped"}}}
	}
	observed := Result{Status: "ok", Backend: "jj", JJRepository: &JJRepository{RepositoryDir: repo, WorkspaceName: "other"}}
	for _, tc := range []struct {
		name     string
		results  []Result
		want     error
		foreign  bool
		wantText string
	}{
		{"source timeout", []Result{stopped("jj-timeout")}, context.DeadlineExceeded, false, ""},
		{"destination timeout", []Result{source, stopped("jj-timeout")}, context.DeadlineExceeded, false, ""},
		{"destination canceled", []Result{source, stopped("canceled")}, context.Canceled, false, ""},
		{"observed other workspace", []Result{source, observed}, nil, true, "foreign or ambiguous jj destination ownership"},
		{"observed ambiguity", []Result{source, {Status: "error", Backend: "ambiguous", Diagnostics: []Diagnostic{{Code: "ambiguous-backend", Message: "x"}}}}, nil, true, "foreign or ambiguous jj destination ownership"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			restore := verifyInspect
			t.Cleanup(func() { verifyInspect = restore })
			verifyInspect = func(context.Context, string) Result {
				r := tc.results[calls]
				calls++
				return r
			}
			_, err := VerifyJJWorkspace(context.Background(), "/source", "task", "/destination", repo)
			if err == nil {
				t.Fatal("verification of a stopped or foreign destination succeeded")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("stopped inspection hidden: %v, want %v", err, tc.want)
			}
			if tc.foreign != strings.Contains(err.Error(), "foreign or ambiguous") {
				t.Fatalf("wrong ownership wording: %v", err)
			}
		})
	}
}
