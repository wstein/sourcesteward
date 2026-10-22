// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"net/http"
	"testing"
)

func serveRepo(f *fakeForge, body string) *WatchReader {
	f.handlers["repos/o/r"] = func(*http.Request) *http.Response { return response(200, body) }
	return f.reader("github.com")
}

func TestRepoInfoVisibility(t *testing.T) {
	for _, tc := range []struct{ name, perms, want string }{
		{"admin", `{"admin":true,"maintain":true,"push":true,"triage":true,"pull":true}`, DraftsWriteAccess},
		{"maintain only", `{"admin":false,"maintain":true,"push":false,"triage":true,"pull":true}`, DraftsWriteAccess},
		{"push only", `{"admin":false,"maintain":false,"push":true,"triage":false,"pull":true}`, DraftsWriteAccess},
		{"admin only", `{"admin":true,"maintain":false,"push":false}`, DraftsWriteAccess},
		{"read only", `{"admin":false,"maintain":false,"push":false,"triage":false,"pull":true}`, DraftsNoWriteAccess},
		{"triage is no write access", `{"admin":false,"maintain":false,"push":false,"triage":true,"pull":true}`, DraftsNoWriteAccess},
		{"absent", ``, DraftsUnknown},
		{"null", `null`, DraftsUnknown},
		{"empty", `{}`, DraftsUnknown},
		{"incomplete false", `{"admin":false,"pull":true}`, DraftsUnknown},
		{"incomplete but write", `{"push":true}`, DraftsWriteAccess},
	} {
		doc := `{"default_branch":"main","html_url":"https://github.com/o/r"`
		if tc.perms != "" {
			doc += `,"permissions":` + tc.perms
		}
		info, err := serveRepo(newFakeForge(t), doc+`}`).RepoInfo(context.Background(), "o/r")
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := info.DraftsVisibility(); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
		if info.DefaultBranch != "main" || info.HTMLURL != "https://github.com/o/r" {
			t.Errorf("%s: %+v", tc.name, info)
		}
	}
}

func TestRepoInfoKeepsAllFivePermissions(t *testing.T) {
	info, err := serveRepo(newFakeForge(t), `{"default_branch":"main","html_url":"u","permissions":{"admin":true,"maintain":false,"push":true,"triage":false,"pull":true}}`).RepoInfo(context.Background(), "o/r")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"admin": true, "maintain": false, "push": true, "triage": false, "pull": true} {
		got := map[string]*bool{"admin": info.Admin, "maintain": info.Maintain, "push": info.Push, "triage": info.Triage, "pull": info.Pull}[name]
		if got == nil || *got != want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

func TestRepoInfoRefusesUnusableResponses(t *testing.T) {
	for _, body := range map[string]string{
		"no default branch": `{"html_url":"u"}`,
		"no url":            `{"default_branch":"main"}`,
		"not an object":     `[]`,
		"duplicate key":     `{"default_branch":"a","default_branch":"b","html_url":"u"}`,
		"bad permission":    `{"default_branch":"main","html_url":"u","permissions":{"push":"yes"}}`,
	} {
		_, err := serveRepo(newFakeForge(t), body).RepoInfo(context.Background(), "o/r")
		assertWatchCode(t, err, CodeMalformed)
	}
}

func TestRepoInfoRefusesInvalidRepoWithoutRequest(t *testing.T) {
	f := newFakeForge(t)
	_, err := f.reader("github.com").RepoInfo(context.Background(), "o/../r")
	assertWatchCode(t, err, CodeMalformed)
	if len(f.log) != 0 {
		t.Fatalf("sent %v", f.log)
	}
}

func TestRepoInfoClassifiesFailures(t *testing.T) {
	f := newFakeForge(t)
	f.handlers["repos/o/r"] = func(*http.Request) *http.Response { return response(404, `{"message":"Not Found"}`) }
	_, err := f.reader("github.com").RepoInfo(context.Background(), "o/r")
	if we := assertWatchCode(t, err, CodeForbidden); we.Status != 404 {
		t.Fatalf("%+v", we)
	}
}
