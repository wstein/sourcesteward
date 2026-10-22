// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// Acceptance of Spec 035 (user stories US1 to US10) through the real command, an in-process fake forge
// and a fake clock; nothing touches the network. Unit-level proofs (the generated crash matrix, the
// transport allowlist, the restacker selection) live next to their packages; this file is the end to end
// run per story and validates every printed line against schemas/v1/watch-event.schema.json.

type accPR struct {
	N               int
	Head, Base, Ref string
	Title, Author   string
	Draft, Fork     bool
	Mergeable       bool
	Check           string // success, failure, pending
	Labels          []string
}

type accRepo struct {
	open   []accPR
	gone   map[int]string    // pulls/N bodies of PRs that left the open list
	commit map[string]string // commits/SHA bodies
	pulls  map[int][]string  // pulls/N/commits
	tips   map[string]string
}

type accForge struct {
	t       *testing.T
	mu      sync.Mutex
	repos   map[string]*accRepo
	rate    string
	status  int  // when non-zero every request answers this status
	offline bool // every request fails like a refused connection
	log     []string
	hits304 int
	cache   map[string]map[string]string
}

func newAccForge(t *testing.T, prs ...accPR) *accForge {
	f := &accForge{t: t, repos: map[string]*accRepo{}, rate: `{"resources":{"core":{"limit":5000,"remaining":4990,"reset":4102444800}}}`}
	f.add("o/r", prs...)
	return f
}

func (f *accForge) add(name string, prs ...accPR) *accRepo {
	r := &accRepo{open: prs, gone: map[int]string{}, commit: map[string]string{}, pulls: map[int][]string{}, tips: map[string]string{"main": strings.Repeat("a", 40)}}
	f.repos[name] = r
	return r
}

func (f *accForge) r() *accRepo { return f.repos["o/r"] }

func accPRn(n int) accPR {
	return accPR{N: n, Head: fmt.Sprintf("%040x", n*7919), Base: "main", Ref: "feat/" + strconv.Itoa(n), Title: "PR " + strconv.Itoa(n),
		Mergeable: true, Check: "success", Author: "alice"}
}

func (f *accForge) build(name string, r *accRepo) map[string]string {
	pre := "repos/" + name + "/"
	d := map[string]string{}
	var list []string
	bases := map[string]bool{}
	for _, p := range r.open {
		labels := "[]"
		if len(p.Labels) > 0 {
			var ls []string
			for _, l := range p.Labels {
				ls = append(ls, `{"name":"`+l+`"}`)
			}
			labels = "[" + strings.Join(ls, ",") + "]"
		}
		list = append(list, fmt.Sprintf(`{"number":%d,"title":%q,"user":{"login":%q},"labels":%s,"draft":%t,"head":{"sha":%q,"ref":%q},"base":{"sha":%q,"ref":%q}}`,
			p.N, p.Title, p.Author, labels, p.Draft, p.Head, p.Ref, strings.Repeat("b", 40), p.Base))
		bases[p.Base] = true
		headRepo := name
		if p.Fork {
			headRepo = "fork/r"
		}
		state := "clean"
		if !p.Mergeable {
			state = "dirty"
		}
		d[pre+"pulls/"+strconv.Itoa(p.N)] = fmt.Sprintf(`{"mergeable":%t,"mergeable_state":%q,"head":{"repo":{"full_name":%q}},"base":{"repo":{"full_name":%q}}}`, p.Mergeable, state, headRepo, name)
		run := `{"name":"ci","status":"completed","conclusion":"success","output":{}}`
		switch p.Check {
		case "failure":
			run = `{"name":"ci","status":"completed","conclusion":"failure","output":{"title":"--- FAIL: TestX","summary":"","text":""}}`
		case "pending":
			run = `{"name":"ci","status":"in_progress","conclusion":null,"output":{}}`
		}
		d[pre+"commits/"+p.Head+"/check-runs?per_page=100&page=1"] = `{"total_count":1,"check_runs":[` + run + `]}`
		d[pre+"commits/"+p.Head+"/statuses?per_page=100&page=1"] = `[]`
		if c, ok := r.pulls[p.N]; ok {
			var items []string
			for _, s := range c {
				items = append(items, `{"sha":"`+s+`"}`)
			}
			d[pre+"pulls/"+strconv.Itoa(p.N)+"/commits?per_page=100&page=1"] = "[" + strings.Join(items, ",") + "]"
		}
	}
	pages := (len(list) + 24) / 25
	if pages == 0 {
		pages = 1
	}
	for pg := 1; pg <= pages; pg++ {
		lo, hi := (pg-1)*25, min(pg*25, len(list))
		d[fmt.Sprintf("%spulls?state=open&per_page=25&page=%d", pre, pg)] = "[" + strings.Join(list[lo:hi], ",") + "]"
	}
	for b := range bases {
		tip := r.tips[b]
		if tip == "" {
			tip = strings.Repeat("c", 40)
		}
		d[pre+"branches/"+strings.ReplaceAll(b, "/", "%2F")] = `{"commit":{"sha":"` + tip + `"}}`
	}
	for n, body := range r.gone {
		d[pre+"pulls/"+strconv.Itoa(n)] = body
	}
	for sha, body := range r.commit {
		d[pre+"commits/"+sha] = body
	}
	return d
}

