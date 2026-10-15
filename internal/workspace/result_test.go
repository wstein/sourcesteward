// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"encoding/json"
	"github.com/wstein/sourcesteward/internal/inspection"
	"strings"
	"testing"
)

func TestResultContractArraysAndDiagnostic(t *testing.T) {
	r := newResult("workspace-plan")
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"actions":[]`, `"diagnostics":[]`, `"limitations":[]`, `"plan":null`, `"task":null`} {
		if !strings.Contains(string(data), field) {
			t.Fatalf("missing %s: %s", field, data)
		}
	}
	r = refuse(r, "stale-plan", "plan state changed")
	if r.Status != "refused" || r.Error() != "plan state changed" {
		t.Fatalf("wrong refusal: %#v", r)
	}
}

func TestInputBoundsAndIdentifiers(t *testing.T) {
	for _, s := range []string{"", ".", "../x", "has space", strings.Repeat("a", 129), "é"} {
		if validID(s) {
			t.Fatalf("accepted %q", s)
		}
	}
	for _, s := range []string{"a", "task-1", "a._B9"} {
		if !validID(s) {
			t.Fatalf("rejected %q", s)
		}
	}
	if validText(strings.Repeat("x", 4097)) || validText(string([]byte{255})) || validText("a\x00b") {
		t.Fatal("accepted invalid bounded text")
	}
}

func TestBoundedResultAlwaysFitsHostileEvidence(t *testing.T) {
	r := newResult("workspace-apply")
	r.Status = "partial"
	r.Actions = []ActionOutcome{{"native-effect", "completed", strings.Repeat("x", recordLimit+1)}}
	r.Limitations = []string{strings.Repeat("y", recordLimit+1)}
	r = bounded(r)
	data, e := json.Marshal(r)
	if e != nil || len(data) > recordLimit {
		t.Fatalf("unbounded fallback: %d %v", len(data), e)
	}
	if len(r.Actions) == 0 || r.Actions[0].Status != "completed" {
		t.Fatal("completed native effect lost")
	}
}

func TestBoundedResultCountsEscapedJSONBytes(t *testing.T) {
	r := newResult("workspace-apply")
	for i := 0; i < 1024; i++ {
		r.Actions = append(r.Actions, ActionOutcome{strings.Repeat("\x01", 128), "completed", strings.Repeat("\x01", 1024)})
	}
	r = bounded(r)
	data, e := json.Marshal(r)
	if e != nil || len(data) > recordLimit {
		t.Fatalf("escaped output bound: %d %v", len(data), e)
	}
	if r.Status != "partial" {
		t.Fatal("bounded evidence claimed complete")
	}
}
func TestOmittedInventoryIsPartial(t *testing.T) {
	r := newResult("workspace-handoff")
	r.Status = "ok"
	r.Task = &TaskRecord{RemainingWork: []string{}, RecoveryLimits: []string{}, Inventory: &inspection.InventoryResult{InputPath: strings.Repeat("x", recordLimit+1)}}
	r = bounded(r)
	if r.Status != "partial" || r.Task == nil || r.Task.Inventory != nil {
		t.Fatal("omitted inventory did not disclose partial result")
	}
}

func TestBoundedResultCapsSmallAggregatedRecords(t *testing.T) {
	for _, kind := range []string{"limitations", "actions"} {
		r := newResult("workspace-apply")
		r.Status = "ok"
		for i := 0; i < 1025; i++ {
			if kind == "limitations" {
				r.Limitations = append(r.Limitations, "bounded")
			} else {
				r.Actions = append(r.Actions, ActionOutcome{"x", "completed", "bounded"})
			}
		}
		r = bounded(r)
		if r.Status != "partial" || len(r.Limitations) > 1024 || len(r.Actions) > 1024 {
			t.Fatalf("unbounded record count: %s actions%d limits%d", r.Status, len(r.Actions), len(r.Limitations))
		}
	}
}
