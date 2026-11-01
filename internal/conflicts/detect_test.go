// SPDX-License-Identifier: EUPL-1.2
package conflicts

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func byNumber(fs []Finding) map[int]Finding {
	m := map[int]Finding{}
	for _, f := range fs {
		m[f.Number] = f
	}
	return m
}

func TestDetectRealRepository(t *testing.T) {
	f := newRepoFixture(t)
	r := NewRunner(f.dir)
	defer r.Close()
	before := snapshot(t, f.dir)
	got := byNumber(Detect(context.Background(), r, "origin", f.prs()))

	c := got[1]
	if c.State != StateConflicts || c.Base != "main" || c.BaseSHA != f.mainTip || c.MergeBase != f.base || c.Head != f.head1 {
		t.Fatalf("pr 1: %+v", c)
	}
	if !reflect.DeepEqual(c.Files, []FileConflict{{Path: "rules.md", BaseCommit: f.mainTouchRules}}) {
		t.Fatalf("pr 1 files: %+v", c.Files)
	}
	if len(c.Overlap) != 0 {
		t.Fatalf("a conflicting pull request lists no overlap: %+v", c.Overlap)
	}
	if s := got[2]; s.State != StateClean || s.Base != "feat-one" || s.BaseSHA != f.head1 || len(s.Files) != 0 || len(s.Overlap) != 0 {
		t.Fatalf("pr 2 (clean against its parent): %+v", s)
	}
	if s := got[3]; s.State != StateClean || len(s.Overlap) != 0 {
		t.Fatalf("pr 3 (no shared file): %+v", s)
	}
	if s := got[4]; s.State != StateClean || !reflect.DeepEqual(s.Overlap, []string{"notes.md"}) || len(s.Files) != 0 {
		t.Fatalf("pr 4 (early warning, no conflict): %+v", s)
	}
	if after := snapshot(t, f.dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("local state changed:\nbefore %v\nafter  %v", before, after)
	}
}

// fakeGit scripts every answer and records the calls.
type fakeGit struct {
	fetchErr  error
	refs      map[string]string
	mergeBase map[string]string
	tree      map[string][]string // base+head -> conflict files
	touch     map[string]string   // path -> commit
	changed   map[string][]string // from+to -> files
	treeErr   map[string]error
	calls     []string
}

func (g *fakeGit) Fetch(_ context.Context, remote string, pulls []int, bases []string) error {
	g.calls = append(g.calls, fmt.Sprintf("fetch %s %v %v", remote, pulls, bases))
	return g.fetchErr
}
func (g *fakeGit) Resolve(_ context.Context, ref string) (string, error) { return g.refs[ref], nil }
func (g *fakeGit) MergeBase(_ context.Context, a, b string) (string, error) {
	return g.mergeBase[a+b], nil
}
func (g *fakeGit) MergeTree(_ context.Context, base, head string) (bool, []string, error) {
	if err := g.treeErr[base+head]; err != nil {
		return false, nil, err
	}
	files, ok := g.tree[base+head]
	return ok, files, nil
}
func (g *fakeGit) LastTouch(_ context.Context, _, _, path string) (string, error) {
	return g.touch[path], nil
}
func (g *fakeGit) Changed(_ context.Context, from, to string) ([]string, error) {
	return g.changed[from+to], nil
}

func sh(c string) string { return strings.Repeat(c, 40) }

func newFake() *fakeGit {
	return &fakeGit{refs: map[string]string{}, mergeBase: map[string]string{}, tree: map[string][]string{}, touch: map[string]string{}, changed: map[string][]string{}, treeErr: map[string]error{}}
}

