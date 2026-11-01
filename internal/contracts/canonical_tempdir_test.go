// SPDX-License-Identifier: EUPL-1.2

package contracts_test

import (
	"path/filepath"
	"testing"
)

// canonicalTempDir returns a temporary directory without symlink components. On macOS
// t.TempDir() is under /var, a symlink to /private/var, which stw refuses as a state or
// destination path.
func canonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
