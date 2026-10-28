//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2

package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func admissionGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func canonicalTemp(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// The physical owner is the effective user: the same directory is refused when the owner function
// stands for another user (an injected owner function, no real chown).
func TestRetirementLocationAdmissionRequiresTheOwnerOfTheDirectories(t *testing.T) {
	parent := canonicalTemp(t)
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(parent, "wt")
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	if err := RetirementLocationAdmission(dest); err != nil {
		t.Fatalf("a directory of the user in a directory of the user: %v", err)
	}
	old := physicalOwnerUID
	t.Cleanup(func() { physicalOwnerUID = old })
	physicalOwnerUID = func() uint32 { return uint32(os.Geteuid()) + 1 }
	if err := RetirementLocationAdmission(dest); err == nil || !strings.Contains(err.Error(), "foreign") {
		t.Errorf("another owner must be refused, got %v", err)
	}
	if err := RetirementParentAdmission(parent); err == nil {
		t.Error("the parent of another owner must be refused")
	}
	if err := RetirementDestinationAdmission(dest); err == nil {
		t.Error("the destination of another owner must be refused")
	}
}

// A real directory of another owner (root): /tmp is one where the tests run as an ordinary user.
func TestRetirementParentAdmissionRefusesTheSharedTemporaryDirectory(t *testing.T) {
	var st syscall.Stat_t
	if err := syscall.Stat("/tmp", &st); err != nil || int(st.Uid) == os.Geteuid() {
		t.Skip("/tmp belongs to the current user here")
	}
	if err := RetirementParentAdmission("/tmp"); err == nil {
		t.Error("/tmp is owned by another user and must be refused as the parent of a worktree")
	}
}

func TestRetirementParentAdmissionRefusesASymbolicLinkSibling(t *testing.T) {
	parent := canonicalTemp(t)
	if err := os.Symlink("/nonexistent", filepath.Join(parent, "link")); err != nil {
		t.Fatal(err)
	}
	if err := RetirementParentAdmission(parent); err == nil {
		t.Error("a symbolic link next to the worktree must be refused (retirement-sibling-refused)")
	}
}

func admissionRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(canonicalTemp(t), "repo")
	if err := os.Mkdir(repo, 0755); err != nil {
		t.Fatal(err)
	}
	admissionGit(t, repo, "init", "-q", "-b", "main")
	admissionGit(t, repo, "commit", "-q", "--allow-empty", "-m", "base")
	return repo
}

// RefusedRegistration agrees with qualifyTidyV3Registrations for every shape of registration.
func TestRefusedRegistrationAgreesWithTheQualificationOfThePlans(t *testing.T) {
	root := canonicalTemp(t)
	present := filepath.Join(root, "present")
	if err := os.Mkdir(present, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(present, alias); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "missing")
	orphan := filepath.Join(root, "no-parent", "missing")
	cases := []struct {
		name    string
		reg     RegistrationFact
		refused bool
		code    string
	}{
		{"present", RegistrationFact{Path: present}, false, ""},
		{"prunable, unlocked, absent below a real parent", RegistrationFact{Path: missing, Prunable: true}, false, ""},
		{"locked and absent", RegistrationFact{Path: missing, Prunable: true, Locked: true}, true, AdmissionUnknownWorkspacePath},
		{"absent but not prunable", RegistrationFact{Path: missing}, true, AdmissionUnknownWorkspacePath},
		{"prunable with a parent that does not exist", RegistrationFact{Path: orphan, Prunable: true}, true, AdmissionUnknownWorkspacePath},
		{"symbolic link alias", RegistrationFact{Path: alias}, true, AdmissionUnknownWorkspacePath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, refused := RefusedRegistration(context.Background(), []RegistrationFact{{Path: present}, tc.reg}, "")
			native := V3NativeState{Registrations: []V3Registration{{Path: present}, {Path: tc.reg.Path, Locked: tc.reg.Locked, Prunable: tc.reg.Prunable, Bare: tc.reg.Bare}}}
			planErr := qualifyTidyV3Registrations(context.Background(), native, nil, "")
			if refused != tc.refused || (planErr != nil) != tc.refused {
				t.Fatalf("admission refused=%t plan error=%v, want refused=%t", refused, planErr, tc.refused)
			}
			if refused && (got.Code != tc.code || got.Path != tc.reg.Path || got.Locked != tc.reg.Locked) {
				t.Errorf("%+v, want code %s path %s", got, tc.code, tc.reg.Path)
			}
		})
	}
	t.Run("state directory overlap", func(t *testing.T) {
		state := filepath.Join(present, "state")
		got, refused := RefusedRegistration(context.Background(), []RegistrationFact{{Path: present}}, state)
		if !refused || got.Code != AdmissionStateOverlap {
			t.Errorf("%+v %t", got, refused)
		}
		if err := qualifyTidyV3Registrations(context.Background(), V3NativeState{Registrations: []V3Registration{{Path: present}}}, nil, state); err == nil {
			t.Error("the plan must refuse the same overlap")
		}
	})
	t.Run("a cancelled context is refused, not admitted", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, refused := RefusedRegistration(ctx, []RegistrationFact{{Path: present}}, ""); !refused {
			t.Error("a cancelled check must not admit")
		}
	})
}
