// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
	// integration is the forge-free proof engine (Spec 031).
	integration integrationEngine
	// proofRunner replaces the apply re-evaluation of a recorded class; tests
	// use it to present a verdict that differs from the recorded evidence.
	proofRunner func(context.Context, *integrationRun, string, string, V3ProofPolicy) (integrationVerdict, error)
	// writeReport stores the rendered review report in the owner-named
	// directory; tests replace it to fail the write.
	writeReport func(dir string, markdown, jsonData []byte) error
}

func defaultTidyV3RefsCoordinator() tidyV3RefsCoordinator {
	return tidyV3RefsCoordinator{inspect: inspection.Inspect, observe: observeTidyV3Git, ancestry: tidyV3NativeAncestry, integration: defaultIntegrationEngine(), writeReport: defaultWriteReport,
		openHosting: func(ctx context.Context, path string, o hosting.TidySessionOptions) (tidyV3Hosting, error) {
			return hosting.OpenTidySession(ctx, path, o)
		}}
}
func failTidyV3(r V3Result, err error) V3Result {
	failed := failResult(newResult(r.Command), err)
	r.Status, r.Diagnostics, r.Plan, r.ReviewSummary, r.Boundary.Complete = failed.Status, failed.Diagnostics, nil, nil, false
	switch failed.Diagnostics[0].Code {
	case "unsupported-backend", "unsupported-mode", "hosting-proof-unsupported", "evidence-changed", "invalid-options", "invalid-input", "unknown-reference":
		r.Status = "refused"
	}
	return r
}
func (c tidyV3RefsCoordinator) plan(ctx context.Context, o V3PlanOptions) V3Result {
	ctx, cancel := context.WithTimeout(ctx, v3ProofPlanTimeout(o))
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
	policy, err := v3ProofPolicyFromOptions(o)
	if err != nil {
		return failTidyV3(r, err)
	}
	var decisions *V3DecisionFile
	if o.DecisionsFile != "" {
		file, e := ReadV3DecisionFile(o.DecisionsFile)
		if e != nil {
			return failTidyV3(r, e)
		}
		decisions = &file
	}
	// Grafts and alternates are hard stops of the proofs, not refusals.
	ctx = withHistoryHardStops(ctx)
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
	if o.ReportDir != "" {
		if err = checkReportDirectory(o.ReportDir, reportRoots(source, o.StateDir, observation.Expected.Identity, v3NativeRoots(observation.Native))); err != nil {
			return failTidyV3(r, err)
		}
	}
	selected := append([]string{}, o.Refs...)
	sort.Strings(selected)
	p := V3Plan{SchemaVersion: 3, Policy: "tidy-git-v3", Mode: "local-refs", SourcePath: source, StateDir: o.StateDir, Expected: observation.Expected, SelectedRefs: selected, Candidates: []V3Candidate{}, Effects: []string{}, RecoveryLimits: v3RecoveryLimits(), ConfigNotes: observation.ConfigNotes, ProofPolicy: &policy}
	run, closeRun, err := c.integration.begin(ctx, observation.Expected.Identity, observation.ConfigNotes)
	if err != nil {
		return failTidyV3(r, err)
	}
	defer closeRun()
	ranges, err := run.resolveConsolidationRanges(ctx, o.ConsolidationRanges)
	if err != nil {
		return failTidyV3(r, err)
	}
	review, reviewBounds, err := reviewBindingFor(o, ranges)
	if err != nil {
		return failTidyV3(r, err)
	}
	p.Review = review
	proofs := c.localProofs(run, p.Expected, policy)
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
			proof, e := proofs.prove(ctx, ref.OID, candidate.WorkspacePaths)
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
	o.PRHints = normalizeV3PRHints(o.PRHints)
	hints := map[string]int{}
	hintSelectors := map[int64]V3PRHint{}
	for _, h := range o.PRHints {
		if !selectedOIDs[h.OID] {
			return failTidyV3(r, failure("invalid-options", fmt.Sprintf("%s names a head that no selected reference carries.", v3HintSelector(h))))
		}
		if h.Number > int64(^uint(0)>>1) {
			return failTidyV3(r, failure("invalid-options", fmt.Sprintf("%s names an unsupported PR number.", v3HintSelector(h))))
		}
		if first, ok := hintSelectors[h.Number]; ok {
			return failTidyV3(r, failure("invalid-options", fmt.Sprintf("%s and %s name the same PR number for different heads.", v3HintSelector(first), v3HintSelector(h))))
		}
		hints[h.OID] = int(h.Number)
		hintSelectors[h.Number] = h
	}
	// Validate the complete native baseline before credentials, including every
	// symbolic chain and all registration facts, not just selected references.
	if !validV3NativeState(observation.Native, p, p.Expected) {
		return failTidyV3(r, failure("incomplete-evidence", "Complete native references and canonical registrations are required."))
	}
	var session tidyV3Hosting
	var discoveryNotes []string
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
		notes, err := c.providerProofs(ctx, session, &p, hints)
		if err != nil {
			return failTidyV3(r, err)
		}
		discoveryNotes = notes
		// No provider re-read happens at plan time: discovery already read each
		// chosen PR in full, and RevalidatePullRequest pairs run only at apply,
		// outside the 128-request plan budget (Spec 031 FR-018, Spec 029
		// amendment 5). A plan-time pass would spend the budget discovery
		// reports as exhausted.
	}
	var rendered *renderedReport
	if o.ReportDir != "" || decisions != nil {
		report, e := c.buildReviewReport(ctx, run, p, proofs.verdicts, reviewBounds)
		if e != nil {
			return failTidyV3(r, e)
		}
		rendered = &report
	}
	if decisions != nil {
		p.Review.DecisionsDigest = decisions.Digest
		applyDecisions(&p, *decisions, *rendered)
	}
	written := rendered
	if o.ReportDir == "" {
		written = nil
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
	if written != nil {
		// The report is written before the plan is stored: a failed write stores
		// no plan.
		if err = c.writeReport(o.ReportDir, rendered.Markdown, rendered.JSON); err != nil {
			return failTidyV3(r, err)
		}
	}
	if p.StateDir != "" {
		s, e := openV3Store(p.StateDir, true, v3NativeRoots(fresh.Native), fresh.Native.ProtectedRoots)
		if e != nil {
			return failTidyV3(r, e)
		}
		defer s.Close()
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
	r.ReviewSummary = reviewSummary(p, written)
	r.Limitations = append(append([]string{}, p.RecoveryLimits...), discoveryNotes...)
	return r
}

// providerProofs returns plan notes naming discovery that could not decide.
func (c tidyV3RefsCoordinator) providerProofs(ctx context.Context, s tidyV3Hosting, p *V3Plan, hints map[string]int) ([]string, error) {
	targets := []string{}
	seen := map[string]bool{}
	requestHints := map[string]int{}
	for _, v := range p.Candidates {
		if tidyV3OnlineEligible(v) && !seen[v.OID] {
			targets = append(targets, v.OID)
			seen[v.OID] = true
			if n, ok := hints[v.OID]; ok {
				requestHints[v.OID] = n
			}
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}
	sort.Strings(targets)
	rows, err := s.DiscoverPullRequests(ctx, targets, requestHints)
	if err != nil {
		return nil, tidyV3HostingError(err)
	}
	if len(rows) != len(targets) {
		return nil, failure("hosting-evidence-unavailable", "Provider discovery did not return every selected head.")
	}
	byOID := map[string]hosting.TidyHeadEvidence{}
	for i, row := range rows {
		if row.HeadOID != targets[i] {
			return nil, failure("hosting-evidence-unavailable", "Provider discovery returned an inconsistent target order.")
		}
		byOID[row.HeadOID] = row
	}
	repo := s.Repository()
	for i, v := range p.Candidates {
		if !tidyV3OnlineEligible(v) {
			continue
		}
		row := byOID[v.OID]
		// The ancestry class runs only when selected: the first call of the
		// proof is the tip's own ancestry.
		tipChecked := false
		proof, e := proveTidyV3Commit(ctx, p.Expected, v.OID, v.WorkspacePaths, &repo, &row, func(ctx context.Context, target, base string) (bool, error) {
			if !tipChecked {
				tipChecked = true
				if !p.ProofPolicy.has(integrationAncestor) {
					return false, nil
				}
			}
			return c.ancestry(ctx, p.Expected, target, base)
		})
		if e != nil {
			return nil, e
		}
		if proof.Proof == "none" && v.Reason == integrationReviewDisposition {
			continue // no provider proof: the review reasons stay
		}
		setTidyV3CandidateProof(&p.Candidates[i], proof)
		if proof.Evidence != nil && proof.Evidence.GitHub != nil {
			digest := proof.Evidence.GitHub.ObservationDigest
			if !containsString(p.Online.ObservationDigests, digest) {
				p.Online.ObservationDigests = append(p.Online.ObservationDigests, digest)
			}
		}
	}
	records := tidyV3DiscoveryRecords(rows, p.Candidates, requestHints)
	notes := tidyV3DiscoveryNotes(records)
	// The stored list has its own bounds and never makes a plan refuse: records
	// beyond them are dropped in OID order with a note, after the notes above
	// were built from the full set.
	kept, refs, size := 0, 0, 0
	for kept < len(records) && kept < v3DiscoveryRecordCap && refs+len(records[kept].Refs) <= v3DiscoveryRefCap && size+v3DiscoveryBytes(records[kept]) <= v3DiscoveryByteCap {
		refs += len(records[kept].Refs)
		size += v3DiscoveryBytes(records[kept])
		kept++
	}
	if kept > 0 {
		p.Online.Discovery = records[:kept]
	}
	if kept < len(records) {
		notes = append(notes, fmt.Sprintf("discovery-records-truncated: stored %d of %d discovery records (at most %d records, %d refs and %d KiB); the plan decisions are unaffected.", kept, len(records), v3DiscoveryRecordCap, v3DiscoveryRefCap, v3DiscoveryByteCap>>10))
	}
	return notes, nil
}

// tidyV3DiscoveryRecords builds one record per distinct head OID listing every
// candidate ref that carries it, in ascending OID order. Rows from doubles that
// predate result and coverage fall back to the closed-outcome reading.
func tidyV3DiscoveryRecords(rows []hosting.TidyHeadEvidence, candidates []V3Candidate, hints map[string]int) []V3DiscoveryRecord {
	refs := map[string][]string{}
	for _, v := range candidates {
		if v.Name != "" {
			refs[v.OID] = append(refs[v.OID], v.Name)
		}
	}
	records := []V3DiscoveryRecord{}
	for _, row := range rows {
		names := refs[row.HeadOID]
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		result, coverage := row.Result, row.Coverage
		if result == "" {
			switch row.Reason {
			case "github-merged-head":
				result = "github-merged-head"
			case "pr-ambiguous":
				result = "ambiguous-needs-hint"
			default:
				result = "no-merged-pr"
			}
		}
		if coverage == "" {
			coverage = "complete"
		}
		source := "discovered"
		if _, ok := hints[row.HeadOID]; ok {
			source = "hint"
		}
		record := V3DiscoveryRecord{OID: row.HeadOID, Source: source, Refs: names, Coverage: coverage, Result: result}
		if row.PullRequest != nil && result == "github-merged-head" {
			record.PR = &V3PRIdentity{ID: row.PullRequest.ID, NodeID: row.PullRequest.NodeID, Number: int64(row.PullRequest.Number)}
		}
		records = append(records, record)
	}
	return records
}

const tidyV3NoteBytes = 2048

// tidyV3DiscoveryNotes names, per undecided result, the head OIDs and refs that
// stay retained as integration-not-proven. The list is bounded; no note claims
// absence of a merged PR.
func tidyV3DiscoveryNotes(records []V3DiscoveryRecord) []string {
	notes := []string{}
	for _, result := range []string{"discovery-incomplete", "discovery-budget-exhausted", "ambiguous-needs-hint"} {
		selected := []V3DiscoveryRecord{}
		covered := 0
		for _, d := range records {
			if d.Result == result {
				selected = append(selected, d)
				covered += len(d.Refs)
			}
		}
		if len(selected) == 0 {
			continue
		}
		unit := "head OIDs"
		if len(selected) == 1 {
			unit = "head OID"
		}
		explain := "the closed pull request collection reached its page bound; found merged heads remain proof, absence is not claimed"
		switch result {
		case "discovery-budget-exhausted":
			explain = "the shared 128-request budget could not cover their candidate reads; rerun with --pr OID=N for these heads"
		case "ambiguous-needs-hint":
			explain = "several admissible merged pull requests exist; select one with --pr OID=N"
		}
		note := fmt.Sprintf("%s: %d %s (%d refs) retained as integration-not-proven; %s.", result, len(selected), unit, covered, explain)
		shown := 0
		for _, d := range selected {
			item := fmt.Sprintf(" %s [%s]", d.OID, strings.Join(d.Refs, " "))
			if len(note)+len(item) > tidyV3NoteBytes {
				break
			}
			note += item
			shown++
		}
		if shown < len(selected) {
			note += fmt.Sprintf(" and %d more", len(selected)-shown)
		}
		notes = append(notes, note)
	}
	return notes
}

// tidyV3OnlineEligible selects the tips that online evidence may still prove:
// not proven by a native class and not stopped by a hard stop.
func tidyV3OnlineEligible(c V3Candidate) bool {
	return c.Proof == "none" && (c.Reason == "integration-not-proven" || c.Reason == integrationReviewDisposition)
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
