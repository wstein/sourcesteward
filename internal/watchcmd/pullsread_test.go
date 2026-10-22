// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
)

func TestDecodePullDetailReadsHeadSHAAndRebaseable(t *testing.T) {
	sha := sha40("c")
	d, err := DecodePullDetail([]byte(`{"mergeable":true,"mergeable_state":"clean","rebaseable":false,"head":{"sha":"` + sha + `","repo":{"full_name":"o/r"}},"base":{"repo":{"full_name":"O/R"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if d.Mergeable == nil || !*d.Mergeable || d.MergeableState != "clean" || d.Rebaseable == nil || *d.Rebaseable || d.HeadSHA != sha || d.Fork {
		t.Fatalf("%+v", d)
	}
}

func TestDecodePullDetailNullsAndFork(t *testing.T) {
	d, err := DecodePullDetail([]byte(`{"mergeable":null,"mergeable_state":"unknown","rebaseable":null,"head":{"repo":null},"base":{"repo":{"full_name":"o/r"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if d.Mergeable != nil || d.Rebaseable != nil || d.HeadSHA != "" || !d.Fork || d.MergeableState != "unknown" {
		t.Fatalf("%+v", d)
	}
	var we *hosting.WatchError
	if _, err := DecodePullDetail([]byte(`[1]`)); !errors.As(err, &we) || we.Code != hosting.CodeMalformed {
		t.Fatalf("err %v", err)
	}
}

func TestReadPullDetailIsUnconditional(t *testing.T) {
	f := newFake()
	f.details[4] = detailJSON
	f.detailET[4] = `"d4"`
	d, err := ReadPullDetail(context.Background(), f, "o/r", 4)
	if err != nil {
		t.Fatal(err)
	}
	if d.MergeableState != "clean" || f.etags["detail/4"] != "" || len(f.calls) != 1 {
		t.Fatalf("%+v calls=%v etags=%v", d, f.calls, f.etags)
	}
	f.errAt["detail/5"] = errors.New("boom")
	if _, err := ReadPullDetail(context.Background(), f, "o/r", 5); err == nil {
		t.Fatal("read error swallowed")
	}
}

func TestReadChecksIsUnconditionalAndClassifies(t *testing.T) {
	f := newFake()
	sha := sha40("d")
	f.runs[sha] = `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{}}]}`
	obs, class, err := ReadChecks(context.Background(), f, "o/r", sha)
	if err != nil {
		t.Fatal(err)
	}
	if class != "green" || len(obs.Runs) != 1 || f.etags["runs/"+sha[:4]] != "" || f.etags["stats/"+sha[:4]] != "" {
		t.Fatalf("class=%q obs=%+v etags=%v", class, obs, f.etags)
	}
	empty, class, err := ReadChecks(context.Background(), f, "o/r", sha40("e"))
	if err != nil || class != "none" || len(empty.Runs)+len(empty.Statuses) != 0 {
		t.Fatalf("class=%q %+v err=%v", class, empty, err)
	}
}

func TestEscIsExportedAndBounded(t *testing.T) {
	if got := Esc("a\nb\x1b[0m\u202e"); got != `a\nb\x1b[0m`+`\u202e` {
		t.Fatalf("%q", got)
	}
	if got := Esc(strings.Repeat("x", maxHumanText+5)); got != strings.Repeat("x", maxHumanText)+"..." {
		t.Fatalf("len %d", len(got))
	}
}
