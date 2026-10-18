// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/readiness"
)

// readyReviewCollection is an intermediate US2 projection, not full Ready
// eligibility or a publishable installed check. Native ownership/resolution,
// complete CI/constraint/deployment collection and public rendering are later
// operation work. No report body, raw response or caller path is returned.
type readyReviewCollection struct {
	githubReported readiness.GitHubReported
	// The once-loaded immutable baseline feeds the shared result evaluator.
	policy         readiness.Policy
	lock           readiness.Lock
	Review         readiness.Component
	Evidence       readiness.ReviewEvidence
	Policy         readiness.PolicyBinding
	Repository     readiness.Repository
	PullRequest    readiness.PullRequest
	Sources        []readySource
	Requests       int
	PolicyRequests int
	// PolicySources accounts within PolicyRequests, never a second addition to
	// the operation total. Local lock observations have zero HTTP requests.
	PolicySources []readySource
	// Complete describes collected review evidence, never full Ready eligibility
	// or completeness of an arbitrary checker/credential dependency closure.
	Complete    bool
	Limitations []string
}

// collectReadyReview consumes a locally resolved native target and its selected
// fixed-source client. It performs one initial sample and one final resample,
// sharing the existing global request/deadline budget without retries. Installed
// profiles remain unsupported before any HTTP: neither the caller lock nor the
// reader's Accepted observation flag can establish independent activation trust.
func collectReadyReview(target, path string, number int, s *readySources) (out readyReviewCollection) {
	out.Review = readiness.Component{State: "unknown", Reasons: []readiness.Reason{}, Evidence: []readiness.SourceReference{}}
	out.Evidence.RuntimeAttestation = "unavailable"
	out.Limitations = []string{
		"Caller-policy review projection only; CI, effective constraints, native ownership and deployed enforcement are not established.",
		"Only normative known paths and baseline trust_roots are classified; arbitrary checker and credential dependency closure is unverified.",
		"Installed bootstrap and authenticated owner-transition approval are unavailable; sampling is not a transaction.",
	}
	problem := func(state, code, source, field string) {
		if state == "fail" || out.Review.State != "fail" {
			out.Review.State = state
		}
		if state == "unknown" {
			out.Review.Complete = false
			out.Complete = false
		}
		out.Review.Reasons = append(out.Review.Reasons, readiness.Reason{Code: code, Subject: field, Sources: []readiness.SourceReference{{Source: source, Field: field}}})
	}
	if s == nil || s.ctx == nil || s.budget == nil {
		problem("unknown", "evidence-incomplete", "collection", "client")
		return
	}
	defer func() { out.Requests = s.budget.requests }()
	l, failure := readReadyLockTyped(target, path)
	if failure != nil {
		out.PolicySources = append(out.PolicySources, *failure.Source)
		problem("unknown", failure.Reason, "policy-lock", failure.Stage)
		return
	}
	out.PolicySources = append(out.PolicySources, readySource{Name: "policy-lock", Outcome: readyOK, Complete: true})
	if l.Value().Deployment.Profile != "none" {
		problem("unknown", "deployment-unverified", "activation", "capability")
		return
	}
	if !validReadyLock(l) {
		problem("unknown", "lock-invalid", "lock", "binding")
		return
	}
	if stopped := s.stopped(s.ctx); stopped != readyOK {
		problem("unknown", readyCollectionCode(stopped), "collection", "budget")
		return
	}
	pr := s.pullRequest(number)
	out.Sources = append(out.Sources, pr.Source)
	if !pr.Source.Complete {
		problem("unknown", readyCollectionCode(pr.Source.Outcome), "pr", "source")
		return
	}
	p := pr.Value
	out.githubReported = readiness.GitHubReported{Mergeable: p.Mergeable, Rebaseable: p.Rebaseable, MergeableState: p.MergeableState}
	out.Repository = readiness.Repository{Host: s.repo.Host, ID: *p.Base.RepositoryID, FullName: p.Base.Repository, BaseRef: p.Base.Ref}
	out.PullRequest = readiness.PullRequest{Number: int64(p.Number), HeadSHA: p.Head.SHA, BaseSHA: p.Base.SHA, HeadRef: p.Head.Ref, BaseRef: p.Base.Ref, HeadRepositoryID: p.Head.RepositoryID, BaseRepositoryID: *p.Base.RepositoryID, State: p.State, Merged: p.Merged, Draft: p.Draft}
	if !sameReadyRepository(out.Repository, l.Value().Repository) {
		problem("fail", "repository-mismatch", "pr", "repository")
		return
	}
	if p.State != "open" || p.Merged {
		out.Review.Complete = true
		problem("fail", "pull-request-closed", "pr", "state")
		return
	}
	loaded := loadReadyPolicyEvidence(target, path, out.Repository, s)
	out.PolicyRequests += loaded.Requests
	out.PolicySources = append(out.PolicySources, loaded.Sources...)
	if loaded.Failure != nil {
		problem("unknown", loaded.Failure.Reason, loaded.Failure.referenceSource(), loaded.Failure.Stage)
		return
	}
	policy := loaded.Value
	if !sameReadyLockBytes(l, policy.lock) {
		problem("unknown", "sample-changed", "lock", "initial-bytes")
		return
	}
	out.Policy = policy.binding()
	out.policy = policy.policy
	out.lock = policy.lock
	associated := s.openPullRequests()
	files := s.files(number, p.ChangedFiles)
	statuses := s.statuses(p.Head.SHA)
	out.Sources = append(out.Sources, associated.Source, files.Source, statuses.Source)
	web := "https://" + s.repo.Host
	apiHost, apiPrefix := apiDestination(s.repo.Host)
	api := "https://" + apiHost + strings.TrimSuffix(apiPrefix, "/")
	sample := readiness.ReviewSample{Repository: out.Repository, PullRequest: out.PullRequest, Policy: out.Policy, WebOrigin: web, APIOrigin: api, ChangedFiles: p.ChangedFiles, Complete: associated.Source.Complete && files.Source.Complete && statuses.Source.Complete, Stable: true}
	initialSet, associationOK := readyAssociatedPRs(associated.Value, p.Head.SHA, out.Repository.ID)
	initialAssociationBound := associationOK && readyAssociationMatches(initialSet, p)
	sample.Complete = sample.Complete && initialAssociationBound
	for _, pr := range initialSet {
		sample.AssociatedOpenPRs = append(sample.AssociatedOpenPRs, int64(pr.Number))
	}
	for _, f := range files.Value {
		sample.Files = append(sample.Files, readiness.ReviewFile{Filename: f.Filename, Status: f.Status, PreviousFilename: f.PreviousFilename})
	}
	for _, v := range statuses.Value {
		r := readiness.ReviewStatus{ID: v.ID, CreatorID: v.CreatorID, Context: v.Context, State: v.State}
		if v.TargetURL != nil {
			r.TargetURL = *v.TargetURL
		}
		if v.Description != nil {
			r.Description = *v.Description
		}
		sample.Statuses = append(sample.Statuses, r)
	}
	selected := newestReadyReviewStatus(statuses.Value, policy.policy.Value().Review.Context)
	var report readyEvidence[readyReport]
	if id, ok := readyReviewCommentID(selected, web, out.Repository.FullName, number); ok && statuses.Source.Complete {
		report = s.report(id, number)
		out.Sources = append(out.Sources, report.Source)
		sample.Comment = readiness.ReviewComment{ID: report.Value.ID, AuthorID: report.Value.AuthorID, IssueURL: report.Value.IssueURL, Body: report.Value.Body}
		sample.Complete = sample.Complete && report.Source.Complete
	} else {
		sample.Complete = false
	}
	for _, src := range out.Sources[1:] {
		sample.Sources = append(sample.Sources, readiness.ReviewSource{Host: src.Host, Name: src.Name, Repository: src.Repository, Selector: src.Selector, Outcome: string(src.Outcome), Complete: src.Complete, Requests: src.Requests})
	}
	out.Review, out.Evidence = readiness.EvaluateReview(policy.policy, sample)
	out.Complete = sample.Complete && out.Review.Complete
	if !initialAssociationBound {
		out.Evidence.Singleton = false
		problem("unknown", "association-incomplete", "open-prs", "detail-binding")
	}
	// Mandatory workflow/policy/checker roots cannot be removed by candidate
	// policy text. Additional roots are literal baseline-owned path rules. This
	// caller-only increment never authenticates a transition or an installed
	// credential dependency closure; installed acceptance remains T024/T025.
	roots := append([]string{".github/workflows/", ".stw/ready-policy.json", "scripts/review-evidence.py"}, policy.policy.Value().TrustRoots...)
	for _, root := range roots {
		if !validReadyTrustRoot(root) {
			problem("unknown", "policy-invalid", "policy", "trust-roots")
			continue
		}
		for _, f := range files.Value {
			if readyRootMatches(root, f.Filename) || f.PreviousFilename != nil && readyRootMatches(root, *f.PreviousFilename) {
				problem("fail", "policy-transition-required", "files", "trust-root")
				break
			}
		}
	}
	// Fixed final reads never select a replacement favorable report or head.
	finalPR := s.pullRequest(number)
	finalAssociation := s.openPullRequests()
	finalStatuses := s.statuses(p.Head.SHA)
	out.Sources = append(out.Sources, finalPR.Source, finalAssociation.Source, finalStatuses.Source)
	finalSet, finalSetOK := readyAssociatedPRs(finalAssociation.Value, p.Head.SHA, out.Repository.ID)
	if !finalPR.Source.Complete || !finalAssociation.Source.Complete || !finalStatuses.Source.Complete || !finalSetOK || !readyAssociationMatches(finalSet, finalPR.Value) {
		out.Evidence.Singleton = false
		problem("unknown", "evidence-incomplete", "resample", "sources")
	} else if !sameReadyReviewPR(pr.Value, finalPR.Value) || !reflect.DeepEqual(initialSet, finalSet) || !reflect.DeepEqual(selected, newestReadyReviewStatus(finalStatuses.Value, policy.policy.Value().Review.Context)) {
		problem("unknown", "sample-changed", "resample", "bindings")
	}
	if report.Source.Complete {
		finalReport := s.report(report.Value.ID, number)
		out.Sources = append(out.Sources, finalReport.Source)
		if !finalReport.Source.Complete {
			problem("unknown", "evidence-incomplete", "report", "resample")
		} else if !reflect.DeepEqual(report.Value, finalReport.Value) {
			problem("unknown", "sample-changed", "report", "resample")
		}
	}
	resampled := policy.resampleEvidence(target, path, out.Repository, s)
	out.PolicyRequests += resampled.Requests
	out.PolicySources = append(out.PolicySources, resampled.Sources...)
	if resampled.Failure != nil {
		problem("unknown", resampled.Failure.Reason, resampled.Failure.referenceSource(), resampled.Failure.Stage)
	}
	if stopped := s.stopped(s.ctx); stopped != readyOK {
		problem("unknown", readyCollectionCode(stopped), "collection", "budget")
	}
	return
}

