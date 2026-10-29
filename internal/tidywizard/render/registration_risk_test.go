// SPDX-License-Identifier: EUPL-1.2
package render

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
)

func TestRegistrationRiskRequiresEveryNativeGate(t *testing.T) {
	cases := []struct {
		name          string
		change        func(*tidywizard.RegistrationPrune)
		level, reason string
	}{
		{"admitted", func(*tidywizard.RegistrationPrune) {}, "orange", "owned-missing-registration"},
		{"ownership refusal", func(p *tidywizard.RegistrationPrune) {
			p.Verdict = tidywizard.RegistrationPruneRetain
			p.Reason = "registration-prune-ownership-unproven"
			p.Proof = "none"
			p.Effects = nil
		}, "kept", ""},
		{"absence refusal", func(p *tidywizard.RegistrationPrune) {
			p.Verdict = tidywizard.RegistrationPruneRetain
			p.Reason = "registration-prune-absence-unproven"
			p.Proof = "none"
			p.Effects = nil
		}, "kept", ""},
		{"blocked", func(p *tidywizard.RegistrationPrune) { p.Verdict = tidywizard.RegistrationPruneBlocked }, "red", "not-judged"},
		{"unknown", func(p *tidywizard.RegistrationPrune) { p.Verdict = tidywizard.RegistrationPruneUnknown }, "red", "not-judged"},
		{"other retain", func(p *tidywizard.RegistrationPrune) {
			p.Verdict = tidywizard.RegistrationPruneRetain
			p.Reason = "unexpected"
		}, "red", "not-judged"},
	}
	gates := map[string]func(*tidywizard.RegistrationPrune){
		"id":                  func(p *tidywizard.RegistrationPrune) { p.ID = strings.Repeat("b", 64) },
		"kind":                func(p *tidywizard.RegistrationPrune) { p.Kind = "stale-worktree" },
		"repo":                func(p *tidywizard.RegistrationPrune) { p.Repo = "" },
		"name":                func(p *tidywizard.RegistrationPrune) { p.Name = "" },
		"gitdir":              func(p *tidywizard.RegistrationPrune) { p.GitDir = "" },
		"destination":         func(p *tidywizard.RegistrationPrune) { p.Destination = "" },
		"owner":               func(p *tidywizard.RegistrationPrune) { p.Owner = "" },
		"owner source":        func(p *tidywizard.RegistrationPrune) { p.OwnerSource = owner.SourcePathConvention },
		"task":                func(p *tidywizard.RegistrationPrune) { p.Task = "" },
		"unknown scope":       func(p *tidywizard.RegistrationPrune) { p.Scope = owner.ScopeUnknown },
		"bad scope":           func(p *tidywizard.RegistrationPrune) { p.Scope = "future" },
		"contradictory scope": func(p *tidywizard.RegistrationPrune) { p.Scope = owner.ScopeOther },
		"ready reason":        func(p *tidywizard.RegistrationPrune) { p.Reason = "integrated" },
		"proof":               func(p *tidywizard.RegistrationPrune) { p.Proof = "ancestor" },
		"empty effects":       func(p *tidywizard.RegistrationPrune) { p.Effects = nil },
		"file effect":         func(p *tidywizard.RegistrationPrune) { p.Effects = append(p.Effects, "remove-files") },
		"branch effect":       func(p *tidywizard.RegistrationPrune) { p.Effects = []string{"delete-branch"} },
		"digest":              func(p *tidywizard.RegistrationPrune) { p.EvidenceDigest = "" },
		"uppercase digest":    func(p *tidywizard.RegistrationPrune) { p.EvidenceDigest = strings.Repeat("A", 64) },
	}
	for name, change := range gates {
		cases = append(cases, struct {
			name          string
			change        func(*tidywizard.RegistrationPrune)
			level, reason string
		}{name, change, "red", "not-judged"})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := pruneRow(0)
			p.ID = tidywizard.RegistrationPruneID(p.Repo, p.Name, p.GitDir)
			tc.change(&p)
			if tc.name != "id" {
				p.ID = tidywizard.RegistrationPruneID(p.Repo, p.Name, p.GitDir)
			}
			in := input()
			in.Mode.Caller = "codex"
			d := BuildV4(in, []tidywizard.RegistrationPrune{p})
			b, _, err := JSONV4(d)
			if err != nil {
				t.Fatal(err)
			}
			var raw struct {
				Rows []map[string]any `json:"registration_prunes"`
			}
			if err = json.Unmarshal(b, &raw); err != nil {
				t.Fatal(err)
			}
			r := raw.Rows[0]
			if r["risk"] != tc.level {
				t.Fatalf("risk=%v want %s: %s", r["risk"], tc.level, b)
			}
			if tc.reason == "" {
				if _, ok := r["risk_reasons"]; ok {
					t.Fatal("kept reasons must be omitted")
				}
			} else if !reflect.DeepEqual(r["risk_reasons"], []any{tc.reason}) {
				t.Fatalf("reasons=%v", r["risk_reasons"])
			}
			if r["reason"] != p.Reason || r["proof"] != p.Proof || r["id"] != p.ID || r["evidence_digest"] != p.EvidenceDigest {
				t.Fatal("annotation changed native evidence")
			}
			for _, key := range []string{"pr", "group"} {
				if _, ok := r[key]; ok {
					t.Fatalf("unexpected %s", key)
				}
			}
			text, _ := TextV4(d, TextOptions{})
			if !strings.Contains(text, "risk "+tc.level) || !strings.Contains(text, p.Reason) {
				t.Fatalf("missing risk or exact reason: %s", text)
			}
			if tc.level == "orange" && !strings.Contains(text, "Only the registration would be removed; the missing checkout may have been moved.") {
				t.Fatal("missing orange warning")
			}
		})
	}
}

