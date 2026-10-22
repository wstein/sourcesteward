// SPDX-License-Identifier: EUPL-1.2

// Package pulls is the stateless, read-only cycle behind `stw pulls` (Spec 036): list the
// open pull requests of a set of repositories, read pulls/N and the checks of as many as the
// detail budget allows, and give every PR a group and reason by the ordered verdict table.
// It writes nothing: no file, no state, no forge write.
package pulls

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watch"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// Reader is the read-only part of the hosting watch reader that the cycle uses;
// *hosting.WatchReader implements it. Every method is a GET.
type Reader interface {
	watchcmd.Reader
	ListOpenPullItems(ctx context.Context, repo string, maxOpen int) ([]hosting.PullListItem, error)
}

var _ Reader = (*hosting.WatchReader)(nil)

// Options selects the repositories, the filters and the detail budget.
type Options struct {
	Repos        []string // OWNER/NAME; de-duplicated case-insensitively and sorted by the cycle
	NoBots       bool
	BotsOnly     bool
	Authors      []string // case-insensitive; empty means all
	DetailDrafts bool
	Limit        int // detail budget, 0 means 200, at most 500
	Parallel     int // repositories read at the same time, 0 means 4
	Now          func() time.Time
}

// Row is one open pull request with its verdict. Pointer fields are null in the JSON
// contract: Checks, Mergeable, MergeableState and Rebaseable are nil without detail,
// AgeDays is nil for an unknown created_at.
type Row struct {
	Repo, Title, URL, Author, Base, HeadRef, Head string
	Number                                        int
	Bot, Draft, Fork                              bool
	CreatedAt                                     time.Time // zero when GitHub gave none
	AgeDays                                       *int
	Labels, Failing, Reviewers                    []string
	Checks                                        *string
	Mergeable                                     *bool
	MergeableState                                *string
	Rebaseable                                    *bool
	Group, Reason                                 string // Reason is empty unless Group is blocked
	Ready                                         bool
	Detail                                        bool

	PastBudget bool // not selected for detail by the budget (counted by summary.not_read); a failed or skipped read is not past the budget
	headOK     bool // pulls/N reported the listed head (validated)
}

// RepoStatus is the per-repository line of the document.
type RepoStatus struct {
	Repo   string
	Open   *int // open PRs seen, nil when the list could not be read
	Listed int  // rows after the filters
	Error  string
}

// Error is one failure of a repository.
type Error struct{ Repo, Code, Message string }

// Summary counts the rows. NotRead counts only the non-draft rows past the detail budget
// (--limit); a read that failed or was skipped after a rate limit keeps reason not-read on its
// row but is reported through Errors and the repository's Error.
type Summary struct {
	Total, Ready, Checks, Conflict, Draft, Blocked, Bots, NotRead, ReposRead, ReposFailed int
}

// Result is the snapshot. Rows are newest first (created_at, then repository, then number;
// an unknown created_at last), the order in which the detail budget was spent.
type Result struct {
	Time    time.Time
	Limit   int
	Rows    []Row
	Repos   []RepoStatus
	Errors  []Error
	Summary Summary
}

