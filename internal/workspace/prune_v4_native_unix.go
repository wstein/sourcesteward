// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

func pruneV4Directory(w pruneDirectoryWitness) PruneV4Directory {
	return PruneV4Directory{w.Identity, w.UID, w.GID, w.Mode, w.Mount}
}

// observePruneV4Common binds native mount identity AND descriptor/path identity.
// This closes #273 F2 for the new planner without changing its admission-only API.
type pruneV4MountReaderKey struct{}

func observePruneV4Common(ctx context.Context, path string) (PruneV4Directory, error) {
	mountReader := pruneMountIdentity
	if reader, ok := ctx.Value(pruneV4MountReaderKey{}).(func(int) (string, error)); ok {
		mountReader = reader
	}
	var zero PruneV4Directory
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	root, id, e := openCanonical(path)
	if e != nil {
		return zero, e
	}
	defer root.Close()
	f, e := root.OpenFile(".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return zero, failure("prune-mount-unavailable", "Common-directory descriptor is unavailable.")
	}
	defer f.Close()
	a, e := pruneDirectoryFactWithMount(int(f.Fd()), path, mountReader)
	if e != nil {
		return zero, e
	}
	pruneObservationStep(ctx, "common-mount-observed", path, &a)
	b, e := pruneDirectoryFactWithMount(int(f.Fd()), path, mountReader)
	if e != nil {
		return zero, e
	}
	current, e := currentIdentity(path)
	if e != nil || id != current || a.Identity != id || a != b {
		return zero, failure("prune-mount-changed", "Common-directory mount or descriptor/path binding changed.")
	}
	return pruneV4Directory(a), nil
}