// invalidate drops the rendered documents; the runner calls it before every command.
func (f *accForge) invalidate() { f.mu.Lock(); f.cache = nil; f.mu.Unlock() }

func (f *accForge) doc(uri string) (string, bool) {
	if uri == "rate_limit" {
		return f.rate, true
	}
	if f.cache == nil {
		f.cache = map[string]map[string]string{}
		for name, r := range f.repos {
			f.cache[name] = f.build(name, r)
		}
	}
	parts := strings.SplitN(uri, "/", 4)
	if len(parts) < 4 || parts[0] != "repos" {
		return "", false
	}
	d, ok := f.cache[parts[1]+"/"+parts[2]]
	if !ok {
		return "", false
	}
	body, ok := d[uri]
	return body, ok
}

func (f *accForge) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method != http.MethodGet {
		f.t.Errorf("non-GET request reached the forge: %s %s", r.Method, r.URL)
	}
	uri := strings.TrimPrefix(r.URL.EscapedPath(), "/")
	if r.URL.RawQuery != "" {
		uri += "?" + r.URL.RawQuery
	}
	f.log = append(f.log, uri)
	if f.offline {
		return nil, errors.New("connection refused")
	}
	h := http.Header{"Content-Type": []string{"application/json"}}
	reply := func(code int, body string) (*http.Response, error) {
		return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	}
	if f.status != 0 {
		return reply(f.status, `{"message":"x"}`)
	}
	body, ok := f.doc(uri)
	if !ok {
		return reply(404, `{"message":"Not Found"}`)
	}
	if i := strings.Index(uri, "/pulls?state=open&per_page=25&page="); i > 0 {
		pg, _ := strconv.Atoi(uri[strings.LastIndex(uri, "=")+1:])
		if _, more := f.doc(fmt.Sprintf("%s/pulls?state=open&per_page=25&page=%d", uri[:i], pg+1)); more {
			h.Set("Link", fmt.Sprintf(`<https://api.github.com/%s/pulls?state=open&per_page=25&page=%d>; rel="next"`, uri[:i], pg+1))
		}
	}
	sum := sha1.Sum([]byte(body))
	etag := `"` + hex.EncodeToString(sum[:6]) + `"`
	h.Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		f.hits304++
		return reply(304, "")
	}
	return reply(200, body)
}

