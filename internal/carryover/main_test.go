// SPDX-License-Identifier: EUPL-1.2
package carryover

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestMain gives every test a clean Git environment: repository-selecting variables are removed and
// no user or system configuration is read, so no test can touch a real repository or a real identity.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "GIT_") {
			if err := os.Unsetenv(name); err != nil {
				fmt.Fprintln(os.Stderr, "tests refuse to run:", err)
				os.Exit(2)
			}
		}
	}
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	os.Exit(m.Run())
}
