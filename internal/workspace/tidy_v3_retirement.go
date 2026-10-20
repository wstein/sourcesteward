// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
)

// Each operation owns its observer, deadline and non-resetting content ledger.
// Seams are per-coordinator; production uses only the fixed native operations.
type tidyV3RetirementCoordinator struct {
	observe     func(context.Context, V3PlanOptions, *tidyV3ProofBudget) (tidyV3LegacyObservation, error)
	openStore   func(string, bool, []string, []string) (*v3Store, error)
	pin         func(context.Context, string, NativeIdentity, TidyRecovery, []string) (bool, error)
	remove      func(context.Context, string, []string, string, []string, []string) (string, int, error)
	ownedRemove func(context.Context, string, string, string, []string, []string) (string, int, error)
	refs        tidyV3RefsCoordinator
}

func defaultTidyV3RetirementCoordinator() tidyV3RetirementCoordinator {
	return tidyV3RetirementCoordinator{observe: observeTidyV3Retirement, openStore: openV3Store, pin: pinTidyV3Recovery, remove: runSandboxed, ownedRemove: gitRetirementOwnedNativeRemove, refs: defaultTidyV3RefsCoordinator()}
}
func (c tidyV3RetirementCoordinator) plan(ctx context.Context, o V3PlanOptions) (r V3Result) {
	timeout := 10 * time.Second
	if o.Online {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	budget := newTidyV3ProofBudget(false)
	defer func() { retirementV3BudgetResult(&r, budget); r = boundV3Result(r) }()
	r = newV3Result("tidy-plan")
	ctx, _, closeSession, sessionErr := withPlanningSession(ctx)
	if sessionErr != nil {
		return failTidyV3(r, sessionErr)
	}
	defer closeSession()
	if err := validateV3PlanOptions(o); err != nil {
		return failTidyV3(r, err)
	}
	first, err := c.observe(ctx, o, budget)
	if err != nil {
		return failTidyV3(r, err)
	}
	p := V3Plan{SchemaVersion: 3, Policy: "tidy-git-v3", Mode: "legacy-retirement", SourcePath: o.Path, StateDir: o.StateDir, Expected: first.Native.Expected, SelectedRefs: []string{}, Candidates: []V3Candidate{}, Retirement: &first.Retirement, Effects: []string{}, RecoveryLimits: v3RecoveryLimits(), ConfigNotes: first.Native.ConfigNotes}
	if o.Task != "" {
		p.Mode = "owned-retirement"
	}
	r.Backend = "git"
	p.PlanID, err = randomID()
	if err != nil {
		return failTidyV3(r, err)
	}
	var session tidyV3Hosting
	if o.Online {
		raw, digest, e := qualifyTidyV3Remote(first.Native, o.Remote, o.Repo)
		if e != nil {
			return failTidyV3(r, e)
		}
		hints := map[string]int{}
		for _, h := range o.PRHints {
			if h.OID != first.Retirement.Target.CommitID || h.Number > int64(^uint(0)>>1) {
				return failTidyV3(r, failure("invalid-options", "Retirement hints must name the exact selected native head."))
			}
			hints[h.OID] = int(h.Number)
		}
		session, e = c.refs.openHosting(ctx, o.Path, hosting.TidySessionOptions{Online: true, Remote: o.Remote, Repo: o.Repo})
		if session != nil {
			defer func() {
				b := session.Budget()
				r.Boundary.NetworkAttempted = b.NetworkAttempted
				r.Boundary.Budget.HTTPRequests = int64(b.Requests)
				r.Boundary.Budget.HTTPBytes = b.RawBytes
				session.Close()
			}()
		}
		if e != nil {
			return failTidyV3(r, tidyV3HostingError(e))
		}
		if session == nil {
			return failTidyV3(r, failure("hosting-evidence-unavailable", "Complete original typed provider session is required."))
		}
		repo := session.Repository()
		if !v3RemoteMatches(raw, repo.FullName) {
			return failTidyV3(r, failure("evidence-changed", "Selected native remote differs from provider repository."))
		}
		p.Online = &V3OnlineSelection{Repository: tidyV3Repository(repo), Remote: o.Remote, RemoteURL: raw, ConfigDigest: digest, PRHints: append([]V3PRHint{}, o.PRHints...), ObservationDigests: []string{}, BudgetPolicy: "github-read-v3"}
		temporary := p
		temporary.Retirement = nil
		temporary.Candidates = []V3Candidate{retirementV3Candidate(first.Retirement)}
		if e = c.refs.providerProofs(ctx, session, &temporary, hints); e != nil {
			return failTidyV3(r, e)
		}
		proof := temporary.Candidates[0]
		p.Retirement.Integration = V3CommitProof{OID: proof.OID, Decision: proof.Decision, Reason: proof.Reason, Proof: proof.Proof, WorkspacePaths: proof.WorkspacePaths, Evidence: proof.Evidence}
		p.Online = temporary.Online
		retirementV3Actions(p.Retirement)
	}
	p.Effects = v3PlanEffects(p)
	p.Digest, err = v3PlanDigest(p)
	if err != nil {
		return failTidyV3(r, err)
	}
	if err = validateV3Plan(p, p.StateDir, p.PlanID); err != nil {
		return failTidyV3(r, err)
	}
	if err = retirementV3RecoveryFeasible(ctx, p); err != nil {
		return failTidyV3(r, err)
	}
	fresh, err := c.observeCurrent(ctx, o, p, p.Expected, first.Native.Native, first, budget, session)
	if err != nil {
		return failTidyV3(r, err)
	}
	s, err := c.openStore(p.StateDir, true, v3NativeRoots(fresh.Native.Native), fresh.Native.Native.ProtectedRoots)
	if err != nil {
		return failTidyV3(r, err)
	}
	defer s.Close()
	if _, err = c.observeCurrent(ctx, o, p, p.Expected, first.Native.Native, first, budget, session); err != nil {
		return failTidyV3(r, err)
	}
	if err = ctx.Err(); err != nil {
		return failTidyV3(r, err)
	}
	if err = s.writePlan(p, first.Native.Native); err != nil {
		return failTidyV3(r, err)
	}
	r.Status, r.Plan, r.Boundary.Complete = "ok", &p, true
	r.Limitations = append([]string{}, p.RecoveryLimits...)
	r.Actions = append(r.Actions, ActionOutcome{Name: "store-plan", Status: "complete", Detail: "Stored exact retirement and separate registration/files authorizations; workspaces and branches remain unchanged."})
	return r
}
func retirementV3Candidate(r V3Retirement) V3Candidate {
	p := r.Integration
	return V3Candidate{ActionID: r.ActionID, OID: p.OID, Decision: p.Decision, Reason: p.Reason, Proof: p.Proof, WorkspacePaths: p.WorkspacePaths, Evidence: p.Evidence}
}
func retirementV3Actions(r *V3Retirement) {
	for i := range r.Actions {
		r.Actions[i].Decision = "retain"
		r.Actions[i].Reason = r.Integration.Reason
		if r.Integration.Decision == "delete" {
			r.Actions[i].Decision = "delete"
			r.Actions[i].Reason = "integrated"
			if r.Actions[i].Name == "remove-files" {
				r.Actions[i].Reason = "clean"
			}
		}
	}
}
func retirementV3BudgetResult(r *V3Result, b *tidyV3ProofBudget) {
	n := b.snapshot()
	r.Limitations = append(r.Limitations, "Budget native_processes counts raw-object processes and native_bytes counts instrumented content-proof bytes; other bounded native controls and Git removal internal I/O are not measured by this ledger.")
	// NativeProcesses counts this explicitly scoped raw-object channel, not all
	// configuration/layout reads or Git removal's internal filesystem activity.
	r.Boundary.Budget.NativeProcesses = int64(n.RawProcesses)
	r.Boundary.Budget.NativeBytes = n.TotalBytes
	r.Boundary.Budget.Observations = int64(n.Observations)
	r.Boundary.Budget.UserBytes = n.UserBytes
	r.Boundary.Budget.MetadataBytes = n.MetadataBytes
	r.Boundary.Budget.FramingBytes = n.FramingBytes
	r.Boundary.Budget.StderrBytes = n.StderrBytes
}
func (c tidyV3RetirementCoordinator) observeCurrent(ctx context.Context, o V3PlanOptions, p V3Plan, expected Snapshot, native V3NativeState, baseline tidyV3LegacyObservation, budget *tidyV3ProofBudget, session tidyV3Hosting) (tidyV3LegacyObservation, error) {
	var out tidyV3LegacyObservation
	if session != nil {
		if err := (tidyV3RefsApplyCoordinator{refs: c.refs}).revalidateProof(ctx, p, expected, retirementV3Candidate(*p.Retirement), session); err != nil {
			return out, err
		}
	}
	out, err := c.observe(ctx, o, budget)
	if err != nil {
		return out, err
	}
	if p.Online != nil {
		raw, digest, e := qualifyTidyV3Remote(out.Native, p.Online.Remote, p.Online.Repository.Name)
		if e != nil {
			return out, e
		}
		if raw != p.Online.RemoteURL || digest != p.Online.ConfigDigest {
			return out, failure("evidence-changed", "Selected native provider configuration changed.")
		}
	}
	if out.Native.Expected != expected || !reflect.DeepEqual(out.Native.Native, native) {
		return out, failure("evidence-changed", "Source/native state differs from the exact permitted operation delta.")
	}
	want := baseline
	want.Native = out.Native
	// Integration evidence is revalidated through the original provider session;
	// every physical/content/authority fact still comes from this full sample.
	if p.Online != nil {
		out.Retirement.Integration = baseline.Retirement.Integration
		out.Retirement.Actions = baseline.Retirement.Actions
	}
	if !reflect.DeepEqual(out, want) {
		return out, failure("evidence-changed", "Retirement native, physical, content, parent, sibling or authority evidence changed.")
	}
	return out, nil
}

// Native recovery must not deterministically change any immutable full
// directory fact before the remaining samples. This is a finite layout check,
// not namespace creation, a content pass or a substitute snapshot.
func retirementV3RecoveryFeasible(ctx context.Context, p V3Plan) error {
	if !v3RetirementEligible(*p.Retirement) {
		return nil
	}
	r := p.Retirement
	facts := append([]V3PhysicalIdentity{r.Parent, r.RegistrationParent}, r.Siblings...)
	facts = append(facts, r.RegistrationSiblings...)
	unique := map[string]V3PhysicalIdentity{}
	for _, fact := range facts {
		if old, ok := unique[fact.Path]; ok && old != fact {
			return failure("evidence-changed", "Conflicting canonical directory roles prevent recovery feasibility proof.")
		}
		unique[fact.Path] = fact
	}
	recovery := v3RecoveryFor(p, r.ActionID, r.Target.CommitID)
	outputs := []string{filepath.Join(p.Expected.Identity.RepositoryDir, recovery.Name), filepath.Join(p.Expected.Identity.RepositoryDir, "logs", recovery.Name)}
	for _, fact := range unique {
		if fact.Kind != "directory" {
			continue
		}
		for _, output := range outputs {
			if e := ctx.Err(); e != nil {
				return e
			}
			rel, e := filepath.Rel(fact.Path, output)
			if e != nil {
				return e
			}
			if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			if rel == "." {
				return failure("retirement-layout-unsupported", "Recovery output overlaps an immutable protected directory.")
			}
			child := strings.SplitN(rel, string(filepath.Separator), 2)[0]
			if _, e = retirementV3Directory(filepath.Join(fact.Path, child)); e != nil {
				return failure("retirement-layout-unsupported", "Recovery reference or potential reflog would change an immutable protected directory; planning refuses before effects.")
			}
		}
	}
	return ctx.Err()
}
