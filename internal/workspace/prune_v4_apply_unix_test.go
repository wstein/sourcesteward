// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRegistrationPruneV4ApplyRequiresAuthorization(t *testing.T) {
	_, p := fixturePruneV4(t)
	r := ApplyRegistrationPruneV4(context.Background(), RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Mode: p.Mode})
	if r.Status != "refused" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "authorization-required" {
		t.Fatalf("refusal reason: %+v", r)
	}
	if _, e := os.Stat(p.Registration.GitDir); e != nil {
		t.Fatal("registration changed", e)
	}
	if _, e := os.Stat(p.PayloadPath); !os.IsNotExist(e) {
		t.Fatal("unauthorized recovery write", e)
	}
}

func TestRegistrationPruneV4ApplyPreservesBinaryRecovery(t *testing.T) {
	_, p := fixturePruneV4(t)
	original := map[string][]byte{}
	for _, m := range p.Expected.Metadata {
		if m.Kind == "file" {
			b, e := os.ReadFile(filepath.Join(p.Registration.GitDir, m.Path))
			if e != nil {
				t.Fatal(e)
			}
			original[m.Path] = b
		}
	}
	r := ApplyRegistrationPruneV4(context.Background(), RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Mode: p.Mode, Authorize: true})
	if r.Status != "ok" {
		t.Fatalf("apply: %+v", r)
	}
	if _, e := os.Lstat(p.Registration.GitDir); !os.IsNotExist(e) {
		t.Fatal("registration remains", e)
	}
	if _, e := os.Lstat(p.Registration.Destination); !os.IsNotExist(e) {
		t.Fatal("checkout created", e)
	}
	manifest, digest, e := readPruneV4Payload(context.Background(), p)
	if e != nil || digest == "" || manifest.PlanDigest != p.Digest {
		t.Fatalf("payload: %+v %s %v", manifest, digest, e)
	}
	for path, want := range original {
		got, e := os.ReadFile(filepath.Join(p.PayloadPath, "files", path))
		if e != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("binary payload %s: %v", path, e)
		}
	}
	ref := "refs/stw/recovery/registration-prune/" + p.PlanID + "/head"
	if got := gitFixture(t, p.SourcePath, "rev-parse", ref); got != p.Expected.ResolvedHEAD {
		t.Fatalf("history pin %s", got)
	}
	again := ApplyRegistrationPruneV4(context.Background(), RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Mode: p.Mode, Authorize: true})
	if again.Status != "ok" {
		t.Fatalf("verified replay: %+v", again)
	}
}

func TestRegistrationPruneV4ApplyRefusesChangedEvidence(t *testing.T) {
	for _, kind := range []string{"metadata", "destination", "owner", "mode", "mount"} {
		t.Run(kind, func(t *testing.T) {
			_, p := fixturePruneV4(t)
			ctx := context.Background()
			opts := RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Mode: p.Mode, Authorize: true}
			switch kind {
			case "metadata":
				if e := os.WriteFile(filepath.Join(p.Registration.GitDir, "locked"), []byte("foreign\n"), 0600); e != nil {
					t.Fatal(e)
				}
			case "destination":
				if e := os.Mkdir(p.Registration.Destination, 0700); e != nil {
					t.Fatal(e)
				}
			case "owner":
				opts.Mode.Caller = "another-owner"
			case "mode":
				opts.Mode.Mode = "human"
			case "mount":
				ctx = context.WithValue(ctx, pruneV4MountReaderKey{}, func(int) (string, error) { return "foreign-mount", nil })
			}
			r := ApplyRegistrationPruneV4(ctx, opts)
			want := map[string]string{"metadata": "prune-lock-refused", "destination": "prune-destination-present", "owner": "prune-scope-refused", "mode": "prune-scope-refused", "mount": "prune-evidence-changed"}[kind]
			if r.Status != "refused" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != want {
				t.Fatalf("reason: want %s, %+v", want, r)
			}
			if _, e := os.Stat(p.Registration.GitDir); e != nil {
				t.Fatal("registration changed", e)
			}
		})
	}
}