func TestRegistrationRiskCountsBeforeEveryOutputCut(t *testing.T) {
	in := input()
	in.Mode.Caller = "codex"
	in.Result.Candidates = nil
	rows := make([]tidywizard.RegistrationPrune, tidywizard.MaxCandidates+1)
	for i := range rows {
		rows[i] = pruneRow(i)
		p := &rows[i]
		p.ID = tidywizard.RegistrationPruneID(p.Repo, p.Name, p.GitDir)
	}
	rows[0].Verdict = tidywizard.RegistrationPruneRetain
	rows[0].Reason = "registration-prune-absence-unproven"
	rows[1].Verdict = tidywizard.RegistrationPruneUnknown
	d := BuildV4(in, rows)
	b, cut, err := JSONV4(d)
	if err != nil || !cut {
		t.Fatalf("cut=%v err=%v", cut, err)
	}
	assertCounts := func(b []byte) {
		t.Helper()
		var raw struct {
			Summary struct {
				Prunes                 map[string]int `json:"registration_prunes"`
				Safe, Retain, Decision int
			} `json:"summary"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatal(err)
		}
		want := map[string]int{"green": 0, "yellow": 0, "orange": len(rows) - 2, "red": 1, "kept": 1}
		if !reflect.DeepEqual(raw.Summary.Prunes, want) || raw.Summary.Safe+raw.Summary.Retain+raw.Summary.Decision != 0 {
			t.Fatalf("summary=%+v", raw.Summary)
		}
	}
	assertCounts(b)
	rows = rows[:220]
	d = BuildV4(in, rows)
	for i := range d.RegistrationPrunes {
		d.RegistrationPrunes[i].Limitations = []string{strings.Repeat("x", 40000)}
	}
	b, cut, err = JSONV4(d)
	if err != nil || !cut {
		t.Fatalf("byte cut=%v err=%v", cut, err)
	}
	assertCounts(b)
	text, _ := TextV4(d, TextOptions{})
	if !strings.Contains(text, "registration risks: 0 green, 0 yellow, 218 orange, 1 red, 1 kept") {
		t.Fatal("text counts used truncated rows")
	}
}

func TestRegistrationRiskDoesNotEnterLegacyTotalsOrGroups(t *testing.T) {
	in := input()
	in.Mode.Caller = "codex"
	in.Now = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	stale := cand("a", tidywizard.KindStaleWorktree, "/missing/legacy", tidywizard.VerdictRetain, "evidence-incomplete", owner.ScopeOwn)
	in.Result.Candidates = append(in.Result.Candidates, stale)
	legacy := Build(in)
	p := pruneRow(0)
	p.ID = tidywizard.RegistrationPruneID(p.Repo, p.Name, p.GitDir)
	d := BuildV4(in, []tidywizard.RegistrationPrune{p})
	if d.Summary.Safe != legacy.Summary.Safe || d.Summary.Retain != legacy.Summary.Retain || d.Summary.Decision != legacy.Summary.Decision || !reflect.DeepEqual(d.Summary.Risk, legacy.Summary.Risk) || !reflect.DeepEqual(Groups(d.Document), Groups(legacy)) {
		t.Fatal("native prune changed legacy totals or groups")
	}
	for _, c := range d.Candidates {
		if c.ID == stale.ID && c.Risk != "kept" {
			t.Fatalf("legacy stale row changed: %+v", c)
		}
	}
	if p.Risk != "" || p.RiskReasons != nil {
		t.Fatal("builder mutated input annotations")
	}
}
