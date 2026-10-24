// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// stateDirSeen runs one workspace subcommand without --state-dir and returns the directory the
// operation received.
func stateDirSeen(t *testing.T, operation string) (string, error) {
	t.Helper()
	var seen string
	called := false
	cmd := workspaceCommandWith(workspaceRunner(func(_ context.Context, _ string, p workspace.PlanOptions, a workspace.ApplyOptions, o workspace.Options) workspace.Result {
		called = true
		seen = p.StateDir + a.StateDir + o.StateDir
		return workspace.Result{SchemaVersion: 1, Status: "ok", Backend: "git"}
	}))
	args := []string{operation}
	switch operation {
	case "plan":
		args = append(args, "--base", "HEAD", "--destination", "/tmp/dest", "--task", "task", "--owner", "owner")
	case "apply":
		args = append(args, "--plan", "id", "--authorize")
	}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	if err == nil && !called {
		t.Fatal("no operation ran")
	}
	if err != nil && called {
		t.Fatalf("the operation ran although %v", err)
	}
	return seen, err
}

func TestWorkspaceUsesTheSharedDefaultStateDirectory(t *testing.T) {
	for _, operation := range []string{"plan", "apply", "handoff", "retire-plan"} {
		t.Run(operation, func(t *testing.T) {
			xdg, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("STW_STATE_DIR", "")
			t.Setenv("XDG_STATE_HOME", xdg)
			want := filepath.Join(xdg, "stw", "workspace")
			got, err := stateDirSeen(t, operation)
			if err != nil || got != want {
				t.Fatalf("%q %v", got, err)
			}
			if st, err := os.Stat(want); err != nil || st.Mode().Perm() != 0700 {
				t.Fatalf("not created 0700: %v %v", st, err)
			}
		})
	}
}

func TestWorkspaceStateDirectoryFromEnvironmentAndUnsafeRefusal(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "env")
	t.Setenv("STW_STATE_DIR", dir)
	if got, err := stateDirSeen(t, "plan"); err != nil || got != dir {
		t.Fatalf("%q %v", got, err)
	}
	if err = os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	_, err = stateDirSeen(t, "handoff")
	var failure runtimeFailure
	if !errors.As(err, &failure) {
		t.Fatalf("an unsafe $STW_STATE_DIR must be an operational refusal: %v", err)
	}
}

func TestWorkspaceUsageErrorsDoNotCreateTheDefault(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("STW_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", xdg)
	cmd := workspaceCommandWith(workspaceRunner(func(context.Context, string, workspace.PlanOptions, workspace.ApplyOptions, workspace.Options) workspace.Result {
		t.Fatal("operation ran")
		return workspace.Result{}
	}))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"plan"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("plan without flags accepted")
	}
	if entries, _ := os.ReadDir(xdg); len(entries) != 0 {
		t.Fatalf("a refused command created %v", entries)
	}
}

func TestWorkspaceUnsafeDefaultGivesAVersionedJSONRefusal(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "env")
	if err = os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STW_STATE_DIR", dir)
	cmd := workspaceCommandWith(workspaceRunner(func(context.Context, string, workspace.PlanOptions, workspace.ApplyOptions, workspace.Options) workspace.Result {
		t.Fatal("operation ran")
		return workspace.Result{}
	}))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"handoff", "--json"})
	var failure runtimeFailure
	if err = cmd.Execute(); !errors.As(err, &failure) {
		t.Fatalf("%v", err)
	}
	var result workspace.Result
	if err = json.NewDecoder(&out).Decode(&result); err != nil || result.SchemaVersion != 1 || result.Command != "workspace-handoff" || result.Status != "error" || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "unsafe-state" {
		t.Fatalf("%q %v", out.String(), err)
	}
}
