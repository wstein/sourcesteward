// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func assertPruneFailure(t *testing.T, err error, code string) {
	t.Helper()
	var op operationError
	if !errors.As(err, &op) || op.Code != code || op.Message == "" || strings.ContainsAny(op.Message, "\n\r\x1b") {
		t.Fatalf("want refusal %s with safe reason, got %v", code, err)
	}
}
func TestPruneAbsenceBindsCreationParentAndRechecks(t *testing.T) {
	parent := gitTempDir(t)
	expected, err := currentIdentity(parent)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "space and ünicode\nmissing")
	before, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := observePruneAbsence(context.Background(), target, expected)
	if err != nil {
		t.Fatal(err)
	}
	if witness.Destination != target || witness.Parent != expected || len(witness.Ancestors) < 2 {
		t.Fatalf("incomplete witness: %+v", witness)
	}
	if err = revalidatePruneAbsence(context.Background(), witness); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(parent)
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("read changed parent: %v", err)
	}
	if err = os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	assertPruneFailure(t, revalidatePruneAbsence(context.Background(), witness), "prune-destination-present")
}
func TestPruneAbsenceRefusesPresentAndAliasedPaths(t *testing.T) {
	for _, kind := range []string{"directory", "file", "dangling_symlink", "symlink_parent", "missing_parent", "file_parent", "unreadable_parent", "wrong_parent", "aliased_path", "relative", "nul"} {
		t.Run(kind, func(t *testing.T) {
			parent := gitTempDir(t)
			expected, err := currentIdentity(parent)
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(parent, "target")
			code := "prune-destination-present"
			switch kind {
			case "directory":
				err = os.Mkdir(target, 0700)
			case "file":
				err = os.WriteFile(target, []byte("retain"), 0600)
			case "dangling_symlink":
				err = os.Symlink("missing", target)
			case "symlink_parent":
				link := filepath.Join(gitTempDir(t), "alias")
				err = os.Symlink(parent, link)
				target = filepath.Join(link, "target")
				expected.Path = link
				code = "prune-parent-unavailable"
			case "missing_parent":
				target = filepath.Join(parent, "missing", "target")
				expected.Path = filepath.Dir(target)
				code = "prune-parent-unavailable"
			case "file_parent":
				err = os.WriteFile(filepath.Join(parent, "file"), []byte("retain"), 0600)
				target = filepath.Join(parent, "file", "target")
				expected.Path = filepath.Dir(target)
				code = "prune-parent-unavailable"
			case "unreadable_parent":
				err = os.Chmod(parent, 0000)
				t.Cleanup(func() { _ = os.Chmod(parent, 0700) })
				code = "prune-parent-unavailable"
			case "wrong_parent":
				expected.Inode++
				code = "prune-parent-changed"
			case "aliased_path":
				target = parent + "/./target"
				code = "prune-path-refused"
			case "relative":
				target = "relative"
				code = "prune-path-refused"
			case "nul":
				target = parent + "/bad\x00"
				code = "prune-path-refused"
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = observePruneAbsence(context.Background(), target, expected)
			assertPruneFailure(t, err, code)
		})
	}
}
func TestPruneAbsenceRetainsChangedParentOrMountEvidence(t *testing.T) {
	parent := filepath.Join(gitTempDir(t), "parent")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	expected, err := currentIdentity(parent)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := observePruneAbsence(context.Background(), filepath.Join(parent, "missing"), expected)
	if err != nil {
		t.Fatal(err)
	}
	changed := witness
	changed.Ancestors = append([]pruneDirectoryWitness(nil), witness.Ancestors...)
	changed.Ancestors[len(changed.Ancestors)-1].Mount += "changed"
	assertPruneFailure(t, revalidatePruneAbsence(context.Background(), changed), "prune-absence-changed")
	if err = os.Rename(parent, parent+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	assertPruneFailure(t, revalidatePruneAbsence(context.Background(), witness), "prune-parent-changed")
}
func TestPruneAbsenceCanceledObservationDoesNotSucceed(t *testing.T) {
	parent := gitTempDir(t)
	expected, err := currentIdentity(parent)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = observePruneAbsence(ctx, filepath.Join(parent, "missing"), expected)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled absence: %v", err)
	}
}
func fixturePruneObservation(t *testing.T) (pruneInspectionOptions, MutationPlan, TaskRecord, string) {
	t.Helper()
	source, creation, task := gitRetirementFixture(t)
	moved := task.Destination + "-moved"
	if err := os.Rename(task.Destination, moved); err != nil {
		t.Fatal(err)
	}
	return pruneInspectionOptions{Source: source, StateDir: creation.StateDir, Task: task.TaskID, Owner: task.Owner, Registrations: []string{filepath.Base(task.Native.GitDir)}}, creation, task, moved
}
func TestPruneObservationReadsExactMissingLockedRegistration(t *testing.T) {
	opts, creation, task, moved := fixturePruneObservation(t)
	before, err := os.ReadFile(filepath.Join(task.Native.GitDir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	observation, err := observePruneRegistration(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Task.TaskID != task.TaskID || observation.Creation.Digest != creation.Digest || observation.CreationJournalDigest == "" || observation.Registration.Path != task.Native.GitDir || observation.Absence.Destination != task.Destination || observation.MetadataBytes < int64(len(before)) {
		t.Fatalf("missing ownership/payload witness: %+v", observation)
	}
	found := false
	for _, entry := range observation.Metadata {
		if entry.Path == "index" {
			found = true
			if entry.Digest != gitDigest(string(before)) || entry.Size != int64(len(before)) {
				t.Fatal("binary index not bound")
			}
		}
	}
	if !found {
		t.Fatal("index absent from manifest")
	}
	after, err := os.ReadFile(filepath.Join(task.Native.GitDir, "index"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("metadata changed: %v", err)
	}
	if _, err = os.Stat(moved); err != nil {
		t.Fatal("moved checkout was touched", err)
	}
	again, err := observePruneRegistration(context.Background(), opts)
	if err != nil || !reflect.DeepEqual(observation, again) {
		t.Fatalf("unstable observation: %v", err)
	}
}
func TestPruneObservationRefusalReasons(t *testing.T) {
	for _, kind := range []string{"unnamed", "two_names", "prefix", "foreign_lock", "unlocked", "present", "changed_gitdir", "changed_commondir", "unknown_metadata", "metadata_symlink", "metadata_hardlink", "wrong_owner", "wrong_repository", "unlocked_missing_sibling"} {
		t.Run(kind, func(t *testing.T) {
			opts, _, task, moved := fixturePruneObservation(t)
			code := "prune-registration-refused"
			var err error
			switch kind {
			case "unnamed":
				opts.Registrations = nil
				code = "prune-selection-refused"
			case "two_names":
				opts.Registrations = append(opts.Registrations, "other")
				code = "prune-selection-refused"
			case "prefix":
				opts.Registrations[0] = opts.Registrations[0][:len(opts.Registrations[0])-1]
			case "foreign_lock":
				err = os.WriteFile(filepath.Join(task.Native.GitDir, "locked"), []byte("stw:other\n"), 0600)
				code = "prune-lock-refused"
			case "unlocked":
				err = os.Remove(filepath.Join(task.Native.GitDir, "locked"))
				code = "prune-lock-refused"
			case "present":
				err = os.Rename(moved, task.Destination)
				code = "prune-destination-present"
			case "changed_gitdir":
				err = os.WriteFile(filepath.Join(task.Native.GitDir, "gitdir"), []byte(moved+"/.git\n"), 0600)
				code = "prune-pointer-refused"
			case "changed_commondir":
				err = os.WriteFile(filepath.Join(task.Native.GitDir, "commondir"), []byte("../../elsewhere\n"), 0600)
				code = "prune-pointer-refused"
			case "unknown_metadata":
				err = os.WriteFile(filepath.Join(task.Native.GitDir, "unknown"), []byte("retain"), 0600)
				code = "prune-metadata-refused"
			case "metadata_symlink":
				if err = os.Remove(filepath.Join(task.Native.GitDir, "ORIG_HEAD")); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				err = os.Symlink("HEAD", filepath.Join(task.Native.GitDir, "ORIG_HEAD"))
				code = "prune-metadata-refused"
			case "metadata_hardlink":
				if err = os.Remove(filepath.Join(task.Native.GitDir, "ORIG_HEAD")); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				err = os.Link(filepath.Join(task.Native.GitDir, "HEAD"), filepath.Join(task.Native.GitDir, "ORIG_HEAD"))
				code = "prune-metadata-refused"
			case "wrong_owner":
				opts.Owner = "other"
				code = "invalid-ownership"
			case "wrong_repository":
				opts.Source = fixtureGit(t)
				code = "prune-repository-refused"
			case "unlocked_missing_sibling":
				sibling := filepath.Join(gitTempDir(t), "sibling")
				gitFixture(t, opts.Source, "worktree", "add", "--detach", sibling, "HEAD")
				err = os.RemoveAll(sibling)
				code = "prune-sibling-refused"
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = observePruneRegistration(context.Background(), opts)
			assertPruneFailure(t, err, code)
		})
	}
}

func TestPruneAbsenceRequiresExactENOENT(t *testing.T) {
	if err := pruneMissingLeafError(unix.ENOENT); err != nil {
		t.Fatal(err)
	}
	assertPruneFailure(t, pruneMissingLeafError(nil), "prune-destination-present")
	for _, err := range []error{unix.EACCES, unix.EPERM, unix.ENOTDIR, unix.EIO, unix.ESTALE, context.Canceled, fmt.Errorf("unexpected wrapped error: %w", unix.ENOENT)} {
		assertPruneFailure(t, pruneMissingLeafError(err), "prune-absence-unavailable")
	}
}
