// SPDX-License-Identifier: EUPL-1.2
package issues

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

const (
	defaultLimit = 200
	MaxLimit     = 500  // largest accepted Options.Limit
	maxPages     = 10   // pages of 100 entries per repository
	maxListed    = 20   // labels and assignees kept in a row
	maxString    = 2048 // bytes of any row string
	parallelism  = 4    // repositories read at the same time
)

// Reader is the read-only part of the hosting watch reader that the cycle uses;
// *hosting.WatchReader implements it. Every method is a GET.
type Reader interface {
	IssuesPage(ctx context.Context, repo string, page int) (hosting.WatchResponse, error)
	Viewer(ctx context.Context) (string, error)
}

var _ Reader = (*hosting.WatchReader)(nil)

// Options selects the repositories, the filters and the row cap. The filters are applied
// locally to the decoded issues and never reach a request. Repeats of one filter are
// alternatives (any of); different filters intersect; empty means all.
type Options struct {
	Repos     []string // OWNER/NAME; de-duplicated case-insensitively and sorted by the cycle
	Labels    []string // matched against every label
	Assignees []string // a login, AssigneeMe or AssigneeNone
	Authors   []string // a login, with its [bot] marker
	Limit     int      // row cap of the output, 0 means 200, at most 500
	Now       func() time.Time
}

// Run reads the snapshot. A repository that cannot be read is listed in Result.Repos and
// Result.Errors with no rows; the others are still read. Nothing is ever slept or retried.
func Run(ctx context.Context, r Reader, o Options) (*Result, error) {
	if o.Limit == 0 {
		o.Limit = defaultLimit
	}
	if o.Limit < 1 || o.Limit > MaxLimit {
		return nil, fmt.Errorf("limit %d is outside 1..%d", o.Limit, MaxLimit)
	}
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	res := &Result{Time: now().UTC(), Limit: o.Limit}
	c := &cycle{r: r, o: o, now: res.Time, res: res}
	repos := pulls.Dedupe(o.Repos)
	c.readViewer(ctx, len(repos) > 0)
	c.run(ctx, repos)
	return res, nil
}

type cycle struct {
	r       Reader
	o       Options
	now     time.Time
	res     *Result
	viewer  string      // the login the token resolves to; empty when unknown
	limited atomic.Bool // the core rate limit was hit: nothing is read any more
	mu      sync.Mutex
}

// listing is everything one repository gave.
type listing struct {
	items     []hosting.IssueListItem
	dropped   int
	truncated bool
}

func (c *cycle) run(ctx context.Context, repos []string) {
	// --assignee @me without a login would list a wrong, smaller set: fail the run, read nothing.
	unresolved := slices.Contains(c.o.Assignees, AssigneeMe) && c.viewer == ""
	if unresolved {
		c.res.Errors = append(c.res.Errors, Error{Code: CodeViewerUnknown, Message: "the authenticated user is unknown"})
	}
	status := make([]RepoStatus, len(repos))
	lists := make([]listing, len(repos))
	pool(len(repos), parallelism, func(i int) {
		status[i].Repo = repos[i]
		if unresolved && !c.limited.Load() {
			status[i].State, status[i].Reason = StateSkipped, CodeViewerUnknown
			return
		}
		l, err := c.list(ctx, repos[i])
		var we *hosting.WatchError
		switch {
		case errors.As(err, &we) && we.Code == hosting.CodeIssuesDisabled:
			zero := 0
			status[i].State, status[i].Reason, status[i].Open, status[i].PullsDropped = StateSkipped, hosting.CodeIssuesDisabled, &zero, &zero
		case err != nil:
			c.failErr(&status[i], repos[i], err)
		default:
			open := len(l.items)
			status[i].State, status[i].Open, status[i].PullsDropped = StateOK, &open, &l.dropped
			lists[i] = l
			if l.truncated {
				status[i].State = StatePartial
				c.fail(&status[i], repos[i], CodeTooManyOpenIssues, "more open issues and pull requests than the page limit")
			}
		}
	})

	var rows []Row
	for i, repo := range repos {
		for _, it := range lists[i].items {
			if c.keep(it) {
				rows = append(rows, c.row(repo, it))
			}
		}
	}
	sort.Slice(rows, func(a, b int) bool { return before(rows[a], rows[b]) })
	c.counts(rows)
	if len(rows) > c.o.Limit {
		c.res.Summary.Omitted = len(rows) - c.o.Limit
		rows = rows[:c.o.Limit]
	}
	c.res.Rows = rows
	c.res.Repos = status
	sort.SliceStable(c.res.Errors, func(a, b int) bool {
		x, y := c.res.Errors[a], c.res.Errors[b]
		if x.Repo != y.Repo {
			return strings.ToLower(x.Repo) < strings.ToLower(y.Repo)
		}
		return x.Code < y.Code
	})
	c.summarize()
}

