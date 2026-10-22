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
			state := PullListState{PerPage: tc.perPage}
			_, err := f.reader("github.com").ListOpenPulls(context.Background(), "a/b", state, tc.maxOpen)
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
