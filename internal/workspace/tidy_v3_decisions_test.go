// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// Spec 031 T033: the decision file (contracts/decisions.md), its application
// to the plan and the journal, and the apply re-check.

func decisionJSON(decisions ...map[string]any) string { return decisionJSONv(1, decisions...) }

func validDecision(branch, tip, base string) map[string]any {
	return map[string]any{"branch": branch, "tip_oid": tip, "base_oid": base, "decision": "integrated", "reason": "already on the base", "reviewer": "reviewer", "kind": "human"}
}

func writeDecisionFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(gitTempDir(t), "decisions.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func usageMessage(t *testing.T, err error) string {
	t.Helper()
	var oe operationError
	if err == nil || !errorsAs(err, &oe) || oe.Code != "invalid-options" {
		t.Fatalf("not a usage error: %v", err)
	}
	return oe.Message
}

func TestDecisionFileParsing(t *testing.T) {
	tip, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
	good := func(mutate func(map[string]any)) string {
		d := validDecision("refs/heads/x", tip, base)
		if mutate != nil {
			mutate(d)
		}
		return decisionJSON(d)
	}
	f, err := ReadV3DecisionFile(writeDecisionFile(t, good(nil)))
	if err != nil || len(f.Decisions) != 1 || f.Decisions[0].Kind != "human" || len(f.Digest) != 64 {
		t.Fatalf("valid file: %+v %v", f, err)
	}
	keep, err := ReadV3DecisionFile(writeDecisionFile(t, good(func(d map[string]any) { d["decision"] = "keep"; d["reason"] = "" })))
	if err != nil || keep.Decisions[0].Decision != "keep" {
		t.Fatalf("keep needs no reason: %v", err)
	}
	refusals := map[string]string{
		"malformed JSON":         "{",
		"unknown schema version": strings.Replace(good(nil), `"schema_version":1`, `"schema_version":3`, 1),
		"unknown top field":      strings.Replace(good(nil), `"schema_version":1`, `"schema_version":1,"extra":1`, 1),
		"unknown decision field": good(func(d map[string]any) { d["note"] = "smuggled" }),
		"invalid decision":       good(func(d map[string]any) { d["decision"] = "approve" }),
		"invalid kind":           good(func(d map[string]any) { d["kind"] = "robot" }),
		"integrated no reason":   good(func(d map[string]any) { d["reason"] = "" }),
		"drop no reason":         good(func(d map[string]any) { d["decision"] = "drop"; d["reason"] = "" }),
		"reason too long":        good(func(d map[string]any) { d["reason"] = strings.Repeat("r", 1025) }),
		"reviewer empty":         good(func(d map[string]any) { d["reviewer"] = "" }),
		"reviewer too long":      good(func(d map[string]any) { d["reviewer"] = strings.Repeat("r", 129) }),
		"branch outside heads":   good(func(d map[string]any) { d["branch"] = "refs/remotes/origin/x" }),
		"short tip":              good(func(d map[string]any) { d["tip_oid"] = "abc" }),
		"short base":             good(func(d map[string]any) { d["base_oid"] = "abc" }),
		"duplicate branch":       decisionJSON(validDecision("refs/heads/x", tip, base), validDecision("refs/heads/x", tip, base)),
		"null field":             good(func(d map[string]any) { d["reviewer"] = nil }),
	}
	for _, field := range []string{"branch", "tip_oid", "base_oid", "decision", "reason", "reviewer", "kind"} {
		refusals["missing "+field] = good(func(d map[string]any) { delete(d, field) })
	}
	for name, content := range refusals {
		t.Run(name, func(t *testing.T) {
			_, err := ReadV3DecisionFile(writeDecisionFile(t, content))
			if msg := usageMessage(t, err); !strings.Contains(msg, "--decisions") {
				t.Errorf("message does not name the flag: %q", msg)
			}
		})
	}
	t.Run("keys must be unique and exactly the fixed names", func(t *testing.T) {
		valid := good(nil)
		swap := func(a, b string) string { return strings.Replace(valid, a, b, 1) }
		for name, content := range map[string]string{
			"duplicate decision key":    swap(`"decision":"integrated"`, `"decision":"keep","decision":"integrated"`),
			"duplicate last wins":       swap(`"decision":"integrated"`, `"decision":"integrated","decision":"drop"`),
			"duplicate top-level key":   swap(`"schema_version":1`, `"schema_version":1,"schema_version":1`),
			"duplicate decisions array": strings.TrimSuffix(valid, "}") + `,"decisions":[]}`,
			"upper-case key":            swap(`"decision":`, `"DECISION":`),
			"capitalized key":           swap(`"kind":`, `"Kind":`),
			"case variant top-level":    swap(`"decisions":`, `"Decisions":`),
			"case variant version":      swap(`"schema_version":`, `"Schema_Version":`),
			"nested object in a field":  swap(`"reason":"already on the base"`, `"reason":{"a":1,"a":2}`),
			"trailing JSON":             valid + `{"schema_version":1,"decisions":[]}`,
			"trailing garbage":          valid + ` x`,
		} {
			if _, err := ReadV3DecisionFile(writeDecisionFile(t, content)); err == nil {
				t.Errorf("%s accepted", name)
			} else {
				usageMessage(t, err)
			}
		}
	})
	t.Run("object ids have the width of an object format", func(t *testing.T) {
		for _, bad := range []string{strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("A", 40), strings.Repeat("0", 40), strings.Repeat("0", 64)} {
			content := good(func(d map[string]any) { d["tip_oid"] = bad })
			if _, err := ReadV3DecisionFile(writeDecisionFile(t, content)); err == nil {
				t.Errorf("tip_oid %q accepted", bad)
			}
		}
		for _, ok := range []string{strings.Repeat("a", 40), strings.Repeat("a", 64)} {
			if _, err := ReadV3DecisionFile(writeDecisionFile(t, good(func(d map[string]any) { d["base_oid"] = ok }))); err != nil {
				t.Errorf("base_oid %q refused: %v", ok, err)
			}
		}
	})
	t.Run("the offending decision is named by index", func(t *testing.T) {
		_, err := ReadV3DecisionFile(writeDecisionFile(t, decisionJSON(validDecision("refs/heads/a", tip, base), func() map[string]any {
			d := validDecision("refs/heads/b", tip, base)
			d["decision"] = "approve"
			return d
		}())))
		if msg := usageMessage(t, err); !strings.Contains(msg, "decisions[1]") {
			t.Errorf("no index: %q", msg)
		}
	})
	t.Run("1,024 decisions pass and 1,025 refuse", func(t *testing.T) {
		many := func(n int) string {
			var ds []map[string]any
			for i := 0; i < n; i++ {
				ds = append(ds, validDecision(fmt.Sprintf("refs/heads/b%d", i), tip, base))
			}
			return decisionJSON(ds...)
		}
		if f, err := ReadV3DecisionFile(writeDecisionFile(t, many(1024))); err != nil || len(f.Decisions) != 1024 {
			t.Fatalf("1,024: %v", err)
		}
		usageMessage(t, func() error { _, e := ReadV3DecisionFile(writeDecisionFile(t, many(1025))); return e }())
	})
	t.Run("one MiB passes and one byte more refuses", func(t *testing.T) {
		content := good(nil)
		content += strings.Repeat(" ", 1<<20-len(content))
		if _, err := ReadV3DecisionFile(writeDecisionFile(t, content)); err != nil {
			t.Fatalf("exactly 1 MiB: %v", err)
		}
		usageMessage(t, func() error { _, e := ReadV3DecisionFile(writeDecisionFile(t, content+" ")); return e }())
	})
	t.Run("path kinds", func(t *testing.T) {
		dir := gitTempDir(t)
		real := filepath.Join(dir, "real.json")
		os.WriteFile(real, []byte(good(nil)), 0o600)
		link := filepath.Join(dir, "link.json")
		os.Symlink(real, link)
		fifo := filepath.Join(dir, "fifo.json")
		syscall.Mkfifo(fifo, 0o600)
		for name, path := range map[string]string{"relative": "decisions.json", "symlink": link, "directory": dir, "fifo": fifo, "missing": filepath.Join(dir, "absent.json")} {
			if _, err := ReadV3DecisionFile(path); err == nil {
				t.Errorf("%s accepted", name)
			} else {
				usageMessage(t, err)
			}
		}
	})
}

