// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/carryover"
)

// carryOverFake answers every question as a pure rebase (one commit, equal patch-id, no merges).
type carryOverFake struct{ merges []string }

func (carryOverFake) FetchPull(context.Context, string, int) (string, error) { return "", nil }
func (carryOverFake) HasCommit(context.Context, string) (bool, error)        { return true, nil }
func (carryOverFake) PatchID(context.Context, string, string) (string, error) {
	return strings.Repeat("9", 40), nil
}
func (f carryOverFake) Merges(context.Context, string, string) ([]string, error) {
	return f.merges, nil
}
func (carryOverFake) Commits(context.Context, string, string) ([]carryover.CommitPatch, error) {
	return []carryover.CommitPatch{{Message: "subject\n", Patch: "\ndiff --git a/f b/f\n"}}, nil
}

func carryOverRun(t *testing.T, g carryover.Git, args ...string) (int, string, string) {
	t.Helper()
	carryOverGit = g
	t.Cleanup(func() { carryOverGit = nil })
	var stdout, stderr bytes.Buffer
	status := Run(context.Background(), "test", append([]string{"review", "carry-over"}, args...), &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

func carryOverArgs(extra ...string) []string {
	s := func(c string) string { return strings.Repeat(c, 40) }
	return append([]string{s("1"), s("2"), s("3"), s("4"), "--pr", "12"}, extra...)
}

func TestReviewCarryOverYesPrintsTheLineLastAndExitsZero(t *testing.T) {
	status, out, errOut := carryOverRun(t, carryOverFake{}, carryOverArgs()...)
	want := "Carried-over: from=" + strings.Repeat("2", 40) + " to=" + strings.Repeat("4", 40) + " patch-id=" + strings.Repeat("9", 40) + " pr=12"
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if status != 0 || lines[len(lines)-1] != want || errOut != "" {
		t.Fatalf("status %d\n%s\n%s", status, out, errOut)
	}
}

func TestReviewCarryOverNoExitsOneWithoutALine(t *testing.T) {
	status, out, errOut := carryOverRun(t, carryOverFake{merges: []string{strings.Repeat("7", 40)}}, carryOverArgs()...)
	if status != 1 || strings.Contains(out, "Carried-over:") || !strings.Contains(out, "carry-over: no") || !strings.Contains(errOut, "merge-commits") {
		t.Fatalf("status %d\n%s\n%s", status, out, errOut)
	}
}

func TestReviewCarryOverJSON(t *testing.T) {
	status, out, _ := carryOverRun(t, carryOverFake{}, carryOverArgs("--json")...)
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil || status != 0 || m["carry_over"] != true || m["pr"] != float64(12) {
		t.Fatalf("status %d %v\n%s", status, err, out)
	}
	status, out, _ = carryOverRun(t, carryOverFake{merges: []string{strings.Repeat("7", 40)}}, carryOverArgs("--json")...)
	m = nil
	if err := json.Unmarshal([]byte(out), &m); err != nil || status != 1 || m["carry_over"] != false || m["line"] != nil {
		t.Fatalf("status %d %v\n%s", status, err, out)
	}
}

func TestReviewCarryOverUsageErrorsExitTwoBeforeAnyGit(t *testing.T) {
	good := carryOverArgs()
	for name, args := range map[string][]string{
		"no arguments":     {},
		"three ids":        good[:3],
		"missing pr":       good[:4],
		"ref name":         append([]string{"main"}, good[1:]...),
		"leading dash":     append([]string{"--output=/tmp/x"}, good[1:]...),
		"range":            append([]string{"a..b"}, good[1:]...),
		"space":            append([]string{"a b"}, good[1:]...),
		"bad pr":           append(append([]string{}, good[:4]...), "--pr", "0"),
		"remote with dash": carryOverArgs("--remote", "-u"),
		"unknown flag":     carryOverArgs("--post"),
	} {
		status, out, _ := carryOverRun(t, failingGit{t}, args...)
		if status != 2 || strings.Contains(out, "Carried-over:") {
			t.Errorf("%s: status %d\n%s", name, status, out)
		}
	}
}

// failingGit fails the test on any call: usage errors must not reach Git.
type failingGit struct{ t *testing.T }

func (f failingGit) FetchPull(context.Context, string, int) (string, error) {
	f.t.Fatal("git was called")
	return "", nil
}
func (f failingGit) HasCommit(context.Context, string) (bool, error) {
	f.t.Fatal("git was called")
	return false, nil
}
func (f failingGit) PatchID(context.Context, string, string) (string, error) {
	f.t.Fatal("git was called")
	return "", nil
}
func (f failingGit) Merges(context.Context, string, string) ([]string, error) {
	f.t.Fatal("git was called")
	return nil, nil
}
func (f failingGit) Commits(context.Context, string, string) ([]carryover.CommitPatch, error) {
	f.t.Fatal("git was called")
	return nil, nil
}

func TestReviewHelpNamesTheSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := Run(context.Background(), "test", []string{"review", "carry-over", "--help"}, &stdout, &stderr); status != 0 || !strings.Contains(stdout.String(), "posts nothing") {
		t.Fatalf("status %d\n%s%s", status, stdout.String(), stderr.String())
	}
}
