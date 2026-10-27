// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func TestJanitorApplyUsesOneTypedConfirmationPerWord(t *testing.T) {
	j := newJanitor(t)
	out, tty, err := j.run("a\n\n", func(q string) string {
		if strings.Contains(q, "Apply it now?") {
			return "y"
		}
		return rightWord(q)
	})
	if err != nil {
		t.Fatalf("apply failed: %v\n%s", err, out)
	}
	if len(j.applied) != 1 || !j.applied[0].Authorize {
		t.Fatalf("terminal y must authorize one apply; calls=%+v\n%s", j.applied, out)
	}
	typed, answers := 0, 0
	for _, q := range tty.prompts {
		if strings.Contains(q, "Type ") {
			typed++
		}
		if strings.Contains(q, "Apply it now?") {
			answers++
			if !strings.Contains(q, "words typed above cover this plan") {
				t.Fatalf("apply question must explain the prior words: %q", q)
			}
		}
	}
	if typed != 1 || answers != 1 {
		t.Fatalf("one foreign word and one terminal apply question required: %q", tty.prompts)
	}
}

func TestJanitorApplyDeclineKeepsStoredPlanWithoutApplying(t *testing.T) {
	j := newJanitor(t)
	out, tty, err := j.run("a\n\n", func(q string) string {
		if strings.Contains(q, "Apply it now?") {
			return "n"
		}
		return rightWord(q)
	})
	if err != nil {
		t.Fatalf("decline: %v\n%s", err, out)
	}
	if len(j.applied) != 0 || len(storedSets(t, j.prep.PlanStore)) != 1 {
		t.Fatalf("decline must retain only the stored plan: calls=%+v\n%s", j.applied, out)
	}
	asked := false
	for _, q := range tty.prompts {
		asked = asked || strings.Contains(q, "Apply it now?")
	}
	if !asked {
		t.Fatalf("missing terminal apply question: %q\n%s", tty.prompts, out)
	}
}

// The backend is a double, but authorization and receipt consumption run through
// the real planset.Apply so dropping the receipt at the CLI boundary is observable.
type p4bCountingApplier struct{ calls int }

func (a *p4bCountingApplier) Apply(context.Context, planset.Step) workspace.V3Result {
	a.calls++
	return workspace.V3Result{Status: "ok"}
}

func TestJanitorReceiptReachesRealPlanSetApply(t *testing.T) {
	j := newJanitor(t)
	backend := &p4bCountingApplier{}
	tidyApplyPlanSet = func(ctx context.Context, req planset.ApplyRequest) planset.ApplyResult {
		req.Applier = backend
		return planset.Apply(ctx, req)
	}
	out, tty, err := j.run("a\n\n", func(q string) string {
		if strings.Contains(q, "Apply it now?") {
			return "y"
		}
		return rightWord(q)
	})
	if err != nil || backend.calls == 0 {
		t.Fatalf("real authorization must reach the backend: err=%v calls=%d\n%s", err, backend.calls, out)
	}
	typed := 0
	for _, q := range tty.prompts {
		if strings.Contains(q, "Type ") {
			typed++
		}
	}
	if typed != 1 {
		t.Fatalf("receipt must cross CLI handoff: typed=%d prompts=%q", typed, tty.prompts)
	}
}
