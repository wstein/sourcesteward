// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/restack"
)

const restackHex = "0123456789abcdef0123456789abcdef01234567"

func restackFlags() []string {
	return []string{"restack", "plan", "--branch", "refs/heads/child", "--old-parent", restackHex, "--new-parent", "refs/heads/main"}
}

func stubResult(status string) restack.Result {
	r := restack.Result{SchemaVersion: 1, Kind: "restack-plan", Status: status, Backend: "git", Outcome: restack.OutcomeNone, Notice: restack.Notice,
		Input:       restack.Input{Moved: restack.Moved{Commits: []restack.MovedCommit{}}},
		Limitations: []string{restack.LimitSemanticBreaks}, Diagnostics: []restack.Diagnostic{}}
	switch status {
	case restack.StatusError:
		r.Diagnostics = []restack.Diagnostic{{Code: "target-dirty", Message: "The target checkout has uncommitted changes to tracked files.", Details: map[string]any{}}}
	case restack.StatusIncomplete:
		r.Diagnostics = []restack.Diagnostic{{Code: "bound-exceeded", Message: "A documented bound was reached.", Details: map[string]any{"bound": "max_paths"}}}
	}
	return r
}

func runRestack(t *testing.T, result restack.Result, args []string) (code int, stdout, stderr string, called *restack.Options) {
	t.Helper()
	var seen restack.Options
	invoked := false
	root, _ := newRoot("dev", &bytes.Buffer{}, &bytes.Buffer{})
	root.RemoveCommand(findCommand(root, "restack"))
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.AddCommand(restackCommandWith(func(ctx context.Context, o restack.Options) restack.Result {
		seen, invoked = o, true
		return result
	}))
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	code = 0
	if err != nil {
		code = exitStatus(&errOut, err)
	}
	if invoked {
		called = &seen
	}
	return code, out.String(), errOut.String(), called
}

func findCommand(root *cobra.Command, name string) *cobra.Command {
	for _, c := range root.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

func TestRestackIsRegisteredAndHiddenUntilAcceptance(t *testing.T) {
	root, _ := newRoot("dev", &bytes.Buffer{}, &bytes.Buffer{})
	command, _, err := root.Find([]string{"restack", "plan"})
	if err != nil || command == nil || command.Name() != "plan" {
		t.Fatalf("restack plan is not registered: %v", err)
	}
	parent := findCommand(root, "restack")
	if parent == nil || !parent.Hidden {
		t.Fatal("stw restack stays hidden until the conformance evidence of Spec 015 exists")
	}
	var out bytes.Buffer
	if code := Run(context.Background(), "dev", []string{"--help"}, &out, &bytes.Buffer{}); code != 0 || strings.Contains(out.String(), "restack") {
		t.Fatalf("root help advertises restack: %d", code)
	}
	for _, doc := range CommandReference() {
		if strings.Contains(doc.Path, "restack") {
			t.Fatalf("generated reference lists %q", doc.Path)
		}
	}
}

func TestRestackSyntaxErrorsExitTwoBeforeTheOperation(t *testing.T) {
	good := restackFlags()
	replace := func(flag, value string) []string {
		out := append([]string{}, good...)
		for i, a := range out {
			if a == flag {
				out[i+1] = value
			}
		}
		return out
	}
	cases := map[string][]string{
		"no flags":           {"restack", "plan"},
		"missing new parent": {"restack", "plan", "--branch", "refs/heads/a", "--old-parent", restackHex},
		"HEAD":               replace("--old-parent", "HEAD"),
		"short oid":          replace("--new-parent", "0123456"),
		"revision":           replace("--new-parent", "refs/heads/main~1"),
		"short branch":       replace("--branch", "child"),
		"duplicate branch":   append(append([]string{}, good...), "--branch", "refs/heads/other"),
		"duplicate old":      append(append([]string{}, good...), "--old-parent", restackHex),
		"bad identity":       append(append([]string{}, good...), "--identity", "no email"),
		"bad remote":         append(append([]string{}, good...), "--remote", "a/b"),
		"bad expect":         append(append([]string{}, good...), "--expect-head", "abc"),
		"two paths":          append(append([]string{}, good...), "a", "b"),
		"empty path":         append(append([]string{}, good...), ""),
		"unknown flag":       append(append([]string{}, good...), "--keep-both-config", "x"),
		"force flag":         append(append([]string{}, good...), "--force"),
		"bad json":           append(append([]string{}, good...), "--json=maybe"),
	}
	for name, args := range cases {
		code, stdout, _, called := runRestack(t, stubResult(restack.StatusOK), args)
		if code != 2 || stdout != "" || called != nil {
			t.Errorf("%s: exit %d stdout %q called %v", name, code, stdout, called)
		}
	}
}

func TestRestackPassesTheExplicitInputThrough(t *testing.T) {
	args := append(restackFlags(), "--expect-head", restackHex, "--remote", "upstream", "--identity", "A B <a@b.c>", "--identity", "C <c@d.e>", "/some/path")
	code, _, _, called := runRestack(t, stubResult(restack.StatusOK), args)
	if code != 0 || called == nil {
		t.Fatalf("exit %d", code)
	}
	want := restack.Options{Path: "/some/path", Branch: "refs/heads/child", OldParent: restackHex, NewParent: "refs/heads/main", ExpectHead: restackHex, Remote: "upstream", Identities: []string{"A B <a@b.c>", "C <c@d.e>"}}
	if called.Path != want.Path || called.Branch != want.Branch || called.OldParent != want.OldParent || called.NewParent != want.NewParent || called.ExpectHead != want.ExpectHead || called.Remote != want.Remote || strings.Join(called.Identities, "|") != strings.Join(want.Identities, "|") {
		t.Fatalf("%+v", *called)
	}
	_, _, _, called = runRestack(t, stubResult(restack.StatusOK), restackFlags())
	if called.Remote != "origin" || called.Path != "" {
		t.Fatalf("defaults %+v", *called)
	}
}

func TestRestackExitStatuses(t *testing.T) {
	for status, want := range map[string]int{restack.StatusOK: 0, restack.StatusError: 1, restack.StatusIncomplete: 3} {
		for _, json := range []bool{false, true} {
			args := restackFlags()
			if json {
				args = append(args, "--json")
			}
			code, stdout, stderr, _ := runRestack(t, stubResult(status), args)
			if code != want || stdout == "" {
				t.Errorf("%s json=%v: exit %d stdout %q", status, json, code, stdout)
			}
			if status == restack.StatusOK && stderr != "" {
				t.Errorf("stderr %q", stderr)
			}
			if status != restack.StatusOK && (strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "Error: restack plan ")) {
				t.Errorf("%s: want one concise Error line, got %q", status, stderr)
			}
		}
	}
	// --json always emits exactly one object, including refusals and incomplete results.
	for _, status := range []string{restack.StatusError, restack.StatusIncomplete, restack.StatusOK} {
		_, stdout, _, _ := runRestack(t, stubResult(status), append(restackFlags(), "--json"))
		var decoded map[string]any
		if err := json.Unmarshal([]byte(stdout), &decoded); err != nil || decoded["kind"] != "restack-plan" || decoded["status"] != status || strings.Count(strings.TrimSpace(stdout), "\n") != 0 {
			t.Errorf("%s: %v %q", status, err, stdout)
		}
	}
}

