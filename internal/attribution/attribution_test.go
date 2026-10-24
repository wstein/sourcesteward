// SPDX-License-Identifier: EUPL-1.2

package attribution

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func TestFromEnvironKinds(t *testing.T) {
	cases := []struct {
		name     string
		environ  []string
		markers  []string
		human    bool
		kind, id string
	}{
		{"human", []string{"HOME=/h"}, nil, true, "human", ""},
		{"claude with session", []string{"CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=abc-123"}, []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID"}, false, "claude", "abc-123"},
		{"claude without session", []string{"CLAUDECODE=1"}, []string{"CLAUDECODE"}, false, "claude", ""},
		{"codex session source unknown", []string{"CODEX_HOME=/c"}, []string{"CODEX_HOME"}, false, "codex", ""},
		{"agy", []string{"AGY_X=1"}, []string{"AGY_X"}, false, "agy", ""},
		{"declared agent", []string{"STW_AGENT=1"}, []string{"STW_AGENT"}, false, "agent", ""},
		{"agent without marker", nil, nil, false, "unknown", ""},
		{"unsafe session id is not recorded", []string{"CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=a b\n"}, []string{"CLAUDECODE"}, false, "claude", ""},
		{"128 byte session id is kept", []string{"CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=" + strings.Repeat("a", 128)}, []string{"CLAUDECODE"}, false, "claude", strings.Repeat("a", 128)},
		{"129 byte session id is dropped", []string{"CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=" + strings.Repeat("a", 129)}, []string{"CLAUDECODE"}, false, "claude", ""},
		{"human never records a session", []string{"CLAUDE_CODE_SESSION_ID=abc"}, nil, true, "human", ""},
	}
	for _, c := range cases {
		o := FromEnviron(c.environ, c.markers, c.human, now)
		if o.Kind != c.kind || o.SessionID != c.id || o.CreatedAt != "2026-10-24T12:00:00Z" {
			t.Errorf("%s: got %+v", c.name, o)
		}
	}
}

func TestGate(t *testing.T) {
	rec := &Origin{Kind: "claude", SessionID: "s1"}
	cases := []struct {
		name    string
		rec     *Origin
		cur     Origin
		reason  string
		allowed bool
	}{
		{"same session", rec, Origin{Kind: "claude", SessionID: "s1"}, "", true},
		{"other session", rec, Origin{Kind: "claude", SessionID: "s2"}, ReasonMismatch, false},
		{"no current session", rec, Origin{Kind: "claude"}, ReasonUnknown, false},
		{"other kind without session", rec, Origin{Kind: "codex"}, ReasonUnknown, false},
		{"human run", rec, Origin{Kind: "human"}, "", true},
		{"no record", nil, Origin{Kind: "claude", SessionID: "s2"}, "", true},
		{"record without session", &Origin{Kind: "codex"}, Origin{Kind: "claude", SessionID: "s2"}, "", true},
		{"human record", &Origin{Kind: "human"}, Origin{Kind: "claude", SessionID: "s2"}, "", true},
	}
	for _, c := range cases {
		err := Gate(c.rec, c.cur)
		if c.allowed != (err == nil) {
			t.Errorf("%s: err=%v", c.name, err)
			continue
		}
		var r Refusal
		if err != nil && (!errors.As(err, &r) || r.Reason != c.reason) {
			t.Errorf("%s: reason %v want %s", c.name, err, c.reason)
		}
		if err != nil && (strings.Contains(err.Error(), "s1") || strings.Contains(err.Error(), "s2")) {
			t.Errorf("%s: message leaks a session id: %v", c.name, err)
		}
	}
}
