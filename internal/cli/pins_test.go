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

	"github.com/wstein/sourcesteward/internal/inspection"
)

func pinsCLIResult(status string, errorFindings, warnings int) inspection.PinsResult {
	r := inspection.NewPinsResult("/repo")
	r.Status, r.Backend = status, "git"
	for i := 0; i < errorFindings; i++ {
		_ = r.AddFinding("pin-unpublished", pinsPtr("sub"), "local evidence shows no remote contains the pin")
	}
	for i := 0; i < warnings; i++ {
		_ = r.AddFinding("submodule-dirty", pinsPtr("sub"), "dirty")
	}
	if status == "incomplete" {
		_ = r.AddFinding("pins-limit", nil, "a bound was exhausted")
	}
	if status == "refused" || status == "error" {
		r.Error = &inspection.PinsError{Code: "pins-failed", Message: "failed"}
	}
	r.Finalize()
	return r
}
func pinsPtr(s string) *string { return &s }

func TestPinsCommandStaysHiddenUntilTheFinalWorkItem(t *testing.T) {
	root, _ := newRoot("dev", &bytes.Buffer{}, &bytes.Buffer{})
	command, _, err := root.Find([]string{"pins"})
	if err != nil || command == nil || command.Name() != "pins" {
		t.Fatalf("pins command is not registered: %v", err)
	}
	if !command.Hidden {
		t.Fatal("stw pins must stay hidden until work item WI5 of Spec 021 unhides it")
	}
	var out, diagnostic bytes.Buffer
	if code := Run(context.Background(), "dev", []string{"--help"}, &out, &diagnostic); code != 0 || strings.Contains(out.String(), "pins") {
		t.Fatalf("root help advertises pins: %d %q", code, out.String())
	}
	for _, doc := range CommandReference() {
		if strings.Contains(doc.Path, "pins") {
			t.Fatalf("generated reference lists %q", doc.Path)
		}
	}
}
func TestPinsUsageErrorsExitTwoBeforeTheOperation(t *testing.T) {
	for _, args := range [][]string{{"one", "two"}, {"--bogus"}, {"--json=maybe"}, {"--strict=maybe"}, {"a\x00b"}, {"\xff"}, {""}, {"--json", "a", "b"}} {
		called := false
		command := pinsCommandWith(func(context.Context, inspection.PinsOptions) inspection.PinsResult {
			called = true
			return pinsCLIResult("ok", 0, 0)
		})
		command.SetArgs(args)
		var stdout bytes.Buffer
		command.SetOut(&stdout)
		command.SetErr(&bytes.Buffer{})
		err := command.ExecuteContext(context.Background())
		var runtime runtimeFailure
		var strict strictFindingsError
		if err == nil || errors.As(err, &runtime) || errors.As(err, &strict) || called || stdout.Len() != 0 {
			t.Fatalf("%q: err=%v called=%t stdout=%q", args, err, called, stdout.String())
		}
	}
	for _, args := range [][]string{{"pins", "one", "two"}, {"pins", "--bogus"}, {"pins", "--json", "a", "b"}, {"pins", "", "--json"}} {
		var out, diagnostic bytes.Buffer
		if code := Run(context.Background(), "dev", args, &out, &diagnostic); code != 2 || out.Len() != 0 {
			t.Fatalf("%q: exit %d stdout %q", args, code, out.String())
		}
	}
}
func TestPinsPathAndFlags(t *testing.T) {
	var got inspection.PinsOptions
	command := pinsCommandWith(func(_ context.Context, o inspection.PinsOptions) inspection.PinsResult {
		got = o
		return pinsCLIResult("ok", 0, 0)
	})
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetArgs([]string{"repo path", "--json"})
	if err := command.Execute(); err != nil || got.Path != "repo path" {
		t.Fatalf("%v %+v", err, got)
	}
	var decoded inspection.PinsResult
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || decoded.Command != "pins" || strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("%s %v", out.String(), err)
	}
}
func TestPinsExitStatusPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result inspection.PinsResult
		strict bool
		want   int // 0, 1, 3
	}{
		{"complete clean", pinsCLIResult("ok", 0, 0), false, 0},
		{"complete clean strict", pinsCLIResult("ok", 0, 0), true, 0},
		{"errors without strict", pinsCLIResult("ok", 2, 1), false, 0},
		{"warnings only strict", pinsCLIResult("ok", 0, 3), true, 0},
		{"errors strict", pinsCLIResult("ok", 2, 1), true, 3},
		{"incomplete", pinsCLIResult("incomplete", 1, 0), false, 1},
		{"incomplete with errors strict", pinsCLIResult("incomplete", 2, 0), true, 1},
		{"refused", pinsCLIResult("refused", 0, 0), false, 1},
		{"refused strict", pinsCLIResult("refused", 0, 0), true, 1},
		{"error", pinsCLIResult("error", 0, 0), false, 1},
		{"error strict", pinsCLIResult("error", 0, 0), true, 1},
	} {
		for _, jsonOutput := range []bool{false, true} {
			command := pinsCommandWith(func(context.Context, inspection.PinsOptions) inspection.PinsResult { return tc.result })
			args := []string{}
			if tc.strict {
				args = append(args, "--strict")
			}
			if jsonOutput {
				args = append(args, "--json")
			}
			command.SetArgs(args)
			var out bytes.Buffer
			command.SetOut(&out)
			err := command.Execute()
			var runtime runtimeFailure
			var strict strictFindingsError
			switch tc.want {
			case 0:
				if err != nil {
					t.Fatalf("%s: %v", tc.name, err)
				}
			case 1:
				if !errors.As(err, &runtime) || errors.As(err, &strict) {
					t.Fatalf("%s: %v", tc.name, err)
				}
			case 3:
				if !errors.As(err, &strict) || strict.count != 2 || errors.As(err, &runtime) {
					t.Fatalf("%s: %v", tc.name, err)
				}
			}
			if out.Len() == 0 {
				t.Fatalf("%s: the full result must still be written", tc.name)
			}
		}
	}
}
func TestExitStatusForStrictFindings(t *testing.T) {
	var diagnostic bytes.Buffer
	if code := exitStatus(&diagnostic, strictFindingsError{count: 3}); code != 3 || diagnostic.String() != "Error: strict: 3 error findings\n" {
		t.Fatalf("%d %q", code, diagnostic.String())
	}
	diagnostic.Reset()
	if code := exitStatus(&diagnostic, strictFindingsError{count: 1}); code != 3 || diagnostic.String() != "Error: strict: 1 error finding\n" {
		t.Fatalf("%d %q", code, diagnostic.String())
	}
	if code := exitStatus(&bytes.Buffer{}, runtimeFailure{errors.New("x")}); code != 1 {
		t.Fatal(code)
	}
	if code := exitStatus(&bytes.Buffer{}, errors.New("usage")); code != 2 {
		t.Fatal(code)
	}
}
func TestPinsEndToEndRefusalExitsOneWithJSON(t *testing.T) {
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, ".jj"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	for _, extra := range [][]string{{}, {"--strict"}} {
		var out, diagnostic bytes.Buffer
		args := append([]string{"pins", d, "--json"}, extra...)
		if code := Run(context.Background(), "dev", args, &out, &diagnostic); code != 1 {
			t.Fatalf("exit %d: %s", code, diagnostic.String())
		}
		var r inspection.PinsResult
		if err := json.Unmarshal(out.Bytes(), &r); err != nil || r.Status != "refused" || r.Error == nil || r.Error.Code != "unsupported-backend" {
			t.Fatalf("%s %v", out.String(), err)
		}
	}
	var out, diagnostic bytes.Buffer
	if code := Run(context.Background(), "dev", []string{"pins", d}, &out, &diagnostic); code != 1 || !strings.Contains(out.String(), "unsupported-backend") {
		t.Fatalf("%d %q", code, out.String())
	}
}
func TestPinsTextShowsEvidenceAndEscapesRepositoryData(t *testing.T) {
	hostile := "evil\nError: forged\x1b[31m\u202e\u009b\\"
	r := inspection.NewPinsResult(hostile)
	r.Backend = "git"
	head := strings.Repeat("a", 40)
	r.Identity = &inspection.Result{SchemaVersion: 1, Status: "ok", Backend: "git", Repository: &inspection.Repository{Worktree: pinsPtr("/repo"), Head: inspection.Head{State: "attached", OID: &head}}}
	reach := inspection.PinsSubmodule{Path: hostile, PathEncoding: "utf8"}
	reach.Pin.HeadCommit = pinsPtr(strings.Repeat("5", 40))
	reach.Checkout = inspection.PinsCheckout{State: "initialized", HeadCommit: pinsPtr(strings.Repeat("5", 40)), RelationToPin: "same"}
	reach.Checkout.Dirty = inspection.PinsDirty{Tracked: "absent", Untracked: "absent"}
	reach.Publication = inspection.PinsPublication{Status: "reachable", ReachableFrom: []inspection.PinsRemoteRef{{Remote: "origin", Ref: "refs/remotes/origin/" + hostile}}, Truncated: true}
	lost := inspection.PinsSubmodule{Path: "workharbor", PathEncoding: "utf8"}
	lost.Pin.HeadCommit = pinsPtr(strings.Repeat("c", 40))
	lost.Checkout = inspection.PinsCheckout{State: "initialized", HeadCommit: pinsPtr(strings.Repeat("d", 40)), RelationToPin: "diverged", Ahead: pinsInt(1), Behind: pinsInt(12)}
	lost.Checkout.Dirty = inspection.PinsDirty{Tracked: "present", Untracked: "absent"}
	lost.Publication = inspection.PinsPublication{Status: "unreachable"}
	unknown := inspection.PinsSubmodule{Path: "unknown", PathEncoding: "utf8"}
	unknown.Checkout = inspection.PinsCheckout{State: "uninitialized", RelationToPin: "unknown"}
	unknown.Publication = inspection.PinsPublication{Status: "unknown", Reason: "submodule is not initialized"}
	r.Submodules = []inspection.PinsSubmodule{reach, lost, unknown}
	_ = r.AddFinding("pin-unpublished", pinsPtr("workharbor"), "local evidence shows no remote contains the pin")
	_ = r.AddFinding("submodule-dirty", pinsPtr(hostile), hostile)
	_ = r.AddFinding("remote-refs-may-be-stale", nil, "stale")
	r.Finalize()
	var out bytes.Buffer
	if err := renderPinsText(&out, r); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"SourceSteward pins:", "(git, HEAD aaaaaaa…)", "effects: read-only, no network, no fetch", "workharbor", "pin ccccccc…", "checkout ddddddd… (diverged, ahead 1, behind 12)", "UNREACHABLE (no remote-tracking ref contains it)", "reachable from origin/", "truncated", "unknown (submodule is not initialized)", "uninitialized", "findings: 1 error, 1 warning, 1 info", "error    workharbor  pin-unpublished", "note: remote-tracking refs may be stale; no fetch was made", "dirty"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	for _, bad := range []string{"\x1b", "\u202e", "\u009b", "\r"} {
		if strings.Contains(text, bad) {
			t.Fatalf("unescaped %q in:\n%s", bad, text)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Error:") || strings.HasPrefix(line, "forged") {
			t.Fatalf("forged line %q", line)
		}
	}
	if !strings.Contains(text, `evil\nError: forged\x1b[31m`+`\u202e`+`\u009b\\`) {
		t.Fatalf("escaping form changed:\n%s", text)
	}
}
func TestPinsTextForRefusalAndWriterFailures(t *testing.T) {
	r := pinsCLIResult("refused", 0, 0)
	r.Backend = "jj"
	r.Error = &inspection.PinsError{Code: "unsupported-backend", Message: "jj has no Git submodule model"}
	var out bytes.Buffer
	if err := renderPinsText(&out, r); err != nil || !strings.Contains(out.String(), "refused: unsupported-backend: jj has no Git submodule model") || !strings.Contains(out.String(), "(jj)") {
		t.Fatalf("%v %q", err, out.String())
	}
	for _, jsonOutput := range []bool{false, true} {
		command := pinsCommandWith(func(context.Context, inspection.PinsOptions) inspection.PinsResult { return pinsCLIResult("ok", 0, 0) })
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
func TestPinsCanceledContextIsRuntimeFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	command := pinsCommandWith(func(context.Context, inspection.PinsOptions) inspection.PinsResult {
		cancel()
		return pinsCLIResult("ok", 0, 0)
	})
	command.SetArgs([]string{})
	command.SetOut(&bytes.Buffer{})
	var failure runtimeFailure
	if err := command.ExecuteContext(ctx); !errors.As(err, &failure) {
		t.Fatalf("%v", err)
	}
}
func pinsInt(n int) *int { return &n }
