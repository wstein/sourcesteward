// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseHTTPOnlyFixedGETsAndOnlineAuth(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "gh")
	log := filepath.Join(d, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CALLS\"\nif [ \"$1\" = --version ];then echo 'gh version 2.102.0';else [ \"$1 $2\" = 'auth token' ] || exit 8;echo fake-secret;fi\n"
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_PATH", exe)
	t.Setenv("CALLS", log)
	sha := strings.Repeat("a", 40)
	count := 0
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		count++
		if req.Method != "GET" || req.URL.Host != "api.github.com" || !releaseEndpoint(strings.TrimPrefix(req.URL.RequestURI(), "/")) {
			t.Fatal(req.URL)
		}
		if req.Header.Get("Authorization") != "token fake-secret" && req.Header.Get("Authorization") != "Bearer fake-secret" {
			t.Fatal("missing fixture authentication")
		}
		switch {
		case strings.Contains(req.URL.Path, "/runs"):
			return response(200, `{"total_count":0,"workflow_runs":[]}`), nil
		case strings.HasSuffix(req.URL.Path, "/main"):
			return response(200, `{"ref":"refs/heads/main","object":{"type":"commit","sha":"`+sha+`"}}`), nil
		default:
			return response(200, `[]`), nil
		}
	})
	got, err := ReadReleaseWithTransport(context.Background(), t.TempDir(), Repository{Host: "github.com", Owner: "o", Name: "r"}, sha, []string{"ci.yml"}, rt)
	if err != nil || count != 5 || len(got.Network) != 5 {
		t.Fatalf("count=%d %+v %v", count, got, err)
	}
	raw, err := os.ReadFile(log)
	if err != nil || string(raw) != "--version\nauth token --hostname github.com\n" {
		t.Fatalf("%s %v", raw, err)
	}
}
func TestReleaseHTTPRefusesCompressionRedirectAndOverflow(t *testing.T) {
	for _, kind := range []string{"compressed", "redirect", "overflow"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			client, err := newSDKClient("github.com", "fake-secret", t.TempDir(), roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				switch kind {
				case "compressed":
					r := response(200, "[]")
					r.Header.Set("Content-Encoding", "gzip")
					return r, nil
				case "redirect":
					r := response(302, "")
					r.Header.Set("Location", "https://evil.invalid/secret")
					return r, nil
				default:
					return response(200, strings.Repeat("x", maxBytes+1)), nil
				}
			}))
			if err != nil {
				t.Fatal(err)
			}
			api := &ghAPI{host: "github.com", client: client}
			_, err = api.get(context.Background(), "repos/o/r/releases?per_page=100&page=1")
			if err == nil || calls != 1 || strings.Contains(err.Error(), "fake-secret") {
				t.Fatalf("calls %d %v", calls, err)
			}
		})
	}
}
func TestReleaseJSONDuplicateAndIncompleteWorkflowRefused(t *testing.T) {
	for _, raw := range []string{`{"total_count":1,"workflow_runs":[]}`, `{"total_count":0,"total_count":1,"workflow_runs":[]}`, `{"total_count":0,"workflow_runs":null}`} {
		_, err := readRelease(context.Background(), apiReaderFunc(func(context.Context, string) ([]byte, error) { return []byte(raw), nil }), "o/r", strings.Repeat("a", 40), []string{"ci.yml"})
		if err == nil {
			t.Fatal(raw)
		}
	}
}
