// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Decision file schema 2 (reviewer format): needs-human records a branch the
// reviewer cannot decide, and an optional repo label keeps a decision file to
// one repository. Schema 1 files keep their exact meaning.

func decisionJSONv(version int, decisions ...map[string]any) string {
	b, _ := json.Marshal(map[string]any{"schema_version": version, "decisions": decisions})
	return string(b)
}

func TestDecisionFileSchema2AddsNeedsHumanAndRepo(t *testing.T) {
	tip, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
	d2 := func(mutate func(map[string]any)) string {
		d := validDecision("refs/heads/x", tip, base)
		if mutate != nil {
			mutate(d)
		}
		return decisionJSONv(2, d)
	}
	f, err := ReadV3DecisionFile(writeDecisionFile(t, d2(func(d map[string]any) {
		d["decision"], d["reason"], d["repo"] = "needs-human", "the base moved the same file and I cannot tell which is newer", "sourcesteward"
	})))
	if err != nil || len(f.Decisions) != 1 || f.Decisions[0].Decision != "needs-human" || f.Repo != "sourcesteward" {
		t.Fatalf("needs-human: %+v %v", f, err)
	}
	if f, err = ReadV3DecisionFile(writeDecisionFile(t, d2(nil))); err != nil || f.Repo != "" || f.Decisions[0].Decision != "integrated" {
		t.Fatalf("schema 2 without repo: %+v %v", f, err)
	}
	refusals := map[string]string{
		"needs-human without a reason": d2(func(d map[string]any) { d["decision"], d["reason"] = "needs-human", "" }),
		"empty repo":                   d2(func(d map[string]any) { d["repo"] = "" }),
		"repo too long":                d2(func(d map[string]any) { d["repo"] = strings.Repeat("r", 129) }),
		"null repo":                    d2(func(d map[string]any) { d["repo"] = nil }),
		"schema 3":                     strings.Replace(d2(nil), `"schema_version":2`, `"schema_version":3`, 1),
		"case variant repo":            strings.Replace(d2(func(d map[string]any) { d["repo"] = "a" }), `"repo":`, `"Repo":`, 1),
		"mixed repositories": decisionJSONv(2,
			func() map[string]any {
				d := validDecision("refs/heads/a", tip, base)
				d["repo"] = "workharbor"
				return d
			}(),
			func() map[string]any {
				d := validDecision("refs/heads/b", tip, base)
				d["repo"] = "sourcesteward"
				return d
			}()),
	}
	for name, content := range refusals {
		t.Run(name, func(t *testing.T) {
			_, err := ReadV3DecisionFile(writeDecisionFile(t, content))
			msg := usageMessage(t, err)
			if name == "mixed repositories" && !strings.Contains(msg, "more than one repository") {
				t.Errorf("message %q", msg)
			}
		})
	}
	// Schema 1 keeps its meaning: neither the new decision nor the repo field.
	for name, mutate := range map[string]func(map[string]any){
		"needs-human in schema 1": func(d map[string]any) { d["decision"] = "needs-human" },
		"repo in schema 1":        func(d map[string]any) { d["repo"] = "sourcesteward" },
	} {
		t.Run(name, func(t *testing.T) {
			d := validDecision("refs/heads/x", tip, base)
			mutate(d)
			usageMessage(t, func() error { _, e := ReadV3DecisionFile(writeDecisionFile(t, decisionJSONv(1, d))); return e }())
		})
	}
}

func TestNeedsHumanIsANoteAndNeverClearsARef(t *testing.T) {
	f := reportRepo(t)
	r := f.decide(t, func(tip func(string) string, base string) []map[string]any {
		human := validDecision("refs/heads/real", tip("real"), base)
		human["decision"], human["reason"] = "needs-human", "cannot tell whether main carries it"
		stale := validDecision("refs/heads/bin", tip("bin"), base)
		stale["decision"], stale["reason"], stale["base_oid"] = "needs-human", "undecidable", tip("bin")
		return []map[string]any{human, stale}
	}, V3PlanOptions{})
	if r.Status != "ok" {
		t.Fatalf("%+v", r.Diagnostics)
	}
	if got, want := noteCodes(r), []string{"needs-human:refs/heads/real", "stale-decision:refs/heads/bin"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("notes %v, want %v", got, want)
	}
	if d := r.Plan.Review.Notes[0].Detail; !strings.Contains(d, "cannot tell whether main carries it") {
		t.Errorf("the reason is not in the note: %q", d)
	}
	if real := candidateByName(t, r.Plan, "real"); real.Decision != "retain" || real.Reason != "needs-review" {
		t.Errorf("needs-human changed the ref: %+v", real)
	}
}

func TestNeedsHumanIsRecordedEvenWhenTheEntryWasCut(t *testing.T) {
	f := reportRepo(t)
	r := f.decide(t, func(tip func(string) string, base string) []map[string]any {
		d := validDecision("refs/heads/clash", tip("clash"), base)
		d["decision"], d["reason"] = "needs-human", "the diff was truncated"
		return []map[string]any{d}
	}, V3PlanOptions{ReportMaxDiffBytes: 200})
	if got := noteCodes(r); !reflect.DeepEqual(got, []string{"needs-human:refs/heads/clash"}) {
		t.Fatalf("notes %v", got)
	}
}
