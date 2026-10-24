// SPDX-License-Identifier: EUPL-1.2
package statusremote

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// The CI sources of one repository (Spec 037, WP-C): `ci-main`, the check class of the default
// branch head and whether the local clone lags origin, and `ci-release`, the newest runs of the
// release workflow for the latest tag of each product and for the unreleased tags. Each function
// makes its requests one after the other and reports one source state; a failed or skipped source has
// no data.

const (
	// maxCIRuns is the number of runs of one tag that are kept, newest first.
	maxCIRuns = 20
	// maxUnreleasedTags is the number of unreleased tags of a repository that are read (Spec 037); the latest
	// tag of every pattern is read besides, so a repository makes N+5 runs reads of page 1 for N patterns.
	maxUnreleasedTags = 5
)

// CIReader is the reader of the CI sources: the watch reads of the check classification and the
// fixed workflow run read. *hosting.WatchReader implements it.
type CIReader interface {
	watchcmd.Reader
	WorkflowRuns(ctx context.Context, repo, workflow, sha string) (hosting.WatchResponse, error)
}

var _ CIReader = (*hosting.WatchReader)(nil)

// CIMainResult is the outcome of the `ci-main` source; Main is set exactly when the state is ok.
type CIMainResult struct {
	State overview.SourceState
	Main  *overview.Main
}

// ReadCIMain reads the head of the default branch and classifies its checks with the classification of
// `stw watch` and `stw pulls`. dir is the local clone: with it the result says whether the clone lacks
// the head of origin, decided in the local object store alone (BehindOrigin stays nil without a
// clone or when the local branch does not exist). Nothing is fetched.
func ReadCIMain(ctx context.Context, r CIReader, repo, branch, dir string) CIMainResult {
	res, err := r.Branch(ctx, repo, branch, "")
	if err != nil {
		return CIMainResult{State: ciState(ctx, err)}
	}
	head, err := hosting.DecodeBranchTip(res.Body)
	if err != nil {
		return CIMainResult{State: ciState(ctx, err)}
	}
	_, class, err := watchcmd.ReadChecks(ctx, r, repo, head)
	if err != nil {
		return CIMainResult{State: ciState(ctx, err)}
	}
	main := &overview.Main{Branch: branch, Head: head, Class: class}
	if dir != "" {
		if behind, known := stwsync.BehindOrigin(ctx, dir, branch, head); known {
			main.BehindOrigin = &behind
		}
	}
	return CIMainResult{State: overview.SourceState{Outcome: overview.OutcomeOK}, Main: main}
}

// CodeContractInvalid is the error code of `ci-release` when a release contract exists but cannot be
// used: the owner must fix it, the defaults are not applied in its place.
const CodeContractInvalid = "contract-invalid"

// CIReleaseInput is what the `ci-release` source needs: the repository (OWNER/NAME), the release
// products read from its contracts and the tag entries that the `tags` source found with them
// (Releases.Tags: one entry per pattern and tag, the latest of the pattern or unreleased). The runs
// are added to those entries; which tags matter is not decided here.
type CIReleaseInput struct {
	Repo     string
	Products ReleaseProducts
	Tags     []overview.ReleaseTag
}

// CIReleaseTag is a tag entry with the runs of the release workflow of its product, newest first.
type CIReleaseTag struct {
	Pattern    string
	Product    string
	Tag        string
	SHA        string
	Latest     bool
	Unreleased bool
	Workflow   string
	Runs       []overview.Run
}

// CIRelease is the data of the `ci-release` source: the tags read, in the order of the input.
type CIRelease struct {
	Tags []CIReleaseTag
}

// ReleaseTags gives the entries the way the action model reads them: the newest run decides a failure.
func (c CIRelease) ReleaseTags() []overview.ReleaseTag {
	out := make([]overview.ReleaseTag, len(c.Tags))
	for i, t := range c.Tags {
		out[i] = overview.ReleaseTag{Pattern: t.Pattern, Tag: t.Tag, SHA: t.SHA, Latest: t.Latest, Unreleased: t.Unreleased}
		if len(t.Runs) > 0 {
			out[i].Run = &t.Runs[0]
		}
	}
	return out
}

// CIReleaseResult is the outcome of the `ci-release` source; Release is set exactly when the state is ok.
type CIReleaseResult struct {
	State   overview.SourceState
	Release *CIRelease
}

