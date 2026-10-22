// SPDX-License-Identifier: EUPL-1.2

package statusremote

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

// forge is one fake GitHub: canned pull request lists, pulls/N and checks, and a record of every
// read. It embeds a nil watchcmd.Reader on purpose: a method the cycle must not use panics. Nothing
// here reads a credential, a configuration file or the network.
type forge struct {
	watchcmd.Reader
	mu      sync.Mutex
	lists   map[string][]hosting.PullListItem
	details map[string]string   // repo#n -> body
	seq     map[string][]string // repo#n -> bodies of the next pulls/N reads, then details
	runs    map[string]string   // head sha -> check_runs body
	errAt   map[string]error    // "list/repo", "detail/repo#n"
	onRead  func(call string)   // called after a read was recorded
	calls   []string
}

func newForge() *forge {
	return &forge{lists: map[string][]hosting.PullListItem{}, details: map[string]string{}, seq: map[string][]string{}, runs: map[string]string{}, errAt: map[string]error{}}
}

func (f *forge) note(call string) error {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	err, hook := f.errAt[call], f.onRead
	f.mu.Unlock()
	if hook != nil {
		hook(call)
	}
	return err
}

func (f *forge) count(prefix string) int {
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

func (f *forge) ListOpenPullItems(_ context.Context, repo string, _ int) ([]hosting.PullListItem, error) {
	if err := f.note("list/" + repo); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists[repo], nil
}

func (f *forge) PullDetail(_ context.Context, repo string, n int, _ string) (hosting.WatchResponse, error) {
	if err := f.note(fmt.Sprintf("detail/%s#%d", repo, n)); err != nil {
		return hosting.WatchResponse{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	k := fmt.Sprintf("%s#%d", repo, n)
	if q := f.seq[k]; len(q) > 0 {
		f.seq[k] = q[1:]
		return hosting.WatchResponse{Status: 200, Body: []byte(q[0])}, nil
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(f.details[k])}, nil
}

func (f *forge) CheckRuns(_ context.Context, _ string, s string, _ int, _ string) (hosting.WatchResponse, error) {
	if err := f.note("runs/" + s[:4]); err != nil {
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

func (f *forge) Statuses(_ context.Context, _ string, s string, _ int, _ string) (hosting.WatchResponse, error) {
	if err := f.note("stats/" + s[:4]); err != nil {
		return hosting.WatchResponse{}, err
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(`[]`)}, nil
}

// DiscoverOwners makes the fake a pullscmd.Source for the parity test; the status cycle never searches.
func (f *forge) DiscoverOwners(context.Context, []string) []hosting.OwnerDiscovery { return nil }

// pr is one canned pull request.
type pr struct {
	repo      string
	n         int
	user      string
	utype     string
	draft     bool
	age       time.Duration
	mergeable string // JSON: true|false|null
	state     string
	class     string // none|pending|green|red
	head      string
}

func human(repo string, n int) pr {
	return pr{repo: repo, n: n, user: "alice", utype: "User", age: time.Duration(n) * time.Hour, mergeable: "true", state: "clean", class: "green", head: sha(fmt.Sprintf("%x", n%15+1))}
}

func (f *forge) add(p pr) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists[p.repo] = append(f.lists[p.repo], hosting.PullListItem{
		Number: p.n, Title: fmt.Sprintf("title %d", p.n), User: p.user, UserType: p.utype, Draft: p.draft,
		HTMLURL: fmt.Sprintf("https://github.com/%s/pull/%d", p.repo, p.n), HeadSHA: p.head, HeadRef: "feat", BaseRef: "main",
		CreatedAt: t0.Add(-p.age),
	})
	f.details[fmt.Sprintf("%s#%d", p.repo, p.n)] = detail(p, p.mergeable, p.state)
	conclusion := map[string]string{"red": `"status":"completed","conclusion":"failure"`, "pending": `"status":"in_progress","conclusion":null`, "green": `"status":"completed","conclusion":"success"`}[p.class]
	if conclusion != "" {
		f.runs[p.head] = `{"total_count":1,"check_runs":[{"name":"ci",` + conclusion + `,"output":{}}]}`
	}
}

func detail(p pr, mergeable, state string) string {
	return fmt.Sprintf(`{"mergeable":%s,"mergeable_state":%q,"rebaseable":true,"head":{"sha":%q,"repo":{"full_name":%q}},"base":{"repo":{"full_name":%q}}}`, mergeable, state, p.head, p.repo, p.repo)
}
