// SPDX-License-Identifier: EUPL-1.2
package workspace

import "context"

// proveTidyV3RetirementCommit proves the integration of the real HEAD of a
// retirement with the proofs of Spec 011 and Spec 029 (Spec 041 plan WP-1): the
// native ancestry first, then the forge-free classes the policy selects, through
// the same code as local-reference plans. A head the forge-free classes do not
// prove stays `integration-not-proven`; their review evidence has no place in a
// retirement plan.
func proveTidyV3RetirementCommit(ctx context.Context, observed tidyV3NativeObservation, policy V3ProofPolicy, oid string, paths []string) (V3CommitProof, error) {
	c := defaultTidyV3RefsCoordinator()
	proof := V3CommitProof{OID: oid, Decision: "retain", Reason: "integration-not-proven", Proof: "none", WorkspacePaths: append([]string{}, paths...)}
	if policy.has(integrationAncestor) {
		var err error
		proof, err = proveTidyV3Commit(ctx, observed.Expected, oid, paths, nil, nil, func(ctx context.Context, target, base string) (bool, error) {
			return c.ancestry(ctx, observed.Expected, target, base)
		})
		if err != nil || proof.Decision == "delete" {
			return proof, err
		}
	}
	if !policy.has(integrationPatchID) && !policy.has(integrationMergeNoop) && !policy.has(integrationNoNetChange) {
		return proof, nil
	}
	ctx, _, closeSession, err := withPlanningSession(ctx)
	if err != nil {
		return proof, err
	}
	defer closeSession()
	run, closeRun, err := c.integration.begin(ctx, observed.Expected.Identity, observed.ConfigNotes)
	if err != nil {
		return proof, err
	}
	defer closeRun()
	proven, err := c.localProofs(run, observed.Expected, policy).prove(ctx, oid, paths)
	if err != nil {
		return proof, err
	}
	if proven.Decision == "delete" {
		return proven, nil
	}
	return proof, nil
}
