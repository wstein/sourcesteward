// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"fmt"
	"os"
	"testing"
)

func recordFixture(t *testing.T, name string) (ReleaseRecord, RecordContext) {
	t.Helper()
	raw, err := os.ReadFile("../../specs/026-release-process/examples/" + name + ".record.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, f := ParseReleaseDocument(raw)
	if len(f) > 0 {
		t.Fatal(f)
	}
	r := *doc.(*ReleaseRecord)
	raw, err = os.ReadFile("../../specs/026-release-process/examples/" + name + ".release.toml")
	if err != nil {
		t.Fatal(err)
	}
	m, f := ParseTOML(raw)
	if len(f) > 0 {
		t.Fatal(f)
	}
	c := modelFrom(m)
	c.applyDefaults()
	checksums := map[string]string{}
	for _, a := range r.Assets {
		if a.File != nil {
			checksums[a.File.Name] = a.File.SHA256
		}
	}
	return r, RecordContext{Contract: c, Repository: r.Repository, Checksums: checksums}
}
func TestValidateRecordRules(t *testing.T) {
	cases := []struct {
		name, fixture string
		change        func(*ReleaseRecord, *RecordContext)
	}{
		{"version", "go-cli", func(r *ReleaseRecord, c *RecordContext) { r.Version = "v01.2.3" }},
		{"tag", "go-cli", func(r *ReleaseRecord, c *RecordContext) { r.Tag = "v9.0.0" }},
		{"commit", "go-cli", func(r *ReleaseRecord, c *RecordContext) { r.Commit = "abcdef" }},
		{"checksum missing", "go-cli", func(r *ReleaseRecord, c *RecordContext) { delete(c.Checksums, r.Assets[0].File.Name) }},
		{"checksum wrong", "go-cli", func(r *ReleaseRecord, c *RecordContext) { c.Checksums[r.Assets[0].File.Name] = r.Assets[1].File.SHA256 }},
		{"image tags", "oci-bundle", func(r *ReleaseRecord, c *RecordContext) { r.Assets[0].Image.Tags = []string{"latest"} }},
		{"extra image tag", "oci-bundle", func(r *ReleaseRecord, c *RecordContext) {
			r.Assets[0].Image.Tags = append(r.Assets[0].Image.Tags, "latest")
		}},
		{"asset limit", "go-cli", func(r *ReleaseRecord, c *RecordContext) {
			a := r.Assets[0]
			for len(r.Assets) < 65 {
				r.Assets = append(r.Assets, a)
			}
		}},
		{"platform limit", "oci-bundle", func(r *ReleaseRecord, c *RecordContext) {
			a := r.Assets[0].Image.Platforms[0]
			for len(r.Assets[0].Image.Platforms) < 33 {
				r.Assets[0].Image.Platforms = append(r.Assets[0].Image.Platforms, a)
			}
		}},
		{"size limit", "go-cli", func(r *ReleaseRecord, c *RecordContext) { r.Assets[0].File.Size = 2147483649 }},
		{"repository forged", "go-cli", func(r *ReleaseRecord, c *RecordContext) { r.Repository = "evil/project" }},
		{"signer forged", "go-cli", func(r *ReleaseRecord, c *RecordContext) {
			r.Attestation.SignerWorkflow = "evil/project/.github/workflows/build.yml"
		}},
		{"url forged", "go-cli", func(r *ReleaseRecord, c *RecordContext) { r.Assets[0].File.URLTemplate = "https://evil.example/{name}" }},
		{"project forged", "go-cli", func(r *ReleaseRecord, c *RecordContext) { r.Project = "Forged" }},
		{"product forged", "go-cli", func(r *ReleaseRecord, c *RecordContext) { r.Product = "forged" }},
		{"product omitted", "oci-bundle", func(r *ReleaseRecord, c *RecordContext) { r.Product = "" }},
		{"missing context", "go-cli", func(r *ReleaseRecord, c *RecordContext) { c.Contract = nil }},
	}
	for _, name := range []string{"go-cli", "oci-bundle", "polyglot", "skill-pack", "mirror-zlib", "mirror-busybox"} {
		t.Run("valid/"+name, func(t *testing.T) {
			r, c := recordFixture(t, name)
			if f := ValidateRecord(r, c); len(f) > 0 {
				t.Fatal(f)
			}
		})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, c := recordFixture(t, tc.fixture)
			tc.change(&r, &c)
			f := ValidateRecord(r, c)
			if len(f) == 0 {
				t.Fatal("mutant accepted")
			}
			if tc.name == "version" && (len(f) != 1 || f[0].Pointer != "/version") {
				t.Fatalf("expected isolated version finding, got %+v", f)
			}
			for _, x := range f {
				if x.Code != "record-invalid" {
					t.Errorf("wrong code: %+v", x)
				}
			}
		})
	}
	t.Run("explicit bootstrap signer", func(t *testing.T) {
		r, c := recordFixture(t, "go-cli")
		c.SignerWorkflowFile = ".github/workflows/bootstrap.yml"
		r.Attestation.SignerWorkflow = c.Repository + "/" + c.SignerWorkflowFile
		if f := ValidateRecord(r, c); len(f) > 0 {
			t.Fatal(f)
		}
	})
}

// Keep all checksum evidence valid so only the name uniqueness rule can reject
// this record. A checksum mismatch must never mask a disabled duplicate guard.
func TestValidateRecordDuplicateNames(t *testing.T) {
	r, c := recordFixture(t, "go-cli")
	first := *r.Assets[0].File
	r.Assets[1].File = &first
	findings := ValidateRecord(r, c)
	if len(findings) != 1 {
		t.Fatalf("expected only duplicate-name finding, got %+v", findings)
	}
	f := findings[0]
	if f.Code != "record-invalid" || f.Pointer != "/assets/1/name" || f.Message != "file asset names must be unique" {
		t.Fatalf("wrong duplicate-name finding: %+v", f)
	}
}

func TestValidateRecordPlaceholderClosure(t *testing.T) {
	for _, tc := range []struct{ name, template, message string }{
		{"unknown", "https://example/{unknown}", "the placeholder {unknown} is unknown or not allowed in this field"},
		{"unclosed", "https://example/{name", "an opening brace without a closing brace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, c := recordFixture(t, "go-cli")
			c.Contract.Outputs.URLTemplate = tc.template
			// Keep the independent URL equality rule satisfied for every asset.
			for _, asset := range r.Assets {
				asset.File.URLTemplate = tc.template
			}
			findings := ValidateRecord(r, c)
			if len(findings) != len(r.Assets) {
				t.Fatalf("expected one closure finding per asset, got %+v", findings)
			}
			for i, f := range findings {
				wantPointer := fmt.Sprintf("/assets/%d/url_template", i)
				if f.Code != "record-invalid" || f.Pointer != wantPointer || f.Message != tc.message {
					t.Fatalf("wrong closure finding: %+v", f)
				}
			}
		})
	}
}