// decisionFix plans the report fixture with a decision file and returns the
// result; tips and the base come from the repository.
func (f *proofFix) decide(t *testing.T, build func(tip func(string) string, base string) []map[string]any, o V3PlanOptions) V3Result {
	t.Helper()
	tip := func(name string) string { return f.rev("refs/heads/" + name) }
	ds, version := build(tip, f.rev(f.main)), 1
	for _, d := range ds {
		if _, repo := d["repo"]; repo || d["decision"] == decisionNeedsHuman {
			version = 2
		}
	}
	o.DecisionsFile = writeDecisionFile(t, decisionJSONv(version, ds...))
	return f.planReport(o)
}

func noteCodes(r V3Result) []string {
	var out []string
	for _, n := range r.Plan.Review.Notes {
		out = append(out, n.Code+":"+n.Branch)
	}
	return out
}

func TestDecisionsMakeNeedsReviewRefsDeletableOrRetained(t *testing.T) {
	f := reportRepo(t)
	plain := f.planReport(V3PlanOptions{})
	r := f.decide(t, func(tip func(string) string, base string) []map[string]any {
		integrated := validDecision("refs/heads/real", tip("real"), base)
		drop := validDecision("refs/heads/bin", tip("bin"), base)
		drop["decision"], drop["reason"], drop["kind"] = "drop", "abandoned experiment", "ai"
		keep := validDecision("refs/heads/exec", tip("exec"), base)
		keep["decision"], keep["reason"] = "keep", ""
		return []map[string]any{integrated, drop, keep}
	}, V3PlanOptions{})
	if r.Status != "ok" {
		t.Fatalf("%+v", r.Diagnostics)
	}
	p := r.Plan
	real, bin, exec := candidateByName(t, p, "real"), candidateByName(t, p, "bin"), candidateByName(t, p, "exec")
	if real.Decision != "delete" || real.Reason != "reviewed-integrated" || real.Proof != "reviewed-integrated" {
		t.Errorf("integrated: %+v", real)
	}
	e := real.Evidence.Integration
	if e.Class != "reviewed-integrated" || e.Strength != "reviewer-decision" || e.Disposition != "proven" || e.Decision == nil || e.Decision.Kind != "human" || e.Decision.Reason != "already on the base" {
		t.Errorf("integrated evidence: %+v", e)
	}
	if bin.Decision != "delete" || bin.Proof != "reviewed-drop" || bin.Evidence.Integration.Decision.Kind != "ai" {
		t.Errorf("drop: %+v", bin)
	}
	if exec.Decision != "retain" || exec.Reason != "reviewed-keep" || exec.Evidence.Integration.Decision.Decision != "keep" || exec.Evidence.Integration.Disposition != "retained" {
		t.Errorf("keep: %+v %+v", exec, exec.Evidence.Integration)
	}
	// Nothing else changed, and the file digest is bound into the plan.
	for _, c := range p.Candidates {
		if c.Name == real.Name || c.Name == bin.Name || c.Name == exec.Name {
			continue
		}
		if !reflect.DeepEqual(c, candidateByName(t, plain.Plan, strings.TrimPrefix(c.Name, "refs/heads/"))) {
			t.Errorf("%s changed without a decision", c.Name)
		}
	}
	if len(p.Review.DecisionsDigest) != 64 || p.Digest == plain.Plan.Digest || r.ReviewSummary.Proven != plain.ReviewSummary.Proven+2 || r.ReviewSummary.NeedsReview != plain.ReviewSummary.NeedsReview-3 {
		t.Errorf("binding %+v summary %+v", p.Review, r.ReviewSummary)
	}
	if len(p.Review.Notes) != 0 {
		t.Errorf("notes %v", noteCodes(r))
	}
}

