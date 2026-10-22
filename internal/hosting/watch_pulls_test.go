// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func pullJSON(n int) string {
	return fmt.Sprintf(`{"number":%d,"title":"t%d","user":{"login":"u"},"labels":[{"name":"l1"},{"name":"l2"}],"draft":%v,"head":{"sha":"%040x","ref":"feat/%d"},"base":{"ref":"main","sha":"%040x"},"ignored":{"x":[1,2,3]}}`, n, n, n%2 == 0, n, n, n+1000)
}

func pageBody(from, to int) string {
	var items []string
	for n := from; n <= to; n++ {
		items = append(items, pullJSON(n))
	}
	return "[" + strings.Join(items, ",") + "]"
}

func listURI(perPage, page int) string {
	return fmt.Sprintf("repos/a/b/pulls?state=open&per_page=%d&page=%d", perPage, page)
}

func linkNext(perPage, page int) map[string]string {
	return map[string]string{"Link": fmt.Sprintf(`<https://api.github.com/repositories/1/pulls?state=open&per_page=%d&page=%d>; rel="next", <https://api.github.com/repositories/1/pulls?state=open&per_page=%d&page=9>; rel="last"`, perPage, page, perPage)}
}

// threePages serves 60 PRs as 25+25+10 with a version suffix in each ETag.
func threePages(f *fakeForge, v [3]string) {
	f.serve(listURI(25, 1), `"p1`+v[0]+`"`, pageBody(1, 25), linkNext(25, 2))
	f.serve(listURI(25, 2), `"p2`+v[1]+`"`, pageBody(26, 50), linkNext(25, 3))
	f.serve(listURI(25, 3), `"p3`+v[2]+`"`, pageBody(51, 60), nil)
}

