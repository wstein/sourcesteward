// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

// WorkflowDispatchInputs preserves the exact five-input workflow contract.
type WorkflowDispatchInputs struct {
	Mode    string `json:"mode"`
	Product string `json:"product"`
	Commit  string `json:"commit"`
	Tag     string `json:"tag"`
	Version string `json:"version"`
}

// DispatchResponse counts admitted transport attempts, including failed writes.
// A failed write may have started a workflow; it must never be retried here.
type DispatchResponse struct{ Requests, StatusCode int }

// ErrDispatchAuth distinguishes pre-request authentication failure from a write
// whose remote outcome may be unknown. No process output is exposed.
var ErrDispatchAuth = errors.New("workflow dispatch authentication unavailable")

var dispatchRoute = regexp.MustCompile(`\Arepos/[A-Za-z0-9_][A-Za-z0-9_.-]*/[A-Za-z0-9_][A-Za-z0-9_.-]*/actions/workflows/[A-Za-z0-9_.-]+\.ya?ml/dispatches\z`)
var dispatchProductID = regexp.MustCompile(`\A[a-z0-9][a-z0-9-]{0,31}\z`)

func fixedDispatchEndpoint(endpoint string) bool { return dispatchRoute.MatchString(endpoint) }

// DispatchWorkflow is the one-write adapter. Callers must already have fresh
// release gates, matching plan identity and explicit task-context build approval.
// It never resolves a repository or substitutes a caller-supplied URL/method.
func DispatchWorkflow(ctx context.Context, target string, repo Repository, workflow string, inputs WorkflowDispatchInputs) (DispatchResponse, error) {
	return DispatchWorkflowWithTransport(ctx, target, repo, workflow, inputs, nil)
}

// DispatchWorkflowWithTransport uses the same boundary with in-process fixtures.
// Preparation preserves the existing external neutral gh directory and selected-
// host token acquisition; invalid selections and canceled calls launch nothing.
func DispatchWorkflowWithTransport(parent context.Context, target string, repo Repository, workflow string, inputs WorkflowDispatchInputs, rt http.RoundTripper) (DispatchResponse, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return DispatchResponse{}, err
	}
	endpoint := "repos/" + repo.Owner + "/" + repo.Name + "/actions/workflows/" + workflow + "/dispatches"
	if !validHost(repo.Host) || !validRepo(repo.Owner+"/"+repo.Name) || !fixedDispatchEndpoint(endpoint) || !validDispatchInputs(inputs) {
		return DispatchResponse{}, errors.New("invalid workflow dispatch selection")
	}
	body, err := json.Marshal(struct {
		Ref    string                 `json:"ref"`
		Inputs WorkflowDispatchInputs `json:"inputs"`
	}{"main", inputs})
	if err != nil {
		return DispatchResponse{}, errors.New("cannot encode workflow dispatch")
	}
	api, cleanup, err := prepareGH(ctx, target, repo.Host)
	if err != nil {
		if ctx.Err() != nil {
			return DispatchResponse{}, ctx.Err()
		}
		return DispatchResponse{}, fmt.Errorf("%w: cannot prepare selected-host credentials", ErrDispatchAuth)
	}
	defer cleanup()
	token, err := api.token(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return DispatchResponse{}, ctx.Err()
		}
		return DispatchResponse{}, ErrDispatchAuth
	}
	if rt == nil {
		owned := newHTTPTransport()
		defer owned.CloseIdleConnections()
		rt = owned
	}
	destination, prefix := apiDestination(repo.Host)
	guard := &dispatchGuard{next: rt, host: destination, prefix: prefix, endpoint: endpoint, body: body}
	client, err := newSDKClientWithGuard(repo.Host, token, api.dir, guard)
	if err != nil {
		return DispatchResponse{}, errors.New("cannot initialize fixed dispatch transport")
	}
	return (&dispatchAPI{client: client, host: repo.Host, guard: guard}).postFixed(ctx, endpoint, body)
}

func validDispatchInputs(inputs WorkflowDispatchInputs) bool {
	text := func(s string) bool {
		return s != "" && utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
	}
	if !text(inputs.Version) || (inputs.Product != "" && !dispatchProductID.MatchString(inputs.Product)) {
		return false
	}
	switch inputs.Mode {
	case "commit":
		return releaseCommit(inputs.Commit) && inputs.Tag == ""
	case "tag":
		return inputs.Commit == "" && text(inputs.Tag)
	}
	return false
}

