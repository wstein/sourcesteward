// SPDX-License-Identifier: EUPL-1.2

// Package planset is the plan set of the stw tidy wizard (Spec 041, plan set and apply): a
// manifest of independent sub-plans (one per repository per effect kind, retirement sub-plans one
// per worktree) with a content-addressed plan id over the sub-plans, the derived values, the
// selected items with their owner markers, authorized_scopes and the mode evidence. Building a
// set asks a Planner for the sub-plans (the existing Spec 011 and 029 plan kinds) and never
// deletes anything; the set is stored in the private state directory only on request. Applying a
// set is a later task.
package planset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// SchemaVersion is the version of the stored manifest.
const SchemaVersion = 1

// Bounds of a set. They follow the candidate bound of the overview.
const (
	MaxItems    = tidywizard.MaxCandidates
	MaxSubPlans = tidywizard.MaxCandidates
)

// SubKind is the effect kind of a sub-plan.
type SubKind string

// The effect kinds. The order of apply is retirements, branches, tracking references (see
// Ordered). A stale registration has no plan kind (T012b).
const (
	KindRetire   SubKind = "retire"
	KindBranches SubKind = "branches"
	KindTracking SubKind = "tracking"
)

var rank = map[SubKind]int{KindRetire: 0, KindBranches: 1, KindTracking: 2}

// Errors of building, storing and loading a set.
var (
	// ErrNotPlannable means a chosen item may not enter a plan in this mode or verdict.
	ErrNotPlannable = errors.New("item cannot be planned")
	// ErrInvalid means the set is incoherent.
	ErrInvalid = errors.New("invalid plan set")
	// ErrIDMismatch means the plan id does not match the content (the stored set was edited or
	// stored under another name).
	ErrIDMismatch = errors.New("plan id does not match the plan set content")
	// ErrNotFound means the state directory holds no such set.
	ErrNotFound = errors.New("plan set not found")
)

// Item is a selected candidate with the markers apply needs: the owner, its source and the scope
// are part of the content-addressed id, so a relabelled item changes the id.
type Item struct {
	ID          string       `json:"id"`
	Repo        string       `json:"repo"`
	Kind        string       `json:"kind"`
	Owner       string       `json:"owner"`
	OwnerSource owner.Source `json:"owner_source"`
	Scope       owner.Scope  `json:"scope"`
	Path        string       `json:"path"`
	Branch      string       `json:"branch"`
	Task        string       `json:"task"`
	// Effects are the effects the owner chose with the item (for example delete-branch:NAME for
	// the dependent branch leg of a worktree retirement). They are bound to the plan id.
	Effects []string `json:"effects"`
}

// key is the canonical JSON of the whole item, so that no stored field can change without
// changing the plan id.
func (i Item) key() string {
	b, _ := json.Marshal(i) // plain strings: cannot fail
	return string(b)
}

// SubPlan is one independent sub-plan: ID is the id of the stored plan the Planner made (a Spec
// 011 or 029 plan) and StateDir the private state directory that holds it, Items the ids of the selected items it covers, Worktree the one worktree of a
// retirement.
type SubPlan struct {
	ID       string   `json:"id"`
	StateDir string   `json:"state_dir"`
	Repo     string   `json:"repo"`
	Kind     SubKind  `json:"kind"`
	Worktree string   `json:"worktree"`
	Items    []string `json:"items"`
}

func (s SubPlan) key() string {
	items := append([]string{}, s.Items...)
	sort.Strings(items)
	return strings.Join([]string{s.ID, s.StateDir, string(s.Kind), s.Repo, s.Worktree, strings.Join(items, "\x1e")}, "\x1f")
}

// Set is the stored plan set.
type Set struct {
	SchemaVersion    int              `json:"schema_version"`
	PlanID           string           `json:"plan_id"`
	SubPlans         []SubPlan        `json:"sub_plans"`
	Derived          []derive.Derived `json:"derived"`
	Items            []Item           `json:"items"`
	AuthorizedScopes []owner.Scope    `json:"authorized_scopes"`
	Mode             owner.Evidence   `json:"mode"`
}

