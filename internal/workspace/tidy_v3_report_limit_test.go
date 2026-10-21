// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// Reviewer format: a limit hit names the bound (name, unit, limit, observed) and
// the entry still says why the diff is missing.

func limitOfErr(t *testing.T, err error) *integrationLimitError {
	t.Helper()
	var le *integrationLimitError
	if !errors.Is(err, errIntegrationLimit) || !errors.As(err, &le) {
		t.Fatalf("not a typed limit: %v", err)
	}
	return le
}

func TestLimitErrorsNameTheBoundThatWasHit(t *testing.T) {
	hex := strings.Repeat("a", 40)
	stream := func(n int) io.Reader { return strings.NewReader(strings.Repeat(hex+"\n", n)) }
	err := readIntegrationRecords(stream(specMergeBase.maxRecords+1), specMergeBase, 40, func([]byte) error { return nil })
	le := limitOfErr(t, err)
	if le.Name != "merge-base --all" || le.Unit != "records" || le.Limit != int64(specMergeBase.maxRecords) || le.Observed != int64(specMergeBase.maxRecords+1) {
		t.Errorf("records bound: %+v", le)
	}
	err = readIntegrationRecords(strings.NewReader("zz\n"), specMergeBase, 40, func([]byte) error { return nil })
	if le = limitOfErr(t, err); le.Name != "merge-base --all" || le.Unit != "grammar" {
		t.Errorf("grammar: %+v", le)
	}
	_, err = parseReportRaw(strings.NewReader(strings.Repeat(":100644 100755 "+hex+" "+hex+" M\x00p\x00", reportRawRecords+1)), 40)
	if le = limitOfErr(t, err); le.Name != "diff-tree --raw" || le.Unit != "records" || le.Limit != reportRawRecords || le.Observed != reportRawRecords+1 {
		t.Errorf("raw records: %+v", le)
	}
	if got := le.Error(); got != errIntegrationLimit.Error() {
		t.Errorf("message changed: %q", got)
	}
}

func TestReportProofLimitNamesTheBoundAndExplainsTheMissingDiff(t *testing.T) {
	f := newProofFix(t)
	m0 := f.rev("HEAD")
	f.branchFrom("real", m0, map[string]string{"new.txt": "hello\n"}, "add")
	f.commit("main moves", map[string]string{"other.txt": "o\n"})
	bin := wrapGit(t, "exit 3")
	e, v := f.reportEntry(f.main, "real", true, func(r *integrationRun) { r.bin = bin })
	if v.Limit == nil || !strings.HasPrefix(v.Limit.Name, "merge-tree") {
		t.Errorf("the proof phase did not keep the bound: %+v", v.Limit)
	}
	if !containsAll(e.Reasons, "proof-limit") {
		t.Fatalf("reasons %v", e.Reasons)
	}
	l := e.ProofLimit
	if l == nil || l.Name != "merge-tree" || l.Unit != "grammar" {
		t.Fatalf("proof_limit %+v", l)
	}
	if e.Diff.Source != "unavailable" || !strings.Contains(e.Diff.UnavailableReason, "proof-limit") || !strings.Contains(e.Diff.UnavailableReason, "merge-tree") {
		t.Errorf("diff %+v", e.Diff)
	}
	// What the report could still collect stays: the branch's commits.
	if len(e.Commits) != 1 || e.Commits[0].Subject.Text != "add" {
		t.Errorf("commits %+v", e.Commits)
	}
	if e.Verdict.Verdict != "unclear" {
		t.Errorf("a limit hit keeps the branch: %+v", e.Verdict)
	}
}

func TestReportUnavailableDiffWithoutALimitStillGivesAReason(t *testing.T) {
	f := newProofFix(t)
	m0 := f.rev("HEAD")
	f.branchFrom("real", m0, map[string]string{"new.txt": "hello\n"}, "add")
	f.commit("main moves", map[string]string{"other.txt": "o\n"})
	e, _ := f.reportEntry(f.main, "real", true, func(r *integrationRun) {
		r.engine.makeObjectDir = func() (string, error) { return "", errors.New("no space") }
	})
	if e.Diff.Source != "unavailable" || !strings.Contains(e.Diff.UnavailableReason, "object-dir-unavailable") || e.ProofLimit != nil {
		t.Fatalf("entry %+v", e)
	}
}

func TestReportMarkdownNamesTheBoundAndTheMissingDiffReason(t *testing.T) {
	e := syntheticEntry("a", 10)
	e.Diff = emptyReportDiff("unavailable")
	e.Diff.UnavailableReason = "proof-limit: diff-tree --raw hit its records bound (limit 8192, observed 8193); no diff was collected"
	e.ProofLimit = &V3ReportLimit{Name: "diff-tree --raw", Unit: "records", Limit: 8192, Observed: 8193}
	r, err := renderReport(syntheticDocument(e))
	if err != nil {
		t.Fatal(err)
	}
	md := string(r.Markdown)
	for _, want := range []string{"Proof limit: `diff-tree --raw` records, limit 8192, observed 8193", "Not available: proof-limit: diff-tree --raw hit its records bound"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q\n%s", want, md)
		}
	}
	if !strings.Contains(string(r.JSON), `"proof_limit":{"name":"diff-tree --raw","unit":"records","limit":8192,"observed":8193}`) || !strings.Contains(string(r.JSON), `"unavailable_reason":"proof-limit:`) {
		t.Errorf("json lacks the bound\n%s", r.JSON)
	}
}
