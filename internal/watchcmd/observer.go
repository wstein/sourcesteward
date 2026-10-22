// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watch"
)

// Reader is the part of the hosting watch reader that the observer uses; *hosting.WatchReader implements it.
// Every method is a GET: the observer cannot write to a forge.
type Reader interface {
	ListOpenPulls(ctx context.Context, repo string, prior hosting.PullListState, maxOpen int) (hosting.PullListResult, error)
	PullDetail(ctx context.Context, repo string, number int, etag string) (hosting.WatchResponse, error)
	Branch(ctx context.Context, repo, name, etag string) (hosting.WatchResponse, error)
	Commit(ctx context.Context, repo, sha, etag string) (hosting.WatchResponse, error)
	PullCommits(ctx context.Context, repo string, number, page int, etag string) (hosting.WatchResponse, error)
	CheckRuns(ctx context.Context, repo, sha string, page int, etag string) (hosting.WatchResponse, error)
	Statuses(ctx context.Context, repo, sha string, page int, etag string) (hosting.WatchResponse, error)
	RateLimit(ctx context.Context) (hosting.RateInfo, error)
}

var _ Reader = (*hosting.WatchReader)(nil)
var _ watch.Observer = (*HostObserver)(nil)

const maxCheckPages = 10

// HostObserver implements watch.Observer over the conditional REST reads of the hosting watch reader
// (Spec 035, "Observation"): list pages, then pulls/N, branch tips and checks only when the state makes
// them necessary, and pulls/N once for a PR that left the open list.
type HostObserver struct {
	r             Reader
	now           func() time.Time
	maxOpen       int
	ignoreAuthors []string
	ignoreLabels  []string

	mu        sync.Mutex
	lastClass map[string]string // repo#n: check class of the last read, to confirm a debounce candidate
	holdUntil time.Time         // a rate-limit wait that the next Observe reports

	// Diag receives one-line diagnostics that are not events (never tokens); nil discards them.
	Diag   func(string)
	warned map[string]bool
}

// NewHostObserver builds the observer. maxOpen is the bound of open PRs per repository (1..2000).
func NewHostObserver(r Reader, now func() time.Time, maxOpen int, ignoreAuthors, ignoreLabels []string) *HostObserver {
	if now == nil {
		now = time.Now
	}
	if maxOpen < 1 {
		maxOpen = DefaultMaxOpenPRs
	}
	return &HostObserver{r: r, now: now, maxOpen: maxOpen, ignoreAuthors: ignoreAuthors, ignoreLabels: ignoreLabels,
		lastClass: map[string]string{}, warned: map[string]bool{}}
}

// AfterCycle reads rate_limit (free) once per cycle and applies the slowdown policy: it reports whether
// the next interval is to be doubled and arms a wait that the next Observe turns into a run-wide
// watch-error "rate-limited".
func (o *HostObserver) AfterCycle(ctx context.Context) (slow bool) {
	info, err := o.r.RateLimit(ctx)
	if err != nil {
		return false
	}
	d := hosting.AssessRateLimit(info)
	o.mu.Lock()
	defer o.mu.Unlock()
	if d.Advice == hosting.AdviceWait {
		o.holdUntil = d.Until
	}
	return d.Advice == hosting.AdviceSlow
}

func (o *HostObserver) takeHold() time.Duration {
	o.mu.Lock()
	defer o.mu.Unlock()
	until := o.holdUntil
	o.holdUntil = time.Time{}
	if d := until.Sub(o.now()); d > 0 {
		return d
	}
	return 0
}

func (o *HostObserver) ignored(it hosting.PullItem) bool {
	for _, a := range o.ignoreAuthors {
		if it.User == a {
			return true
		}
	}
	for _, l := range o.ignoreLabels {
		for _, x := range it.Labels {
			if x == l {
				return true
			}
		}
	}
	return false
}