func TestListOpenPullsPaginatesWithLinkAcrossThreePages(t *testing.T) {
	f := newFakeForge(t)
	threePages(f, [3]string{"a", "a", "a"})
	r := f.reader("github.com")
	res, err := r.ListOpenPulls(context.Background(), "a/b", PullListState{}, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 60 || res.Unchanged || res.State.PerPage != 25 || len(res.State.Pages) != 3 {
		t.Fatalf("%d items unchanged=%v state=%+v", len(res.Items), res.Unchanged, res.State.PerPage)
	}
	it := res.Items[1]
	if it.Number != 2 || it.Title != "t2" || it.User != "u" || len(it.Labels) != 2 || it.Labels[1] != "l2" || !it.Draft || it.HeadRef != "feat/2" || it.HeadSHA != fmt.Sprintf("%040x", 2) || it.BaseRef != "main" || it.BaseSHA != fmt.Sprintf("%040x", 1002) {
		t.Fatalf("%+v", it)
	}
	want := []string{listURI(25, 1), listURI(25, 2), listURI(25, 3)}
	if strings.Join(f.log, "|") != strings.Join(want, "|") {
		t.Fatalf("requests %v", f.log)
	}
	if res.State.Pages[0].ETag != `"p1a"` || res.State.Pages[1].Link == "" || res.State.Pages[2].Link != "" {
		t.Fatalf("stored page data %+v", res.State.Pages)
	}
	if c := r.TakeCalls()[KindList]; c.Requests != 3 || c.NotModified != 0 {
		t.Fatalf("%+v", c)
	}
}

func TestListOpenPulls304ReusesStoredLinkAndItems(t *testing.T) {
	f := newFakeForge(t)
	threePages(f, [3]string{"a", "a", "a"})
	r := f.reader("github.com")
	first, err := r.ListOpenPulls(context.Background(), "a/b", PullListState{}, 2000)
	if err != nil {
		t.Fatal(err)
	}
	r.TakeCalls()
	f.log, f.etags = nil, nil
	second, err := r.ListOpenPulls(context.Background(), "a/b", first.State, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Unchanged || len(second.Items) != 60 || len(f.log) != 3 {
		t.Fatalf("unchanged=%v items=%d log=%v", second.Unchanged, len(second.Items), f.log)
	}
	for i, e := range f.etags {
		if e != first.State.Pages[i].ETag {
			t.Fatalf("If-None-Match %d = %q", i, e)
		}
	}
	if c := r.TakeCalls()[KindList]; c.Requests != 3 || c.NotModified != 3 {
		t.Fatalf("all three must be 304: %+v", c)
	}
	// Only page 2 changes: pages 1 and 3 stay 304 and the Link chain still reaches page 3.
	threePages(f, [3]string{"a", "b", "a"})
	f.handlers[listURI(25, 2)] = func(r *http.Request) *http.Response {
		return forgeResponse(200, pageBody(26, 49), map[string]string{"ETag": `"p2b"`, "Link": linkNext(25, 3)["Link"]})
	}
	f.log = nil
	third, err := r.ListOpenPulls(context.Background(), "a/b", second.State, 2000)
	if err != nil || third.Unchanged || len(third.Items) != 59 || third.State.Pages[1].ETag != `"p2b"` {
		t.Fatalf("%v %d", err, len(third.Items))
	}
}

func TestListOpenPullsHalvesAndRestartsAtPageOne(t *testing.T) {
	f := newFakeForge(t)
	f.serve(listURI(25, 1), `"x1"`, pageBody(1, 25), linkNext(25, 2))
	f.handlers[listURI(25, 2)] = func(*http.Request) *http.Response {
		return forgeResponse(200, strings.Repeat("x", maxBytes+1), nil)
	}
	f.serve(listURI(12, 1), `"y1"`, pageBody(1, 12), linkNext(12, 2))
	f.serve(listURI(12, 2), `"y2"`, pageBody(13, 24), linkNext(12, 3))
	f.serve(listURI(12, 3), `"y3"`, pageBody(25, 30), nil)
	r := f.reader("github.com")
	res, err := r.ListOpenPulls(context.Background(), "a/b", PullListState{}, 2000)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{listURI(25, 1), listURI(25, 2), listURI(12, 1), listURI(12, 2), listURI(12, 3)}
	if strings.Join(f.log, "|") != strings.Join(want, "|") {
		t.Fatalf("restart at page 1 expected: %v", f.log)
	}
	if res.State.PerPage != 12 || len(res.Items) != 30 || len(res.State.Pages) != 3 {
		t.Fatalf("%+v %d", res.State.PerPage, len(res.Items))
	}
	// 12 is kept for the repository: the next listing starts at 12 and is conditional.
	f.log, f.etags = nil, nil
	again, err := r.ListOpenPulls(context.Background(), "a/b", res.State, 2000)
	if err != nil || !again.Unchanged || f.log[0] != listURI(12, 1) || f.etags[0] != `"y1"` {
		t.Fatalf("%v %v %v", err, f.log, f.etags)
	}
}

func TestListOpenPullsPageTooLargeAtTwelve(t *testing.T) {
	f := newFakeForge(t)
	big := func(*http.Request) *http.Response { return forgeResponse(200, strings.Repeat("x", maxBytes+1), nil) }
	f.handlers[listURI(25, 1)] = big
	f.handlers[listURI(12, 1)] = big
	_, err := f.reader("github.com").ListOpenPulls(context.Background(), "a/b", PullListState{}, 2000)
	assertWatchCode(t, err, CodePageTooLarge)
	if len(f.log) != 2 {
		t.Fatalf("%v", f.log)
	}
	// Also when the declared Content-Length alone is over the cap.
	f2 := newFakeForge(t)
	f2.handlers[listURI(25, 1)] = func(*http.Request) *http.Response {
		res := forgeResponse(200, "[]", nil)
		res.ContentLength = maxBytes + 1
		return res
	}
	f2.handlers[listURI(12, 1)] = f2.handlers[listURI(25, 1)]
	_, err = f2.reader("github.com").ListOpenPulls(context.Background(), "a/b", PullListState{}, 2000)
	assertWatchCode(t, err, CodePageTooLarge)
}

func TestListOpenPullsTooManyOpenPRs(t *testing.T) {
	for _, tc := range []struct {
		perPage, maxOpen, wantPages int
	}{{25, 2000, 80}, {12, 2000, 167}, {25, 30, 2}, {25, 25, 1}} {
		t.Run(fmt.Sprintf("%d-%d", tc.perPage, tc.maxOpen), func(t *testing.T) {
			f := newFakeForge(t)
			for p := 1; p <= 200; p++ {
				uri := listURI(tc.perPage, p)
				body := "[" + pullJSON(p) + "]"
				f.serve(uri, fmt.Sprintf(`"e%d"`, p), body, linkNext(tc.perPage, p+1)) // endless list
			}
			r := f.reader("github.com")
			if tc.perPage == 12 {
				r.markHalved("a/b")
			}
			_, err := r.ListOpenPulls(context.Background(), "a/b", PullListState{PerPage: tc.perPage}, tc.maxOpen)
			assertWatchCode(t, err, CodeTooManyPRs)
			if len(f.log) != tc.wantPages {
				t.Fatalf("fetched %d pages, want %d", len(f.log), tc.wantPages)
			}
		})
	}
	// More items than max_open_prs on the last page.
	f := newFakeForge(t)
	f.serve(listURI(25, 1), `"e"`, pageBody(1, 25), nil)
	_, err := f.reader("github.com").ListOpenPulls(context.Background(), "a/b", PullListState{}, 10)
	assertWatchCode(t, err, CodeTooManyPRs)
}

func TestListOpenPullsRejectsBadBoundsAndLinks(t *testing.T) {
	f := newFakeForge(t)
	r := f.reader("github.com")
	for _, max := range []int{0, -1, WatchMaxOpenPRs + 1} {
		_, err := r.ListOpenPulls(context.Background(), "a/b", PullListState{}, max)
		assertWatchCode(t, err, CodeMalformed)
	}
	if len(f.log) != 0 {
		t.Fatal("request sent for invalid bounds")
	}
	// A Link that skips pages or goes backwards is never followed.
	for _, link := range []string{linkNext(25, 3)["Link"], linkNext(25, 1)["Link"], `<https://api.github.com/x?page=zzz>; rel="next"`} {
		f := newFakeForge(t)
		f.serve(listURI(25, 1), `"e"`, pageBody(1, 3), map[string]string{"Link": link})
		_, err := f.reader("github.com").ListOpenPulls(context.Background(), "a/b", PullListState{}, 2000)
		assertWatchCode(t, err, CodeMalformed)
		if len(f.log) != 1 {
			t.Fatalf("followed %v", f.log)
		}
	}
	// Malformed items.
	for _, body := range []string{`{"a":1}`, `[{"number":0}]`, `[{"number":1,"head":{"sha":"short"}}]`, `[1]`, `not json`} {
		f := newFakeForge(t)
		f.serve(listURI(25, 1), `"e"`, body, nil)
		_, err := f.reader("github.com").ListOpenPulls(context.Background(), "a/b", PullListState{}, 2000)
		assertWatchCode(t, err, CodeMalformed)
	}
}

func TestListOpenPullsErrorsPassThrough(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[listURI(25, 1)] = func(*http.Request) *http.Response {
		return forgeResponse(301, "x", map[string]string{"Location": "https://api.github.com/repos/new/name/pulls?state=open&per_page=25&page=1"})
	}
	_, err := f.reader("github.com").ListOpenPulls(context.Background(), "a/b", PullListState{}, 2000)
	if we := assertWatchCode(t, err, CodeRepoMoved); we.NewName != "new/name" {
		t.Fatalf("%q", we.NewName)
	}
	f = newFakeForge(t)
	f.handlers[listURI(25, 1)] = func(*http.Request) *http.Response {
		return forgeResponse(403, `{"message":"secondary rate limit"}`, map[string]string{"Retry-After": "30"})
	}
	_, err = f.reader("github.com").ListOpenPulls(context.Background(), "a/b", PullListState{}, 2000)
	assertWatchCode(t, err, CodeRateLimited)
	if len(f.log) != 1 {
		t.Fatalf("retried a rate-limited list: %v", f.log)
	}
}

// GitHub's 304 carries no Link header; the chain must come from the stored Link.
func TestListOpenPullsStoredLinkSurvivesLinklessNotModified(t *testing.T) {
	f := newFakeForge(t)
	threePages(f, [3]string{"a", "a", "a"})
	r := f.reader("github.com")
	first, err := r.ListOpenPulls(context.Background(), "a/b", PullListState{}, 2000)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := r.PullListPage(context.Background(), "a/b", 25, 1, first.State.Pages[0].ETag)
	if err != nil || !probe.NotModified || probe.Link != "" {
		t.Fatalf("fake forge must answer a Link-less 304: %+v %v", probe, err)
	}
	f.log = nil
	second, err := r.ListOpenPulls(context.Background(), "a/b", first.State, 2000)
	if err != nil || !second.Unchanged || len(second.Items) != 60 || len(f.log) != 3 {
		t.Fatalf("%v unchanged=%v items=%d log=%v", err, second.Unchanged, len(second.Items), f.log)
	}
	if second.State.Pages[0].Link != first.State.Pages[0].Link || second.State.Pages[1].Link == "" {
		t.Fatalf("stored Link lost: %+v", second.State.Pages)
	}
}

// A 304 reuses the stored Link, so an exactly full last page hides a PR that was
// reopened behind it: the next page is probed once, conditionally.
func TestListOpenPullsProbesPastAFullLastPageOnNotModified(t *testing.T) {
	f := newFakeForge(t)
	f.serve(listURI(25, 1), `"p1"`, pageBody(1, 25), nil)
	f.serve(listURI(25, 2), `"p2"`, "[]", nil)
	r := f.reader("github.com")
	ctx := context.Background()
	// A 200 is authoritative: its missing Link needs no probe.
	first, err := r.ListOpenPulls(ctx, "a/b", PullListState{}, 2000)
	if err != nil || len(first.Items) != 25 || len(f.log) != 1 {
		t.Fatalf("%v items=%d log=%v", err, len(first.Items), f.log)
	}
	// A 304 on the full last page probes page 2 (first time: unconditional).
	f.log, f.etags = nil, nil
	second, err := r.ListOpenPulls(ctx, "a/b", first.State, 2000)
	if err != nil || !second.Unchanged || len(second.Items) != 25 || strings.Join(f.log, "|") != listURI(25, 1)+"|"+listURI(25, 2) || f.etags[1] != "" {
		t.Fatalf("%v unchanged=%v log=%v etags=%q", err, second.Unchanged, f.log, f.etags)
	}
	// The probed empty page is stored, so the next probe is conditional and the listing stays unchanged.
	f.log, f.etags = nil, nil
	third, err := r.ListOpenPulls(ctx, "a/b", second.State, 2000)
	if err != nil || !third.Unchanged || len(f.log) != 2 || f.etags[1] != `"p2"` {
		t.Fatalf("%v unchanged=%v log=%v etags=%q", err, third.Unchanged, f.log, f.etags)
	}
	// An older PR is reopened: page 1 stays 304, page 2 now holds it.
	f.serve(listURI(25, 2), `"p2b"`, pageBody(26, 26), nil)
	fourth, err := r.ListOpenPulls(ctx, "a/b", third.State, 2000)
	if err != nil || fourth.Unchanged || len(fourth.Items) != 26 || fourth.Items[25].Number != 26 {
		t.Fatalf("reopened PR missed: %v unchanged=%v items=%d", err, fourth.Unchanged, len(fourth.Items))
	}
	// A short last page is never probed.
	g := newFakeForge(t)
	g.serve(listURI(25, 1), `"q1"`, pageBody(1, 24), nil)
	gr := g.reader("github.com")
	s1, err := gr.ListOpenPulls(ctx, "a/b", PullListState{}, 2000)
	if err != nil {
		t.Fatal(err)
	}
	g.log = nil
	if _, err = gr.ListOpenPulls(ctx, "a/b", s1.State, 2000); err != nil || len(g.log) != 1 {
		t.Fatalf("%v %v", err, g.log)
	}
}

// The probe of a full last page at the page bound must not leave the allowlist.
func TestListOpenPullsNoProbeBeyondAllowlistBound(t *testing.T) {
	f := newFakeForge(t)
	for p := 1; p <= 80; p++ {
		next := linkNext(25, p+1)
		if p == 80 {
			next = nil
		}
		f.serve(listURI(25, p), fmt.Sprintf(`"e%d"`, p), pageBody((p-1)*25+1, p*25), next)
	}
	r := f.reader("github.com")
	first, err := r.ListOpenPulls(context.Background(), "a/b", PullListState{}, 2000)
	if err != nil || len(first.Items) != 2000 {
		t.Fatalf("%v %d", err, len(first.Items))
	}
	f.log = nil
	if _, err := r.ListOpenPulls(context.Background(), "a/b", first.State, 2000); err != nil || len(f.log) != 80 {
		t.Fatalf("%v requests=%d", err, len(f.log))
	}
}

// The halved page size lives in the reader (until restart); it is not read back from
// the stored state, so a restart begins at 25 again and never trusts stored 12-sized pages.
func TestHalvedPageSizeIsInMemoryOnly(t *testing.T) {
	f := newFakeForge(t)
	f.serve(listURI(25, 1), `"x1"`, pageBody(1, 25), linkNext(25, 2))
	f.handlers[listURI(25, 2)] = func(*http.Request) *http.Response {
		return forgeResponse(200, strings.Repeat("x", maxBytes+1), nil)
	}
	f.serve(listURI(12, 1), `"y1"`, pageBody(1, 12), linkNext(12, 2))
	f.serve(listURI(12, 2), `"y2"`, pageBody(13, 20), nil)
	ctx := context.Background()
	r := f.reader("github.com")
	res, err := r.ListOpenPulls(ctx, "a/b", PullListState{}, 2000)
	if err != nil || res.State.PerPage != 12 {
		t.Fatalf("%v %+v", err, res.State.PerPage)
	}
	// Same reader: still 12 (kept for the repository until restart).
	f.log = nil
	if _, err = r.ListOpenPulls(ctx, "a/b", res.State, 2000); err != nil || f.log[0] != listURI(12, 1) {
		t.Fatalf("%v %v", err, f.log)
	}
	// Another repository on the same reader is unaffected.
	f.log = nil
	f.serve("repos/c/d/pulls?state=open&per_page=25&page=1", `"z"`, pageBody(1, 3), nil)
	if _, err = r.ListOpenPulls(ctx, "c/d", PullListState{}, 2000); err != nil || f.log[0] != "repos/c/d/pulls?state=open&per_page=25&page=1" {
		t.Fatalf("%v %v", err, f.log)
	}
	// A new reader (restart) with the stored 12-sized state starts at 25 and sends no stale validator.
	f.log, f.etags = nil, nil
	r2 := f.reader("github.com")
	if _, err = r2.ListOpenPulls(ctx, "a/b", res.State, 2000); err != nil {
		t.Fatal(err)
	}
	if f.log[0] != listURI(25, 1) || f.etags[0] != "" {
		t.Fatalf("restart must begin at 25 without validators: %v %q", f.log, f.etags)
	}
}
