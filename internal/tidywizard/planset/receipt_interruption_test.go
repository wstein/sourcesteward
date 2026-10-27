// SPDX-License-Identifier: EUPL-1.2
package planset

import (
	"context"
	"testing"
)

func TestReceiptConsumedByCanceledApply(t *testing.T) {
	dir, set, receipt := confirmedSet(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	backend := &fakeApplier{}
	result := Apply(ctx, receiptRequest(dir, set, receipt, backend))
	if result.Status != "failed" || len(result.Outcomes) != 1 || result.Outcomes[0].Status != StatusSkipped || result.Outcomes[0].Code != "cancelled" || len(backend.calls) != 0 {
		t.Fatalf("canceled apply must retain its no-effect reason: %+v calls=%d", result, len(backend.calls))
	}
	requireReceiptPrompt(t, receiptRequest(dir, set, receipt, nil))
}