// Observe implements watch.Observer.
func (o *HostObserver) Observe(ctx context.Context, repo string, h watch.Hints) (watch.RepoObservation, error) {
	var obs watch.RepoObservation
	if wait := o.takeHold(); wait > 0 {
		return obs, &watch.ObserveError{Code: hosting.CodeRateLimited, Message: "rate limit nearly exhausted; waiting for the reset", RunWide: true, RetryIn: wait}
	}
	cond := cloneCond(h.Cond)
	list, err := o.r.ListOpenPulls(ctx, repo, storedList(cond), o.maxOpen)
	if err != nil {
		return obs, o.fail(ctx, err)
	}
	cond.Pages = storeList(list.State)
	known := map[int]watch.KnownPR{}
	for _, k := range h.Known {
		known[k.Number] = k
	}
	open := map[int]bool{}
	for _, it := range list.Items {
		open[it.Number] = true
	}
	gone := map[int]watch.GoneObs{}
	mergedAny := false
	for _, k := range h.Known {
		if open[k.Number] {
			continue
		}
		g, ok, err := o.readGone(ctx, repo, k)
		if err != nil {
			return obs, o.fail(ctx, err)
		}
		if ok {
			gone[k.Number] = g
			mergedAny = mergedAny || g.Merged
		}
	}
	items := append([]hosting.PullItem(nil), list.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Number < items[j].Number })
	tips, err := o.readTips(ctx, repo, items, cond)
	if err != nil {
		return obs, o.fail(ctx, err)
	}
	for _, it := range items {
		p := watch.PRObs{Number: it.Number, Title: it.Title, Author: it.User, Labels: it.Labels, Draft: it.Draft, Head: it.HeadSHA,
			HeadRef: it.HeadRef, Base: it.BaseRef}
		if !o.ignored(it) {
			k, isKnown := known[it.Number]
			refresh := h.DetailEvery > 0 && (h.Cycle+it.Number)%h.DetailEvery == 0
			headSame := isKnown && k.Head == it.HeadSHA
			force := !headSame || k.DetailDue || tips[it.BaseRef] || mergedAny
			if force || refresh {
				etag := ""
				if !force {
					etag = cond.Details[detailKey(it.Number)]
				}
				d, fork, err := o.readDetail(ctx, repo, it.Number, etag, cond)
				if err != nil {
					return obs, o.fail(ctx, err)
				}
				p.Detail = d
				if d != nil { // a 304 read nothing: the accepted fork flag stays
					p.Fork, p.ForkKnown = fork, true
				}
			}
			ck := repo + "#" + strconv.Itoa(it.Number)
			o.mu.Lock()
			last := o.lastClass[ck]
			o.mu.Unlock()
			inflight := last != "" && last != k.Class || k.ClassCand != ""
			if !headSame || k.Class == "" || k.Class == watch.ClassPending || k.Class == watch.ClassNone || inflight || refresh {
				ch, class, err := o.readChecks(ctx, repo, it.HeadSHA, inflight, cond)
				if err != nil {
					return obs, o.fail(ctx, err)
				}
				p.Checks = ch
				if ch != nil {
					o.mu.Lock()
					o.lastClass[ck] = class
					o.mu.Unlock()
				}
			}
		}
		obs.PRs = append(obs.PRs, p)
	}
	obs.Gone = gone
	obs.DefaultBranch = guessDefaultBranch(items)
	pruneCond(cond, items)
	obs.Cond = cond
	return obs, nil
}

// guessDefaultBranch: the hosting allowlist has no repository read, so the default branch is "main" or
// "master" when an open PR targets it, else unknown (the core then classifies a red head conservatively).
func guessDefaultBranch(items []hosting.PullItem) string {
	has := map[string]bool{}
	for _, it := range items {
		has[it.BaseRef] = true
	}
	for _, name := range []string{"main", "master"} {
		if has[name] {
			return name
		}
	}
	return ""
}

func detailKey(n int) string { return "pulls/" + strconv.Itoa(n) }

func checksKey(sha, which string) string { return "checks/" + sha + "/" + which }

func cloneCond(c *watch.Conditional) *watch.Conditional {
	out := &watch.Conditional{Pages: map[string]watch.PageEntry{}, Tips: map[string]watch.TipEntry{}, Details: map[string]string{}}
	if c == nil {
		return out
	}
	for k, v := range c.Pages {
		out.Pages[k] = v
	}
	for k, v := range c.Tips {
		out.Tips[k] = v
	}
	for k, v := range c.Details {
		out.Details[k] = v
	}
	return out
}

