// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRecordCanonicalExamples(t *testing.T) {
	for _, name := range []string{"go-cli.record", "oci-bundle.record", "polyglot.record", "skill-pack.record", "mirror-zlib.record", "mirror-busybox.record", "go-cli.manifest", "mirror-zlib.manifest", "oci-bundle.result"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("../../specs/026-release-process/examples/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			doc, findings := ParseReleaseDocument(raw)
			if len(findings) > 0 {
				t.Fatal(findings)
			}
			got, err := EncodeReleaseDocument(doc)
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatalf("round trip: %v\n%s", err, got)
			}
			for _, mutant := range [][]byte{bytes.TrimSuffix(raw, []byte("\n")), bytes.Replace(raw, []byte("  \""), []byte(" \""), 1), bytes.Replace(raw, []byte("\"kind\""), []byte("\"\\u006bind\""), 1), append([]byte(" \n"), raw...)} {
				_, f := ParseReleaseDocument(mutant)
				if len(f) == 0 || f[0].Code != "record-not-canonical" {
					t.Errorf("noncanonical accepted: %+v", f)
				}
			}
		})
	}
}
func TestCanonicalEscapesAndIntegerOnly(t *testing.T) {
	got, err := canonicalValue(map[string]any{"é": "<&>\b\f\n\r\t\x00\\\""})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"é\": \"<&>\\u0008\\u000c\\n\\r\\t\\u0000\\\\\\\"\"\n}\n"
	if string(got) != want {
		t.Fatalf("%q != %q", got, want)
	}
	if _, err := canonicalValue(1.0); err == nil {
		t.Fatal("float accepted")
	}
	raw, _ := os.ReadFile("../../specs/026-release-process/examples/go-cli.record.json")
	for _, mutant := range []string{strings.Replace(string(raw), "1000000", "1e6", 1), strings.Replace(string(raw), "1000000", "1000000.0", 1), strings.Replace(string(raw), "\"assets\":", "\"unknown\": true,\n  \"assets\":", 1)} {
		if _, f := ParseReleaseDocument([]byte(mutant)); len(f) == 0 {
			t.Fatal("invalid document accepted")
		}
	}
}

