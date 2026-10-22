// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"context"
	"errors"
	"sort"
	"time"
)

// cycle is the computation of one poll: it mutates the state through State.apply for every event, so the
// events and their effects stay in step with the replay.
type cycle struct {
	w       *Watcher
	st      *State
	now     time.Time
	nowS    string
	events  []Event
	errs    map[string]error
	retryIn time.Duration
	stop    bool
}

func (c *cycle) emit(repo *string, pr *int, kind, key string, details any) {
	r := ""
	if repo != nil {
		r = *repo
	}
	ev := Event{SchemaVersion: 1, ID: eventID(r, pr, kind, key), Time: c.nowS, Repo: repo, PR: pr, Kind: kind, Details: details}
	c.st.apply(ev)
	c.events = append(c.events, ev)
}

func rp(s string) *string { return &s }
func pp(n int) *int       { return &n }

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (c *cycle) compute(ctx context.Context) error {
	st, o := c.st, c.w.o
	st.Cycle++
	st.IntervalSeconds = int(o.Interval / time.Second)
	c.syncRepos()
	if c.w.baseline {
		c.w.baseline = false
		for _, rs := range st.Repo {
			rs.Baselined, rs.RestackDue, rs.RestackDone = false, nil, nil
		}
	}
	if h := st.Health[""]; h != nil && h.waiting(c.now) {
		c.retryIn = h.until().Sub(c.now)
		return nil
	}
	for _, repo := range o.Repos {
		if c.stop {
			break
		}
		if err := c.poll(ctx, repo); err != nil {
			return err
		}
	}
	if !c.stop {
		if h := st.Health[""]; h != nil && h.ErrOpen {
			c.emit(nil, nil, KindWatchRecovered, keyRecoveredError(h.Episode), &WatchRecoveredDetails{AfterFailures: h.Failures})
		}
		if h := st.Health[""]; h != nil {
			h.Failures, h.LastCode, h.NextTry = 0, "", ""
		}
	}
	return nil
}

