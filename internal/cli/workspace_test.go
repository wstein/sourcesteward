// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/workspace"
)

func workspaceRunner(run func(context.Context, string, workspace.PlanOptions, workspace.ApplyOptions, workspace.Options) workspace.Result) workspaceOperations {
	return workspaceOperations{
		plan: func(c context.Context, o workspace.PlanOptions) workspace.Result {
			return run(c, "workspace-plan", o, workspace.ApplyOptions{}, workspace.Options{})
		},
		apply: func(c context.Context, o workspace.ApplyOptions) workspace.Result {
			return run(c, "workspace-apply", workspace.PlanOptions{}, o, workspace.Options{})
		},
		handoff: func(c context.Context, o workspace.Options) workspace.Result {
			return run(c, "workspace-handoff", workspace.PlanOptions{}, workspace.ApplyOptions{}, o)
		},
		retire: func(c context.Context, o workspace.Options) workspace.Result {
			return run(c, "workspace-retire-plan", workspace.PlanOptions{}, workspace.ApplyOptions{}, o)
		},
	}
}
func TestWorkspaceUsageBeforeOperation(t *testing.T) {
	for _, args := range [][]string{
		{"plan"}, {"plan", "--base", "HEAD", "--destination", "/tmp/dest", "--task", "task"},
		{"plan", "one", "two"}, {"apply", "--state-dir", "/tmp/state"}, {"apply", "--plan", "id", "--state-dir", "/tmp/state", "--base", "HEAD"},
		{"handoff", "one", "two"}, {"retire-plan", "--state-dir", "/tmp/state", "--authorize"}, {"unknown"},
		{"apply", "--plan", "../escape", "--state-dir", "/tmp/state"},
		{"handoff", strings.Repeat("x", 4097), "--state-dir", "/tmp/state"},
	} {
		called := false
		cmd := workspaceCommandWith(workspaceRunner(func(context.Context, string, workspace.PlanOptions, workspace.ApplyOptions, workspace.Options) workspace.Result {
			called = true
			return workspace.Result{Status: "ok"}
		}))
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		err := cmd.Execute()
		var failure runtimeFailure
		if err == nil || errors.As(err, &failure) || called {
			t.Fatalf("args %q err=%v called=%t", args, err, called)
		}
	}
}
func TestWorkspaceCommandsAndLiteralFlags(t *testing.T) {
	for _, operation := range []string{"plan", "apply", "handoff", "retire-plan"} {
		cmd := workspaceCommandWith(workspaceRunner(func(_ context.Context, name string, p workspace.PlanOptions, a workspace.ApplyOptions, o workspace.Options) workspace.Result {
			if name != "workspace-"+operation {
				t.Fatal(name)
			}
			switch operation {
			case "plan":
				if p.Path != "literal\nrepo" || p.Base != "HEAD" || p.Destination != "/tmp/dest" || p.Task != "task" || p.Owner != "owner" || p.StateDir != "/tmp/state" {
					t.Fatalf("%+v", p)
				}
			case "apply":
				if a.PlanID != "id" || a.StateDir != "/tmp/state" || !a.Authorize {
					t.Fatalf("%+v", a)
				}
			default:
				if o.Path != "literal\nrepo" || o.StateDir != "/tmp/state" {
					t.Fatalf("%+v", o)
				}
			}
			return workspace.Result{SchemaVersion: 1, Command: name, Status: "ok", Backend: "git"}
		}))
		args := []string{operation, "--state-dir", "/tmp/state", "--json"}
		switch operation {
		case "plan":
			args = append(args, "literal\nrepo", "--base", "HEAD", "--destination", "/tmp/dest", "--task", "task", "--owner", "owner")
		case "apply":
			args = append(args, "--plan", "id", "--authorize")
		default:
			args = append(args, "literal\nrepo")
		}
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var result workspace.Result
		if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Command != "workspace-"+operation {
			t.Fatalf("%s %v", out.String(), err)
		}
	}
}
func TestWorkspaceRefusalsAndWriterErrors(t *testing.T) {
	for _, status := range []string{"ok", "refused", "partial", "error"} {
		for _, structured := range []bool{false, true} {
			cmd := workspaceCommandWith(workspaceRunner(func(context.Context, string, workspace.PlanOptions, workspace.ApplyOptions, workspace.Options) workspace.Result {
				return workspace.Result{SchemaVersion: 1, Command: "workspace-apply", Status: status, Backend: "unknown", Limitations: []string{"literal\nwarning\x1b[31m"}}
			}))
			args := []string{"apply", "--plan", "id", "--state-dir", "/tmp/state"}
			if structured {
				args = append(args, "--json")
			}
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(args)
			err := cmd.Execute()
			var failure runtimeFailure
			if (err != nil) != (status != "ok") || (err != nil && !errors.As(err, &failure)) {
				t.Fatalf("status %s err %v", status, err)
			}
			if out.Len() == 0 || strings.ContainsRune(out.String(), '\x1b') {
				t.Fatalf("unsafe/missing output %q", out.String())
			}
			cmd = workspaceCommandWith(workspaceRunner(func(context.Context, string, workspace.PlanOptions, workspace.ApplyOptions, workspace.Options) workspace.Result {
				return workspace.Result{Status: "ok"}
			}))
			cmd.SetOut(&failOnWrite{fail: 1})
			cmd.SetArgs(args)
			if err := cmd.Execute(); !errors.As(err, &failure) {
				t.Fatalf("writer %v", err)
			}
		}
	}
}
func TestWorkspaceCancellationAfterOperation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := workspaceCommandWith(workspaceRunner(func(context.Context, string, workspace.PlanOptions, workspace.ApplyOptions, workspace.Options) workspace.Result {
		cancel()
		return workspace.Result{Status: "ok"}
	}))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"handoff", "--state-dir", "/tmp/state"})
	var failure runtimeFailure
	if err := cmd.ExecuteContext(ctx); !errors.As(err, &failure) {
		t.Fatalf("%v", err)
	}
}
func TestWorkspaceHelpWithoutBackends(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, args := range [][]string{{"workspace", "--help"}, {"workspace", "plan", "--help"}, {"workspace", "apply", "--help"}, {"workspace", "handoff", "--help"}, {"workspace", "retire-plan", "--help"}} {
		var out, diagnostic bytes.Buffer
		if code := Run(context.Background(), "dev", args, &out, &diagnostic); code != 0 || out.Len() == 0 || diagnostic.Len() != 0 {
			t.Fatalf("%q %d %s", args, code, diagnostic.String())
		}
	}
}

