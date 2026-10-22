// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fullItem(n int, user, utype string) string {
	return fmt.Sprintf(`{"number":%d,"title":"t%d","html_url":"https://github.com/a/b/pull/%d","created_at":"2026-10-01T12:30:00Z","user":{"login":%q,"type":%q},`+
		`"labels":[{"name":"x"},{"name":"y"}],"draft":false,"requested_reviewers":[{"login":"r1"},{"login":"r2"}],"requested_teams":[{"slug":"core","name":"Core"}],`+
		`"head":{"sha":"%040x","ref":"feat/%d"},"base":{"ref":"main","sha":"%040x"}}`, n, n, n, user, utype, n, n, n+1000)
}

func TestParsePullListCarriesTheExtraFields(t *testing.T) {
	body := "[" + fullItem(1, "alice", "User") + "," + fullItem(2, "dependabot[bot]", "Bot") + "]"
	items, err := ParsePullList([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("%d items", len(items))
	}
	a, b := items[0], items[1]
	if a.Number != 1 || a.Title != "t1" || a.User != "alice" || a.UserType != "User" || a.HTMLURL != "https://github.com/a/b/pull/1" ||
		!a.CreatedAt.Equal(time.Date(2026, 10, 1, 12, 30, 0, 0, time.UTC)) || a.Draft || a.HeadSHA != fmt.Sprintf("%040x", 1) || a.HeadRef != "feat/1" || a.BaseRef != "main" ||
		!reflect.DeepEqual(a.Labels, []string{"x", "y"}) || !reflect.DeepEqual(a.Reviewers, []string{"r1", "r2"}) || !reflect.DeepEqual(a.Teams, []string{"core"}) {
		t.Fatalf("%+v", a)
	}
	if b.User != "dependabot[bot]" || b.UserType != "Bot" {
		t.Fatalf("bot item %+v", b)
	}
}

func TestParsePullListMissingOptionalFields(t *testing.T) {
	body := fmt.Sprintf(`[{"number":7,"head":{"sha":"%040x"}}]`, 7)
	items, err := ParsePullList([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	it := items[0]
	if it.Number != 7 || it.UserType != "" || it.HTMLURL != "" || !it.CreatedAt.IsZero() || len(it.Labels) != 0 || len(it.Reviewers) != 0 || len(it.Teams) != 0 || it.User != "" || it.Draft {
		t.Fatalf("%+v", it)
	}
	// null user (a deleted account) and null reviewer arrays are tolerated.
	body = fmt.Sprintf(`[{"number":8,"user":null,"requested_reviewers":null,"requested_teams":null,"labels":null,"head":{"sha":"%040x"}}]`, 8)
	if _, err := ParsePullList([]byte(body)); err != nil {
		t.Fatal(err)
	}
}

func TestParsePullListRefusesMalformedItems(t *testing.T) {
	good := fmt.Sprintf(`"head":{"sha":"%040x"}`, 1)
	for name, body := range map[string]string{
		"not an array": `{"number":1}`,
		"no number":    `[{` + good + `}]`,
		"bad sha":      `[{"number":1,"head":{"sha":"XYZ"}}]`,
		"bad created":  `[{"number":1,"created_at":"yesterday",` + good + `}]`,
	} {
		_, err := ParsePullList([]byte(body))
		var we *WatchError
		if err == nil || !asWatchError(err, &we) || we.Code != CodeMalformed {
			t.Errorf("%s: err %v", name, err)
		}
	}
}

func asWatchError(err error, target **WatchError) bool {
	we, ok := err.(*WatchError)
	*target = we
	return ok
}

// PullItem is the stored form of watch state.json: the new decoder must not widen it.
func TestPullItemStoredFormIsUnchanged(t *testing.T) {
	b, err := json.Marshal(PullItem{})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	want := "base_ref base_sha draft head_ref head_sha labels number title user"
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	if got := sortedJoin(keys); got != want {
		t.Fatalf("PullItem keys %q, want %q", got, want)
	}
}

func sortedJoin(s []string) string {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
	return strings.Join(s, " ")
}

// The three guard modes: each admits only its own set (Spec 036 amendment of Spec 022 FR-005).
func TestGuardModesAdmitTheirOwnShapes(t *testing.T) {
	cases := []struct {
		path               string
		oneShot, watch, pu bool
	}{
		{"repos/a/b/pulls?state=open&per_page=25&page=1", false, true, true},
		{"rate_limit", false, true, true},
		{"repos/a/b/commits/" + strings.Repeat("a", 40), false, true, true},
		{"repos/a/b/pulls/3", true, true, true},
		{"repos/a/b/branches/main", false, true, true},
		{"repos/a/b/pulls?state=closed&per_page=25&page=1", false, false, false},
		{"repos/a/b/issues", false, false, false},
		// The search shape belongs to the owner-mode work package; until then no mode reaches it.
		{"search/issues?q=user%3Aa+is%3Apr+is%3Aopen+archived%3Afalse&per_page=100&page=1&sort=created&order=asc", false, false, false},
	}
	for _, c := range cases {
		for mode, want := range map[guardMode]bool{guardOneShot: c.oneShot, guardWatch: c.watch, guardPulls: c.pu} {
			g := guardedTransport{next: roundTripFunc(func(r *http.Request) (*http.Response, error) { return response(200, `{}`), nil }), host: "api.github.com", prefix: "/", mode: mode}
			req, _ := http.NewRequest("GET", "https://api.github.com/"+c.path, nil)
			if got := g.allowed(req); got != want {
				t.Errorf("mode %d path %s: allowed=%v want %v", mode, c.path, got, want)
			}
		}
	}
}

func TestGuardPullsModeAllowsEscapedBranchLikeWatch(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/a/b/branches/feat%2Fx", nil)
	for mode, want := range map[guardMode]bool{guardOneShot: false, guardWatch: true, guardPulls: true} {
		g := guardedTransport{host: "api.github.com", prefix: "/", mode: mode}
		if got := g.allowed(req); got != want {
			t.Errorf("mode %d: %v", mode, got)
		}
	}
}

// The legacy bool field still selects watch mode, so existing constructors keep working.
func TestGuardLegacyWatchFieldMeansWatchMode(t *testing.T) {
	g := guardedTransport{host: "api.github.com", prefix: "/", watch: true}
	req, _ := http.NewRequest("GET", "https://api.github.com/rate_limit", nil)
	if !g.allowed(req) {
		t.Fatal("watch:true must admit watch shapes")
	}
}

func TestListOpenPullItemsFollowsLinkUnconditionally(t *testing.T) {
	f := newFakeForge(t)
	threePages(f, [3]string{"a", "a", "a"})
	r := f.reader("github.com")
	items, err := r.ListOpenPullItems(context.Background(), "a/b", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 60 || items[59].Number != 60 || items[0].User != "u" {
		t.Fatalf("%d items", len(items))
	}
	for i, e := range f.etags {
		if e != "" {
			t.Fatalf("request %d sent If-None-Match %q", i, e)
		}
	}
	if c := r.TakeCalls()[KindList]; c.Requests != 3 || c.NotModified != 0 {
		t.Fatalf("%+v", c)
	}
}

func TestListOpenPullItemsHalvesAndRestarts(t *testing.T) {
	f := newFakeForge(t)
	f.serve(listURI(25, 1), `"x1"`, pageBody(1, 25), linkNext(25, 2))
	f.handlers[listURI(25, 2)] = func(*http.Request) *http.Response {
		return forgeResponse(200, strings.Repeat("x", maxBytes+1), nil)
	}
	f.serve(listURI(12, 1), `"y1"`, pageBody(1, 12), linkNext(12, 2))
	f.serve(listURI(12, 2), `"y2"`, pageBody(13, 24), linkNext(12, 3))
	f.serve(listURI(12, 3), `"y3"`, pageBody(25, 30), nil)
	items, err := f.reader("github.com").ListOpenPullItems(context.Background(), "a/b", 2000)
	if err != nil || len(items) != 30 {
		t.Fatalf("%v %d", err, len(items))
	}
	want := []string{listURI(25, 1), listURI(25, 2), listURI(12, 1), listURI(12, 2), listURI(12, 3)}
	if strings.Join(f.log, "|") != strings.Join(want, "|") {
		t.Fatalf("%v", f.log)
	}
}

func TestListOpenPullItemsBoundsAndErrors(t *testing.T) {
	f := newFakeForge(t)
	threePages(f, [3]string{"a", "a", "a"})
	r := f.reader("github.com")
	var we *WatchError
	if _, err := r.ListOpenPullItems(context.Background(), "a/b", 59); !errors.As(err, &we) || we.Code != CodeTooManyPRs {
		t.Fatalf("too many: %v", err)
	}
	if _, err := r.ListOpenPullItems(context.Background(), "a/b", 0); !errors.As(err, &we) || we.Code != CodeMalformed {
		t.Fatalf("bound: %v", err)
	}
	g := newFakeForge(t)
	g.serve(listURI(25, 1), `"x"`, `{"not":"an array"}`, nil)
	if _, err := g.reader("github.com").ListOpenPullItems(context.Background(), "a/b", 100); !errors.As(err, &we) || we.Code != CodeMalformed {
		t.Fatalf("malformed: %v", err)
	}
	h := newFakeForge(t)
	h.serve(listURI(25, 1), `"x"`, pageBody(1, 25), map[string]string{"Link": `<https://api.github.com/x?page=3>; rel="next"`})
	if _, err := h.reader("github.com").ListOpenPullItems(context.Background(), "a/b", 100); !errors.As(err, &we) || we.Code != CodeMalformed {
		t.Fatalf("link: %v", err)
	}
}
