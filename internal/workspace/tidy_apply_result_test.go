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
	p.Backend = "jj"
	c.Kind = "bookmark"
	c.ChangeID = strings.Repeat("k", 32)
	jj := tidyRecoveryFor(p, c)
	if !strings.HasSuffix(jj.Name, "?") {
		t.Fatal("jj recovery name could autoexport to Git")
	}
	p.Backend = "git"
	c.Kind = "branch"
	c.ChangeID = ""
	c.Name = "refs/heads/topic-other"
	if tidyRecoveryFor(p, c).Name == r.Name {
		t.Fatal("target aliases recovery name")
	}
}
