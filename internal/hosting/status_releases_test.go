// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

const releasesPage = "repos/o/r/releases?per_page=100&page="
const tagsPage = "repos/o/r/tags?per_page=100&page="

func link(next int) map[string]string {
	return map[string]string{"Link": fmt.Sprintf(`<https://api.github.com/repositories/1/releases?per_page=100&page=%d>; rel="next", <https://api.github.com/repositories/1/releases?per_page=100&page=99>; rel="last"`, next)}
}

func (f *fakeForge) body(uri, body string, header map[string]string) {
	f.handlers[uri] = func(*http.Request) *http.Response { return forgeResponse(200, body, header) }
}

func rel(draft bool, tag string) string {
	return fmt.Sprintf(`{"draft":%v,"name":"Name %s","tag_name":%q,"created_at":"2026-10-01T10:00:00Z","html_url":"https://github.com/o/r/releases/%s","assets":[{"id":1},{"id":2}]}`, draft, tag, tag, tag)
}

func TestReleaseListTypedFields(t *testing.T) {
	f := newFakeForge(t)
	f.body(releasesPage+"1", `[`+rel(true, "v1.1.0")+`,`+rel(false, "v1.0.0")+`,{"draft":true,"name":null,"tag_name":"v2.0.0","created_at":null,"html_url":"https://github.com/o/r/releases/x","assets":[]}]`, nil)
	r := f.reader("github.com")
	got, err := r.ReleaseList(context.Background(), "o/r")
	if err != nil {
		t.Fatal(err)
	}
	if calls := r.TakeCalls(); calls[KindReleases].Requests != 1 || len(calls) != 1 {
		t.Fatalf("accounting %+v", calls)
	}
	want := []StatusRelease{
		{Draft: true, Name: "Name v1.1.0", Tag: "v1.1.0", URL: "https://github.com/o/r/releases/v1.1.0", Assets: 2, CreatedAt: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)},
		{Draft: false, Name: "Name v1.0.0", Tag: "v1.0.0", URL: "https://github.com/o/r/releases/v1.0.0", Assets: 2, CreatedAt: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)},
		{Draft: true, Tag: "v2.0.0", URL: "https://github.com/o/r/releases/x"},
	}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %+v want %+v", i, got[i], want[i])
		}
	}
	if len(f.log) != 1 {
		t.Fatalf("one page, one request: %v", f.log)
	}
}

func TestReleaseListFollowsLinkAcrossTwoPages(t *testing.T) {
	f := newFakeForge(t)
	f.body(releasesPage+"1", `[`+rel(false, "v1.0.0")+`]`, link(2))
	f.body(releasesPage+"2", `[`+rel(true, "v1.1.0")+`]`, nil)
	got, err := f.reader("github.com").ReleaseList(context.Background(), "o/r")
	if err != nil || len(got) != 2 || !got[1].Draft || got[1].Tag != "v1.1.0" {
		t.Fatalf("%+v %v", got, err)
	}
	if strings.Join(f.log, " ") != releasesPage+"1 "+releasesPage+"2" {
		t.Fatalf("%v", f.log)
	}
}

// A full page without a Link header is the last page: the Link header decides, not the row count.
func TestReleaseListStopsWithoutLinkEvenOnAFullPage(t *testing.T) {
	f := newFakeForge(t)
	rows := make([]string, 100)
	for i := range rows {
		rows[i] = rel(false, fmt.Sprintf("v1.0.%d", i))
	}
	f.body(releasesPage+"1", `[`+strings.Join(rows, ",")+`]`, nil)
	got, err := f.reader("github.com").ReleaseList(context.Background(), "o/r")
	if err != nil || len(got) != 100 || len(f.log) != 1 {
		t.Fatalf("%d %v %v", len(got), err, f.log)
	}
}

// The request is built from the typed page number: the URL inside the Link header is never requested.
func TestReleaseListNeverRequestsTheLinkURL(t *testing.T) {
	f := newFakeForge(t)
	f.body(releasesPage+"1", `[`+rel(false, "v1.0.0")+`]`, map[string]string{"Link": `<https://evil.example/x?page=2>; rel="next"`})
	f.body(releasesPage+"2", `[]`, nil)
	if _, err := f.reader("github.com").ReleaseList(context.Background(), "o/r"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.log, " ") != releasesPage+"1 "+releasesPage+"2" {
		t.Fatalf("%v", f.log)
	}
}

