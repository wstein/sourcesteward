// SPDX-License-Identifier: EUPL-1.2
package pulls

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/reviewstate"
)

// ReviewSource is what the review pass reads beyond Reader (Spec 046): the comments of a pull
// request, the closed list and the repository (default branch). *hosting.WatchReader implements it;
// a reader that does not leaves every review nil. The head commit date comes from Reader.Commit.
type ReviewSource interface {
	ReadIssueComments(ctx context.Context, repo string, number int) ([]hosting.IssueComment, bool, error)
	ClosedPulls(ctx context.Context, repo string) (hosting.WatchResponse, error)
	RepoInfo(ctx context.Context, repo string) (hosting.RepoInfo, error)
}

var _ ReviewSource = (*hosting.WatchReader)(nil)

// ReviewInfo is the review state of one pull request with the evidence of its decisive comment
// (Spec 046). The verdict is the claim of a comment, not proof; At and URL are zero/empty without one.
type ReviewInfo struct {
	State, Verdict, Head, PatchID, URL string
	At                                 time.Time
	Carried, Edited, Truncated         bool
}

// ReviewCounts counts rows per review state; NotRead counts rows whose comments were not read.
type ReviewCounts struct{ None, Findings, Clear, ClearCarried, ClearStale, Unparsed, NotRead int }

// HintCounts counts rows per hint.
type HintCounts struct {
	ClearNotReady, NoFinalReview, BaseNotDefault, ParentMergedRetarget, ReadyMergeable int
}

