// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func findGate(r PlanResult, code string) string {
	for _, g := range r.Gates {
		if g.Code == code {
			return g.Outcome
		}
	}
	return "absent"
}
func TestTagPlanOfflineGates(t *testing.T) {
	for _, annotated := range []bool{false, true} {
		d, c := planRepo(t)
		if annotated {
			git(t, d, "tag", "-a", "v1.2.3", "-m", "release")
		} else {
			git(t, d, "tag", "v1.2.3")
		}
		r := Plan(context.Background(), PlanOptions{Path: d, Subject: "v1.2.3"})
		want := "fail"
		if annotated {
			want = "pass"
		}
		if r.Status != "ok" || findGate(r, "tag-not-annotated") != want || findGate(r, "record-missing") != "fail" || r.Subject.TagCommit == nil || *r.Subject.TagCommit != c {
			t.Fatalf("%+v", r)
		}
	}
}
func TestTagBuildMode(t *testing.T) {
	d, _ := planRepo(t)
	git(t, d, "tag", "-a", "v1.2.3", "-m", "release")
	r := Plan(context.Background(), PlanOptions{Path: d, Subject: "v1.2.3", Mode: "build"})
	if r.Status != "ok" || findGate(r, "tag-not-annotated") != "pass" || findGate(r, "version-invalid") != "pass" || findGate(r, "record-missing") != "absent" {
		t.Fatalf("%+v", r)
	}
}
func TestPublishCanonicalRecordAndDiff(t *testing.T) {
	d, c := planRepo(t)
	raw, err := osReadExampleRecord("skill-pack")
	if err != nil {
		t.Fatal(err)
	}
	doc, ff := ParseReleaseDocument(raw)
	if len(ff) > 0 {
		t.Fatal(ff)
	}
	rec := doc.(*ReleaseRecord)
	rec.Commit = c
	rec.Repository = "example/skills"
	rec.Attestation.SignerWorkflow = "example/skills/.github/workflows/release-build.yml"
	raw, err = EncodeReleaseDocument(rec)
	if err != nil {
		t.Fatal(err)
	}
	write(t, d, "docs/releases/"+rec.Version+".json", raw)
	end := commitAll(t, d)
	git(t, d, "update-ref", "refs/remotes/upstream/main", end)
	git(t, d, "tag", "-a", rec.Tag, "-m", "release")
	r := Plan(context.Background(), PlanOptions{Path: d, Subject: rec.Tag})
	if r.Status != "ok" || findGate(r, "build-commit-not-ancestor") != "pass" || findGate(r, "diff-not-allowlisted") != "pass" {
		t.Fatalf("%+v", r)
	}
}

func osReadExampleRecord(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join("..", "..", "specs", "026-release-process", "examples", name+".record.json"))
}

func TestTagGrammarMismatchFails(t *testing.T) {
	d, _ := planRepo(t)
	git(t, d, "tag", "-a", "release-invalid", "-m", "release")
	r := Plan(context.Background(), PlanOptions{Path: d, Subject: "release-invalid"})
	if findGate(r, "tag-version-mismatch") != "fail" {
		t.Fatalf("%+v", r.Gates)
	}
}

func TestTagPlanChecksContractAtTagCommit(t *testing.T) {
	d, _ := planRepo(t)
	write(t, d, ".stw/release.toml", []byte("schema_version = 2\n"))
	bad := commitAll(t, d)
	git(t, d, "update-ref", "refs/remotes/upstream/main", bad)
	git(t, d, "tag", "-a", "v1.2.3", "-m", "invalid contract")
	p := Plan(context.Background(), PlanOptions{Path: d, Subject: "v1.2.3"})
	if p.Status != "ok" || findGate(p, "config-invalid") != "fail" || p.Verdict != "fail" {
		t.Fatalf("%+v", p)
	}
}
