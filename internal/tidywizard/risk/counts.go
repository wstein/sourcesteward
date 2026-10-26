// SPDX-License-Identifier: EUPL-1.2
package risk

// Counts includes every phase-1 display class, including retained red/kept rows.
type Counts struct{ Green, Yellow, Orange, Red, Kept int }

// Count counts the supplied assessments without observing or filtering rows.
func Count(in []Assessment) Counts {
	out := Counts{}
	for _, a := range in {
		switch a.Level {
		case Green:
			out.Green++
		case Yellow:
			out.Yellow++
		case Orange:
			out.Orange++
		case Kept:
			out.Kept++
		default:
			out.Red++
		}
	}
	return out
}

// Worst returns the worst risk, or Kept for an empty/all-kept set. Unknown
// levels cannot lower an assessment. Kept is not a deletion group level.
func Worst(in []Assessment) Level {
	out := Kept
	for _, a := range in {
		if rank(a.Level) > rank(out) {
			out = a.Level
			if out != Green && out != Yellow && out != Orange && out != Kept {
				out = Red
			}
		}
	}
	return out
}
func rank(level Level) int {
	switch level {
	case Kept:
		return -1
	case Green:
		return 0
	case Yellow:
		return 1
	case Orange:
		return 2
	default:
		return 3
	}
}