// syncRepos compares the resolved set with the state: silent on the first run, a repos-changed event
// afterwards. Added repositories get a silent baseline of their own (apply marks them).
func (c *cycle) syncRepos() {
	st, o := c.st, c.w.o
	if st.Repos == nil {
		st.Repos = append([]string{}, o.Repos...)
		st.Skipped = append([]Skipped{}, o.Skipped...)
		for _, r := range o.Repos {
			if st.Repo[r] == nil {
				st.Repo[r] = newRepoState(c.nowS)
			}
		}
		// A rebuilt record of a repository that is no longer configured stays, as after a normal
		// removal: its counters keep ids unique if the repository returns.
		return
	}
	cfg, have := map[string]bool{}, map[string]bool{}
	for _, r := range o.Repos {
		cfg[r] = true
	}
	for _, r := range st.Repos {
		have[r] = true
	}
	added, removed := []string{}, []string{}
	for r := range cfg {
		if !have[r] {
			added = append(added, r)
		}
	}
	for r := range have {
		if !cfg[r] {
			removed = append(removed, r)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	if len(added) == 0 && len(removed) == 0 && sameSkipped(st.Skipped, o.Skipped) {
		return
	}
	c.emit(nil, nil, KindReposChanged, keyRepos(st.ReposEpoch+1),
		&ReposChangedDetails{Added: added, Removed: removed, Skipped: append([]Skipped{}, o.Skipped...)})
}

func sameSkipped(a, b []Skipped) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (c *cycle) backoff(failures int) time.Duration {
	iv := c.w.o.Interval
	limit := 10 * iv
	if limit > 10*time.Minute {
		limit = 10 * time.Minute
	}
	d := iv
	for i := 1; i < failures && d < limit; i++ {
		d *= 2
	}
	if d > limit {
		d = limit
	}
	return d
}

func (c *cycle) poll(ctx context.Context, repo string) error {
	st, w := c.st, c.w
	rs := st.Repo[repo]
	if rs == nil {
		return nil
	}
	if h := st.Health[repo]; h != nil && h.waiting(c.now) {
		c.stalled(repo, rs)
		return nil
	}
	hints := Hints{Cycle: st.Cycle, DetailEvery: w.o.DetailEvery, Cond: rs.Cond}
	for _, p := range rs.sortedPRs() {
		if !p.Gone {
			hints.Known = append(hints.Known, KnownPR{Number: p.Number, Head: p.Head, Class: p.Class, ClassCand: p.ClassCand, DetailDue: p.DetailDue})
		}
	}
	obs, err := w.o.Observer.Observe(ctx, repo, hints)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.fail(repo, rs, err)
		return nil
	}
	c.succeed(repo, rs)
	if obs.DefaultBranch != "" {
		rs.DefaultBranch = obs.DefaultBranch
	}
	if obs.Cond != nil {
		rs.Cond = obs.Cond
	}
	if !rs.Baselined {
		c.baseline(rs, obs)
		return nil
	}
	c.diff(repo, rs, obs)
	c.restack(ctx, repo, rs)
	return nil
}

func (c *cycle) ignored(o PRObs) bool {
	for _, a := range c.w.o.IgnoreAuthors {
		if o.Author == a {
			return true
		}
	}
	for _, l := range c.w.o.IgnoreLabels {
		for _, x := range o.Labels {
			if x == l {
				return true
			}
		}
	}
	return false
}

func sortedObs(prs []PRObs) []PRObs {
	out := append([]PRObs(nil), prs...)
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// baseline replaces the accepted view with the observation, silently. Counters of PRs seen before are
// kept (ids stay unique); PRs that vanished are remembered so that a reappearance gets the next generation.
func (c *cycle) baseline(rs *RepoState, obs RepoObservation) {
	fresh := map[int]*PRState{}
	for _, o := range sortedObs(obs.PRs) {
		if c.ignored(o) {
			continue
		}
		p := &PRState{Number: o.Number, Title: o.Title, Author: o.Author, Labels: o.Labels, Draft: o.Draft, Head: o.Head, HeadRef: o.HeadRef,
			Base: o.Base, Fork: o.Fork, DetailDue: o.Detail == nil}
		var fired *PRState // a timeout that already fired for this head stays fired (same id otherwise)
		if old := rs.PRs[o.Number]; old != nil {
			p.Epochs, p.Generation = old.Epochs, old.Generation
			if old.Head == o.Head && old.TimeoutFired {
				fired = old
			}
			if !o.ForkKnown {
				p.Fork = old.Fork
			}
		} else if rec, _, ok := rs.closedRecord(o.Number); ok {
			p.Epochs, p.Generation = rec.Epochs, rec.Generation+1
		}
		if o.Detail != nil && o.Detail.Mergeable != nil {
			v := *o.Detail.Mergeable
			p.Mergeable, p.MState, p.Conflicting, p.Behind = &v, o.Detail.State, !v, o.Detail.State == "behind"
		}
		if o.Checks != nil {
			res := ClassifyChecksWith(o.Checks.Runs, o.Checks.Statuses, o.Base, rs.DefaultBranch, c.w.o.StackPatterns)
			p.Class = res.Class
			if res.Class == ClassPending {
				p.PendingSince, p.PendingNames = c.nowS, res.Pending
				if fired != nil {
					p.TimeoutFired, p.PendingSince = true, fired.PendingSince
				}
			}
		}
		fresh[o.Number] = p
	}
	for n, old := range rs.PRs {
		if fresh[n] == nil {
			rs.recordClosed(ClosedPR{Number: n, Generation: old.Generation, Epochs: old.Epochs})
		}
	}
	rs.PRs, rs.Baselined = fresh, true
}

func (c *cycle) diff(repo string, rs *RepoState, obs RepoObservation) {
	present := map[int]bool{}
	for _, o := range sortedObs(obs.PRs) {
		present[o.Number] = true
		if c.ignored(o) {
			if old := rs.PRs[o.Number]; old != nil { // became ignored: vanish silently
				rs.recordClosed(ClosedPR{Number: o.Number, Generation: old.Generation, Epochs: old.Epochs})
				delete(rs.PRs, o.Number)
			}
			continue
		}
		n := o.Number
		p := rs.PRs[n]
		if p == nil {
			gen := 0
			if rec, _, ok := rs.closedRecord(n); ok {
				gen = rec.Generation + 1
			}
			c.emit(rp(repo), pp(n), KindNewPR, keyGeneration(gen),
				&NewPRDetails{Title: o.Title, Author: o.Author, Draft: o.Draft, Base: o.Base, HeadRef: o.HeadRef, Head: o.Head, Fork: o.Fork})
			p = rs.PRs[n]
		} else {
			if o.Head != p.Head {
				e := p.Epochs.Head + 1
				c.emit(rp(repo), pp(n), KindHeadChanged, keyHead(o.Head, e), &HeadChangedDetails{Old: p.Head, New: o.Head})
			}
			if o.Draft != p.Draft {
				d := &ReadyDetails{From: "ready", To: "draft"}
				if p.Draft {
					d = &ReadyDetails{From: "draft", To: "ready"}
				}
				c.emit(rp(repo), pp(n), KindReadyStateChanged, keyReady(p.Epochs.Ready+1), d)
			}
			if o.Base != p.Base {
				c.emit(rp(repo), pp(n), KindBaseChanged, keyBase(p.Epochs.Base+1), &BaseChangedDetails{Old: p.Base, New: o.Base})
			}
			p.Gone = false
		}
		p.Title, p.Author, p.Labels, p.HeadRef = o.Title, o.Author, o.Labels, o.HeadRef
		if o.ForkKnown {
			p.Fork = o.Fork
		}
		c.volatile(repo, rs, p, o)
	}
	for _, p := range rs.sortedPRs() {
		if present[p.Number] {
			continue
		}
		g, ok := obs.Gone[p.Number]
		if !ok {
			p.Gone = true // gone, unresolved: retried every cycle, never guessed
			continue
		}
		c.resolve(repo, rs, p, g)
	}
}

func orElse(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (c *cycle) resolve(repo string, rs *RepoState, p *PRState, g GoneObs) {
	head, base := orElse(g.Head, p.Head), orElse(g.Base, p.Base)
	if !g.Merged {
		c.emit(rp(repo), pp(p.Number), KindClosed, keyGeneration(p.Generation), &ClosedDetails{Head: head, Base: base})
		return
	}
	commit := g.MergeCommit
	if commit != nil && *commit == "" {
		commit = nil
	}
	hint := g.MethodHint
	switch hint {
	case "merge", "rebase-or-squash":
	default:
		hint = "unknown"
	}
	c.emit(rp(repo), pp(p.Number), KindMerged, keyMerged(commit), &MergedDetails{MergeCommit: commit, Base: base, Head: head,
		HeadRef: orElse(g.HeadRef, p.HeadRef), MergedAt: orElse(g.MergedAt, c.nowS), MethodHint: hint})
}

func (c *cycle) volatile(repo string, rs *RepoState, p *PRState, o PRObs) {
	if o.Detail != nil {
		p.DetailDue = false
		c.mergeable(repo, p, o.Detail)
		if p.MergeCand != nil && o.Detail.Mergeable == nil {
			p.DetailDue = true // still being computed: keep the candidate's confirming read due
		}
	}
	if o.Checks != nil {
		c.checks(repo, rs, p, o.Checks)
	}
	c.timeout(repo, p)
}

func (c *cycle) mergeable(repo string, p *PRState, d *DetailObs) {
	if d.Mergeable == nil {
		return // not computed: keep the previous value
	}
	m, behind := *d.Mergeable, d.State == "behind"
	if p.Mergeable == nil { // first knowledge is adopted, not announced
		p.Mergeable, p.MState, p.Conflicting, p.Behind, p.MergeCand = &m, d.State, !m, behind, nil
		return
	}
	if *p.Mergeable == m && p.Behind == behind {
		p.MState, p.Conflicting, p.MergeCand = d.State, !m, nil
		return
	}
	cand := MergeCand{Mergeable: m, Behind: behind, State: d.State}
	if p.MergeCand == nil || p.MergeCand.Mergeable != m || p.MergeCand.Behind != behind {
		p.MergeCand, p.DetailDue = &cand, true // the second sighting needs a fresh read next cycle
		return
	}
	mm := m
	c.emit(rp(repo), pp(p.Number), KindMergeableChanged, keyMergeable(p.Head, p.Epochs.Mergeable+1),
		&MergeableDetails{Mergeable: &mm, State: d.State, Conflicting: !m, Behind: behind, Base: p.Base})
}

func (c *cycle) checks(repo string, rs *RepoState, p *PRState, ch *ChecksObs) {
	res := ClassifyChecksWith(ch.Runs, ch.Statuses, p.Base, rs.DefaultBranch, c.w.o.StackPatterns)
	if res.Class == p.Class {
		p.ClassCand = ""
		if res.Class == ClassPending {
			p.PendingNames = res.Pending
		}
		return
	}
	if p.ClassCand != res.Class {
		p.ClassCand = res.Class
		return
	}
	e := p.Epochs.Checks + 1
	switch res.Class {
	case ClassGreen:
		c.emit(rp(repo), pp(p.Number), KindChecksGreen, keyChecks(p.Head, e), &ChecksGreenDetails{Head: p.Head, Draft: p.Draft, Base: p.Base, Total: res.Total})
	case ClassRed:
		c.emit(rp(repo), pp(p.Number), KindChecksRed, keyChecks(p.Head, e), &ChecksRedDetails{Head: p.Head, Failing: nonNil(res.Failing),
			FailingTotal: res.FailingTotal, Shape: res.Shape, Base: p.Base, Draft: p.Draft})
	default:
		p.acceptClass(res.Class, e, c.nowS)
		if res.Class == ClassPending {
			p.PendingNames = res.Pending
		}
	}
}

// timeout is level based: it fires once per pending period (the fired flag is set by the event and its replay).
func (c *cycle) timeout(repo string, p *PRState) {
	limit := c.w.o.PendingTimeout
	if limit <= 0 || p.Class != ClassPending || p.TimeoutFired || p.PendingSince == "" {
		return
	}
	since, err := time.Parse(time.RFC3339, p.PendingSince)
	if err != nil || c.now.Sub(since) <= limit {
		return
	}
	c.emit(rp(repo), pp(p.Number), KindChecksPendingTimout, keyTimeout(p.Head, p.Epochs.Checks),
		&PendingTimeoutDetails{Head: p.Head, Pending: nonNil(append([]string(nil), p.PendingNames...)), Since: p.PendingSince})
}

// restack asks the WP-D hook for the children of every merge still due and emits the pairs not yet
// journalled. The restack_due entries are cleared at the end of the cycle that finished them.
func (c *cycle) restack(ctx context.Context, repo string, rs *RepoState) {
	if len(rs.RestackDue) == 0 {
		return
	}
	if c.w.o.Restacker == nil {
		rs.RestackDue, rs.RestackDone = nil, nil
		return
	}
	var open []OpenPR
	for _, p := range rs.sortedPRs() {
		if !p.Gone {
			open = append(open, OpenPR{Number: p.Number, Base: p.Base, HeadRef: p.HeadRef, Head: p.Head, Fork: p.Fork})
		}
	}
	done := map[string]bool{}
	for _, id := range rs.RestackDone {
		done[id] = true
	}
	failed := false
	for _, due := range append([]RestackDue(nil), rs.RestackDue...) {
		cands, err := c.w.o.Restacker.Candidates(ctx, repo, due, open)
		if err != nil {
			failed = true
			c.errs[repo] = err
			continue
		}
		sort.Slice(cands, func(i, j int) bool { return cands[i].PR < cands[j].PR })
		for _, cand := range cands {
			id := eventID(repo, pp(cand.PR), KindRestackNeeded, keyRestack(due.MergedPR, due.MergeCommit))
			if done[id] {
				continue
			}
			argv := append([]string(nil), cand.Argv...)
			if cand.Argv == nil || due.MergeCommit == nil {
				argv = nil
			}
			c.emit(rp(repo), pp(cand.PR), KindRestackNeeded, keyRestack(due.MergedPR, due.MergeCommit), &RestackDetails{MergedPR: due.MergedPR,
				Base: due.Base, Reason: cand.Reason, OldParent: due.OldParent, NewParent: due.MergeCommit, NewParentSource: "merge_commit_sha",
				Branch: cand.Branch, Argv: argv})
			done[id] = true
		}
	}
	if !failed {
		rs.RestackDue, rs.RestackDone = nil, nil
	}
}

// fail records one failed read of a repository (or of the run).
func (c *cycle) fail(repo string, rs *RepoState, err error) {
	code, msg, runWide, retry := "network", err.Error(), false, time.Duration(0)
	var oe *ObserveError
	if errors.As(err, &oe) {
		code, msg, runWide, retry = oe.Code, oe.Message, oe.RunWide, oe.RetryIn
	}
	key, rptr := repo, rp(repo)
	if runWide {
		key, rptr = "", nil
	}
	h := c.st.health(key)
	h.Failures++
	if h.Failures == 1 {
		h.Episode++
	}
	if retry <= 0 {
		retry = c.backoff(h.Failures)
	}
	h.NextTry = c.now.Add(retry).Format(time.RFC3339)
	if h.Failures == 1 || code != h.LastCode || h.Failures%10 == 0 {
		c.emit(rptr, nil, KindWatchError, keyError(code, h.Episode, h.Failures), &WatchErrorDetails{Code: code, Message: sanitize(msg, 512),
			Failures: h.Failures, RetryInSeconds: int(retry / time.Second), Repo: rptr})
	}
	if runWide {
		c.retryIn, c.stop = retry, true
		return
	}
	c.stalled(repo, rs)
}

// stalled emits watch-stalled once per stall when no read succeeded for max(5 x interval, 10 min).
func (c *cycle) stalled(repo string, rs *RepoState) {
	h := c.st.health(repo)
	if h.Stalled {
		return
	}
	threshold := 5 * c.w.o.Interval
	if threshold < 10*time.Minute {
		threshold = 10 * time.Minute
	}
	since := rs.LastOK
	if since == "" {
		since = rs.Since
	}
	t, err := time.Parse(time.RFC3339, since)
	if err != nil || c.now.Sub(t) < threshold {
		return
	}
	c.emit(rp(repo), nil, KindWatchStalled, keyStall(h.StallEpisode+1), &WatchStalledDetails{Since: since, ThresholdSeconds: int(threshold / time.Second), Repo: repo})
}

// succeed closes the open failure and stall episodes of a repository with watch-recovered events.
func (c *cycle) succeed(repo string, rs *RepoState) {
	h := c.st.health(repo)
	after := h.Failures
	if h.ErrOpen {
		c.emit(rp(repo), nil, KindWatchRecovered, keyRecoveredError(h.Episode), &WatchRecoveredDetails{AfterFailures: after, Repo: rp(repo)})
	}
	if h.Stalled {
		c.emit(rp(repo), nil, KindWatchRecovered, keyStall(h.StallEpisode), &WatchRecoveredDetails{AfterFailures: after, Repo: rp(repo)})
	}
	h.Failures, h.LastCode, h.NextTry = 0, "", ""
	rs.LastOK = c.nowS
}

func (h *Health) until() time.Time {
	t, _ := time.Parse(time.RFC3339, h.NextTry)
	return t
}

// waiting reports whether the backoff of this repository (or the run-wide hold) has not elapsed.
func (h *Health) waiting(now time.Time) bool {
	return h.NextTry != "" && now.Before(h.until())
}
