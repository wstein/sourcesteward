// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watch"
)

// PullDetail is the decoded `pulls/N` document: what watch reads (mergeable,
// mergeable_state, fork) plus the head SHA and rebaseable that `stw pulls` needs
// (Spec 036). Nil pointers mean GitHub answered null (not computed yet).
type PullDetail struct {
	Mergeable      *bool
	MergeableState string
	Rebaseable     *bool
	HeadSHA        string
	// Fork is true when the head repository is missing or differs from the base repository.
	Fork bool
}

// DecodePullDetail decodes the body of one `pulls/N` read.
func DecodePullDetail(body []byte) (PullDetail, error) {
	var doc struct {
		Mergeable      *bool  `json:"mergeable"`
		MergeableState string `json:"mergeable_state"`
		Rebaseable     *bool  `json:"rebaseable"`
		Head           struct {
			SHA  string `json:"sha"`
			Repo *struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Repo *struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"base"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return PullDetail{}, &hosting.WatchError{Code: hosting.CodeMalformed, Message: "pull request response is not usable"}
	}
	return PullDetail{
		Mergeable:      doc.Mergeable,
		MergeableState: doc.MergeableState,
		Rebaseable:     doc.Rebaseable,
		HeadSHA:        doc.Head.SHA,
		Fork:           doc.Head.Repo == nil || doc.Base.Repo == nil || !strings.EqualFold(doc.Head.Repo.FullName, doc.Base.Repo.FullName),
	}, nil
}

// ReadPullDetail reads and decodes `pulls/N` without a validator: a snapshot reader keeps no ETags.
func ReadPullDetail(ctx context.Context, r Reader, repo string, n int) (PullDetail, error) {
	res, err := r.PullDetail(ctx, repo, n, "")
	if err != nil {
		return PullDetail{}, err
	}
	if res.NotModified {
		return PullDetail{}, &hosting.WatchError{Code: hosting.CodeMalformed, Message: "unexpected 304 for an unconditional read"}
	}
	return DecodePullDetail(res.Body)
}

// ReadChecks reads the check runs and commit statuses of one head without validators and returns
// them with their class (`none|pending|green|red`, the watch classification with default patterns).
func ReadChecks(ctx context.Context, r Reader, repo, sha string) (*watch.ChecksObs, string, error) {
	runs, _, _, err := readRuns(ctx, r, repo, sha, "")
	if err != nil {
		return nil, "", err
	}
	stats, _, _, err := readStatuses(ctx, r, repo, sha, "")
	if err != nil {
		return nil, "", err
	}
	class := watch.ClassifyChecks(runs, stats, "", "").Class
	return &watch.ChecksObs{Runs: runs, Statuses: stats}, class, nil
}
