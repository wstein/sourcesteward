// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func dispatchInputsFixture() WorkflowDispatchInputs {
	return WorkflowDispatchInputs{Mode: "commit", Commit: strings.Repeat("a", 40), Version: "v0.1.0"}
}
func fakeDispatchGH(t *testing.T, authOK bool) (string, string) {
	t.Helper()
	d := t.TempDir()
	script := filepath.Join(d, "gh")
	marker := filepath.Join(d, "calls")
	body := `#!/bin/sh
printf '%s\n' "$*" >> '` + marker + `'
[ "$PWD" != "$DISPATCH_TARGET" ] || exit 3
if [ "$1" = "--version" ]; then echo 'gh version 2.102.0'; exit 0; fi
[ "$1 $2 $3 $4" = "auth token --hostname github.com" ] || exit 4
`
	if authOK {
		body += "echo fixturetoken\n"
	} else {
		body += "echo secret-auth-body >&2\nexit 1\n"
	}
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	t.Setenv("GH_PATH", script)
	t.Setenv("DISPATCH_TARGET", target)
	return target, marker
}

func TestDispatchPOSTExactOneRequest(t *testing.T) {
	target, marker := fakeDispatchGH(t, true)
	calls := 0
	result, err := DispatchWorkflowWithTransport(context.Background(), target, Repository{Host: "github.com", Owner: "owner", Name: "repo"}, ".release.yml", dispatchInputsFixture(), roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.String() != "https://api.github.com/repos/owner/repo/actions/workflows/.release.yml/dispatches" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "token fixturetoken" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("missing selected auth/JSON headers: %v", r.Header)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err = json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		inputs, ok := got["inputs"].(map[string]any)
		if len(got) != 2 || got["ref"] != "main" || !ok || len(inputs) != 5 || inputs["mode"] != "commit" || inputs["commit"] != strings.Repeat("a", 40) || inputs["tag"] != "" || inputs["product"] != "" || inputs["version"] != "v0.1.0" {
			t.Errorf("wrong dispatch body: %s", raw)
		}
		return response(204, ""), nil
	}))
	if err != nil || result.Requests != 1 || result.StatusCode != 204 || calls != 1 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
	}
	raw, err := os.ReadFile(marker)
	if err != nil || string(raw) != "--version\nauth token --hostname github.com\n" {
		t.Fatalf("auth process calls: %s, %v", raw, err)
	}
}

type dispatchBodySpy struct {
	reads  int
	closed bool
}

func (b *dispatchBodySpy) Read([]byte) (int, error) {
	b.reads++
	return 0, errors.New("secret-response-read-error")
}
func (b *dispatchBodySpy) Close() error { b.closed = true; return nil }

func TestDispatchPOSTStatusOnlyNoRedirectOrRetry(t *testing.T) {
	for _, status := range []int{200, 201, 204, 301, 302, 303, 307, 308, 400, 401, 403, 404, 422, 429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			target, _ := fakeDispatchGH(t, true)
			calls := 0
			spy := &dispatchBodySpy{}
			result, err := DispatchWorkflowWithTransport(context.Background(), target, Repository{Host: "github.com", Owner: "owner", Name: "repo"}, "release.yaml", dispatchInputsFixture(), roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"https://evil.test/secret"}, "Content-Type": []string{"application/json"}, "Retry-After": []string{"0"}}, Body: spy, ContentLength: 1 << 40}, nil
			}))
			if calls != 1 || result.Requests != 1 || result.StatusCode != status || (err == nil) != (status == 204) || !spy.closed || spy.reads != 0 {
				t.Fatalf("result=%+v calls=%d reads=%d closed=%v err=%v", result, calls, spy.reads, spy.closed, err)
			}
			if err != nil && (strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), strconv.Itoa(status))) {
				t.Fatalf("status missing or body leaked: %v", err)
			}
		})
	}
}
func TestDispatchPOSTFailureAttemptsAreTruthful(t *testing.T) {
	for _, which := range []string{"auth", "canceled", "transport", "partial", "nil-response"} {
		t.Run(which, func(t *testing.T) {
			target, marker := fakeDispatchGH(t, which != "auth")
			calls := 0
			spy := &dispatchBodySpy{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if which == "canceled" {
				cancel()
			}
			result, err := DispatchWorkflowWithTransport(ctx, target, Repository{Host: "github.com", Owner: "owner", Name: "repo"}, "release.yml", dispatchInputsFixture(), roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if which == "nil-response" {
					return nil, nil
				}
				if which == "partial" {
					return &http.Response{StatusCode: 503, Body: spy}, errors.New("secret-transport")
				}
				return nil, errors.New("secret-transport")
			}))
			want := 1
			if which == "auth" || which == "canceled" {
				want = 0
			}
			if err == nil || strings.Contains(err.Error(), "secret") || calls != want || result.Requests != want {
				t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
			}
			if which == "auth" && !errors.Is(err, ErrDispatchAuth) {
				t.Fatalf("pre-request authentication classification lost: %v", err)
			}
			if which == "partial" && !spy.closed {
				t.Fatal("partial error body not closed")
			}
			if which == "canceled" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost: %v", err)
				}
				if _, err = os.Stat(marker); !os.IsNotExist(err) {
					t.Fatalf("canceled request executed gh: %v", err)
				}
			}
		})
	}
}

