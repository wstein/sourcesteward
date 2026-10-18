// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Ready inputs are additive: none of these published hosting bytes may change.
func TestReadyPreservesHostingContractBytes(t *testing.T) {
	fixtures := map[string]string{
		"schemas/v1/hosting.schema.json":                   "5cf8bd2e67e209ee5f39befc2a0b0765a1f0fa1bb8082a940d1993e360133ff7",
		"internal/cli/testdata/hosting-ci.json":            "4e9f3b67616744dd4cd67e4cee4370ef69c9229526fc23b0ea95a9719a1d801b",
		"internal/cli/testdata/hosting-partial-error.json": "e72f1fb521ab5f8e6ca498f2d1d05be8859d47575a5b06cdb892d76c8a286f59",
		"internal/cli/testdata/hosting-pr.json":            "11aa7816ba946b5f9ce146454bb4aaf126d0e0099ca7a153010859931e7907b2",
		"internal/cli/testdata/hosting-repo.json":          "ff2b867ea5c51cd3cec0ed4537c96cc05fb79cadeacd02eb86e48a5238547239",
	}
	s := schema(t, "hosting")
	for name, want := range fixtures {
		raw, err := os.ReadFile(filepath.Join(root(t), name))
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != want {
			t.Errorf("published compatibility bytes changed: %s", name)
		}
		if filepath.Ext(name) == ".json" && name != "schemas/v1/hosting.schema.json" {
			valid(t, s, raw)
		}
	}
}
