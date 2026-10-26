// SPDX-License-Identifier: EUPL-1.2
package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

func pruneRow(n int) tidywizard.RegistrationPrune {
	return tidywizard.RegistrationPrune{ID: fmt.Sprintf("%064x", n+1), Kind: "registration-prune", Repo: "a", Name: fmt.Sprintf("missing-%d", n), GitDir: fmt.Sprintf("/repo/.git/worktrees/missing-%d", n), Destination: fmt.Sprintf("/missing/%d", n), Owner: "codex", OwnerSource: owner.SourceTaskRecord, Scope: owner.ScopeOwn, Task: fmt.Sprintf("task-%d", n), Verdict: tidywizard.RegistrationPruneSafe, Reason: "registration-prune-ready", Proof: "owned-missing-registration", Effects: []string{"remove-registration"}, EvidenceDigest: strings.Repeat("a", 64), Limitations: []string{"Administrative metadata only; no checkout files are removed."}}
}

func TestOverviewV4IsExplicitAndDoesNotChangeLegacyOutput(t *testing.T) {
	in := input()
	before, _, e := JSON(Build(in))
	if e != nil {
		t.Fatal(e)
	}
	d := BuildV4(in, nil)
	b, cut, e := JSONV4(d)
	if e != nil || cut {
		t.Fatalf("v4 encode: cut=%v error=%v", cut, e)
	}
	var raw map[string]any
	if e := json.Unmarshal(b, &raw); e != nil {
		t.Fatal(e)
	}
	if raw["schema_version"] != float64(4) || raw["command"] != "tidy-overview" || raw["registration_prunes"] == nil || len(raw["registration_prunes"].([]any)) != 0 {
		t.Fatalf("v4 envelope: %s", b)
	}
	after, _, e := JSON(Build(in))
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("building v4 changed legacy input or output")
	}
	if bytes.Contains(before, []byte("registration_prunes")) {
		t.Fatal("legacy overview gained the new field")
	}
}

func TestOverviewV4ReplacesOnlyMatchingLegacyRegistrations(t *testing.T) {
	p := pruneRow(0)
	for _, kind := range []tidywizard.Kind{tidywizard.KindWorktree, tidywizard.KindStaleWorktree} {
		t.Run(string(kind), func(t *testing.T) {
			in := input()
			matched := cand("a", kind, p.Destination, tidywizard.VerdictRetain, "evidence-incomplete", owner.ScopeOwn)
			otherRepo := cand("b", kind, p.Destination, tidywizard.VerdictRetain, "evidence-incomplete", owner.ScopeOwn)
			branch := cand("a", tidywizard.KindBranch, "branch", tidywizard.VerdictRetain, "unmerged", owner.ScopeOwn)
			in.Result.Candidates = []tidywizard.Candidate{matched, otherRepo, branch}
			d := BuildV4(in, []tidywizard.RegistrationPrune{p})
			if len(d.Candidates) != 2 || d.Candidates[0].Repo != "b" || d.Candidates[1].Kind != tidywizard.KindBranch || len(d.RegistrationPrunes) != 1 {
				t.Fatalf("deduplication: %+v", d)
			}
			if len(in.Result.Candidates) != 3 || in.Result.Candidates[0].ID != matched.ID {
				t.Fatal("v4 mutated the legacy candidate slice")
			}
		})
	}
}

func TestOverviewV4CombinedCandidateBound(t *testing.T) {
	for _, tc := range []struct {
		legacy, prunes int
		cut            bool
	}{{4095, 1, false}, {4096, 1, true}, {0, 4096, false}, {0, 4097, true}} {
		t.Run(fmt.Sprintf("%d-%d", tc.legacy, tc.prunes), func(t *testing.T) {
			in := input()
			in.Result.Candidates = nil
			for i := 0; i < tc.legacy; i++ {
				in.Result.Candidates = append(in.Result.Candidates, cand("a", tidywizard.KindBranch, fmt.Sprint(i), tidywizard.VerdictRetain, "unmerged", owner.ScopeOwn))
			}
			rows := make([]tidywizard.RegistrationPrune, tc.prunes)
			for i := range rows {
				rows[i] = pruneRow(i)
			}
			d := BuildV4(in, rows)
			if len(d.Candidates)+len(d.RegistrationPrunes) != min(tc.legacy+tc.prunes, 4096) || d.Truncated != tc.cut {
				t.Fatalf("combined bound: legacy=%d prunes=%d cut=%v", len(d.Candidates), len(d.RegistrationPrunes), d.Truncated)
			}
		})
	}
}

