// SPDX-License-Identifier: EUPL-1.2
package statusremote

import (
	"context"
	"errors"
	"sort"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/release"
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
	// maxUnreleasedTags is the number of unreleased tags of a repository that are read (Spec 037).
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

// CITag is a tag of the `tags` read with the commit it points at.
type CITag struct {
	Name string
	SHA  string
}

// CIReleaseInput is what the `ci-release` source needs: the repository (OWNER/NAME), its local clone
// (empty when there is none; the release contracts are read from it), the tags that the `tags` source
// read and the names of the unreleased ones among them (Spec 037: newer than the latest release, no
// release carries them).
type CIReleaseInput struct {
	Repo       string
	Dir        string
	Tags       []CITag
	Unreleased []string
}

// CIReleaseTag is a tag whose release workflow runs were read: the latest tag of Product (empty for the
// default product) or an unreleased tag, newest run first.
type CIReleaseTag struct {
	Product  string
	Tag      string
	SHA      string
	Released bool
	Runs     []overview.Run
}

// CIRelease is the data of the `ci-release` source: the single release workflow (its file name) and
// the tags read, the latest tag of each product first, then the unreleased tags.
type CIRelease struct {
	Workflow string
	Tags     []CIReleaseTag
}

// ReleaseTags gives the tags the way the action model reads them: the newest run decides a failure.
func (c CIRelease) ReleaseTags() []overview.ReleaseTag {
	out := make([]overview.ReleaseTag, len(c.Tags))
	for i, t := range c.Tags {
		out[i] = overview.ReleaseTag{Tag: t.Tag, SHA: t.SHA, Unreleased: !t.Released}
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

// ReadCIRelease reads, for the commit of each tag to inspect, the runs of the one release workflow of
// the repository: the `ci.release_workflow` of its release contracts, or `release.yml` when it declares
// none. The tags to inspect are the latest tag of each declared product (the default `v*.*.*` for a
// repository without contracts) and the unreleased tags, at most 5, one entry per tag name. A workflow
// the forge does not know ends the source as skipped, reason not-found.
func ReadCIRelease(ctx context.Context, r CIReader, in CIReleaseInput) CIReleaseResult {
	products := ciProducts(in.Dir)
	workflow := defaultReleaseWorkflow
	if len(products) == 0 {
		products = []ciProduct{defaultProduct}
	} else {
		workflow = products[0].workflow
	}
	out := &CIRelease{Workflow: workflow, Tags: []CIReleaseTag{}}
	for _, t := range ciTargets(products, in) {
		res, err := r.WorkflowRuns(ctx, in.Repo, workflow, t.SHA)
		if err != nil {
			return CIReleaseResult{State: ciReleaseState(ctx, err)}
		}
		runs, err := hosting.DecodeWorkflowRuns(res.Body)
		if err != nil {
			return CIReleaseResult{State: ciState(ctx, err)}
		}
		t.Runs = newestRuns(workflow, runs)
		out.Tags = append(out.Tags, t)
	}
	return CIReleaseResult{State: overview.SourceState{Outcome: overview.OutcomeOK}, Release: out}
}

// ciTargets lists the tags whose runs are read: the latest tag of each product, then the unreleased
// tags (the first maxUnreleasedTags names), each tag name once. An unreleased tag that is also a latest
// tag is one entry and not released. Names that are not among the tags read are dropped.
func ciTargets(products []ciProduct, in CIReleaseInput) []CIReleaseTag {
	bySHA := map[string]string{}
	for _, t := range in.Tags {
		if _, dup := bySHA[t.Name]; !dup {
			bySHA[t.Name] = t.SHA
		}
	}
	var targets []CIReleaseTag
	index := map[string]int{}
	add := func(product, tag string, released bool) {
		if i, ok := index[tag]; ok {
			targets[i].Released = targets[i].Released && released
			return
		}
		index[tag] = len(targets)
		targets = append(targets, CIReleaseTag{Product: product, Tag: tag, SHA: bySHA[tag], Released: released})
	}
	for _, p := range products {
		if latest, ok := latestTag(p.scheme, in.Tags); ok {
			add(p.id, latest, true)
		}
	}
	unreleased := in.Unreleased
	if len(unreleased) > maxUnreleasedTags {
		unreleased = unreleased[:maxUnreleasedTags]
	}
	for _, name := range unreleased {
		if _, known := bySHA[name]; known {
			add(productOf(products, name), name, false)
		}
	}
	return targets
}

// latestTag returns the tag of the highest version among those the scheme accepts; equal versions
// (`v1.0.0` and `1.0.0`) resolve to the greater name, so the choice does not depend on the order read.
func latestTag(scheme *release.VersionScheme, tags []CITag) (string, bool) {
	best, found := "", false
	var bestTuple release.VersionTuple
	for _, t := range tags {
		_, tuple, ok := scheme.ParseTag(t.Name)
		if !ok {
			continue
		}
		cmp := release.CompareVersionTuples(tuple, bestTuple)
		if !found || cmp > 0 || (cmp == 0 && t.Name > best) {
			best, bestTuple, found = t.Name, tuple, true
		}
	}
	return best, found
}

// productOf names the first product whose grammar accepts the tag, or no product.
func productOf(products []ciProduct, tag string) string {
	for _, p := range products {
		if _, _, ok := p.scheme.ParseTag(tag); ok {
			return p.id
		}
	}
	return ""
}

// newestRuns orders the runs newest first by creation time (the order of the response is not relied on;
// equal times keep it) and keeps the first maxCIRuns.
func newestRuns(workflow string, runs []hosting.WorkflowRun) []overview.Run {
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].CreatedAt.After(runs[j].CreatedAt) })
	if len(runs) > maxCIRuns {
		runs = runs[:maxCIRuns]
	}
	out := make([]overview.Run, len(runs))
	for i, run := range runs {
		out[i] = overview.Run{Workflow: workflow, Conclusion: run.Conclusion, Status: run.Status, URL: run.URL}
	}
	return out
}

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

// ciReleaseState is ciState for the workflow runs, where a 404 means the workflow does not exist: the
// source is skipped, not failed.
func ciReleaseState(ctx context.Context, err error) overview.SourceState {
	var we *hosting.WatchError
	if errors.As(err, &we) && we.Code == hosting.CodeForbidden && we.Status == 404 {
		return overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonNotFound}
	}
	return ciState(ctx, err)
}