// Run reads the snapshot. An error is returned only for unusable options or a cancelled
// context; failures of a repository are in Result.Errors.
func Run(ctx context.Context, r Reader, o Options) (*Result, error) {
	if o.Limit == 0 {
		o.Limit = defaultLimit
	}
	if o.Limit < 1 || o.Limit > MaxLimit {
		return nil, fmt.Errorf("limit %d is outside 1..%d", o.Limit, MaxLimit)
	}
	if o.Parallel < 1 {
		o.Parallel = defaultParallel
	}
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	res := &Result{Time: now().UTC(), Limit: o.Limit}
	c := &cycle{r: r, o: o, now: res.Time, res: res}
	c.run(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return res, nil
}

type cycle struct {
	r       Reader
	o       Options
	now     time.Time
	res     *Result
	limited atomic.Bool // the core rate limit was hit: nothing is read any more
	mu      sync.Mutex
}

type item struct {
	repo string
	hosting.PullListItem
	row    Row
	budget bool // selected for detail
	reread bool // detail says null/unknown mergeability on an otherwise judgeable row
}

func (c *cycle) run(ctx context.Context) {
	repos := Dedupe(c.o.Repos)
	status := make([]RepoStatus, len(repos))
	lists := make([][]hosting.PullListItem, len(repos))
	for i, repo := range repos {
		status[i].Repo = repo
	}
	pool(len(repos), c.o.Parallel, func(i int) {
		if c.limited.Load() || ctx.Err() != nil {
			c.fail(&status[i], repos[i], hosting.CodeRateLimited, "rate limit reached before this repository was read")
			return
		}
		items, err := c.r.ListOpenPullItems(ctx, repos[i], hosting.WatchMaxOpenPRs)
		if err != nil {
			c.failErr(&status[i], repos[i], err)
			return
		}
		n := len(items)
		status[i].Open = &n
		lists[i] = items
	})

	var all []*item
	for i, repo := range repos {
		for _, p := range lists[i] {
			if !c.keep(p) {
				continue
			}
			it := &item{repo: repo, PullListItem: p}
			it.row = c.baseRow(repo, p)
			all = append(all, it)
			status[i].Listed++
		}
	}
	sort.SliceStable(all, func(a, b int) bool { return newer(all[a], all[b]) })

	budget := c.o.Limit
	byRepo := map[string][]*item{}
	for _, it := range all {
		if (it.Draft && !c.o.DetailDrafts) || budget == 0 {
			continue
		}
		budget--
		it.budget = true
		byRepo[it.repo] = append(byRepo[it.repo], it)
	}
	read := make([]string, 0, len(byRepo))
	for repo := range byRepo {
		read = append(read, repo)
	}
	sort.Strings(read)
	index := map[string]int{}
	for i, repo := range repos {
		index[repo] = i
	}
	pool(len(read), c.o.Parallel, func(i int) {
		for _, it := range byRepo[read[i]] {
			if c.limited.Load() || ctx.Err() != nil {
				c.fail(&status[index[read[i]]], read[i], hosting.CodeRateLimited, "rate limit reached before this pull request was read")
				return
			}
			if err := c.readDetail(ctx, it); err != nil {
				c.failErr(&status[index[read[i]]], read[i], err)
			}
		}
	})

	c.reread(ctx, all, read, index, status)

	for _, it := range all {
		it.row.PastBudget = !it.budget
		c.finish(it)
		c.res.Rows = append(c.res.Rows, it.row)
	}
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

// reread gives every row whose pulls/N came back with mergeable null or mergeable_state
// "unknown" (GitHub computes mergeability lazily) ONE more pulls/N read, after all first reads
// so that GitHub had time to compute it. No sleep, no loop, no second re-read, no check read;
// the reads are bounded by the number of such rows and do not use the --limit budget. After a
// rate limit none is made and the rows stay unknown.
func (c *cycle) reread(ctx context.Context, all []*item, repos []string, index map[string]int, status []RepoStatus) {
	byRepo := map[string][]*item{}
	for _, it := range all {
		if it.reread {
			byRepo[it.repo] = append(byRepo[it.repo], it)
		}
	}
	pool(len(repos), c.o.Parallel, func(i int) {
		for _, it := range byRepo[repos[i]] {
			if c.limited.Load() || ctx.Err() != nil {
				return
			}
			d, err := watchcmd.ReadPullDetail(ctx, c.r, it.repo, it.Number)
			if err != nil {
				c.failErr(&status[index[repos[i]]], repos[i], err)
				continue
			}
			// Same guards as the first read: the re-read must report the listed head.
			it.row.Fork, it.row.Mergeable, it.row.Rebaseable = d.Fork, d.Mergeable, d.Rebaseable
			state := clip(d.MergeableState)
			it.row.MergeableState = &state
			it.row.headOK = ValidSHA(d.HeadSHA) && d.HeadSHA == it.HeadSHA
		}
	})
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

// Dedupe sorts by name and drops case-insensitive duplicates (the first spelling wins).
func Dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		k := strings.ToLower(s)
		if !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return strings.ToLower(out[a]) < strings.ToLower(out[b]) })
	return out
}

func isBot(p hosting.PullListItem) bool {
	return p.UserType == "Bot" || strings.HasSuffix(p.User, "[bot]")
}

func (c *cycle) keep(p hosting.PullListItem) bool {
	bot := isBot(p)
	if (c.o.NoBots && bot) || (c.o.BotsOnly && !bot) {
		return false
	}
	if len(c.o.Authors) == 0 {
		return true
	}
	for _, a := range c.o.Authors {
		if strings.EqualFold(a, p.User) {
			return true
		}
	}
	return false
}

// newer orders newest first by created_at, then repository, then number; an unknown
// created_at sorts after every known one.
func newer(a, b *item) bool {
	za, zb := a.CreatedAt.IsZero(), b.CreatedAt.IsZero()
	switch {
	case za != zb:
		return zb
	case !za && !a.CreatedAt.Equal(b.CreatedAt):
		return a.CreatedAt.After(b.CreatedAt)
	case a.repo != b.repo:
		return strings.ToLower(a.repo) < strings.ToLower(b.repo)
	}
	return a.Number < b.Number
}

