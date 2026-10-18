// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/wstein/sourcesteward/internal/hosting"
	"path"
	"strings"
	"time"
)

type DispatchOptions struct{ Path, Commit, Tag, Version, Product, Repo, Host, Apply string }
type DispatchBody struct {
	Ref    string         `json:"ref"`
	Inputs DispatchInputs `json:"inputs"`
}
type DispatchRequest struct {
	Method   string       `json:"method"`
	Endpoint string       `json:"endpoint"`
	Body     DispatchBody `json:"body"`
}
type DispatchResult struct {
	PlanResult
	Request     *DispatchRequest `json:"request"`
	PlanID      string           `json:"plan_id"`
	Applied     bool             `json:"applied"`
	WorkflowURL string           `json:"workflow_url"`
	HTTPStatus  *int             `json:"http_status"`
	Approval    string           `json:"approval"`
}
type dispatchWriter func(context.Context, string, hosting.Repository, string, hosting.WorkflowDispatchInputs) (hosting.DispatchResponse, error)

func Dispatch(ctx context.Context, o DispatchOptions) DispatchResult {
	return dispatchWithAdapters(ctx, o, hosting.ReadRelease, hosting.DispatchWorkflow)
}
func dispatchWithAdapters(parent context.Context, o DispatchOptions, reader releaseReader, writer dispatchWriter) (r DispatchResult) {
	ctx, cancel := context.WithTimeout(parent, 120*time.Second)
	defer cancel()
	options := PlanOptions{Path: o.Path, Subject: o.Commit, Version: o.Version, Product: o.Product, Repo: o.Repo, Host: o.Host, Mode: "build", dispatch: true}
	if o.Tag != "" {
		options.Subject = o.Tag
		options.explicitTag = true
	}
	r = DispatchResult{PlanResult: planOffline(ctx, options), Approval: "a build was not authorized by this plan"}
	r.Command = "release-dispatch"
	defer func() { r.PlanResult = escapePlan(r.PlanResult) }()
	refuse := func(code, msg string) DispatchResult { r.PlanResult = planRefusal(r.PlanResult, code, msg); return r }
	if r.Status != "ok" {
		return r
	}
	// Invalid contracts or versions have no authorizable request. Native failed
	// gates remain inspectable without acquiring credentials unnecessarily.
	if r.configSHA256 == "" || r.contract == nil || r.Subject.Commit == nil {
		if o.Apply != "" {
			return refuse("gate-failed", "dispatch requires passing release gates")
		}
		return r
	}
	var repository hosting.Repository
	remoteMain := ""
	called := false
	var readError error
	capture := func(ctx context.Context, root string, repo hosting.Repository, commit string, workflows []string) (hosting.ReleaseEvidence, error) {
		called = true
		repository = repo
		e, err := reader(ctx, root, repo, commit, workflows)
		readError = err
		remoteMain = e.MainSHA
		return e, err
	}
	r.PlanResult = applyOnlinePlanWithReader(ctx, options, r.PlanResult, false, capture)
	if errors.Is(readError, hosting.ErrAuthentication) {
		return refuse("auth-unavailable", "selected-host release authentication unavailable")
	}
	if errors.Is(readError, context.Canceled) || errors.Is(readError, context.DeadlineExceeded) {
		return refuse("canceled", "dispatch online evidence canceled")
	}
	if r.Status != "ok" {
		if r.Error != nil && r.Error.Code == "read-failed" {
			r.Error.Code = "online-unavailable"
		}
		return r
	}
	if !called {
		if o.Apply != "" {
			return refuse("gate-failed", "dispatch requires passing release gates")
		}
		return r
	}
	if r.Subject.MainRef.Commit == nil || !IsFullCommitID(remoteMain) || *r.Subject.MainRef.Commit != remoteMain {
		return refuse("main-ref-stale", "fetch the selected remote main before planning a dispatch")
	}
	inputs := DispatchInputs{Mode: "commit", Commit: *r.Subject.Commit, Version: r.Subject.Version, Product: r.contract.Project.Product}
	if o.Tag != "" {
		inputs.Mode = "tag"
		inputs.Commit = ""
		inputs.Tag = o.Tag
	}
	workflow := path.Base(r.contract.CI.ReleaseWorkflow)
	identity := DispatchPlan{Repository: repository.Owner + "/" + repository.Name, Host: repository.Host, Workflow: workflow, Ref: "main", Inputs: inputs, ResolvedCommit: *r.Subject.Commit, MainCommit: remoteMain, ConfigSHA256: r.configSHA256, Verdict: r.Verdict}
	id, err := identity.ID()
	if err != nil {
		return refuse("gate-failed", "cannot form a valid dispatch identity")
	}
	endpoint := "repos/" + identity.Repository + "/actions/workflows/" + workflow + "/dispatches"
	r.PlanID = id
	r.Request = &DispatchRequest{Method: "POST", Endpoint: endpoint, Body: DispatchBody{Ref: "main", Inputs: inputs}}
	r.WorkflowURL = "https://" + repository.Host + "/" + identity.Repository + "/actions/workflows/" + workflow
	if o.Apply == "" {
		return r
	}
	if o.Apply != r.PlanID {
		return refuse("plan-stale", "dispatch plan changed; inspect and approve the new plan")
	}
	if r.Verdict != "pass" {
		return refuse("gate-failed", "dispatch requires passing release gates")
	}
	if ctx.Err() != nil {
		return refuse("canceled", "dispatch canceled before request")
	}
	response, err := writer(ctx, r.root, repository, workflow, hosting.WorkflowDispatchInputs{Mode: inputs.Mode, Product: inputs.Product, Commit: inputs.Commit, Tag: inputs.Tag, Version: inputs.Version})
	if response.Requests > 0 {
		r.Network = append(r.Network, PlanRequest{"POST", endpoint})
		r.Boundary.Effects.Network = true
		r.Boundary.Effects.Writes = true
	}
	if response.StatusCode != 0 {
		status := response.StatusCode
		r.HTTPStatus = &status
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return refuse("canceled", "dispatch canceled; an attempted write may have started a workflow")
	}
	if err != nil || response.Requests != 1 || response.StatusCode != 204 {
		if errors.Is(err, hosting.ErrDispatchAuth) {
			return refuse("auth-unavailable", "selected-host dispatch authentication unavailable")
		}
		message := "workflow dispatch unavailable; an attempted write may have started a workflow"
		if response.StatusCode != 0 {
			message = fmt.Sprintf("workflow dispatch unavailable (HTTP %d)", response.StatusCode)
		}
		return refuse("online-unavailable", message)
	}
	r.Applied = true
	return r
}