type dispatchAPI struct {
	client *http.Client
	host   string
	guard  *dispatchGuard
}

func (api *dispatchAPI) postFixed(ctx context.Context, endpoint string, body []byte) (DispatchResponse, error) {
	result := func() DispatchResponse {
		r := DispatchResponse{StatusCode: int(api.guard.status.Load())}
		if api.guard.attempted.Load() {
			r.Requests = 1
		}
		return r
	}
	if !fixedDispatchEndpoint(endpoint) || endpoint != api.guard.endpoint || !bytes.Equal(body, api.guard.body) {
		return result(), errors.New("invalid fixed dispatch request")
	}
	if err := ctx.Err(); err != nil {
		return result(), err
	}
	host, prefix := apiDestination(api.host)
	u := url.URL{Scheme: "https", Host: host, Path: prefix + endpoint}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return result(), errors.New("cannot prepare workflow dispatch")
	}
	req.GetBody = nil // The POST is deliberately non-replayable, including HTTP redirects.
	req.Header.Set("Content-Type", "application/json")
	res, err := api.client.Do(req)
	if res != nil && res.Body != nil {
		res.Body.Close()
	}
	if ctx.Err() != nil {
		return result(), ctx.Err()
	}
	if err != nil || res == nil || res.StatusCode != http.StatusNoContent {
		if status := result().StatusCode; status != 0 {
			return result(), fmt.Errorf("workflow dispatch failed (HTTP %d)", status)
		}
		return result(), errors.New("workflow dispatch request failed")
	}
	return result(), nil
}

// dispatchGuard is an operation-scoped capability for one exact endpoint/body.
// Read-only SDK clients keep their existing separate guardedTransport unchanged.
type dispatchGuard struct {
	next                   http.RoundTripper
	host, prefix, endpoint string
	body                   []byte
	attempted              atomic.Bool
	status                 atomic.Int64
}

func (g *dispatchGuard) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || !fixedDispatchEndpoint(g.endpoint) || req.Method != http.MethodPost || req.URL.Scheme != "https" || req.URL.Host != g.host || (req.Host != "" && req.Host != g.host) || req.URL.Path != g.prefix+g.endpoint || req.URL.RawPath != "" || req.URL.RawQuery != "" || req.URL.ForceQuery || req.URL.User != nil || req.URL.Fragment != "" || req.URL.Opaque != "" || req.Body == nil || req.ContentLength != int64(len(g.body)) || len(req.TransferEncoding) != 0 || req.Header.Get("Content-Type") != "application/json" {
		return nil, errors.New("hosting request refused outside fixed dispatch destination")
	}
	raw, err := io.ReadAll(io.LimitReader(req.Body, int64(len(g.body))+1))
	req.Body.Close()
	if err != nil || !bytes.Equal(raw, g.body) {
		return nil, errors.New("hosting dispatch body refused")
	}
	if err = req.Context().Err(); err != nil {
		return nil, err
	}
	if !g.attempted.CompareAndSwap(false, true) {
		return nil, errors.New("workflow dispatch retry refused")
	}
	req.Body = io.NopCloser(bytes.NewReader(g.body))
	req.GetBody = nil
	res, err := g.next.RoundTrip(req)
	if res != nil {
		g.status.Store(int64(res.StatusCode))
		if res.Body != nil {
			res.Body.Close()
		}
	}
	if err != nil {
		return nil, errors.New("hosting dispatch transport failed")
	}
	if res == nil {
		return nil, errors.New("hosting dispatch response unavailable")
	}
	for _, value := range res.Header.Values("Content-Encoding") {
		for _, encoding := range strings.Split(value, ",") {
			if encoding = strings.ToLower(strings.TrimSpace(encoding)); encoding != "" && encoding != "identity" {
				return nil, errors.New("compressed hosting responses are unsupported")
			}
		}
	}
	// Status-only: never read the remote body, feed it through SDK sanitizers or
	// retain redirect/header values that could cause another request or leak data.
	return &http.Response{StatusCode: res.StatusCode, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
}
