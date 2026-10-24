// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
)

// Owned mode opens original completed creation records read-only on every full
// observation. Legacy declarations never create, adopt or replace ownership.
func readTidyV3OwnedProvenance(o V3PlanOptions) (V3OwnedProvenance, error) {
	var out V3OwnedProvenance
	old, e := openStoreKinds(o.StateDir, false, []string{"plans", "tasks", "operations"})
	if e != nil {
		return out, e
	}
	defer old.Close()
	creation, task, digest, e := readRetirementOwnership(old, o.Task, o.Owner)
	if e != nil {
		return out, e
	}
	if task.Backend != "git" || o.RemoveFiles != task.Destination {
		return out, failure("changed-ownership", "Exact native Git task and original destination are required.")
	}
	return V3OwnedProvenance{CreationPlan: creation, Task: task, CreationJournalDigest: digest, LockReason: "stw:" + task.TaskID}, nil
}
func observeTidyV3Retirement(ctx context.Context, o V3PlanOptions, budget *tidyV3ProofBudget) (tidyV3LegacyObservation, error) {
	return observeTidyV3RetirementWith(ctx, o, budget, tidyV3LegacyObserver{source: observeTidyV3Git, target: gitNativeIdentity})
}

// observeTidyV3RetirementWith is the entry of both retirement paths over an
// observer, so tests can watch the context the native observation receives.
func observeTidyV3RetirementWith(ctx context.Context, o V3PlanOptions, budget *tidyV3ProofBudget, observer tidyV3LegacyObserver) (tidyV3LegacyObservation, error) {
	if o.Task == "" {
		return observeTidyV3Legacy(ctx, o, budget)
	}
	// The observation itself carries the deadline, including the proof phase.
	if e := ctx.Err(); e != nil {
		return tidyV3LegacyObservation{}, e
	}
	if e := validateV3PlanOptions(o); e != nil {
		return tidyV3LegacyObservation{}, e
	}
	owned, e := readTidyV3OwnedProvenance(o)
	if e != nil {
		return tidyV3LegacyObservation{}, e
	}
	return observer.observeWithOwnership(ctx, o, budget, &owned)
}
