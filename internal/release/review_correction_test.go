// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rewriteRecordManifest(t *testing.T, dir string, m *BuildManifest) {
	t.Helper()
	raw, err := EncodeReleaseDocument(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build-manifest.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	sums := ""
	for _, a := range m.Assets {
		if a.File != nil {
			sums += a.File.SHA256 + "  " + a.File.Name + "\n"
		}
	}
	sums += hashBytes(raw) + "  build-manifest.json\n"
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums), 0644); err != nil {
		t.Fatal(err)
	}
}

func recordImageFixture(t *testing.T) (string, string, string, *BuildManifest) {
	t.Helper()
	config := exampleBytes(t, "oci-bundle")
	d := repoWith(t, map[string][]byte{".stw/release.toolbox.toml": config, "bundles/toolbox/tools.lock.json": []byte("lock"), "bundles/toolbox/tools.json": []byte("tools")})
	c := commitAll(t, d)
	raw, err := osReadExampleRecord("oci-bundle")
	if err != nil {
		t.Fatal(err)
	}
	doc, ff := ParseReleaseDocument(raw)
	if len(ff) > 0 {
		t.Fatal(ff)
	}
	s := doc.(*ReleaseRecord)
	m := &BuildManifest{SchemaVersion: 1, Kind: "build-manifest", Project: s.Project, Product: s.Product, Version: s.Version, Commit: c, SourceDateEpoch: s.SourceDateEpoch, ConfigSHA256: hashBytes(config), STWVersion: s.Build.STWVersion, Repository: s.Repository, Run: RecordRun{s.Build.Workflow, s.Build.RunID, s.Build.RunAttempt}, Builds: []ManifestBuild{{ID: "toolbox-image", Network: "none", NetworkHosts: []string{}, LockFiles: []PathSHA{}}}, Assets: s.Assets}
	dir := t.TempDir()
	rewriteRecordManifest(t, dir, m)
	return d, c, dir, m
}

func TestReviewCorrectionUnknownFileTarget(t *testing.T) {
	d, c, dir := recordCommandFixture(t)
	raw, err := os.ReadFile(filepath.Join(dir, "build-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, ff := ParseReleaseDocument(raw)
	if len(ff) > 0 {
		t.Fatal(ff)
	}
	m := doc.(*BuildManifest)
	m.Assets[0].File.Target = "undeclared"
	rewriteRecordManifest(t, dir, m)
	r := Record(context.Background(), RecordOptions{Path: d, Commit: c, Version: m.Version, From: dir})
	if r.Status != "failed" || r.Error == nil || r.Error.Code != "record-invalid" {
		t.Fatalf("expected undeclared file target refusal: %+v", r)
	}
}
func TestReviewCorrectionUnknownImageTarget(t *testing.T) {
	d, c, dir, m := recordImageFixture(t)
	a := m.Assets[0].Image
	a.Target = "undeclared"
	a.Repository = "ghcr.io/attacker/redirect"
	a.Lock = nil
	a.ToolList = nil
	rewriteRecordManifest(t, dir, m)
	r := Record(context.Background(), RecordOptions{Path: d, Commit: c, Version: m.Version, From: dir, Product: m.Product})
	if r.Status != "failed" || r.Error == nil || r.Error.Code != "record-invalid" {
		t.Fatalf("expected undeclared image target refusal: %+v", r)
	}
}
func TestReviewCorrectionEditedImageRecordRepository(t *testing.T) {
	d, c, dir, m := recordImageFixture(t)
	r := Record(context.Background(), RecordOptions{Path: d, Commit: c, Version: m.Version, From: dir, Product: m.Product})
	if r.Status != "ok" {
		t.Fatal(r.Error)
	}
	r.Record.Assets[0].Image.Repository = "ghcr.io/attacker/redirect"
	raw, err := EncodeReleaseDocument(r.Record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, d, r.Path, raw)
	end := commitAll(t, d)
	git(t, d, "remote", "add", "origin", "https://github.com/"+m.Repository+".git")
	git(t, d, "update-ref", "refs/remotes/origin/main", end)
	git(t, d, "tag", "-a", r.Record.Tag, "-m", "release")
	p := Verify(context.Background(), VerifyOptions{PlanOptions: PlanOptions{Path: d, Subject: r.Record.Tag}, Artifacts: dir})
	if findGate(p.PlanResult, "record-invalid") != "fail" {
		t.Fatalf("edited image repository accepted: status=%s verdict=%s record-invalid=%s gates=%+v", p.Status, p.Verdict, findGate(p.PlanResult, "record-invalid"), p.Gates)
	}
}

func TestRecordRejectsKnownTargetWithWrongKindBeforeAssembly(t *testing.T) {
	d, c, dir := recordCommandFixture(t)
	raw, e := os.ReadFile(filepath.Join(dir, "build-manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	doc, ff := ParseReleaseDocument(raw)
	if len(ff) > 0 {
		t.Fatal(ff)
	}
	m := doc.(*BuildManifest)
	image, _ := recordFixture(t, "oci-bundle")
	replacement := *image.Assets[0].Image
	replacement.Target = m.Assets[0].File.Target
	m.Assets[0] = RecordAsset{Image: &replacement}
	rewriteRecordManifest(t, dir, m)
	r := Record(context.Background(), RecordOptions{Path: d, Commit: c, Version: "v0.2.0", From: dir})
	if r.Error == nil || r.Error.Code != "record-invalid" || !strings.Contains(r.Error.Message, "same kind") {
		t.Fatalf("%+v", r)
	}
}