// ReadCIRelease adds the newest runs of the release workflow to the latest tag entry of every pattern and the first maxUnreleasedTags unreleased ones: for
// each, the runs of the workflow of its product (release.yml for a pattern no contract declares) for
// the commit of the tag, at most 20, newest first. A run of another commit is ignored. The default workflow of a pattern no contract declares that the forge does not know is not applicable (skipped, not-found, when no entry has one); a declared workflow it does not know ends the source skipped, not-found, with CodeDeclaredWorkflow. The entries read are kept with every state; an existing but unusable
// contract ends it with the error code contract-invalid before any request.
func ReadCIRelease(ctx context.Context, r CIReader, in CIReleaseInput) CIReleaseResult {
	if in.Products.invalid {
		return CIReleaseResult{State: overview.SourceState{Outcome: overview.OutcomeError, Code: CodeContractInvalid}}
	}
	entries := tagsToRead(in.Tags)
	out := &CIRelease{Tags: []CIReleaseTag{}}
	defaultMissing, declaredMissing := false, false
	for _, e := range entries {
		product, workflow := in.Products.of(e.Pattern)
		res, err := r.WorkflowRuns(ctx, in.Repo, workflow, e.SHA)
		if isWorkflowNotFound(err) {
			// Only the default release.yml of a pattern nobody declares is not applicable; a workflow a
			// contract declares and the forge does not know is a gap. Either way the entries read stay.
			if product == "" {
				defaultMissing = true
			} else {
				declaredMissing = true
			}
			continue
		}
		if err != nil {
			return CIReleaseResult{State: ciState(ctx, err), Release: partial(out)}
		}
		runs, err := hosting.DecodeWorkflowRuns(res.Body)
		if err != nil {
			return CIReleaseResult{State: ciState(ctx, err), Release: partial(out)}
		}
		out.Tags = append(out.Tags, CIReleaseTag{Pattern: e.Pattern, Product: product, Tag: e.Tag, SHA: e.SHA, Latest: e.Latest,
			Unreleased: e.Unreleased, Workflow: workflow, Runs: newestRuns(in.Repo, workflow, e.SHA, runs)})
	}
	switch {
	case declaredMissing:
		return CIReleaseResult{State: overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonNotFound, Code: CodeDeclaredWorkflow}, Release: partial(out)}
	case defaultMissing && len(out.Tags) == 0:
		return CIReleaseResult{State: overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonNotFound}}
	}
	return CIReleaseResult{State: overview.SourceState{Outcome: overview.OutcomeOK}, Release: out}
}

// partial is the entries read before a source did not end ok, or nil when there are none.
func partial(c *CIRelease) *CIRelease {
	if len(c.Tags) == 0 {
		return nil
	}
	return c
}

// CodeDeclaredWorkflow marks, in the Code of a source skipped as not-found, that the missing workflow is one
// a contract declares: a gap that is reported, not the fact that a repository has no release workflow. It is
// internal and never printed.
const CodeDeclaredWorkflow = "declared-workflow"

// isWorkflowNotFound is true for the 404 of a workflow the forge does not know.
func isWorkflowNotFound(err error) bool {
	var we *hosting.WatchError
	return errors.As(err, &we) && we.Code == hosting.CodeForbidden && we.Status == 404
}

// tagsToRead keeps, in order, every latest entry and the unreleased entries up to maxUnreleasedTags; an
// entry that is both counts as one of the unreleased.
func tagsToRead(entries []overview.ReleaseTag) []overview.ReleaseTag {
	var out []overview.ReleaseTag
	unreleased := 0
	for _, e := range entries {
		if e.Unreleased {
			unreleased++
		}
		if e.Latest || (e.Unreleased && unreleased <= maxUnreleasedTags) {
			out = append(out, e)
		}
	}
	return out
}

// newestRuns keeps the runs of the commit, orders them newest first by creation time (the order of the
// response is not relied on; equal times keep it) and keeps the first maxCIRuns. The forge's strings
// are data: a page that is not under the repository is dropped, and an unknown status or conclusion
// becomes empty, which the action model treats as information only.
func newestRuns(repo, workflow, sha string, runs []hosting.WorkflowRun) []overview.Run {
	var mine []hosting.WorkflowRun
	for _, run := range runs {
		if run.HeadSHA == sha {
			mine = append(mine, run)
		}
	}
	sort.SliceStable(mine, func(i, j int) bool { return mine[i].CreatedAt.After(mine[j].CreatedAt) })
	if len(mine) > maxCIRuns {
		mine = mine[:maxCIRuns]
	}
	out := make([]overview.Run, len(mine))
	for i, run := range mine {
		status := knownRunStatus(run.Status)
		conclusion := ""
		if status == "completed" {
			conclusion = knownRunConclusion(run.Conclusion)
		}
		out[i] = overview.Run{Workflow: workflow, Conclusion: conclusion, Status: status, URL: repoPageURL(repo, run.URL)}
	}
	return out
}

func knownRunStatus(s string) string {
	switch s {
	case "queued", "in_progress", "completed", "waiting", "requested", "pending":
		return s
	}
	return ""
}

func knownRunConclusion(s string) string {
	switch s {
	case "success", "failure", "cancelled", "timed_out", "neutral", "skipped", "action_required", "startup_failure", "stale":
		return s
	}
	return ""
}

// repoPageURL returns raw when it is an https page of the forge under the repository (`/OWNER/NAME/`,
// the case of the name is not significant) and no URL otherwise.
func repoPageURL(repo, raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != forgeHost || u.User != nil || u.Opaque != "" {
		return ""
	}
	prefix := "/" + repo + "/"
	if len(u.Path) <= len(prefix) || !strings.EqualFold(u.Path[:len(prefix)], prefix) || strings.Contains(u.Path, "/../") {
		return ""
	}
	return raw
}

// forgeHost is the host of the pages the runs link to.
const forgeHost = "github.com"

// ciState turns a failed read into the state of its source: skipped with the deadline reason when the
// run's own context ended during a request, otherwise an error with the code of the read.
func ciState(ctx context.Context, err error) overview.SourceState {
	code := hosting.CodeNetwork
	var we *hosting.WatchError
	if errors.As(err, &we) {
		code = we.Code
	}
	if code == hosting.CodeNetwork && ctx.Err() != nil {
		return overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline}
	}
	return overview.SourceState{Outcome: overview.OutcomeError, Code: code}
}
