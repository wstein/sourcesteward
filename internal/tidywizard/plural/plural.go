// SPDX-License-Identifier: EUPL-1.2

// Package plural picks the singular or the plural of a noun for the counts of the wizard text.
package plural

import "fmt"

// Pick returns one for a count of exactly 1 and many otherwise (0 and every other number).
func Pick(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Count returns the count and the noun in agreement, for example "1 repository" or "0 repositories".
func Count(n int, one, many string) string { return fmt.Sprintf("%d %s", n, Pick(n, one, many)) }
