// SPDX-License-Identifier: EUPL-1.2

package overview

// Build is the action model: the ordered items of every repository, the notices and the summary counts.
// It is a pure function of in, and its result does not depend on the order of the repositories or of
// the rows inside them.
func Build(in Input) Overview {
	items := sortItems(allItems(in))
	ns := notices(in)
	ov := Overview{Items: items, Notices: ns, Summary: Summary{Repos: len(in.Repos), Notices: len(ns)}}
	for _, it := range items {
		switch it.Priority {
		case Red:
			ov.Summary.Priorities.Red++
		case Yellow:
			ov.Summary.Priorities.Yellow++
		case Green:
			ov.Summary.Priorities.Green++
		case Blue:
			ov.Summary.Priorities.Blue++
		case Violet:
			ov.Summary.Priorities.Violet++
		}
	}
	for _, r := range in.Repos {
		if r.Local.Attention {
			ov.Summary.Attention++
		}
		if r.Local.State == "error" {
			ov.Summary.Errors++
		}
		if r.Remote == nil {
			continue
		}
		for _, st := range r.Remote.Sources {
			switch st.Outcome {
			case OutcomeError:
				ov.Summary.Failed, ov.Summary.Degraded = true, true
			case OutcomeSkipped:
				ov.Summary.Degraded = true
			}
		}
	}
	return ov
}