func TestCanonicalOrderAndNoCallerMutation(t *testing.T) {
	raw, err := os.ReadFile("../../specs/026-release-process/examples/go-cli.record.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, f := ParseReleaseDocument(raw)
	if len(f) > 0 {
		t.Fatal(f)
	}
	r := doc.(*ReleaseRecord)
	r.Assets[0], r.Assets[1] = r.Assets[1], r.Assets[0]
	first := r.Assets[0].File.Name
	got, err := EncodeReleaseDocument(r)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("asset sorting: %v", err)
	}
	if r.Assets[0].File.Name != first {
		t.Fatal("encoder mutated caller assets")
	}
	reordered := bytes.Replace(raw, []byte("  \"tag\": \"v0.2.0\",\n  \"version\": \"v0.2.0\""), []byte("  \"version\": \"v0.2.0\",\n  \"tag\": \"v0.2.0\""), 1)
	if _, f := ParseReleaseDocument(reordered); len(f) != 1 || f[0].Code != "record-not-canonical" {
		t.Fatalf("key order: %+v", f)
	}
	// Reverse complete asset objects while preserving every other canonical byte.
	node, _ := recordNode(reflect.ValueOf(r))
	var out bytes.Buffer
	if err := writeCanonical(&out, node, 0); err != nil {
		t.Fatal(err)
	}
	out.WriteByte('\n')
	if _, f := ParseReleaseDocument(out.Bytes()); len(f) != 1 || f[0].Code != "record-not-canonical" {
		t.Fatalf("asset order: %+v", f)
	}
	raw, err = os.ReadFile("../../specs/026-release-process/examples/oci-bundle.result.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, f = ParseReleaseDocument(raw)
	if len(f) > 0 {
		t.Fatal(f)
	}
	o := doc.(*OCIResult)
	o.Platforms[0], o.Platforms[1] = o.Platforms[1], o.Platforms[0]
	node, _ = recordNode(reflect.ValueOf(o))
	out.Reset()
	if err := writeCanonical(&out, node, 0); err != nil {
		t.Fatal(err)
	}
	out.WriteByte('\n')
	if _, f := ParseReleaseDocument(out.Bytes()); len(f) != 1 || f[0].Code != "record-not-canonical" {
		t.Fatalf("platform order: %+v", f)
	}
}
func TestReleaseDocumentLimitsAndMalformedJSON(t *testing.T) {
	for _, raw := range [][]byte{[]byte("{} {}"), []byte("{\"kind\":\"release-record\",\"kind\":\"release-record\"}"), []byte("\xef\xbb\xbf{}"), {0xff}} {
		if _, f := ParseReleaseDocument(raw); len(f) != 1 || f[0].Code != "record-invalid" {
			t.Fatalf("malformed input: %+v", f)
		}
	}
	if _, f := ParseReleaseDocument(bytes.Repeat([]byte(" "), MaxReleaseDocumentBytes+1)); len(f) != 1 || f[0].Code != "release-limit" {
		t.Fatalf("size limit: %+v", f)
	}
}

func TestCanonicalImageTagOrderAndNoCallerMutation(t *testing.T) {
	r, _ := recordFixture(t, "oci-bundle")
	// The schema permits several tags; semantic validation separately restricts
	// release records to [version]. Exercise canonical array ordering on a manifest.
	manifest := BuildManifest{
		SchemaVersion: 1, Kind: "build-manifest", Project: r.Project, Product: r.Product,
		Version: r.Version, Commit: r.Commit, SourceDateEpoch: r.SourceDateEpoch,
		ConfigSHA256: strings.Repeat("a", 64), STWVersion: r.Build.STWVersion,
		Repository: r.Repository, Run: RecordRun{Workflow: r.Build.Workflow, RunID: r.Build.RunID, RunAttempt: r.Build.RunAttempt},
		Assets: r.Assets,
	}
	manifest.Assets[0].Image.Tags = []string{"z-tag", "a-tag"}
	canonical, err := EncodeReleaseDocument(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Assets[0].Image.Tags[0] != "z-tag" || manifest.Assets[0].Image.Tags[1] != "a-tag" {
		t.Fatal("encoder mutated caller tags")
	}
	doc, f := ParseReleaseDocument(canonical)
	if len(f) > 0 {
		t.Fatal(f)
	}
	tags := doc.(*BuildManifest).Assets[0].Image.Tags
	if len(tags) != 2 || tags[0] != "a-tag" || tags[1] != "z-tag" {
		t.Fatalf("tags not sorted: %v", tags)
	}
	node, err := recordNode(reflect.ValueOf(manifest))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeCanonical(&out, node, 0); err != nil {
		t.Fatal(err)
	}
	out.WriteByte('\n')
	if _, f := ParseReleaseDocument(out.Bytes()); len(f) != 1 || f[0].Code != "record-not-canonical" {
		t.Fatalf("tag order accepted: %+v", f)
	}
}

func TestCanonicalPlatformVariantOrderAndNoCallerMutation(t *testing.T) {
	raw, err := os.ReadFile("../../specs/026-release-process/examples/oci-bundle.result.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, f := ParseReleaseDocument(raw)
	if len(f) > 0 {
		t.Fatal(f)
	}
	result := doc.(*OCIResult)
	first := result.Platforms[0]
	first.Variant = "v9"
	second := first
	second.Variant = "v8"
	result.Platforms = []ImagePlatform{first, second}
	canonical, err := EncodeReleaseDocument(result)
	if err != nil {
		t.Fatal(err)
	}
	if result.Platforms[0].Variant != "v9" || result.Platforms[1].Variant != "v8" {
		t.Fatal("encoder mutated caller platform variants")
	}
	doc, f = ParseReleaseDocument(canonical)
	if len(f) > 0 {
		t.Fatal(f)
	}
	platforms := doc.(*OCIResult).Platforms
	if len(platforms) != 2 || platforms[0].Variant != "v8" || platforms[1].Variant != "v9" {
		t.Fatalf("variant order not sorted: %+v", platforms)
	}
	node, err := recordNode(reflect.ValueOf(result))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeCanonical(&out, node, 0); err != nil {
		t.Fatal(err)
	}
	out.WriteByte('\n')
	if _, f := ParseReleaseDocument(out.Bytes()); len(f) != 1 || f[0].Code != "record-not-canonical" {
		t.Fatalf("variant order accepted: %+v", f)
	}
}
