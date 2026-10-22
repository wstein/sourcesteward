// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseIssueListDecodesTheReportedFields(t *testing.T) {
	body := `[{"number":41,"title":"token leaks","html_url":"https://github.com/o/r/issues/41","created_at":"2026-10-20T08:00:00Z","comments":3,
	"user":{"login":"alice","type":"User"},"labels":[{"name":"security"},{"name":"Bug"}],"assignees":[{"login":"Wstein"},{"login":"bob"}],"state":"open"},
	{"number":12,"title":"docs","html_url":"","user":{"login":"dependabot[bot]","type":"Bot"},"labels":[],"assignees":[]}]`
	items, dropped, err := ParseIssueList([]byte(body))
	if err != nil || dropped != 0 || len(items) != 2 {
		t.Fatalf("%+v %d %v", items, dropped, err)
	}
	three := 3
	want := IssueListItem{Number: 41, Title: "token leaks", Author: "alice", AuthorType: "User", HTMLURL: "https://github.com/o/r/issues/41", CreatedAt: time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC), Comments: &three, Labels: []string{"security", "Bug"}, Assignees: []string{"Wstein", "bob"}}
	if !reflect.DeepEqual(items[0], want) {
		t.Fatalf("%+v", items[0])
	}
	second := items[1]
	if second.Author != "dependabot[bot]" || second.AuthorType != "Bot" || !second.CreatedAt.IsZero() || second.Comments != nil || len(second.Labels) != 0 || len(second.Assignees) != 0 {
		t.Fatalf("%+v", second)
	}
	empty, dropped, err := ParseIssueList([]byte(`[]`))
	if err != nil || dropped != 0 || empty == nil || len(empty) != 0 {
		t.Fatalf("%v %d %v", empty, dropped, err)
	}
}

func TestParseIssueListDropsEveryEntryWithAPullRequestKey(t *testing.T) {
	body := `[{"number":1,"pull_request":{"url":"x"}},{"number":2,"pull_request":null},{"number":3},
	{"number":"not-a-number","pull_request":{}},{"pull_request":null},{"number":4,"pull_requests":{}},{"number":5,"Pull_Request":{}}]`
	// encoding/json matches keys case-insensitively, so a case variant is a marker too: dropping is the safe side.
	items, dropped, err := ParseIssueList([]byte(body))
	if err != nil || dropped != 5 {
		t.Fatalf("%d %v", dropped, err)
	}
	var got []int
	for _, it := range items {
		got = append(got, it.Number)
	}
	if !reflect.DeepEqual(got, []int{3, 4}) {
		t.Fatalf("a key that only looks similar is not a pull request marker, and dropped entries are not validated: %v", got)
	}
}

func TestParseIssueListLabelsAndAssigneesAreData(t *testing.T) {
	body := `[{"number":1,"labels":null,"assignees":null},
	{"number":2,"labels":[{"name":"a"},{},{"name":null},{"name":5},{"name":["x"]},"plain",null,{"name":""},{"name":"b","color":"x"}],"assignees":[{"login":"x"},{},{"login":3},null,"s",{"login":""}]}]`
	items, _, err := ParseIssueList([]byte(body))
	if err != nil || len(items) != 2 {
		t.Fatalf("%+v %v", items, err)
	}
	if len(items[0].Labels) != 0 || len(items[0].Assignees) != 0 {
		t.Fatalf("%+v", items[0])
	}
	if !reflect.DeepEqual(items[1].Labels, []string{"a", "b"}) || !reflect.DeepEqual(items[1].Assignees, []string{"x"}) {
		t.Fatalf("only entries with a string name or login count: %+v", items[1])
	}
	for _, bad := range []string{`{"number":1,"labels":{}}`, `{"number":1,"labels":"x"}`, `{"number":1,"assignees":5}`} {
		if _, _, err := ParseIssueList([]byte("[" + bad + "]")); err == nil {
			t.Errorf("%s admitted", bad)
		}
	}
}