func TestReleaseListTenPagesThenTooMany(t *testing.T) {
	f := newFakeForge(t)
	for p := 1; p <= 10; p++ {
		f.body(fmt.Sprintf("%s%d", releasesPage, p), `[`+rel(false, fmt.Sprintf("v1.0.%d", p))+`]`, link(p+1))
	}
	_, err := f.reader("github.com").ReleaseList(context.Background(), "o/r")
	assertWatchCode(t, err, CodeTooManyReleases)
	if len(f.log) != 10 {
		t.Fatalf("page 11 must never be requested: %v", f.log)
	}
}

func TestReleaseListTenthPageWithoutNextIsComplete(t *testing.T) {
	f := newFakeForge(t)
	for p := 1; p <= 10; p++ {
		h := link(p + 1)
		if p == 10 {
			h = nil
		}
		f.body(fmt.Sprintf("%s%d", releasesPage, p), `[`+rel(false, fmt.Sprintf("v1.0.%d", p))+`]`, h)
	}
	got, err := f.reader("github.com").ReleaseList(context.Background(), "o/r")
	if err != nil || len(got) != 10 {
		t.Fatalf("%d %v", len(got), err)
	}
}

func TestReleaseListRefusesBadLinks(t *testing.T) {
	for name, header := range map[string]map[string]string{
		"skips a page":     link(3),
		"repeats the page": link(1),
		"unusable next":    {"Link": `<https://api.github.com/x?page=abc>; rel="next"`},
	} {
		f := newFakeForge(t)
		f.body(releasesPage+"1", `[`+rel(false, "v1.0.0")+`]`, header)
		_, err := f.reader("github.com").ReleaseList(context.Background(), "o/r")
		if we := assertWatchCode(t, err, CodeMalformed); we == nil {
			t.Fatal(name)
		}
		if len(f.log) != 1 {
			t.Fatalf("%s: %v", name, f.log)
		}
	}
}

func TestReleaseListPageTooLarge(t *testing.T) {
	f := newFakeForge(t)
	f.body(releasesPage+"1", `[{"draft":false,"tag_name":"v1","body":"`+strings.Repeat("x", maxBytes)+`"}]`, nil)
	_, err := f.reader("github.com").ReleaseList(context.Background(), "o/r")
	assertWatchCode(t, err, CodePageTooLarge)
}

func TestReleaseListExactlyOneMiBIsAccepted(t *testing.T) {
	f := newFakeForge(t)
	prefix, suffix := `[{"draft":false,"tag_name":"v1","body":"`, `"}]`
	f.body(releasesPage+"1", prefix+strings.Repeat("x", maxBytes-len(prefix)-len(suffix))+suffix, nil)
	if got, err := f.reader("github.com").ReleaseList(context.Background(), "o/r"); err != nil || len(got) != 1 {
		t.Fatalf("%v", err)
	}
}

func TestReleaseListMalformedPages(t *testing.T) {
	for name, body := range map[string]string{
		"object":           `{}`,
		"null":             `null`,
		"no draft flag":    `[{"tag_name":"v1"}]`,
		"bad created_at":   `[{"draft":true,"tag_name":"v1","created_at":"yesterday"}]`,
		"duplicate key":    `[{"draft":true,"draft":false,"tag_name":"v1"}]`,
		"assets not array": `[{"draft":true,"tag_name":"v1","assets":3}]`,
		"over 100 rows":    `[` + strings.Repeat(`{"draft":false},`, 100) + `{"draft":false}]`,
	} {
		f := newFakeForge(t)
		f.body(releasesPage+"1", body, nil)
		_, err := f.reader("github.com").ReleaseList(context.Background(), "o/r")
		if we := assertWatchCode(t, err, CodeMalformed); we == nil {
			t.Fatal(name)
		}
	}
}

func TestReleaseListFailuresKeepTheirCodes(t *testing.T) {
	for status, code := range map[int]string{401: CodeUnauthorized, 403: CodeForbidden, 404: CodeForbidden, 500: CodeHTTP5xx, 429: CodeRateLimited} {
		f := newFakeForge(t)
		f.handlers[releasesPage+"1"] = func(*http.Request) *http.Response { return response(status, `{}`) }
		_, err := f.reader("github.com").ReleaseList(context.Background(), "o/r")
		assertWatchCode(t, err, code)
	}
}