func TestRestackHumanOutputComesFromTheResultAndIsInert(t *testing.T) {
	r := stubResult(restack.StatusError)
	r.Diagnostics[0].Message = "boom\x1b[31m\nforged line"
	r.Input.Branch.Ref = "refs/heads/a\x1b]0;title\x07b"
	_, stdout, stderr, _ := runRestack(t, r, restackFlags())
	for _, text := range []string{stdout, stderr} {
		if strings.ContainsAny(text, "\x1b\x07") || strings.Contains(text, "forged line\n") && strings.Contains(text, "\nforged line") {
			t.Fatalf("control characters or forged lines reach the terminal: %q", text)
		}
	}
}

func TestRestackPlanEndToEnd(t *testing.T) {
	dir := restackFixture(t)
	var out, errOut bytes.Buffer
	args := []string{"restack", "plan", dir, "--branch", "refs/heads/child", "--old-parent", "refs/heads/parent", "--new-parent", "refs/heads/main", "--identity", "Fixture Owner <owner@example.org>", "--json"}
	if code := Run(context.Background(), "dev", args, &out, &errOut); code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out.String(), errOut.String())
	}
	var decoded restack.Result
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || decoded.Outcome != restack.OutcomeClean || decoded.Evidence == nil || !decoded.Evidence.RangePairing.AllEqual {
		t.Fatalf("%v %s", err, out.String())
	}
	out.Reset()
	if code := Run(context.Background(), "dev", args[:len(args)-1:len(args)-1], &out, &errOut); code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"Restack plan: refs/heads/child", "Outcome: evidence-clean", "Range pairing: = =", "Identity: pass", "Limitation: semantic breaks are not detected", "Lease push (not run):\n  NEW_FULL_SHA=", "\n  git push --force-with-lease='refs/heads/child:", "Resync"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("human output lacks %q:\n%s", want, out.String())
		}
	}
	// A refusal: dirty target exits 1 with the diagnostic code on standard error.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), "dev", args, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "target-dirty") {
		t.Fatalf("exit %d %q", code, errOut.String())
	}
}

// restackFixture builds main: A; parent: B1; child: C1; main: B1' (rebase-merged parent).
func restackFixture(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "repo")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tick := 1_700_000_000
	git := func(args ...string) {
		t.Helper()
		tick += 60
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		date := "@" + strconv.Itoa(tick) + " +0000"
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=Fixture Owner", "GIT_AUTHOR_EMAIL=owner@example.org", "GIT_COMMITTER_NAME=Fixture Owner", "GIT_COMMITTER_EMAIL=owner@example.org",
			"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	file := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	file("README", "a\n")
	git("add", "-A")
	git("commit", "-q", "-m", "A")
	git("checkout", "-q", "-b", "parent")
	file("b1", "b1\n")
	git("add", "-A")
	git("commit", "-q", "-m", "B1")
	git("checkout", "-q", "-b", "child")
	file("c1", "c1\n")
	git("add", "-A")
	git("commit", "-q", "-m", "C1")
	file("c2", "c2\n")
	git("add", "-A")
	git("commit", "-q", "-m", "C2")
	git("checkout", "-q", "main")
	git("cherry-pick", "parent")
	return dir
}