func TestParseIssueListNullUserIsTheGhostNotABot(t *testing.T) {
	for _, entry := range []string{`{"number":1,"user":null}`, `{"number":1,"user":{}}`, `{"number":1,"user":{"login":"","type":"Bot"}}`, `{"number":1}`} {
		items, _, err := ParseIssueList([]byte("[" + entry + "]"))
		if err != nil || len(items) != 1 || items[0].Author != GhostLogin || items[0].AuthorType != "" {
			t.Errorf("%s: %+v %v", entry, items, err)
		}
	}
}

func TestParseIssueListRefusesMalformedEntries(t *testing.T) {
	for name, body := range map[string]string{
		"number zero":       `[{"number":0}]`,
		"number negative":   `[{"number":-1}]`,
		"number missing":    `[{"title":"x"}]`,
		"number string":     `[{"number":"7"}]`,
		"number float":      `[{"number":1.5}]`,
		"number huge":       `[{"number":1e30}]`,
		"number beyond int": `[{"number":9223372036854775808}]`,
		"number null":       `[{"number":null}]`,
		"title number":      `[{"number":1,"title":5}]`,
		"user string":       `[{"number":1,"user":"alice"}]`,
		"comments negative": `[{"number":1,"comments":-1}]`,
		"comments string":   `[{"number":1,"comments":"3"}]`,
		"created_at bad":    `[{"number":1,"created_at":"yesterday"}]`,
		"created_at number": `[{"number":1,"created_at":5}]`,
		"entry null":        `[null]`,
		"entry number":      `[3]`,
		"entry array":       `[[]]`,
		"object":            `{"number":1}`,
		"null":              `null`,
		"empty":             ``,
		"garbage":           `[{`,
	} {
		_, _, err := ParseIssueList([]byte(body))
		if err == nil {
			t.Errorf("%s admitted", name)
			continue
		}
		assertWatchCode(t, err, CodeMalformed)
	}
}

func TestParseIssueListAccepts100AndRefuses101Entries(t *testing.T) {
	page := func(n int) []byte {
		entries := make([]string, n)
		for i := range entries {
			entries[i] = fmt.Sprintf(`{"number":%d}`, i+1)
		}
		return []byte("[" + strings.Join(entries, ",") + "]")
	}
	items, _, err := ParseIssueList(page(100))
	if err != nil || len(items) != 100 {
		t.Fatalf("%d %v", len(items), err)
	}
	_, _, err = ParseIssueList(page(101))
	assertWatchCode(t, err, CodePageTooLarge)
	// Pull requests count against the page: a page of 101 entries is too large whatever they are.
	pulls := strings.Repeat(`{"number":1,"pull_request":{}},`, 100) + `{"number":2}`
	_, _, err = ParseIssueList([]byte("[" + pulls + "]"))
	assertWatchCode(t, err, CodePageTooLarge)
}

func TestParseViewer(t *testing.T) {
	for _, ok := range []string{"wstein", "A-b-9", strings.Repeat("a", 39)} {
		got, err := ParseViewer([]byte(`{"login":"` + ok + `","id":1,"type":"User"}`))
		if err != nil || got != ok {
			t.Errorf("%q: %q %v", ok, got, err)
		}
	}
	for name, body := range map[string]string{
		"missing":    `{}`,
		"null":       `{"login":null}`,
		"empty":      `{"login":""}`,
		"number":     `{"login":5}`,
		"40":         `{"login":"` + strings.Repeat("a", 40) + `"}`,
		"space":      `{"login":"a b"}`,
		"newline":    `{"login":"a\nb"}`,
		"escape":     `{"login":"\u001b[31m"}`,
		"bidi":       `{"login":"a‮b"}`,
		"unicode":    `{"login":"ä"}`,
		"bot suffix": `{"login":"x[bot]"}`,
		"hyphen":     `{"login":"-a"}`,
		"double":     `{"login":"a--b"}`,
		"slash":      `{"login":"a/b"}`,
		"array":      `[]`,
		"null doc":   `null`,
		"empty doc":  ``,
	} {
		if got, err := ParseViewer([]byte(body)); err == nil {
			t.Errorf("%s admitted as %q", name, got)
		} else {
			assertWatchCode(t, err, CodeMalformed)
		}
	}
}
