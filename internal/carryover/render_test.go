// SPDX-License-Identifier: EUPL-1.2
package carryover

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestHumanOutputEndsWithTheLineOnlyForAYes(t *testing.T) {
	yes := Check(t.Context(), okFake(), req)
	var b bytes.Buffer
	if err := WriteHuman(&b, yes); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if want := "Carried-over: from=" + sha("2") + " to=" + sha("4") + " patch-id=" + sha("9") + " pr=3"; lines[len(lines)-1] != want {
		t.Fatalf("last line %q", lines[len(lines)-1])
	}
	if !strings.Contains(b.String(), "carry-over: yes") {
		t.Fatal(b.String())
	}

	f := okFake()
	f.merges = []string{sha("7")}
	no := Check(t.Context(), f, req)
	b.Reset()
	if err := WriteHuman(&b, no); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "Carried-over:") || !strings.Contains(b.String(), "carry-over: no (merge-commits)") || !strings.Contains(b.String(), "2 merge commits found") {
		t.Fatal(b.String())
	}
}

func TestJSONHasTheContractKeys(t *testing.T) {
	var b bytes.Buffer
	if err := WriteJSON(&b, Check(t.Context(), okFake(), req)); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"schema_version", "command", "pr", "from", "to", "patch_id", "merge_commits", "range_diff", "pr_head", "carry_over", "reasons", "line"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	if m["command"] != "review carry-over" || m["carry_over"] != true || m["line"] == nil {
		t.Fatalf("%v", m)
	}
	f := okFake()
	f.merges = []string{sha("7")}
	b.Reset()
	_ = WriteJSON(&b, Check(t.Context(), f, req))
	m = nil
	_ = json.Unmarshal(b.Bytes(), &m)
	if m["carry_over"] != false || m["line"] != nil {
		t.Fatalf("%v", m)
	}
}

func TestLineFormat(t *testing.T) {
	got := Line(sha("a"), sha("b"), sha("c"), 7)
	if want := "Carried-over: from=" + sha("a") + " to=" + sha("b") + " patch-id=" + sha("c") + " pr=7"; got != want {
		t.Fatalf("%q", got)
	}
}
