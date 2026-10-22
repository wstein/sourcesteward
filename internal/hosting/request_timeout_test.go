// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

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
	ctx := context.Background()
	for name, mk := range map[string]func() (*WatchReader, func(), error){
		"watch": func() (*WatchReader, func(), error) {
			return NewWatchReaderWithTransport(ctx, t.TempDir(), "github.com", roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("x") }))
		},
		"pulls": func() (*WatchReader, func(), error) {
			return NewPullsReaderWithTransport(ctx, t.TempDir(), "github.com", roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("x") }))
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
