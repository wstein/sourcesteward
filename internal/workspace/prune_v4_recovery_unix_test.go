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
