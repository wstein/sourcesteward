// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistrationPruneV4RestoreGuardPostPublicationNativeState(t *testing.T) {
	p := restoreV4Fixture(t)
	ref := "refs/heads/added-after-restore-publication"
	fired := false
	ctx := pruneHookContext(func(stage, _ string, _ any) {
		if stage != "v4-restore-before-post" {
			return
		}
		fired = true
		if _, e := os.Stat(p.Registration.GitDir); e != nil {
			t.Fatalf("native-state change must happen after publication: %v", e)
		}
		gitFixture(t, p.SourcePath, "update-ref", ref, p.Expected.ResolvedHEAD)
	})
	r := RestoreRegistrationPruneV4(ctx, restoreV4ReviewApplied(p))
	restoreV4ReviewReason(t, r, "partial", "prune-restore-refused", "Administrative restore requires an absent original registration and unchanged recovery, ownership and native state.")
	if !fired {
		t.Fatal("post-publication observation hook was not reached")
	}
	if _, e := os.Stat(p.Registration.GitDir); e != nil {
		t.Fatalf("partial restore lost published metadata: %v", e)
	}
	if got := strings.TrimSpace(gitFixture(t, p.SourcePath, "rev-parse", ref)); got != p.Expected.ResolvedHEAD {
		t.Fatalf("external reference changed: got %q, want %q", got, p.Expected.ResolvedHEAD)
	}
}

func TestRegistrationPruneV4RestoreGuardSlotOccupiedAtPublication(t *testing.T) {
	p := restoreV4Fixture(t)
	var occupied os.FileInfo
	fired := false
	ctx := pruneHookContext(func(stage, _ string, _ any) {
		if stage != "v4-restore-staged-opened" {
			return
		}
		fired = true
		pruneMust(t, os.Mkdir(p.Registration.GitDir, 0700))
		pruneMust(t, os.WriteFile(filepath.Join(p.Registration.GitDir, "external-owner"), []byte("preserve this entry"), 0600))
		var e error
		occupied, e = os.Stat(p.Registration.GitDir)
		pruneMust(t, e)
	})
	r := RestoreRegistrationPruneV4(ctx, restoreV4ReviewApplied(p))
	restoreV4ReviewReason(t, r, "partial", "prune-registration-occupied", "The original administrative registration slot became occupied before publication; retained recovery has not been overwritten.")
	if !fired {
		t.Fatal("after-validation publication hook was not reached")
	}
	after, e := os.Stat(p.Registration.GitDir)
	pruneMust(t, e)
	if !os.SameFile(occupied, after) {
		t.Fatal("publication replaced the external owner's registration slot")
	}
	b, e := os.ReadFile(filepath.Join(p.Registration.GitDir, "external-owner"))
	pruneMust(t, e)
	if string(b) != "preserve this entry" {
		t.Fatal("publication modified the external owner's data")
	}
	if _, e := os.Stat(restoreV4GuardStaging(t, p)); e != nil {
		t.Fatalf("failed publication did not retain staging: %v", e)
	}
}