func TestDispatchPOSTInvalidSelectionBeforeAuth(t *testing.T) {
	for _, which := range []string{"repo-path", "host-url", "workflow-path", "workflow-query", "workflow-traversal", "mode", "conflicting-inputs"} {
		t.Run(which, func(t *testing.T) {
			target, marker := fakeDispatchGH(t, true)
			repo := Repository{Host: "github.com", Owner: "owner", Name: "repo"}
			workflow := "release.yml"
			inputs := dispatchInputsFixture()
			switch which {
			case "repo-path":
				repo.Name = "repo/other"
			case "host-url":
				repo.Host = "https://github.com"
			case "workflow-path":
				workflow = ".github/workflows/release.yml"
			case "workflow-query":
				workflow = "release.yml?x=1"
			case "workflow-traversal":
				workflow = "../release.yml"
			case "mode":
				inputs.Mode = "publish"
			case "conflicting-inputs":
				inputs.Tag = "v1"
			}
			calls := 0
			result, err := DispatchWorkflowWithTransport(context.Background(), target, repo, workflow, inputs, roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(204, ""), nil }))
			if err == nil || result.Requests != 0 || calls != 0 {
				t.Fatalf("invalid selection reached HTTP: %+v %d %v", result, calls, err)
			}
			if _, err = os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("invalid selection executed gh: %v", err)
			}
		})
	}
}

func TestDispatchPOSTGuardRejectsOtherMethodsPathsAndBodies(t *testing.T) {
	endpoint := "repos/owner/repo/actions/workflows/.release.yml/dispatches"
	body := []byte(`{"ref":"main","inputs":{"mode":"commit","product":"","commit":"` + strings.Repeat("a", 40) + `","tag":"","version":"v0.1.0"}}`)
	mutations := map[string]func(*http.Request){
		"GET":            func(r *http.Request) { r.Method = "GET" },
		"PUT":            func(r *http.Request) { r.Method = "PUT" },
		"DELETE":         func(r *http.Request) { r.Method = "DELETE" },
		"other workflow": func(r *http.Request) { r.URL.Path = strings.Replace(r.URL.Path, ".release.yml", "other.yml", 1) },
		"read endpoint":  func(r *http.Request) { r.URL.Path = "/repos/owner/repo/pulls/1" },
		"other owner":    func(r *http.Request) { r.URL.Path = strings.Replace(r.URL.Path, "/owner/", "/other/", 1) },
		"traversal":      func(r *http.Request) { r.URL.Path = "/repos/owner/repo/actions/workflows/../dispatches" },
		"query":          func(r *http.Request) { r.URL.RawQuery = "other=1" },
		"empty query":    func(r *http.Request) { r.URL.ForceQuery = true },
		"fragment":       func(r *http.Request) { r.URL.Fragment = "secret" },
		"encoded path":   func(r *http.Request) { r.URL.RawPath = r.URL.Path },
		"userinfo":       func(r *http.Request) { r.URL.User = url.User("secret") },
		"scheme":         func(r *http.Request) { r.URL.Scheme = "http" },
		"host":           func(r *http.Request) { r.URL.Host = "evil.test" },
		"host override":  func(r *http.Request) { r.Host = "evil.test" },
		"body": func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader(strings.Replace(string(body), "main", "evil", 1)))
		},
		"missing body": func(r *http.Request) { r.Body = nil },
		"length":       func(r *http.Request) { r.ContentLength++ },
		"encoding":     func(r *http.Request) { r.TransferEncoding = []string{"chunked"} },
		"media type":   func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			calls := 0
			guard := &dispatchGuard{host: "api.github.com", prefix: "/", endpoint: endpoint, body: body, next: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(204, ""), nil })}
			req, _ := http.NewRequest("POST", "https://api.github.com/"+endpoint, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			mutate(req)
			if _, err := guard.RoundTrip(req); err == nil || calls != 0 || guard.attempted.Load() {
				t.Fatalf("non-fixed request admitted: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestDispatchPOSTFixedEndpointAndOneAttemptCapability(t *testing.T) {
	endpoint := "repos/owner/repo/actions/workflows/release.yml/dispatches"
	body := []byte(`{"ref":"main"}`)
	calls := 0
	guard := &dispatchGuard{host: "api.github.com", prefix: "/", endpoint: endpoint, body: body, next: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(204, ""), nil })}
	client, err := newSDKClientWithGuard("github.com", "fixturetoken", t.TempDir(), guard)
	if err != nil {
		t.Fatal(err)
	}
	api := &dispatchAPI{client: client, host: "github.com", guard: guard}
	for _, path := range []string{"repos/owner/repo/pulls/1", endpoint + "?x=1", strings.Replace(endpoint, "release.yml", "other.yml", 1), "https://evil.test/" + endpoint} {
		result, err := api.postFixed(context.Background(), path, body)
		if err == nil || calls != 0 || result.Requests != 0 {
			t.Fatalf("invalid path reached transport: %s %+v %d %v", path, result, calls, err)
		}
	}
	for attempt := 1; attempt <= 2; attempt++ {
		result, err := api.postFixed(context.Background(), endpoint, body)
		if (err == nil) != (attempt == 1) || calls != 1 || result.Requests != 1 {
			t.Fatalf("attempt %d: %+v calls=%d err=%v", attempt, result, calls, err)
		}
	}
	readClient, err := newSDKClient("github.com", "fixturetoken", t.TempDir(), roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("read-only client sent POST")
		return response(204, ""), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", "https://api.github.com/"+endpoint, bytes.NewReader(body))
	if _, err = readClient.Do(req); err == nil {
		t.Fatal("existing read-only client gained dispatch capability")
	}
}

func TestDispatchPOSTCancellationAfterAttemptIsNotRetried(t *testing.T) {
	target, _ := fakeDispatchGH(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	result, err := DispatchWorkflowWithTransport(ctx, target, Repository{Host: "github.com", Owner: "owner", Name: "repo"}, "release.yml", dispatchInputsFixture(), roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; cancel(); return nil, ctx.Err() }))
	if !errors.Is(err, context.Canceled) || calls != 1 || result.Requests != 1 {
		t.Fatalf("attempted cancellation lost: %+v calls=%d err=%v", result, calls, err)
	}
}

