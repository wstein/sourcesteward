// SPDX-License-Identifier: EUPL-1.2
package watch

import "sort"

// apply is the one place where an event changes the state. A live cycle calls it for every event it
// emits and the start-up replay calls it for every journal line past applied_seq, so both reach the same
// state by construction (the "Replay effects" table of Spec 035). Counters are set to the value encoded in
// the id and never lowered, and applying an event twice changes nothing.
func (s *State) apply(ev Event) {
	switch ev.Kind {
	case KindReposChanged:
		s.applyRepos(ev)
		return
	case KindWatchError, KindWatchRecovered, KindWatchStalled:
		s.applyHealth(ev)
		return
	}
	if ev.Repo == nil || ev.PR == nil {
		return
	}
	rs := s.Repo[*ev.Repo]
	if rs == nil { // only a rebuild of lost state gets here: the counters still matter
		rs = newRepoState(ev.Time)
		s.Repo[*ev.Repo] = rs
	}
	n := *ev.PR
	p := rs.PRs[n]
	switch ev.Kind {
	case KindNewPR:
		d := ev.Details.(*NewPRDetails)
		gen, _ := idCounter(ev.ID, 'g')
		if p == nil {
			p = &PRState{Number: n, Class: ClassNone}
			rs.PRs[n] = p
			if rec, _, ok := rs.closedRecord(n); ok {
				p.Epochs = p.Epochs.max(rec.Epochs)
			}
		}
		p.Title, p.Author, p.Draft, p.Base, p.HeadRef, p.Head = d.Title, d.Author, d.Draft, d.Base, d.HeadRef, d.Head
		p.Generation, p.DetailDue, p.Gone = gen, true, false
	case KindMerged:
		d := ev.Details.(*MergedDetails)
		delete(rs.PRs, n)
		due := RestackDue{MergedPR: n, MergeCommit: d.MergeCommit, OldParent: d.Head, Base: d.Base, HeadRef: d.HeadRef}
		dup := false
		for _, x := range rs.RestackDue {
			if x.MergedPR == n && keyMerged(x.MergeCommit) == keyMerged(d.MergeCommit) {
				dup = true
			}
		}
		if !dup {
			rs.RestackDue = append(rs.RestackDue, due)
		}
		for _, o := range rs.PRs {
			o.DetailDue = true
		}
	case KindClosed:
		gen, _ := idCounter(ev.ID, 'g')
		rec := ClosedPR{Number: n, Generation: gen}
		if p != nil {
			rec.Epochs = p.Epochs
		}
		if old, _, ok := rs.closedRecord(n); ok {
			rec.Epochs = rec.Epochs.max(old.Epochs)
		}
		rs.recordClosed(rec)
		delete(rs.PRs, n)
	case KindRestackNeeded:
		for _, id := range rs.RestackDone {
			if id == ev.ID {
				return
			}
		}
		rs.RestackDone = append(rs.RestackDone, ev.ID)
	default:
		if p == nil { // a lost state: keep the counters of a PR whose new-pr was never journalled
			p = &PRState{Number: n}
			rs.PRs[n] = p
		}
		applyPR(p, ev)
	}
}

func raise(dst *int, v int) {
	if v > *dst {
		*dst = v
	}
}

func (p *PRState) resetChecks() {
	p.Class, p.ClassCand, p.PendingSince, p.PendingNames, p.TimeoutFired = "", "", "", nil, false
	p.MergeCand = nil
}

// acceptClass is the accepted class change: the epoch grows on every one, also to pending or none.
func (p *PRState) acceptClass(class string, epoch int, now string) {
	p.Class, p.ClassCand, p.TimeoutFired, p.PendingSince = class, "", false, ""
	if class == ClassPending {
		p.PendingSince = now
	} else {
		p.PendingNames = nil
	}
	raise(&p.Epochs.Checks, epoch)
}

