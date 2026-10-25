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
)

// WriteEffect is the closed set of effect classes the stack-publication write edge knows. Today
// there is exactly one: the Ready write (Spec 016 T020/T021, OQ-WRITES owner exception of
// 2026-10-25). Every other forge write of Spec 016 stays unavailable until its own reviewed
// allowlist entry exists.
type WriteEffect string

// EffectReady is the effect class of the one fixed markPullRequestReadyForReview mutation.
const EffectReady WriteEffect = "ready"

// StackWriteCapability says which write effect classes may be sent. The zero value allows nothing:
// the capability is disabled by default, and while it is off no process is started and no request
// is made.
type StackWriteCapability struct{ Ready bool }

// Allows reports whether the effect class is enabled.
func (c StackWriteCapability) Allows(e WriteEffect) bool { return e == EffectReady && c.Ready }

// Typed errors of the write edge. ErrStackWriteUnknown means a request was sent and its outcome
// is not proven: a lost reply, a GraphQL errors array, an unexpected status or a malformed body
// are never a proven failure, so the caller re-reads and reconciles instead of retrying.
var (
	ErrStackWriteDisabled = errors.New("stack write capability is disabled")
	ErrStackWriteInvalid  = errors.New("invalid stack write selection")
	ErrStackWriteAuth     = errors.New("stack write authentication unavailable")
	ErrStackWriteUnknown  = errors.New("stack write outcome unknown")
)

// StackWriteResponse counts admitted transport attempts and the HTTP status seen, if any.
type StackWriteResponse struct{ Requests, StatusCode int }

// readyMutationQuery is the one fixed query string. The pull request node id is the only variable.
// The mutation field and its input pullRequestId were measured through `gh pr ready` (research.md
// W5); the exact query text gh sent and the response shape were not recorded, so this text and the
// selected response field are pinned here and covered by tests, not copied from a measurement.
const readyMutationQuery = `mutation($id:ID!){markPullRequestReadyForReview(input:{pullRequestId:$id}){pullRequest{isDraft}}}`

// nodeIDGrammar is the pinned grammar of a pull request node id (conservative: URL-safe base64
// characters only). stackpublish.nodeIDGrammar repeats it; keep them equal.
var nodeIDGrammar = regexp.MustCompile(`\A[A-Za-z0-9_=-]{4,128}\z`)

func validNodeID(id string) bool { return nodeIDGrammar.MatchString(id) }

// readyMutationBody is the exact request body for a validated node id.
func readyMutationBody(nodeID string) []byte {
	body, err := json.Marshal(struct {
		Query     string            `json:"query"`
		Variables map[string]string `json:"variables"`
	}{readyMutationQuery, map[string]string{"id": nodeID}})
	if err != nil {
		return nil
	}
	return body
}

// MarkPullRequestReady sends the one fixed Ready mutation for the pull request node id, on
// github.com only. It makes no request and starts no process unless the capability is on, the
// node id is valid and the host and repository are valid. The write is never retried here. A nil
// error means HTTP 200 with a data object and no GraphQL errors; every other result after the
// request was sent is ErrStackWriteUnknown. The caller re-reads the pull request to confirm.
func MarkPullRequestReady(ctx context.Context, target string, repo Repository, nodeID string, capability StackWriteCapability) (StackWriteResponse, error) {
	return MarkPullRequestReadyWithTransport(ctx, target, repo, nodeID, capability, nil)
}

