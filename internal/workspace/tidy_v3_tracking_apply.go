// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"strings"

	"github.com/wstein/sourcesteward/internal/hosting"
)

// Tracking uses the same acknowledged recovery/journal/native-transaction
// orchestration as local refs. Only its typed hosting proof is mode-specific.
func (c tidyV3TrackingCoordinator) referenceApply() tidyV3RefsApplyCoordinator {
	apply := defaultTidyV3RefsApplyCoordinator()
	apply.refs = c.refs
	apply.refs.openHosting = func(ctx context.Context, path string, o hosting.TidySessionOptions) (tidyV3Hosting, error) {
		return c.openHosting(ctx, path, o)
	}
	return apply
}
func (c tidyV3TrackingCoordinator) apply(ctx context.Context, o V3ApplyOptions) V3Result {
	return c.referenceApply().apply(ctx, o)
}

// Each sample is a fresh complete advertisement in the original operation
// session. Equality covers the entire inventory, not just the selected name.
func revalidateTidyV3TrackingProof(ctx context.Context, p V3Plan, target V3Candidate, session tidyV3Hosting) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.Online == nil || target.Decision != "delete" || target.Proof != "github-branch-absent" || !validV3Candidate(target, p) {
		return failure("hosting-evidence-unavailable", "Exact repository-bound tracking absence evidence is required.")
	}
	branches, ok := session.(tidyV3TrackingHosting)
	if !ok || branches == nil {
		return failure("hosting-evidence-unavailable", "Tracking proof requires the original complete branch-advertisement session.")
	}
	repository := branches.Repository()
	if tidyV3Repository(repository) != p.Online.Repository {
		return failure("evidence-changed", "Provider repository identity changed since the tracking sample.")
	}
	advertisement, err := branches.ReadBranches(ctx)
	if err != nil {
		return tidyV3HostingError(err)
	}
	if err := validateTidyV3Advertisement(advertisement, repository); err != nil {
		return err
	}
	proof := target.Evidence.Absence
	if advertisement.Fingerprint != proof.AdvertisementDigest {
		return failure("evidence-changed", "The complete remote branch advertisement changed; retain remaining tracking targets.")
	}
	source := strings.TrimPrefix(proof.SourceRef, "refs/heads/")
	for _, branch := range advertisement.Branches {
		if branch.Name == source {
			return failure("evidence-changed", "The mapped remote source branch is present; retain its local tracking reference.")
		}
	}
	return ctx.Err()
}
