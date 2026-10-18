// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func recordCommandFixture(t *testing.T) (string, string, string) {
	t.Helper()
	config := exampleBytes(t, "go-cli")
	d := repoWith(t, map[string][]byte{".stw/release.toml": config, "docs/guide.md": []byte("original guide\n")})
	c := commitAll(t, d)
	dir := t.TempDir()
	raw, e := os.ReadFile(filepath.Join("..", "..", "specs", "026-release-process", "examples", "go-cli.manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	doc, ff := ParseReleaseDocument(raw)
	if len(ff) > 0 {
		t.Fatal(ff)
	}
	m := doc.(*BuildManifest)
	m.Commit = c
	m.ConfigSHA256 = fmt.Sprintf("%x", sha256.Sum256(config))
	for i := range m.Assets {
		a := m.Assets[i].File
		data := []byte(a.Name)
		a.Size = int64(len(data))
		a.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
		os.WriteFile(filepath.Join(dir, a.Name), data, 0644)
	}
	raw, e = EncodeReleaseDocument(m)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "build-manifest.json"), raw, 0644)
	sums := ""
	for _, a := range m.Assets {
		sums += a.File.SHA256 + "  " + a.File.Name + "\n"
	}
	sums += fmt.Sprintf("%x  build-manifest.json\n", sha256.Sum256(raw))
	os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums), 0644)
	return d, c, dir
}
func TestRecordDeterministicAndInputAgreement(t *testing.T) {
	d, c, dir := recordCommandFixture(t)
	o := RecordOptions{Path: d, Commit: c, Version: "v0.2.0", From: dir}
	one := Record(context.Background(), o)
	two := Record(context.Background(), o)
	if one.Status != "ok" || two.Status != "ok" || !bytes.Equal(one.Canonical, two.Canonical) {
		t.Fatalf("%+v %+v", one, two)
	}
	o.Repo = "forged/repo"
	bad := Record(context.Background(), o)
	if bad.Error == nil || bad.Error.Code != "artifact-mismatch" {
		t.Fatalf("%+v", bad)
	}
}
func TestRecordCheckAndWrite(t *testing.T) {
	d, c, dir := recordCommandFixture(t)
	o := RecordOptions{Path: d, Commit: c, Version: "v0.2.0", From: dir, Write: true}
	r := Record(context.Background(), o)
	if r.Status != "ok" || !r.Written {
		t.Fatalf("%+v", r)
	}
	r = Record(context.Background(), o)
	if r.Status != "ok" || r.Written {
		t.Fatalf("%+v", r)
	}
	o.Write = false
	o.Check = filepath.Join(d, r.Path)
	r = Record(context.Background(), o)
	if r.Status != "ok" {
		t.Fatalf("%+v", r)
	}
	os.WriteFile(o.Check, []byte("{}"), 0644)
	r = Record(context.Background(), o)
	if r.Error == nil || r.Error.Code != "record-invalid" {
		t.Fatalf("%+v", r)
	}
}

func TestRecordWriteHasOnlyOneNewFileEffect(t *testing.T) {
	d, c, dir := recordCommandFixture(t)
	if e := os.MkdirAll(filepath.Join(d, "docs/releases"), 0755); e != nil {
		t.Fatal(e)
	}
	makeIndexStale(t, d)
	markers := t.TempDir()
	plantSentinels(t, d, markers)
	before := snapshot(t, d)
	r := Record(context.Background(), RecordOptions{Path: d, Commit: c, Version: "v0.2.0", From: dir, Write: true})
	if r.Status != "ok" {
		t.Fatal(r.Error)
	}
	after := snapshot(t, d)
	for path, old := range before {
		now, ok := after[path]
		if !ok {
			t.Fatalf("removed %s", path)
		}
		if old.mode.IsDir() {
			continue
		}
		if old != now {
			t.Fatalf("changed existing file %s", path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok && path != r.Path {
			t.Fatalf("unexpected new entry %s", path)
		}
	}
	if fired, _ := filepath.Glob(filepath.Join(markers, "fired-*")); len(fired) > 0 {
		t.Fatal(fired)
	}
}

func TestRecordNamedImageUsesCommittedLockHashes(t *testing.T) {
	config := exampleBytes(t, "oci-bundle")
	lock := []byte("committed lock\x00\xff")
	tools := []byte("committed tools\n")
	d := repoWith(t, map[string][]byte{".stw/release.toolbox.toml": config, "bundles/toolbox/tools.lock.json": lock, "bundles/toolbox/tools.json": tools})
	c := commitAll(t, d)
	raw, e := osReadExampleRecord("oci-bundle")
	if e != nil {
		t.Fatal(e)
	}
	doc, ff := ParseReleaseDocument(raw)
	if len(ff) > 0 {
		t.Fatal(ff)
	}
	sample := doc.(*ReleaseRecord)
	manifest := BuildManifest{SchemaVersion: 1, Kind: "build-manifest", Project: sample.Project, Product: sample.Product, Version: sample.Version, Commit: c, SourceDateEpoch: sample.SourceDateEpoch, ConfigSHA256: hashBytes(config), STWVersion: sample.Build.STWVersion, Repository: sample.Repository, Run: RecordRun{sample.Build.Workflow, sample.Build.RunID, sample.Build.RunAttempt}, Builds: []ManifestBuild{{ID: "toolbox-image", Network: "none", NetworkHosts: []string{}, LockFiles: []PathSHA{}}}, Assets: sample.Assets}
	raw, e = EncodeReleaseDocument(manifest)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "build-manifest.json"), raw, 0644)
	os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(hashBytes(raw)+"  build-manifest.json\n"), 0644)
	write(t, d, "bundles/toolbox/tools.lock.json", []byte("working tree drift"))
	r := Record(context.Background(), RecordOptions{Path: d, Commit: c, Version: sample.Version, From: dir, Product: "toolbox", Write: true})
	if r.Status != "ok" {
		t.Fatalf("%+v", r.Error)
	}
	if r.Path != "docs/releases/toolbox/"+sample.Version+".json" {
		t.Fatal(r.Path)
	}
	image := r.Record.Assets[0].Image
	if image.Lock == nil || image.Lock.SHA256 != hashBytes(lock) || image.ToolList == nil || image.ToolList.SHA256 != hashBytes(tools) {
		t.Fatalf("%+v", image)
	}
}
