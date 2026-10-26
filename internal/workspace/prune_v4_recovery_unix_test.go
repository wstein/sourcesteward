// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestRegistrationPruneV4PayloadRejectsExtraSidecar(t *testing.T) {
	_, p := fixturePruneV4(t)
	r := ApplyRegistrationPruneV4(context.Background(), RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode})
	if r.Status != "ok" {
		t.Fatal(r)
	}
	if e := os.WriteFile(filepath.Join(p.PayloadPath, "files", "unexpected"), []byte("unbound"), 0600); e != nil {
		t.Fatal(e)
	}
	_, _, e := readPruneV4Payload(context.Background(), p)
	assertPruneReason(t, e, "prune-recovery-refused", "Complete private registration recovery could not be verified.")
}
func TestRegistrationPruneV4SourceChangeBeforeRemoval(t *testing.T) {
	for _, kind := range []string{"reference", "source-config"} {
		t.Run(kind, func(t *testing.T) {
			_, p := fixturePruneV4(t)
			fired := false
			ctx := pruneHookContext(func(stage, path string, _ any) {
				if fired || stage != "v4-before-unlink" {
					return
				}
				fired = true
				if kind == "reference" {
					gitFixture(t, p.SourcePath, "update-ref", "refs/heads/foreign", p.Expected.ResolvedHEAD)
				} else {
					gitFixture(t, p.SourcePath, "config", "stw.changed", "true")
				}
			})
			r := ApplyRegistrationPruneV4(ctx, RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode})
			if r.Status != "partial" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "prune-evidence-changed" {
				t.Fatalf("source refusal %+v", r)
			}
			for _, m := range p.Expected.Metadata {
				if m.Kind == "file" {
					b, e := os.ReadFile(filepath.Join(p.Registration.GitDir, m.Path))
					if e != nil || gitDigest(string(b)) != m.Digest {
						t.Fatalf("metadata removed before detecting source change: %s %v", m.Path, e)
					}
				}
			}
		})
	}
}

func TestRegistrationPruneV4LateMountAndPartialRemoval(t *testing.T) {
	for _, kind := range []string{"common-mount", "after-first-unlink"} {
		t.Run(kind, func(t *testing.T) {
			_, p := fixturePruneV4(t)
			armed := false
			calls := 0
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := context.WithValue(base, pruneV4MountReaderKey{}, func(fd int) (string, error) {
				if armed {
					return "changed-mount", nil
				}
				return pruneMountIdentity(fd)
			})
			ctx = context.WithValue(ctx, pruneObservationHookKey{}, pruneObservationHook(func(stage, path string, _ any) {
				if stage != "v4-before-unlink" {
					return
				}
				calls++
				if kind == "common-mount" {
					armed = true
				} else if calls == 2 {
					cancel()
				}
			}))
			r := ApplyRegistrationPruneV4(ctx, RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode})
			want := "prune-mount-changed"
			if kind == "after-first-unlink" {
				want = "canceled"
			}
			if r.Status != "partial" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != want {
				t.Fatalf("partial reason %+v", r)
			}
			_, digest, e := readPruneV4Payload(context.Background(), p)
			if e != nil || digest == "" {
				t.Fatalf("recovery lost: %v", e)
			}
			raw, e := os.ReadFile(filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json"))
			if e != nil {
				t.Fatal(e)
			}
			var j pruneV4Journal
			if e = json.Unmarshal(raw, &j); e != nil {
				t.Fatal(e)
			}
			if j.Phase != "partial" || len(j.Remaining) == 0 {
				t.Fatalf("missing residual %+v", j)
			}
			observed := 0
			for _, effect := range j.Effects {
				if effect.Action == "remove-metadata" && effect.Outcome == "observed" {
					observed++
				}
			}
			if kind == "after-first-unlink" && observed != 1 || kind == "common-mount" && observed != 0 {
				t.Fatalf("wrong observed removals %d", observed)
			}
		})
	}
}

func TestRegistrationPruneV4PayloadExactBounds(t *testing.T) {
	for _, kind := range []string{"file", "total", "entries"} {
		for _, over := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/over=%v", kind, over), func(t *testing.T) {
				_, p := fixturePruneV4(t)
				var maxFile int64
				for _, m := range p.Expected.Metadata {
					if m.Size > maxFile {
						maxFile = m.Size
					}
				}
				limits := pruneMetadataLimits{File: maxFile, Total: p.Expected.MetadataBytes, Entries: len(p.Expected.Metadata)}
				if over {
					switch kind {
					case "file":
						limits.File--
					case "total":
						limits.Total--
					case "entries":
						limits.Entries--
					}
				}
				s, e := openStoreKinds(filepath.Join(p.StateDir, pruneV4Namespace), true, []string{"plans", "journals", "payloads"})
				if e != nil {
					t.Fatal(e)
				}
				defer s.Close()
				digest, e := preparePruneV4PayloadWithLimits(context.Background(), p, s, limits)
				if over {
					assertPruneReason(t, e, "prune-metadata-limit", "")
					if digest != "" {
						t.Fatal("overflow claimed recovery")
					}
				} else if e != nil || digest == "" {
					t.Fatalf("exact bound: %v", e)
				}
				if _, e = os.Stat(p.Registration.GitDir); e != nil {
					t.Fatal("payload preparation removed registration", e)
				}
			})
		}
	}
}

