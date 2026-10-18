// SPDX-License-Identifier: EUPL-1.2
package readiness

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestReportImmutableExactBytes(t *testing.T) {
	raw := designFile(t, "examples/report.json")
	m := object(t, raw)
	m["findings"] = []any{map[string]any{"id": "R1", "severity": "low", "state": "accepted-followup", "owner": "owner", "evidence": "public issue"}}
	m["rounds"].([]any)[0].(map[string]any)["findings"] = []any{"R1"}
	m["blockers"] = []any{"blocker"}
	raw = jsonBytes(t, m)
	r, e := ParseReport(raw)
	if e != nil {
		t.Fatal(e)
	}
	original := append([]byte{}, raw...)
	raw[0] = '!'
	snapshot := r.Value()
	snapshot.Rounds[0].Findings[0] = "changed"
	*snapshot.Findings[0].Owner = "changed"
	*snapshot.Findings[0].Evidence = "changed"
	snapshot.Blockers[0] = "changed"
	copy := r.RawJSON()
	copy[0] = '!'
	if !bytes.Equal(r.RawJSON(), original) || r.SHA256() != fmt.Sprintf("%x", sha256.Sum256(original)) || r.Value().Rounds[0].Findings[0] == "changed" || *r.Value().Findings[0].Owner == "changed" || *r.Value().Findings[0].Evidence == "changed" || r.Value().Blockers[0] == "changed" {
		t.Fatal("mutable report alias")
	}
	spaced, e := ParseReport(append(original, ' '))
	if e != nil || spaced.SHA256() == r.SHA256() {
		t.Fatal("digest normalized raw bytes")
	}
	// The command contract caps all decoded strings at 2,048 UTF-8 bytes,
	// independently of the schemas' Unicode code-point length bounds.
	m["blockers"] = []any{strings.Repeat("é", 1025)}
	if _, e := ParseReport(jsonBytes(t, m)); e == nil {
		t.Fatal("accepted decoded string over 2048 bytes")
	}
}

func TestReportMultibyteAndEscapedUnicodeBoundaries(t *testing.T) {
	raw := designFile(t, "examples/report.json")
	m := object(t, raw)
	m["blockers"] = []any{strings.Repeat("é", 1024)}
	if _, e := ParseReport(jsonBytes(t, m)); e != nil {
		t.Fatalf("2048 decoded UTF-8 bytes refused: %v", e)
	}
	escaped := bytes.Replace(raw, []byte(`"independent reviewer"`), []byte(`"\ud83d\ude00"`), 1)
	r, e := ParseReport(escaped)
	if e != nil || r.Value().Reviewer != "😀" {
		t.Fatalf("valid surrogate pair refused: %v", e)
	}
}
