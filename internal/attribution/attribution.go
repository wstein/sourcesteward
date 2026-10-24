// SPDX-License-Identifier: EUPL-1.2

// Package attribution records which agent session created a task and gates remote writes on it.
// It is attribution, not a security boundary: agents share one OS user and the session id is an
// environment value set by the harness, not a secret. Only one variable is ever read for the id;
// no other variable value (in particular no token) is read or stored.
package attribution

import (
	"strings"
	"time"
)

// Origin is stored in the task record in stw state only, never in git or PR text.
type Origin struct {
	Kind      string `json:"kind"`
	SessionID string `json:"session_id,omitempty"`
	CreatedAt string `json:"created_at"`
}

// Reasons of a refusal.
const (
	ReasonMismatch = "attribution-mismatch"
	ReasonUnknown  = "attribution-unknown"
)

// Kinds. KindUnknown is an agent run whose source no marker names.
const (
	KindHuman   = "human"
	KindUnknown = "unknown"
	KindAgent   = "agent"
)

// claudeSessionVar is the only variable whose value is read.
const claudeSessionVar = "CLAUDE_CODE_SESSION_ID"

// FromEnviron derives the origin of the current run. markers are the agent marker names found by the
// existing mode detection and human is its verdict; environ is in os.Environ form. The session id
// comes only from CLAUDE_CODE_SESSION_ID for a claude run; a Codex source is not yet measured and
// stays absent, never guessed. An id that is not a bounded plain token is dropped.
func FromEnviron(environ, markers []string, human bool, now time.Time) Origin {
	o := Origin{Kind: KindHuman, CreatedAt: now.UTC().Format(time.RFC3339)}
	if human {
		return o
	}
	o.Kind = kindOf(markers)
	if o.Kind == "claude" {
		for _, kv := range environ {
			if name, v, ok := strings.Cut(kv, "="); ok && name == claudeSessionVar && validSession(v) {
				o.SessionID = v
			}
		}
	}
	return o
}

func kindOf(markers []string) string {
	has := func(match func(string) bool) bool {
		for _, m := range markers {
			if match(m) {
				return true
			}
		}
		return false
	}
	switch {
	case has(func(m string) bool { return m == "CLAUDECODE" || strings.HasPrefix(m, "CLAUDE_CODE_") }):
		return "claude"
	case has(func(m string) bool { return strings.HasPrefix(m, "CODEX_") }):
		return "codex"
	case has(func(m string) bool { return strings.HasPrefix(m, "AGY_") }):
		return "agy"
	case has(func(m string) bool { return m == "STW_AGENT" }):
		return KindAgent
	}
	return KindUnknown
}

func validSession(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// Refusal is the typed refusal of a remote write. Its message never carries a session id.
type Refusal struct{ Reason string }

func (r Refusal) Error() string {
	if r.Reason == ReasonMismatch {
		return ReasonMismatch + ": the task was created by another agent session; remote writes for it are refused"
	}
	return ReasonUnknown + ": the task records an agent session and the current session cannot be identified; remote writes for it are refused"
}

// Gate decides whether the current run may write remotely for a task with the recorded origin. A
// human run, a task without a recorded session and a matching session are allowed.
func Gate(recorded *Origin, current Origin) error {
	if current.Kind == KindHuman || recorded == nil || recorded.SessionID == "" {
		return nil
	}
	if current.SessionID == "" {
		return Refusal{ReasonUnknown}
	}
	if current.SessionID != recorded.SessionID {
		return Refusal{ReasonMismatch}
	}
	return nil
}
