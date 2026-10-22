// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"context"
	"errors"
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

func TestReadPullDetailUnexpected304IsAnError(t *testing.T) {
	r := notModifiedReader{newFake()}
	_, err := ReadPullDetail(context.Background(), r, "o/r", 4)
	var we *hosting.WatchError
	if !errors.As(err, &we) || we.Code != hosting.CodeMalformed {
		t.Fatalf("err %v", err)
	}
}

// notModifiedReader answers every pulls/N read with 304 even though no validator was sent.
type notModifiedReader struct{ *fakeReader }

func (notModifiedReader) PullDetail(context.Context, string, int, string) (hosting.WatchResponse, error) {
	// The body would decode, so only the 304 check can make the read fail.
	return hosting.WatchResponse{Status: 304, NotModified: true, Body: []byte(detailJSON)}, nil
}

func TestReadChecksStatusesOnly(t *testing.T) {
	f := newFake()
	red, pending := sha40("1"), sha40("2")
	f.stats[red] = `[{"context":"legacy","state":"failure","description":"x"}]`
	f.stats[pending] = `[{"context":"legacy","state":"pending","description":"x"}]`
	for sha, want := range map[string]string{red: "red", pending: "pending"} {
		obs, class, err := ReadChecks(context.Background(), f, "o/r", sha)
		if err != nil || class != want || len(obs.Runs) != 0 || len(obs.Statuses) != 1 {
			t.Fatalf("%s: class=%q obs=%+v err=%v", want, class, obs, err)
		}
	}
}
