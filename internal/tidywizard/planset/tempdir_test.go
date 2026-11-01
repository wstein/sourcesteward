// SPDX-License-Identifier: EUPL-1.2

package planset

import (
	"path/filepath"
	"testing"
)

// realTempDir returns a temporary directory without symlink components. On macOS t.TempDir()
// lives under /var, a symlink to /private/var, which the store and the planners reject as an
// alias.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
