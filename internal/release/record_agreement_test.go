// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordManifestTrustedFieldsIndependently(t *testing.T) {
	for _, field := range []string{"commit", "version", "config", "project", "product"} {
		t.Run(field, func(t *testing.T) {
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
			switch field {
			case "commit":
				m.Commit = strings.Repeat("a", len(c))
				if m.Commit == c {
					m.Commit = strings.Repeat("b", len(c))
				}
			case "version":
				m.Version = "v0.3.0"
			case "config":
				m.ConfigSHA256 = strings.Repeat("0", 64)
			case "project":
				m.Project = "Another project"
			case "product":
				m.Product = "another"
			}
			rewriteRecordManifest(t, dir, m)
			result := Record(context.Background(), RecordOptions{Path: d, Commit: c, Version: "v0.2.0", From: dir})
			if result.Error == nil || result.Error.Code != "artifact-mismatch" {
				t.Fatalf("isolated %s mismatch accepted: %+v", field, result)
			}
		})
	}
}
