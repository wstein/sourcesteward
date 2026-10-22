// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/watch"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

const watchToken = "fixture-watch-token-4711"

// watchForge is an in-process GitHub: fixed documents per request URI, GET only (anything else fails
// the test), a request log and ETag handling.
type watchForge struct {
	t    *testing.T
	mu   sync.Mutex
	docs map[string]string
	log  []string
}

func (f *watchForge) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method != http.MethodGet {
		f.t.Errorf("non-GET request reached the forge: %s %s", r.Method, r.URL)
	}
	if got := r.Header.Get("Authorization"); got != "token "+watchToken {
		f.t.Errorf("unexpected credentials %q", got)
	}
	uri := strings.TrimPrefix(r.URL.EscapedPath(), "/")
	if r.URL.RawQuery != "" {
		uri += "?" + r.URL.RawQuery
	}
	f.log = append(f.log, uri)
	h := http.Header{"Content-Type": []string{"application/json"}}
	body, ok := f.docs[uri]
	if !ok {
		return &http.Response{StatusCode: 404, Header: h, Body: io.NopCloser(strings.NewReader(`{"message":"Not Found"}`)), Request: r}, nil
	}
	etag := `"` + string(rune('a'+len(body)%26)) + `-` + uri[len(uri)-3:] + `"`
	h.Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		return &http.Response{StatusCode: 304, Header: h, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}
	return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func watchEnv(t *testing.T, withToken bool) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GH_CONFIG_DIR", filepath.Join(home, "gh"))
	t.Setenv("XDG_CONFIG_HOME", home)
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		t.Setenv(name, "")
	}
	script := "#!/bin/sh\ncase \"$*\" in\n --version) echo 'gh version 2.102.0';;\n 'auth token --hostname github.com') echo " + watchToken + ";;\n *) exit 1;;\nesac\n"
	if !withToken {
		script = "#!/bin/sh\ncase \"$*\" in\n --version) echo 'gh version 2.102.0';;\n *) exit 1;;\nesac\n"
	}
	fake := filepath.Join(t.TempDir(), "gh")
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_PATH", fake)
}

func pullDoc(n int, head string) string {
	return `{"number":` + strconv.Itoa(n) + `,"title":"PR ` + strconv.Itoa(n) + `","user":{"login":"alice"},"labels":[],"draft":false,"head":{"sha":"` + head + `","ref":"feat/` + strconv.Itoa(n) + `"},"base":{"sha":"` + strings.Repeat("b", 40) + `","ref":"main"}}`
}

// forgeWith serves the given PR numbers (heads are 40 repetitions of the digit).
func forgeWith(t *testing.T, numbers ...int) *watchForge {
	f := &watchForge{t: t, docs: map[string]string{}}
	var list []string
	for _, n := range numbers {
		head := strings.Repeat(strconv.Itoa(n), 40)
		list = append(list, pullDoc(n, head))
		f.docs["repos/o/r/pulls/"+strconv.Itoa(n)] = `{"mergeable":true,"mergeable_state":"clean","head":{"repo":{"full_name":"o/r"}},"base":{"repo":{"full_name":"o/r"}}}`
		f.docs["repos/o/r/commits/"+head+"/check-runs?per_page=100&page=1"] = `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{}}]}`
		f.docs["repos/o/r/commits/"+head+"/statuses?per_page=100&page=1"] = `[]`
	}
	f.docs["repos/o/r/pulls?state=open&per_page=25&page=1"] = "[" + strings.Join(list, ",") + "]"
	f.docs["repos/o/r/branches/main"] = `{"commit":{"sha":"` + strings.Repeat("a", 40) + `"}}`
	f.docs["rate_limit"] = `{"resources":{"core":{"limit":5000,"remaining":4990,"reset":4102444800}}}`
	return f
}

func watchRun(t *testing.T, forge http.RoundTripper, args ...string) (int, string, string) {
	t.Helper()
	watchTransport = forge
	watchHook = func(p *watchcmd.Params) {
		p.Sleep = func(ctx context.Context, d time.Duration) error { return ctx.Err() }
		p.Jitter = func() time.Duration { return 0 }
	}
	t.Cleanup(func() { watchTransport, watchHook = nil, nil })
	var stdout, stderr bytes.Buffer
	status := Run(context.Background(), "test", append([]string{"watch"}, args...), &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

func TestWatchOnceBaselineThenTransitionEndToEnd(t *testing.T) {
	watchEnv(t, true)
	dir := filepath.Join(t.TempDir(), "state")
	forge := forgeWith(t, 1)
	status, out, errOut := watchRun(t, forge, "--repo", "o/r", "--once", "--state-dir", dir)
	if status != 0 || out != "" {
		t.Fatalf("baseline: %d %q %q", status, out, errOut)
	}
	forge2 := forgeWith(t, 1, 2)
	status, out, errOut = watchRun(t, forge2, "--repo", "o/r", "--once", "--state-dir", dir)
	if status != 0 || errOut != "" {
		t.Fatalf("%d %q", status, errOut)
	}
	var ev map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &ev); err != nil || ev["kind"] != "new-pr" || ev["pr"] != float64(2) || ev["repo"] != "o/r" || ev["seq"] != float64(1) {
		t.Fatalf("%v %q", err, out)
	}
	d, _ := ev["details"].(map[string]any)
	if d["title"] != "PR 2" || d["author"] != "alice" || d["base"] != "main" || d["fork"] != false || len(d) != 7 {
		t.Fatalf("%v", d)
	}
	// the journal line and the state never contain the token
	filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			if raw, _ := os.ReadFile(path); strings.Contains(string(raw), watchToken) {
				t.Errorf("token in %s", path)
			}
		}
		return nil
	})
	if strings.Contains(out+errOut, watchToken) {
		t.Fatal("token in output")
	}
	// --status: fresh right after a run; JSON has the contract keys
	status, out, _ = watchRun(t, forge2, "--status", "--json", "--state-dir", dir)
	var st map[string]any
	if status != 0 || json.Unmarshal([]byte(out), &st) != nil || st["fresh"] != true || st["seq"] != float64(1) || st["lock_held"] != false {
		t.Fatalf("%d %s", status, out)
	}
	// --human prints one line per event
	forge3 := forgeWith(t, 1, 2, 3)
	status, out, _ = watchRun(t, forge3, "--repo", "o/r", "--once", "--human", "--state-dir", dir)
	if status != 0 || !strings.Contains(out, " o/r#3 new-pr PR 3 by alice into main") || !strings.Contains(out, " o/r#2 checks-green ") || strings.Count(out, "\n") != 2 {
		t.Fatalf("%d %q", status, out)
	}
}

