// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"time"
)

const (
	observationPerWorktree = 500 * time.Millisecond
	observationWindowMax   = 10 * time.Minute
	// ObservationTimeoutMax is the largest explicit --timeout of the wizard analysis.
	ObservationTimeoutMax = time.Hour
)

// WithObservationTimeout carries an explicit --timeout into the observation (stub).
func WithObservationTimeout(ctx context.Context, d time.Duration) context.Context { return ctx }

// observationWindow is the deadline of one repository observation (stub).
func observationWindow(ctx context.Context, registrations int) time.Duration {
	return planObservationTimeout
}