func TestWorkspaceHelpLinksImplementedRetirement(t *testing.T) {
	var out, diagnostic bytes.Buffer
	code := Run(context.Background(), "test", []string{"workspace", "--help"}, &out, &diagnostic)
	if code != 0 || !strings.Contains(out.String(), "stw tidy") || strings.Contains(out.String(), "retirement execution remains planned") {
		t.Fatalf("workspace help must point to implemented retirement: code%d %s %s", code, out.String(), diagnostic.String())
	}
}

func TestWorkspacePlanRendersSourceContext(t *testing.T) {
	realCaller := "/other/checkout"
	sha := strings.Repeat("a", 40)
	other := strings.Repeat("b", 40)
	run := func(extra ...string) string {
		cmd := workspaceCommandWith(workspaceRunner(func(_ context.Context, name string, _ workspace.PlanOptions, _ workspace.ApplyOptions, _ workspace.Options) workspace.Result {
			return workspace.Result{SchemaVersion: 1, Command: name, Status: "ok", Backend: "git", Plan: &workspace.MutationPlan{PlanID: "p1", SourcePath: "/src", Destination: "/dst"},
				Source: &workspace.SourceContext{SourcePath: "/src", BaseSelector: "HEAD", BaseCommit: sha, SourceHead: sha, CallerDirectory: realCaller, CallerCheckout: realCaller, CallerHead: other, CallerDiffers: true, Notice: "caller differs"}}
		}))
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(append([]string{"plan", "/src", "--base", "HEAD", "--destination", "/dst", "--task", "t", "--owner", "o", "--state-dir", "/tmp/state"}, extra...))
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	human := run()
	for _, want := range []string{"Source checkout: \"/src\"", "Base: \"HEAD\" resolved to " + `"` + sha + `"`, "Source HEAD: \"" + sha + "\"", "Caller directory: \"" + realCaller + "\"", "Notice: \"caller differs\""} {
		if !strings.Contains(human, want) {
			t.Fatalf("human output lacks %q:\n%s", want, human)
		}
	}
	js := run("--json")
	var decoded struct {
		Source map[string]any `json:"source_context"`
	}
	if err := json.Unmarshal([]byte(js), &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"source_path", "base_selector", "base_commit", "source_head", "caller_directory", "caller_checkout", "caller_head", "caller_differs", "notice"} {
		if _, ok := decoded.Source[key]; !ok {
			t.Fatalf("json lacks source_context.%s: %s", key, js)
		}
	}
}

func TestWorkspacePlanRendersTheNextCall(t *testing.T) {
	run := func(extra ...string) string {
		cmd := workspaceCommandWith(workspaceRunner(func(_ context.Context, name string, _ workspace.PlanOptions, _ workspace.ApplyOptions, _ workspace.Options) workspace.Result {
			return workspace.Result{SchemaVersion: 1, Command: name, Status: "ok", Backend: "git", Plan: &workspace.MutationPlan{PlanID: "p1", SourcePath: "/src", Destination: "/dst"},
				Next: &workspace.NextCall{Command: "stw workspace apply --plan p1 --state-dir /s --authorize", Argv: []string{"stw", "workspace", "apply", "--plan", "p1", "--state-dir", "/s", "--authorize"}}}
		}))
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(append([]string{"plan", "/src", "--base", "HEAD", "--destination", "/dst", "--task", "t", "--owner", "o", "--state-dir", "/tmp/state"}, extra...))
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	human := run()
	if !strings.Contains(human, "Next: stw workspace apply --plan p1 --state-dir /s --authorize\nNothing is applied until you run it; running it is the authorization of the effects above.\n") {
		t.Fatalf("human output:\n%s", human)
	}
	var decoded struct {
		Next struct {
			Command string   `json:"command"`
			Argv    []string `json:"argv"`
		} `json:"next_call"`
	}
	if err := json.Unmarshal([]byte(run("--json")), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Next.Argv) != 8 || decoded.Next.Argv[2] != "apply" || decoded.Next.Command == "" {
		t.Fatalf("%+v", decoded)
	}
}

func TestWorkspacePlanWithAnUnprintableCallPointsToJSONArgv(t *testing.T) {
	cmd := workspaceCommandWith(workspaceRunner(func(_ context.Context, name string, _ workspace.PlanOptions, _ workspace.ApplyOptions, _ workspace.Options) workspace.Result {
		return workspace.Result{SchemaVersion: 1, Command: name, Status: "ok", Backend: "git", Next: &workspace.NextCall{Argv: []string{"stw", "workspace", "apply", "--state-dir", "/s\x1b"}}}
	}))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"plan", "/src", "--base", "HEAD", "--destination", "/dst", "--task", "t", "--owner", "o", "--state-dir", "/tmp/state"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if text := out.String(); strings.Contains(text, "\x1b") || !strings.Contains(text, "next_call.argv") || !strings.Contains(text, "Nothing is applied until you run it") {
		t.Fatalf("%q", text)
	}
}

// Only workspace plan offers a prefabricated apply call; tidy output must not carry one
// (owner decision on #192).
func TestTidyNeverPrintsAPrefabricatedAuthorizeCall(t *testing.T) {
	files, err := filepath.Glob("tidy*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("tidy sources: %v %v", files, err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, "stw ") && strings.Contains(line, "--authorize") && !strings.HasPrefix(strings.TrimSpace(line), "//") {
				if strings.Contains(line, "Flags()") {
					continue
				}
				t.Fatalf("%s:%d prints an authorize call: %s", name, i+1, line)
			}
		}
	}
}
