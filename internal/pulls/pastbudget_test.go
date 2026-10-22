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

func TestDraftsAreNeverPastBudgetUnlessTheyWereEligibleForDetail(t *testing.T) {
	f := newForge()
	d := okPR("o/r", 1)
	d.draft = true
	f.add(d)
	f.add(okPR("o/r", 2))
	res := run(t, f, Options{Repos: []string{"o/r"}})
	if r := row(t, res, "o/r", 1); r.PastBudget || r.Detail || r.Group != GroupDraft {
		t.Fatalf("a draft skipped by default is not past the budget: %+v", r)
	}
	// With --detail-drafts a draft competes for the budget and can be past it; it is still
	// a draft, so it is not counted as not read.
	res = run(t, f, Options{Repos: []string{"o/r"}, DetailDrafts: true, Limit: 1})
	if r := row(t, res, "o/r", 1); r.PastBudget || !r.Detail {
		t.Fatalf("%+v", r)
	}
	if r := row(t, res, "o/r", 2); !r.PastBudget || r.Reason != ReasonNotRead {
		t.Fatalf("%+v", r)
	}
	f2 := newForge()
	d2 := okPR("o/r", 1)
	d2.draft = true
	d2.age = 48 * time.Hour
	f2.add(d2)
	f2.add(okPR("o/r", 2))
	res = run(t, f2, Options{Repos: []string{"o/r"}, DetailDrafts: true, Limit: 1})
	if r := row(t, res, "o/r", 1); !r.PastBudget || r.Group != GroupDraft || r.Detail {
		t.Fatalf("an eligible draft past the budget: %+v", r)
	}
	if res.Summary.NotRead != 0 {
		t.Fatalf("a draft is never counted as not read: %+v", res.Summary)
	}
}
