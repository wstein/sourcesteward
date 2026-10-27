// SPDX-License-Identifier: EUPL-1.2
package plural

import "testing"

func TestPickAndCountAgreeInNumber(t *testing.T) {
	for _, tc := range []struct {
		n           int
		pick, count string
	}{
		{0, "repositories", "0 repositories"},
		{1, "repository", "1 repository"},
		{2, "repositories", "2 repositories"},
		{-1, "repositories", "-1 repositories"},
	} {
		if got := Pick(tc.n, "repository", "repositories"); got != tc.pick {
			t.Errorf("Pick(%d) = %q want %q", tc.n, got, tc.pick)
		}
		if got := Count(tc.n, "repository", "repositories"); got != tc.count {
			t.Errorf("Count(%d) = %q want %q", tc.n, got, tc.count)
		}
	}
}
