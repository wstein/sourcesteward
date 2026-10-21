// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"testing"
	"time"
)

func TestV3ApplyTimeoutScalesWithPlannedDeletionsUpToACeiling(t *testing.T) {
	for effects, want := range map[int]time.Duration{0: 60 * time.Second, 1: 63 * time.Second, 54: 222 * time.Second, 10000: 10 * time.Minute} {
		if got := v3ApplyTimeout(effects); got != want {
			t.Errorf("%d effects: %s, want %s", effects, got, want)
		}
	}
}
