// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"os"
	"testing"
)

func TestRegistrationPruneV4FinalRmdirBindsHeldRegistration(t *testing.T) {
	_, p := fixturePruneV4(t)
	fired := false
	ctx := pruneHookContext(func(stage, path string, _ any) {
		if stage != "v4-before-root-rmdir" {
			return
		}
		fired = true
		pruneMust(t, os.Rename(p.Registration.GitDir, p.Registration.GitDir+"-held"))
		pruneMust(t, os.Mkdir(p.Registration.GitDir, 0700))
	})
	r := ApplyRegistrationPruneV4(ctx, RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode})
	if !fired || r.Status != "partial" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "prune-evidence-changed" {
		t.Fatalf("descriptor binding: fired=%v result=%+v", fired, r)
	}
	for _, path := range []string{p.Registration.GitDir, p.Registration.GitDir + "-held"} {
		if _, e := os.Stat(path); e != nil {
			t.Fatalf("unbound directory removed: %s %v", path, e)
		}
	}
}
