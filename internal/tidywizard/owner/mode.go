// SPDX-License-Identifier: EUPL-1.2

package owner

import (
	"sort"
	"strings"

	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
)

// Mode is the run mode of the wizard.
type Mode string

// The run modes. Agent mode is the narrow, safe one.
const (
	ModeHuman Mode = "human"
	ModeAgent Mode = "agent"
)

// Env holds the observed facts mode detection reads. Environ is in os.Environ form (KEY=VALUE).
type Env struct {
	StdinTTY, StdoutTTY bool
	Environ             []string
	OSUser              string
	Mine                bool
	ByOwner             []string
}

// Evidence is the mode evidence recorded in the stored plan and the apply journal and shown by
// history. Markers lists variable names only, never values.
type Evidence struct {
	Mode         Mode     `json:"mode"`
	StdinTTY     bool     `json:"stdin_tty"`
	StdoutTTY    bool     `json:"stdout_tty"`
	Markers      []string `json:"markers"`
	Mine         bool     `json:"mine"`
	ByOwner      []string `json:"by_owner"`
	Caller       string   `json:"caller"`
	CallerSource string   `json:"caller_source"`
}

// markerEnv is the variable that declares an agent run; markerNames are exact agent variables and
// markerPrefixes are agent variable families. Extend by pull request, with a test row.
const markerEnv = "STW_AGENT"

var (
	markerNames    = []string{"CLAUDECODE"}
	markerPrefixes = []string{"CLAUDE_CODE_", "CODEX_", "AGY_"}
)

// Detect derives the run mode. Human mode needs both streams on terminals, no STW_AGENT, no agent
// marker and no --mine; everything else is agent mode. This is an accident
// guard against a careless agent run, not a boundary against a deliberately evasive process.
// An agent variable counts when it is present in the environment, empty or not; an empty
// STW_OWNER is unset.
func Detect(in Env) Evidence {
	stwOwner := ""
	seen := map[string]bool{}
	for _, kv := range in.Environ {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if name == "STW_OWNER" && value != "" {
			stwOwner = value
		}
		if isMarker(name) {
			seen[name] = true
		}
	}
	markers := make([]string, 0, len(seen))
	for name := range seen {
		markers = append(markers, name)
	}
	sort.Strings(markers)
	byOwner := append([]string{}, in.ByOwner...)

	mode := ModeHuman
	if !in.StdinTTY || !in.StdoutTTY || len(markers) > 0 || in.Mine {
		mode = ModeAgent
	}
	caller := derive.Caller(stwOwner, in.OSUser, mode == ModeAgent)
	return Evidence{
		Mode: mode, StdinTTY: in.StdinTTY, StdoutTTY: in.StdoutTTY, Markers: markers,
		Mine: in.Mine, ByOwner: byOwner, Caller: caller.Value, CallerSource: caller.Source,
	}
}

func isMarker(name string) bool {
	if name == markerEnv {
		return true
	}
	for _, n := range markerNames {
		if name == n {
			return true
		}
	}
	for _, p := range markerPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
