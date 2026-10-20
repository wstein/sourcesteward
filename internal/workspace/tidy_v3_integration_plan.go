// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"slices"
)

// historyHardStopsKey marks a planning context in which grafts and alternates
// are the hard stops of the proof contract (history-override,
// unsupported-metadata) instead of observation refusals. Apply keeps its strict
// admission.
type historyHardStopsKey struct{}

func withHistoryHardStops(ctx context.Context) context.Context {
	return context.WithValue(ctx, historyHardStopsKey{}, true)
}
func historyHardStopsTolerated(ctx context.Context) bool {
	v, _ := ctx.Value(historyHardStopsKey{}).(bool)
	return v
}

// tidyV3LocalProofs proves the tips of one local-reference plan: the ancestry
// proof of Spec 029 first, then the forge-free classes, once per distinct tip.
type tidyV3LocalProofs struct {
	c        tidyV3RefsCoordinator
	run      *integrationRun
	expected Snapshot
	policy   V3ProofPolicy
	done     map[string]V3CommitProof
	// verdicts keeps the verdict of every tip that needs review for the report.
	verdicts map[string]integrationVerdict
}

func (c tidyV3RefsCoordinator) localProofs(run *integrationRun, expected Snapshot, policy V3ProofPolicy) *tidyV3LocalProofs {
	return &tidyV3LocalProofs{c: c, run: run, expected: expected, policy: policy, done: map[string]V3CommitProof{}, verdicts: map[string]integrationVerdict{}}
}

func (l *tidyV3LocalProofs) hasForgeFreeClass() bool {
	return l.policy.has(integrationPatchID) || l.policy.has(integrationMergeNoop) || l.policy.has(integrationNoNetChange)
}

func (l *tidyV3LocalProofs) prove(ctx context.Context, oid string, paths []string) (V3CommitProof, error) {
	if proof, ok := l.done[oid]; ok {
		proof.WorkspacePaths = append([]string{}, paths...)
		return proof, nil
	}
	proof, err := l.proveTip(ctx, oid, paths)
	if err == nil {
		l.done[oid] = proof
	}
	return proof, err
}

func (l *tidyV3LocalProofs) proveTip(ctx context.Context, oid string, paths []string) (V3CommitProof, error) {
	out := V3CommitProof{OID: oid, Decision: "retain", Reason: "integration-not-proven", Proof: "none", WorkspacePaths: append([]string{}, paths...)}
	evidence := func(v integrationVerdict) *V3IntegrationEvidence {
		e := &V3IntegrationEvidence{Class: v.Class, Disposition: v.Disposition, Reasons: append([]string{}, v.Reasons...), Flags: append([]string{}, v.Flags...), BaseSelector: l.expected.BaseSelector, BaseOID: l.expected.BaseCommit, TipOID: oid, Matched: v.Matched, Unmatched: v.Unmatched, Empty: v.Empty, SkippedMerges: v.SkippedMerges}
		e.Strength = integrationStrength[v.Class]
		return e
	}
	review := func(v integrationVerdict) V3CommitProof {
		out.Reason, out.Evidence = integrationReviewDisposition, &V3ProofEvidence{Integration: evidence(v)}
		l.verdicts[oid] = v
		return out
	}
	if len(l.run.hardStops) > 0 {
		v, _ := l.run.evaluate(ctx, l.expected.BaseCommit, oid, l.policy)
		out.Reason, out.Evidence = v.Reasons[0], &V3ProofEvidence{Integration: evidence(v)}
		return out, nil
	}
	if err := l.run.beginTip(); err != nil {
		return review(reviewVerdict(integrationVerdict{Flags: []string{}}, "proof-budget-exhausted")), nil
	}
	if l.policy.has(integrationAncestor) {
		l.run.traceClass(integrationAncestor)
		proof, err := proveTidyV3Commit(ctx, l.expected, oid, paths, nil, nil, func(ctx context.Context, target, base string) (bool, error) {
			return l.c.ancestry(ctx, l.expected, target, base)
		})
		if err != nil || proof.Decision == "delete" {
			return proof, err
		}
		out = proof
	}
	if !l.hasForgeFreeClass() {
		return out, nil
	}
	v, err := l.run.evaluate(ctx, l.expected.BaseCommit, oid, l.policy)
	if err != nil {
		return out, err
	}
	switch v.Disposition {
	case integrationProvenDisposition:
		out.Decision, out.Reason, out.Proof = "delete", v.Class, v.Class
		out.Evidence = &V3ProofEvidence{Integration: evidence(v)}
	case integrationReviewDisposition:
		return review(v), nil
	}
	return out, nil
}

