// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/wstein/sourcesteward/internal/readiness"
)

// ReadyResult preserves the shared versioned result, including early failures.
// This caller-only adapter leaves CI/constraints unknown; it cannot be eligible.
type ReadyResult = readiness.ResultData

// ReadyComponent is the same value model exposed through the hosting boundary.
type ReadyComponent = readiness.Component
type ReadyOptions struct {
	Options
	PolicyLock string
}

func ValidateReadyOptions(o ReadyOptions) error {
	o.Kind = "pr"
	if err := ValidateOptions(o.Options); err != nil {
		return err
	}
	if o.PolicyLock == "" {
		return readyOptionError("--policy-lock is required")
	}
	return nil
}

type readyOptionError string

func (e readyOptionError) Error() string { return string(e) }

type readyOperationDeps struct {
	resolve func(context.Context, string, Options) Result
	api     func(context.Context, string, string, http.RoundTripper) (*ghAPI, func(), error)
}

func CheckReady(ctx context.Context, path string, o ReadyOptions) ReadyResult {
	return CheckReadyWithTransport(ctx, path, o, nil)
}

// CheckReadyWithTransport retains real native/authentication behavior; only HTTP
// is replaceable by an inert fixture. No installed authority is injectable.
func CheckReadyWithTransport(ctx context.Context, path string, o ReadyOptions, rt http.RoundTripper) ReadyResult {
	return checkReady(ctx, path, o, rt, readyOperationDeps{resolve: Inspect, api: newGHWithTransport})
}
func readyUnknown() readiness.Component {
	return readiness.Component{State: "unknown", Reasons: []readiness.Reason{}, Evidence: []readiness.SourceReference{}}
}
func readyOperationFailure(r ReadyResult, code string) ReadyResult {
	r.Decision = "error"
	r.Boundary.Complete = false
	r.Components.Review.Complete = false
	if r.Components.Review.State == "pass" {
		r.Components.Review.State = "unknown"
	}
	r.Components.Review.Reasons = append(r.Components.Review.Reasons, readiness.Reason{Code: code, Subject: "operation", Sources: []readiness.SourceReference{{Source: "collection", Field: "operation"}}})
	r.Diagnostics = []readiness.Diagnostic{{Code: code, Message: "Ready assessment could not be completed."}}
	return r
}
func readyPublicSource(s readySource) readiness.Source {
	failure := ""
	if s.Outcome != readyOK {
		failure = readyCollectionCode(s.Outcome)
	}
	return readiness.Source{Name: s.Name, Outcome: string(s.Outcome), RequestCount: s.Requests, ObservationIdentities: []string{s.Host, s.Repository, s.Selector}, Failure: failure}
}
func checkReady(parent context.Context, path string, o ReadyOptions, rt http.RoundTripper, d readyOperationDeps) (r ReadyResult) {
	started := time.Now()
	ctx, cancel := context.WithDeadline(parent, started.Add(30*time.Second))
	defer cancel()
	budget := &readyReadBudget{started: started}
	r = ReadyResult{SchemaVersion: 1, Kind: "ready-check", Decision: "indeterminate", ObservedAt: started.UTC().Format(time.RFC3339), Components: readiness.Components{Review: readyUnknown(), CI: readyUnknown(), Constraints: readyUnknown(), Deployment: readyUnknown()}, Sources: []readiness.Source{}, Diagnostics: []readiness.Diagnostic{}}
	r.ReviewEvidence.RuntimeAttestation = "unavailable"
	r.Boundary.Budgets = readiness.Usage{Requests: 80, Bytes: maxBytes, Pages: 10, Records: 1000, Seconds: 30}
	r.Boundary.Limitations = []string{"Caller-policy review only; CI and effective constraints are unavailable, so final eligibility is not established. Raw mergeability fields display the initial PR read only.", "Installed bootstrap, owner-transition authority and arbitrary credential dependency closure remain unavailable.", "Byte usage counts body.Read-delivered bytes, including partial errors and overflow probes; it excludes headers, wire bytes and unread bodies.", "Page usage counts collection HTTP attempts; records count parsed array rows across all samples. Byte/page/record caps apply per body or collection, not to these cumulative observations."}
	defer func() {
		r.ObservedAt = time.Now().UTC().Format(time.RFC3339)
		r.Boundary.Usage = readiness.Usage{Requests: budget.requests, Bytes: budget.bodyBytes, Pages: budget.pages, Records: budget.records, Seconds: int(time.Since(started).Seconds())}
		r.Boundary.NetworkAttempted = budget.requests > 0
		if parent.Err() != nil {
			r = readyOperationFailure(r, "hosting-canceled")
		} else if ctx.Err() != nil || time.Since(started) >= 30*time.Second {
			r = readyOperationFailure(r, "source-limit")
		}
		if raw, err := json.Marshal(r); err != nil || len(raw) > maxBytes {
			r = readyOperationFailure(r, "output-limit")
			r.Identity = nil
			r.Backend = nil
			r.Sources = []readiness.Source{}
			r.Repository = nil
			r.PullRequest = nil
			r.ReviewEvidence = readiness.ReviewEvidence{RuntimeAttestation: "unavailable"}
		}
	}()
	if ValidateReadyOptions(o) != nil {
		return readyOperationFailure(r, "ready-invalid-options")
	}
	if ctx.Err() != nil {
		return readyOperationFailure(r, "hosting-canceled")
	}
	nativeOpts := o.Options
	nativeOpts.Kind = "repo"
	nativeOpts.Number = 0
	native := d.resolve(ctx, path, nativeOpts)
	if native.Identity != nil && (native.Backend == "git" || native.Backend == "jj") {
		backend := native.Backend
		r.Backend = &backend
		r.Identity, _ = json.Marshal(native.Identity)
	}
	if native.Status != "ok" || native.Identity == nil || native.Repository == nil || o.RequireJJ && native.Backend != "jj" {
		return readyOperationFailure(r, "hosting-native-failed")
	}
	target := native.Identity.InputPath
	if v := native.Identity.Repository; v != nil {
		if v.Bare {
			target = v.GitDir
		}
		if v.Worktree != nil {
			target = *v.Worktree
		}
	}
	if v := native.Identity.JJRepository; v != nil {
		target = v.WorkspaceRoot
	}
	lock, failure := readReadyLockTyped(target, o.PolicyLock)
	if failure != nil {
		r.Sources = append(r.Sources, readyPublicSource(*failure.Source))
		r = readyOperationFailure(r, failure.Reason)
		reason := &r.Components.Review.Reasons[len(r.Components.Review.Reasons)-1]
		reason.Subject = failure.Stage
		reason.Sources = []readiness.SourceReference{{Source: failure.referenceSource(), Field: failure.Stage}}
		return r
	}
	r.Sources = append(r.Sources, readyPublicSource(readySource{Name: "policy-lock", Outcome: readyOK, Complete: true}))
	if lock.Value().Deployment.Profile != "none" {
		r.Components.Review.Reasons = []readiness.Reason{{Code: "deployment-unverified", Subject: "capability", Sources: []readiness.SourceReference{{Source: "activation", Field: "capability"}}}}
		r.Components.Deployment = r.Components.Review
		return r
	}
	if !validReadyLock(lock) {
		return readyOperationFailure(r, "lock-invalid")
	}
	if ctx.Err() != nil {
		return readyOperationFailure(r, "hosting-canceled")
	}
	if rt == nil {
		owned := newHTTPTransport()
		defer owned.CloseIdleConnections()
		rt = owned
	}
	api, cleanup, err := d.api(ctx, target, native.Repository.Host, readyMeterTransport{next: rt, budget: budget})
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil || api == nil || api.client == nil {
		return readyOperationFailure(r, "evidence-incomplete")
	}
	// Preserve original-parent cancellation versus this operation's own deadline.
	sources := newReadySources(parent, api, *native.Repository)
	sources.budget = budget
	c := collectReadyReview(target, o.PolicyLock, o.Number, sources)
	r.Components.Review = c.Review
	r.GitHubReported = c.githubReported
	r.ReviewEvidence = c.Evidence
	r.Policy = c.Policy
	r.Boundary.Complete = c.Complete
	r.Boundary.Limitations = append(r.Boundary.Limitations, c.Limitations...)
	if c.Repository.ID > 0 {
		r.Repository = &c.Repository
	}
	if c.PullRequest.Number > 0 {
		r.PullRequest = &c.PullRequest
	}
	for _, src := range append(c.Sources, c.PolicySources...) {
		r.Sources = append(r.Sources, readyPublicSource(src))
	}
	if len(c.lock.RawJSON()) != 0 && !sameReadyLockBytes(lock, c.lock) {
		r = readyOperationFailure(r, "sample-changed")
		r.Decision = "indeterminate"
		return r
	}
	if len(c.policy.RawJSON()) == 0 {
		if c.Review.State == "fail" {
			r.Decision = "blocked"
		}
		return r
	}
	r.Components.Deployment = readiness.Component{State: "not-required", Complete: true, Reasons: []readiness.Reason{}, Evidence: []readiness.SourceReference{}}
	sample := readiness.Sample{ObservedAt: r.ObservedAt, Backend: native.Backend, Identity: r.Identity, Repository: r.Repository, PullRequest: r.PullRequest, Policy: r.Policy, Components: r.Components, ReviewEvidence: r.ReviewEvidence, Sources: r.Sources, Boundary: r.Boundary, Diagnostics: r.Diagnostics, GitHubReported: r.GitHubReported, OperationalError: ctx.Err() != nil}
	evaluated, err := readiness.Evaluate(c.policy, sample)
	if err != nil {
		return readyOperationFailure(r, "evidence-incomplete")
	}
	return evaluated.Value()
}