func TestOverviewV4JSONBoundsBothCandidateArrays(t *testing.T) {
	in := input()
	rows := make([]tidywizard.RegistrationPrune, 220)
	for i := range rows {
		rows[i] = pruneRow(i)
		rows[i].Limitations = []string{strings.Repeat("x", 40000)}
	}
	b, cut, e := JSONV4(BuildV4(in, rows))
	if e != nil || !cut || len(b) > MaxDocumentBytes {
		t.Fatalf("output bound: bytes=%d cut=%v error=%v", len(b), cut, e)
	}
	var d DocumentV4
	if e := json.Unmarshal(b, &d); e != nil {
		t.Fatal(e)
	}
	if !d.Truncated || len(d.RegistrationPrunes) >= len(rows) || len(d.RegistrationPrunes) == 0 {
		t.Fatalf("missing complete truncated rows: %d", len(d.RegistrationPrunes))
	}
	in.Notices = []string{strings.Repeat("x", MaxDocumentBytes+1)}
	if b, _, e := JSONV4(BuildV4(in, nil)); e == nil || len(b) != 0 {
		t.Fatal("oversized envelope must fail without emitting an invalid document")
	}
}

func TestOverviewV4EscapesPruneTextAndKeepsDisplayBound(t *testing.T) {
	in := input()
	rows := make([]tidywizard.RegistrationPrune, 12)
	hostile := "name\x1b[2J\r\x00\u202e" + strings.Repeat("long", 100)
	for i := range rows {
		rows[i] = pruneRow(i)
		rows[i].Name = hostile
		rows[i].GitDir = hostile
		rows[i].Destination = hostile
		rows[i].Owner = hostile
		rows[i].Task = hostile
		rows[i].Limitations = []string{hostile}
	}
	text, cut := TextV4(BuildV4(in, rows), TextOptions{})
	if cut {
		t.Fatal("small text overview was cut")
	}
	if strings.ContainsAny(text, "\x1b\r\x00\u202e") || !strings.Contains(text, untrusted.Esc(hostile)) || !strings.Contains(text, "and 2 more") {
		t.Fatalf("escaped bounded text missing: %s", text)
	}
	b, _, e := JSONV4(BuildV4(in, rows))
	if e != nil || bytes.Contains(b, []byte("\u202e")) {
		t.Fatalf("JSON invisible-character escaping: %v", e)
	}
}

func TestOverviewV4PlanIDUsesSeparateManifestDomain(t *testing.T) {
	in := input()
	for _, id := range []string{"", strings.Repeat("a", 64)} {
		in.PlanID = id
		if _, _, e := JSONV4(BuildV4(in, nil)); e != nil {
			t.Fatalf("valid v4 id %q: %v", id, e)
		}
	}
	for _, id := range []string{strings.Repeat("a", 12), strings.Repeat("A", 64), "../plan"} {
		in.PlanID = id
		if _, _, e := JSONV4(BuildV4(in, nil)); e == nil {
			t.Fatalf("accepted incompatible manifest id %q", id)
		}
	}
}

func TestOverviewV4TextBoundsWholeDocument(t *testing.T) {
	row := pruneRow(0)
	row.Limitations = make([]string, 50000)
	for i := range row.Limitations {
		row.Limitations[i] = strings.Repeat("x", 120)
	}
	text, cut := TextV4(BuildV4(input(), []tidywizard.RegistrationPrune{row}), TextOptions{})
	if !cut || len(text) > MaxDocumentBytes || !strings.Contains(text, "output-limit") {
		t.Fatalf("text byte bound: bytes=%d cut=%v", len(text), cut)
	}
}

func TestOverviewV4LegacyRowsRemainPlainText(t *testing.T) {
	in := input()
	in.Result.Candidates = []tidywizard.Candidate{cand("a", tidywizard.KindBranch, "plain-branch", tidywizard.VerdictSafe, "merged", owner.ScopeOwn)}
	text, cut := TextV4(BuildV4(in, nil), TextOptions{})
	if cut || !strings.Contains(text, "plain-branch") || strings.Contains(text, "\x1b[") {
		t.Fatalf("plain overview row: cut=%v text=%q", cut, text)
	}
}
