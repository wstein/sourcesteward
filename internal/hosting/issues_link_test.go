// SPDX-License-Identifier: EUPL-1.2
package hosting

import "testing"

func TestIssuesLinkNextPage(t *testing.T) {
	for _, tc := range []struct {
		name, link string
		next       int
		more       bool
		bad        bool
	}{
		{"none", ``, 0, false, false},
		{"last page", `<https://api.github.com/x?page=1>; rel="first", <https://api.github.com/x?page=4>; rel="prev"`, 0, false, false},
		{"next", `<https://api.github.com/repositories/1/issues?page=2>; rel="next", <https://api.github.com/x?page=9>; rel="last"`, 2, true, false},
		{"next of another host is only a number", `<https://evil.example/x?page=7>; rel="next"`, 7, true, false},
		{"next without a number", `<https://api.github.com/x?page=abc>; rel="next"`, 0, false, true},
		{"next with page 0", `<https://api.github.com/x?page=0>; rel="next"`, 0, false, true},
		{"next not parseable", `garbage; rel="next"`, 0, false, false},
	} {
		next, more, err := LinkNextPage(tc.link)
		if next != tc.next || more != tc.more || (err != nil) != tc.bad {
			t.Errorf("%s: %d %v %v", tc.name, next, more, err)
		}
		if tc.bad {
			assertWatchCode(t, err, CodeMalformed)
		}
	}
}
