// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

// Restore of deleted local branches from the tidy v3 recovery journal (Spec 031
// FR-042..FR-046, contracts/restore.md). The plan is a read-only observation: it
// reads the stored journal through the shared reader and the clone's references
// by exact name, under the constant planning arguments and environment of
// package B, and never writes.

const restoreRefPrefix = "refs/heads/"

// Entry states and reasons of the restore contract.
const (
	restoreRestorable        = "restorable"
	restoreAlreadyPresent    = "already-present"
	restoreNameTaken         = "name-taken"
	restoreRecoveryMissing   = "recovery-missing"
	restoreJournalIncomplete = "journal-incomplete"
)

var restoreLimits = []string{"tip-and-history-only", "no-config", "no-reflog", "no-worktrees"}

// V3RestoreOptions selects a restore plan or, with Apply, its authorized effect.
type V3RestoreOptions struct {
	Path, StateDir, PlanID string
	Actions                []string
	As                     string
	Apply, Authorize       bool
}

// V3RestoreDecision is the journal's reviewer decision, shown as data.
type V3RestoreDecision struct {
	Reviewer string `json:"reviewer"`
	Kind     string `json:"kind"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// V3RestoreEntry is one journal entry of the plan.
type V3RestoreEntry struct {
	ActionID    string             `json:"action_id"`
	Ref         string             `json:"ref"`
	OID         string             `json:"oid"`
	RecoveryRef string             `json:"recovery_ref"`
	State       string             `json:"state"`
	Reason      *string            `json:"reason"`
	Class       string             `json:"class"`
	Flags       []string           `json:"flags"`
	Decision    *V3RestoreDecision `json:"decision"`
	// BranchConfig is additive: present only when the journal recorded branch
	// evidence before the deletion (Spec 031 T045).
	BranchConfig *V3RestoreBranchConfig `json:"branch_config,omitempty"`
	// Outcome and RestoreJournal are present on an apply result only.
	Outcome        string              `json:"outcome,omitempty"`
	RestoreJournal *V3RestoreRecordRef `json:"restore_journal,omitempty"`
}

// V3RestoreCounts counts the in-scope entries per state.
type V3RestoreCounts struct {
	Restorable        int `json:"restorable"`
	AlreadyPresent    int `json:"already-present"`
	NameTaken         int `json:"name-taken"`
	RecoveryMissing   int `json:"recovery-missing"`
	JournalIncomplete int `json:"journal-incomplete"`
}

// V3RestoreResult is the candidate (unpublished) restore plan result.
type V3RestoreResult struct {
	SchemaVersion int              `json:"schema_version"`
	Command       string           `json:"command"`
	Status        string           `json:"status"`
	PlanID        string           `json:"plan_id"`
	Mode          string           `json:"mode"`
	Entries       []V3RestoreEntry `json:"entries"`
	Counts        V3RestoreCounts  `json:"counts"`
	Limits        []string         `json:"limits"`
	Diagnostics   []Diagnostic     `json:"diagnostics"`
}

func newV3RestoreResult(planID, mode string) V3RestoreResult {
	return V3RestoreResult{SchemaVersion: 1, Command: "tidy-restore", Status: "error", PlanID: planID, Mode: mode, Entries: []V3RestoreEntry{}, Limits: append([]string{}, restoreLimits...), Diagnostics: []Diagnostic{}}
}

// validRestoreRefName is the ref name check of FR-043: a full refs/heads/ name
// with git check-ref-format semantics, at most 4,096 bytes, no control
// characters, and a branch part that cannot be read as an option.
func validRestoreRefName(name string) bool {
	short, ok := strings.CutPrefix(name, restoreRefPrefix)
	return ok && short != "" && !strings.HasPrefix(short, "-") && v3Ref(name)
}

// validateV3RestoreOptions holds the argument errors that need no store or
// native work (contract: usage errors exit 2).
func validateV3RestoreOptions(o V3RestoreOptions) error {
	bad := func(why string) error { return failure("invalid-options", why) }
	if !v3CanonicalPath(o.StateDir) || !validID(o.PlanID) {
		return bad("A canonical state directory and a valid plan identity are required.")
	}
	seen := map[string]bool{}
	for _, a := range o.Actions {
		if !validID(a) || seen[a] {
			return bad("--action must name distinct valid action identities.")
		}
		seen[a] = true
	}
	if o.As != "" && (len(o.Actions) != 1 || !validRestoreRefName(o.As)) {
		return bad("--as needs exactly one --action and a valid refs/heads/ name.")
	}
	if o.Authorize && !o.Apply {
		return bad("--authorize applies only with --apply.")
	}
	if o.Apply && !o.Authorize {
		return failure("authorization-required", "Restore apply requires explicit authorization of the named local effects.")
	}
	return nil
}

// restoreRef is the value of one reference read without dereferencing.
type restoreRef struct {
	Present, Symbolic bool
	OID               string
}

// restoreNative runs the typed Git reads of a restore under the planning
// session of the context (constant arguments, private environment).
type restoreNative struct {
	bin     string
	id      NativeIdentity
	observe func(args []string)
}

const restoreRefChunk = 100

func (n restoreNative) run(ctx context.Context, args []string) (string, int, error) {
	if planningSessionFrom(ctx) == nil {
		return "", -1, failure("planning-environment-unavailable", "Restore reads run only under a planning session.")
	}
	if n.observe != nil {
		n.observe(args)
	}
	out, code, err := runSandboxed(ctx, n.bin, args, n.id.WorkspaceRoot, nil, nil)
	if err != nil {
		return "", -1, gitExecutionError(err)
	}
	return out, code, nil
}

// readRefs reads exact reference names with one for-each-ref per chunk. The
// symref field tells a symbolic reference from a direct one without following it.
func (n restoreNative) readRefs(ctx context.Context, names []string) (map[string]restoreRef, error) {
	want := map[string]bool{}
	ordered := []string{}
	for _, name := range names {
		if !want[name] {
			want[name] = true
			ordered = append(ordered, name)
		}
	}
	out := map[string]restoreRef{}
	for len(ordered) > 0 {
		chunk := ordered[:min(len(ordered), restoreRefChunk)]
		ordered = ordered[len(chunk):]
		args := append([]string{"for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)", "--"}, chunk...)
		text, code, err := n.run(ctx, args)
		if err != nil {
			return nil, err
		}
		if code != 0 {
			return nil, failure("git-query-refused", "Native reference reads were refused.")
		}
		for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
			if line == "" {
				continue
			}
			f := strings.Split(line, "\x00")
			if len(f) != 3 {
				return nil, failure("git-query-refused", "Native reference reads returned an unexpected record.")
			}
			if want[f[0]] {
				out[f[0]] = restoreRef{Present: true, Symbolic: f[2] != "", OID: f[1]}
			}
		}
	}
	return out, nil
}

// readableCommits asks one batched cat-file whether each object peels to a
// readable commit; the answers are in input order.
func (n restoreNative) readableCommits(ctx context.Context, oids []string, outputLimit int64) ([]bool, error) {
	if len(oids) == 0 {
		return []bool{}, nil
	}
	if planningSessionFrom(ctx) == nil {
		return nil, failure("planning-environment-unavailable", "Restore reads run only under a planning session.")
	}
	args := []string{"cat-file", "--batch-check"}
	if n.observe != nil {
		n.observe(args)
	}
	streamCtx, stop := context.WithCancel(ctx)
	defer stop()
	cmd, err := prepareSandboxed(streamCtx, n.bin, args, n.id.WorkspaceRoot, nil, nil)
	if err != nil {
		return nil, gitExecutionError(err)
	}
	var input strings.Builder
	for _, oid := range oids {
		input.WriteString(oid + "^{commit}\n")
	}
	results := make([]bool, 0, len(oids))
	_, _, err = executeNativeStreamExit(streamCtx, stop, cmd, strings.NewReader(input.String()), outputLimit, func(r io.Reader) error {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 4096), 1<<16)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			results = append(results, len(fields) == 3 && fields[1] == "commit")
		}
		return scanner.Err()
	}, nil, false)
	if err != nil {
		return nil, err
	}
	if len(results) != len(oids) {
		return nil, failure("git-query-refused", "Native object reads returned an unexpected number of records.")
	}
	return results, nil
}

// classifyRestore is the state decision after the journal checks, in the order
// of the contract: the recovery ref and its commit, then the target ref.
func classifyRestore(recovery restoreRef, oid string, readable bool, target restoreRef) (state string, reason string) {
	switch {
	case !recovery.Present:
		return restoreRecoveryMissing, "recovery-ref-absent"
	case recovery.Symbolic:
		return restoreRecoveryMissing, "recovery-ref-symbolic"
	case recovery.OID != oid:
		return restoreRecoveryMissing, "recovery-ref-retargeted"
	case !readable:
		return restoreRecoveryMissing, "object-unreadable"
	case target.Present && target.Symbolic:
		return restoreNameTaken, "symbolic-reference"
	case target.Present && target.OID == oid:
		return restoreAlreadyPresent, ""
	case target.Present:
		return restoreNameTaken, ""
	}
	return restoreRestorable, ""
}

func (c *V3RestoreCounts) add(state string) {
	switch state {
	case restoreRestorable:
		c.Restorable++
	case restoreAlreadyPresent:
		c.AlreadyPresent++
	case restoreNameTaken:
		c.NameTaken++
	case restoreRecoveryMissing:
		c.RecoveryMissing++
	case restoreJournalIncomplete:
		c.JournalIncomplete++
	}
}

func reasonOf(e V3RestoreEntry) string {
	if e.Reason == nil {
		return ""
	}
	return *e.Reason
}

func reasonPointer(reason string) *string {
	if reason == "" {
		return nil
	}
	return &reason
}

// restoreItem pairs a plan entry with the journal facts behind it.
type restoreItem struct {
	entry   V3RestoreEntry
	journal V3JournalEntry
	// origin is the branch the journal deleted; entry.Ref differs under --as.
	origin string
}

// restoreItems builds the in-scope entries in journal order. The branch name,
// tip and class come from the immutable plan candidate that the validated
// journal entry belongs to; the recovery ref name is the journal's own, which
// validation derived from the plan and the action.
func restoreItems(view v3JournalView, actions []string, as string) ([]restoreItem, error) {
	eligible := v3Eligible(view.Plan)
	if len(eligible) != len(view.Journal.Entries) {
		return nil, failure("journal-unreadable", "The journal does not match its plan.")
	}
	scope := map[string]bool{}
	for _, a := range actions {
		scope[a] = true
	}
	known := map[string]bool{}
	items := []restoreItem{}
	for i, je := range view.Journal.Entries {
		c := eligible[i]
		known[je.ActionID] = true
		if len(actions) > 0 && !scope[je.ActionID] {
			continue
		}
		ref := c.Name
		if as != "" {
			ref = as
		}
		e := V3RestoreEntry{ActionID: je.ActionID, Ref: ref, OID: c.OID, RecoveryRef: je.PlannedRecovery.Name, Class: c.Proof, Flags: []string{}}
		if c.Evidence != nil && c.Evidence.Integration != nil {
			e.Flags = append(e.Flags, c.Evidence.Integration.Flags...)
		}
		if d := decisionOf(c); d != nil {
			e.Decision = &V3RestoreDecision{Reviewer: d.Reviewer, Kind: d.Kind, Decision: d.Decision, Reason: d.Reason}
		}
		items = append(items, restoreItem{entry: e, journal: je, origin: c.Name})
	}
	for _, a := range actions {
		if !known[a] {
			return nil, failure("action-not-found", "An action is not in the journal of the plan.")
		}
	}
	return items, nil
}

// confirmedRecovery reports whether the entry reached a confirmed recovery.
func confirmedRecovery(je V3JournalEntry) bool { return je.Recovery != nil && je.Phase != "prepared" }

// evaluateRestoreItems classifies each item from fresh native reads. Entries
// that are incomplete are decided from the journal alone and cause no native read.
func evaluateRestoreItems(ctx context.Context, n restoreNative, items []restoreItem) error {
	read := []*V3RestoreEntry{}
	for i := range items {
		e := &items[i].entry
		switch {
		case !confirmedRecovery(items[i].journal):
			e.State, e.Reason = restoreJournalIncomplete, reasonPointer("no-confirmed-recovery")
		case !validRestoreRefName(e.Ref):
			e.State, e.Reason = restoreJournalIncomplete, reasonPointer("invalid-ref-name")
		default:
			read = append(read, e)
		}
	}
	if err := classifyRestoreEntries(ctx, n, read); err != nil {
		return err
	}
	return n.planBranchConfigs(ctx, items)
}

// classifyRestoreEntries sets the state of entries whose journal side is
// complete and whose ref name is valid: one batched reference read and one
// batched object read.
func classifyRestoreEntries(ctx context.Context, n restoreNative, entries []*V3RestoreEntry) error {
	if len(entries) == 0 {
		return nil
	}
	names, oids := []string{}, []string{}
	for _, e := range entries {
		names = append(names, e.RecoveryRef, e.Ref)
		oids = append(oids, e.OID)
	}
	refs, err := n.readRefs(ctx, names)
	if err != nil {
		return err
	}
	readable, err := n.readableCommits(ctx, oids, 4<<20)
	if err != nil {
		return err
	}
	for i, e := range entries {
		state, reason := classifyRestore(refs[e.RecoveryRef], e.OID, readable[i], refs[e.Ref])
		e.State, e.Reason = state, reasonPointer(reason)
	}
	return nil
}

// tidyV3RestoreCoordinator carries the seams of one restore operation; tests
// replace them to observe arguments and to race the apply effects.
type tidyV3RestoreCoordinator struct {
	identity    func(context.Context, string) (NativeIdentity, error)
	now         func() time.Time
	who         func() string
	observeArgs func([]string)
	// writeRecord stores one restore record; tests fail it.
	writeRecord func(*store, string, restoreRecord) error
	// afterEvaluate runs once the effects are computed, beforeCAS between the
	// last re-evaluation and the compare-and-swap, afterIntent and afterCAS
	// after the intent and after the effect (a returned error simulates a crash).
	afterEvaluate         func()
	beforeCAS             func(V3RestoreEntry)
	afterIntent, afterCAS func(V3RestoreEntry) error
}

func defaultTidyV3RestoreCoordinator() tidyV3RestoreCoordinator {
	return tidyV3RestoreCoordinator{identity: gitNativeIdentity, now: time.Now}
}

// ValidateV3RestoreOptions reports the argument errors of a restore command,
// which need no store or native work (the CLI exits 2 on them).
func ValidateV3RestoreOptions(o V3RestoreOptions) error { return validateV3RestoreOptions(o) }

// RestoreTidyV3 plans (read-only) the restore of deleted branches.
func RestoreTidyV3(ctx context.Context, o V3RestoreOptions) V3RestoreResult {
	return defaultTidyV3RestoreCoordinator().restore(ctx, o)
}

func failRestore(r V3RestoreResult, err error) V3RestoreResult {
	failed := failResult(newResult(r.Command), err)
	code := failed.Diagnostics[0].Code
	r.Status, r.Diagnostics, r.Entries, r.Counts = "error", failed.Diagnostics, []V3RestoreEntry{}, V3RestoreCounts{}
	switch code {
	case "invalid-options", "authorization-required", "plan-not-found", "action-not-found", "unsupported-mode", "journal-unreadable", "git-hooks-refused", "git-unsafe-configuration":
		r.Status = "refused"
	}
	return r
}

// restoreRepository resolves PATH to the clone to restore into. The journal does
// not have to come from this clone: a re-clone simply has no recovery refs.
func restoreRepository(ctx context.Context, identity func(context.Context, string) (NativeIdentity, error), observe func([]string), path, stateDir string) (restoreNative, error) {
	path, err := canonicalSource(path)
	if err != nil {
		return restoreNative{}, err
	}
	id, err := identity(ctx, path)
	if err != nil {
		return restoreNative{}, err
	}
	for _, root := range []string{id.WorkspaceRoot, id.RepositoryDir, id.GitDir} {
		if v3Overlaps(stateDir, root) {
			return restoreNative{}, failure("state-overlap", "The private state directory overlaps the repository.")
		}
	}
	bin, err := gitExecutable()
	if err != nil {
		return restoreNative{}, err
	}
	return restoreNative{bin: bin, id: id, observe: observe}, nil
}

// readRestoreJournal opens the store read-only and maps its failures to the
// refusals of the contract.
func readRestoreJournal(stateDir, planID string) (v3JournalView, error) {
	s, err := openV3ReadStore(stateDir)
	if err != nil {
		return v3JournalView{}, err
	}
	defer s.Close()
	view, err := s.readJournalView(planID)
	switch {
	case err == nil:
		return view, nil
	case errors.Is(err, errV3JournalAbsent):
		return view, failure("plan-not-found", "No stored tidy journal has this plan identity.")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return view, err
	}
	return view, failure("journal-unreadable", "The stored journal of the plan could not be read.")
}

func (c tidyV3RestoreCoordinator) restore(ctx context.Context, o V3RestoreOptions) V3RestoreResult {
	mode := "plan"
	if o.Apply {
		mode = "apply"
	}
	r := newV3RestoreResult(o.PlanID, mode)
	if err := validateV3RestoreOptions(o); err != nil {
		return failRestore(r, err)
	}
	// An apply spends about four native processes per entry and at most 1,024
	// entries exist; a rerun continues where a deadline stopped it.
	deadline := 60 * time.Second
	if o.Apply {
		deadline = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	ctx, _, closeSession, err := withPlanningSession(ctx)
	if err != nil {
		return failRestore(r, err)
	}
	defer closeSession()
	n, err := restoreRepository(ctx, c.identity, c.observeArgs, o.Path, o.StateDir)
	if err != nil {
		return failRestore(r, err)
	}
	view, err := readRestoreJournal(o.StateDir, o.PlanID)
	if err != nil {
		return failRestore(r, err)
	}
	if view.Plan.Mode != "local-refs" {
		return failRestore(r, failure("unsupported-mode", "Only local-reference journals can be restored."))
	}
	items, err := restoreItems(view, o.Actions, o.As)
	if err != nil {
		return failRestore(r, err)
	}
	if err = evaluateRestoreItems(ctx, n, items); err != nil {
		return failRestore(r, err)
	}
	if o.Apply {
		return c.applyRestore(ctx, n, o, view, items, r)
	}
	for _, item := range items {
		r.Entries = append(r.Entries, item.entry)
		r.Counts.add(item.entry.State)
	}
	r.Status = "ok"
	return r
}
