// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"github.com/wstein/sourcesteward/internal/inspection"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

type coordinator struct {
	inspect   func(context.Context, string) inspection.Result
	adapter   func(string) (nativeAdapter, error)
	inventory func(context.Context, string, inspection.InventoryOptions) inspection.InventoryResult
}

func defaultCoordinator() coordinator {
	return coordinator{inspection.Inspect, selectAdapter, inspection.Inventory}
}
func selectAdapter(backend string) (nativeAdapter, error) {
	switch backend {
	case "git":
		return gitAdapter{}, nil
	case "jj":
		return jjAdapter{}, nil
	}
	return nil, failure("unsupported-backend", "Task workspace creation requires unambiguous native Git or jj ownership.")
}
func PlanCreate(ctx context.Context, o PlanOptions) Result {
	if o.CallerDir == "" {
		o.CallerDir, _ = os.Getwd()
	}
	ctx, cancel := context.WithTimeout(withCreationPolicy(ctx), planObservationTimeout)
	defer cancel()
	return bounded(defaultCoordinator().plan(ctx, o))
}
func Apply(ctx context.Context, o ApplyOptions) Result {
	ctx, cancel := context.WithTimeout(withCreationPolicy(ctx), 60*time.Second)
	defer cancel()
	return bounded(defaultCoordinator().apply(ctx, o))
}
func Handoff(ctx context.Context, o Options) Result {
	ctx, cancel := context.WithTimeout(ctx, planObservationTimeout)
	defer cancel()
	return bounded(defaultCoordinator().handoff(ctx, o, false))
}
func RetirePlan(ctx context.Context, o Options) Result {
	ctx, cancel := context.WithTimeout(ctx, planObservationTimeout)
	defer cancel()
	return bounded(defaultCoordinator().handoff(ctx, o, true))
}
func canonicalSource(path string) (string, error) {
	if path == "" {
		path = "."
	}
	if !validText(path) {
		return "", failure("invalid-input", "Source path exceeds4096bytes or is invalid UTF-8.")
	}
	p, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	p, e = filepath.EvalSymlinks(p)
	if e != nil {
		return "", failure("invalid-path", "Source directory is inaccessible.")
	}
	r, _, e := openCanonical(p)
	if e == nil {
		r.Close()
	}
	return p, e
}
func (c coordinator) plan(ctx context.Context, o PlanOptions) Result {
	r := newResult("workspace-plan")
	if !validID(o.Task) || !validID(o.Owner) || o.Base == "" || !validText(o.Base) || !validText(o.Destination) || !validText(o.StateDir) {
		return refuse(r, "invalid-input", "Explicit bounded base, destination, state directory, task and owner are required.")
	}
	source, e := canonicalSource(o.Path)
	if e != nil {
		return failResult(r, e)
	}
	parent, e := destinationParent(o.Destination)
	if e != nil {
		return refuse(r, "unsafe-destination", e.Error())
	}
	identity := c.inspect(ctx, source)
	r.Backend = identity.Backend
	if identity.Status != "ok" {
		return refuse(r, "unsupported-backend", identity.Error())
	}
	a, e := c.adapter(identity.Backend)
	if e != nil {
		return failResult(r, e)
	}
	if identity.Backend == "jj" {
		if e = inspection.CheckJJWorkspaceTaskAvailable(ctx, source, o.Task); e != nil {
			return failResult(r, failure("jj-task-unavailable", "Native jj task name is unavailable or cannot be safely checked."))
		}
	}
	snap, e := a.Observe(ctx, source, o.Base)
	if e != nil {
		return failResult(r, e)
	}
	p := MutationPlan{SchemaVersion: 1, TaskID: o.Task, Owner: o.Owner, SourcePath: source, Destination: o.Destination, StateDir: o.StateDir, Backend: identity.Backend, Reason: "Create one isolated task checkout at the explicitly selected base.", Expected: snap, DestinationParent: parent, Effects: []string{"Persist private plan, pre-action recovery journal and task ownership records.", "Reserve one new private destination directory.", "Create one native task workspace registration and materialize its checkout; shared native metadata may change."}, RecoveryLimits: []string{"No automatic rollback, filesystem removal, branch/bookmark deletion or source restoration.", "Inspect the durable journal and native registration after interruption; incomplete operations require manual recovery.", "Sampled checks do not exclude concurrent external or ABA changes; owner labels are not authentication."}}
	p.Effects = append(p.Effects, nativeEffects(p.Backend)...)
	p.RecoveryLimits = append(p.RecoveryLimits, "Linux sandbox execution restrictions are limited by native runtime needs; configuration/helper preflight and network denial are enforced, but hostile same-user concurrent replacement cannot be excluded.")
	if e = disjointStore(p); e != nil {
		return failResult(r, e)
	}
	s, e := openStore(o.StateDir, true)
	if e != nil {
		return failResult(r, e)
	}
	defer s.Close()
	ids, e := s.list("plans")
	if e != nil {
		return failResult(r, e)
	}
	if len(ids) >= observationLimit {
		return failResult(r, failure("record-limit", "State store has1024 creation plans."))
	}
	for _, id := range ids {
		var old MutationPlan
		if e = s.read("plans", id, &old); e != nil {
			return failResult(r, e)
		}
		if old.TaskID == o.Task {
			return refuse(r, "task-collision", "Task identity is already reserved by a stored plan.")
		}
	}
	if e = ctx.Err(); e != nil {
		return failResult(r, e)
	}
	p.PlanID, e = randomID()
	if e != nil {
		return failResult(r, e)
	}
	p.Digest = planDigest(p)
	if e = validatePlan(p, o.StateDir, p.PlanID); e != nil {
		return failResult(r, e)
	}
	if e = s.write("plans", p.PlanID, p, true); e != nil {
		return failResult(r, e)
	}
	r.Status = "ok"
	r.Plan = &p
	r.Source = c.sourceContext(ctx, o.CallerDir, p)
	r.Actions = []ActionOutcome{{"store-plan", "completed", "Immutable plan stored; native repository and destination are unchanged."}}
	r.Limitations = append(r.Limitations, p.RecoveryLimits...)
	return r
}

