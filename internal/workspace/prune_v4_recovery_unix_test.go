// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistrationPruneV4JournalUsesClosedVocabulary(t *testing.T) {
	_, p := fixturePruneV4(t)
	ctx := pruneHookContext(func(stage, path string, _ any) {
		if stage != "v4-before-unlink" {
			return
		}
		b, e := os.ReadFile(filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json"))
		if e != nil {
			t.Fatal(e)
		}
		var j pruneV4Journal
		if e = json.Unmarshal(b, &j); e != nil {
			t.Fatal(e)
		}
		checkPruneV4JournalVocabulary(t, j)
	})
	r := ApplyRegistrationPruneV4(ctx, RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode})
	if r.Status != "ok" {
		t.Fatalf("apply %+v", r)
	}
	b, e := os.ReadFile(filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json"))
	if e != nil {
		t.Fatal(e)
	}
	var j pruneV4Journal
	if e = json.Unmarshal(b, &j); e != nil {
		t.Fatal(e)
	}
	checkPruneV4JournalVocabulary(t, j)
}
func checkPruneV4JournalVocabulary(t *testing.T, j pruneV4Journal) {
	t.Helper()
	switch j.Phase {
	case "prepared", "recovery-confirmed", "removing", "complete", "partial":
	default:
		t.Fatalf("non-contract phase %q", j.Phase)
	}
	for _, e := range j.Effects {
		switch e.Outcome {
		case "planned", "observed", "refused", "unknown":
		default:
			t.Fatalf("non-contract outcome %q", e.Outcome)
		}
	}
}
func TestRegistrationPruneV4LateRefusalsKeepRecovery(t *testing.T) {
	for _, kind := range []string{"canceled", "payload", "new-entry", "replacement", "ownership"} {
		t.Run(kind, func(t *testing.T) {
			_, p := fixturePruneV4(t)
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			fired := false
			ctx := context.WithValue(base, pruneObservationHookKey{}, pruneObservationHook(func(stage, path string, _ any) {
				if fired || stage != "v4-before-unlink" {
					return
				}
				fired = true
				var e error
				switch kind {
				case "canceled":
					cancel()
				case "payload":
					e = os.WriteFile(filepath.Join(p.PayloadPath, "files", "index"), []byte("broken payload"), 0600)
				case "new-entry":
					e = os.WriteFile(filepath.Join(p.Registration.GitDir, "foreign"), []byte("keep"), 0600)
				case "replacement":
					e = os.Rename(p.Registration.GitDir, p.Registration.GitDir+"-saved")
					if e == nil {
						e = os.Mkdir(p.Registration.GitDir, 0700)
					}
				case "ownership":
					e = os.Chmod(filepath.Join(p.StateDir, "tasks", p.Registration.Task+".json"), 0644)
				}
				if e != nil {
					t.Fatal(e)
				}
			}))
			r := ApplyRegistrationPruneV4(ctx, RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode})
			if !fired || r.Status != "partial" || len(r.Diagnostics) != 1 {
				t.Fatalf("late refusal %+v fired=%v", r, fired)
			}
			want := map[string]string{"canceled": "canceled", "payload": "prune-recovery-refused", "new-entry": "prune-metadata-refused", "replacement": "prune-evidence-changed", "ownership": "prune-ownership-refused"}[kind]
			if r.Diagnostics[0].Code != want || r.Diagnostics[0].Message == "" {
				t.Fatalf("reason want %s got %+v", want, r.Diagnostics)
			}
			if _, e := os.Lstat(p.Registration.GitDir); e != nil {
				t.Fatal("registration removed", e)
			}
			if _, e := os.Stat(filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json")); e != nil {
				t.Fatal("partial journal lost", e)
			}
			if _, e := os.Stat(p.PayloadPath); e != nil {
				t.Fatal("payload lost", e)
			}
		})
	}
}
func TestRegistrationPruneV4RejectsUnverifiedSiblingIDs(t *testing.T) {
	for _, kind := range []string{"self", "malformed", "too-many"} {
		t.Run(kind, func(t *testing.T) {
			_, p := fixturePruneV4(t)
			ids := []string{p.PlanID}
			if kind == "malformed" {
				ids = []string{"../outside"}
			}
			if kind == "too-many" {
				ids = make([]string, v3ChainLimit+1)
			}
			r := ApplyRegistrationPruneV4(context.Background(), RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode, SiblingPlans: ids})
			if r.Status != "refused" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "invalid-options" {
				t.Fatalf("chain reason %+v", r)
			}
			if _, e := os.Stat(p.Registration.GitDir); e != nil {
				t.Fatal("registration changed", e)
			}
		})
	}
}
func TestRegistrationPruneV4JournalRejectsUnknownPhase(t *testing.T) {
	_, p := fixturePruneV4(t)
	r := ApplyRegistrationPruneV4(context.Background(), RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode})
	if r.Status != "ok" {
		t.Fatal(r)
	}
	path := filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json")
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	b = []byte(strings.Replace(string(b), `"phase":"complete"`, `"phase":"invented"`, 1))
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	r = ApplyRegistrationPruneV4(context.Background(), RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode})
	if r.Status != "refused" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "prune-journal-refused" {
		t.Fatalf("journal reason %+v", r)
	}
}