func TestRegistrationPruneV4ReviewJournalRejectsUnknownVocabulary(t *testing.T) {
	for _, field := range []string{"action", "intent", "target", "remaining", "duplicate", "reordered", "missing-effect", "early-payload-digest"} {
		t.Run(field, func(t *testing.T) {
			_, p := fixturePruneV4(t)
			o := RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode}
			if r := ApplyRegistrationPruneV4(context.Background(), o); r.Status != "ok" {
				t.Fatal(r)
			}
			path := filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json")
			raw, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			var j pruneV4Journal
			if e = json.Unmarshal(raw, &j); e != nil {
				t.Fatal(e)
			}
			switch field {
			case "action":
				j.Effects[0].Action = "invented"
			case "intent":
				j.Effects[0].Intent = "invented"
			case "target":
				j.Effects[0].Target = "refs/heads/unrelated"
			case "remaining":
				j.Phase = "partial"
				j.Remaining = []string{"invented"}
			case "duplicate":
				j.Effects = append(j.Effects, j.Effects[len(j.Effects)-1])
			case "reordered":
				j.Effects[0], j.Effects[1] = j.Effects[1], j.Effects[0]
			case "missing-effect":
				j.Effects = j.Effects[:len(j.Effects)-1]
			case "early-payload-digest":
				j.Phase = "partial"
				j.Effects = []pruneV4Effect{}
				j.PayloadDigest = "malformed"
				j.Remaining = []string{"prepare-recovery", "pin-history", "remove-registration"}
			}
			raw, e = pruneV4Canonical(j, "")
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(path, append(raw, '\n'), 0600); e != nil {
				t.Fatal(e)
			}
			r2 := ApplyRegistrationPruneV4(context.Background(), o)
			if r2.Status != "refused" || len(r2.Diagnostics) != 1 || r2.Diagnostics[0].Code != "prune-journal-refused" {
				t.Fatalf("incompatible %s accepted: status=%s diagnostics=%+v", field, r2.Status, r2.Diagnostics)
			}
		})
	}
}
func TestRegistrationPruneV4ReviewPartialExposesKnownEffectsAndRecovery(t *testing.T) {
	_, p := fixturePruneV4(t)
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := 0
	ctx := context.WithValue(base, pruneObservationHookKey{}, pruneObservationHook(func(stage, path string, _ any) {
		if stage == "v4-before-unlink" {
			n++
			if n == 2 {
				cancel()
			}
		}
	}))
	r := ApplyRegistrationPruneV4(ctx, RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode})
	if r.Status != "partial" || n != 2 {
		t.Fatalf("probe failed to produce partial removal: %+v count=%d", r, n)
	}
	if len(r.Actions) == 0 {
		t.Fatalf("partial output omits known removal and available recovery: actions=%+v payload=%s diagnostics=%+v", r.Actions, p.PayloadPath, r.Diagnostics)
	}
	for _, result := range []RegistrationPruneV4Result{r, ApplyRegistrationPruneV4(context.Background(), RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode})} {
		payload, pin, removed := false, false, 0
		for _, action := range result.Actions {
			if strings.Contains(action.Detail, p.PayloadPath) {
				payload = true
			}
			if action.Name == "pin-history" && strings.Contains(action.Detail, pruneV4HistoryRef(p)) {
				pin = true
			}
			if action.Name == "remove-metadata" && action.Status == "complete" {
				removed++
			}
		}
		if result.Status != "partial" || !payload || !pin || removed != 1 {
			t.Fatalf("partial replay lost recorded evidence: %+v", result)
		}
	}
}

func TestRegistrationPruneV4RefusesSymbolicHistoryPinBeforeRemoval(t *testing.T) {
	_, p := fixturePruneV4(t)
	fired := false
	ctx := pruneHookContext(func(stage, path string, _ any) {
		if fired || stage != "v4-before-unlink" {
			return
		}
		fired = true
		gitFixture(t, p.SourcePath, "symbolic-ref", pruneV4HistoryRef(p), p.Expected.SourceSnapshot.Identity.HeadRef)
	})
	r := ApplyRegistrationPruneV4(ctx, RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode})
	if r.Status != "partial" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "prune-evidence-changed" {
		t.Fatalf("literal recovery refusal %+v", r)
	}
	for _, m := range p.Expected.Metadata {
		if m.Kind == "file" {
			b, e := os.ReadFile(filepath.Join(p.Registration.GitDir, m.Path))
			if e != nil || gitDigest(string(b)) != m.Digest {
				t.Fatalf("metadata removed with symbolic history pin: %s %v", m.Path, e)
			}
		}
	}
}
