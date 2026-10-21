// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// Admission is evidence only. It neither persists a completion claim nor
// creates a task/creation journal, acquires a task lock, or removes anything.
// The operation coordinator owns the original deadline and operation ledger.
type tidyV3LegacyObservation struct {
	Native               tidyV3NativeObservation
	Retirement           V3Retirement
	RegistrationParent   V3PhysicalIdentity
	RegistrationSiblings []V3PhysicalIdentity
}

func retirementV3Directory(path string) (V3PhysicalIdentity, error) {
	root, _, e := openCanonical(path)
	if e != nil {
		return V3PhysicalIdentity{}, e
	}
	defer root.Close()
	st, e := root.Stat(".")
	if e != nil {
		return V3PhysicalIdentity{}, e
	}
	id, e := retirementV3Physical(path, st)
	if e != nil {
		return id, e
	}
	if id.Kind != "directory" {
		return id, errors.New("retirement root is not a directory")
	}
	return id, nil
}

func retirementV3NoOperation(ctx context.Context, id NativeIdentity) error {
	seen := map[string]bool{}
	for _, root := range []string{id.GitDir, id.RepositoryDir} {
		if seen[root] {
			continue
		}
		seen[root] = true
		if e := ctx.Err(); e != nil {
			return e
		}
		for _, name := range []string{"MERGE_HEAD", "REBASE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG", "rebase-merge", "rebase-apply", "sequencer", "index.lock", "HEAD.lock", "packed-refs.lock"} {
			if _, e := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(e) {
				return failure("active-workspace", "Ongoing native operation or lock metadata prevents retirement.")
			}
		}
		if e := retirementV3RefLocks(ctx, filepath.Join(root, "refs")); e != nil {
			return e
		}
	}
	return nil
}

// Only the selected Git v3 registration vocabulary can be read as metadata.
// Missing mandatory files and unknown/special nodes refuse; optional native
// files remain optional rather than being fabricated for a legacy worktree.
func retirementV3RegistrationAdmission(path string, policy retirementContentPolicy) (retirementManifestAdmission, error) {
	return retirementV3RegistrationAdmissionMode(path, policy, false)
}
func retirementV3RegistrationAdmissionMode(path string, policy retirementContentPolicy, owned bool) (retirementManifestAdmission, error) {
	a := retirementManifestAdmission{Backend: "git", NativeMetadataPaths: []string{}}
	paths, e := gitRetirementV3MetadataPaths(policy)
	if e != nil {
		return a, e
	}
	for _, name := range paths {
		_, e := os.Lstat(filepath.Join(path, name))
		if e == nil {
			a.NativeMetadataPaths = append(a.NativeMetadataPaths, name)
		} else if !os.IsNotExist(e) {
			return a, e
		}
	}
	for _, name := range []string{"HEAD", "commondir", "gitdir", "index"} {
		if !containsString(a.NativeMetadataPaths, name) {
			return a, failure("invalid-evidence", "Required legacy registration metadata is missing.")
		}
	}
	if !owned && containsString(a.NativeMetadataPaths, "locked") {
		return a, failure("active-workspace", "Locked legacy worktrees are retained; completion never unlocks them.")
	}
	return a, nil
}

func retirementV3Siblings(parent, target string) ([]V3PhysicalIdentity, error) {
	id, e := currentIdentity(parent)
	if e != nil {
		return nil, e
	}
	facts, e := gitRetirementSiblings([]PathIdentity{id})
	if e != nil {
		return nil, e
	}
	out := []V3PhysicalIdentity{}
	for _, f := range facts {
		if f.Identity.Path == target {
			continue
		}
		st, e := os.Lstat(f.Identity.Path)
		if e != nil {
			return nil, e
		}
		physical, e := retirementV3Physical(f.Identity.Path, st)
		if e != nil {
			return nil, e
		}
		if physical.Kind != "directory" && physical.Kind != "regular" {
			return nil, failure("unsupported-workspace", "Retirement parent siblings require supported owned directory or single-link regular identities.")
		}
		if physical.Device != f.Identity.Device || physical.Inode != f.Identity.Inode {
			return nil, failure("evidence-changed", "Retirement sibling changed during observation.")
		}
		out = append(out, physical)
	}
	return out, nil
}

