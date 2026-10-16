// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/inspection"
)

func reposCLIResult(status string) inspection.ReposResult {
	return inspection.ReposResult{
		SchemaVersion: 1,
		Command:       "repos",
		Status:        status,
		InputPaths:    []string{"/w/repo"},
		Boundary: inspection.ReposBoundary{
			Complete:    status == "ok",
			Freshness:   "sampled-nontransactional",
			Effects:     inspection.ReposDefaultEffects(),
			Limits:      inspection.ReposDefaultLimits(),
			Limitations: []string{"No ahead/behind facts."},
		},
		Entries: []inspection.ReposEntry{
			{
				Index:   0,
				Path:    "/w/repo",
				Origin:  "explicit",
				Status:  status,
				Backend: "git",
			},
		},
		Summary: inspection.ReposSummary{
			Entries: 1,
		},
	}
}

func TestReposUsageErrorsExitTwoBeforeTheOperation(t *testing.T) {
	for _, args := range [][]string{
		{},                     // 0 paths
		{"--bogus"},            // unknown flag
		{"--json=maybe"},       // invalid flag syntax
		{"--submodules=maybe"}, // invalid flag syntax
		{"a\x00b"},             // null byte
		{""},                   // empty path
		{"\xff"},               // invalid UTF-8
	} {
		called := false
		command := reposCommandWith(func(context.Context, inspection.ReposOptions) inspection.ReposResult {
			called = true
			return reposCLIResult("ok")
		})
		command.SetArgs(args)
		var stdout, stderr bytes.Buffer
		command.SetOut(&stdout)
		command.SetErr(&stderr)
		err := command.ExecuteContext(context.Background())
		var runtime runtimeFailure
		if err == nil || errors.As(err, &runtime) || called || stdout.Len() != 0 {
			t.Fatalf("args %v: unexpected success or runtime failure (called=%v, stdout=%q, err=%v)", args, called, stdout.String(), err)
		}
	}
}

func TestReposExitStatuses(t *testing.T) {
	cases := []struct {
		status   string
		wantExit int
	}{
		{"ok", 0},
		{"partial", 1},
		{"error", 1},
	}

	for _, c := range cases {
		command := reposCommandWith(func(context.Context, inspection.ReposOptions) inspection.ReposResult {
			return reposCLIResult(c.status)
		})
		command.SetArgs([]string{"/w/repo"})
		var stdout, stderr bytes.Buffer
		command.SetOut(&stdout)
		command.SetErr(&stderr)
		err := command.ExecuteContext(context.Background())
		var runtime runtimeFailure
		if c.wantExit == 0 && err != nil {
			t.Fatalf("status %s: want exit 0, got err: %v", c.status, err)
		}
		if c.wantExit == 1 && (err == nil || !errors.As(err, &runtime)) {
			t.Fatalf("status %s: want runtimeFailure (exit 1), got: %v", c.status, err)
		}
	}
}

func TestReposJSONFlagEmitsJSON(t *testing.T) {
	command := reposCommandWith(func(context.Context, inspection.ReposOptions) inspection.ReposResult {
		return reposCLIResult("ok")
	})
	command.SetArgs([]string{"--json", "/w/repo"})
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nOutput: %s", err, stdout.String())
	}
	if res["command"] != "repos" || res["status"] != "ok" {
		t.Fatalf("unexpected JSON content: %v", res)
	}
}

func TestReposTextOutput(t *testing.T) {
	res := reposCLIResult("ok")
	res.Entries[0].Facts = &inspection.ReposFacts{
		HeadState: "attached",
		HeadRef:   func(s string) *string { return &s }("refs/heads/main"),
		Dirty:     inspection.ReposDirty{Value: "true"},
		Workspaces: inspection.ReposWorkspacesFacts{
			Total: 1,
		},
	}
	command := reposCommandWith(func(context.Context, inspection.ReposOptions) inspection.ReposResult {
		return res
	})
	command.SetArgs([]string{"/w/repo"})
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "SourceSteward repos: ok") {
		t.Fatalf("missing header in text output: %s", out)
	}
	if !strings.Contains(out, "/w/repo") {
		t.Fatalf("missing entry path in text output: %s", out)
	}
	if !strings.Contains(out, "summary:") {
		t.Fatalf("missing summary in text output: %s", out)
	}
}

func TestReposRegisteredInRoot(t *testing.T) {
	root, _ := newRoot("dev", &bytes.Buffer{}, &bytes.Buffer{})
	cmd, _, err := root.Find([]string{"repos"})
	if err != nil || cmd == nil || cmd.Name() != "repos" {
		t.Fatalf("repos command not registered in root: %v", err)
	}
}
