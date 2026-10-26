// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
)

// Spec 044 D13: the tracking reads of the observation use the run budget of the context as the
// inner transport of their session, so its cap and its rate-limit stop apply to them; without a
// budget they keep the production transport.
func TestTrackingSessionUsesTheRunBudgetOfTheContext(t *testing.T) {
	var got hosting.TidyTransport
	called := 0
	old := openTrackingSession
	t.Cleanup(func() { openTrackingSession = old })
	openTrackingSession = func(_ context.Context, _ string, _ hosting.TidySessionOptions, rt hosting.TidyTransport) (*hosting.TidySession, error) {
		got = rt
		called++
		return nil, errors.New("stop here")
	}
	open := defaultTidyV3TrackingCoordinator().openHosting
	budget := hosting.NewTidyRunBudget(nil, 10)
	defer budget.Close()
	if _, err := open(hosting.WithTidyRunBudget(context.Background(), budget), "/x", hosting.TidySessionOptions{Online: true, Remote: "origin"}); err == nil {
		t.Fatal("the fixture stops the open")
	}
	if got != hosting.TidyTransport(budget) || called != 1 {
		t.Fatalf("the session did not get the run budget: %v", got)
	}
	got = budget
	if _, err := open(context.Background(), "/x", hosting.TidySessionOptions{Online: true, Remote: "origin"}); err == nil || got != nil || called != 2 {
		t.Fatalf("without a budget the transport is the production one (nil): %v %v", got, err)
	}
}