func TestDecisionsAreIgnoredWithANoteWhenTheyDoNotApply(t *testing.T) {
	f := reportRepo(t)
	r := f.decide(t, func(tip func(string) string, base string) []map[string]any {
		d := func(name string, mutate func(map[string]any)) map[string]any {
			x := validDecision("refs/heads/"+name, tip(name), base)
			if mutate != nil {
				mutate(x)
			}
			return x
		}
		return []map[string]any{
			d("real", func(x map[string]any) { x["tip_oid"] = tip("bin") }),
			d("bin", func(x map[string]any) { x["base_oid"] = tip("bin") }),
			d("trunk", nil),
			d("rebased", nil),
			d("ancestor", nil),
			validDecision("refs/heads/nosuch", tip("real"), base),
		}
	}, V3PlanOptions{})
	if r.Status != "ok" {
		t.Fatalf("%+v", r.Diagnostics)
	}
	want := []string{"stale-decision:refs/heads/real", "stale-decision:refs/heads/bin", "decision-not-applicable:refs/heads/trunk", "decision-not-applicable:refs/heads/rebased", "decision-not-applicable:refs/heads/ancestor", "decision-not-applicable:refs/heads/nosuch"}
	if got := noteCodes(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("notes %v, want %v", got, want)
	}
	for i, why := range []string{"", "", "protected or a hard stop", "already proven", "already proven", "absent from the plan"} {
		if why != "" && !strings.Contains(r.Plan.Review.Notes[i].Detail, why) {
			t.Errorf("note %d: %q lacks %q", i, r.Plan.Review.Notes[i].Detail, why)
		}
	}
	if n := r.Plan.Review.Notes[0]; !strings.Contains(n.Detail, tip(f, "real")) || !strings.Contains(n.Detail, tip(f, "bin")) {
		t.Errorf("stale note must name both OIDs: %q", n.Detail)
	}
	if real := candidateByName(t, r.Plan, "real"); real.Decision != "retain" || real.Reason != "needs-review" {
		t.Errorf("a stale decision cleared a ref: %+v", real)
	}
}

