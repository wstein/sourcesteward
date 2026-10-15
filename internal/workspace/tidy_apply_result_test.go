// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"strings"
	"testing"
)

func TestTidyRecoveryBindsLiteralTargetAndPlan(t *testing.T) {
	p := TidyPlanRecord{PlanID: strings.Repeat("a", 32), Backend: "git"}
	c := TidyCandidate{Kind: "branch", Name: "refs/heads/topic", CommitID: strings.Repeat("b", 40)}
	r := tidyRecoveryFor(p, c)
	if r.Kind != "git-recovery-ref" || !strings.HasPrefix(r.Name, "refs/stw/recovery/"+p.PlanID+"/") || r.CommitID != c.CommitID {
		t.Fatalf("unbound recovery %+v", r)
	}
	c.Name = "refs/heads/topic-other"
	if tidyRecoveryFor(p, c).Name == r.Name {
		t.Fatal("target aliases recovery name")
	}
}
