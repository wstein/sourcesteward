// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/inspection"
)

// Only the reviewed typed reads cross this boundary. Each operation owns one
// session and its original deadline/ledger; there are no mutable global seams.
type tidyV3Hosting interface {
	Repository() hosting.TidyRepositoryIdentity
	Budget() hosting.TidyHTTPBudget
	Close()
	DiscoverPullRequests(context.Context, []string, map[string]int) ([]hosting.TidyHeadEvidence, error)
	RevalidatePullRequest(context.Context, hosting.TidyPullRequest) error
	RevalidateRepository(context.Context) error
}
type tidyV3RefsCoordinator struct {
	inspect     func(context.Context, string) inspection.Result
	observe     func(context.Context, string, string) (tidyV3NativeObservation, error)
	ancestry    func(context.Context, Snapshot, string, string) (bool, error)
	openHosting func(context.Context, string, hosting.TidySessionOptions) (tidyV3Hosting, error)
}

func defaultTidyV3RefsCoordinator() tidyV3RefsCoordinator {
	return tidyV3RefsCoordinator{inspect: inspection.Inspect, observe: observeTidyV3Git, ancestry: tidyV3NativeAncestry,
		openHosting: func(ctx context.Context, path string, o hosting.TidySessionOptions) (tidyV3Hosting, error) {
			return hosting.OpenTidySession(ctx, path, o)
		}}
}
func failTidyV3(r V3Result, err error) V3Result {
	failed := failResult(newResult(r.Command), err)
	r.Status, r.Diagnostics, r.Plan, r.Boundary.Complete = failed.Status, failed.Diagnostics, nil, false
	switch failed.Diagnostics[0].Code {
	case "unsupported-backend", "unsupported-mode", "hosting-proof-unsupported", "evidence-changed", "invalid-options", "invalid-input", "unknown-reference":
		r.Status = "refused"
	}
	return r
}
func (c tidyV3RefsCoordinator) plan(ctx context.Context, o V3PlanOptions) V3Result {
	timeout := 10 * time.Second
	if o.Online {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx, _, closeSession, err := withPlanningSession(ctx)
	if err != nil {
		return boundV3Result(failTidyV3(newV3Result("tidy-plan"), err))
	}
	defer closeSession()
	return boundV3Result(c.planRefs(ctx, o))
}
func (c tidyV3RefsCoordinator) planRefs(ctx context.Context, o V3PlanOptions) (r V3Result) {
	r = newV3Result("tidy-plan")
	if err := validateV3PlanOptions(o); err != nil {
		return failTidyV3(r, err)
	}
	if o.Task != "" || o.CompletedWorktree != "" || o.TrackingRemote != "" {
		return failTidyV3(r, failure("unsupported-mode", "This coordinator plans local references only."))
	}
	if err := ctx.Err(); err != nil {
		return failTidyV3(r, err)
	}
	source, err := canonicalSource(o.Path)
	if err != nil {
		return failTidyV3(r, err)
	}
	if source != o.Path {
		return failTidyV3(r, failure("invalid-input", "Source path must be canonical without aliases."))
	}
	id := c.inspect(ctx, source)
	r.Backend = id.Backend
	if id.Status != "ok" || id.Backend != "git" {
		return failTidyV3(r, failure("unsupported-backend", "V3 requires admitted native Git ownership; existing backend contracts remain separate."))
	}
	observation, err := c.observe(ctx, source, o.Base)
	if err != nil {
		return failTidyV3(r, err)
	}
	if err = qualifyTidyWorkspaceRoots(ctx, v3NativeRoots(observation.Native), o.StateDir); err != nil {
		return failTidyV3(r, err)
	}
	selected := append([]string{}, o.Refs...)
	sort.Strings(selected)
	p := V3Plan{SchemaVersion: 3, Policy: "tidy-git-v3", Mode: "local-refs", SourcePath: source, StateDir: o.StateDir, Expected: observation.Expected, SelectedRefs: selected, Candidates: []V3Candidate{}, Effects: []string{}, RecoveryLimits: v3RecoveryLimits(), ConfigNotes: observation.ConfigNotes}
	if p.StateDir != "" {
		p.PlanID, err = randomID()
		if err != nil {
			return failTidyV3(r, err)
		}
	}
	selectedSet := map[string]bool{}
	for _, name := range selected {
		selectedSet[name] = true
	}
	found, selectedOIDs := map[string]bool{}, map[string]bool{}
	for _, ref := range observation.Native.References {
		if !strings.HasPrefix(ref.Name, "refs/heads/") || len(selected) > 0 && !selectedSet[ref.Name] {
			continue
		}
		found[ref.Name], selectedOIDs[ref.OID] = true, true
		candidate := V3Candidate{ActionID: "ref-" + gitDigest(ref.Name)[:32], Kind: "branch", Name: ref.Name, OID: ref.OID, Decision: "retain", Reason: "integration-not-proven", Proof: "none", WorkspacePaths: []string{}}
		for _, w := range observation.Native.Registrations {
			if w.HeadRef == ref.Name || w.HeadOID == ref.OID {
				candidate.WorkspacePaths = append(candidate.WorkspacePaths, w.Path)
			}
		}
		switch {
		case ref.Symbolic != "" || tidyV3SymbolicTarget(observation.Native, ref.Name):
			candidate.Reason = "symbolic-reference"
		case ref.Name == p.Expected.BaseSelector || ref.OID == p.Expected.BaseCommit:
			candidate.Reason = "selected-base"
		case len(candidate.WorkspacePaths) > 0:
			candidate.Reason = "workspace-used"
		default:
			proof, e := proveTidyV3Commit(ctx, p.Expected, ref.OID, candidate.WorkspacePaths, nil, nil, func(ctx context.Context, target, base string) (bool, error) {
				return c.ancestry(ctx, p.Expected, target, base)
			})
			if e != nil {
				return failTidyV3(r, e)
			}
			setTidyV3CandidateProof(&candidate, proof)
		}
		p.Candidates = append(p.Candidates, candidate)
	}
	for _, name := range selected {
		if !found[name] {
			return failTidyV3(r, failure("unknown-reference", "A selected exact local reference was not completely observed."))
		}
	}
	hints := map[string]int{}
	hintNumbers := map[int64]bool{}
	for _, h := range o.PRHints {
		if !selectedOIDs[h.OID] || h.Number > int64(^uint(0)>>1) || hintNumbers[h.Number] {
			return failTidyV3(r, failure("invalid-options", "PR hints must name selected full native heads and supported numbers."))
		}
		hints[h.OID] = int(h.Number)
		hintNumbers[h.Number] = true
	}
	// Validate the complete native baseline before credentials, including every
	// symbolic chain and all registration facts, not just selected references.
	if !validV3NativeState(observation.Native, p, p.Expected) {
		return failTidyV3(r, failure("incomplete-evidence", "Complete native references and canonical registrations are required."))
	}
	var session tidyV3Hosting
	if o.Online {
		raw, config, e := qualifyTidyV3Remote(observation, o.Remote, o.Repo)
		if e != nil {
			return failTidyV3(r, e)
		}
		session, e = c.openHosting(ctx, source, hosting.TidySessionOptions{Online: true, Remote: o.Remote, Repo: o.Repo})
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
			return failTidyV3(r, failure("hosting-evidence-unavailable", "A complete typed hosting session is required."))
		}
		repo := session.Repository()
		if !v3RemoteMatches(raw, repo.FullName) || !v3Repo(tidyV3Repository(repo)) {
			return failTidyV3(r, failure("evidence-changed", "Provider repository does not match the selected native remote."))
		}
		p.Online = &V3OnlineSelection{Repository: tidyV3Repository(repo), Remote: o.Remote, RemoteURL: raw, ConfigDigest: config, PRHints: append([]V3PRHint{}, o.PRHints...), ObservationDigests: []string{}, BudgetPolicy: "github-read-v3"}
		if err = c.providerProofs(ctx, session, &p, hints); err != nil {
			return failTidyV3(r, err)
		}
		if err = revalidateTidyV3Provider(ctx, session, p); err != nil {
			return failTidyV3(r, err)
		}
	}
	p.Effects = v3PlanEffects(p)
	p.Digest, err = v3PlanDigest(p)
	if err != nil {
		return failTidyV3(r, err)
	}
	if err = validateV3Plan(p, p.StateDir, p.PlanID); err != nil {
		return failTidyV3(r, err)
	}
	fresh, err := c.observe(ctx, source, o.Base)
	if err != nil {
		return failTidyV3(r, err)
	}
	if !sameTidyV3Observation(observation, fresh) {
		return failTidyV3(r, failure("evidence-changed", "Native evidence changed while planning; replan."))
	}
	if err = qualifyTidyWorkspaceRoots(ctx, v3NativeRoots(fresh.Native), o.StateDir); err != nil {
		return failTidyV3(r, err)
	}
	if p.StateDir != "" {
		s, e := openV3Store(p.StateDir, true, v3NativeRoots(fresh.Native), fresh.Native.ProtectedRoots)
		if e != nil {
			return failTidyV3(r, e)
		}
		defer s.Close()
		if session != nil {
			if e = revalidateTidyV3Provider(ctx, session, p); e != nil {
				return failTidyV3(r, e)
			}
		}
		fresh, e = c.observe(ctx, source, o.Base)
		if e != nil {
			return failTidyV3(r, e)
		}
		if !sameTidyV3Observation(observation, fresh) {
			return failTidyV3(r, failure("evidence-changed", "Native evidence changed under the private plan lock."))
		}
		if e = qualifyTidyWorkspaceRoots(ctx, v3NativeRoots(fresh.Native), o.StateDir); e != nil {
			return failTidyV3(r, e)
		}
		if e = ctx.Err(); e != nil {
			return failTidyV3(r, e)
		}
		if e = s.writePlan(p, fresh.Native); e != nil {
			return failTidyV3(r, e)
		}
		r.Actions = append(r.Actions, ActionOutcome{Name: "store-plan", Status: "complete", Detail: "Stored immutable v3 plan; native references and workspaces are unchanged."})
	}
	if err = ctx.Err(); err != nil {
		return failTidyV3(r, err)
	}
	r.Status, r.Plan, r.Boundary.Complete = "ok", &p, true
	r.Limitations = append([]string{}, p.RecoveryLimits...)
	return r
}
func (c tidyV3RefsCoordinator) providerProofs(ctx context.Context, s tidyV3Hosting, p *V3Plan, hints map[string]int) error {
	targets := []string{}
	seen := map[string]bool{}
	requestHints := map[string]int{}
	for _, v := range p.Candidates {
		if v.Reason == "integration-not-proven" && v.Proof == "none" && !seen[v.OID] {
			targets = append(targets, v.OID)
			seen[v.OID] = true
			if n, ok := hints[v.OID]; ok {
				requestHints[v.OID] = n
			}
		}
	}
	if len(targets) == 0 {
		return nil
	}
	rows, err := s.DiscoverPullRequests(ctx, targets, requestHints)
	if err != nil {
		return tidyV3HostingError(err)
	}
	if len(rows) != len(targets) {
		return failure("hosting-evidence-unavailable", "Provider discovery did not return every selected head.")
	}
	byOID := map[string]hosting.TidyHeadEvidence{}
	for i, row := range rows {
		if row.HeadOID != targets[i] {
			return failure("hosting-evidence-unavailable", "Provider discovery returned an inconsistent target order.")
		}
		byOID[row.HeadOID] = row
	}
	repo := s.Repository()
	for i, v := range p.Candidates {
		if v.Reason != "integration-not-proven" || v.Proof != "none" {
			continue
		}
		row := byOID[v.OID]
		proof, e := proveTidyV3Commit(ctx, p.Expected, v.OID, v.WorkspacePaths, &repo, &row, func(ctx context.Context, target, base string) (bool, error) {
			return c.ancestry(ctx, p.Expected, target, base)
		})
		if e != nil {
			return e
		}
		setTidyV3CandidateProof(&p.Candidates[i], proof)
		if proof.Evidence != nil && proof.Evidence.GitHub != nil {
			digest := proof.Evidence.GitHub.ObservationDigest
			if !containsString(p.Online.ObservationDigests, digest) {
				p.Online.ObservationDigests = append(p.Online.ObservationDigests, digest)
			}
		}
	}
	return nil
}
func setTidyV3CandidateProof(c *V3Candidate, p V3CommitProof) {
	c.Decision, c.Reason, c.Proof, c.Evidence = p.Decision, p.Reason, p.Proof, p.Evidence
}
func tidyV3SymbolicTarget(n V3NativeState, name string) bool {
	for _, r := range n.References {
		if r.Symbolic == name {
			return true
		}
	}
	return false
}
func revalidateTidyV3Provider(ctx context.Context, s tidyV3Hosting, p V3Plan) error {
	if p.Online == nil || tidyV3Repository(s.Repository()) != p.Online.Repository {
		return failure("evidence-changed", "Selected provider repository identity changed.")
	}
	if err := s.RevalidateRepository(ctx); err != nil {
		return tidyV3HostingError(err)
	}
	seen := map[string]bool{}
	for _, c := range p.Candidates {
		if c.Evidence == nil || c.Evidence.GitHub == nil {
			continue
		}
		g := c.Evidence.GitHub
		if seen[g.ObservationDigest] {
			continue
		}
		seen[g.ObservationDigest] = true
		proof := V3CommitProof{OID: c.OID, Decision: c.Decision, Reason: c.Reason, Proof: c.Proof, WorkspacePaths: c.WorkspacePaths, Evidence: c.Evidence}
		if err := revalidateTidyV3Commit(ctx, s, p.Expected, proof, func(ctx context.Context, target, base string) (bool, error) {
			return tidyV3NativeAncestry(ctx, p.Expected, target, base)
		}); err != nil {
			return err
		}
	}
	return nil
}