func tip(f *proofFix, name string) string { return f.rev("refs/heads/" + name) }

func TestDecisionsNeedTheWholeEntryToHaveBeenReviewed(t *testing.T) {
	f := reportRepo(t)
	build := func(names ...string) func(func(string) string, string) []map[string]any {
		return func(tip func(string) string, base string) []map[string]any {
			var ds []map[string]any
			for _, n := range names {
				ds = append(ds, validDecision("refs/heads/"+n, tip(n), base))
			}
			return ds
		}
	}
	t.Run("entry bound", func(t *testing.T) {
		r := f.decide(t, build("bin", "ws"), V3PlanOptions{ReportMaxEntries: 1})
		if r.Status != "ok" {
			t.Fatalf("%+v", r.Diagnostics)
		}
		if bin, ws := candidateByName(t, r.Plan, "bin"), candidateByName(t, r.Plan, "ws"); bin.Decision != "delete" || ws.Decision != "retain" {
			t.Errorf("bin %+v ws %+v", bin.Decision, ws.Decision)
		}
		if got := noteCodes(r); !reflect.DeepEqual(got, []string{"decision-not-applicable:refs/heads/ws"}) || !strings.Contains(r.Plan.Review.Notes[0].Detail, "not-fully-reviewed") {
			t.Errorf("notes %v %+v", got, r.Plan.Review.Notes)
		}
	})
	t.Run("truncated diff", func(t *testing.T) {
		r := f.decide(t, build("clash", "bin"), V3PlanOptions{ReportMaxDiffBytes: 200})
		if r.Status != "ok" {
			t.Fatalf("%+v", r.Diagnostics)
		}
		if c := candidateByName(t, r.Plan, "clash"); c.Decision != "retain" {
			t.Errorf("a truncated entry was cleared: %+v", c)
		}
		if len(r.Plan.Review.Notes) == 0 || !strings.Contains(r.Plan.Review.Notes[0].Detail, "not-fully-reviewed") {
			t.Errorf("notes %+v", r.Plan.Review.Notes)
		}
	})
	t.Run("byte bound", func(t *testing.T) {
		dir := gitTempDir(t)
		if r := f.planReport(V3PlanOptions{ReportDir: dir}); r.Status != "ok" {
			t.Fatalf("%+v", r.Diagnostics)
		}
		full, _ := os.ReadFile(filepath.Join(dir, "tidy-report.json"))
		r := f.decide(t, build("wsmatch"), V3PlanOptions{ReportMaxBytes: len(full) * 2 / 3})
		if r.Status != "ok" {
			t.Fatalf("%+v", r.Diagnostics)
		}
		if c := candidateByName(t, r.Plan, "wsmatch"); c.Decision != "retain" || c.Reason != "needs-review" {
			t.Errorf("an entry left out of the report was cleared: %+v", c)
		}
		if got := noteCodes(r); !reflect.DeepEqual(got, []string{"decision-not-applicable:refs/heads/wsmatch"}) {
			t.Errorf("notes %v", got)
		}
	})
}