func TestReleaseListRefusesInvalidRepoWithoutRequest(t *testing.T) {
	f := newFakeForge(t)
	_, err := f.reader("github.com").ReleaseList(context.Background(), "o")
	assertWatchCode(t, err, CodeMalformed)
	if len(f.log) != 0 {
		t.Fatalf("%v", f.log)
	}
}

func TestReleaseListRefusesRedirect(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[releasesPage+"1"] = func(*http.Request) *http.Response {
		return forgeResponse(301, ``, map[string]string{"Location": "https://api.github.com/repos/o/new/releases?per_page=100&page=1"})
	}
	_, err := f.reader("github.com").ReleaseList(context.Background(), "o/r")
	if we := assertWatchCode(t, err, CodeRepoMoved); we.NewName != "o/new" {
		t.Fatalf("%+v", we)
	}
}

func TestTagListTypedFieldsAndPaging(t *testing.T) {
	sha1, sha2 := strings.Repeat("a", 40), strings.Repeat("b", 40)
	f := newFakeForge(t)
	f.body(tagsPage+"1", `[{"name":"v1.0.0","commit":{"sha":"`+sha1+`"},"zipball_url":"x"}]`, link(2))
	f.body(tagsPage+"2", `[{"name":"v1.1.0","commit":{"sha":"`+sha2+`"}}]`, nil)
	got, err := f.reader("github.com").TagList(context.Background(), "o/r")
	if err != nil || len(got) != 2 || got[0] != (StatusTag{"v1.0.0", sha1}) || got[1] != (StatusTag{"v1.1.0", sha2}) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestTagListBoundsAndMalformed(t *testing.T) {
	sha := strings.Repeat("a", 40)
	f := newFakeForge(t)
	for p := 1; p <= 10; p++ {
		f.body(fmt.Sprintf("%s%d", tagsPage, p), `[{"name":"v1.0.0","commit":{"sha":"`+sha+`"}}]`, link(p+1))
	}
	_, err := f.reader("github.com").TagList(context.Background(), "o/r")
	assertWatchCode(t, err, CodeTooManyReleases)
	if len(f.log) != 10 {
		t.Fatalf("%v", f.log)
	}
	for name, body := range map[string]string{
		"no name":     `[{"commit":{"sha":"` + sha + `"}}]`,
		"no sha":      `[{"name":"v1"}]`,
		"short sha":   `[{"name":"v1","commit":{"sha":"abc"}}]`,
		"upper sha":   `[{"name":"v1","commit":{"sha":"` + strings.Repeat("A", 40) + `"}}]`,
		"not a array": `{}`,
	} {
		g := newFakeForge(t)
		g.body(tagsPage+"1", body, nil)
		_, err := g.reader("github.com").TagList(context.Background(), "o/r")
		if we := assertWatchCode(t, err, CodeMalformed); we == nil {
			t.Fatal(name)
		}
	}
	big := newFakeForge(t)
	big.body(tagsPage+"1", `[{"name":"`+strings.Repeat("x", maxBytes)+`","commit":{"sha":"`+sha+`"}}]`, nil)
	_, err = big.reader("github.com").TagList(context.Background(), "o/r")
	assertWatchCode(t, err, CodePageTooLarge)
}

// The status reader itself (guard mode status, not the legacy watch flag of fakeForge.reader) admits
// the four reads and refuses the search.
func TestStatusGuardModeAdmitsTheStatusReads(t *testing.T) {
	sha := strings.Repeat("a", 40)
	f := newFakeForge(t)
	f.body("repos/o/r", `{"default_branch":"main","html_url":"https://github.com/o/r"}`, nil)
	f.body(releasesPage+"1", `[`+rel(false, "v1.0.0")+`]`, nil)
	f.body(tagsPage+"1", `[{"name":"v1.0.0","commit":{"sha":"`+sha+`"}}]`, nil)
	client, err := newSDKClientGuardMode("github.com", "fixturetoken", t.TempDir(), f, guardStatus)
	if err != nil {
		t.Fatal(err)
	}
	r := newWatchReader(&ghAPI{client: client, host: "github.com"})
	r.mode = guardStatus
	ctx := context.Background()
	if _, err := r.RepoInfo(ctx, "o/r"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReleaseList(ctx, "o/r"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.TagList(ctx, "o/r"); err != nil {
		t.Fatal(err)
	}
	if len(f.log) != 3 {
		t.Fatalf("%v", f.log)
	}
}
