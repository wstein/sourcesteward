// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
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

// An explicit --timeout replaces every default deadline of the operation, the
// shorter inner ones included; without it the defaults stand.
func TestV3TimeoutOverrideReplacesDefaults(t *testing.T) {
	if got := v3Timeout(context.Background(), 10*time.Second); got != 10*time.Second {
		t.Fatalf("default %s", got)
	}
	ctx := withV3Timeout(context.Background(), 30*time.Minute)
	if got := v3Timeout(ctx, 10*time.Second); got != 30*time.Minute {
		t.Fatalf("override %s", got)
	}
	if got := v3Timeout(withV3Timeout(context.Background(), 0), time.Second); got != time.Second {
		t.Fatalf("zero override %s", got)
	}
}
