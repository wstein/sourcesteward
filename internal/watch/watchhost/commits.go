// SPDX-License-Identifier: EUPL-1.2
// Package watchhost binds the hosting watch reader to the interfaces of internal/watch. The core stays free
// of hosting imports (its source test); this package is the only place that joins them.
package watchhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watch"
)

// PullCommitsReader is the part of *hosting.WatchReader the restacker needs.
type PullCommitsReader interface {
	PullCommits(ctx context.Context, repo string, number, page int, etag string) (hosting.WatchResponse, error)
}

// HostingCommits adapts the hosting watch reader to CommitLister. Commit pages are read unconditionally:
// they are only read in the cycle of a merge, so no ETag is kept.
type HostingCommits struct{ r PullCommitsReader }

// NewHostingCommits wraps r.
func NewHostingCommits(r PullCommitsReader) *HostingCommits { return &HostingCommits{r: r} }

// PullCommits implements CommitLister. A malformed answer is reported as watch.ErrMalformed.
func (h *HostingCommits) PullCommits(ctx context.Context, repo string, number, page int) ([]string, bool, error) {
	res, err := h.r.PullCommits(ctx, repo, number, page, "")
	if err != nil {
		var we *hosting.WatchError
		if errors.As(err, &we) && we.Code == hosting.CodeMalformed {
			return nil, false, fmt.Errorf("%w: %w", watch.ErrMalformed, err)
		}
		return nil, false, err
	}
	var items []struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(res.Body, &items); err != nil {
		return nil, false, fmt.Errorf("%w: commit list is not a JSON array", watch.ErrMalformed)
	}
	shas := make([]string, 0, len(items))
	for _, it := range items {
		if !isSHA(it.SHA) {
			return nil, false, fmt.Errorf("%w: commit without a 40-hex sha", watch.ErrMalformed)
		}
		shas = append(shas, it.SHA)
	}
	return shas, len(items) >= 100, nil
}

func isSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

var _ watch.CommitLister = (*HostingCommits)(nil)

// NewRestacker is the restacker of `stw watch`: commit pages from the hosting reader and the hosting
// branch-segment encoder as the name check. diag may be nil.
func NewRestacker(r PullCommitsReader, diag func(string)) *watch.GitRestacker {
	return &watch.GitRestacker{
		Commits: NewHostingCommits(r),
		Diag:    diag,
		BranchOK: func(name string) error {
			_, err := hosting.EncodeBranchSegment(name)
			return err
		},
	}
}
