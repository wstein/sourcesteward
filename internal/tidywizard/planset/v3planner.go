// SPDX-License-Identifier: EUPL-1.2

package planset

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// Reasons a sub-plan cannot be mapped to the existing plan kinds. They are stable codes: the
// wizard shows the item as retained with the code, and nothing is guessed.
const (
	ReasonRepositoryUnknown     = "plan-repository-unknown"
	ReasonBaseMissing           = "plan-base-missing"
	ReasonPlanStoreMissing      = "plan-state-dir-missing"
	ReasonLegacyOwnerUnknown    = "legacy-owner-unknown"
	ReasonTrackingRemoteMissing = "tracking-remote-missing"
	ReasonTrackingOnline        = "tracking-online-required"
	ReasonTrackingRefUnmappable = "tracking-ref-unmappable"
	ReasonBranchUnmappable      = "branch-name-unmappable"
	ReasonStaleNoPlanKind       = "stale-registration-no-plan-kind"
)

// Unplannable means a selected item cannot become a sub-plan; Reason is the stable code.
type Unplannable struct {
	Reason, Detail string
}

func (u *Unplannable) Error() string { return u.Reason + ": " + u.Detail }

func unplannable(reason, format string, args ...any) error {
	return fmt.Errorf("%w: %w", ErrNotPlannable, &Unplannable{Reason: reason, Detail: fmt.Sprintf(format, args...)})
}

// RepoSpec is what the mapping needs to know about one repository, from the derivation: the path,
// the derived base reference, and the remote and online selection for tracking references.
type RepoSpec struct {
	Name, Path, Base string
	Remote           string
	Online           bool
}

// V3Planner is the Planner over workspace.PlanTidyV3: it maps a sub-plan spec to the options of
// the existing Spec 011/029/031 plan kinds and stores the plan in the private state directory.
// PlanStore is the first state directory (where plans of branches, tracking references and
// legacy completions are written); an owned retirement is planned in the store that holds its
// creation record. RecordStore finds that store (NewV3Planner wires it to the derived stores) and
// returns a derivation reason code instead when the record is missing or ambiguous.
type V3Planner struct {
	Repos       map[string]RepoSpec
	PlanStore   string
	RecordStore func(task string) (storePath, reason string)
	PlanFunc    func(context.Context, workspace.V3PlanOptions) workspace.V3Result
}

// NewV3Planner returns the production planner over the validated state directories (the first is
// the plan store).
func NewV3Planner(repos []RepoSpec, stores []derive.Store) V3Planner {
	p := V3Planner{Repos: map[string]RepoSpec{}, PlanFunc: workspace.PlanTidyV3}
	for _, r := range repos {
		p.Repos[r.Name] = r
	}
	for _, s := range stores {
		if s.Issue == nil {
			p.PlanStore = s.Path
			break
		}
	}
	p.RecordStore = func(task string) (string, string) {
		l := derive.LookupTask(stores, task)
		if l.Ambiguity != nil {
			return "", l.Ambiguity.Code
		}
		for _, s := range stores {
			if s.Issue != nil {
				continue
			}
			if _, ok, err := workspace.ReadTask(s.Path, task); err == nil && ok {
				return s.Path, ""
			}
		}
		return "", "registration-record-missing"
	}
	return p
}

func validLabel(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case i > 0 && (c == '.' || c == '_' || c == '-'):
		default:
			return false
		}
	}
	return true
}

// Options maps one sub-plan spec to the options of the existing plan kind. It reads nothing and
// plans nothing. An input that is not supplied is a typed *Unplannable (inside ErrNotPlannable)
// with a stable reason, never a guess.
func (p V3Planner) Options(s SubSpec) (workspace.V3PlanOptions, error) {
	repo, ok := p.Repos[s.Repo]
	if !ok || repo.Path == "" {
		return workspace.V3PlanOptions{}, unplannable(ReasonRepositoryUnknown, "repository %q has no path", s.Repo)
	}
	if repo.Base == "" {
		return workspace.V3PlanOptions{}, unplannable(ReasonBaseMissing, "repository %q has no derived base", s.Repo)
	}
	o := workspace.V3PlanOptions{Path: repo.Path, Base: repo.Base, ProtectWorktrees: []string{}}
	if s.Kind == KindStaleWorktrees {
		return o, unplannable(ReasonStaleNoPlanKind, "no existing plan kind removes a registration whose directory is missing")
	}
	if len(s.Items) == 0 {
		return o, unplannable(ReasonRepositoryUnknown, "the sub-plan covers no item")
	}
	if s.Kind == KindRetire {
		return p.retirement(o, s)
	}
	if p.PlanStore == "" {
		return o, unplannable(ReasonPlanStoreMissing, "no state directory to write the plan to")
	}
	o.StateDir = p.PlanStore
	switch s.Kind {
	case KindBranches:
		for _, c := range s.Items {
			if c.Branch == "" || strings.HasPrefix(c.Branch, "refs/") {
				return o, unplannable(ReasonBranchUnmappable, "branch %q is not a short branch name", c.Branch)
			}
			o.Refs = append(o.Refs, "refs/heads/"+c.Branch)
		}
	case KindTracking:
		if repo.Remote == "" {
			return o, unplannable(ReasonTrackingRemoteMissing, "repository %q has no derived remote", s.Repo)
		}
		if !repo.Online {
			return o, unplannable(ReasonTrackingOnline, "tracking references are planned only with --online")
		}
		prefix := "refs/remotes/" + repo.Remote + "/"
		for _, c := range s.Items {
			if !strings.HasPrefix(c.Branch, prefix) || len(c.Branch) == len(prefix) {
				return o, unplannable(ReasonTrackingRefUnmappable, "%q is not a reference of remote %q", c.Branch, repo.Remote)
			}
			o.Refs = append(o.Refs, c.Branch)
		}
		o.TrackingRemote, o.Online = repo.Remote, true
	default:
		return o, unplannable(ReasonRepositoryUnknown, "unknown sub-plan kind %q", s.Kind)
	}
	sort.Strings(o.Refs)
	return o, nil
}

