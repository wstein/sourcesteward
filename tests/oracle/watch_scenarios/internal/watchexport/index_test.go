// SPDX-License-Identifier: EUPL-1.2
package watchexport

import "testing"

// TestExportIndex writes index.json for the scenarios the other packages wrote. It runs last (the driver calls it separately).
func TestExportIndex(t *testing.T) {
	dir, err := OutDir()
	if err != nil {
		t.Skip("STW_EXPORT_DIR not set: " + err.Error())
	}
	idx, err := BuildIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d scenarios %v", idx.Count, idx.ByKind)
}