// list reads the pages 1..10 of one repository by number and decodes them. The Link header
// only says that another page exists; its URL is never requested. All or nothing: a failed
// page discards the pages before it. A tenth page that still announces a next one is the
// explicit truncation: everything read is kept and listing.truncated is set.
func (c *cycle) list(ctx context.Context, repo string) (listing, error) {
	var l listing
	seen := map[int]bool{}
	for page := 1; ; page++ {
		if c.limited.Load() {
			return listing{}, &hosting.WatchError{Code: hosting.CodeRateLimited, Message: "rate limit reached before this repository was read"}
		}
		res, err := c.r.IssuesPage(ctx, repo, page)
		if err != nil {
			return listing{}, err
		}
		items, dropped, err := hosting.ParseIssueList(res.Body)
		if err != nil {
			return listing{}, err
		}
		for _, it := range items {
			if !seen[it.Number] { // concurrent updates can shift an item onto the next page: the first (oldest-first order) wins
				seen[it.Number] = true
				l.items = append(l.items, it)
			}
		}
		l.dropped += dropped
		next, more, err := hosting.LinkNextPage(res.Link)
		if err != nil {
			return listing{}, err
		}
		switch {
		case !more:
			return l, nil
		case next != page+1:
			return listing{}, &hosting.WatchError{Code: hosting.CodeMalformed, Message: "Link next is not the following page"}
		case page == maxPages:
			l.truncated = true
			return l, nil
		}
	}
}

func (c *cycle) row(repo string, it hosting.IssueListItem) Row {
	priority, reason := classify(it, c.viewer)
	row := Row{
		Repo: repo, Number: it.Number, Title: clip(it.Title), Author: clip(it.Author), Bot: isBot(it),
		CreatedAt: it.CreatedAt, Labels: capped(it.Labels), LabelsTotal: len(it.Labels), Assignees: capped(it.Assignees),
		Comments: it.Comments, Priority: priority, Reason: reason,
	}
	if strings.HasPrefix(it.HTMLURL, "https://") {
		row.URL = clip(it.HTMLURL)
	}
	if !it.CreatedAt.IsZero() {
		days := max(int(c.now.Sub(it.CreatedAt)/(24*time.Hour)), 0)
		row.AgeDays = &days
	}
	if (priority == Red || priority == Yellow) && untrusted.ValidRepo(repo) {
		row.Command, row.Effect = fmt.Sprintf("gh issue view %d -R %s", it.Number, repo), "read"
	}
	return row
}

func rank(priority string) int {
	switch priority {
	case Red:
		return 0
	case Yellow:
		return 1
	case Blue:
		return 2
	}
	return 3
}

// before is the document order: priority, then repository (ASCII case-insensitive), then number.
func before(a, b Row) bool {
	if ra, rb := rank(a.Priority), rank(b.Priority); ra != rb {
		return ra < rb
	}
	if ra, rb := strings.ToLower(a.Repo), strings.ToLower(b.Repo); ra != rb {
		return ra < rb
	}
	return a.Number < b.Number
}

// counts records what was read with the filters applied and the cap not.
func (c *cycle) counts(rows []Row) {
	for _, r := range rows {
		switch r.Priority {
		case Red:
			c.res.Summary.Counts.Red++
		case Yellow:
			c.res.Summary.Counts.Yellow++
		case Blue:
			c.res.Summary.Counts.Blue++
		default:
			c.res.Summary.Counts.Violet++
		}
	}
}

func (c *cycle) summarize() {
	s := &c.res.Summary
	s.Total = len(c.res.Rows)
	for _, r := range c.res.Rows {
		switch r.Priority {
		case Red:
			s.Red++
		case Yellow:
			s.Yellow++
		case Blue:
			s.Blue++
		default:
			s.Violet++
		}
		if r.Bot {
			s.Bots++
		}
	}
	for _, st := range c.res.Repos {
		switch {
		case st.State == StateOK, st.State == StateSkipped && st.Reason == hosting.CodeIssuesDisabled:
			s.ReposRead++
		case st.State == StatePartial, st.State == StateError:
			s.ReposFailed++
		}
	}
}

func capped(in []string) []string {
	out := make([]string, 0, min(len(in), maxListed))
	for _, s := range in {
		if len(out) == maxListed {
			break
		}
		out = append(out, clip(s))
	}
	return out
}

// clip cuts s to 2048 bytes on a UTF-8 boundary.
func clip(s string) string {
	if len(s) <= maxString {
		return s
	}
	n := maxString
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// failErr records a failed read of a repository; a rate limit stops all further reads.
func (c *cycle) failErr(st *RepoStatus, repo string, err error) {
	code, msg := hosting.CodeNetwork, "read failed"
	var we *hosting.WatchError
	if errors.As(err, &we) {
		code, msg = we.Code, we.Message
	}
	st.State = StateError
	c.fail(st, repo, code, msg)
	if code == hosting.CodeRateLimited {
		c.limited.Store(true)
	}
}

func (c *cycle) fail(st *RepoStatus, repo, code, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st.Error = code
	c.res.Errors = append(c.res.Errors, Error{Repo: repo, Code: code, Message: msg})
}

// pool runs fn(0..n-1) with at most width calls at the same time.
func pool(n, width int, fn func(int)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, width)
	for i := 0; i < n; i++ {
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer func() { <-sem; wg.Done() }()
			fn(i)
		}(i)
	}
	wg.Wait()
}