func applyPR(p *PRState, ev Event) {
	switch ev.Kind {
	case KindHeadChanged:
		d := ev.Details.(*HeadChangedDetails)
		e, _ := idCounter(ev.ID, 'h')
		p.Head = d.New
		raise(&p.Epochs.Head, e)
		p.resetChecks()
		p.DetailDue = true
	case KindChecksGreen, KindChecksRed:
		head := ""
		class := ClassGreen
		if d, ok := ev.Details.(*ChecksGreenDetails); ok {
			head = d.Head
		}
		if d, ok := ev.Details.(*ChecksRedDetails); ok {
			head, class = d.Head, ClassRed
		}
		if head != p.Head {
			return
		}
		e, _ := idCounter(ev.ID, 'c')
		p.acceptClass(class, e, "")
	case KindChecksPendingTimout:
		d := ev.Details.(*PendingTimeoutDetails)
		if d.Head != p.Head {
			return
		}
		e, _ := idCounter(ev.ID, 'c')
		raise(&p.Epochs.Checks, e)
		p.Class, p.ClassCand, p.PendingSince, p.TimeoutFired = ClassPending, "", d.Since, true
		p.PendingNames = append([]string(nil), d.Pending...)
	case KindMergeableChanged:
		d := ev.Details.(*MergeableDetails)
		e, _ := idCounter(ev.ID, 'm')
		raise(&p.Epochs.Mergeable, e)
		if d.Mergeable != nil {
			v := *d.Mergeable
			p.Mergeable = &v
		}
		p.MState, p.Conflicting, p.Behind, p.MergeCand = d.State, d.Conflicting, d.Behind, nil
	case KindReadyStateChanged:
		d := ev.Details.(*ReadyDetails)
		e, _ := idCounter(ev.ID, 'r')
		raise(&p.Epochs.Ready, e)
		p.Draft = d.To == "draft"
	case KindBaseChanged:
		d := ev.Details.(*BaseChangedDetails)
		e, _ := idCounter(ev.ID, 'b')
		raise(&p.Epochs.Base, e)
		p.Base = d.New
		p.DetailDue = true
	}
}

func (s *State) applyRepos(ev Event) {
	d := ev.Details.(*ReposChangedDetails)
	e, _ := idCounter(ev.ID, 'p')
	raise(&s.ReposEpoch, e)
	removed := map[string]bool{}
	for _, r := range d.Removed {
		removed[r] = true
		delete(s.Repo, r)
		delete(s.Health, r)
	}
	var repos []string
	have := map[string]bool{}
	for _, r := range s.Repos {
		if !removed[r] && !have[r] {
			repos = append(repos, r)
			have[r] = true
		}
	}
	for _, r := range d.Added {
		if !have[r] {
			repos = append(repos, r)
			have[r] = true
		}
		rs := s.Repo[r]
		if rs == nil {
			rs = newRepoState(ev.Time)
			s.Repo[r] = rs
		}
		rs.Baselined = false // needs a silent baseline of its own
	}
	s.Repos = repos
	s.Skipped = append([]Skipped{}, d.Skipped...)
}

func (s *State) applyHealth(ev Event) {
	key := ""
	if ev.Repo != nil {
		key = *ev.Repo
	}
	h := s.health(key)
	switch ev.Kind {
	case KindWatchError:
		d := ev.Details.(*WatchErrorDetails)
		e, _ := idCounter(ev.ID, 'e')
		raise(&h.Episode, e)
		raise(&h.Failures, d.Failures)
		h.LastCode, h.ErrOpen = d.Code, true
	case KindWatchRecovered:
		if e, ok := idCounter(ev.ID, 'e'); ok { // after a watch-error episode
			raise(&h.Episode, e)
			h.ErrOpen, h.Failures, h.LastCode = false, 0, ""
		} else if e, ok := idCounter(ev.ID, 's'); ok { // after a watch-stalled episode
			raise(&h.StallEpisode, e)
			h.Stalled = false
		}
	case KindWatchStalled:
		e, _ := idCounter(ev.ID, 's')
		raise(&h.StallEpisode, e)
		h.Stalled = true
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
