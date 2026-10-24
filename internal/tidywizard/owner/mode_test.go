// SPDX-License-Identifier: EUPL-1.2

package owner

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDetectMode(t *testing.T) {
	cases := []struct {
		name    string
		in      Env
		want    Mode
		markers []string
	}{
		{"plain pty is human", Env{StdinTTY: true, StdoutTTY: true}, ModeHuman, []string{}},
		{"plain pty with unrelated env", Env{StdinTTY: true, StdoutTTY: true, Environ: []string{"HOME=/h", "CLAUDE=x", "MYCODEX_X=1"}}, ModeHuman, []string{}},
		{"non-tty is agent", Env{}, ModeAgent, []string{}},
		{"stdin only tty is agent", Env{StdinTTY: true}, ModeAgent, []string{}},
		{"stdout only tty is agent", Env{StdoutTTY: true}, ModeAgent, []string{}},
		{"pty plus STW_AGENT", Env{StdinTTY: true, StdoutTTY: true, Environ: []string{"STW_AGENT=1"}}, ModeAgent, []string{"STW_AGENT"}},
		{"empty STW_AGENT is present", Env{StdinTTY: true, StdoutTTY: true, Environ: []string{"STW_AGENT="}}, ModeAgent, []string{"STW_AGENT"}},
		{"empty CLAUDECODE is present", Env{StdinTTY: true, StdoutTTY: true, Environ: []string{"CLAUDECODE="}}, ModeAgent, []string{"CLAUDECODE"}},
		{"entry without equals sign is no variable", Env{StdinTTY: true, StdoutTTY: true, Environ: []string{"CLAUDECODE"}}, ModeHuman, []string{}},
		{"pty plus CLAUDECODE", Env{StdinTTY: true, StdoutTTY: true, Environ: []string{"CLAUDECODE=1"}}, ModeAgent, []string{"CLAUDECODE"}},
		{"pty plus CLAUDE_CODE_ENTRYPOINT", Env{StdinTTY: true, StdoutTTY: true, Environ: []string{"CLAUDE_CODE_ENTRYPOINT=cli"}}, ModeAgent, []string{"CLAUDE_CODE_ENTRYPOINT"}},
		{"pty plus CODEX_HOME", Env{StdinTTY: true, StdoutTTY: true, Environ: []string{"CODEX_HOME=/c"}}, ModeAgent, []string{"CODEX_HOME"}},
		{"pty plus AGY_SESSION", Env{StdinTTY: true, StdoutTTY: true, Environ: []string{"AGY_SESSION=1"}}, ModeAgent, []string{"AGY_SESSION"}},
		{"markers sorted and unique", Env{StdinTTY: true, StdoutTTY: true, Environ: []string{"CODEX_B=1", "AGY_A=1", "CODEX_B=2", "CLAUDECODE=1"}}, ModeAgent, []string{"AGY_A", "CLAUDECODE", "CODEX_B"}},
		{"mine forces agent on a pty", Env{StdinTTY: true, StdoutTTY: true, Mine: true}, ModeAgent, []string{}},
		{"by-owner keeps a human on a pty human", Env{StdinTTY: true, StdoutTTY: true, ByOwner: []string{"codex"}}, ModeHuman, []string{}},
		{"by-owner does not make a non-tty human", Env{ByOwner: []string{"codex"}}, ModeAgent, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := Detect(c.in)
			if ev.Mode != c.want {
				t.Fatalf("mode = %q, want %q", ev.Mode, c.want)
			}
			if !reflect.DeepEqual(ev.Markers, c.markers) {
				t.Fatalf("markers = %v, want %v", ev.Markers, c.markers)
			}
		})
	}
}

func TestDetectRecordsEvidence(t *testing.T) {
	ev := Detect(Env{StdinTTY: true, StdoutTTY: false, Environ: []string{"CODEX_TOKEN=secret", "STW_OWNER=claude"}, OSUser: "werner", Mine: true, ByOwner: []string{"codex"}})
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"mode":"agent","stdin_tty":true,"stdout_tty":false,"markers":["CODEX_TOKEN"],"mine":true,"by_owner":["codex"],"caller":"claude","caller_source":"$STW_OWNER"}`
	if string(b) != want {
		t.Fatalf("evidence = %s\nwant       %s", b, want)
	}
}

func TestEmptySTWOwnerIsUnset(t *testing.T) {
	ev := Detect(Env{StdinTTY: true, StdoutTTY: true, Environ: []string{"STW_OWNER="}, OSUser: "werner"})
	if ev.Caller != "werner" || ev.CallerSource != "os-user" {
		t.Fatalf("caller = %q/%q", ev.Caller, ev.CallerSource)
	}
}

func TestDetectCallerByMode(t *testing.T) {
	human := Detect(Env{StdinTTY: true, StdoutTTY: true, OSUser: "werner"})
	if human.Caller != "werner" || human.CallerSource != "os-user" {
		t.Fatalf("human caller = %q/%q", human.Caller, human.CallerSource)
	}
	agent := Detect(Env{OSUser: "werner"})
	if agent.Caller != "" || agent.CallerSource != "unset" {
		t.Fatalf("agent without STW_OWNER must have no caller, got %q/%q", agent.Caller, agent.CallerSource)
	}
}
