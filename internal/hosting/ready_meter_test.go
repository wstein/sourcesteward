// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type readyPartialBody struct{ raw string }

func (b *readyPartialBody) Read(p []byte) (int, error) {
	n := copy(p, b.raw)
	b.raw = b.raw[n:]
	return n, errors.New("private-token /private/path")
}
func (*readyPartialBody) Close() error { return nil }

func TestReadyMeterCountsBodyReadsBeforeGuard(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      io.ReadCloser
		wantBytes int
		outcome   readyOutcome
	}{
		{"ok", 200, io.NopCloser(strings.NewReader(`{"x":1}`)), 7, readyOK},
		{"partial-error", 200, &readyPartialBody{raw: `{"x":1}`}, 7, readyTransport},
		{"overflow-probe", 200, io.NopCloser(strings.NewReader(strings.Repeat("x", maxBytes+1))), maxBytes + 1, readyLimit},
		{"denied-body-unread", 403, io.NopCloser(strings.NewReader("private-token /private/path")), 0, readyDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budget := &readyReadBudget{started: time.Now()}
			rt := readyMeterTransport{next: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: tc.body}, nil
			}), budget: budget}
			client, err := newSDKClient("github.com", "fixture-token", t.TempDir(), rt)
			if err != nil {
				t.Fatal(err)
			}
			s := newReadySources(context.Background(), &ghAPI{host: "github.com", client: client}, Repository{Host: "github.com", Owner: "o", Name: "r"})
			s.budget = budget
			src := s.source("repository", "")
			s.get(&src, "repos/o/r")
			if budget.bodyBytes != tc.wantBytes || budget.requests != 1 || src.Outcome != tc.outcome {
				t.Fatalf("bytes=%d requests=%d outcome=%s", budget.bodyBytes, budget.requests, src.Outcome)
			}
		})
	}
}

func TestReadyMeterCountsAllCollectionSamples(t *testing.T) {
	f := newCollectionFixture(t)
	got := f.collect()
	if !got.Complete || got.policy.SHA256() != got.Policy.SHA256 {
		t.Fatal("parsed immutable governing policy unavailable")
	}
	if f.s.budget.requests != 15 || f.s.budget.pages != 5 || f.s.budget.records != 5 {
		t.Fatalf("actual sample usage: %+v", f.s.budget)
	}
	f = newCollectionFixture(t)
	f.change = func(path string, n int, raw string) (int, string) {
		if path == collectionOpen {
			return 403, `{"private":"token"}`
		}
		return 200, raw
	}
	f.collect()
	if f.s.budget.pages != 5 || f.s.budget.records != 3 {
		t.Fatalf("denied pages were not observed separately from decoded rows: %+v", f.s.budget)
	}
}

type readyCloseProbe struct {
	io.ReadCloser
	closes int
}

func (b *readyCloseProbe) Close() error { b.closes++; return b.ReadCloser.Close() }

func TestReadyMeterPreservesCloseAndLateCancel(t *testing.T) {
	for _, kind := range []string{"success", "transport-error", "declared-oversize", "late-cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := &readyCloseProbe{ReadCloser: io.NopCloser(strings.NewReader(`{"x":1}`))}
			budget := &readyReadBudget{started: time.Now()}
			rt := readyMeterTransport{next: roundTripFunc(func(*http.Request) (*http.Response, error) {
				res := &http.Response{StatusCode: 200, Header: http.Header{}, Body: body}
				if kind == "transport-error" {
					return res, errors.New("private-token")
				}
				if kind == "declared-oversize" {
					res.ContentLength = maxBytes + 1
				}
				if kind == "late-cancel" {
					cancel()
				}
				return res, nil
			}), budget: budget}
			client, err := newSDKClient("github.com", "fixture-token", t.TempDir(), rt)
			if err != nil {
				t.Fatal(err)
			}
			s := newReadySources(ctx, &ghAPI{host: "github.com", client: client}, Repository{Host: "github.com", Owner: "o", Name: "r"})
			s.budget = budget
			src := s.source("repository", "")
			s.get(&src, "repos/o/r")
			want := map[string]readyOutcome{"success": readyOK, "transport-error": readyTransport, "declared-oversize": readyLimit, "late-cancel": readyCanceled}[kind]
			wantBytes := 0
			if kind == "success" {
				wantBytes = 7
			}
			if src.Outcome != want || body.closes != 1 || budget.bodyBytes != wantBytes {
				t.Fatalf("outcome=%s closes=%d bytes=%d", src.Outcome, body.closes, budget.bodyBytes)
			}
		})
	}
}

func TestReadyMeterCountsCheckResamples(t *testing.T) {
	s := readyFixture(t, func(*http.Request) (int, string) {
		return 200, `{"total_count":1,"check_runs":[{"id":1,"app":{"id":2},"name":"ci","status":"completed","head_sha":"` + readySHA + `","conclusion":"success","output":{"summary":null}}]}`
	})
	for i := 1; i <= 2; i++ {
		if !s.checks(readySHA).Source.Complete {
			t.Fatal("fixture incomplete")
		}
		if s.budget.pages != i || s.budget.records != i || s.budget.ciRows != 1 {
			t.Fatalf("resample usage %+v", s.budget)
		}
	}
}
