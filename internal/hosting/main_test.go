// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"os"
	"testing"

	"github.com/wstein/sourcesteward/internal/gittest"
)

// The tidy evidence of the hosting package reads repositories through the inspection that needs Git
// 2.46; on older Git no test of this package runs.
func TestMain(m *testing.M) {
	gittest.LimitBelow(2, 46, "^$", "")
	os.Exit(m.Run())
}
