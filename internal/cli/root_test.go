// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestCommandContract(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		code   int
		output string
	}{
		{"root", nil, 0, "Explainable version-control tools"},
		{"help", []string{"--help"}, 0, "version"},
		{"version", []string{"version"}, 0, "SourceSteward dev\n"},
		{"json", []string{"version", "--json"}, 0, "{\"schema_version\":1,\"name\":\"SourceSteward\",\"version\":\"dev\"}\n"},
		{"unknown command", []string{"cleanup"}, 2, ""},
		{"unknown help topic", []string{"help", "bogus"}, 2, ""},
		{"extra help argument", []string{"help", "version", "extra"}, 2, ""},
		{"help command", []string{"help"}, 0, "version"},
		{"version help", []string{"help", "version"}, 0, "--json"},
		{"unknown flag", []string{"version", "--bogus"}, 2, ""},
		{"extra version arg", []string{"version", "extra"}, 2, ""},
		{"extra root arg", []string{"extra"}, 2, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			code := Run(context.Background(), "dev", tc.args, &out, &diagnostic)
			if code != tc.code {
				t.Fatalf("exit=%d stderr=%q", code, diagnostic.String())
			}
			if tc.code == 0 {
				if !strings.Contains(out.String(), tc.output) || diagnostic.Len() != 0 {
					t.Fatalf("stdout=%q stderr=%q", out.String(), diagnostic.String())
				}
				if tc.name == "json" && out.String() != tc.output {
					t.Fatalf("JSON contract changed: %q", out.String())
				}
			} else if out.Len() != 0 || !strings.HasPrefix(diagnostic.String(), "Error: ") || strings.Count(diagnostic.String(), "\n") != 1 {
				t.Fatalf("stdout=%q stderr=%q", out.String(), diagnostic.String())
			}
		})
	}
}

func TestCommandsDoNotLeakFlags(t *testing.T) {
	var first, second, diagnostic bytes.Buffer
	if Run(context.Background(), "v0.0.1", []string{"version", "--json"}, &first, &diagnostic) != 0 {
		t.Fatal(diagnostic.String())
	}
	if Run(context.Background(), "v0.0.2", []string{"version"}, &second, &diagnostic) != 0 {
		t.Fatal(diagnostic.String())
	}
	if second.String() != "SourceSteward v0.0.2\n" {
		t.Fatalf("flag/version leaked: %q", second.String())
	}
}

func TestRootHelpAgentHints(t *testing.T) {
	want := "Are you an AI? Use these resources ONLY IF your task specifically asks you to:\n" +
		"  Help a human understand, set up or use SourceSteward:\n" +
		"    https://raw.githubusercontent.com/wstein/sourcesteward/main/docs/agent-guide.md\n" +
		"  Debug or investigate a problem with SourceSteward:\n" +
		"    https://raw.githubusercontent.com/wstein/sourcesteward/main/llms.txt\n"
	for _, args := range [][]string{nil, {"--help"}, {"-h"}, {"help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			if code := Run(context.Background(), "dev", args, &out, &diagnostic); code != 0 || diagnostic.Len() != 0 {
				t.Fatalf("code=%d stderr=%q", code, diagnostic.String())
			}
			if strings.Count(out.String(), want) != 1 || !strings.HasSuffix(out.String(), want) {
				t.Fatalf("expected one trailing agent notice: %q", out.String())
			}
			if !strings.Contains(out.String(), "Usage:") || !strings.Contains(out.String(), "Available Commands:") {
				t.Fatalf("ordinary root help missing: %q", out.String())
			}
		})
	}
}

func TestAgentHintsAbsentOutsideRootHelp(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"version"}, 0},
		{[]string{"version", "--json"}, 0},
		{[]string{"version", "--help"}, 0},
		{[]string{"version", "-h"}, 0},
		{[]string{"help", "version"}, 0},
		{[]string{"help", "help"}, 0},
		{[]string{"help", "--help"}, 0},
		{[]string{"help", "-h"}, 0},
		{[]string{"unknown"}, 2},
		{[]string{"--unknown"}, 2},
		{[]string{"help", "unknown"}, 2},
		{[]string{"version", "--unknown"}, 2},
		{[]string{"version", "extra"}, 2},
		{[]string{"help", "version", "extra"}, 2},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			if code := Run(context.Background(), "dev", tc.args, &out, &diagnostic); code != tc.code {
				t.Fatalf("code=%d stderr=%q", code, diagnostic.String())
			}
			combined := out.String() + diagnostic.String()
			for _, forbidden := range []string{"Are you an AI?", "raw.githubusercontent.com", "docs/agent-guide.md", "llms.txt"} {
				if strings.Contains(combined, forbidden) {
					t.Fatalf("unexpected agent hint %q: %q", forbidden, combined)
				}
			}
			if tc.code != 0 && out.Len() != 0 {
				t.Fatalf("error produced stdout: %q", out.String())
			}
		})
	}
}

type failOnWrite struct {
	buffer bytes.Buffer
	calls  int
	fail   int
}

func (w *failOnWrite) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.fail {
		return 0, errors.New("selected help write failed")
	}
	return w.buffer.Write(p)
}

func (w *failOnWrite) String() string { return w.buffer.String() }

func TestAgentHintWriteFailure(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"-h"}, {"help"}} {
		for _, fail := range []int{1, 2} {
			out := &failOnWrite{fail: fail}
			var diagnostic bytes.Buffer
			if code := Run(context.Background(), "dev", args, out, &diagnostic); code != 1 {
				t.Fatalf("args=%q fail=%d code=%d stderr=%q", args, fail, code, diagnostic.String())
			}
			if out.calls != fail || diagnostic.String() != "Error: selected help write failed\n" {
				t.Fatalf("args=%q fail=%d writes=%d stderr=%q", args, fail, out.calls, diagnostic.String())
			}
			if fail == 2 && !strings.Contains(out.String(), "Usage:") {
				t.Fatalf("notice failure lost successful ordinary help: %q", out.String())
			}
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestRuntimeFailureContract(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"version", "--help"}, {"version"}, {"version", "--json"}} {
		var diagnostic bytes.Buffer
		if code := Run(context.Background(), "dev", args, failingWriter{}, &diagnostic); code != 1 || !strings.Contains(diagnostic.String(), "output unavailable") {
			t.Fatalf("code=%d error=%q", code, diagnostic.String())
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, diagnostic bytes.Buffer
	if code := Run(ctx, "dev", []string{"version"}, &out, &diagnostic); code != 1 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "canceled") {
		t.Fatalf("code=%d out=%q error=%q", code, out.String(), diagnostic.String())
	}
	if code := Run(context.Background(), "dev", []string{"unknown"}, io.Discard, failingWriter{}); code != 2 {
		t.Fatalf("unwritable error stream changed outcome: %d", code)
	}
}
