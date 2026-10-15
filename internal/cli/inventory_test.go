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

func TestInventoryUsageBeforeBackend(t *testing.T) {
	for _, args := range [][]string{{"--query", "ancestor", "--from", "one"}, {"--query", "ref-exists"}, {"--ref", "main"}, {"--query", "ref-exists", "--ref", "main", "--scope", "disk"}, {"--scope", "bogus"}, {"--path", "../outside"}, {"--anchor", "bogus"}, {"one", "two"}} {
		called := false
		command := inventoryCommandWith(func(context.Context, string, inspection.InventoryOptions) inspection.InventoryResult {
			called = true
			return inspection.InventoryResult{}
		})
		command.SetArgs(args)
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		err := command.ExecuteContext(context.Background())
		var runtime runtimeFailure
		if err == nil || errors.As(err, &runtime) || called {
			t.Fatalf("args %q err=%v called=%t", args, err, called)
		}
	}
}
func TestInventoryFlagsAndLiteralArguments(t *testing.T) {
	var got inspection.InventoryOptions
	command := inventoryCommandWith(func(_ context.Context, path string, o inspection.InventoryOptions) inspection.InventoryResult {
		got = o
		if path != "repo" {
			t.Fatalf("path %q", path)
		}
		return inventoryCLIResult("ok", "false")
	})
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetArgs([]string{"repo", "--json", "--query", "dirty", "--scope", "disk", "--scope", "untracked", "--path", "literal,*\nname", "--path", "other", "--remote", "upstream", "--remote", "backup", "--base", "base", "--revision", "tip", "--anchor", "cwd"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if got.Query != "dirty" || got.Base != "base" || got.Revision != "tip" || got.Anchor != "cwd" || len(got.Scopes) != 2 || len(got.Paths) != 2 || got.Paths[0] != "literal,*\nname" || len(got.Remotes) != 2 {
		t.Fatalf("%+v", got)
	}
	var result inspection.InventoryResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Query.Value != "false" {
		t.Fatalf("%s %v", out.String(), err)
	}
}
func inventoryCLIResult(status, value string) inspection.InventoryResult {
	return inspection.InventoryResult{SchemaVersion: 1, Command: "inventory", Status: status, Backend: "git", InputPath: "/repo", Query: &inspection.InventoryQuery{Predicate: "dirty", Value: value, Reason: "sampled"}, Boundary: inspection.InventoryBoundary{Complete: status == "ok", Limitations: []string{"not transactional"}}, Workspaces: []inspection.InventoryWorkspace{}, References: []inspection.InventoryReference{}, Bases: []inspection.InventoryBase{}, Files: []inspection.InventoryFile{}, Diagnostics: []inspection.Diagnostic{}}
}
func TestInventoryResultExits(t *testing.T) {
	for _, tc := range []struct {
		status, value string
		failed        bool
	}{{"ok", "true", false}, {"ok", "false", false}, {"partial", "unknown", true}, {"error", "unknown", true}, {"ok", "unknown", true}} {
		for _, jsonOutput := range []bool{false, true} {
			command := inventoryCommandWith(func(context.Context, string, inspection.InventoryOptions) inspection.InventoryResult {
				return inventoryCLIResult(tc.status, tc.value)
			})
			args := []string{}
			if jsonOutput {
				args = append(args, "--json")
			}
			command.SetArgs(args)
			var out bytes.Buffer
			command.SetOut(&out)
			err := command.Execute()
			var failure runtimeFailure
			if (err != nil) != tc.failed || (err != nil && !errors.As(err, &failure)) {
				t.Fatalf("%+v err=%v", tc, err)
			}
			if out.Len() == 0 {
				t.Fatal("missing result")
			}
		}
	}
}
func TestInventoryTextQuotesRepositoryData(t *testing.T) {
	r := inventoryCLIResult("partial", "unknown")
	path := "file\nError: forged\x1b[31m"
	r.Files = []inspection.InventoryFile{{Path: path, Scope: "disk", Change: "modified"}}
	r.Boundary.Limitations = []string{path}
	r.Query.Reason = path
	r.Diagnostics = []inspection.Diagnostic{{Code: "example", Message: path}}
	var out bytes.Buffer
	if err := renderInventoryText(&out, r); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\nError: forged") || strings.ContainsRune(out.String(), '\x1b') || !strings.Contains(out.String(), `file\nError: forged\x1b[31m`) {
		t.Fatal(out.String())
	}
}
func TestInventoryWriterFailures(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		command := inventoryCommandWith(func(context.Context, string, inspection.InventoryOptions) inspection.InventoryResult {
			return inventoryCLIResult("ok", "false")
		})
		args := []string{}
		if jsonOutput {
			args = append(args, "--json")
		}
		command.SetArgs(args)
		command.SetOut(&failOnWrite{fail: 1})
		var failure runtimeFailure
		if err := command.Execute(); !errors.As(err, &failure) {
			t.Fatalf("%v", err)
		}
	}
}
func TestInventoryHelpIsLocal(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var out, diagnostic bytes.Buffer
	if code := Run(context.Background(), "dev", []string{"inventory", "--help"}, &out, &diagnostic); code != 0 || diagnostic.Len() != 0 || !strings.Contains(out.String(), "native jj") {
		t.Fatalf("%d %s %s", code, out.String(), diagnostic.String())
	}
}

func TestInventoryFailureQuotesDiagnostic(t *testing.T) {
	r := inventoryCLIResult("error", "unknown")
	r.Diagnostics = []inspection.Diagnostic{{Code: "failure", Message: "repository\x1b[31m\nforged"}}
	command := inventoryCommandWith(func(context.Context, string, inspection.InventoryOptions) inspection.InventoryResult { return r })
	command.SetOut(&bytes.Buffer{})
	command.SetArgs([]string{"--json"})
	err := command.Execute()
	if err == nil || strings.ContainsRune(err.Error(), '\x1b') || strings.ContainsRune(err.Error(), '\n') || !strings.Contains(err.Error(), `\x1b`) {
		t.Fatalf("unsafe diagnostic %v", err)
	}
}