func (s Set) computeID() string {
	return s.computeDigest()[:12]
}

func (s Set) computeDigest() string {
	subs := make([]string, 0, len(s.SubPlans))
	for _, p := range s.SubPlans {
		subs = append(subs, p.key())
	}
	selected := make([]string, 0, len(s.Items))
	for _, i := range s.Items {
		selected = append(selected, i.key())
	}
	return owner.PlanDigest(owner.PlanContent{SubPlans: subs, Derived: s.Derived, Selected: selected, AuthorizedScopes: s.AuthorizedScopes, Mode: s.Mode})
}

// NeedsAuthorization reports whether the set holds an item of scope other or unknown. Apply
// refuses such a set in agent mode and without authorized_scopes (a later task).
func (s Set) NeedsAuthorization() bool {
	for _, i := range s.Items {
		if i.Scope != owner.ScopeOwn {
			return true
		}
	}
	return false
}

// Ordered returns the sub-plans in the fixed apply order: retirements, branches,
// tracking references; within a kind by repository, worktree and id.
func (s Set) Ordered() []SubPlan {
	out := append([]SubPlan{}, s.SubPlans...)
	sort.SliceStable(out, func(a, b int) bool {
		x, y := out[a], out[b]
		switch {
		case rank[x.Kind] != rank[y.Kind]:
			return rank[x.Kind] < rank[y.Kind]
		case x.Repo != y.Repo:
			return x.Repo < y.Repo
		case x.Worktree != y.Worktree:
			return x.Worktree < y.Worktree
		}
		return x.ID < y.ID
	})
	return out
}

func validPlanID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for i, c := range []byte(id) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case i > 0 && (c == '.' || c == '_' || c == '-'):
		default:
			return false
		}
	}
	return true
}

// Validate checks that the set is coherent: known kinds, plain sub-plan ids without repeats, one
// worktree per retirement, every item in exactly one sub-plan and every sub-plan item selected,
// scopes that need authorization only (and only in human mode).
func (s Set) Validate() error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
	}
	if s.SchemaVersion != SchemaVersion {
		return bad("schema version %d", s.SchemaVersion)
	}
	if len(s.SubPlans) == 0 || len(s.Items) == 0 || len(s.SubPlans) > MaxSubPlans || len(s.Items) > MaxItems {
		return bad("%d sub-plans and %d items are outside 1..%d", len(s.SubPlans), len(s.Items), MaxItems)
	}
	items := map[string]Item{}
	for _, i := range s.Items {
		if _, dup := items[i.ID]; i.ID == "" || dup {
			return bad("item %q is empty or repeated", i.ID)
		}
		items[i.ID] = i
	}
	covered := map[string]bool{}
	seen := map[string]bool{}
	worktrees := map[string]bool{}
	for _, p := range s.SubPlans {
		if _, ok := rank[p.Kind]; !ok {
			return bad("unknown sub-plan kind %q", p.Kind)
		}
		if !validPlanID(p.ID) || seen[p.ID] {
			return bad("sub-plan id %q is not a plain unique identifier", p.ID)
		}
		if !filepath.IsAbs(p.StateDir) || filepath.Clean(p.StateDir) != p.StateDir {
			return bad("sub-plan %s has no canonical state directory", p.ID)
		}
		seen[p.ID] = true
		if len(p.Items) == 0 {
			return bad("sub-plan %s covers no item", p.ID)
		}
		if p.Kind == KindRetire {
			if p.Worktree == "" || len(p.Items) != 1 || worktrees[p.Repo+"\x00"+p.Worktree] {
				return bad("a retirement sub-plan covers exactly one distinct worktree (%s)", p.ID)
			}
			worktrees[p.Repo+"\x00"+p.Worktree] = true
		} else if p.Worktree != "" {
			return bad("only a retirement names a worktree (%s)", p.ID)
		}
		for _, id := range p.Items {
			item, ok := items[id]
			if !ok || covered[id] {
				return bad("item %q of sub-plan %s is not selected or covered twice", id, p.ID)
			}
			if kind, known := subKind(tidywizard.Kind(item.Kind)); !known || kind != p.Kind || item.Repo != p.Repo {
				return bad("item %q (repository %q, kind %q) does not belong to sub-plan %s (%s, %s)", id, item.Repo, item.Kind, p.ID, p.Repo, p.Kind)
			}
			if p.Kind == KindRetire && item.Path != p.Worktree {
				return bad("item %q names worktree %q, not the one of sub-plan %s", id, item.Path, p.ID)
			}
			covered[id] = true
		}
	}
	if len(covered) != len(items) {
		return bad("%d selected items are in no sub-plan", len(items)-len(covered))
	}
	if len(s.AuthorizedScopes) > 0 && s.Mode.Mode != owner.ModeHuman {
		return bad("only the human path writes authorized_scopes")
	}
	carried := map[owner.Scope]bool{}
	for _, i := range s.Items {
		carried[i.Scope] = true
	}
	for _, sc := range s.AuthorizedScopes {
		if sc != owner.ScopeOther && sc != owner.ScopeUnknown {
			return bad("scope %q does not need authorization", sc)
		}
		if !carried[sc] {
			return bad("authorized scope %q is carried by no selected item", sc)
		}
	}
	return nil
}

