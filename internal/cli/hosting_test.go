// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/inspection"
)

func TestHostingCommandsShareOptions(t *testing.T) {
	for _, jj := range []bool{false, true} {
		for _, kind := range []string{"repo", "pr", "ci"} {
			t.Run(kind+map[bool]string{false: "-stw", true: "-jj"}[jj], func(t *testing.T) {
				var got hosting.Options
				var path string
				calls := 0
				cmd := hostingCommandWith(jj, func(_ context.Context, p string, o hosting.Options) hosting.Result {
					calls++
					got = o
					path = p
					return hosting.Result{SchemaVersion: 1, Kind: o.Kind, Status: "ok", Backend: "jj"}
				})
				var out bytes.Buffer
				cmd.SetOut(&out)
				args := []string{kind}
				if kind != "repo" {
					args = append(args, "42")
				}
				args = append(args, "space path", "--repo", "owner/repo", "--remote", "upstream", "--host", "github.com", "--json")
				cmd.SetArgs(args)
				if e := cmd.ExecuteContext(context.Background()); e != nil {
					t.Fatal(e)
				}
				if calls != 1 || path != "space path" || got.Kind != kind || got.RequireJJ != jj || got.Repo != "owner/repo" || got.Remote != "upstream" {
					t.Fatalf("%d %q %#v", calls, path, got)
				}
				if kind != "repo" && got.Number != 42 {
					t.Fatal(got)
				}
				if !strings.Contains(out.String(), `"schema_version":1`) {
					t.Fatal(out.String())
				}
			})
		}
	}
}
func TestHostingUsageDoesNotExecute(t *testing.T) {
	for _, args := range [][]string{{"pr"}, {"pr", "+1"}, {"ci", "0"}, {"ci", "1", "--repo", "../x"}, {"repo", "--host", "https://bad"}, {"repo", "a", "b"}} {
		calls := 0
		cmd := hostingCommandWith(false, func(context.Context, string, hosting.Options) hosting.Result { calls++; return hosting.Result{} })
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		cmd.SetArgs(args)
		if e := cmd.ExecuteContext(context.Background()); e == nil || calls != 0 {
			t.Fatalf("%q calls%d err%v", args, calls, e)
		}
	}
}
func TestHostingErrorsAndHumanEscaping(t *testing.T) {
	cmd := hostingCommandWith(false, func(context.Context, string, hosting.Options) hosting.Result {
		return hosting.Result{SchemaVersion: 1, Kind: "pr", Status: "error", Backend: "jj", PullRequest: &hosting.PullRequest{Number: 42, Title: "remote\x1b[31m\ntext"}}
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"pr", "42"})
	e := cmd.ExecuteContext(context.Background())
	var failure runtimeFailure
	if !errors.As(e, &failure) || strings.ContainsAny(out.String(), "\x1b\r") || !strings.Contains(out.String(), `\x1b`) {
		t.Fatalf("%v %q", e, out.String())
	}
}
func TestGHJJHelpIsLocalAndBoundedSurface(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, args := range [][]string{{"--help"}, {"repo", "--help"}, {"pr", "--help"}, {"ci", "--help"}, {"version", "--json"}} {
		var out, diag bytes.Buffer
		if code := RunGHJJ(context.Background(), "test", args, &out, &diag); code != 0 || diag.Len() != 0 {
			t.Fatalf("%q %d %s", args, code, diag.String())
		}
	}
	var out, diag bytes.Buffer
	if code := RunGHJJ(context.Background(), "test", []string{"workspace"}, &out, &diag); code != 2 {
		t.Fatalf("%d %s", code, diag.String())
	}
}

func TestHostingHumanRetainsNativeJJIdentityAndQualification(t *testing.T) {
	var out bytes.Buffer
	r := hosting.Result{Kind: "repo", Status: "ok", Backend: "jj", Identity: &inspection.Result{Backend: "jj", JJRepository: &inspection.JJRepository{ChangeID: "change", CommitID: "commit", OperationID: "operation", Observation: inspection.JJObservation{WorkingCopy: "stored-unsnapshotted", Limitation: "Disk freshness is unknown; no snapshot."}}}}
	if e := renderHosting(&out, r); e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"change", "commit", "operation", "stored-unsnapshotted", "Disk freshness is unknown"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
}