func (r DispatchResult) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "SourceSteward release dispatch: %s as %s (%s)\n", r.Mode, r.Subject.Version, r.Backend)
	b.WriteString("effects: network write on --apply: starts a CI build (draft release, attestations); no tag, no publication\n")
	b.WriteString(r.Approval + "\n")
	for _, g := range r.Gates {
		fmt.Fprintf(&b, "  %s %s", g.Outcome, g.Code)
		if g.Reason != nil {
			fmt.Fprintf(&b, " %s", *g.Reason)
		}
		if g.Detail != nil {
			fmt.Fprintf(&b, " %s", *g.Detail)
		}
		b.WriteByte('\n')
	}
	for _, build := range r.WouldBuild {
		fmt.Fprintf(&b, "would build %s; network: %s; hosts: %q; lock files: %q\n", build.ID, build.Network, build.NetworkHosts, build.LockFiles)
	}
	if r.Request != nil {
		raw, _ := json.Marshal(r.Request.Body)
		fmt.Fprintf(&b, "request: %s /%s %s\nplan_id: %s\n", r.Request.Method, r.Request.Endpoint, Escape(string(raw)), r.PlanID)
	}
	fmt.Fprintf(&b, "verdict: %s\n", r.Verdict)
	if r.Error != nil {
		fmt.Fprintf(&b, "%s: %s\n", r.Error.Code, r.Error.Message)
	}
	if r.Applied {
		fmt.Fprintf(&b, "dispatched: %s\n", r.WorkflowURL)
	}
	return b.String()
}
