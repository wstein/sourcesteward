// SPDX-License-Identifier: EUPL-1.2

package planset

import (
	"slices"
	"testing"
)

func TestEnvironAndGetenvReadTheProcessEnvironment(t *testing.T) {
	t.Setenv("STW_PLANSET_PROBE", "v")
	if Getenv("STW_PLANSET_PROBE") != "v" || !slices.Contains(Environ(), "STW_PLANSET_PROBE=v") {
		t.Fatal("the process environment is not read")
	}
}
