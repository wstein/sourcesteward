// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/cli"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// Spec 031 T035, T036: the review report, consolidation ranges and decision
// file through the real CLI, against the candidate schemas.

type reviewRepo struct {
	t      *testing.T
	source string
	git    func(args ...string) string
}

func newReviewRepo(t *testing.T) *reviewRepo {
	t.Helper()
	binary := tidyV3FixtureGitBinary(t)
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	env := tidyV3FixtureEnv(home, t.TempDir())
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &reviewRepo{t: t, source: filepath.Join(parent, "source")}
	r.git = func(args ...string) string {
		t.Helper()
		raw, err := tidyV3FixtureCommand(binary, env, append([]string{"-c", "user.name=Tester", "-c", "user.email=test@example.org"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %q: %s %v", args, raw, err)
		}
		return strings.TrimSpace(string(raw))
	}
	commit := func(msg, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(r.source, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		r.git("-C", r.source, "add", "-A")
		r.git("-C", r.source, "commit", "-qm", msg)
	}
	r.git("init", "-q", "-b", "main", r.source)
	commit("base", "file", "stored\n")
	r.git("-C", r.source, "checkout", "-qb", "fresh")
	commit("fresh", "fresh.txt", "fresh\n")
	r.git("-C", r.source, "checkout", "-qb", "second", "main")
	commit("second", "second.txt", "second\n")
	r.git("-C", r.source, "checkout", "-q", "main")
	commit("main moves", "other", "o\n")
	return r
}

func (r *reviewRepo) run(extra ...string) (int, string, string) {
	r.t.Helper()
	args := append([]string{"tidy", "plan", r.source, "--contract", "3", "--base", "refs/heads/main"}, extra...)
	var out, diagnostic bytes.Buffer
	code := cli.Run(context.Background(), "test", args, &out, &diagnostic)
	return code, out.String(), diagnostic.String()
}

func (r *reviewRepo) runJSON(extra ...string) (int, workspace.V3Result, []byte) {
	r.t.Helper()
	args := append(append([]string{"tidy", "plan", r.source, "--contract", "3", "--base", "refs/heads/main"}, extra...), "--json")
	var out, diagnostic bytes.Buffer
	code := cli.Run(context.Background(), "test", args, &out, &diagnostic)
	var res workspace.V3Result
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		r.t.Fatalf("code=%d output=%s diagnostic=%s", code, out.String(), diagnostic.String())
	}
	return code, res, out.Bytes()
}

func TestTidyV3ReviewSummaryAndReportThroughTheCLI(t *testing.T) {
	r := newReviewRepo(t)
	s := schemaAt(t, "v3", "tidy")
	reportSchema := schemaAt(t, "v3", "tidy-report")
	code, res, raw := r.runJSON()
	if code != 0 || res.ReviewSummary == nil || res.ReviewSummary.NeedsReview != 2 || res.ReviewSummary.ReportWritten || res.Plan.Review != nil {
		t.Fatalf("without a report: code=%d %+v", code, res.ReviewSummary)
	}
	valid(t, s, raw)
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	code, res, raw = r.runJSON("--report-dir", dir, "--report-max-entries", "1", "--consolidation-range", "main~1..main")
	if code != 0 {
		t.Fatalf("a report with review entries must not change the exit code: %d", code)
	}
	valid(t, s, raw)
	rs := res.ReviewSummary
	if rs == nil || !rs.ReportWritten || rs.Entries != 1 || rs.Omitted != 1 || res.Plan.Review == nil || res.Plan.Review.MaxEntries != 1 || len(res.Plan.Review.Ranges) != 1 {
		t.Fatalf("summary %+v review %+v", rs, res.Plan.Review)
	}
	report, err := os.ReadFile(filepath.Join(dir, "tidy-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	valid(t, reportSchema, report)
	// The human output carries the same facts.
	dir2, _ := filepath.EvalSymlinks(t.TempDir())
	_, human, _ := r.run("--report-dir", dir2, "--report-max-entries", "1", "--consolidation-range", "main~1..main")
	for _, fact := range []string{"Review summary: proven=0 needs_review=2 retained=1 report_written=true entries=1 omitted=1", "Review bounds: max_entries=1", `Review range: "main~1..main"`} {
		if !strings.Contains(human, fact) {
			t.Errorf("fact %q missing from the human output:\n%s", fact, human)
		}
	}
}

func TestTidyV3ReviewUsageErrorsExitTwo(t *testing.T) {
	r := newReviewRepo(t)
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	os.WriteFile(filepath.Join(dir, "tidy-report.md"), nil, 0o600)
	badDecisions := filepath.Join(t.TempDir(), "d.json")
	os.WriteFile(badDecisions, []byte(`{"schema_version":3,"decisions":[]}`), 0o600)
	inRepo := filepath.Join(r.source, "reports")
	os.Mkdir(inRepo, 0o700)
	for name, extra := range map[string][]string{
		"malformed range":        {"--consolidation-range", "main"},
		"unresolvable range":     {"--consolidation-range", "nosuch..main"},
		"existing report target": {"--report-dir", dir},
		"report dir in the repo": {"--report-dir", inRepo},
		"bad decision file":      {"--decisions", badDecisions},
		"missing decision file":  {"--decisions", filepath.Join(t.TempDir(), "absent.json")},
	} {
		t.Run(name, func(t *testing.T) {
			if code, out, diagnostic := r.run(extra...); code != 2 {
				t.Fatalf("exit %d, want 2: %s %s", code, out, diagnostic)
			}
		})
	}
}

func TestTidyV3ReportWriteFailureExitsOneAndStoresNoPlan(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a read-only directory does not stop root")
	}
	r := newReviewRepo(t)
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	state, _ := filepath.EvalSymlinks(t.TempDir())
	os.Chmod(state, 0o700)
	code, res, _ := r.runJSON("--report-dir", dir, "--state-dir", state)
	if code != 1 || res.Plan != nil || res.Status == "ok" {
		t.Fatalf("code=%d status=%q", code, res.Status)
	}
	if entries, _ := os.ReadDir(filepath.Join(state, "tidy-v3-plans")); len(entries) != 0 {
		t.Errorf("a plan was stored: %v", entries)
	}
}

func TestTidyV3DecisionsThroughTheCLIValidateAgainstTheSchemas(t *testing.T) {
	r := newReviewRepo(t)
	s := schemaAt(t, "v3", "tidy")
	ds := schemaAt(t, "v3", "tidy-decisions")
	tip, base := r.git("-C", r.source, "rev-parse", "fresh"), r.git("-C", r.source, "rev-parse", "main")
	file := filepath.Join(t.TempDir(), "decisions.json")
	body := `{"schema_version":1,"decisions":[{"branch":"refs/heads/fresh","tip_oid":"` + tip + `","base_oid":"` + base + `","decision":"integrated","reason":"covered by main","reviewer":"claude","kind":"ai"},` +
		`{"branch":"refs/heads/second","tip_oid":"` + r.git("-C", r.source, "rev-parse", "second") + `","base_oid":"` + base + `","decision":"keep","reason":"","reviewer":"me","kind":"human"}]}`
	os.WriteFile(file, []byte(body), 0o600)
	valid(t, ds, []byte(body))
	for name, mutate := range map[string]func(map[string]any){
		"unknown field":        func(v map[string]any) { v["decisions"].([]any)[0].(map[string]any)["note"] = "x" },
		"invalid decision":     func(v map[string]any) { v["decisions"].([]any)[0].(map[string]any)["decision"] = "approve" },
		"integrated no reason": func(v map[string]any) { v["decisions"].([]any)[0].(map[string]any)["reason"] = "" },
		"schema version":       func(v map[string]any) { v["schema_version"] = 3 },
		"needs-human in 1":     func(v map[string]any) { v["decisions"].([]any)[0].(map[string]any)["decision"] = "needs-human" },
		"repo in 1":            func(v map[string]any) { v["decisions"].([]any)[0].(map[string]any)["repo"] = "sourcesteward" },
		"kind":                 func(v map[string]any) { v["decisions"].([]any)[0].(map[string]any)["kind"] = "bot" },
		"short oid":            func(v map[string]any) { v["decisions"].([]any)[0].(map[string]any)["tip_oid"] = "abc" },
	} {
		t.Run("decision schema "+name, func(t *testing.T) {
			v := value(t, []byte(body)).(map[string]any)
			mutate(v)
			if ds.Validate(v) == nil {
				t.Fatal("the decision schema admitted a violation")
			}
		})
	}
	// Schema 2 adds needs-human and the repo label; schema 1 is unchanged.
	body2 := strings.Replace(body, `"schema_version":1`, `"schema_version":2`, 1)
	body2 = strings.Replace(body2, `"decision":"keep","reason":""`, `"decision":"needs-human","repo":"sourcesteward","reason":"cannot judge"`, 1)
	valid(t, ds, []byte(body2))
	for name, mutate := range map[string]func(map[string]any){
		"needs-human no reason": func(v map[string]any) { v["decisions"].([]any)[1].(map[string]any)["reason"] = "" },
		"empty repo":            func(v map[string]any) { v["decisions"].([]any)[1].(map[string]any)["repo"] = "" },
		"long repo":             func(v map[string]any) { v["decisions"].([]any)[1].(map[string]any)["repo"] = strings.Repeat("r", 129) },
		"unknown field":         func(v map[string]any) { v["decisions"].([]any)[1].(map[string]any)["note"] = "x" },
	} {
		t.Run("decision schema 2 "+name, func(t *testing.T) {
			v := value(t, []byte(body2)).(map[string]any)
			mutate(v)
			if ds.Validate(v) == nil {
				t.Fatal("the decision schema admitted a violation")
			}
		})
	}
	code, res, raw := r.runJSON("--decisions", file)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	valid(t, s, raw)
	byName := map[string]workspace.V3Candidate{}
	for _, c := range res.Plan.Candidates {
		byName[c.Name] = c
	}
	if c := byName["refs/heads/fresh"]; c.Decision != "delete" || c.Proof != "reviewed-integrated" || c.Evidence.Integration.Decision.Kind != "ai" {
		t.Errorf("fresh: %+v", c)
	}
	if c := byName["refs/heads/second"]; c.Decision != "retain" || c.Reason != "reviewed-keep" {
		t.Errorf("second: %+v", c)
	}
	if res.Plan.Review == nil || len(res.Plan.Review.DecisionsDigest) != 64 || res.ReviewSummary.Proven != 1 {
		t.Errorf("review %+v summary %+v", res.Plan.Review, res.ReviewSummary)
	}
	// A forged reviewed class is refused by the closed schema.
	v := value(t, raw).(map[string]any)
	cands := v["plan"].(map[string]any)["candidates"].([]any)
	integrationOf(cands, "fresh")["strength"] = "ancestry"
	if s.Validate(v) == nil {
		t.Error("a reviewed class with another strength was admitted")
	}
	// Human output shows who decided what.
	_, human, _ := r.run("--decisions", file)
	if !strings.Contains(human, `Decision: branch="refs/heads/fresh" decision="integrated" kind="ai"`) {
		t.Errorf("decision missing from the human output:\n%s", human)
	}
}