type tidyV3LegacyObserver struct {
	source func(context.Context, string, string) (tidyV3NativeObservation, error)
	target func(context.Context, string) (NativeIdentity, error)
}

func observeTidyV3Legacy(ctx context.Context, o V3PlanOptions, budget *tidyV3ProofBudget) (tidyV3LegacyObservation, error) {
	return (tidyV3LegacyObserver{source: observeTidyV3Git, target: gitNativeIdentity}).observe(ctx, o, budget)
}

func (c tidyV3LegacyObserver) observe(ctx context.Context, o V3PlanOptions, budget *tidyV3ProofBudget) (tidyV3LegacyObservation, error) {
	if o.Task != "" {
		return tidyV3LegacyObservation{}, failure("completion-required", "Legacy completion cannot replace original owned creation records.")
	}
	return c.observeWithOwnership(ctx, o, budget, nil)
}
func (c tidyV3LegacyObserver) observeWithOwnership(ctx context.Context, o V3PlanOptions, budget *tidyV3ProofBudget, owned *V3OwnedProvenance) (tidyV3LegacyObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, v3Timeout(ctx, planObservationTimeout))
	defer cancel()
	out := tidyV3LegacyObservation{}
	if e := ctx.Err(); e != nil {
		return out, e
	}
	if e := validateV3PlanOptions(o); e != nil {
		return out, e
	}
	if owned == nil && (o.CompletedWorktree == "" || o.Task != "") {
		return out, failure("completion-required", "Select one exact completed legacy worktree with current exclusive retirement authority.")
	}
	source, e := canonicalSource(o.Path)
	if e != nil {
		return out, e
	}
	if source != o.Path {
		return out, failure("invalid-input", "Legacy admission requires an exact canonical source root.")
	}
	dest := o.CompletedWorktree
	if owned != nil {
		dest = owned.Task.Destination
	}
	if v3Overlaps(o.Path, dest) || dest == string(filepath.Separator) {
		return out, failure("active-workspace", "The source/current/primary or overlapping workspace is protected.")
	}
	cwd, ancestors, e := retirementV3CurrentDirectory(ctx)
	if e != nil {
		return out, e
	}
	if cwd == dest || strings.HasPrefix(cwd, dest+string(filepath.Separator)) {
		return out, failure("active-workspace", "The physical current working directory is inside the selected target.")
	}
	for _, root := range o.ProtectWorktrees {
		if v3Overlaps(root, dest) {
			return out, failure("active-workspace", "A known active protection overlaps the completed target.")
		}
	}
	if e = budget.beginObservation(); e != nil {
		return out, e
	}
	before, e := c.source(ctx, o.Path, o.Base)
	if e != nil {
		return out, e
	}
	if before.Expected.Identity.WorkspaceRoot != o.Path {
		return out, failure("invalid-input", "Source must be the exact checkout root, not an interior directory.")
	}
	for _, metadataRoot := range []string{before.Expected.Identity.RepositoryDir, before.Expected.Identity.GitDir} {
		if v3Overlaps(o.StateDir, metadataRoot) {
			return out, failure("state-overlap", "Legacy state must be disjoint from every native metadata root, including separate Git directories.")
		}
	}
	before.Native.ProtectedRoots = append([]string{}, o.ProtectWorktrees...)
	before.Expected = v3SnapshotWithState(before.Expected, before.Native)
	out.Native = before
	roots := v3NativeRoots(before.Native)
	if e = qualifyTidyWorkspaceRoots(ctx, append(append([]string{}, roots...), o.ProtectWorktrees...), o.StateDir); e != nil {
		return out, e
	}
	sourceIdentity, e := retirementV3Directory(o.Path)
	if e != nil {
		return out, e
	}
	commonIdentity, e := retirementV3Directory(before.Expected.Identity.RepositoryDir)
	if e != nil {
		return out, e
	}
	found := false
	var registration V3Registration
	for _, w := range before.Native.Registrations {
		if w.Path == dest {
			found = true
			registration = w
		} else if v3Overlaps(w.Path, dest) {
			return out, failure("active-workspace", "Another registered workspace overlaps the selected target.")
		}
	}
	if !found || registration.Bare || registration.Prunable || (owned == nil && registration.Locked) || (owned != nil && (!registration.Locked || registration.LockReason != owned.LockReason)) {
		return out, failure("active-workspace", "The exact ordinary unlocked legacy registration is unavailable.")
	}
	target, e := c.target(ctx, dest)
	if e != nil {
		return out, e
	}
	if target.WorkspaceRoot != dest || target.RepositoryDir != before.Expected.Identity.RepositoryDir || target.ObjectFormat != before.Expected.Identity.ObjectFormat || target.GitDir == target.RepositoryDir || filepath.Dir(target.GitDir) != filepath.Join(target.RepositoryDir, "worktrees") || target.CommitID != registration.HeadOID || target.HeadRef != registration.HeadRef {
		return out, failure("invalid-evidence", "Target identity does not bind an exact linked registration in the source repository.")
	}
	if owned != nil {
		verified, e := (gitAdapter{}).Verify(ctx, owned.CreationPlan)
		if e != nil || !verified.Complete || verified.Identity != target || !sameOwner(target, owned.Task.Native) || target.ObjectFormat != owned.Task.Native.ObjectFormat || target.HeadRef != "" {
			return out, failure("changed-ownership", "Exact original stable registration, format and task lock must bind the current detached target.")
		}
	}
	if e = retirementV3NoOperation(ctx, before.Expected.Identity); e != nil {
		return out, e
	}
	if e = retirementV3NoOperation(ctx, target); e != nil {
		return out, e
	}
	destination, e := retirementV3Directory(dest)
	if e != nil {
		return out, e
	}
	if owned != nil && owned.Task.DestinationIdentity != (PathIdentity{Path: dest, Device: destination.Device, Inode: destination.Inode}) {
		return out, failure("changed-ownership", "Original owned destination physical identity changed.")
	}
	if destination.Device == sourceIdentity.Device && destination.Inode == sourceIdentity.Inode {
		return out, failure("active-workspace", "Source and destination have aliased physical identities.")
	}
	for _, ancestor := range ancestors {
		if ancestor.Device == destination.Device && ancestor.Inode == destination.Inode {
			return out, failure("active-workspace", "The selected target is a physical ancestor of the current directory.")
		}
	}
	parent, e := retirementV3Directory(filepath.Dir(dest))
	if e != nil {
		return out, e
	}
	regIdentity, e := retirementV3Directory(target.GitDir)
	if e != nil {
		return out, e
	}
	regParent, e := retirementV3Directory(filepath.Dir(target.GitDir))
	if e != nil {
		return out, e
	}
	out.RegistrationParent = regParent
	bin, e := gitExecutable()
	if e != nil {
		return out, e
	}
	// Retain the existing bounded layout/config/filter admission. In particular
	// this verifies reciprocal .git/gitdir/commondir and does not trust inventory.
	safety, e := gitSafety(ctx, bin, target, target.CommitID)
	if e != nil {
		return out, e
	}
	policy, e := retirementPolicy("git", 3)
	if e != nil {
		return out, e
	}
	stored, e := gitRetirementStoredFilesWithPolicy(ctx, bin, target, policy, budget)
	if e != nil {
		return out, e
	}
	diskAdmission := retirementManifestAdmission{Backend: "git", NativeMetadataPaths: []string{".git"}, StoredFiles: stored}
	disk, e := observeRetirementV3Manifest(ctx, dest, diskAdmission, false, policy, budget)
	if e != nil {
		return out, e
	}
	metadataAdmission, e := retirementV3RegistrationAdmissionMode(target.GitDir, policy, owned != nil)
	if e != nil {
		return out, e
	}
	metadata, e := observeRetirementV3Manifest(ctx, target.GitDir, metadataAdmission, true, policy, budget)
	if e != nil {
		return out, e
	}
	siblings, e := retirementV3Siblings(parent.Path, dest)
	if e != nil {
		return out, e
	}
	out.RegistrationSiblings, e = retirementV3Siblings(regParent.Path, target.GitDir)
	if e != nil {
		return out, e
	}
	paths := []string{}
	for _, w := range before.Native.Registrations {
		if w.HeadOID == target.CommitID {
			paths = append(paths, w.Path)
		}
	}
	proof, e := proveTidyV3Commit(ctx, before.Expected, target.CommitID, paths, nil, nil, func(ctx context.Context, target, base string) (bool, error) {
		return tidyV3NativeAncestry(ctx, before.Expected, target, base)
	})
	if e != nil {
		return out, e
	}
	// An initialized submodule is data this retirement never opens or removes:
	// retain rather than delete the worktree (Spec 031 FR-021).
	if proof.Decision == "delete" && submodulesInitialized(before.Config, target.RepositoryDir) {
		proof = V3CommitProof{OID: proof.OID, Decision: "retain", Reason: "submodule-present", Proof: "none", WorkspacePaths: proof.WorkspacePaths}
	}
	r := V3Retirement{ActionID: "retirement-" + gitDigest(dest)[:32], RegisteredRoots: roots, ProtectedRoots: append([]string{}, o.ProtectWorktrees...), Destination: destination, Parent: parent, Registration: regIdentity, RegistrationParent: regParent, RegistrationSiblings: out.RegistrationSiblings, Target: target, RemoveRegistration: true, RemoveFiles: dest, ContentPolicy: "git-v3", Disk: disk, Metadata: metadata, Siblings: siblings, Integration: proof}
	r.Provenance = V3Provenance{Kind: "legacy-completion", Legacy: &V3LegacyProvenance{Owner: o.Owner, Completed: true, SameUserAuthority: true, SelectedPath: dest, ProtectedRoots: append([]string{}, o.ProtectWorktrees...), ObservedIdentity: destination}}
	if owned != nil {
		r.Provenance = V3Provenance{Kind: "owned", Owned: owned}
	}
	r.Actions = []V3RetirementAction{{Name: "remove-registration", Target: dest, Decision: "retain", Reason: proof.Reason, Blockers: []string{}}, {Name: "remove-files", Target: dest, Decision: "retain", Reason: proof.Reason, Blockers: []string{}}}
	if proof.Decision == "delete" {
		r.Actions[0].Decision = "delete"
		r.Actions[1].Decision = "delete"
		r.Actions[0].Reason = "integrated"
		r.Actions[1].Reason = "clean"
	}
	// Fresh complete disk and registration manifests are required inside this
	// sample, as are unchanged native bindings and physical parent/root facts.
	finalTarget, e := c.target(ctx, dest)
	if e != nil || finalTarget != target {
		return out, failure("evidence-changed", "Target HEAD/ref/native identity changed during admission.")
	}
	finalSafety, e := gitSafety(ctx, bin, finalTarget, finalTarget.CommitID)
	if e != nil {
		return out, e
	}
	if finalSafety != safety {
		return out, failure("evidence-changed", "Target layout/configuration/safety changed during admission.")
	}
	after, e := c.source(ctx, o.Path, o.Base)
	if e != nil {
		return out, e
	}
	after.Native.ProtectedRoots = append([]string{}, o.ProtectWorktrees...)
	after.Expected = v3SnapshotWithState(after.Expected, after.Native)
	if !sameTidyV3Observation(before, after) {
		return out, failure("evidence-changed", "Source/reference/registration/configuration bindings changed during admission.")
	}
	if e = verifyRecordedRetirementV3Manifest(ctx, dest, diskAdmission, false, policy, budget, disk); e != nil {
		return out, e
	}
	if e = verifyRecordedRetirementV3Manifest(ctx, target.GitDir, metadataAdmission, true, policy, budget, metadata); e != nil {
		return out, e
	}
	for _, want := range []V3PhysicalIdentity{sourceIdentity, commonIdentity, destination, parent, regIdentity, regParent} {
		got, e := retirementV3Directory(want.Path)
		if e != nil || got != want {
			return out, failure("evidence-changed", "A source/common/destination/parent/registration physical identity changed.")
		}
	}
	freshSiblings, e := retirementV3Siblings(parent.Path, dest)
	if e != nil {
		return out, e
	}
	if !reflect.DeepEqual(freshSiblings, siblings) {
		return out, failure("evidence-changed", "Destination parent siblings changed during admission.")
	}
	freshRegistrationSiblings, e := retirementV3Siblings(regParent.Path, target.GitDir)
	if e != nil {
		return out, e
	}
	if !reflect.DeepEqual(freshRegistrationSiblings, out.RegistrationSiblings) {
		return out, failure("evidence-changed", "Registration parent siblings changed during admission.")
	}
	if e = retirementV3NoOperation(ctx, target); e != nil {
		return out, e
	}
	if e = retirementV3NoOperation(ctx, before.Expected.Identity); e != nil {
		return out, e
	}
	_, currentAncestors, e := retirementV3CurrentDirectory(ctx)
	if e != nil {
		return out, e
	}
	for _, ancestor := range currentAncestors {
		if ancestor.Device == destination.Device && ancestor.Inode == destination.Inode {
			return out, failure("active-workspace", "The current physical directory entered the selected target during admission.")
		}
	}
	if owned != nil {
		fresh, e := readTidyV3OwnedProvenance(o)
		if e != nil || !reflect.DeepEqual(fresh, *owned) {
			return out, failure("changed-ownership", "Original creation journal/task evidence changed during observation.")
		}
		verified, e := (gitAdapter{}).Verify(ctx, owned.CreationPlan)
		if e != nil || !verified.Complete || verified.Identity != target {
			return out, failure("changed-ownership", "Original owned native binding changed during observation.")
		}
	}
	if e = ctx.Err(); e != nil {
		return out, e
	}
	// Consume the shared observer's proven primary checkout binding, then
	// retain the complete central source/target/protection and record guards.
	plan := V3Plan{SourcePath: o.Path, StateDir: o.StateDir, Expected: before.Expected, Mode: "legacy-retirement", Retirement: &r, SelectedRefs: []string{}, Candidates: []V3Candidate{}, Effects: []string{}, RecoveryLimits: v3RecoveryLimits()}
	if owned != nil {
		plan.Mode = "owned-retirement"
	}
	if !validV3Retirement(r, plan) || !validV3NativeState(before.Native, plan, plan.Expected) {
		return out, failure("invalid-evidence", "Complete native registrations must bind the exact source/target/protection and physical retirement model; unproven or inconsistent primary mappings refuse.")
	}
	if e = v3Bounds(v3StoredPlan{Plan: plan, Native: before.Native}); e != nil {
		return out, e
	}
	out.Retirement = r
	return out, nil
}

