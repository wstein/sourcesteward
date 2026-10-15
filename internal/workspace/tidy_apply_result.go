// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"crypto/sha256"
	"fmt"
)

type TidyRecovery struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	CommitID    string `json:"commit_id"`
	ChangeID    string `json:"change_id"`
	OperationID string `json:"operation_id"`
}
type TidyNativeOutcome struct {
	Attempted            bool
	Complete             bool
	Effects, Limitations []string
	// Expected is populated only after the native adapter verifies the exact
	// full reference/operation delta. The coordinator independently reobserves it.
	Expected *Snapshot
}
type tidyMutationAdapter interface {
	tidyAdapter
	PrepareRecovery(context.Context, TidyPlanRecord, TidyCandidate) (TidyRecovery, Snapshot, TidyNativeOutcome, error)
	Delete(context.Context, TidyPlanRecord, TidyCandidate, TidyRecovery, Snapshot) (TidyNativeOutcome, error)
}

func tidyRecoveryFor(p TidyPlanRecord, c TidyCandidate) TidyRecovery {
	hash := sha256.Sum256([]byte(c.Kind + "\x00" + c.Name))
	suffix := fmt.Sprintf("%x", hash[:8])
	r := TidyRecovery{CommitID: c.CommitID, ChangeID: c.ChangeID, OperationID: p.Expected.Identity.OperationID}
	if p.Backend == "git" {
		r.Kind = "git-recovery-ref"
		r.Name = "refs/stw/recovery/" + p.PlanID + "/" + suffix
	} else {
		r.Kind = "jj-recovery-bookmark"
		r.Name = "stw-recovery-" + p.PlanID + "-" + suffix + "?"
	}
	return r
}
