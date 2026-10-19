// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if err := isolateGitEnvironment(); err != nil {
		fmt.Fprintln(os.Stderr, "tests refuse to run:", err)
		os.Exit(2)
	}
	os.Exit(m.Run())
}