func (p V3Planner) retirement(o workspace.V3PlanOptions, s SubSpec) (workspace.V3PlanOptions, error) {
	if len(s.Items) != 1 || s.Worktree == "" || s.Worktree != s.Items[0].Path {
		return o, unplannable(ReasonRepositoryUnknown, "a retirement covers exactly one worktree")
	}
	c := s.Items[0]
	o.RemoveRegistration, o.RemoveFiles = true, s.Worktree
	if c.Task != "" && c.OwnerSource == owner.SourceTaskRecord {
		if p.RecordStore == nil {
			return o, unplannable("registration-record-missing", "no creation record lookup")
		}
		store, reason := p.RecordStore(c.Task)
		if reason != "" {
			return o, unplannable(reason, "task %q", c.Task)
		}
		o.StateDir, o.Task, o.Owner = store, c.Task, c.Owner
		return o, nil
	}
	// An unregistered worktree: the Spec 029 legacy completion, with the owner the candidate
	// carries. A label that is not one plain identifier (an unknown owner, a commit author hint)
	// is never turned into one.
	if !validLabel(c.Owner) || c.Scope == owner.ScopeUnknown {
		return o, unplannable(ReasonLegacyOwnerUnknown, "worktree %q has no owner label that can be declared", s.Worktree)
	}
	if p.PlanStore == "" {
		return o, unplannable(ReasonPlanStoreMissing, "no state directory to write the plan to")
	}
	o.StateDir, o.CompletedWorktree, o.Owner = p.PlanStore, s.Worktree, c.Owner
	return o, nil
}

// Preflight reports, without planning anything, whether the sub-plan can be mapped.
func (p V3Planner) Preflight(s SubSpec) error {
	_, err := p.Options(s)
	return err
}

// Plan maps and plans the sub-plan through PlanTidyV3, which stores it as an ordinary v3 plan,
// and returns its id. Every selected item must be deletable by the plan's own judgement: a plan
// that retains one is not stored as a sub-plan of the set, its reason is returned.
func (p V3Planner) Plan(ctx context.Context, s SubSpec) (string, error) {
	o, err := p.Options(s)
	if err != nil {
		return "", err
	}
	res := p.PlanFunc(ctx, o)
	if res.Status != "ok" || res.Plan == nil {
		code, msg := res.Status, "no plan"
		if len(res.Diagnostics) > 0 {
			code, msg = res.Diagnostics[0].Code, res.Diagnostics[0].Message
		}
		return "", fmt.Errorf("planning %s sub-plan of %s: %s: %s", s.Kind, s.Repo, code, msg)
	}
	if reason := notDeletable(res.Plan, s.Kind); reason != "" {
		return "", unplannable(reason, "the %s plan of %s retains an item", s.Kind, s.Repo)
	}
	return res.Plan.PlanID, nil
}

// notDeletable returns the reason of the first item the plan retains, or "".
func notDeletable(plan *workspace.V3Plan, kind SubKind) string {
	if kind == KindRetire {
		r := plan.Retirement
		if r == nil || len(r.Actions) != 2 {
			return "retirement-not-eligible"
		}
		for _, a := range r.Actions {
			if a.Decision != "delete" {
				return nonEmpty(a.Reason, "retirement-not-eligible")
			}
		}
		return ""
	}
	if len(plan.Candidates) == 0 {
		return "no-candidate"
	}
	for _, c := range plan.Candidates {
		if c.Decision != "delete" {
			return nonEmpty(c.Reason, "retained")
		}
	}
	return ""
}

func nonEmpty(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

var (
	_ Planner   = V3Planner{}
	_ Preflight = V3Planner{}
)