// Lock observation is a separate metadata control check, never a user-content
// pass. Bound the complete traversal by the existing observation-entry/path
// limits and caller deadline; lock bytes are never opened or changed.
func retirementV3RefLocks(ctx context.Context, path string) error {
	st, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return failure("active-workspace", "Loose reference lock evidence is not a canonical directory.")
	}
	initial, e := retirementV3Physical(path, st)
	if e != nil {
		return e
	}
	root, _, e := openCanonical(path)
	if e != nil {
		return e
	}
	defer root.Close()
	opened, e := root.Stat(".")
	if e != nil {
		return e
	}
	openedID, e := retirementV3Physical(path, opened)
	if e != nil || openedID != initial {
		return failure("evidence-changed", "Loose reference root changed while opening.")
	}
	count := 0
	var walk func(*os.Root, string) error
	walk = func(dir *os.Root, relative string) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		before, e := dir.Stat(".")
		if e != nil {
			return e
		}
		id, e := retirementV3Physical(filepath.Join(path, relative), before)
		if e != nil {
			return e
		}
		f, e := dir.Open(".")
		if e != nil {
			return e
		}
		names, e := f.Readdirnames(observationLimit - count + 1)
		f.Close()
		if e != nil && !errors.Is(e, io.EOF) {
			return e
		}
		count += len(names)
		if count > observationLimit {
			return failure("record-limit", "Loose reference lock observation exceeds its complete entry bound.")
		}
		for _, name := range names {
			if e := ctx.Err(); e != nil {
				return e
			}
			rel := filepath.Join(relative, name)
			if count > observationLimit || len(rel) > 4096 || !retirementLiteralPath(rel) {
				return failure("record-limit", "Loose reference lock observation exceeds its complete entry/path bound.")
			}
			if strings.HasSuffix(name, ".lock") {
				return failure("active-workspace", "An active loose-reference update lock prevents retirement.")
			}
			st, e := dir.Lstat(name)
			if e != nil {
				return e
			}
			node, e := retirementV3Physical(filepath.Join(path, rel), st)
			if e != nil {
				return e
			}
			if node.Kind == "directory" {
				child, e := dir.OpenRoot(name)
				if e != nil {
					return e
				}
				opened, e := child.Stat(".")
				if e != nil {
					child.Close()
					return e
				}
				current, e := retirementV3Physical(node.Path, opened)
				if e != nil || current != node {
					child.Close()
					return failure("evidence-changed", "Loose reference directory changed while opening.")
				}
				e = walk(child, rel)
				child.Close()
				if e != nil {
					return e
				}
				final, e := dir.Lstat(name)
				if e != nil {
					return e
				}
				current, e = retirementV3Physical(node.Path, final)
				if e != nil || current != node {
					return failure("evidence-changed", "Loose reference directory changed during lock observation.")
				}
			}
		}
		after, e := dir.Stat(".")
		if e != nil {
			return e
		}
		current, e := retirementV3Physical(id.Path, after)
		if e != nil || current != id {
			return failure("evidence-changed", "Loose reference entries changed during lock observation.")
		}
		return nil
	}
	if e = walk(root, ""); e != nil {
		return e
	}
	final, e := os.Lstat(path)
	if e != nil {
		return failure("evidence-changed", "Loose reference root became unavailable during lock observation.")
	}
	finalID, e := retirementV3Physical(path, final)
	if e != nil || finalID != initial {
		return failure("evidence-changed", "Loose reference root changed during lock observation.")
	}
	return nil
}

