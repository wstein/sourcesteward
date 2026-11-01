// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"os"
	"path/filepath"
	"testing"
)

// requireNonUTF8Names skips the test where the file system rejects file names that are not valid
// UTF-8 (APFS does). Git stores a ref as a file, so such a branch name cannot exist there.
func requireNonUTF8Names(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(t.TempDir(), "br\xff"), nil, 0o600); err != nil {
		t.Skipf("the file system rejects non-UTF-8 file names, so the fixture cannot exist: %v", err)
	}
}