// revalidateIntegrationProof re-evaluates the recorded class with fresh state
// for apply and requires the identical evidence; any difference retains.
func (c tidyV3RefsApplyCoordinator) revalidateIntegrationProof(ctx context.Context, p V3Plan, expected Snapshot, target V3Candidate) error {
	changed := func() error {
		return failure("evidence-changed", "The recorded integration proof no longer holds with fresh native state; retained.")
	}
	recorded := target.Evidence.Integration
	if p.ProofPolicy == nil || recorded == nil || !integrationProofTag(target.Proof) || !p.ProofPolicy.has(target.Proof) {
		return failure("invalid-evidence", "Stored integration proof does not bind its recorded policy.")
	}
	ctx, _, closeSession, err := withPlanningSession(ctx)
	if err != nil {
		return err
	}
	defer closeSession()
	observation, err := c.refs.observe(ctx, p.SourcePath, p.Expected.BaseSelector)
	if err != nil {
		return err
	}
	if observation.Expected != expected {
		return changed()
	}
	run, cleanup, err := c.refs.integration.begin(ctx, expected.Identity, observation.ConfigNotes)
	if err != nil {
		return err
	}
	defer cleanup()
	if err = run.beginTip(); err != nil {
		return changed()
	}
	only := V3ProofPolicy{Version: p.ProofPolicy.Version, Kinds: []string{target.Proof}, AcceptWhitespaceDifferences: p.ProofPolicy.AcceptWhitespaceDifferences}
	evaluate := c.refs.proofRunner
	if evaluate == nil {
		evaluate = func(ctx context.Context, run *integrationRun, base, tip string, policy V3ProofPolicy) (integrationVerdict, error) {
			return run.evaluate(ctx, base, tip, policy)
		}
	}
	v, err := evaluate(ctx, run, expected.BaseCommit, target.OID, only)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return changed()
	}
	if v.Disposition != integrationProvenDisposition || v.Class != recorded.Class || !slices.Equal(v.Flags, recorded.Flags) ||
		v.Matched != recorded.Matched || v.Unmatched != recorded.Unmatched || v.Empty != recorded.Empty || v.SkippedMerges != recorded.SkippedMerges {
		return changed()
	}
	return nil
}

// revalidateReviewedDecision re-checks a reviewer decision for apply (FR-012):
// the reference still points at the exact decided tip, the plan's base is the
// decided base and no protection applies now. A decision never skips a check.
func (c tidyV3RefsApplyCoordinator) revalidateReviewedDecision(ctx context.Context, p V3Plan, expected Snapshot, target V3Candidate) error {
	changed := func() error {
		return failure("evidence-changed", "The reviewed decision no longer holds with fresh native state; retained.")
	}
	d := decisionOf(target)
	if d == nil || d.TipOID != target.OID || d.BaseOID != p.Expected.BaseCommit || d.Branch != target.Name {
		return failure("invalid-evidence", "Stored reviewer decision does not bind the exact tip and base.")
	}
	ctx, _, closeSession, err := withPlanningSession(ctx)
	if err != nil {
		return err
	}
	defer closeSession()
	observation, err := c.refs.observe(ctx, p.SourcePath, p.Expected.BaseSelector)
	if err != nil {
		return err
	}
	if observation.Expected != expected {
		return changed()
	}
	n := observation.Native
	found := false
	for _, ref := range n.References {
		if ref.Name != target.Name {
			continue
		}
		found = ref.OID == target.OID && ref.Symbolic == ""
	}
	if !found || tidyV3SymbolicTarget(n, target.Name) || target.Name == p.Expected.BaseSelector || target.OID == p.Expected.BaseCommit {
		return changed()
	}
	for _, w := range n.Registrations {
		if w.HeadRef == target.Name || w.HeadOID == target.OID {
			return changed()
		}
	}
	return nil
}
