// SPDX-License-Identifier: EUPL-1.2
package watchhost

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watch"
)

type fakePullCommits struct {
	body string
	err  error
	got  []string
}

func (f *fakePullCommits) PullCommits(ctx context.Context, repo string, number, page int, etag string) (hosting.WatchResponse, error) {
	f.got = append(f.got, fmt.Sprintf("%s#%d p%d etag=%q", repo, number, page, etag))
	return hosting.WatchResponse{Status: 200, Body: []byte(f.body)}, f.err
}

func TestHostingCommitsDecodes(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	f := &fakePullCommits{body: `[{"sha":"` + a + `","commit":{"message":"x"}},{"sha":"` + b + `"}]`}
	shas, more, err := NewHostingCommits(f).PullCommits(context.Background(), "o/r", 12, 2)
	if err != nil || more || len(shas) != 2 || shas[0] != a || shas[1] != b {
		t.Fatalf("%v %v %v", shas, more, err)
	}
	if f.got[0] != `o/r#12 p2 etag=""` {
		t.Fatalf("%v", f.got)
	}
}

func TestHostingCommitsFullPageHasMore(t *testing.T) {
	var parts []string
	for i := 0; i < 100; i++ {
		parts = append(parts, `{"sha":"`+strings.Repeat("ab", 20)+`"}`)
	}
	_, more, err := NewHostingCommits(&fakePullCommits{body: "[" + strings.Join(parts, ",") + "]"}).PullCommits(context.Background(), "o/r", 1, 1)
	if err != nil || !more {
		t.Fatalf("%v %v", more, err)
	}
}

func TestHostingCommitsMalformed(t *testing.T) {
	for name, body := range map[string]string{"not json": `{`, "not array": `{}`, "bad sha": `[{"sha":"XYZ"}]`} {
		_, _, err := NewHostingCommits(&fakePullCommits{body: body}).PullCommits(context.Background(), "o/r", 1, 1)
		if !errors.Is(err, watch.ErrMalformed) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	we := &hosting.WatchError{Code: hosting.CodeMalformed, Message: "x"}
	_, _, err := NewHostingCommits(&fakePullCommits{err: we}).PullCommits(context.Background(), "o/r", 1, 1)
	if !errors.Is(err, watch.ErrMalformed) {
		t.Fatalf("watch malformed: %v", err)
	}
	other := &hosting.WatchError{Code: hosting.CodeNetwork, Message: "x"}
	_, _, err = NewHostingCommits(&fakePullCommits{err: other}).PullCommits(context.Background(), "o/r", 1, 1)
	if errors.Is(err, watch.ErrMalformed) || !errors.Is(err, other) {
		t.Fatalf("other: %v", err)
	}
}

func TestNewRestackerUsesHostingNameCheck(t *testing.T) {
	old := strings.Repeat("a", 40)
	f := &fakePullCommits{body: `[{"sha":"` + old + `"}]`}
	var diag []string
	rs := NewRestacker(f, func(s string) { diag = append(diag, s) })
	due := watch.RestackDue{MergedPR: 10, OldParent: old, Base: "main", HeadRef: "p"}
	open := []watch.OpenPR{{Number: 11, Base: "p", HeadRef: "bad%20name"}, {Number: 12, Base: "main", HeadRef: "feat/ok"}}
	got, err := rs.Candidates(context.Background(), "o/r", due, open)
	if err != nil || len(got) != 1 || got[0].PR != 12 || len(diag) != 1 || len(f.got) != 1 {
		t.Fatalf("%v %+v %v %v", err, got, diag, f.got)
	}
}
