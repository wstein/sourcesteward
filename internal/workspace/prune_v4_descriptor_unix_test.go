// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"os"
	"path/filepath"
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

func TestRegistrationPruneV4HeldParentMatchesPlan(t *testing.T) {
	_, p := fixturePruneV4(t)
	parent, e := openStoreKinds(p.StateDir, true, []string{pruneV4Namespace})
	if e != nil {
		t.Fatal(e)
	}
	defer parent.Close()
	s, e := openStoreKinds(filepath.Join(p.StateDir, pruneV4Namespace), true, []string{"plans", "journals", "payloads"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	expected := p.Expected
	expected.RegistrationParent.Inode++
	e = removePruneV4Metadata(context.Background(), p, expected, &pruneV4Journal{}, s, parent)
	assertPruneReason(t, e, "prune-evidence-changed", "Administrative parent identity changed before removal.")
	if _, e := os.Stat(p.Registration.GitDir); e != nil {
		t.Fatal("registration changed", e)
	}
}