// SubSpec asks the Planner for one sub-plan. Items are the selected candidates it covers, sorted
// by id; Worktree is set for a retirement.
type SubSpec struct {
	Repo     string
	Kind     SubKind
	Worktree string
	Items    []tidywizard.Candidate
}

// Planner makes and stores one sub-plan (the existing Spec 011/029 plan kinds) and returns its
// plan id and state directory. It is the seam to internal/workspace; it must not apply anything.
type Planner interface {
	Plan(ctx context.Context, spec SubSpec) (Planned, error)
}

// Planned is a stored sub-plan: its plan id and the state directory that holds it.
type Planned struct {
	ID, StateDir string
}

// Preflight is an optional interface of a Planner: it checks, without planning anything, that a
// sub-plan can be made. Build asks it for every sub-plan before the first one is planned, so a
// selection that cannot be mapped stores nothing.
type Preflight interface {
	Preflight(spec SubSpec) error
}

// Admitter is an optional interface of a Planner: Admit runs every observation and judgement of
// the sub-plan that Plan runs, up to the point where the plan would be stored, and stores nothing.
// Build and Admit ask it for every sub-plan before the first one is planned, so a refusal that is
// known before storing leaves nothing stored. The dry run stops before the store is opened, so a
// failure at that step, or a tree that changes meanwhile, can still leave earlier sub-plans stored.
type Admitter interface {
	Admit(ctx context.Context, spec SubSpec) error
}

// Request is what Build needs. Chosen are candidate ids (the wizard maps numbers to ids).
// DecisionConfirmed names the decision rows the second confirmation covered. Confirm opens the
// terminal for the typed confirmation of foreign items; it is only used when Chosen has one.
type Request struct {
	Candidates        []tidywizard.Candidate
	Chosen            []string
	Derived           []derive.Derived
	Mode              owner.Evidence
	DecisionConfirmed map[string]bool
	Confirm           owner.Opener
}

func entry(c tidywizard.Candidate) owner.Entry {
	return owner.Entry{ID: c.ID, Owner: owner.Owner{Label: c.Owner, Source: c.OwnerSource, Scope: c.Scope}}
}

