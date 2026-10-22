// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixtureTarget returns a repository root and points GH_PATH at a fake gh that serves a fixture
// token, so reader constructors never read real credentials.
func fixtureTarget(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	target := filepath.Join(d, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(d, "gh")
	body := "#!/bin/sh\nif [ \"$1\" = \"--version\" ];then echo 'gh version 2.102.0';exit 0;fi\necho testsecret\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_PATH", script)
	return target
}

func blockingReader(t *testing.T) *WatchReader {
	t.Helper()
	client, err := newSDKClientMode("github.com", "fixturetoken", t.TempDir(), roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() }), true)
	if err != nil {
		t.Fatal(err)
	}
	return newWatchReader(&ghAPI{client: client, host: "github.com"})
}

func TestRequestTimeoutDefaultsTo30SecondsForEveryConstructor(t *testing.T) {
	if DefaultRequestTimeout != 30*time.Second || StatusRequestTimeout != 15*time.Second {
		t.Fatalf("%v %v", DefaultRequestTimeout, StatusRequestTimeout)
	}
	if got := blockingReader(t).RequestTimeout(); got != 30*time.Second {
		t.Fatalf("watch reader: %v", got)
	}
	f := newFakeForge(t)
	if got := f.reader("github.com").RequestTimeout(); got != 30*time.Second {
		t.Fatalf("%v", got)
	}
	ctx, target := context.Background(), fixtureTarget(t)
	for name, mk := range map[string]func() (*WatchReader, func(), error){
		"watch": func() (*WatchReader, func(), error) {
			return NewWatchReaderWithTransport(ctx, target, "github.com", roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("x") }))
		},
		"pulls": func() (*WatchReader, func(), error) {
			return NewPullsReaderWithTransport(ctx, target, "github.com", roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("x") }))
		},
	} {
		r, cleanup, err := mk()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r.RequestTimeout() != 30*time.Second {
			t.Errorf("%s: %v", name, r.RequestTimeout())
		}
		cleanup()
	}
}

func TestSetRequestTimeoutAppliesToTheNextRequestsOnly(t *testing.T) {
	r := blockingReader(t)
	if err := r.SetRequestTimeout(StatusRequestTimeout); err != nil || r.RequestTimeout() != 15*time.Second {
		t.Fatalf("%v %v", err, r.RequestTimeout())
	}
	if err := r.SetRequestTimeout(20 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := r.PullDetail(context.Background(), "a/b", 7, "")
	assertWatchCode(t, err, CodeNetwork)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Fatalf("the configured deadline must cut the request: %v", err)
	}
}

func TestSetRequestTimeoutRefusesNonPositiveAndKeepsTheOldValue(t *testing.T) {
	r := blockingReader(t)
	for _, d := range []time.Duration{0, -time.Second} {
		if err := r.SetRequestTimeout(d); err == nil {
			t.Errorf("%v accepted", d)
		}
	}
	if r.RequestTimeout() != DefaultRequestTimeout {
		t.Fatalf("%v", r.RequestTimeout())
	}
}