func (f *accForge) reads(prefix string) int {
	n := 0
	for _, l := range f.log {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

type accRun struct {
	t      *testing.T
	dir    string
	now    time.Time
	forge  *accForge
	sch    *jsonschema.Schema
	sleeps []time.Duration
	kinds  map[string]int
}

func newAccRun(t *testing.T, forge *accForge) *accRun {
	watchEnv(t, true)
	return &accRun{t: t, dir: filepath.Join(t.TempDir(), "state"), forge: forge, now: time.Now().UTC().Truncate(time.Second).Add(-time.Hour), sch: acceptanceSchema(t), kinds: map[string]int{}}
}

func acceptanceSchema(t *testing.T) *jsonschema.Schema {
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "v1", "watch-event.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	const id = "https://wstein.github.io/sourcesteward/schemas/v1/watch-event.schema.json"
	c := jsonschema.NewCompiler()
	if err := c.AddResource(id, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// exec runs `stw watch ARGS...` on the fake clock (advanced by a minute first). Every JSON line it
// prints is validated against the schema; the events are returned in order.
func (a *accRun) exec(args ...string) (status int, events []map[string]any, stdout, stderr string) {
	a.t.Helper()
	a.now = a.now.Add(time.Minute)
	a.forge.invalidate()
	a.forge.log = nil
	watchTransport = a.forge
	watchHook = func(p *watchcmd.Params) {
		p.Now = func() time.Time { return a.now }
		p.Jitter = func() time.Duration { return 0 }
		p.Sleep = func(ctx context.Context, d time.Duration) error {
			a.sleeps = append(a.sleeps, d)
			a.now = a.now.Add(d)
			return ctx.Err()
		}
	}
	a.t.Cleanup(func() { watchTransport, watchHook = nil, nil })
	var out, errOut bytes.Buffer
	status = Run(context.Background(), "test", append([]string{"watch"}, args...), &out, &errOut)
	if !strings.Contains(strings.Join(args, " "), "--human") && !strings.Contains(strings.Join(args, " "), "--status") {
		for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			if l == "" {
				continue
			}
			v, err := jsonschema.UnmarshalJSON(strings.NewReader(l))
			if err != nil {
				a.t.Fatalf("not JSON: %q", l)
			}
			if err := a.sch.Validate(v); err != nil {
				a.t.Fatalf("line violates the schema: %s\n%v", l, err)
			}
			m := v.(map[string]any)
			events = append(events, m)
			a.kinds[m["kind"].(string)]++
		}
	}
	return status, events, out.String(), errOut.String()
}

// once polls o/r once with the state directory of the run.
func (a *accRun) once(extra ...string) (int, []map[string]any) {
	a.t.Helper()
	st, ev, _, errOut := a.exec(append([]string{"--repo", "o/r", "--once", "--state-dir", a.dir}, extra...)...)
	if errOut != "" {
		a.t.Logf("stderr: %s", errOut)
	}
	return st, ev
}

func kindsOf(evs []map[string]any) string {
	var out []string
	for _, e := range evs {
		s := fmt.Sprintf("%s#%v", e["kind"], e["pr"])
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

func want(t *testing.T, evs []map[string]any, expect string) {
	t.Helper()
	if got := kindsOf(evs); got != expect {
		t.Fatalf("events:\n got %s\nwant %s", got, expect)
	}
}

// only keeps the events of one kind.
func only(evs []map[string]any, kind string) []map[string]any {
	var out []map[string]any
	for _, e := range evs {
		if e["kind"] == kind {
			out = append(out, e)
		}
	}
	return out
}

func details(e map[string]any) map[string]any { return e["details"].(map[string]any) }

func (a *accRun) journalLines() []string {
	raw, err := os.ReadFile(filepath.Join(a.dir, "journal.jsonl"))
	if err != nil {
		a.t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func TestWatchAcceptance(t *testing.T) {
	t.Run("US1 baseline is silent, then only transitions", func(t *testing.T) {
		forge := newAccForge(t, accPRn(1))
		a := newAccRun(t, forge)
		st, ev := a.once()
		if st != 0 || len(ev) != 0 {
			t.Fatalf("%d %v", st, ev)
		}
		for _, f := range []string{"state.json", "heartbeat", "lock"} {
			if _, err := os.Stat(filepath.Join(a.dir, f)); err != nil {
				t.Fatal(err)
			}
		}
		forge.r().open = append(forge.r().open, accPRn(2))
		_, ev = a.once()
		want(t, ev, "new-pr#2")
		if d := details(ev[0]); d["fork"] != false || d["head_ref"] != "feat/2" || d["author"] != "alice" {
			t.Fatalf("%v", d)
		}
		forge.r().open = append(forge.r().open, accPRn(3))
		st, ev = a.once("--baseline")
		if st != 0 || len(ev) != 0 {
			t.Fatalf("--baseline prints nothing: %d %v", st, ev)
		}
		if _, ev = a.once(); len(ev) != 0 {
			t.Fatalf("the baseline replaced the state: %v", ev)
		}
	})

	t.Run("US2 event kinds and the debounce", func(t *testing.T) {
		forge := newAccForge(t)
		r := forge.r()
		r.tips["dev"] = strings.Repeat("d", 40)
		p1, p2, p3, p4, p5, p6 := accPRn(1), accPRn(2), accPRn(3), accPRn(4), accPRn(5), accPRn(6)
		p2.Draft = true
		p4.Check = "pending"
		r.open = []accPR{p1, p2, p3, p4, p5, p6}
		a := newAccRun(t, forge)
		a.once()
		// stable fields transition at once
		p1.Head, p1.Mergeable, p1.Check = strings.Repeat("9", 40), false, "failure"
		p2.Draft = false
		p3.Base = "dev"
		p4.Check = "failure" // a flap: red for one cycle only
		r.open = []accPR{p1, p2, p3, p4, p5, p6}
		_, ev := a.once()
		want(t, ev, "head-changed#1 ready-state-changed#2 base-changed#3")
		p4.Check = "pending"
		r.open = []accPR{p1, p2, p3, p4, p5, p6}
		_, ev = a.once() // volatile fields: the second consecutive observation confirms
		if got := kindsOf(ev); !strings.Contains(got, "checks-red#1") || !strings.Contains(got, "mergeable-changed#1") || strings.Contains(got, "#4") {
			t.Fatalf("%s", got)
		}
		for _, e := range ev {
			if e["kind"] == "checks-red" {
				if d := details(e); d["shape"] != "real" || fmt.Sprint(d["failing_total"]) != "1" {
					t.Fatalf("%v", d)
				}
			}
		}
		p4.Check = "success"
		r.open = []accPR{p1, p2, p3, p4, p5, p6}
		a.once()
		_, ev = a.once()
		want(t, ev, "checks-green#4")
		// closed without merge, merged
		r.gone[5] = `{"state":"closed","merged":false,"base":{"ref":"main"},"head":{"sha":"` + p5.Head + `","ref":"feat/5"}}`
		mc := strings.Repeat("e", 40)
		r.gone[6] = `{"state":"closed","merged":true,"merge_commit_sha":"` + mc + `","merged_at":"2026-10-22T08:00:00Z","base":{"ref":"main"},"head":{"sha":"` + p6.Head + `","ref":"feat/6"}}`
		r.commit[mc] = `{"parents":[{"sha":"` + strings.Repeat("a", 40) + `"}]}`
		r.open = []accPR{p1, p2, p3, p4, accPRn(7)}
		_, ev = a.once()
		want(t, ev, "new-pr#7 closed#5 merged#6")
		if d := details(ev[2]); d["merge_commit"] != mc || d["method_hint"] != "rebase-or-squash" {
			t.Fatalf("%v", d)
		}
		// errors and recovery, then a repository set change
		forge.status = 500
		st, ev := a.once()
		if st != 1 {
			t.Fatalf("exit %d", st)
		}
		want(t, ev, "watch-error#<nil>")
		forge.status = 0
		a.now = a.now.Add(30 * time.Minute)
		_, ev = a.once()
		want(t, ev, "watch-recovered#<nil> checks-green#7")
		forge.add("o/s")
		a.exec("--repo", "o/r", "--repo", "o/s", "--once", "--state-dir", a.dir)
		for _, k := range []string{"new-pr", "head-changed", "checks-green", "checks-red", "mergeable-changed", "ready-state-changed", "base-changed", "merged", "closed", "watch-error", "watch-recovered"} {
			if a.kinds[k] == 0 {
				t.Errorf("no %s event in the run", k)
			}
		}
	})

	t.Run("US3 stacked rebase merge yields restack-needed", func(t *testing.T) {
		build := func(mergeCommit string, readable bool) (*accRun, *accForge) {
			forge := newAccForge(t)
			r := forge.r()
			r.tips["feat/1"] = strings.Repeat("f", 40)
			parent, child, rewritten, other := accPRn(1), accPRn(2), accPRn(3), accPRn(4)
			child.Base = "feat/1"
			r.open = []accPR{parent, child, rewritten, other}
			r.pulls[3] = []string{parent.Head, rewritten.Head}
			r.pulls[4] = []string{other.Head}
			a := newAccRun(t, forge)
			a.once()
			if readable {
				r.gone[1] = `{"state":"closed","merged":true,"merge_commit_sha":` + mergeCommit + `,"merged_at":"2026-10-22T08:00:00Z","base":{"ref":"main"},"head":{"sha":"` + parent.Head + `","ref":"feat/1"}}`
			}
			r.commit[strings.Repeat("e", 40)] = `{"parents":[{"sha":"` + strings.Repeat("a", 40) + `"}]}`
			r.open = []accPR{child, rewritten, other}
			return a, forge
		}
		mc := strings.Repeat("e", 40)
		a, forge := build(`"`+mc+`"`, true)
		_, ev := a.once()
		want(t, ev, "merged#1 restack-needed#2 restack-needed#3")
		d2, d3 := details(ev[1]), details(ev[2])
		if d2["reason"] != "child-of-merged-branch" || d3["reason"] != "contains-merged-commits" || d2["new_parent"] != mc || d3["new_parent"] != mc ||
			d3["old_parent"] != accPRn(1).Head || d3["branch"] != "feat/3" || fmt.Sprint(d3["merged_pr"]) != "1" {
			t.Fatalf("%v %v", d2, d3)
		}
		argv, _ := d3["argv"].([]any)
		if len(argv) != 10 || argv[6] != "--onto" || argv[7] != mc || argv[8] != accPRn(1).Head || argv[9] != "feat/3" {
			t.Fatalf("%v", argv)
		}
		if forge.reads("repos/o/r/pulls/2/commits") != 0 || forge.reads("repos/o/r/pulls/3/commits") == 0 {
			t.Fatalf("the child of the merged branch is found by its base name alone: %v", forge.log)
		}
		// an unreadable merge commit gives null new_parent and argv
		a, _ = build(`null`, true)
		_, ev = a.once()
		want(t, ev, "merged#1 restack-needed#2 restack-needed#3")
		if d := details(ev[1]); d["new_parent"] != nil || d["argv"] != nil {
			t.Fatalf("%v", d)
		}
		// with pulls/1 unreadable there is no merged and no restack-needed until it is readable
		a, _ = build(`null`, false)
		_, ev = a.once()
		want(t, ev, "watch-error#<nil>")
	})

	t.Run("US4 restart idempotence and ids", func(t *testing.T) {
		forge := newAccForge(t)
		r := forge.r()
		p1, p8 := accPRn(1), accPRn(8)
		r.open = []accPR{p1, p8}
		a := newAccRun(t, forge)
		a.once()
		a.once()
		if len(a.kinds) != 0 {
			t.Fatal("an unchanged poll prints nothing")
		}
		p1.Head = strings.Repeat("7", 40)
		r.open = []accPR{p1, p8}
		_, ev := a.once()
		p1.Head = strings.Repeat("6", 40)
		r.open = []accPR{p1, p8}
		_, ev2 := a.once()
		want(t, ev, "head-changed#1")
		want(t, ev2, "head-changed#1")
		if ev[0]["id"] == ev2[0]["id"] {
			t.Fatal("every force-push has its own id")
		}
		// closed, reopened, closed: two closed events with distinct ids
		r.gone[8] = `{"state":"closed","merged":false,"base":{"ref":"main"},"head":{"sha":"` + p8.Head + `","ref":"feat/8"}}`
		r.open = []accPR{p1}
		_, c1 := a.once()
		r.open = []accPR{p1, p8}
		_, re := a.once()
		r.open = []accPR{p1}
		_, c2 := a.once()
		c1, re, c2 = only(c1, "closed"), only(re, "new-pr"), only(c2, "closed")
		want(t, c1, "closed#8")
		want(t, re, "new-pr#8")
		want(t, c2, "closed#8")
		if c1[0]["id"] == c2[0]["id"] {
			t.Fatal("a second close has a new id")
		}
		// seq is gapless and every journal line validates; a deleted state.json is rebuilt from the journal
		check := func() int {
			lines := a.journalLines()
			for i, l := range lines {
				v, err := jsonschema.UnmarshalJSON(strings.NewReader(l))
				if err != nil || a.sch.Validate(v) != nil {
					t.Fatalf("journal line %d: %v", i+1, l)
				}
				if fmt.Sprint(v.(map[string]any)["seq"]) != strconv.Itoa(i+1) {
					t.Fatalf("seq gap at line %d", i+1)
				}
			}
			return len(lines)
		}
		n := check()
		if err := os.Remove(filepath.Join(a.dir, "state.json")); err != nil {
			t.Fatal(err)
		}
		if st, ev := a.once(); st != 0 || len(ev) != 0 {
			t.Fatalf("a rebuilt state is a silent baseline: %d %v", st, ev)
		}
		r.open = []accPR{p1, accPRn(9)}
		_, ev = a.once()
		want(t, ev, "new-pr#9")
		if fmt.Sprint(ev[0]["seq"]) != strconv.Itoa(n+1) || check() != n+1 {
			t.Fatalf("seq comes from the journal tail: %v", ev[0]["seq"])
		}
	})

	t.Run("US5 conditional reads and rate limit", func(t *testing.T) {
		forge := newAccForge(t, accPRn(1), accPRn(2))
		a := newAccRun(t, forge)
		a.once()
		forge.hits304 = 0
		_, ev := a.once()
		if len(ev) != 0 || forge.hits304 == 0 {
			t.Fatalf("an unchanged poll is answered by 304s: %d %v", forge.hits304, ev)
		}
		if forge.reads("repos/o/r/pulls/1") != 0 || forge.reads("repos/o/r/commits/") != 0 {
			t.Fatalf("no detail or check reads for settled PRs: %v", forge.log)
		}
		// under 10% remaining doubles the next interval
		forge.rate = `{"resources":{"core":{"limit":5000,"remaining":400,"reset":4102444800}}}`
		a.sleeps = nil
		st, _, _, _ := a.exec("--repo", "o/r", "--until-event", "merged", "--timeout", "5m", "--state-dir", a.dir)
		if st != 4 || len(a.sleeps) == 0 || a.sleeps[0] != 2*time.Minute {
			t.Fatalf("%d %v", st, a.sleeps)
		}
		// exhausted: one watch-error rate-limited, then the wait runs until the reset
		forge.rate = fmt.Sprintf(`{"resources":{"core":{"limit":5000,"remaining":3,"reset":%d}}}`, a.now.Add(15*time.Minute).Unix())
		st, ev, _, _ = a.exec("--repo", "o/r", "--until-event", "watch-error", "--timeout", "1h", "--state-dir", a.dir)
		if st != 0 || len(ev) != 1 || details(ev[0])["code"] != "rate-limited" || fmt.Sprint(details(ev[0])["retry_in_seconds"]) == "0" {
			t.Fatalf("%d %v", st, ev)
		}
	})

	t.Run("US6 offline", func(t *testing.T) {
		forge := newAccForge(t, accPRn(1))
		a := newAccRun(t, forge)
		a.once()
		forge.offline = true
		st, ev := a.once()
		if st != 1 {
			t.Fatalf("exit %d", st)
		}
		want(t, ev, "watch-error#<nil>")
		if details(ev[0])["code"] != "network" {
			t.Fatalf("%v", ev[0])
		}
		if _, ev = a.once(); len(ev) != 0 {
			t.Fatalf("the same error is not repeated: %v", ev)
		}
		forge.offline = false
		a.now = a.now.Add(time.Hour)
		st, ev = a.once()
		want(t, ev, "watch-recovered#<nil>")
		if st != 0 {
			t.Fatalf("exit %d", st)
		}
	})

	t.Run("US7 configuration, ignores and repository changes", func(t *testing.T) {
		forge := newAccForge(t)
		bot, labelled, normal := accPRn(1), accPRn(2), accPRn(3)
		bot.Author = "dependabot[bot]"
		labelled.Labels = []string{"no-watch"}
		forge.r().open = []accPR{normal}
		a := newAccRun(t, forge)
		cfgPath := filepath.Join(t.TempDir(), "watch.toml")
		write := func(body string) {
			if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		write("schema_version = 1\nbogus = 1\nrepos = [\"o/r\"]\n")
		if st, _, _, errOut := a.exec("--config", cfgPath, "--once", "--state-dir", a.dir); st != 2 || !strings.Contains(errOut, "bogus") {
			t.Fatalf("an unknown key is a usage error: %d %q", st, errOut)
		}
		write("schema_version = 1\nrepos = [\"o/r\"]\n[ignore]\nauthors = [\"dependabot[bot]\"]\nlabels = [\"no-watch\"]\n")
		a.exec("--config", cfgPath, "--once", "--state-dir", a.dir)
		forge.r().open = []accPR{normal, bot, labelled, accPRn(4)}
		_, ev, _, _ := a.exec("--config", cfgPath, "--once", "--state-dir", a.dir)
		want(t, ev, "new-pr#4")
		forge.add("o/s")
		write("schema_version = 1\nrepos = [\"o/r\", \"o/s\"]\n[ignore]\nauthors = [\"dependabot[bot]\"]\nlabels = [\"no-watch\"]\n")
		_, ev, _, _ = a.exec("--config", cfgPath, "--once", "--state-dir", a.dir)
		if rc := only(ev, "repos-changed"); len(rc) != 1 || fmt.Sprint(details(rc[0])["added"]) != "[o/s]" || len(only(ev, "new-pr")) != 0 {
			t.Fatalf("%s", kindsOf(ev))
		}
	})

	t.Run("US8 modes and liveness", func(t *testing.T) {
		forge := newAccForge(t, accPRn(1))
		a := newAccRun(t, forge)
		a.once()
		// --status: stale (the run is an hour old on the real clock), exit 5
		if st, _, out, _ := a.exec("--status", "--state-dir", a.dir); st != 5 || !strings.Contains(out, "stale") {
			t.Fatalf("%d %q", st, out)
		}
		forge.r().open = append(forge.r().open, accPRn(2), accPRn(3))
		st, ev, _, _ := a.exec("--repo", "o/r", "--until-event", "checks-green,new-pr", "--state-dir", a.dir)
		if np := only(ev, "new-pr"); st != 0 || len(np) != 2 {
			t.Fatalf("the whole poll is printed: %d %v", st, ev)
		}
		if st, ev, _, _ := a.exec("--repo", "o/r", "--until-event", "merged", "--timeout", "1s", "--state-dir", a.dir); st != 4 || len(only(ev, "merged")) != 0 {
			t.Fatalf("%d", st)
		}
		forge.r().open = append(forge.r().open, accPRn(4))
		st, _, out, _ := a.exec("--repo", "o/r", "--once", "--human", "--state-dir", a.dir)
		if st != 0 || !strings.Contains(out, " o/r#4 new-pr PR 4 by alice into main") || strings.Count(out, "\n") != 1 {
			t.Fatalf("%d %q", st, out)
		}
		a.now = time.Now().UTC().Add(-time.Minute)
		a.once()
		st, _, out, _ = a.exec("--status", "--json", "--state-dir", a.dir)
		var sv map[string]any
		if st != 0 || json.Unmarshal([]byte(out), &sv) != nil || sv["fresh"] != true {
			t.Fatalf("%d %s", st, out)
		}
	})

	t.Run("US9 scale", func(t *testing.T) {
		forge := newAccForge(t)
		var prs []accPR
		for n := 1; n <= 120; n++ {
			prs = append(prs, accPRn(n))
		}
		forge.r().open = prs
		a := newAccRun(t, forge)
		if st, ev := a.once(); st != 0 || len(ev) != 0 {
			t.Fatalf("%d", st)
		}
		forge.r().open = append(prs, accPRn(121))
		_, ev := a.once()
		want(t, ev, "new-pr#121")
		if forge.reads("repos/o/r/pulls?state=open") != 5 {
			t.Fatalf("120 PRs are five pages of 25: %v", forge.log)
		}
		if raw, _ := os.ReadFile(filepath.Join(a.dir, "state.json")); !json.Valid(raw) {
			t.Fatal("state.json is not valid JSON")
		}
		// fifty repositories in one cycle
		many := newAccForge(t)
		args := []string{"--once"}
		for i := 0; i < 50; i++ {
			name := fmt.Sprintf("o/repo%02d", i)
			many.add(name, accPRn(1), accPRn(2))
			args = append(args, "--repo", name)
		}
		b := newAccRun(t, many)
		if st, _, _, _ := b.exec(append(args, "--state-dir", b.dir)...); st != 0 {
			t.Fatalf("exit %d", st)
		}
		many.repos["o/repo37"].open = append(many.repos["o/repo37"].open, accPRn(3))
		_, ev, _, _ = b.exec(append(args, "--state-dir", b.dir)...)
		want(t, ev, "new-pr#3")
		if ev[0]["repo"] != "o/repo37" {
			t.Fatalf("%v", ev[0])
		}
		// above the configured bound: too-many-open-prs
		lim := newAccForge(t)
		for n := 1; n <= 40; n++ {
			lim.r().open = append(lim.r().open, accPRn(n))
		}
		c := newAccRun(t, lim)
		cfg := filepath.Join(t.TempDir(), "watch.toml")
		os.WriteFile(cfg, []byte("schema_version = 1\nrepos = [\"o/r\"]\nmax_open_prs = 30\n"), 0o600)
		_, ev, _, _ = c.exec("--config", cfg, "--once", "--state-dir", c.dir)
		if len(ev) != 1 || details(ev[0])["code"] != "too-many-open-prs" {
			t.Fatalf("%v", ev)
		}
	})

	t.Run("US10 read-only proof", func(t *testing.T) {
		// every story above ran against a forge that fails the test on a non-GET request; here the
		// credentials and the sources
		forge := newAccForge(t, accPRn(1))
		a := newAccRun(t, forge)
		a.once()
		forge.r().open = append(forge.r().open, accPRn(2))
		_, _, out, errOut := a.exec("--repo", "o/r", "--once", "--state-dir", a.dir)
		all := out + errOut
		filepath.WalkDir(a.dir, func(path string, e os.DirEntry, err error) error {
			if err == nil && !e.IsDir() {
				raw, _ := os.ReadFile(path)
				all += string(raw)
			}
			return nil
		})
		if strings.Contains(all, watchToken) {
			t.Fatal("the token appears in output, state or journal")
		}
		for _, dir := range []string{"../watch", "../watchcmd", "../watch/watchhost"} {
			files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
			for _, f := range files {
				if strings.HasSuffix(f, "_test.go") {
					continue
				}
				raw, _ := os.ReadFile(f)
				for _, bad := range []string{"MethodPost", "MethodPut", "MethodPatch", "MethodDelete", `"POST"`, `"PUT"`, `"PATCH"`, `"DELETE"`, "graphql"} {
					if strings.Contains(string(raw), bad) {
						t.Errorf("%s mentions %s", f, bad)
					}
				}
			}
		}
	})
}
