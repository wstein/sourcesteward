// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
)

func TestReadyCLIUsageHasNoEffects(t *testing.T) {
	for _, args := range [][]string{{}, {"0", "--policy-lock", "lock"}, {"+1", "--policy-lock", "lock"}, {"1"}, {"1", "--policy-lock", ""}, {"1", "--policy-lock", "lock", "--repo", "../x"}, {"1", "--policy-lock", "lock", "--host", "https://bad"}, {"1", "--policy-lock", "lock", "a", "b"}} {
		calls := 0
		cmd := readyCommand(false, func(context.Context, string, hosting.ReadyOptions) hosting.ReadyResult {
			calls++
			return hosting.ReadyResult{}
		})
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		var out, diag bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&diag)
		cmd.SetArgs(args)
		err := cmd.ExecuteContext(context.Background())
		if err == nil || calls != 0 || out.Len() != 0 || exitStatus(io.Discard, err) != 2 {
			t.Fatalf("args%q calls%d err%v out%q", args, calls, err, out.String())
		}
	}
}
func TestReadyCLIOptionsOutputAndExit(t *testing.T) {
	for _, jj := range []bool{false, true} {
		for _, decision := range []string{"eligible", "blocked", "indeterminate", "error"} {
			var got hosting.ReadyOptions
			var path string
			fixture := hosting.ReadyResult{SchemaVersion: 1, Kind: "ready-check", Decision: decision, ObservedAt: "2026-10-18T00:00:00Z"}
			cmd := readyCommand(jj, func(_ context.Context, p string, o hosting.ReadyOptions) hosting.ReadyResult {
				got = o
				path = p
				return fixture
			})
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs([]string{"7", "space path", "--policy-lock", "outside lock", "--repo", "o/r", "--host", "github.com", "--remote", "upstream", "--json"})
			err := cmd.ExecuteContext(context.Background())
			if got.Number != 7 || got.RequireJJ != jj || got.PolicyLock != "outside lock" || got.Repo != "o/r" || got.Remote != "upstream" || path != "space path" {
				t.Fatalf("options %+v path%q", got, path)
			}
			var decoded hosting.ReadyResult
			if json.Unmarshal(out.Bytes(), &decoded) != nil || decoded.Decision != decision {
				t.Fatalf("output%q", out.String())
			}
			want := 1
			if decision == "eligible" {
				want = 0
			}
			code := 0
			if err != nil {
				code = exitStatus(io.Discard, err)
			}
			if code != want {
				t.Fatalf("decision%s code%d", decision, code)
			}
		}
	}
}

type readyBrokenWriter struct{}

func (readyBrokenWriter) Write([]byte) (int, error) { return 0, errors.New("writer failed") }
func TestReadyCLIRegistrationHelpEscapingAndWriter(t *testing.T) {
	for _, jj := range []bool{false, true} {
		found := false
		for _, cmd := range hostingCommands(jj, hosting.Inspect) {
			if cmd.Name() == "ready-check" {
				found = true
				var out bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetArgs([]string{"--help"})
				if err := cmd.ExecuteContext(context.Background()); err != nil || !strings.Contains(out.String(), "--policy-lock") {
					t.Fatalf("help %s %v", out.String(), err)
				}
			}
		}
		if !found {
			t.Fatal("missing registration")
		}
	}
	result := hosting.ReadyResult{SchemaVersion: 1, Kind: "ready-check", Decision: "indeterminate", ObservedAt: "fixed", Identity: json.RawMessage(`{"backend":"git","label":"private\u001b[31m\n\u202etext"}`)}
	var out bytes.Buffer
	if err := renderReady(&out, result); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.String(), "\x1b\u202e") || !strings.Contains(out.String(), "Eligibility under the named policy") {
		t.Fatalf("unsafe human %q", out.String())
	}
	cmd := readyCommand(false, func(context.Context, string, hosting.ReadyOptions) hosting.ReadyResult { return result })
	cmd.SetOut(readyBrokenWriter{})
	cmd.SetArgs([]string{"7", "--policy-lock", "lock", "--json"})
	if err := cmd.ExecuteContext(context.Background()); exitStatus(io.Discard, err) != 1 {
		t.Fatalf("writer failure %v", err)
	}
}

type readyShortWriter struct{}

func (readyShortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }
func TestReadyCLIWriterBoundsAndHumanJSONAgreement(t *testing.T) {
	// Deliberately hostile display fixture, not authenticated policy authority.
	var result hosting.ReadyResult
	if err := json.Unmarshal([]byte(`{"schema_version":1,"kind":"ready-check","decision":"blocked","observed_at":"2026-10-18T00:00:00Z","backend":"git","policy":{"id":"generic\u202epolicy","authority":"caller-supplied"},"pull_request":{"number":7,"head_sha":"head","base_sha":"base"},"components":{"review":{"state":"fail","complete":true,"reasons":[{"code":"source-denied","subject":"field\u001b\n\u202ex"}]}}}`), &result); err != nil {
		t.Fatal(err)
	}
	var human bytes.Buffer
	if err := renderReady(&human, result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{strconv.Quote(result.Decision), strconv.Quote(result.Policy.ID), strconv.Quote(result.PullRequest.HeadSHA), strconv.Quote(result.PullRequest.BaseSHA), strconv.Quote(result.Components.Review.Reasons[0].Code), strconv.Quote(result.Components.Review.Reasons[0].Subject)} {
		if !strings.Contains(human.String(), want) {
			t.Fatalf("missing shared value %s", want)
		}
	}
	if strings.ContainsAny(human.String(), "\x1b\u202e") {
		t.Fatal("unescaped control")
	}
	if !errors.Is(renderReady(readyShortWriter{}, result), io.ErrShortWrite) {
		t.Fatal("short write passed")
	}
	for _, asJSON := range []bool{false, true} {
		result.Policy.ID = strings.Repeat("x", 1024*1024)
		cmd := readyCommand(false, func(context.Context, string, hosting.ReadyOptions) hosting.ReadyResult { return result })
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		var out bytes.Buffer
		cmd.SetOut(&out)
		args := []string{"7", "--policy-lock", "lock"}
		if asJSON {
			args = append(args, "--json")
		}
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(context.Background()); err == nil || exitStatus(io.Discard, err) != 1 || out.Len() != 0 {
			t.Fatal("oversized partial output")
		}
	}
	for _, extension := range []bool{false, true} {
		var out, diag bytes.Buffer
		args := []string{"ready-check", "0", "--json"}
		run := RunGHJJ
		if !extension {
			run = Run
			args = append([]string{"hosting"}, args...)
		}
		if code := run(context.Background(), "test", args, &out, &diag); code != 2 || out.Len() != 0 {
			t.Fatalf("entrypoint usage %d %q", code, out.String())
		}
	}
}

func TestReadyCLIProductionEntryPointsNativeFailureJSON(t *testing.T) {
	for _, extension := range []bool{false, true} {
		var out, diag bytes.Buffer
		path := t.TempDir()
		args := []string{"ready-check", "7", path, "--policy-lock", "/private-lock-marker/not-read", "--json"}
		run := RunGHJJ
		if !extension {
			run = Run
			args = append([]string{"hosting"}, args...)
		}
		code := run(context.Background(), "test", args, &out, &diag)
		var got hosting.ReadyResult
		if code != 1 || json.Unmarshal(out.Bytes(), &got) != nil || got.Kind != "ready-check" || got.Decision != "error" || got.Boundary.NetworkAttempted || strings.Contains(out.String(), "private-lock-marker") {
			t.Fatalf("production entry code%d out%s diag%s", code, out.String(), diag.String())
		}
	}
}