func TestWatchExitCodes(t *testing.T) {
	watchEnv(t, true)
	dir := filepath.Join(t.TempDir(), "state")
	forge := forgeWith(t, 1)
	for name, args := range map[string][]string{
		"no source":           {"--once"},
		"modes exclusive":     {"--repo", "o/r", "--once", "--stream"},
		"unknown kind":        {"--repo", "o/r", "--until-event", "bogus"},
		"relative state dir":  {"--repo", "o/r", "--once", "--state-dir", "rel"},
		"interval below 10s":  {"--repo", "o/r", "--once", "--interval", "5s"},
		"timeout zero":        {"--repo", "o/r", "--until-event", "merged", "--timeout", "0"},
		"unknown flag":        {"--repo", "o/r", "--nonsense"},
		"missing config file": {"--config", filepath.Join(t.TempDir(), "none.toml"), "--once"},
	} {
		if status, out, errOut := watchRun(t, forge, args...); status != 2 || out != "" || !strings.HasPrefix(errOut, "Error:") {
			t.Errorf("%s: %d %q %q", name, status, out, errOut)
		}
	}
	if len(forge.log) != 0 {
		t.Fatalf("a usage error reads nothing: %v", forge.log)
	}
	// --until-event times out with exit 4 and no output
	status, out, errOut := watchRun(t, forge, "--repo", "o/r", "--until-event", "merged", "--timeout", "1s", "--state-dir", dir)
	if status != 4 || out != "" || !strings.Contains(errOut, "no event") {
		t.Fatalf("%d %q %q", status, out, errOut)
	}
	// stale heartbeat: exit 5 and the report is still printed
	staleDir := filepath.Join(t.TempDir(), "never-ran")
	status, out, _ = watchRun(t, forge, "--status", "--state-dir", staleDir)
	if status != 5 || !strings.Contains(out, "heartbeat: missing") {
		t.Fatalf("%d %q", status, out)
	}
	// a second watcher exits 3 and names the holder
	holder, err := watch.Open(watch.Options{Dir: dir, Repos: []string{"o/r"}, Observer: watchObserverStub{}})
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	status, _, errOut = watchRun(t, forge, "--repo", "o/r", "--once", "--state-dir", dir)
	if status != 3 || !strings.Contains(errOut, "pid") {
		t.Fatalf("%d %q", status, errOut)
	}
}

type watchObserverStub struct{}

func (watchObserverStub) Observe(context.Context, string, watch.Hints) (watch.RepoObservation, error) {
	return watch.RepoObservation{}, nil
}

func TestWatchMissingTokenIsExitOneBeforeAnyRequest(t *testing.T) {
	watchEnv(t, false)
	forge := forgeWith(t, 1)
	status, out, errOut := watchRun(t, forge, "--repo", "o/r", "--once", "--state-dir", filepath.Join(t.TempDir(), "state"))
	if status != 1 || out != "" || !strings.Contains(errOut, "hosting-api-unavailable") || len(forge.log) != 0 {
		t.Fatalf("%d %q %q %v", status, out, errOut, forge.log)
	}
}

func TestWatchNoReachableRepositoryInOnceIsExitOne(t *testing.T) {
	watchEnv(t, true)
	forge := &watchForge{t: t, docs: map[string]string{}} // every read is a 404
	status, out, _ := watchRun(t, forge, "--repo", "o/r", "--once", "--state-dir", filepath.Join(t.TempDir(), "state"))
	var ev map[string]any
	if status != 1 || json.Unmarshal([]byte(strings.TrimSpace(out)), &ev) != nil || ev["kind"] != "watch-error" {
		t.Fatalf("%d %q", status, out)
	}
}

func TestWatchIsHiddenAndDocumentedInItsHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := Run(context.Background(), "test", []string{"--help"}, &stdout, &stderr); status != 0 || strings.Contains(stdout.String(), "  watch ") {
		t.Fatalf("watch must stay hidden until accepted: %d %s", status, stdout.String())
	}
	stdout.Reset()
	if status := Run(context.Background(), "test", []string{"watch", "--help"}, &stdout, &stderr); status != 0 || !strings.Contains(stdout.String(), "--until-event") {
		t.Fatalf("%d %s", status, stdout.String())
	}
}