func TestDecisionsHardStopsAreNeverCleared(t *testing.T) {
	f := reportRepo(t)
	f.write(".git/info/grafts", f.rev("HEAD")+"\n")
	r := f.decide(t, func(tip func(string) string, base string) []map[string]any {
		return []map[string]any{validDecision("refs/heads/real", tip("real"), base)}
	}, V3PlanOptions{})
	if r.Status != "ok" {
		t.Skipf("planning refuses grafts here: %+v", r.Diagnostics)
	}
	if c := candidateByName(t, r.Plan, "real"); c.Decision != "retain" || len(noteCodes(r)) != 1 {
		t.Errorf("hard stop cleared: %+v %v", c, noteCodes(r))
	}
}

func TestDecisionsAreRecordedInTheJournalAndApplyStillRequiresAuthorization(t *testing.T) {
	f := classFix(t)
	r := f.decide(t, func(tip func(string) string, base string) []map[string]any {
		d := validDecision("refs/heads/fresh", tip("fresh"), base)
		d["kind"] = "ai"
		return []map[string]any{d}
	}, V3PlanOptions{})
	if r.Status != "ok" {
		t.Fatalf("%+v", r.Diagnostics)
	}
	p := r.Plan
	apply := func(authorize bool) V3Result {
		return defaultTidyV3RefsApplyCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: authorize, ProtectWorktrees: []string{}})
	}
	if a := apply(false); a.Status == "ok" {
		t.Fatal("a decision authorized an apply")
	}
	if out := f.git("for-each-ref", "refs/heads/fresh", "refs/stw/recovery"); strings.Contains(out, "recovery") {
		t.Fatalf("effects before authorization: %q", out)
	}
	a := apply(true)
	if a.Status != "ok" {
		t.Fatalf("apply: %+v", a.Diagnostics)
	}
	if out := f.git("for-each-ref", "refs/heads/fresh"); out != "" {
		t.Errorf("the reviewed branch still exists: %q", out)
	}
	real := candidateByName(t, p, "fresh")
	if got := f.git("rev-parse", v3RecoveryFor(*p, real.ActionID, real.OID).Name); got != real.OID {
		t.Errorf("recovery at %s", got)
	}
	raw, err := os.ReadFile(filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"decision":"integrated"`, `"kind":"ai"`, `"reviewer":"reviewer"`, `"reason":"already on the base"`, real.OID} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("journal lacks %s", want)
		}
	}
}

func TestApplyOfAReviewedDecisionRechecksTheTipAndProtections(t *testing.T) {
	setup := func(t *testing.T) (*proofFix, V3Plan) {
		f := classFix(t)
		r := f.decide(t, func(tip func(string) string, base string) []map[string]any {
			return []map[string]any{validDecision("refs/heads/fresh", tip("fresh"), base)}
		}, V3PlanOptions{})
		if r.Status != "ok" {
			t.Fatalf("%+v", r.Diagnostics)
		}
		return f, *r.Plan
	}
	apply := func(p V3Plan) V3Result {
		return defaultTidyV3RefsApplyCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	}
	t.Run("the tip moved", func(t *testing.T) {
		f, p := setup(t)
		f.checkout("fresh")
		f.commit("one more", map[string]string{"late.txt": "late\n"})
		f.checkout(f.main)
		if a := apply(p); a.Status == "ok" {
			t.Fatal("applied at a moved tip")
		}
		if f.git("for-each-ref", "refs/heads/fresh") == "" {
			t.Error("the branch was deleted")
		}
	})
	t.Run("a worktree took the branch", func(t *testing.T) {
		f, p := setup(t)
		f.git("worktree", "add", "-q", filepath.Join(gitTempDir(t), "wt"), "fresh")
		if a := apply(p); a.Status == "ok" {
			t.Fatal("applied to a branch now used by a workspace")
		}
		if f.git("for-each-ref", "refs/heads/fresh") == "" {
			t.Error("the branch was deleted")
		}
	})
}

// A stored plan whose reviewed evidence does not bind its decision is refused.
func TestForgedReviewedEvidenceIsRefused(t *testing.T) {
	f := classFix(t)
	r := f.decide(t, func(tip func(string) string, base string) []map[string]any {
		keep := validDecision("refs/heads/ws", tip("ws"), base)
		keep["decision"], keep["reason"] = "keep", ""
		return []map[string]any{validDecision("refs/heads/fresh", tip("fresh"), base), keep}
	}, V3PlanOptions{})
	if r.Status != "ok" {
		t.Fatalf("%+v", r.Diagnostics)
	}
	forge := map[string]func(p *V3Plan){
		"another tip":           func(p *V3Plan) { candidateEvidence(p, "fresh").Decision.TipOID = strings.Repeat("c", 40) },
		"another base":          func(p *V3Plan) { candidateEvidence(p, "fresh").Decision.BaseOID = strings.Repeat("c", 40) },
		"drop for integrated":   func(p *V3Plan) { candidateEvidence(p, "fresh").Decision.Decision = "drop" },
		"no digest":             func(p *V3Plan) { p.Review.DecisionsDigest = "" },
		"with reasons":          func(p *V3Plan) { candidateEvidence(p, "fresh").Reasons = []string{"conflict"} },
		"wrong strength":        func(p *V3Plan) { candidateEvidence(p, "fresh").Strength = "ancestry" },
		"invalid kind":          func(p *V3Plan) { candidateEvidence(p, "fresh").Decision.Kind = "robot" },
		"keep as drop":          func(p *V3Plan) { candidateEvidence(p, "ws").Decision.Decision = "drop" },
		"keep with another tip": func(p *V3Plan) { candidateEvidence(p, "ws").Decision.TipOID = strings.Repeat("c", 40) },
		"decision on a patch-id": func(p *V3Plan) {
			e := candidateEvidence(p, "fresh")
			e.Class, e.Strength = "patch-id", "history-equivalent"
		},
	}
	for name, mutate := range forge {
		t.Run(name, func(t *testing.T) {
			var p V3Plan
			raw, _ := json.Marshal(r.Plan)
			if err := json.Unmarshal(raw, &p); err != nil {
				t.Fatal(err)
			}
			mutate(&p)
			var err error
			if p.Digest, err = v3PlanDigest(p); err != nil {
				t.Fatal(err)
			}
			if validateV3Plan(p, p.StateDir, p.PlanID) == nil {
				t.Fatal("forged reviewed evidence admitted")
			}
		})
	}
}

func candidateEvidence(p *V3Plan, name string) *V3IntegrationEvidence {
	for i := range p.Candidates {
		if p.Candidates[i].Name == "refs/heads/"+name {
			return p.Candidates[i].Evidence.Integration
		}
	}
	panic("no candidate " + name)
}