// The list and detail readers share these identity fields. A matching number
// and head alone cannot establish that their records describe the same PR.
// readyRef comparison preserves repository IDs/names and nullable fork identity.
func sameReadySharedPR(a, b readyPullRequest) bool {
	return a.ID == b.ID && a.Number == b.Number && a.State == b.State && a.Draft == b.Draft && reflect.DeepEqual(a.Head, b.Head) && reflect.DeepEqual(a.Base, b.Base)
}

func readyAssociationMatches(set []readyPullRequest, detail readyPullRequest) bool {
	return len(set) == 1 && sameReadySharedPR(set[0], detail)
}

// Mergeability and opaque aggregate state belong to CI/constraints, independent
// of review. Merged state and changed-file count are detail-only review bindings.
func sameReadyReviewPR(a, b readyPullRequest) bool {
	return sameReadySharedPR(a, b) && a.Merged == b.Merged && a.ChangedFiles == b.ChangedFiles
}

func readyCollectionCode(outcome readyOutcome) string {
	switch outcome {
	case readyCanceled:
		return "hosting-canceled"
	case readyLimit:
		return "source-limit"
	case readyDenied:
		return "source-denied"
	case readyMalformed:
		return "source-malformed"
	default:
		return "evidence-incomplete"
	}
}
func newestReadyReviewStatus(rows []readyStatus, context string) *readyStatus {
	var selected *readyStatus
	for i := range rows {
		if rows[i].Context == context && (selected == nil || rows[i].ID > selected.ID) {
			selected = &rows[i]
		}
	}
	return selected
}

