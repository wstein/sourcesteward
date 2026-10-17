// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTOMLCorpusDump is the Go half of scripts/test_release_toml.py: it parses every *.toml file
// of the directory named by STW_TOML_CORPUS and writes <name>.json with the verdict and data, so
// the script can compare accepted files with Python tomllib. It does nothing otherwise.
func TestTOMLCorpusDump(t *testing.T) {
	dir := os.Getenv("STW_TOML_CORPUS")
	if dir == "" {
		t.Skip("STW_TOML_CORPUS is not set")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus files in %s: %v", dir, err)
	}
	for _, name := range files {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		doc, findings := ParseTOML(raw)
		verdict := map[string]any{"accepted": doc != nil, "data": doc, "findings": len(findings)}
		out, err := json.Marshal(verdict)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(strings.TrimSuffix(name, ".toml")+".json", out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