// MarkPullRequestReadyWithTransport is MarkPullRequestReady with an in-process HTTP fixture; a
// nil transport selects the production HTTPS transport.
func MarkPullRequestReadyWithTransport(parent context.Context, target string, repo Repository, nodeID string, capability StackWriteCapability, rt http.RoundTripper) (StackWriteResponse, error) {
	if !capability.Allows(EffectReady) {
		return StackWriteResponse{}, ErrStackWriteDisabled
	}
	if repo.Host != "github.com" || !validRepo(repo.Owner+"/"+repo.Name) || !validNodeID(nodeID) {
		return StackWriteResponse{}, ErrStackWriteInvalid
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return StackWriteResponse{}, err
	}
	body := readyMutationBody(nodeID)
	api, cleanup, err := prepareGH(ctx, target, repo.Host)
	if err != nil {
		if ctx.Err() != nil {
			return StackWriteResponse{}, ctx.Err()
		}
		return StackWriteResponse{}, fmt.Errorf("%w: cannot prepare selected-host credentials", ErrStackWriteAuth)
	}
	defer cleanup()
	token, err := api.token(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return StackWriteResponse{}, ctx.Err()
		}
		return StackWriteResponse{}, ErrStackWriteAuth
	}
	if rt == nil {
		owned := newHTTPTransport()
		defer owned.CloseIdleConnections()
		rt = owned
	}
	destination, prefix := apiDestination(repo.Host)
	guard := &stackWriteGuard{next: rt, host: destination, path: prefix + "graphql", body: body}
	client, err := newSDKClientWithGuard(repo.Host, token, api.dir, guard)
	if err != nil {
		return StackWriteResponse{}, errors.New("cannot initialize fixed stack write transport")
	}
	u := url.URL{Scheme: "https", Host: destination, Path: guard.path}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return StackWriteResponse{}, errors.New("cannot prepare stack write")
	}
	req.GetBody = nil // The POST is deliberately non-replayable, including HTTP redirects.
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	out := func() StackWriteResponse {
		r := StackWriteResponse{StatusCode: int(guard.status.Load())}
		if guard.attempted.Load() {
			r.Requests = 1
		}
		return r
	}
	if res != nil && res.Body != nil {
		defer res.Body.Close()
	}
	if !guard.attempted.Load() {
		if ctx.Err() != nil {
			return out(), ctx.Err()
		}
		return out(), errors.New("stack write request refused")
	}
	if err != nil || res == nil || res.StatusCode != http.StatusOK {
		return out(), fmt.Errorf("%w: no accepted reply", ErrStackWriteUnknown)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil || len(raw) > maxBytes {
		return out(), fmt.Errorf("%w: reply unreadable or over its bound", ErrStackWriteUnknown)
	}
	if !readyReplyAccepted(raw) {
		return out(), fmt.Errorf("%w: reply carries errors or an unexpected shape", ErrStackWriteUnknown)
	}
	return out(), nil
}

// readyReplyAccepted is true only for one JSON object with a data object holding a non-null
// markPullRequestReadyForReview and no errors key at all. The reply shape is unmeasured, so every
// other reply is unclear.
func readyReplyAccepted(raw []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var top map[string]json.RawMessage
	if dec.Decode(&top) != nil || top == nil {
		return false
	}
	if _, err := dec.Token(); err != io.EOF {
		return false
	}
	if _, has := top["errors"]; has {
		return false
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(top["data"], &data) != nil || data == nil {
		return false
	}
	var mutation map[string]json.RawMessage
	return json.Unmarshal(data["markPullRequestReadyForReview"], &mutation) == nil && mutation != nil
}

// stackWriteGuard is an operation-scoped capability for one exact POST to the GraphQL endpoint with
// one exact body. It admits one request; a second is refused. The read-only guards keep their own
// separate guardedTransport, which refuses every non-GET request and the GraphQL path.
type stackWriteGuard struct {
	next       http.RoundTripper
	host, path string
	body       []byte
	attempted  atomic.Bool
	status     atomic.Int64
}

func (g *stackWriteGuard) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || len(g.body) == 0 || req.Method != http.MethodPost || req.URL.Scheme != "https" || req.URL.Host != g.host || (req.Host != "" && req.Host != g.host) || req.URL.Path != g.path || req.URL.RawPath != "" || req.URL.RawQuery != "" || req.URL.ForceQuery || req.URL.User != nil || req.URL.Fragment != "" || req.URL.Opaque != "" || req.Body == nil || req.ContentLength != int64(len(g.body)) || len(req.TransferEncoding) != 0 || req.Header.Get("Content-Type") != "application/json" {
		return nil, errors.New("hosting request refused outside the fixed stack write destination")
	}
	raw, err := io.ReadAll(io.LimitReader(req.Body, int64(len(g.body))+1))
	req.Body.Close()
	if err != nil || !bytes.Equal(raw, g.body) {
		return nil, errors.New("hosting stack write body refused")
	}
	if err = req.Context().Err(); err != nil {
		return nil, err
	}
	if !g.attempted.CompareAndSwap(false, true) {
		return nil, errors.New("stack write retry refused")
	}
	req.Body = io.NopCloser(bytes.NewReader(g.body))
	req.GetBody = nil
	res, err := g.next.RoundTrip(req)
	if res != nil {
		g.status.Store(int64(res.StatusCode))
	}
	if err != nil {
		if res != nil && res.Body != nil {
			res.Body.Close()
		}
		return nil, errors.New("hosting stack write transport failed")
	}
	if res == nil || res.Body == nil {
		return nil, errors.New("hosting stack write response unavailable")
	}
	for _, value := range res.Header.Values("Content-Encoding") {
		for _, encoding := range strings.Split(value, ",") {
			if encoding = strings.ToLower(strings.TrimSpace(encoding)); encoding != "" && encoding != "identity" {
				res.Body.Close()
				return nil, errors.New("compressed hosting responses are unsupported")
			}
		}
	}
	if res.ContentLength > maxBytes {
		res.Body.Close()
		return nil, errBodyLimit
	}
	res.Body = &cappedBody{ReadCloser: res.Body, remaining: maxBytes}
	return res, nil
}
