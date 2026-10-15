// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/wstein/sourcesteward/internal/inspection"
	"os"
	"path/filepath"
	"reflect"
)

type retirementCoordinator struct {
	inspect       func(context.Context, string) inspection.Result
	adapter       func(string) (retirementAdapter, error)
	sourceObserve func(context.Context, string, string, string) (Snapshot, error)
	cwd           func() (string, error)
}

func defaultRetirementCoordinator() retirementCoordinator {
	return retirementCoordinator{inspection.Inspect, selectRetirementAdapter, observeRetirementSource, currentRetirementCWD}
}
func selectRetirementAdapter(backend string) (retirementAdapter, error) {
	switch backend {
	case "git":
		return gitRetirementAdapter{}, nil
	case "jj":
		return jjRetirementAdapter{}, nil
	}
	return nil, failure("unsupported-backend", "Owned retirement requires native Git or jj ownership.")
}
func observeRetirementSource(ctx context.Context, backend, path, base string) (Snapshot, error) {
	if backend == "git" {
		observation, err := (gitTidyAdapter{}).Observe(ctx, path, base)
		return observation.Expected, err
	}
	a, err := selectAdapter(backend)
	if err != nil {
		return Snapshot{}, err
	}
	return a.Observe(ctx, path, base)
}
func newRetirementResult(command string) TidyResult {
	r := newTidyResult(command)
	r.SchemaVersion = 2
	return r
}
func retirementRecoveryLimits() []string {
	return []string{"Registration removal and exact-path filesystem removal are separate reviewed effects; neither authorizes remote deletion, jj abandonment, pruning or garbage collection.", "Recovery roots preserve the selected committed tree, not arbitrary untracked files, original permission/ownership bits, per-worktree index, reflog or otherwise unreachable historical commits.", "Owner labels and completed creation journals are cooperative ownership evidence, not authentication or proof that another editor or agent is idle.", "Native and filesystem checks are sampled; external races and ABA changes cannot be transactionally excluded. Incomplete operations require manual inspection; blind retry and automatic restoration are refused."}
}
func retirementEffects(p RetirementPlanRecord) []string {
	r := retirementRecoveryFor(p)
	effects := []string{"Proposed owned retirement v2: persist prepared and confirmed recovery journals before native changes; planning changes no native repository or target state.", "Proposed owned retirement v2: create and retain exact native recovery root " + r.Name + " at target commit " + r.CommitID + " change " + r.ChangeID + "."}
	if p.RemoveRegistration {
		effects = append(effects, "Proposed owned retirement v2: remove only the completed owned native workspace registration for task "+p.Task.TaskID+" at "+p.Task.Destination+".")
	}
	if p.RemoveFiles != "" {
		effects = append(effects, "Proposed owned retirement v2: remove only the independently authorized verified physical manifest at "+p.RemoveFiles+".")
	}
	if p.Backend == "git" && p.RemoveFiles != "" {
		effects = append(effects, "Proposed owned retirement v2: native Git removes the exact stw task lock, admitted worktree registration metadata and destination together; no dirty-state override is authorized.")
	}
	return effects
}
func (c retirementCoordinator) plan(ctx context.Context, o TidyPlanOptions) TidyResult {
	r := newRetirementResult("tidy-plan")
	if !validID(o.Task) || !validID(o.Owner) || o.Base == "" || !validText(o.Base) || o.StateDir == "" || !validText(o.StateDir) || !o.RemoveRegistration || len(o.Refs) > 0 || !validText(o.RemoveFiles) {
		return refuseTidy(r, "invalid-input", "Retirement requires exact task/owner, explicit base/private state, registration selection and an optional exact files path; reference selectors cannot be mixed.")
	}
	source, err := canonicalSource(o.Path)
	if err != nil {
		return failTidy(r, err)
	}
	cwd, err := c.cwd()
	if err != nil {
		return failTidy(r, err)
	}
	s, err := openStore(o.StateDir, false)
	if err != nil {
		return failTidy(r, err)
	}
	defer s.Close()
	creation, task, journalDigest, err := readRetirementOwnership(s, o.Task, o.Owner)
	if err != nil {
		return failTidy(r, err)
	}
	r.Backend = task.Backend
	if o.RemoveFiles != "" && o.RemoveFiles != task.Destination {
		return refuseTidy(r, "unsafe-files-target", "Filesystem removal must name exactly the completed owned task destination.")
	}
	if err = protectRetirementWorkspace(source, task.Destination, cwd); err != nil {
		return refuseTidy(r, tidyErrorCode(err), err.Error())
	}
	identity := c.inspect(ctx, source)
	if identity.Status != "ok" || identity.Backend != task.Backend {
		return refuseTidy(r, "unsupported-backend", "Source ownership is unavailable or does not match the completed task backend.")
	}
	a, err := c.adapter(task.Backend)
	if err != nil {
		return failTidy(r, err)
	}
	observation, err := a.Observe(ctx, source, o.Base, task, creation)
	if err != nil {
		return failTidy(r, err)
	}
	if err = validateRetirementObservation(observation, source, o.Base, task); err != nil {
		return failTidy(r, err)
	}
	if !observation.WorkspacePathsComplete {
		return refuseTidy(r, "unknown-workspace-path", "Complete registered workspace roots are required before storing retirement evidence.")
	}
	if err = qualifyTidyWorkspaceRoots(ctx, observation.WorkspacePaths, o.StateDir); err != nil {
		return failTidy(r, err)
	}
	p := RetirementPlanRecord{SchemaVersion: 2, SourcePath: source, StateDir: o.StateDir, Backend: task.Backend, CreationPlan: creation, CreationJournalDigest: journalDigest, Task: task, Expected: observation, RemoveRegistration: o.RemoveRegistration, RemoveFiles: o.RemoveFiles, Effects: []string{}, RecoveryLimits: retirementRecoveryLimits()}
	if err = validateRetirementSource(ctx, p, cwd); err != nil {
		return failTidy(r, err)
	}
	fresh, err := a.Observe(ctx, source, o.Base, task, creation)
	if err != nil {
		return failTidy(r, err)
	}
	if !reflect.DeepEqual(observation, fresh) {
		return refuseTidy(r, "stale-plan", "Native target, disk, metadata, base or ownership changed while retirement was planned.")
	}
	if err = verifyRetirementOwnership(s, p); err != nil {
		return failTidy(r, err)
	}
	p.PlanID, err = randomID()
	if err != nil {
		return failTidy(r, err)
	}
	// Keep the native observation immutable. Authorization selectors are separate
	// fields and never alter the backend's independently verified proof/actions.
	p.Expected = selectedRetirementObservation(p.Expected, p)
	p.Effects = retirementEffects(p)
	p.Digest = retirementPlanDigest(p)
	if err = validateRetirementPlan(p, o.StateDir, p.PlanID); err != nil {
		return failTidy(r, err)
	}
	r.Retirement = &p
	r.Limitations = append(append([]string{}, p.Expected.Limitations...), p.RecoveryLimits...)
	r.Actions = []ActionOutcome{{"store-plan", "completed", "Immutable owned-retirement plan stored; no native registration or files were changed."}}
	if !withinTidyBounds(r) {
		r.Retirement = nil
		return failTidy(r, failure("record-limit", "Complete retirement result exceeds bounded evidence; no plan was stored."))
	}
	// No state files or namespaces are created until complete native ownership,
	// canonical all-workspace disjointness and output bounds have been proved.
	s.Close()
	s, err = openRetirementStore(o.StateDir, true)
	if err != nil {
		return failTidy(r, err)
	}
	defer s.Close()
	if err = verifyRetirementOwnership(s, p); err != nil {
		return failTidy(r, err)
	}
	finalObservation, err := a.Observe(ctx, source, o.Base, task, creation)
	if err != nil {
		return failTidy(r, err)
	}
	finalObservation = selectedRetirementObservation(finalObservation, p)
	if !reflect.DeepEqual(finalObservation, p.Expected) {
		return refuseTidy(r, "stale-plan", "Native target or ownership changed while the private store was locked.")
	}
	ids, err := s.list("tidy-retirement-plans")
	if err != nil {
		return failTidy(r, err)
	}
	if len(ids) >= observationLimit {
		return failTidy(r, failure("record-limit", "Private state has1024 retirement plans."))
	}
	if err = checkOtherTidyNamespace(s, p.PlanID, "tidy-plans"); err != nil {
		return failTidy(r, err)
	}
	if err = ctx.Err(); err != nil {
		return failTidy(r, err)
	}
	if err = s.write("tidy-retirement-plans", p.PlanID, p, true); err != nil {
		return failTidy(r, err)
	}
	r.Status = "ok"
	if !retirementEligible(p) {
		r.Status = "refused"
		r.Diagnostics = []Diagnostic{{"retirement-retained", "The plan retains this workspace because native integration proof or a selected capability is unavailable."}}
	}
	return boundedTidy(r)
}
func retirementEligible(p RetirementPlanRecord) bool {
	if p.Expected.Proof == "none" || !p.RemoveRegistration || (p.Backend == "git" && p.RemoveFiles == "") {
		return false
	}
	for _, name := range []string{"remove-registration", "remove-files"} {
		if name == "remove-files" && p.RemoveFiles == "" {
			continue
		}
		found := false
		for _, action := range p.Expected.Actions {
			if action.Name == name {
				found = action.Decision == "delete" && len(action.Blockers) == 0 && action.Proof == p.Expected.Proof
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func validateRetirementObservation(o RetirementObservation, source, base string, task TaskRecord) error {
	if !validSnapshot(o.Expected) || o.Expected.Identity.Backend != task.Backend || o.Expected.Identity.WorkspaceRoot != source || o.Expected.BaseSelector != base || o.Expected.Identity.RepositoryDir != task.Native.RepositoryDir || !validNativeIdentity(o.Target) || !sameOwner(o.Target, task.Native) || o.Target.WorkspaceRoot != task.Destination || o.Destination != task.DestinationIdentity || o.DestinationParent.Path != filepath.Dir(task.Destination) || !validRetirementPathIdentity(o.DestinationParent) || !validTidyPaths(o.WorkspacePaths) || o.Siblings == nil || o.Actions == nil || !validStrings(o.Limitations) || !validText(o.LockReason) || (o.Proof != "native-ancestry" && o.Proof != "empty-working-copy-integrated-parent" && o.Proof != "none") {
		return failure("invalid-evidence", "Retirement ownership, native identities, paths or complete evidence are malformed.")
	}
	if task.Backend == "git" {
		if !validRetirementPathIdentity(o.RegistrationParent) || o.LockReason != "stw:"+task.TaskID {
			return failure("invalid-evidence", "Git retirement requires the exact owned native registration parent and stw task lock.")
		}
	} else if o.RegistrationParent != (PathIdentity{}) || o.LockReason != "" {
		return failure("invalid-evidence", "jj retirement cannot invent a filesystem registration parent or Git lock.")
	}
	if err := validateRetirementManifest(o.Manifest); err != nil {
		return err
	}
	if err := validateRetirementManifest(o.Metadata); err != nil {
		return err
	}
	if o.SiblingDigest != retirementRecordDigest(o.Siblings) {
		return failure("invalid-evidence", "Retirement sibling digest does not match its exact observations.")
	}
	paths := map[string]bool{}
	for _, sibling := range o.Siblings {
		if !validRetirementPathIdentity(sibling.Identity) || (sibling.Kind != "regular" && sibling.Kind != "directory") || paths[sibling.Identity.Path] {
			return failure("invalid-evidence", "Retirement sibling identities are malformed or repeated.")
		}
		paths[sibling.Identity.Path] = true
	}
	names := map[string]bool{}
	for _, action := range o.Actions {
		if (action.Name != "remove-registration" && action.Name != "remove-files") || names[action.Name] || !validText(action.Target) || action.Target == "" || action.Reason == "" || !validText(action.Reason) || (action.Decision != "delete" && action.Decision != "retain") || !validStrings(action.Effects) || !validStrings(action.Blockers) || action.Proof != o.Proof || (action.Decision == "delete" && (action.Proof == "none" || len(action.Blockers) > 0)) {
			return failure("invalid-evidence", "Retirement actions are malformed or contain incomplete deletion proof.")
		}
		names[action.Name] = true
	}
	if len(names) != 2 {
		return failure("invalid-evidence", "Separate native-registration and filesystem action evidence is required.")
	}
	return nil
}
func validRetirementPathIdentity(id PathIdentity) bool {
	return validText(id.Path) && filepath.IsAbs(id.Path) && filepath.Clean(id.Path) == id.Path && id.Inode != 0
}
func validateRetirementPlan(p RetirementPlanRecord, state, id string) error {
	if p.SchemaVersion != 2 || p.PlanID != id || !validID(id) || p.StateDir != state || !validText(state) || !filepath.IsAbs(state) || filepath.Clean(state) != state || !validText(p.SourcePath) || !filepath.IsAbs(p.SourcePath) || filepath.Clean(p.SourcePath) != p.SourcePath || !validText(p.RemoveFiles) || !p.RemoveRegistration || (p.RemoveFiles != "" && p.RemoveFiles != p.Task.Destination) || !validAlphabet(p.CreationJournalDigest, 64, "0123456789abcdef") || p.Digest != retirementPlanDigest(p) || !validText(retirementJournalPointer(p)) || !reflect.DeepEqual(p.Effects, retirementEffects(p)) || !reflect.DeepEqual(p.RecoveryLimits, retirementRecoveryLimits()) || !validStrings(p.Effects) || !validStrings(p.RecoveryLimits) {
		return failure("invalid-plan", "Owned retirement plan is modified, incompatible or not bound to its private state and exact effects.")
	}
	if err := validatePlan(p.CreationPlan, state, p.CreationPlan.PlanID); err != nil {
		return err
	}
	if !validTask(p.Task, p.CreationPlan) || p.Task.Backend != p.Backend {
		return failure("invalid-ownership", "Retirement task and original creation plan disagree.")
	}
	if err := validateRetirementObservation(p.Expected, p.SourcePath, p.Expected.Expected.BaseSelector, p.Task); err != nil {
		return err
	}
	r := newRetirementResult("tidy-plan")
	r.Retirement = &p
	if !withinTidyBounds(r) {
		return failure("record-limit", "Owned retirement plan exceeds complete bounded evidence.")
	}
	return nil
}
func retirementResultRecordCount(p *RetirementPlanRecord) int {
	if p == nil {
		return 0
	}
	o := p.Expected
	count := len(p.Effects) + len(p.RecoveryLimits) + len(p.CreationPlan.Effects) + len(p.CreationPlan.RecoveryLimits) + len(p.Task.RemainingWork) + len(p.Task.RecoveryLimits) + len(o.Manifest.Entries) + len(o.Metadata.Entries) + len(o.Siblings) + len(o.WorkspacePaths) + len(o.Actions) + len(o.Limitations)
	for _, action := range o.Actions {
		count += len(action.Effects) + len(action.Blockers)
	}
	return count
}
func retirementSnapshotAfterRegistration(before, after Snapshot) bool {
	// Native adapter proves only the exact owned registration disappeared; core
	// independently protects all other stable fields and samples the full digest.
	before.RegistrationDigest = after.RegistrationDigest
	if before.Identity.Backend == "jj" {
		return validTidyNativeTransition(before, after)
	}
	return reflect.DeepEqual(before, after)
}
func retirementPathAbsent(path string, parent PathIdentity) bool {
	root, id, err := openCanonical(filepath.Dir(path))
	if err != nil {
		return false
	}
	defer root.Close()
	if id != parent {
		return false
	}
	_, err = root.Lstat(filepath.Base(path))
	return errors.Is(err, os.ErrNotExist)
}
func retirementJournalEncodedBound(j RetirementJournal) bool {
	if len(j.Actions)+len(j.Diagnostics) > observationLimit {
		return false
	}
	b, err := json.Marshal(j)
	return err == nil && len(b) <= recordLimit
}