// sourceContext reports the facts a reviewer needs to notice a plan built on a
// commit the caller did not mean: the source checkout, the resolved base and, when
// the caller stands in another checkout of the same repository, both HEADs.
func (c coordinator) sourceContext(ctx context.Context, callerDir string, p MutationPlan) *SourceContext {
	s := &SourceContext{SourcePath: p.SourcePath, BaseSelector: p.Expected.BaseSelector, BaseCommit: p.Expected.BaseCommit, SourceHead: p.Expected.Identity.CommitID}
	if callerDir == "" || !validText(callerDir) {
		return s
	}
	if abs, e := filepath.Abs(callerDir); e == nil {
		callerDir = abs
	}
	if real, e := filepath.EvalSymlinks(callerDir); e == nil {
		callerDir = real
	}
	s.CallerDirectory = callerDir
	in := c.inspect(ctx, callerDir)
	if in.Status != "ok" || in.Backend != p.Backend {
		return s
	}
	var repository, root, head string
	switch {
	case in.Repository != nil && p.Backend == "git":
		repository = in.Repository.CommonDir
		if in.Repository.Worktree != nil {
			root = *in.Repository.Worktree
		}
		if in.Repository.Head.OID != nil {
			head = *in.Repository.Head.OID
		}
	case in.JJRepository != nil && p.Backend == "jj":
		repository, root, head = in.JJRepository.RepositoryDir, in.JJRepository.WorkspaceRoot, in.JJRepository.CommitID
	default:
		return s
	}
	if root == "" || !sameFile(repository, p.Expected.Identity.RepositoryDir) {
		return s
	}
	s.CallerCheckout, s.CallerHead = root, head
	if sameFile(root, p.Expected.Identity.WorkspaceRoot) {
		return s
	}
	s.CallerDiffers = true
	s.Notice = "The current directory " + callerDir + " is in another checkout (" + root + ", HEAD " + head + ") of the same repository as the source " + p.SourcePath + " (HEAD " + p.Expected.Identity.CommitID + "); the base " + p.Expected.BaseSelector + " was resolved in the source to " + p.Expected.BaseCommit + ". Plan from the other checkout if that was meant."
	return s
}

func sameFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	x, e1 := os.Stat(a)
	y, e2 := os.Stat(b)
	return e1 == nil && e2 == nil && os.SameFile(x, y)
}

type journal struct {
	SchemaVersion       int              `json:"schema_version"`
	PlanID              string           `json:"plan_id"`
	Phase               string           `json:"phase"`
	Native              *NativeWorkspace `json:"native"`
	DestinationIdentity *PathIdentity    `json:"destination_identity"`
	Actions             []ActionOutcome  `json:"actions"`
	Diagnostics         []Diagnostic     `json:"diagnostics"`
}

