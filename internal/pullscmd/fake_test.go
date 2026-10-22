// SPDX-License-Identifier: EUPL-1.2
package pullscmd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

var t0 = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

func sha(seed string) string { return strings.Repeat(seed, 40)[:40] }

// fakeForge serves canned lists, pulls/N and checks and records every call. It embeds a nil
// watchcmd.Reader on purpose: any method the cycle must not use panics.
type fakeForge struct {
	watchcmd.Reader
	mu      sync.Mutex
	lists   map[string][]hosting.PullListItem
	listErr map[string]error
	details map[string]string // repo#n -> body
	runs    map[string]string // sha -> check_runs body
	stats   map[string]string // sha -> statuses body
	errAt   map[string]error  // "detail/repo#n", "runs/<sha4>"
	calls   []string
	etags   []string
}

func newForge() *fakeForge {
	return &fakeForge{lists: map[string][]hosting.PullListItem{}, listErr: map[string]error{}, details: map[string]string{}, runs: map[string]string{}, stats: map[string]string{}, errAt: map[string]error{}}
}

func (f *fakeForge) note(call string, etag string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	if etag != "" {
		f.etags = append(f.etags, call+" "+etag)
	}
	return f.errAt[call]
}

func (f *fakeForge) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeForge) ListOpenPullItems(_ context.Context, repo string, _ int) ([]hosting.PullListItem, error) {
	if err := f.note("list/"+repo, ""); err != nil {
		return nil, err
	}
	if err := f.listErr[repo]; err != nil {
		return nil, err
	}
	return f.lists[repo], nil
}

func (f *fakeForge) PullDetail(_ context.Context, repo string, n int, etag string) (hosting.WatchResponse, error) {
	key := fmt.Sprintf("detail/%s#%d", repo, n)
	if err := f.note(key, etag); err != nil {
		return hosting.WatchResponse{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return hosting.WatchResponse{Status: 200, Body: []byte(f.details[fmt.Sprintf("%s#%d", repo, n)])}, nil
}

func (f *fakeForge) CheckRuns(_ context.Context, _ string, s string, _ int, etag string) (hosting.WatchResponse, error) {
	if err := f.note("runs/"+s[:4], etag); err != nil {
		return hosting.WatchResponse{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	body := f.runs[s]
	if body == "" {
		body = `{"total_count":0,"check_runs":[]}`
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(body)}, nil
}

func (f *fakeForge) Statuses(_ context.Context, _ string, s string, _ int, etag string) (hosting.WatchResponse, error) {
	if err := f.note("stats/"+s[:4], etag); err != nil {
		return hosting.WatchResponse{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	body := f.stats[s]
	if body == "" {
		body = `[]`
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(body)}, nil
}

// pr is one canned pull request with its detail and check class.
type pr struct {
	repo   string
	n      int
	user   string
	utype  string
	draft  bool
	age    time.Duration // created_at = t0 - age; negative age is the future; zeroTime wins
	noTime bool
	head   string
	// detail
	mergeable  string // JSON: true|false|null
	state      string
	rebaseable string // JSON: true|false|null
	detailHead string // "" means the listed head
	class      string // none|pending|green|red
}

func okPR(repo string, n int) pr {
	return pr{repo: repo, n: n, user: "alice", utype: "User", age: 24 * time.Hour, head: sha(fmt.Sprintf("%x", n%15+1)), mergeable: "true", state: "clean", rebaseable: "true", class: "green"}
}

func (f *fakeForge) add(p pr) {
	it := hosting.PullListItem{Number: p.n, Title: fmt.Sprintf("title %d", p.n), User: p.user, UserType: p.utype, Draft: p.draft, HTMLURL: fmt.Sprintf("https://github.com/%s/pull/%d", p.repo, p.n), HeadSHA: p.head, HeadRef: "feat", BaseRef: "main"}
	if !p.noTime {
		it.CreatedAt = t0.Add(-p.age)
	}
	f.lists[p.repo] = append(f.lists[p.repo], it)
	dh := p.detailHead
	if dh == "" {
		dh = p.head
	}
	f.details[fmt.Sprintf("%s#%d", p.repo, p.n)] = fmt.Sprintf(`{"mergeable":%s,"mergeable_state":%q,"rebaseable":%s,"head":{"sha":%q,"repo":{"full_name":%q}},"base":{"repo":{"full_name":%q}}}`, p.mergeable, p.state, p.rebaseable, dh, p.repo, p.repo)
	switch p.class {
	case "red":
		f.runs[p.head] = `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"failure","output":{}}]}`
	case "pending":
		f.runs[p.head] = `{"total_count":1,"check_runs":[{"name":"ci","status":"in_progress","conclusion":null,"output":{}}]}`
	case "green":
		f.runs[p.head] = `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{}}]}`
	}
}
