// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"sort"
	"strconv"
)

func itoa(n int) string { return strconv.Itoa(n) }

func cloneString(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneInt(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneStrings(in []string) []string {
	return append([]string{}, in...)
}

func cloneMode(m ModeSummary) ModeSummary {
	m.Markers = cloneStrings(m.Markers)
	return m
}

func cloneBlockers(in []Blocker) []Blocker {
	out := make([]Blocker, 0, len(in))
	for _, b := range in {
		out = append(out, Blocker{ActionID: cloneString(b.ActionID), Reason: b.Reason})
	}
	return out
}

func cloneFailure(f *Failure) *Failure {
	if f == nil {
		return nil
	}
	c := *f
	return &c
}

func cloneAction(a Action) Action {
	c := a
	c.Target = Target{Ref: cloneString(a.Target.Ref), PullRequest: cloneInt(a.Target.PullRequest), TrunkBase: cloneString(a.Target.TrunkBase)}
	if a.Target.PullRequests != nil {
		c.Target.PullRequests = append([]int{}, a.Target.PullRequests...)
	}
	c.Expected = Expected{OldSHA: cloneString(a.Expected.OldSHA), NewSHA: cloneString(a.Expected.NewSHA), HeadSHA: cloneString(a.Expected.HeadSHA),
		BaseRef: cloneString(a.Expected.BaseRef), PlannedBase: cloneString(a.Expected.PlannedBase), BodyDigest: cloneString(a.Expected.BodyDigest)}
	c.Effects = cloneStrings(a.Effects)
	c.DependsOn = cloneStrings(a.DependsOn)
	c.Guards = append([]Guard{}, a.Guards...)
	if a.Blocker != nil {
		r := *a.Blocker
		c.Blocker = &r
	}
	return c
}

func sortedStrings(in []string) []string {
	out := cloneStrings(in)
	sort.Strings(out)
	return out
}

// canonicalMode copies the mode with its marker names sorted and unique, so one fact has one id.
func canonicalMode(m ModeSummary) ModeSummary {
	seen := map[string]bool{}
	names := []string{}
	for _, n := range m.Markers {
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	sort.Strings(names)
	m.Markers = names
	return m
}