// SelectSafe returns the ids a non-interactive run stores (--save) or the wizard's "all" answer
// selects: every safe item of scope own, and in agent mode only those whose owner came from the
// task record. Other, unknown, retain and decision rows are never included, and neither is a kind
// with no plan kind (a stale registration, T012b), so the selection always builds.
func SelectSafe(cands []tidywizard.Candidate, ev owner.Evidence) []string {
	entries := []owner.Entry{}
	for _, c := range cands {
		if _, plannable := subKind(c.Kind); plannable && c.Verdict == tidywizard.VerdictSafe && c.Scope == owner.ScopeOwn {
			entries = append(entries, entry(c))
		}
	}
	out := []string{}
	for _, e := range owner.Plannable(entries, ev) {
		out = append(out, e.ID)
	}
	sort.Strings(out)
	return out
}

func subKind(k tidywizard.Kind) (SubKind, bool) {
	switch k {
	case tidywizard.KindWorktree:
		return KindRetire, true
	case tidywizard.KindBranch:
		return KindBranches, true
	case tidywizard.KindTrackingRef:
		return KindTracking, true
	}
	return "", false
}

// choose validates the selection against the candidates and the mode and returns the chosen
// candidates sorted by id.
func choose(req Request) ([]tidywizard.Candidate, error) {
	byID := map[string]tidywizard.Candidate{}
	for _, c := range req.Candidates {
		byID[c.ID] = c
	}
	picked := map[string]bool{}
	var out []tidywizard.Candidate
	for _, id := range req.Chosen {
		if picked[id] {
			continue
		}
		picked[id] = true
		c, ok := byID[id]
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: %q is not a candidate", ErrNotPlannable, id)
		case c.Verdict == tidywizard.VerdictRetain || len(c.Effects) == 0:
			return nil, fmt.Errorf("%w: %q is retained and carries no effect", ErrNotPlannable, id)
		case c.Verdict == tidywizard.VerdictDecision && !req.DecisionConfirmed[id]:
			return nil, fmt.Errorf("%w: %q is a decision row without its second confirmation", ErrNotPlannable, id)
		case c.Verdict != tidywizard.VerdictSafe && c.Verdict != tidywizard.VerdictDecision:
			return nil, fmt.Errorf("%w: %q has verdict %q", ErrNotPlannable, id, c.Verdict)
		}
		if c.Kind == tidywizard.KindStaleWorktree {
			return nil, unplannable(ReasonStaleNoPlanKind, "no existing plan kind removes a registration whose directory is missing (%q)", id)
		}
		if _, ok := subKind(c.Kind); !ok {
			return nil, fmt.Errorf("%w: %q has kind %q", ErrNotPlannable, id, c.Kind)
		}
		if len(owner.Plannable([]owner.Entry{entry(c)}, req.Mode)) == 0 {
			return nil, fmt.Errorf("%w: %q (owner %q, scope %q, source %q) is not plannable in %s mode", ErrNotPlannable, id, c.Owner, c.Scope, c.OwnerSource, req.Mode.Mode)
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: nothing is chosen", ErrNotPlannable)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out, nil
}

// assemble makes the sub-plans for the chosen items and returns the set with its id. It does not
// ask for the typed confirmation.
func assemble(ctx context.Context, req Request, planner Planner, checkOnly bool) (Set, error) {
	chosen, err := choose(req)
	if err != nil {
		return Set{}, err
	}
	scopes := map[owner.Scope]bool{}
	for _, c := range chosen {
		if c.Scope != owner.ScopeOwn {
			scopes[c.Scope] = true
		}
	}
	var authorized []owner.Scope
	if len(scopes) > 0 {
		list := []owner.Scope{}
		for sc := range scopes {
			list = append(list, sc)
		}
		if authorized, err = owner.Authorize(req.Mode, list...); err != nil {
			return Set{}, fmt.Errorf("%w: %v", ErrNotPlannable, err)
		}
	}
	type group struct {
		repo, worktree string
		kind           SubKind
		items          []tidywizard.Candidate
	}
	groups := map[string]*group{}
	var order []string
	for _, c := range chosen {
		kind, _ := subKind(c.Kind)
		key := string(kind) + "\x00" + c.Repo
		if kind == KindRetire {
			key += "\x00" + c.Path
		}
		g := groups[key]
		if g == nil {
			g = &group{repo: c.Repo, kind: kind}
			if kind == KindRetire {
				g.worktree = c.Path
			}
			groups[key] = g
			order = append(order, key)
		}
		g.items = append(g.items, c)
	}
	set := Set{SchemaVersion: SchemaVersion, SubPlans: []SubPlan{}, Derived: append([]derive.Derived{}, req.Derived...), Items: []Item{}, AuthorizedScopes: authorized, Mode: req.Mode}
	if set.AuthorizedScopes == nil {
		set.AuthorizedScopes = []owner.Scope{}
	}
	if set.Derived == nil {
		set.Derived = []derive.Derived{}
	}
	for _, c := range chosen {
		set.Items = append(set.Items, Item{ID: c.ID, Repo: c.Repo, Kind: string(c.Kind), Owner: c.Owner, OwnerSource: c.OwnerSource, Scope: c.Scope, Path: c.Path, Branch: c.Branch, Task: c.Task, Effects: append([]string{}, c.Effects...)})
	}
	// Every sub-plan is checked before any is planned: first the mapping, then the full read-only
	// admission (the plan's own judgement without storing it). Every failure is collected.
	var failures []*SubPlanFailure
	for _, key := range order {
		g := groups[key]
		spec := SubSpec{Repo: g.repo, Kind: g.kind, Worktree: g.worktree, Items: g.items}
		var cause error
		if pre, ok := planner.(Preflight); ok {
			cause = pre.Preflight(spec)
		}
		if adm, ok := planner.(Admitter); ok && cause == nil {
			cause = adm.Admit(ctx, spec)
		}
		if cause != nil {
			f := &SubPlanFailure{Repo: g.repo, Kind: g.kind, Worktree: g.worktree, Err: cause}
			for _, c := range g.items {
				f.Items = append(f.Items, c.ID)
			}
			failures = append(failures, f)
		}
	}
	if len(failures) > 0 {
		return Set{}, &Refused{Failures: failures}
	}
	if checkOnly {
		return set, nil
	}
	for _, key := range order {
		g := groups[key]
		planned, err := planner.Plan(ctx, SubSpec{Repo: g.repo, Kind: g.kind, Worktree: g.worktree, Items: g.items})
		if err != nil {
			f := &SubPlanFailure{Repo: g.repo, Kind: g.kind, Worktree: g.worktree, Err: err}
			for _, c := range g.items {
				f.Items = append(f.Items, c.ID)
			}
			return Set{}, &Refused{Failures: []*SubPlanFailure{f}}
		}
		if !validPlanID(planned.ID) {
			return Set{}, fmt.Errorf("%w: the planner returned the sub-plan id %q", ErrInvalid, planned.ID)
		}
		sp := SubPlan{ID: planned.ID, StateDir: planned.StateDir, Repo: g.repo, Kind: g.kind, Worktree: g.worktree, Items: []string{}}
		for _, c := range g.items {
			sp.Items = append(sp.Items, c.ID)
		}
		set.SubPlans = append(set.SubPlans, sp)
	}
	set.SubPlans = set.Ordered()
	if err := set.Validate(); err != nil {
		return Set{}, err
	}
	set.PlanID = set.computeID()
	return set, nil
}

// Admit runs the checks of Build on the chosen items, up to the full read-only admission of every
// sub-plan, and plans nothing, stores nothing and asks no typed word. A nil result means Build
// meets no refusal that is known before the confirmation; a refusal is a *Refused that names every
// failing sub-plan with the ids of its rows.
func Admit(ctx context.Context, req Request, planner Planner) error {
	_, err := assemble(ctx, req, planner, true)
	return err
}

// Receipt is the in-memory confirmation of the foreign words of one built set. Copies share
// its single-use state. A nil receipt or a zero value never covers a typed prompt.
type Receipt struct {
	state *receiptState
}

type receiptState struct {
	digest string
	words  []string
	used   atomic.Bool
}

// consume takes the shared state once, including when Apply will refuse before loading a set.
func (r *Receipt) consume() *receiptState {
	if r == nil || r.state == nil || r.state.used.Swap(true) {
		return nil
	}
	return r.state
}

// Build plans the chosen items into a set. A chosen item of scope other or unknown needs the
// human path and the typed confirmation read from the terminal Confirm opens (the label or
// "unknown" plus the first 6 characters of the plan id, asked once per word); without a terminal
// the build fails before any sub-plan is made, and a wrong answer fails after the sub-plans were
// planned (they are ordinary stored plans that nothing applies). Build returns a receipt only
// after every word was confirmed; own-only sets return none. Build never deletes.
func Build(ctx context.Context, req Request, planner Planner) (Set, *Receipt, error) {
	chosen, err := choose(req)
	if err != nil {
		return Set{}, nil, err
	}
	words := map[string]bool{}
	for _, c := range chosen {
		if c.Scope != owner.ScopeOwn {
			words[owner.ConfirmWord(owner.Owner{Label: c.Owner, Source: c.OwnerSource, Scope: c.Scope})] = true
		}
	}
	if len(words) > 0 {
		if req.Mode.Mode != owner.ModeHuman {
			return Set{}, nil, fmt.Errorf("%w: %v", ErrNotPlannable, owner.ErrAgentMode)
		}
		if err := terminalAvailable(req.Confirm); err != nil {
			return Set{}, nil, err
		}
	}
	set, err := assemble(ctx, req, planner, false)
	if err != nil {
		return Set{}, nil, err
	}
	list := make([]string, 0, len(words))
	for w := range words {
		list = append(list, w)
	}
	sort.Strings(list)
	for _, w := range list {
		if err := owner.Confirm(req.Confirm, w, set.PlanID); err != nil {
			return Set{}, nil, err
		}
	}
	if len(list) == 0 {
		return set, nil, nil
	}
	return set, &Receipt{state: &receiptState{digest: set.computeDigest(), words: list}}, nil
}

func terminalAvailable(open owner.Opener) error {
	if open == nil {
		return owner.ErrNoTerminal
	}
	t, err := open()
	if err != nil {
		if errors.Is(err, owner.ErrNoTerminal) {
			return err
		}
		return fmt.Errorf("%w: %v", owner.ErrNoTerminal, err)
	}
	closeQuietly(t)
	return nil
}

func closeQuietly(c io.Closer) { _ = c.Close() }

// Store writes the set to the private state directory under its plan id. A set that is invalid or
// whose id does not match its content is refused and nothing is written.
func Store(stateDir string, set Set) error {
	if err := set.Validate(); err != nil {
		return err
	}
	if set.PlanID != set.computeID() {
		return ErrIDMismatch
	}
	b, err := json.Marshal(set)
	if err != nil {
		return err
	}
	return workspace.WriteTidyPlanSet(stateDir, set.PlanID, b)
}

// Load reads the set with that id from the state directory, decodes it strictly, validates it and
// recomputes the id from the content: an edited entry (authorized_scopes included) or one stored
// under another name is refused with ErrIDMismatch.
func Load(stateDir, id string) (Set, error) {
	raw, err := workspace.ReadTidyPlanSet(stateDir, id)
	if err != nil {
		if errors.Is(err, workspace.ErrPlanSetNotFound) {
			return Set{}, ErrNotFound
		}
		return Set{}, err
	}
	var set Set
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&set); err != nil {
		return Set{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := set.Validate(); err != nil {
		return Set{}, err
	}
	if set.PlanID != id || set.computeID() != id {
		return Set{}, ErrIDMismatch
	}
	return set, nil
}