func TestDispatchPOSTTagInputsArePreserved(t *testing.T) {
	target, _ := fakeDispatchGH(t, true)
	inputs := WorkflowDispatchInputs{Mode: "tag", Product: "tool", Tag: "tool/v1.0.0", Version: "v1.0.0"}
	calls := 0
	result, err := DispatchWorkflowWithTransport(context.Background(), target, Repository{Host: "github.com", Owner: "owner", Name: "repo"}, "release.yml", inputs, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var got struct {
			Ref    string
			Inputs WorkflowDispatchInputs
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got.Ref != "main" || got.Inputs != inputs {
			t.Errorf("tag selector changed: %+v %v", got, err)
		}
		return response(204, ""), nil
	}))
	if err != nil || result.Requests != 1 || calls != 1 {
		t.Fatalf("tag dispatch: %+v calls=%d err=%v", result, calls, err)
	}
}

func TestDispatchPOSTContentEncodingRefusesEveryNonIdentityToken(t *testing.T) {
	cases := []struct {
		name    string
		values  []string
		allowed bool
	}{
		{"absent", nil, true},
		{"empty", []string{""}, true},
		{"identity", []string{"identity"}, true},
		{"identity case and whitespace", []string{" \tIdEnTiTy\t "}, true},
		{"multiple identity headers and tokens", []string{"", " IDENTITY, identity ", "identity"}, true},
		{"gzip", []string{"gzip"}, false},
		{"brotli", []string{"br"}, false},
		{"compressed case and whitespace", []string{" \tGZiP\t "}, false},
		{"later header after empty", []string{"", "gzip"}, false},
		{"later header after identity", []string{"identity", "br"}, false},
		{"later comma token", []string{"identity, br"}, false},
		{"first comma token", []string{"gzip, identity"}, false},
		{"middle token after empty", []string{", gzip, identity"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target, _ := fakeDispatchGH(t, true)
			calls := 0
			spy := &dispatchBodySpy{}
			result, err := DispatchWorkflowWithTransport(context.Background(), target, Repository{Host: "github.com", Owner: "owner", Name: "repo"}, "release.yml", dispatchInputsFixture(), roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 204, Header: http.Header{"Content-Encoding": tc.values}, Body: spy}, nil
			}))
			if (err == nil) != tc.allowed || result.Requests != 1 || result.StatusCode != 204 || calls != 1 || spy.reads != 0 || !spy.closed {
				t.Fatalf("encoding %q: result=%+v calls=%d reads=%d closed=%v err=%v; allowed=%v", tc.values, result, calls, spy.reads, spy.closed, err, tc.allowed)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatalf("response read error leaked: %v", err)
			}
		})
	}
}
