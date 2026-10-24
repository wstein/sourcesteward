// SPDX-License-Identifier: EUPL-1.2
package attribcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/attribution"
	"github.com/wstein/sourcesteward/internal/workspace"
)

const messagingSentinel = "MSG-TOKEN-SENTINEL-93b1"

var attributionNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func TestOriginForRunReadsOnlyTheSessionVariable(t *testing.T) {
	environ := []string{"CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=sess-1", "CLAUDE_CODE_MESSAGING_TOKEN=" + messagingSentinel, "CODEX_API_KEY=" + messagingSentinel}
	o := OriginForRun(environ, false, false, attributionNow)
	b, _ := json.Marshal(o)
	if o.Kind != "claude" || o.SessionID != "sess-1" || bytes.Contains(b, []byte(messagingSentinel)) {
		t.Fatalf("origin %s", b)
	}
}

func TestOriginForRunHumanAndHeadless(t *testing.T) {
	if o := OriginForRun([]string{"HOME=/h"}, true, true, attributionNow); o.Kind != "human" || o.SessionID != "" {
		t.Fatalf("human: %+v", o)
	}
	if o := OriginForRun([]string{"HOME=/h"}, false, true, attributionNow); o.Kind != "unknown" {
		t.Fatalf("headless without marker: %+v", o)
	}
}

func TestRemoteWriteGate(t *testing.T) {
	state := t.TempDir()
	read := func(string, string) (workspace.TaskRecord, bool, error) {
		return workspace.TaskRecord{Origin: &attribution.Origin{Kind: "claude", SessionID: "s1"}}, true, nil
	}
	cases := []struct {
		name   string
		cur    attribution.Origin
		reason string
	}{
		{"same", attribution.Origin{Kind: "claude", SessionID: "s1"}, ""},
		{"other", attribution.Origin{Kind: "claude", SessionID: "s2"}, attribution.ReasonMismatch},
		{"unknown", attribution.Origin{Kind: "claude"}, attribution.ReasonUnknown},
		{"human", attribution.Origin{Kind: "human"}, ""},
	}
	for _, c := range cases {
		err := RemoteWriteGate(read, state, "task", c.cur)
		if c.reason == "" {
			if err != nil {
				t.Errorf("%s: %v", c.name, err)
			}
			continue
		}
		var refusal attribution.Refusal
		if !errors.As(err, &refusal) || refusal.Reason != c.reason || !strings.HasPrefix(err.Error(), c.reason) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestRemoteWriteGateTaskWithoutRecord(t *testing.T) {
	missing := func(string, string) (workspace.TaskRecord, bool, error) { return workspace.TaskRecord{}, false, nil }
	if err := RemoteWriteGate(missing, t.TempDir(), "task", attribution.Origin{Kind: "claude", SessionID: "s2"}); err != nil {
		t.Fatal(err)
	}
	broken := func(string, string) (workspace.TaskRecord, bool, error) {
		return workspace.TaskRecord{}, false, errors.New("unreadable")
	}
	if err := RemoteWriteGate(broken, t.TempDir(), "task", attribution.Origin{Kind: "claude", SessionID: "s2"}); err == nil {
		t.Fatal("an unreadable record must refuse")
	}
}

// Only a real terminal is human: the null device (a character device), a bytes buffer, a pipe
// and a regular file are not, whatever the environment.
func TestOriginForStreamsIsHumanOnlyOnARealTerminal(t *testing.T) {
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	reg, err := os.CreateTemp(t.TempDir(), "f")
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	var buf bytes.Buffer
	for name, c := range map[string][2]any{
		"null device": {null, null}, "buffer": {&buf, &buf}, "pipe": {pr, pw}, "regular file": {reg, reg},
	} {
		if o := originForStreams([]string{"HOME=/h"}, c[0], c[1], attributionNow); o.Kind == attribution.KindHuman {
			t.Errorf("%s: %+v", name, o)
		}
	}
}
