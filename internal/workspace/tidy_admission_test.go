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

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// DryRun runs the whole plan and writes nothing: a refusal is the same as without it, a plan that
// passes is returned and the state directory is untouched.
func TestPlanTidyV3DryRunWritesNothing(t *testing.T) {
	repo := admissionRepo(t)
	admissionGit(t, repo, "switch", "-q", "-c", "done")
	admissionGit(t, repo, "commit", "-q", "--allow-empty", "-m", "work")
	admissionGit(t, repo, "switch", "-q", "main")
	admissionGit(t, repo, "merge", "-q", "--no-ff", "-m", "merge", "done")
	state := filepath.Join(canonicalTemp(t), "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	before := listFiles(t, state)
	o := V3PlanOptions{Path: repo, Base: "refs/heads/main", StateDir: state, Refs: []string{"refs/heads/done"}, ProtectWorktrees: []string{}, DryRun: true}
	res := PlanTidyV3(context.Background(), o)
	if res.Status != "ok" || res.Plan == nil || len(res.Plan.Candidates) != 1 || res.Plan.Candidates[0].Decision != "delete" {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
	if after := listFiles(t, state); strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("a dry run wrote to the state directory: %v", after)
	}
	o.DryRun = false
	if real := PlanTidyV3(context.Background(), o); real.Status != "ok" || len(listFiles(t, filepath.Join(state, "tidy-v3-plans"))) < 2 {
		t.Errorf("the real plan must store the plan: %+v", real.Status)
	}
	// The refusal is the same: an unknown reference is refused in a dry run too.
	o.DryRun, o.Refs = true, []string{"refs/heads/nope"}
	if res := PlanTidyV3(context.Background(), o); res.Status == "ok" {
		t.Errorf("a dry run must refuse what the plan refuses: %+v", res.Status)
	}
}

// DryRun of a legacy retirement reaches the physical checks (here: the owner) and stores nothing.
func TestPlanTidyV3DryRunOfARetirementChecksThePlaceAndWritesNothing(t *testing.T) {
	repo := admissionRepo(t)
	root := canonicalTemp(t)
	parent := filepath.Join(root, "wts")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(parent, "wt")
	admissionGit(t, repo, "worktree", "add", "-q", "--detach", dest, "main")
	state := filepath.Join(root, "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	before := listFiles(t, state)
	o := V3PlanOptions{Path: repo, Base: "refs/heads/main", StateDir: state, CompletedWorktree: dest, Owner: "codex", RemoveRegistration: true, RemoveFiles: dest, ProtectWorktrees: []string{}, DryRun: true}
	res := PlanTidyV3(context.Background(), o)
	if res.Status != "ok" || res.Plan == nil {
		t.Fatalf("%+v %+v", res.Status, res.Diagnostics)
	}
	if after := listFiles(t, state); strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("a dry run wrote to the state directory: %v", after)
	}
	old := physicalOwnerUID
	t.Cleanup(func() { physicalOwnerUID = old })
	physicalOwnerUID = func() uint32 { return uint32(os.Geteuid()) + 1 }
	if res := PlanTidyV3(context.Background(), o); res.Status == "ok" {
		t.Error("a dry run must meet the physical owner check of the plan")
	}
}