// storedList decodes the pages kept in state.json (keys "PERPAGE/PAGE").
func storedList(c *watch.Conditional) hosting.PullListState {
	var st hosting.PullListState
	byPage := map[int]hosting.PullPage{}
	for key, e := range c.Pages {
		per, page, ok := splitPageKey(key)
		if !ok || (st.PerPage != 0 && st.PerPage != per) {
			return hosting.PullListState{}
		}
		var items []hosting.PullItem
		if len(e.Items) > 0 && json.Unmarshal(e.Items, &items) != nil {
			return hosting.PullListState{}
		}
		st.PerPage = per
		byPage[page] = hosting.PullPage{ETag: e.ETag, Link: e.Link, Items: items}
	}
	for page := 1; page <= len(byPage); page++ {
		p, ok := byPage[page]
		if !ok {
			return hosting.PullListState{}
		}
		st.Pages = append(st.Pages, p)
	}
	return st
}

func splitPageKey(key string) (per, page int, ok bool) {
	a, b, found := strings.Cut(key, "/")
	per, e1 := strconv.Atoi(a)
	page, e2 := strconv.Atoi(b)
	return per, page, found && e1 == nil && e2 == nil && page >= 1 && (per == 25 || per == 12)
}

func storeList(st hosting.PullListState) map[string]watch.PageEntry {
	out := map[string]watch.PageEntry{}
	for i, p := range st.Pages {
		items, _ := json.Marshal(p.Items)
		out[fmt.Sprintf("%d/%d", st.PerPage, i+1)] = watch.PageEntry{ETag: p.ETag, Link: p.Link, Items: items}
	}
	return out
}

// readTips reads branches/BASE once per distinct base branch of the PRs that are watched and reports
// which tips moved. A base branch that does not exist (404) is not a failure: its PRs just see no move.
func (o *HostObserver) readTips(ctx context.Context, repo string, items []hosting.PullItem, cond *watch.Conditional) (map[string]bool, error) {
	moved := map[string]bool{}
	seen := map[string]bool{}
	for _, it := range items {
		if o.ignored(it) || seen[it.BaseRef] {
			continue
		}
		seen[it.BaseRef] = true
		if _, err := hosting.EncodeBranchSegment(it.BaseRef); err != nil {
			// a base name the guard refuses (for example one with %) must not fail the repository
			o.mu.Lock()
			first := !o.warned[repo+"\x00"+it.BaseRef]
			o.warned[repo+"\x00"+it.BaseRef] = true
			o.mu.Unlock()
			if o.Diag != nil && first {
				o.Diag("watch: " + repo + ": the tip of base branch " + strconv.Quote(it.BaseRef) + " is not read (name not allowed); its PRs are still watched")
			}
			continue
		}
		prior, hadPrior := cond.Tips[it.BaseRef]
		res, err := o.r.Branch(ctx, repo, it.BaseRef, prior.ETag)
		if err != nil {
			var we *hosting.WatchError
			if errors.As(err, &we) && we.Code == hosting.CodeForbidden && we.Status == 404 {
				continue
			}
			return nil, err
		}
		if res.NotModified {
			continue
		}
		var doc struct {
			Commit struct {
				SHA string `json:"sha"`
			} `json:"commit"`
		}
		if json.Unmarshal(res.Body, &doc) != nil || len(doc.Commit.SHA) != 40 {
			return nil, &hosting.WatchError{Code: hosting.CodeMalformed, Message: "branch response is not usable"}
		}
		cond.Tips[it.BaseRef] = watch.TipEntry{ETag: res.ETag, SHA: doc.Commit.SHA}
		moved[it.BaseRef] = !hadPrior || prior.SHA != doc.Commit.SHA
	}
	return moved, nil
}

// readDetail reads pulls/N. A 304 is "unchanged": no detail, the accepted values stay.
func (o *HostObserver) readDetail(ctx context.Context, repo string, n int, etag string, cond *watch.Conditional) (*watch.DetailObs, bool, error) {
	res, err := o.r.PullDetail(ctx, repo, n, etag)
	if err != nil {
		return nil, false, err
	}
	if res.NotModified {
		return nil, false, nil
	}
	doc, err := DecodePullDetail(res.Body)
	if err != nil {
		return nil, false, err
	}
	if res.ETag != "" {
		cond.Details[detailKey(n)] = res.ETag
	}
	fork := doc.Fork
	return &watch.DetailObs{Mergeable: doc.Mergeable, State: doc.MergeableState}, fork, nil
}