func (c *cycle) baseRow(repo string, p hosting.PullListItem) Row {
	row := Row{
		Repo: repo, Number: p.Number, Title: clip(p.Title), Author: clip(p.User), Bot: isBot(p), Draft: p.Draft,
		Base: clip(p.BaseRef), HeadRef: clip(p.HeadRef), Head: p.HeadSHA, CreatedAt: p.CreatedAt,
		Labels: capped(p.Labels), Failing: []string{}, Reviewers: capped(append(append([]string{}, p.Reviewers...), p.Teams...)),
	}
	if strings.HasPrefix(p.HTMLURL, "https://") {
		row.URL = clip(p.HTMLURL)
	}
	if !p.CreatedAt.IsZero() {
		days := int(c.now.Sub(p.CreatedAt) / (24 * time.Hour))
		if days < 0 {
			days = 0
		}
		row.AgeDays = &days
	}
	return row
}

func capped(in []string) []string {
	out := make([]string, 0, len(in))
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
	if len(s) <= MaxString {
		return s
	}
	n := MaxString
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// readDetail reads pulls/N and the checks of the listed head. Any failure leaves the item
// without detail (blocked, not-read) and is returned for the repository's error list.
func (c *cycle) readDetail(ctx context.Context, it *item) error {
	d, err := watchcmd.ReadPullDetail(ctx, c.r, it.repo, it.Number)
	if err != nil {
		return err
	}
	if !ValidSHA(it.HeadSHA) {
		// Nothing safe to read checks for; fail closed without a read error.
		it.row.Detail = true
		fillDetail(&it.row, d, nil, "", false)
		return nil
	}
	obs, class, err := watchcmd.ReadChecks(ctx, c.r, it.repo, it.HeadSHA)
	if err != nil {
		return err
	}
	headOK := ValidSHA(d.HeadSHA) && d.HeadSHA == it.HeadSHA
	it.row.Detail = true
	fillDetail(&it.row, d, obs, class, headOK)
	it.reread = !it.Draft && headOK && class == watch.ClassGreen && (d.Mergeable == nil || d.MergeableState == "unknown")
	return nil
}

func fillDetail(row *Row, d watchcmd.PullDetail, obs *watch.ChecksObs, class string, headOK bool) {
	row.Fork = d.Fork
	row.Mergeable = d.Mergeable
	row.Rebaseable = d.Rebaseable
	state := clip(d.MergeableState)
	row.MergeableState = &state
	if obs != nil {
		cl := class
		row.Checks = &cl
		if class == watch.ClassRed {
			row.Failing = capped(watch.ClassifyChecks(obs.Runs, obs.Statuses, "", "").Failing)
		}
	}
	row.headOK = headOK
}

func (c *cycle) finish(it *item) {
	f := facts{draft: it.row.Draft, detail: it.row.Detail, headOK: it.row.headOK}
	if f.detail {
		if it.row.Checks != nil {
			f.class = *it.row.Checks
		}
		f.mergeable, f.rebaseable = it.row.Mergeable, it.row.Rebaseable
		if it.row.MergeableState != nil {
			f.state = *it.row.MergeableState
		}
	}
	it.row.Group, it.row.Reason = verdict(f)
	it.row.Ready = it.row.Group == GroupReady
}

func (c *cycle) summarize() {
	s := &c.res.Summary
	s.Total = len(c.res.Rows)
	for _, r := range c.res.Rows {
		switch r.Group {
		case GroupReady:
			s.Ready++
		case GroupChecks:
			s.Checks++
		case GroupConflict:
			s.Conflict++
		case GroupDraft:
			s.Draft++
		default:
			s.Blocked++
		}
		if r.Bot {
			s.Bots++
		}
		if r.PastBudget && r.Reason == ReasonNotRead {
			s.NotRead++
		}
	}
	for _, st := range c.res.Repos {
		if st.Error != "" {
			s.ReposFailed++
		} else {
			s.ReposRead++
		}
	}
}

// failErr records an error of a read; a rate limit stops all further reads.
func (c *cycle) failErr(st *RepoStatus, repo string, err error) {
	code, msg := hosting.CodeNetwork, "read failed"
	var we *hosting.WatchError
	if errors.As(err, &we) {
		code, msg = we.Code, we.Message
	}
	c.fail(st, repo, code, msg)
	if code == hosting.CodeRateLimited {
		c.limited.Store(true)
	}
}

func (c *cycle) fail(st *RepoStatus, repo, code, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st.Error == "" {
		st.Error = code
	}
	if len(msg) > 512 {
		n := 512
		for n > 0 && !utf8.RuneStart(msg[n]) {
			n--
		}
		msg = msg[:n]
	}
	for _, e := range c.res.Errors {
		if e.Repo == repo && e.Code == code {
			return
		}
	}
	c.res.Errors = append(c.res.Errors, Error{Repo: repo, Code: code, Message: msg})
}
