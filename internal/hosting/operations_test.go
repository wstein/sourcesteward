// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"github.com/wstein/sourcesteward/internal/inspection"
	"strings"
	"testing"
)

func identity() inspection.Result {
	return inspection.Result{SchemaVersion: 1, Status: "ok", Backend: "git", InputPath: "/tmp/example", Evidence: []inspection.Evidence{}, Diagnostics: []inspection.Diagnostic{}, Repository: &inspection.Repository{GitDir: "/tmp/example/.git", CommonDir: "/tmp/example/.git"}}
}
func prJSON(sha string) []byte {
	return []byte(`{"number":3,"title":"title","html_url":"https://github.com/a/b/pull/3","state":"open","draft":false,"head":{"sha":"` + sha + `","ref":"topic","repo":{"full_name":"fork/b"}},"base":{"sha":"` + strings.Repeat("b", 40) + `","ref":"main","repo":{"full_name":"a/b"}}}`)
}
func fixtureDeps(api func(context.Context, string) ([]byte, error)) dependencies {
	return dependencies{remotes: func(context.Context, string) inspection.RemoteResult {
		return inspection.RemoteResult{Identity: identity(), Status: "ok", Remotes: []inspection.Remote{{Name: "origin", FetchURLs: []string{"https://github.com/a/b.git"}, PushURLs: []string{}}}}
	}, api: func(context.Context, string, string) (apiReader, func(), error) {
		return apiReaderFunc(api), func() {}, nil
	}}
}
func TestReadOnlyRepositoryHasNoAPI(t *testing.T) {
	d := fixtureDeps(func(context.Context, string) ([]byte, error) { t.Fatal("repo network"); return nil, nil })
	r := inspect(context.Background(), ".", Options{Kind: "repo"}, d)
	if r.Status != "ok" || r.Boundary.Network || r.Repository.Name != "b" {
		t.Fatalf("%+v", r)
	}
	r = inspect(context.Background(), ".", Options{Kind: "repo", RequireJJ: true}, d)
	if r.Status != "error" || r.Backend != "git" {
		t.Fatalf("%+v", r)
	}
}
func TestCIExactHeadAndResample(t *testing.T) {
	sha := strings.Repeat("a", 40)
	var paths []string
	d := fixtureDeps(func(_ context.Context, p string) ([]byte, error) {
		paths = append(paths, p)
		switch {
		case p == "repos/a/b/pulls/3":
			return prJSON(sha), nil
		case strings.Contains(p, "check-runs"):
			return []byte(`{"total_count":1,"check_runs":[{"id":1,"name":"ci","status":"in_progress","conclusion":null,"head_sha":"` + sha + `","html_url":"https://example.test/check"}]}`), nil
		default:
			return []byte(`[{"id":2,"context":"legacy","state":"success","description":null,"target_url":null}]`), nil
		}
	})
	r := inspect(context.Background(), ".", Options{Kind: "ci", Number: 3}, d)
	if r.Status != "ok" || len(r.Checks) != 1 || len(r.Statuses) != 1 || !r.Boundary.Complete || len(paths) != 4 {
		t.Fatalf("%+v %v", r, paths)
	}
	for _, p := range paths {
		if strings.Contains(p, "commits/") && !strings.Contains(p, sha) {
			t.Fatal(p)
		}
	}
	count := 0
	d.api = func(context.Context, string, string) (apiReader, func(), error) {
		return apiReaderFunc(func(_ context.Context, p string) ([]byte, error) {
			if strings.Contains(p, "pulls/") {
				count++
				if count > 1 {
					return prJSON(strings.Repeat("c", 40)), nil
				}
				return prJSON(sha), nil
			}
			if strings.Contains(p, "check-runs") {
				return []byte(`{"total_count":0,"check_runs":[]}`), nil
			}
			return []byte(`[]`), nil
		}), func() {}, nil
	}
	r = inspect(context.Background(), ".", Options{Kind: "ci", Number: 3}, d)
	if r.Status != "error" || r.Boundary.Complete {
		t.Fatalf("movedhead %+v", r)
	}
}
func TestAPIRefusesMissingAndForeignIdentity(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for _, raw := range [][]byte{[]byte(`{}`), []byte(`null`), []byte(strings.Replace(string(prJSON(sha)), `"a/b"`, `"else/repo"`, 1)), []byte(strings.Replace(string(prJSON(sha)), `"repo":{"full_name":"fork/b"}`, `"repo":null`, 1))} {
		d := fixtureDeps(func(context.Context, string) ([]byte, error) { return raw, nil })
		r := inspect(context.Background(), ".", Options{Kind: "pr", Number: 3}, d)
		if r.Status != "error" {
			b, _ := json.Marshal(r)
			t.Fatalf("accepted %s", b)
		}
	}
}
func TestCIIncompleteEvidenceRetainsOnlyObservedRecords(t *testing.T) {
	sha := strings.Repeat("a", 40)
	check := `{"id":1,"name":"ci","status":"queued","conclusion":null,"head_sha":"` + sha + `","html_url":""}`
	for name, response := range map[string]string{"missing": "{}", "null": "null", "wronghead": `{"total_count":1,"check_runs":[` + strings.Replace(check, sha, strings.Repeat("b", 40), 1) + `]}`, "duplicate": `{"total_count":2,"check_runs":[` + check + `,` + check + `]}`, "overflowtotal": `{"total_count":1001,"check_runs":[]}`, "prematureempty": `{"total_count":1,"check_runs":[]}`} {
		t.Run(name, func(t *testing.T) {
			d := fixtureDeps(func(_ context.Context, p string) ([]byte, error) {
				if strings.Contains(p, "pulls/") {
					return prJSON(sha), nil
				}
				return []byte(response), nil
			})
			r := inspect(context.Background(), ".", Options{Kind: "ci", Number: 3}, d)
			if r.Status != "error" || r.Boundary.Complete || len(r.Diagnostics) == 0 {
				t.Fatalf("%+v", r)
			}
		})
	}
	d := fixtureDeps(func(_ context.Context, p string) ([]byte, error) {
		switch {
		case strings.Contains(p, "pulls/"):
			return prJSON(sha), nil
		case strings.Contains(p, "check-runs"):
			return []byte(`{"total_count":1,"check_runs":[` + check + `]}`), nil
		default:
			return []byte(`null`), nil
		}
	})
	r := inspect(context.Background(), ".", Options{Kind: "ci", Number: 3}, d)
	if r.Status != "error" || len(r.Checks) != 1 || len(r.Statuses) != 0 {
		t.Fatalf("partial evidence %+v", r)
	}
}
func TestOversizedResultAndMalformedUTF8Refuse(t *testing.T) {
	sha := strings.Repeat("a", 40)
	large := strings.Replace(string(prJSON(sha)), `"title":"title"`, `"title":"`+strings.Repeat("x", maxBytes)+`"`, 1)
	d := fixtureDeps(func(context.Context, string) ([]byte, error) { return []byte(large), nil })
	r := inspect(context.Background(), ".", Options{Kind: "pr", Number: 3}, d)
	encoded, _ := json.Marshal(r)
	if r.Status != "error" || len(encoded) > maxBytes {
		t.Fatalf("unbounded result %d", len(encoded))
	}
	raw := append([]byte{}, prJSON(sha)...)
	raw = []byte(strings.Replace(string(raw), `"title":"title"`, "\"title\":\"\xff\"", 1))
	d = fixtureDeps(func(context.Context, string) ([]byte, error) { return raw, nil })
	r = inspect(context.Background(), ".", Options{Kind: "pr", Number: 3}, d)
	if r.Status != "error" {
		t.Fatal("malformed UTF-8 silently replaced")
	}
}