func observePruneV4Siblings(ctx context.Context, common string) ([]PruneV4Sibling, error) {
	root, _, e := openCanonical(filepath.Join(common, "worktrees"))
	if e != nil {
		return nil, e
	}
	defer root.Close()
	f, e := root.Open(".")
	if e != nil {
		return nil, e
	}
	defer f.Close()
	entries, e := f.ReadDir(v3NativeFactsLimit + 1)
	if e != nil && !errors.Is(e, io.EOF) {
		return nil, e
	}
	if len(entries) > v3NativeFactsLimit {
		return nil, failure("record-limit", "Protected registration inventory exceeds its existing native bound.")
	}
	out := []PruneV4Sibling{}
	for _, entry := range entries {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		path := filepath.Join(common, "worktrees", entry.Name())
		id, e := currentIdentity(path)
		if e != nil {
			return nil, failure("prune-sibling-refused", "Protected registration identity is unavailable or aliased.")
		}
		out = append(out, PruneV4Sibling{Name: entry.Name(), Identity: id})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func pruneV4Owner(o RegistrationPruneV4Options, source NativeIdentity) (pruneInspectionOptions, error) {
	if o.GitDir != filepath.Join(source.RepositoryDir, "worktrees", o.Name) || o.GitDir == source.GitDir {
		return pruneInspectionOptions{}, failure("prune-selection-refused", "Name and gitdir must select the same exact linked registration.")
	}
	raw, _, _, e := gitLayoutFile(filepath.Join(o.GitDir, "locked"))
	if e != nil {
		return pruneInspectionOptions{}, failure("prune-lock-refused", "Original exact stw task lock is unavailable.")
	}
	taskID, ok := strings.CutPrefix(strings.TrimSuffix(string(raw), "\n"), "stw:")
	if !ok || !validID(taskID) {
		return pruneInspectionOptions{}, failure("prune-lock-refused", "Original exact stw task lock is unavailable.")
	}
	task, found, e := ReadTask(o.StateDir, taskID)
	if e != nil || !found || task.TaskID != taskID || task.Native.GitDir != o.GitDir || !validID(task.Owner) {
		return pruneInspectionOptions{}, failure("prune-ownership-refused", "Complete original creation ownership does not match the exact selector.")
	}
	return pruneInspectionOptions{Source: o.Path, StateDir: o.StateDir, Task: taskID, Owner: task.Owner, Registrations: []string{o.Name}}, nil
}

func observePruneV4(ctx context.Context, o RegistrationPruneV4Options, initial *pruneInspectionOptions) (PruneV4Expected, PruneV4Registration, pruneInspectionOptions, error) {
	var expected PruneV4Expected
	var reg PruneV4Registration
	source, e := gitNativeIdentity(ctx, o.Path)
	if e != nil {
		return expected, reg, pruneInspectionOptions{}, e
	}
	opts, e := pruneV4Owner(o, source)
	if e != nil {
		return expected, reg, opts, e
	}
	pruneObservationStep(ctx, "v4-ownership-observed", o.GitDir, &opts)
	if initial != nil && !reflect.DeepEqual(*initial, opts) {
		return expected, reg, opts, failure("changed-ownership", "Original prune ownership changed.")
	}
	common, e := observePruneV4Common(ctx, source.RepositoryDir)
	if e != nil {
		return expected, reg, opts, e
	}
	admission, e := observePruneRegistration(ctx, opts)
	if e != nil {
		return expected, reg, opts, e
	}
	native, e := observeTidyV3Git(ctx, o.Path, source.CommitID)
	if e != nil {
		return expected, reg, opts, e
	}
	if e = qualifyPruneV4Roots(ctx, o.StateDir, source, admission.Task.Destination, native.Native); e != nil {
		return expected, reg, opts, e
	}
	siblings, e := observePruneV4Siblings(ctx, source.RepositoryDir)
	if e != nil {
		return expected, reg, opts, e
	}
	after, e := observePruneV4Common(ctx, source.RepositoryDir)
	if e != nil {
		return expected, reg, opts, e
	}
	pruneObservationStep(ctx, "v4-common-reobserved", source.RepositoryDir, &after)
	if common != after {
		return expected, reg, opts, failure("prune-mount-changed", "Common-directory mount changed during prune planning.")
	}
	taskDigest, e := v3RecordDigest(admission.Task)
	if e != nil {
		return expected, reg, opts, e
	}
	scope := "other"
	if o.Mode.Caller == admission.Task.Owner {
		scope = "own"
	}
	reg = PruneV4Registration{Name: o.Name, GitDir: o.GitDir, Destination: admission.Task.Destination, Task: admission.Task.TaskID, Owner: admission.Task.Owner, OwnerSource: "task-record", Scope: scope}
	ancestors := []PruneV4Directory{}
	for _, d := range admission.Absence.Ancestors {
		ancestors = append(ancestors, pruneV4Directory(d))
	}
	metadata := []PruneV4Metadata{}
	for _, m := range admission.Metadata {
		kind := m.Kind
		if kind == "regular" {
			kind = "file"
		}
		metadata = append(metadata, PruneV4Metadata{m.Path, kind, m.Digest, m.Physical, m.Identity, m.Mode, m.Size})
	}
	sort.Slice(metadata, func(i, j int) bool { return metadata[i].Path < metadata[j].Path })
	expected = PruneV4Expected{CreationDigest: admission.Creation.Digest, CreationJournalDigest: admission.CreationJournalDigest, TaskDigest: taskDigest, SourceSnapshot: v3SnapshotWithState(native.Expected, native.Native), Native: native.Native, Registration: admission.Registration, CommonDirMount: common, Absence: PruneV4Absence{admission.Absence.Destination, admission.Absence.Parent, ancestors}, Metadata: metadata, MetadataBytes: admission.MetadataBytes, ProtectedSiblings: siblings, ResolvedHEAD: admission.ResolvedHEAD, HeadRef: admission.HeadRef}
	pruneObservationStep(ctx, "v4-expected-observed", o.GitDir, &expected)
	pruneObservationStep(ctx, "v4-registration-observed", o.GitDir, &reg)
	return expected, reg, opts, nil
}

// PlanRegistrationPruneV4 persists one immutable plan only. It cannot create
// recovery payload/journals/refs or apply any native effect.
func PlanRegistrationPruneV4(ctx context.Context, o RegistrationPruneV4Options) RegistrationPruneV4Result {
	r := pruneV4Result()
	ctx, cancel := context.WithTimeout(ctx, planObservationTimeout)
	defer cancel()
	if e := validatePruneV4Options(o); e != nil {
		return failPruneV4(r, e)
	}
	source, e := canonicalSource(o.Path)
	if e != nil {
		return failPruneV4(r, e)
	}
	o.Path = source
	ctx, _, closeSession, e := withPlanningSession(ctx)
	if e != nil {
		return failPruneV4(r, e)
	}
	defer closeSession()
	first, reg, opts, e := observePruneV4(ctx, o, nil)
	if e != nil {
		return failPruneV4(r, e)
	}
	r.Backend = "git"
	if reg.Scope != "own" && o.Mode.Mode != "human" {
		return failPruneV4(r, failure("prune-scope-refused", "Agent prune planning requires own task-record ownership."))
	}
	id, e := randomID()
	if e != nil {
		return failPruneV4(r, e)
	}
	mode := *cloneRunMode(&o.Mode)
	sort.Strings(mode.Markers)
	sort.Strings(mode.ByOwner)
	scopes := []string{}
	if reg.Scope == "other" {
		scopes = []string{"other"}
	}
	p := RegistrationPruneV4Plan{SchemaVersion: 4, Operation: "registration-prune", PlanID: id, SourcePath: source, StateDir: o.StateDir, Backend: "git", Mode: mode, AuthorizedScopes: scopes, Registration: reg, Expected: first, PayloadPath: filepath.Join(o.StateDir, pruneV4Namespace, "payloads", id), Effects: []string{"remove-registration"}, RecoveryLimits: []string{"Planning stores evidence only; apply, recovery and restore are unavailable in this source slice.", "Absence does not prove deletion rather than a move; later metadata-only removal may require explicit restore and owner-run repair.", "No automatic recovery-payload expiry; actual recovery must be verified before any future removal.", "Checks are sampled; external or ABA races are not atomically excluded."}}
	p.Digest, e = pruneV4Digest("stw-registration-plan-v4", p, "digest")
	if e != nil {
		return failPruneV4(r, e)
	}
	if reg.Scope == "other" {
		if o.Confirm == nil {
			return failPruneV4(r, failure("prune-scope-refused", "Human other-scope planning requires typed terminal confirmation."))
		}
		if e = o.Confirm(reg.Owner, id); e != nil {
			return failPruneV4(r, failure("prune-scope-refused", "Typed owner and plan confirmation was refused."))
		}
	}
	second, reg2, _, e := observePruneV4(ctx, o, &opts)
	if e != nil {
		return failPruneV4(r, e)
	}
	if !reflect.DeepEqual(first, second) || reg != reg2 {
		return failPruneV4(r, failure("prune-evidence-changed", "Prune evidence changed before private plan persistence."))
	}
	parent, e := openStoreKinds(o.StateDir, true, []string{pruneV4Namespace})
	if e != nil {
		return failPruneV4(r, e)
	}
	defer parent.Close()
	child, e := openStoreKinds(filepath.Join(o.StateDir, pruneV4Namespace), true, []string{"plans"})
	if e != nil {
		return failPruneV4(r, e)
	}
	defer child.Close()
	fresh, reg3, _, e := observePruneV4(ctx, o, &opts)
	if e != nil {
		return failPruneV4(r, e)
	}
	if !reflect.DeepEqual(first, fresh) || reg != reg3 {
		return failPruneV4(r, failure("prune-evidence-changed", "Prune evidence changed under the private store lock."))
	}
	if e = ctx.Err(); e != nil {
		return failPruneV4(r, e)
	}
	if e = writePruneV4Plan(parent, child, p); e != nil {
		return failPruneV4(r, e)
	}
	r.Status = "ok"
	r.Plan = &p
	r.Actions = []ActionOutcome{{Name: "store-plan", Status: "complete", Detail: "Stored one contract-4 registration plan; no recovery or native effect executed."}}
	r.Limitations = append([]string{}, p.RecoveryLimits...)
	return r
}

// qualifyPruneV4Roots keeps the exact selected-absence exception local to v4.
func qualifyPruneV4Roots(ctx context.Context, state string, source NativeIdentity, destination string, native V3NativeState) error {
	if overlaps(state, source.RepositoryDir) || overlaps(state, source.WorkspaceRoot) || overlaps(state, destination) {
		return failure("state-overlap", "Private prune state overlaps protected source metadata or workspace.")
	}
	for _, path := range v3NativeRoots(native) {
		if overlaps(state, path) {
			return failure("state-overlap", "Private prune state overlaps a registered workspace.")
		}
		if path != destination {
			if e := qualifyTidyWorkspaceRoots(ctx, []string{path}, state); e != nil {
				return e
			}
		}
	}
	return nil
}
