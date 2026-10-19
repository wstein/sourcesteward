// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package inspection

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

// TestMain fixes the umask: the pins validation refuses world-writable submodule directories, and a
// sandbox with umask 000 would otherwise create every fixture directory world-writable.
func TestMain(m *testing.M) {
	syscall.Umask(0o022)
	if err := isolateGitEnvironment(); err != nil {
		fmt.Fprintln(os.Stderr, "tests refuse to run:", err)
		os.Exit(2)
	}
	os.Exit(m.Run())
}
