// SPDX-License-Identifier: EUPL-1.2
package pulls

import (
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
)

func threePRs(f *fakeForge) {
	for i := 1; i <= 3; i++ {
		p := okPR("o/r", i)
		p.age = time.Duration(i) * 24 * time.Hour // 1 is newest and read first
		f.add(p)
	}
}

func TestPastBudgetIsExported(t *testing.T) {
	f := newForge()
	threePRs(f)
	res := run(t, f, Options{Repos: []string{"o/r"}, Limit: 1})
	if r := row(t, res, "o/r", 1); r.PastBudget || !r.Detail {
		t.Fatalf("%+v", r)
	}
	for _, n := range []int{2, 3} {
		if r := row(t, res, "o/r", n); !r.PastBudget || r.Reason != ReasonNotRead {
			t.Fatalf("#%d %+v", n, r)
		}
	}
}

func TestFailedAndRateSkippedReadsAreRepositoryErrorsNotPastBudget(t *testing.T) {
	f := newForge()
	threePRs(f)
	f.errAt["detail/o/r#1"] = &hosting.WatchError{Code: hosting.CodeRateLimited, Message: "limit"}
	res := run(t, f, Options{Repos: []string{"o/r"}, Parallel: 1})
	if res.Repos[0].Error == "" || len(res.Errors) == 0 {
		t.Fatalf("the repository carries the error: %+v", res.Repos)
	}
	for _, r := range res.Rows {
		if r.PastBudget || r.Reason != ReasonNotRead {
			t.Fatalf("a failed read is a repository error, not past budget or deadline: %+v", r)
		}
	}
}