// readGone reads pulls/N of a PR that left the open list. ok is false when the PR is still open on
// GitHub (the list lagged): it stays unresolved and is retried.
func (o *HostObserver) readGone(ctx context.Context, repo string, k watch.KnownPR) (watch.GoneObs, bool, error) {
	var g watch.GoneObs
	res, err := o.r.PullDetail(ctx, repo, k.Number, "")
	if err != nil {
		return g, false, err
	}
	var doc struct {
		State          string `json:"state"`
		Merged         bool   `json:"merged"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		MergedAt       string `json:"merged_at"`
		Base           struct {
			Ref string `json:"ref"`
		} `json:"base"`
		Head struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
	}
	if json.Unmarshal(res.Body, &doc) != nil || (doc.State != "open" && doc.State != "closed") {
		return g, false, &hosting.WatchError{Code: hosting.CodeMalformed, Message: "pull request response is not usable"}
	}
	if doc.State == "open" {
		return g, false, nil
	}
	// Values that become event details are validated here: anything else is a malformed response for this PR.
	bad := func() (watch.GoneObs, bool, error) {
		return watch.GoneObs{}, false, &hosting.WatchError{Code: hosting.CodeMalformed, Message: "pull request response has an invalid value"}
	}
	if !lowerHex40(doc.Head.SHA) || (doc.Merged && doc.MergeCommitSHA != "" && !lowerHex40(doc.MergeCommitSHA)) {
		return bad()
	}
	if _, err := time.Parse(time.RFC3339, doc.MergedAt); doc.Merged && err != nil {
		return bad()
	}
	g = watch.GoneObs{Merged: doc.Merged, MergedAt: doc.MergedAt, Base: doc.Base.Ref, Head: doc.Head.SHA, HeadRef: doc.Head.Ref, MethodHint: "unknown"}
	if !doc.Merged {
		return g, true, nil
	}
	if doc.MergeCommitSHA != "" {
		sha := doc.MergeCommitSHA
		g.MergeCommit = &sha
		g.MethodHint = o.methodHint(ctx, repo, sha)
	}
	return g, true, nil
}

func lowerHex40(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}

// methodHint: a merge commit has two parents, a rebase or squash merge one. Unreadable: unknown.
func (o *HostObserver) methodHint(ctx context.Context, repo, sha string) string {
	res, err := o.r.Commit(ctx, repo, sha, "")
	if err != nil {
		return "unknown"
	}
	var doc struct {
		Parents []json.RawMessage `json:"parents"`
	}
	if json.Unmarshal(res.Body, &doc) != nil {
		return "unknown"
	}
	switch len(doc.Parents) {
	case 2:
		return "merge"
	case 1:
		return "rebase-or-squash"
	}
	return "unknown"
}

// readChecks reads the check runs and commit statuses of one head. nil with no error is "unchanged"
// (both conditional reads answered 304). force sends no validator, so a debounce candidate is confirmed
// with a fresh read. The returned class is only used to notice a candidate in flight.
func (o *HostObserver) readChecks(ctx context.Context, repo, sha string, force bool, cond *watch.Conditional) (*watch.ChecksObs, string, error) {
	runsTag, statTag := cond.Details[checksKey(sha, "runs")], cond.Details[checksKey(sha, "statuses")]
	if force {
		runsTag, statTag = "", ""
	}
	runs, runsETag, runsSame, err := readRuns(ctx, o.r, repo, sha, runsTag)
	if err != nil {
		return nil, "", err
	}
	stats, statETag, statSame, err := readStatuses(ctx, o.r, repo, sha, statTag)
	if err != nil {
		return nil, "", err
	}
	if runsSame && statSame {
		return nil, "", nil
	}
	if runsSame { // the other half changed: the classification needs both
		if runs, runsETag, _, err = readRuns(ctx, o.r, repo, sha, ""); err != nil {
			return nil, "", err
		}
	}
	if statSame {
		if stats, statETag, _, err = readStatuses(ctx, o.r, repo, sha, ""); err != nil {
			return nil, "", err
		}
	}
	cond.Details[checksKey(sha, "runs")], cond.Details[checksKey(sha, "statuses")] = runsETag, statETag
	class := watch.ClassifyChecks(runs, stats, "", "").Class
	return &watch.ChecksObs{Runs: runs, Statuses: stats}, class, nil
}

func readRuns(ctx context.Context, r Reader, repo, sha, etag string) (runs []watch.CheckRun, first string, same bool, err error) {
	var total int
	for page := 1; page <= maxCheckPages; page++ {
		tag := ""
		if page == 1 {
			tag = etag
		}
		res, err := r.CheckRuns(ctx, repo, sha, page, tag)
		if err != nil {
			return nil, "", false, err
		}
		if res.NotModified {
			return nil, tag, true, nil
		}
		if page == 1 {
			first = res.ETag
		}
		var doc struct {
			Total int `json:"total_count"`
			Runs  []struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				Output     struct {
					Title   string `json:"title"`
					Summary string `json:"summary"`
					Text    string `json:"text"`
				} `json:"output"`
			} `json:"check_runs"`
		}
		if json.Unmarshal(res.Body, &doc) != nil {
			return nil, "", false, &hosting.WatchError{Code: hosting.CodeMalformed, Message: "check runs response is not usable"}
		}
		total = doc.Total
		for _, cr := range doc.Runs {
			runs = append(runs, watch.CheckRun{Name: cr.Name, Status: cr.Status, Conclusion: cr.Conclusion, Title: cr.Output.Title,
				Summary: cr.Output.Summary, Text: cr.Output.Text})
		}
		if len(doc.Runs) == 0 || len(runs) >= total {
			break
		}
	}
	return runs, first, false, nil
}

func readStatuses(ctx context.Context, r Reader, repo, sha, etag string) (stats []watch.StatusContext, first string, same bool, err error) {
	seen := map[string]bool{}
	for page := 1; page <= maxCheckPages; page++ {
		tag := ""
		if page == 1 {
			tag = etag
		}
		res, err := r.Statuses(ctx, repo, sha, page, tag)
		if err != nil {
			return nil, "", false, err
		}
		if res.NotModified {
			return nil, tag, true, nil
		}
		if page == 1 {
			first = res.ETag
		}
		var doc []struct {
			Context     string `json:"context"`
			State       string `json:"state"`
			Description string `json:"description"`
		}
		if json.Unmarshal(res.Body, &doc) != nil {
			return nil, "", false, &hosting.WatchError{Code: hosting.CodeMalformed, Message: "statuses response is not usable"}
		}
		for _, s := range doc { // newest first: the first entry of a context is its current state
			if !seen[s.Context] {
				seen[s.Context] = true
				stats = append(stats, watch.StatusContext{Context: s.Context, State: s.State, Description: s.Description})
			}
		}
		if len(doc) < 100 {
			break
		}
	}
	return stats, first, false, nil
}

// pruneCond drops validators of PRs and heads that are no longer open, so state.json stays bounded.
func pruneCond(c *watch.Conditional, items []hosting.PullItem) {
	keep := map[string]bool{}
	bases := map[string]bool{}
	for _, it := range items {
		keep[detailKey(it.Number)] = true
		keep[checksKey(it.HeadSHA, "runs")] = true
		keep[checksKey(it.HeadSHA, "statuses")] = true
		bases[it.BaseRef] = true
	}
	for k := range c.Details {
		if !keep[k] {
			delete(c.Details, k)
		}
	}
	for b := range c.Tips {
		if !bases[b] {
			delete(c.Tips, b)
		}
	}
}

// fail maps a read failure to the classified error of the core. A rate limit holds the whole run.
func (o *HostObserver) fail(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var we *hosting.WatchError
	if !errors.As(err, &we) {
		return &watch.ObserveError{Code: hosting.CodeNetwork, Message: "read failed"}
	}
	oe := &watch.ObserveError{Code: we.Code, Message: we.Message}
	if we.Code == hosting.CodeRateLimited {
		oe.RunWide = true
		oe.RetryIn = hosting.RateLimitedUntil(we, o.now()).Sub(o.now())
	}
	return oe
}
