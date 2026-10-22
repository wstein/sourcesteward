// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"net/url"
	"time"
)

// This file holds the reads of the CI state of `stw status` (Spec 037, `ci-main` and `ci-release`):
// the one fixed workflow-run shape of Spec 026 and the typed decoders of the default branch tip and
// of a workflow run list. The shapes are already admitted by every guard mode, so none is added.

// KindRuns counts the workflow run reads.
const KindRuns WatchKind = "runs"

// maxWorkflowRuns is the page size of the fixed run shape.
const maxWorkflowRuns = 100

// WorkflowRuns reads page 1 of `actions/workflows/WORKFLOW/runs?head_sha=SHA&per_page=100`, the
// newest runs of one workflow file for one commit. workflow is the file name without a directory
// (`release.yml`) and sha 40 lowercase hexadecimal digits; the guard re-validates the whole request.
func (r *WatchReader) WorkflowRuns(ctx context.Context, repo, workflow, sha string) (WatchResponse, error) {
	base, err := repoPath(repo)
	if err != nil {
		return WatchResponse{}, err
	}
	if !validSHA(sha) {
		return WatchResponse{}, malformed("invalid commit SHA")
	}
	return r.fetch(ctx, KindRuns, base+"/actions/workflows/"+url.PathEscape(workflow)+"/runs?head_sha="+sha+"&per_page=100&page=1", "", true)
}

// WorkflowRun is one run of a workflow: its status (`queued`, `in_progress`, `completed`), its
// conclusion (empty until completed), its page, the commit it ran for and its creation time.
type WorkflowRun struct {
	Status     string
	Conclusion string
	URL        string
	HeadSHA    string
	CreatedAt  time.Time
}

// DecodeWorkflowRuns decodes the body of WorkflowRuns in the order GitHub gave it.
func DecodeWorkflowRuns(body []byte) ([]WorkflowRun, error) {
	var doc struct {
		Runs []struct {
			Status     string     `json:"status"`
			Conclusion *string    `json:"conclusion"`
			URL        string     `json:"html_url"`
			HeadSHA    string     `json:"head_sha"`
			CreatedAt  *time.Time `json:"created_at"`
		} `json:"workflow_runs"`
	}
	if json.Unmarshal(body, &doc) != nil || doc.Runs == nil || len(doc.Runs) > maxWorkflowRuns {
		return nil, malformed("workflow runs response is not usable")
	}
	runs := make([]WorkflowRun, 0, len(doc.Runs))
	for _, r := range doc.Runs {
		if r.Status == "" || r.CreatedAt == nil {
			return nil, malformed("workflow runs response is not usable")
		}
		run := WorkflowRun{Status: r.Status, URL: r.URL, HeadSHA: r.HeadSHA, CreatedAt: *r.CreatedAt}
		if r.Conclusion != nil {
			run.Conclusion = *r.Conclusion
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// DecodeBranchTip decodes the commit SHA of a `branches/BASE` response.
func DecodeBranchTip(body []byte) (string, error) {
	var doc struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if json.Unmarshal(body, &doc) != nil || !validSHA(doc.Commit.SHA) {
		return "", malformed("branch response is not usable")
	}
	return doc.Commit.SHA, nil
}
