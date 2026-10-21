// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"testing"
	"time"
)

func TestV3ApplyTimeoutScalesWithTheWorkWithoutACeiling(t *testing.T) {
	for _, tc := range []struct {
		effects, entries int
		want             time.Duration
	}{{0, 0, 60 * time.Second}, {1, 0, 65 * time.Second}, {54, 0, 330 * time.Second}, {1, 5000, 60*time.Second + 5*time.Second + 250*time.Second}, {10000, 0, 60*time.Second + 50000*time.Second}} {
		if got := v3ApplyTimeout(tc.effects, tc.entries); got != tc.want {
			t.Errorf("%+v: %s", tc, got)
		}
	}
}