// os.Getwd may return the logical PWD spelling. Bind it to an opened actual
// dot directory, resolve aliases, then sample canonical physical ancestors.
func retirementV3CurrentDirectory(ctx context.Context) (string, []PathIdentity, error) {
	dot, e := os.Open(".")
	if e != nil {
		return "", nil, e
	}
	defer dot.Close()
	actual, e := dot.Stat()
	if e != nil {
		return "", nil, e
	}
	logical, e := os.Getwd()
	if e != nil {
		return "", nil, e
	}
	physical, e := filepath.EvalSymlinks(logical)
	if e != nil {
		return "", nil, e
	}
	root, _, e := openCanonical(physical)
	if e != nil {
		return "", nil, e
	}
	resolved, e := root.Stat(".")
	root.Close()
	if e != nil || !os.SameFile(actual, resolved) {
		return "", nil, failure("evidence-changed", "Logical current directory does not bind the actual physical directory.")
	}
	ancestors := []PathIdentity{}
	for p := physical; ; p = filepath.Dir(p) {
		if e := ctx.Err(); e != nil {
			return "", nil, e
		}
		id, e := currentIdentity(p)
		if e != nil {
			return "", nil, e
		}
		ancestors = append(ancestors, id)
		if p == string(filepath.Separator) {
			break
		}
	}
	final, e := os.Stat(".")
	if e != nil || !os.SameFile(actual, final) {
		return "", nil, failure("evidence-changed", "Actual current directory changed during observation.")
	}
	return physical, ancestors, nil
}