func validatePlan(p MutationPlan, state, id string) error {
	if !validSnapshot(p.Expected) || !validStrings(p.Effects) || !validStrings(p.RecoveryLimits) || !validText(p.Reason) || !validText(p.DestinationParent.Path) || !validText(filepath.Join(p.StateDir, "operations", p.PlanID+".json")) {
		return failure("invalid-plan", "Stored plan contains malformed native identities, digests or bounded evidence.")
	}
	if p.SchemaVersion != 1 || p.PlanID != id || !validID(p.PlanID) || !validID(p.TaskID) || !validID(p.Owner) || p.StateDir != state || p.Digest != planDigest(p) || p.DestinationIdentity != nil || p.Expected.Identity.Backend != p.Backend || p.Expected.BaseSelector == "" || p.Expected.BaseCommit == "" || p.Effects == nil || p.RecoveryLimits == nil {
		return failure("invalid-plan", "Stored plan is incompatible, modified or not bound to this state store.")
	}
	for _, s := range []string{p.SourcePath, p.Destination, p.StateDir, p.Expected.BaseSelector, p.Expected.Identity.RepositoryDir, p.Expected.Identity.WorkspaceRoot, p.Expected.Identity.GitDir} {
		if !validText(s) {
			return failure("invalid-plan", "Stored plan exceeds its text bounds.")
		}
	}
	if !filepath.IsAbs(p.SourcePath) || !filepath.IsAbs(p.Destination) || filepath.Clean(p.SourcePath) != p.SourcePath || filepath.Clean(p.Destination) != p.Destination || p.DestinationParent.Path != filepath.Dir(p.Destination) {
		return failure("invalid-plan", "Stored plan paths are not canonical.")
	}
	return disjointStore(p)
}
func (c coordinator) apply(ctx context.Context, o ApplyOptions) Result {
	r := newResult("workspace-apply")
	if !o.Authorize {
		return refuse(r, "authorization-required", "Apply requires explicit --authorize for the stored local effects.")
	}
	if !validID(o.PlanID) || !validText(o.StateDir) {
		return refuse(r, "invalid-input", "A bounded stored plan identity and state directory are required.")
	}
	s, e := openStore(o.StateDir, true)
	if e != nil {
		return failResult(r, e)
	}
	defer s.Close()
	var p MutationPlan
	if e = s.read("plans", o.PlanID, &p); e != nil {
		return failResult(r, e)
	}
	if e = validatePlan(p, o.StateDir, o.PlanID); e != nil {
		return failResult(r, e)
	}
	r.Plan = &p
	r.Backend = p.Backend
	r.Limitations = append(r.Limitations, p.RecoveryLimits...)
	a, e := c.adapter(p.Backend)
	if e != nil {
		return failResult(r, e)
	}
	var j journal
	e = s.read("operations", p.PlanID, &j)
	if e == nil {
		return c.retry(ctx, s, p, j, a, r)
	}
	if !errors.Is(e, os.ErrNotExist) {
		return failResult(r, e)
	}
	var existing TaskRecord
	if e = s.read("tasks", p.TaskID, &existing); e == nil {
		return refuse(r, "task-collision", "Task ownership already exists; no new workspace was created.")
	} else if !errors.Is(e, os.ErrNotExist) {
		return failResult(r, e)
	}
	id := c.inspect(ctx, p.SourcePath)
	if id.Status != "ok" || id.Backend != p.Backend {
		return refuse(r, "changed-source", "Source ownership or native inspection capability changed.")
	}
	current, e := a.Observe(ctx, p.SourcePath, p.Expected.BaseSelector)
	if e != nil {
		return failResult(r, e)
	}
	if !reflect.DeepEqual(current, p.Expected) {
		return refuse(r, "stale-plan", "Native source, base, references, registrations or safety state changed; create a new plan.")
	}
	if p.Backend == "jj" {
		if e = inspection.CheckJJWorkspaceTaskAvailable(ctx, p.SourcePath, p.TaskID); e != nil {
			return refuse(r, "jj-task-unavailable", "Native jj task name became unavailable; no destination was reserved.")
		}
	}
	parent, e := destinationParent(p.Destination)
	if e != nil || parent != p.DestinationParent {
		return refuse(r, "changed-destination", "Destination or its parent changed; nothing was created.")
	}
	if e = ctx.Err(); e != nil {
		return failResult(r, e)
	}
	j = journal{SchemaVersion: 1, PlanID: p.PlanID, Phase: "prepared", Actions: []ActionOutcome{{"prepare-recovery", "completed", "Durable pre-action recovery recorded before destination/native effects."}}, Diagnostics: []Diagnostic{}}
	if e = s.write("operations", p.PlanID, j, true); e != nil {
		return failResult(r, e)
	}
	r.Actions = j.Actions
	reserved, e := reserveDestination(p)
	if e != nil {
		return c.incomplete(s, p, j, r, e)
	}
	p.DestinationIdentity = &reserved
	j.DestinationIdentity = &reserved
	j.Actions = append(j.Actions, ActionOutcome{"reserve-destination", "completed", "New private destination reserved with native file identity."})
	if e = s.write("operations", p.PlanID, j, false); e != nil {
		return c.incomplete(s, p, j, r, e)
	}
	if e = ctx.Err(); e != nil {
		return c.incomplete(s, p, j, r, e)
	}
	fresh, e := currentIdentity(p.Destination)
	if e != nil || fresh != reserved {
		return c.incomplete(s, p, j, r, failure("changed-destination", "Reserved destination changed before native action."))
	}
	native, e := a.Create(ctx, p)
	if native.Effects == nil {
		native.Effects = []string{}
	}
	if native.Limitations == nil {
		native.Limitations = []string{}
	}
	j.Native = &native
	r.Limitations = append(r.Limitations, native.Limitations...)
	effectStatus := "incomplete"
	if e == nil && native.Complete && native.SourceUnchanged {
		effectStatus = "completed"
	}
	for _, effect := range native.Effects {
		j.Actions = append(j.Actions, ActionOutcome{"native-effect", effectStatus, effect})
	}
	if ctx.Err() != nil {
		e = ctx.Err()
	}
	if e != nil || !native.Complete || !native.SourceUnchanged {
		if e == nil {
			e = failure("incomplete-native", "Native workspace or source-preservation verification is incomplete.")
		}
		return c.incomplete(s, p, j, r, e)
	}
	now, e := currentIdentity(p.Destination)
	if e != nil || now != reserved {
		return c.incomplete(s, p, j, r, failure("changed-destination", "Destination identity changed during native action; inspect the recovery journal."))
	}
	j.Phase = "native-created"
	if e = s.write("operations", p.PlanID, j, false); e != nil {
		return c.incomplete(s, p, j, r, e)
	}
	return c.finalize(ctx, s, p, j, a, r)
}
func (c coordinator) incomplete(s *store, p MutationPlan, j journal, r Result, e error) Result {
	j.Phase = "incomplete"
	failed := failResult(r, e)
	j.Diagnostics = failed.Diagnostics
	j.Actions = append(j.Actions, ActionOutcome{"create-workspace", "incomplete", "Some destination or native effects may remain; automatic retry/adoption is refused."})
	writeErr := s.write("operations", p.PlanID, j, false)
	r.Status = "partial"
	r.Actions = j.Actions
	r.Diagnostics = j.Diagnostics
	if writeErr != nil {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{"journal-write-failed", "Recovery update failed; retain the workspace and inspect the earlier prepared journal."})
	}
	return r
}
func (c coordinator) retry(ctx context.Context, s *store, p MutationPlan, j journal, a nativeAdapter, r Result) Result {
	if !validJournal(j, p) {
		return failResult(r, failure("invalid-state", "Operation journal is incompatible."))
	}
	r.Actions = j.Actions
	if j.Phase != "complete" && j.Phase != "native-created" {
		r.Status = "partial"
		r.Diagnostics = []Diagnostic{{"incomplete-operation", "Prior operation may have destination/native effects; automatic adoption or repeated creation is refused."}}
		return r
	}
	if j.Native == nil || j.DestinationIdentity == nil || !j.Native.Complete || !j.Native.SourceUnchanged {
		return refuse(r, "invalid-ownership", "Journal does not prove completed native creation.")
	}
	p.DestinationIdentity = j.DestinationIdentity
	current, e := currentIdentity(p.Destination)
	if e != nil || current != *j.DestinationIdentity {
		return refuse(r, "changed-destination", "Owned destination was replaced or is unavailable; retain it.")
	}
	if j.Phase == "native-created" {
		return c.finalize(ctx, s, p, j, a, r)
	}
	var task TaskRecord
	if e = s.read("tasks", p.TaskID, &task); e != nil {
		return failResult(r, e)
	}
	if !validTask(task, p) || !sameOwner(task.Native, j.Native.Identity) || task.DestinationIdentity != *j.DestinationIdentity {
		return refuse(r, "invalid-ownership", "Task record and journal ownership disagree.")
	}
	native, e := a.Verify(ctx, p)
	if e != nil || !native.Complete || !validNativeIdentity(native.Identity) || !sameOwner(native.Identity, task.Native) {
		return refuse(r, "changed-ownership", "Current native registration does not match durable task ownership.")
	}
	task.Native = native.Identity
	r.Task = &task
	r.Status = "ok"
	r.Actions = append(r.Actions, ActionOutcome{"retry", "completed", "Previously completed owned workspace verified; no creation repeated."})
	return r
}
func (c coordinator) finalize(ctx context.Context, s *store, p MutationPlan, j journal, a nativeAdapter, r Result) Result {
	if e := ctx.Err(); e != nil {
		return c.incomplete(s, p, j, r, e)
	}
	native, e := a.Verify(ctx, p)
	if ctx.Err() != nil {
		return c.incomplete(s, p, j, r, ctx.Err())
	}
	if e != nil || !native.Complete || native.Identity != j.Native.Identity {
		return c.incomplete(s, p, j, r, failure("verification-failed", "Native registration ownership could not be verified after creation."))
	}
	task := TaskRecord{SchemaVersion: 1, TaskID: p.TaskID, Owner: p.Owner, PlanID: p.PlanID, Backend: p.Backend, Destination: p.Destination, RecoveryPointer: filepath.Join(p.StateDir, "operations", p.PlanID+".json"), Native: j.Native.Identity, DestinationIdentity: *j.DestinationIdentity, BaseCommit: p.Expected.BaseCommit, BaseChange: p.Expected.BaseChange, State: "complete", RemainingWork: []string{}, RecoveryLimits: p.RecoveryLimits}
	var existing TaskRecord
	readErr := s.read("tasks", p.TaskID, &existing)
	if readErr == nil {
		if !validTask(existing, p) || !sameOwner(existing.Native, task.Native) || existing.DestinationIdentity != task.DestinationIdentity {
			return c.incomplete(s, p, j, r, failure("task-collision", "Existing task ownership does not match this creation."))
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return c.incomplete(s, p, j, r, readErr)
	} else if e = s.write("tasks", p.TaskID, task, true); e != nil {
		return c.incomplete(s, p, j, r, e)
	}
	j.Phase = "complete"
	j.Actions = append(j.Actions, ActionOutcome{"create-workspace", "completed", "Native creation, source preservation and durable task ownership verified."})
	if e = s.write("operations", p.PlanID, j, false); e != nil {
		return c.incomplete(s, p, j, r, e)
	}
	r.Status = "ok"
	r.Task = &task
	r.Actions = j.Actions
	return r
}
func sameOwner(a, b NativeIdentity) bool {
	return a.Backend == b.Backend && a.RepositoryDir == b.RepositoryDir && a.WorkspaceRoot == b.WorkspaceRoot && a.WorkspaceName == b.WorkspaceName && a.GitDir == b.GitDir
}
func validTask(t TaskRecord, p MutationPlan) bool {
	return validStrings(t.RemainingWork) && validStrings(t.RecoveryLimits) && t.Inventory == nil && validText(t.RecoveryPointer) && t.State == "complete" && t.RecoveryPointer == filepath.Join(p.StateDir, "operations", p.PlanID+".json") && validNativeIdentity(t.Native) && t.SchemaVersion == 1 && t.TaskID == p.TaskID && t.PlanID == p.PlanID && t.Owner == p.Owner && t.Backend == p.Backend && t.Destination == p.Destination && t.BaseCommit == p.Expected.BaseCommit && t.BaseChange == p.Expected.BaseChange && t.Native.Backend == p.Backend && t.Native.WorkspaceRoot == p.Destination && t.Native.RepositoryDir == p.Expected.Identity.RepositoryDir && t.RemainingWork != nil && t.RecoveryLimits != nil
}

func nativeEffects(backend string) []string {
	if backend == "git" {
		return []string{"Create one detached, locked native Git worktree at the resolved base commit; create no branch or bookmark.", "Write the new common-dir/worktrees registration, its per-worktree HEAD/index/lock metadata and destination checkout only; preexisting workspace metadata stays read-only.", "Refuse configured hooks, checkout filters and unsupported helper/configuration paths rather than skip installed hooks; source HEAD/index/configuration/files remain unchanged."}
	}
	return []string{"Create one named native jj workspace and a new working-copy commit/change based on the explicit resolved base commit.", "Write shared jj repository operation/object/workspace metadata and underlying Git objects/refs/jj where the native backend requires them; preserve source working-copy metadata/files and bookmarks.", "Use isolated noninteractive configuration with SourceSteward/stw@localhost authorship, source snapshot suppression, pinned operation and git.colocate=false.", "Register without source checkout, then materialize only the newly reserved owned destination with native jj update-stale; a failed second phase can leave registration and checkout incomplete."}
}
func validJournal(j journal, p MutationPlan) bool {
	if j.SchemaVersion != 1 || j.PlanID != p.PlanID || j.Actions == nil || j.Diagnostics == nil || len(j.Actions) > observationLimit || len(j.Diagnostics) > observationLimit {
		return false
	}
	switch j.Phase {
	case "prepared", "native-created", "complete", "incomplete":
	default:
		return false
	}
	for _, a := range j.Actions {
		if !validText(a.Name) || !validText(a.Status) || !validText(a.Detail) {
			return false
		}
	}
	for _, d := range j.Diagnostics {
		if !validText(d.Code) || !validText(d.Message) {
			return false
		}
	}
	if j.DestinationIdentity != nil && (!validText(j.DestinationIdentity.Path) || j.DestinationIdentity.Path != p.Destination) {
		return false
	}
	if j.Native != nil {
		if !validStrings(j.Native.Effects) || !validStrings(j.Native.Limitations) || !validText(j.Native.Phase) {
			return false
		}
		if !validNativeIdentity(j.Native.Identity) && (j.Native.Complete || j.Native.Identity != (NativeIdentity{})) {
			return false
		}
	}
	return true
}
func validNativeIdentity(n NativeIdentity) bool {
	for _, s := range []string{n.Backend, n.RepositoryDir, n.WorkspaceRoot, n.WorkspaceName, n.GitDir, n.HeadRef, n.CommitID, n.ChangeID, n.OperationID, n.ObjectFormat} {
		if !validText(s) {
			return false
		}
	}
	for _, p := range []string{n.RepositoryDir, n.WorkspaceRoot} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return false
		}
	}
	if n.GitDir != "" && (!filepath.IsAbs(n.GitDir) || filepath.Clean(n.GitDir) != n.GitDir) {
		return false
	}
	if n.Backend == "git" {
		size := 40
		if n.ObjectFormat == "sha256" {
			size = 64
		} else if n.ObjectFormat != "sha1" {
			return false
		}
		return validAlphabet(n.CommitID, size, "0123456789abcdef") && n.ChangeID == "" && n.OperationID == "" && (n.HeadRef == "" || strings.HasPrefix(n.HeadRef, "refs/heads/"))
	}
	return n.Backend == "jj" && validAlphabet(n.CommitID, 40, "0123456789abcdef") && validAlphabet(n.ChangeID, 32, "klmnopqrstuvwxyz") && validAlphabet(n.OperationID, 128, "0123456789abcdef")
}
func validSnapshot(s Snapshot) bool {
	if !validNativeIdentity(s.Identity) || s.BaseSelector == "" || !validText(s.BaseSelector) || !validAlphabet(s.RegistrationDigest, 64, "0123456789abcdef") || !validAlphabet(s.SafetyDigest, 64, "0123456789abcdef") {
		return false
	}
	size := 40
	if s.Identity.ObjectFormat == "sha256" {
		size = 64
	}
	if !validAlphabet(s.BaseCommit, size, "0123456789abcdef") {
		return false
	}
	if s.Identity.Backend == "git" {
		return s.BaseChange == "" && validAlphabet(s.ReferenceDigest, 64, "0123456789abcdef")
	}
	return validAlphabet(s.BaseChange, 32, "klmnopqrstuvwxyz") && (validAlphabet(s.ReferenceDigest, 64, "0123456789abcdef") || validAlphabet(s.ReferenceDigest, 128, "0123456789abcdef"))
}
func validAlphabet(s string, size int, alphabet string) bool {
	if len(s) != size {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune(alphabet, c) {
			return false
		}
	}
	return true
}
func validStrings(values []string) bool {
	if values == nil || len(values) > observationLimit {
		return false
	}
	for _, s := range values {
		if !validText(s) {
			return false
		}
	}
	return true
}