func TestDetectUnknownReasons(t *testing.T) {
	ctx := context.Background()
	pr := PR{Number: 1, Base: "main", HeadRef: "x", Head: sh("a")}

	g := newFake()
	g.fetchErr = errors.New("boom")
	if f := Detect(ctx, g, "origin", []PR{pr}); f[0].State != StateUnknown || f[0].Reason != ReasonFetchFailed {
		t.Fatalf("fetch failure: %+v", f[0])
	}

	g = newFake()
	g.refs[NamespaceRoot+"pull/1"], g.refs[NamespaceRoot+"base/main"] = sh("b"), sh("c")
	if f := Detect(ctx, g, "origin", []PR{pr}); f[0].State != StateUnknown || f[0].Reason != ReasonHeadMoved {
		t.Fatalf("head moved: %+v", f[0])
	}

	g = newFake()
	g.refs[NamespaceRoot+"pull/1"] = sh("a")
	if f := Detect(ctx, g, "origin", []PR{pr}); f[0].State != StateUnknown || f[0].Reason != ReasonFetchFailed {
		t.Fatalf("base not fetched: %+v", f[0])
	}

	g = newFake()
	g.refs[NamespaceRoot+"pull/1"], g.refs[NamespaceRoot+"base/main"] = sh("a"), sh("c")
	if f := Detect(ctx, g, "origin", []PR{pr}); f[0].State != StateUnknown || f[0].Reason != ReasonNoBase {
		t.Fatalf("no merge base: %+v", f[0])
	}

	g.mergeBase[sh("c")+sh("a")] = sh("d")
	g.treeErr[sh("c")+sh("a")] = errors.New("boom")
	if f := Detect(ctx, g, "origin", []PR{pr}); f[0].State != StateUnknown || f[0].Reason != ReasonProbeFailed {
		t.Fatalf("probe failure: %+v", f[0])
	}

	g = newFake()
	bad := PR{Number: 2, Base: "a..b", HeadRef: "y", Head: sh("a")}
	g.refs[NamespaceRoot+"pull/1"], g.refs[NamespaceRoot+"base/main"] = sh("a"), sh("c")
	got := Detect(ctx, g, "origin", []PR{bad, pr})
	if got[0].State != StateUnknown || got[0].Reason != ReasonRefInvalid {
		t.Fatalf("invalid base: %+v", got[0])
	}
	if strings.Contains(strings.Join(g.calls, " "), "a..b") {
		t.Fatalf("an invalid base reached the fetch: %v", g.calls)
	}
	if got[1].Reason == ReasonRefInvalid {
		t.Fatalf("the valid pull request was refused too: %+v", got[1])
	}

	pr0 := PR{Number: 5, Base: "main", Head: "nothex"}
	if f := Detect(ctx, newFake(), "origin", []PR{pr0}); f[0].State != StateUnknown || f[0].Reason != ReasonRefInvalid {
		t.Fatalf("invalid head: %+v", f[0])
	}
}

func TestDetectProbeLimitAndTruncation(t *testing.T) {
	g := newFake()
	var prs []PR
	for n := 1; n <= MaxProbed+3; n++ {
		prs = append(prs, PR{Number: n, Base: "main", HeadRef: "h", Head: sh("a")})
		g.refs[fmt.Sprintf("%spull/%d", NamespaceRoot, n)] = sh("a")
	}
	g.refs[NamespaceRoot+"base/main"] = sh("c")
	g.mergeBase[sh("c")+sh("a")] = sh("d")
	var many []string
	for i := 0; i < MaxFiles+5; i++ {
		many = append(many, fmt.Sprintf("f%03d.md", i))
	}
	g.tree[sh("c")+sh("a")] = many
	got := Detect(context.Background(), g, "origin", prs)
	if len(got) != len(prs) {
		t.Fatalf("%d findings for %d pull requests", len(got), len(prs))
	}
	if f := got[0]; f.State != StateConflicts || len(f.Files) != MaxFiles || !f.Truncated {
		t.Fatalf("truncation: state %s files %d truncated %v", f.State, len(f.Files), f.Truncated)
	}
	for _, f := range got[MaxProbed:] {
		if f.State != StateUnknown || f.Reason != ReasonPastLimit {
			t.Fatalf("past the limit: %+v", f)
		}
	}
	fetched := g.calls[0]
	if !strings.HasSuffix(fetched, "[main]") {
		t.Fatalf("a base shared by many pull requests is fetched once: %s", fetched)
	}
	if strings.Contains(fetched, fmt.Sprint(MaxProbed+1)+" ") || strings.Contains(fetched, fmt.Sprint(MaxProbed+3)+"]") {
		t.Fatalf("pull requests past the limit were fetched: %s", fetched)
	}
}

func TestDetectOverlapIsSortedAndBounded(t *testing.T) {
	g := newFake()
	g.refs[NamespaceRoot+"pull/1"], g.refs[NamespaceRoot+"base/main"] = sh("a"), sh("c")
	g.mergeBase[sh("c")+sh("a")] = sh("d")
	g.changed[sh("d")+sh("c")] = []string{"b.md", "a.md", "x.md"}
	g.changed[sh("d")+sh("a")] = []string{"x.md", "a.md", "z.md"}
	f := Detect(context.Background(), g, "origin", []PR{{Number: 1, Base: "main", HeadRef: "h", Head: sh("a")}})[0]
	if f.State != StateClean || !reflect.DeepEqual(f.Overlap, []string{"a.md", "x.md"}) {
		t.Fatalf("%+v", f)
	}
}

func TestDetectOverlapTruncation(t *testing.T) {
	g := newFake()
	g.refs[NamespaceRoot+"pull/1"], g.refs[NamespaceRoot+"base/main"] = sh("a"), sh("c")
	g.mergeBase[sh("c")+sh("a")] = sh("d")
	var many []string
	for i := 0; i < MaxFiles+5; i++ {
		many = append(many, fmt.Sprintf("f%03d.md", i))
	}
	g.changed[sh("d")+sh("c")], g.changed[sh("d")+sh("a")] = many, many
	f := Detect(context.Background(), g, "origin", []PR{{Number: 1, Base: "main", HeadRef: "h", Head: sh("a")}})[0]
	if len(f.Overlap) != MaxFiles || !f.Truncated {
		t.Fatalf("overlap %d truncated %v", len(f.Overlap), f.Truncated)
	}
}