// This parser only selects a numeric fixed-reader argument. The shared pure
// evaluator independently verifies the full locator, digest and issuer binding.
func readyReviewCommentID(status *readyStatus, web, repo string, number int) (int64, bool) {
	if status == nil || status.TargetURL == nil {
		return 0, false
	}
	prefix := web + "/" + repo + "/pull/" + strconv.Itoa(number) + "#issuecomment-"
	if !strings.HasPrefix(*status.TargetURL, prefix) {
		return 0, false
	}
	text := strings.TrimPrefix(*status.TargetURL, prefix)
	id, err := strconv.ParseInt(text, 10, 64)
	return id, err == nil && id > 0 && strconv.FormatInt(id, 10) == text
}
func readyAssociatedPRs(rows []readyPullRequest, head string, repositoryID int64) ([]readyPullRequest, bool) {
	set := []readyPullRequest{}
	for _, pr := range rows {
		if pr.Base.RepositoryID == nil || *pr.Base.RepositoryID != repositoryID {
			return nil, false
		}
		if pr.Head.SHA == head {
			set = append(set, pr)
		}
	}
	sort.Slice(set, func(i, j int) bool { return set[i].ID < set[j].ID })
	return set, true
}
func validReadyTrustRoot(root string) bool {
	if root == "" || len(root) > 1024 || !utf8.ValidString(root) || strings.HasPrefix(root, "/") || strings.ContainsAny(root, "\\*?[]{}\x00\r\n") {
		return false
	}
	for _, part := range strings.Split(strings.TrimSuffix(root, "/"), "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func readyRootMatches(root, path string) bool {
	return root == path || strings.HasSuffix(root, "/") && strings.HasPrefix(path, root)
}
