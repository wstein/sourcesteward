// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"bytes"
	"os"
	"testing"
)

func TestRegistrationPruneV4RecoveryLimitTextIsDigestBound(t *testing.T) {
	_, p := fixturePruneV4(t)
	before, err := os.ReadFile(pruneV4PlanPath(p))
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePruneV4Plan(p, p.StateDir, p.PlanID); err != nil {
		t.Fatalf("unmodified control plan is invalid: %v", err)
	}
	old := clonePruneV4(t, p)
	// Exact historical wording, before the apply-capability correction in #290.
	old.RecoveryLimits[0] = "Planning stores evidence only; apply, recovery and restore are unavailable in this source slice."
	digest, err := pruneV4Digest("stw-registration-plan-v4", old, "digest")
	if err != nil {
		t.Fatal(err)
	}
	if digest == p.Digest {
		t.Fatal("recovery-limit text did not change the immutable plan digest")
	}
	// Reusing the original digest after changing only this text must refuse.
	err = validatePruneV4Plan(old, p.StateDir, p.PlanID)
	assertPruneReason(t, err, "prune-plan-refused", "Contract-4 plan version, operation, witness, scope or digest is invalid.")
	after, err := os.ReadFile(pruneV4PlanPath(p))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("stored plan was rewritten: %v", err)
	}
	if _, err = ReadRegistrationPruneV4Plan(p.StateDir, p.PlanID); err != nil {
		t.Fatalf("original immutable plan no longer reads: %v", err)
	}
}
