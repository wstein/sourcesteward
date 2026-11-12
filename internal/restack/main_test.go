// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"fmt"
	"os"
	"testing"

	"github.com/wstein/sourcesteward/internal/gittest"
)

func TestMain(m *testing.M) {
	gittest.LimitBelow(2, 46, "^$", "")
	if err := isolateGitEnvironment(); err != nil {
		fmt.Fprintln(os.Stderr, "tests refuse to run:", err)
		os.Exit(2)
	}
	os.Exit(m.Run())
}
