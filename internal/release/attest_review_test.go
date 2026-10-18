// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/wstein/sourcesteward/internal/hosting"
	"os"
	"path/filepath"
	"testing"
)

func TestAttestationPinnedDescriptorMustBeRegular(t *testing.T) {
	f, err := openAttestationSubject(context.Background(), "/dev/null", fmt.Sprintf("%x", sha256.Sum256(nil)))
	if err == nil {
		f.Close()
		t.Fatal("character device accepted as attestation file")
	}
}
func TestAttestationPreparationRequiresQualifiedAbsoluteTarget(t *testing.T) {
	d := t.TempDir()
	t.Chdir(d)
	exe := filepath.Join(d, "gh")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho 'gh version 2.102.0'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_PATH", exe)
	for _, target := range []string{"", ".", "nested"} {
		if err := os.MkdirAll(filepath.Join(d, "nested"), 0700); err != nil {
			t.Fatal(err)
		}
		_, _, _, cleanup, err := hosting.PrepareGHProcess(context.Background(), target, "github.com")
		cleanup()
		if err == nil {
			t.Fatalf("unqualified target %q admitted repository verifier", target)
		}
	}
}

type qualifiedLaunchFixture struct {
	t    *testing.T
	root string
}

func (f qualifiedLaunchFixture) VerifyRelease(ctx context.Context, o VerifyOptions, _ ReleaseRecord, _ *Contract) ([]Gate, []AttestationObservation, error) {
	f.t.Helper()
	if o.Path != f.root {
		f.t.Errorf("later verifier received unqualified path %q, expected native root %q", o.Path, f.root)
	}
	_, _, _, cleanup, err := hosting.PrepareGHProcess(ctx, o.Path, "github.com")
	cleanup()
	if err == nil {
		f.t.Error("later launch admitted repository-resident executable or configuration")
	}
	return []Gate{}, []AttestationObservation{}, nil
}
func TestVerifyLaterLaunchUsesNativeRootForDefaultRelativeNestedPaths(t *testing.T) {
	for _, configuration := range []bool{false, true} {
		t.Run(fmt.Sprint("configuration=", configuration), func(t *testing.T) {
			d, dir := verificationFixture(t)
			root, err := filepath.EvalSymlinks(d)
			if err != nil {
				t.Fatal(err)
			}
			nested := filepath.Join(d, "nested")
			if err = os.Mkdir(nested, 0700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(d)
			exe := filepath.Join(d, "gh")
			if configuration {
				exe = filepath.Join(t.TempDir(), "gh")
				t.Setenv("GH_CONFIG_DIR", filepath.Join(d, "credentials"))
			}
			if err = os.WriteFile(exe, []byte("#!/bin/sh\necho 'gh version 2.102.0'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GH_PATH", exe)
			for _, path := range []string{"", ".", "nested", nested} {
				r := VerifyWithOnline(context.Background(), VerifyOptions{PlanOptions: PlanOptions{Path: path, Subject: "v0.2.0", Online: true}, Artifacts: dir}, qualifiedLaunchFixture{t, root})
				if r.Status != "ok" {
					t.Fatalf("native fixture failed for %q: %+v", path, r.Error)
				}
			}
		})
	}
}
