// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestTidyPruneV4HistoryDispatch(t *testing.T) {
	for _, verb := range []string{"history", "restore"} {
		t.Run(verb, func(t *testing.T) {
			var s restoreSpy
			args := []string{verb, "--contract", "4", "--state-dir", "/tmp/does-not-exist-stw-prune-v4-history", "--json"}
			if verb == "restore" {
				args = append(args, "--list")
			}
			out, err := s.run(args)
			var runtime runtimeFailure
			if !errors.As(err, &runtime) || exitStatus(io.Discard, err) != 1 || s.historyCalls+s.restoreCalls != 0 {
				t.Fatalf("dispatch error=%v old calls=%d", err, s.historyCalls+s.restoreCalls)
			}
			var r map[string]any
			if e := json.NewDecoder(strings.NewReader(out)).Decode(&r); e != nil {
				t.Fatalf("JSON: %v %s", e, out)
			}
			if r["schema_version"] != float64(4) || r["command"] != "tidy-history" || r["operation"] != "registration-prune" {
				t.Fatalf("envelope: %s", out)
			}
		})
	}
}

func TestTidyPruneV4HistoryUsageRefusesBeforeReader(t *testing.T) {
	for _, verb := range []string{"history", "restore"} {
		base := []string{verb, "--contract", "4", "--state-dir", stateArg}
		if verb == "restore" {
			base = append(base, "--list")
		}
		extras := [][]string{{"--limit", "0"}, {"--limit", "-1"}, {"--limit", "1025"}, {"--limit", "x"}, {"--limit", "1", "--limit", "1"}, {"--json", "--json"}, {"--contract", "4"}, {"--plan", strings.Repeat("a", 32)}, {"--apply=false"}, {"--authorize=false"}, {"--action", "a"}, {"--as", "refs/heads/x"}, {"--timeout", "1s"}}
		for _, extra := range extras {
			t.Run(verb+strings.Join(extra, "_"), func(t *testing.T) {
				var s restoreSpy
				_, err := s.run(append(append([]string{}, base...), extra...))
				if err == nil || exitStatus(io.Discard, err) != 2 || s.historyCalls+s.restoreCalls != 0 {
					t.Fatalf("error=%v calls=%d", err, s.historyCalls+s.restoreCalls)
				}
			})
		}
	}
}