// validateReview checks the review options of Run.
func validateReview(o Options, supported bool) error {
	filtered := len(o.ReviewStates) > 0 || len(o.Hints) > 0
	if filtered && !o.Review {
		return fmt.Errorf("a review or hint filter needs the review state to be read")
	}
	if filtered && !supported {
		return fmt.Errorf("a review or hint filter needs a reader that reads comments")
	}
	for _, s := range o.ReviewStates {
		if !contains(reviewstate.States, s) {
			return fmt.Errorf("unknown review state %q", s)
		}
	}
	for _, h := range o.Hints {
		if !contains(reviewstate.AllHints, h) {
			return fmt.Errorf("unknown hint %q", h)
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// review reads the review state of the rows within the --limit budget (newest first, drafts
// included), the default branch of every repository and, where a base is not the default branch,
// the closed list. It stops after a rate limit and when the context expires, like the detail pass.
func (c *cycle) review(ctx context.Context, src ReviewSource, all []*item, index map[string]int, status []RepoStatus) {
	budget := c.o.Limit
	selected := map[string][]*item{}
	every := map[string][]*item{}
	for _, it := range all {
		every[it.repo] = append(every[it.repo], it)
		if budget > 0 {
			budget--
			selected[it.repo] = append(selected[it.repo], it)
		}
	}
	repos := make([]string, 0, len(every))
	for repo := range every {
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	pool(len(repos), c.o.Parallel, func(i int) {
		repo := repos[i]
		st := &status[index[repo]]
		stop := func() bool {
			if ctx.Err() != nil {
				st.Deadline = true
				return true
			}
			if c.limited.Load() {
				c.fail(st, repo, hosting.CodeRateLimited, "rate limit reached before the review state was read")
				return true
			}
			return false
		}
		if stop() {
			return
		}
		def := ""
		if info, err := src.RepoInfo(ctx, repo); err != nil {
			c.readFailed(ctx, st, repo, err)
		} else {
			def = info.DefaultBranch
		}
		for _, it := range every[repo] {
			it.defaultBranch = def
		}
		for _, it := range selected[repo] {
			if stop() {
				return
			}
			c.readReview(ctx, src, st, it)
		}
		if def == "" {
			return
		}
		need := false
		for _, it := range every[repo] {
			need = need || it.BaseRef != def
		}
		if !need || stop() {
			return
		}
		res, err := src.ClosedPulls(ctx, repo)
		var merged map[string]bool
		if err == nil {
			var list []hosting.ClosedPull
			if list, err = hosting.ParseClosedPulls(res.Body); err == nil {
				merged = map[string]bool{}
				for _, p := range list {
					if p.Merged && !p.Fork {
						merged[p.HeadRef] = true
					}
				}
			}
		}
		if err != nil {
			c.readFailed(ctx, st, repo, err)
			return
		}
		for _, it := range every[repo] {
			it.parentMerged = it.BaseRef != def && merged[it.BaseRef]
		}
	})
}

// readReview reads the comments of one pull request and, only when a strict comment names its head,
// the committer date of that head. Any failure leaves the review nil and is reported.
func (c *cycle) readReview(ctx context.Context, src ReviewSource, st *RepoStatus, it *item) {
	if !ValidSHA(it.HeadSHA) {
		return
	}
	got, truncated, err := src.ReadIssueComments(ctx, it.repo, it.Number)
	if err != nil {
		c.readFailed(ctx, st, it.repo, err)
		return
	}
	comments := make([]reviewstate.Comment, len(got))
	for i, g := range got {
		comments[i] = reviewstate.Comment{ID: g.ID, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt, Body: g.Body, URL: g.URL}
	}
	var headTime time.Time
	if reviewstate.NeedsHeadTime(it.Number, it.HeadSHA, comments) {
		res, err := c.r.Commit(ctx, it.repo, it.HeadSHA, "")
		if err == nil {
			headTime, err = hosting.ParseCommitDate(res.Body)
		}
		if err != nil {
			c.readFailed(ctx, st, it.repo, err)
			return
		}
	}
	r := reviewstate.Evaluate(reviewstate.Input{PR: it.Number, Head: it.HeadSHA, HeadTime: headTime, Comments: comments, Truncated: truncated})
	it.row.Review = &ReviewInfo{State: r.State, Verdict: r.Verdict, Head: r.Head, PatchID: r.PatchID, URL: clip(r.URL), At: r.At,
		Carried: r.Carried, Edited: r.Edited, Truncated: r.Truncated}
}

// hints derives the hints of a finished row.
func (c *cycle) hints(it *item) []string {
	in := reviewstate.HintInput{Draft: it.row.Draft, Group: it.row.Group, Base: it.row.Base, DefaultBranch: it.defaultBranch, ParentMerged: it.parentMerged}
	if it.row.Review != nil {
		in.ReviewRead, in.State = true, it.row.Review.State
	}
	if h := reviewstate.Hints(in); h != nil {
		return h
	}
	return []string{}
}

// keepReview applies --review and --hint to a finished row: OR within a flag, AND across both. A
// row whose comments were not read matches neither flag.
func (c *cycle) keepReview(row Row) bool {
	if len(c.o.ReviewStates) > 0 && (row.Review == nil || !contains(c.o.ReviewStates, row.Review.State)) {
		return false
	}
	if len(c.o.Hints) > 0 {
		if row.Review == nil {
			return false
		}
		for _, h := range row.Hints {
			if contains(c.o.Hints, h) {
				return true
			}
		}
		return false
	}
	return true
}

func countReview(rows []Row) (ReviewCounts, HintCounts) {
	var rc ReviewCounts
	var hc HintCounts
	for _, r := range rows {
		if r.Review == nil {
			rc.NotRead++
		} else {
			switch r.Review.State {
			case reviewstate.StateNone:
				rc.None++
			case reviewstate.StateFindings:
				rc.Findings++
			case reviewstate.StateClear:
				rc.Clear++
			case reviewstate.StateClearCarried:
				rc.ClearCarried++
			case reviewstate.StateClearStale:
				rc.ClearStale++
			case reviewstate.StateUnparsed:
				rc.Unparsed++
			}
		}
		for _, h := range r.Hints {
			switch h {
			case reviewstate.HintClearNotReady:
				hc.ClearNotReady++
			case reviewstate.HintNoFinalReview:
				hc.NoFinalReview++
			case reviewstate.HintBaseNotDefault:
				hc.BaseNotDefault++
			case reviewstate.HintParentMergedRetarget:
				hc.ParentMergedRetarget++
			case reviewstate.HintReadyMergeable:
				hc.ReadyMergeable++
			}
		}
	}
	return rc, hc
}
