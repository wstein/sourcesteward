// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"errors"
	"fmt"
	"github.com/wstein/sourcesteward/internal/inspection"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTidyFixedReadRoutes(t *testing.T) {
	for _, endpoint := range []string{"repos/owner/repo", "repos/owner/repo/pulls?state=closed&per_page=100&page=1", "repos/owner/repo/branches?per_page=100&page=10"} {
		if !transportReadEndpoint(endpoint) {
			t.Errorf("required fixed tidy GET route refused: %s", endpoint)
		}
	}
	for _, endpoint := range []string{"repos/owner/repo/", "repos/owner/repo/pulls?state=closed&head=owner:topic&per_page=100&page=1", "repos/owner/repo/pulls?state=closed&base=main&per_page=100&page=1", "repos/owner/repo/branches?protected=false&per_page=100&page=1", "repos/owner/repo/branches?per_page=100&page=11", "repos/owner/repo?x=1", "repos/../repo", "repos/owner/repo/branches?per_page=100&page=01"} {
		if transportReadEndpoint(endpoint) {
			t.Errorf("unexpected tidy route admitted: %s", endpoint)
		}
	}
}

func tidyNativeFixture(path string) inspection.RemoteResult {
	return inspection.RemoteResult{Status: "ok", Identity: inspection.Result{Status: "ok", Backend: "git", InputPath: path, Repository: &inspection.Repository{GitDir: path, ObjectFormat: "sha1", Worktree: &path}}, Remotes: []inspection.Remote{{Name: "origin", FetchURLs: []string{"https://github.com/owner/repo.git"}}}}
}
func TestTidySessionQualificationBeforeAuthAndOfflineZero(t *testing.T) {
	cases := []struct {
		name    string
		options TidySessionOptions
		change  func(*inspection.RemoteResult)
	}{
		{name: "offline", options: TidySessionOptions{Remote: "origin"}},
		{name: "unacknowledged-apply", options: TidySessionOptions{Apply: true, Remote: "origin"}},
		{name: "missing-remote", options: TidySessionOptions{Online: true}},
		{name: "failed-native", change: func(r *inspection.RemoteResult) { r.Status = "error" }},
		{name: "jj", change: func(r *inspection.RemoteResult) { r.Identity.Backend = "jj" }},
		{name: "sha256", change: func(r *inspection.RemoteResult) { r.Identity.Repository.ObjectFormat = "sha256" }},
		{name: "multiple-urls", change: func(r *inspection.RemoteResult) {
			r.Remotes[0].FetchURLs = append(r.Remotes[0].FetchURLs, "https://github.com/owner/repo")
		}},
		{name: "wrong-provider", change: func(r *inspection.RemoteResult) {
			r.Remotes[0].FetchURLs = []string{"https://github.example/owner/repo"}
		}},
		{name: "conflicting-push-url", change: func(r *inspection.RemoteResult) {
			r.Remotes[0].PushURLs = []string{"https://github.com/other/repo.git"}
		}},
		{name: "credential-url", change: func(r *inspection.RemoteResult) {
			r.Remotes[0].FetchURLs = []string{"https://secret@github.com/owner/repo"}
		}},
		{name: "explicit-disagreement", options: TidySessionOptions{Online: true, Remote: "origin", Repo: "other/repo"}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			o := item.options
			if o == (TidySessionOptions{}) {
				o = TidySessionOptions{Online: true, Remote: "origin"}
			}
			native := tidyNativeFixture(t.TempDir())
			if item.change != nil {
				item.change(&native)
			}
			auth := 0
			s, err := openTidySession(context.Background(), native.Identity.InputPath, o, nil, tidySessionDependencies{remotes: func(context.Context, string) inspection.RemoteResult { return native }, api: func(context.Context, string, string, http.RoundTripper) (*ghAPI, func(), error) {
				auth++
				return nil, func() {}, fmt.Errorf("unexpected auth")
			}})
			if err == nil || auth != 0 || s.Budget().NetworkAttempted || s.Budget().Requests != 0 {
				t.Fatalf("auth=%d stats=%+v err=%v", auth, s.Budget(), err)
			}
		})
	}
}
func TestTidySessionAuthBoundsAndNetworkAccounting(t *testing.T) {
	for _, kind := range []string{"valid", "stdout-over", "stderr-over", "auth-error"} {
		t.Run(kind, func(t *testing.T) {
			path := t.TempDir()
			exe := filepath.Join(t.TempDir(), "gh")
			script := "#!/bin/sh\nif [ \"$1\" = --version ];then echo 'gh version 2.102.0';exit 0;fi\n"
			switch kind {
			case "stdout-over":
				script += "head -c 16385 /dev/zero\n"
			case "stderr-over":
				script += "head -c 16385 /dev/zero >&2\necho fixture-secret\n"
			case "auth-error":
				script += "echo fixture-secret >&2\nexit 1\n"
			default:
				script += "echo fixture-secret\n"
			}
			if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GH_PATH", exe)
			native := tidyNativeFixture(path)
			calls := 0
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != "GET" || req.URL.Host != "api.github.com" || req.Header.Get("Authorization") == "" {
					t.Fatal("unguarded request")
				}
				return response(200, tidyRepositoryJSON()), nil
			})
			s, err := openTidySession(context.Background(), path, TidySessionOptions{Online: true, Remote: "origin"}, rt, tidySessionDependencies{remotes: func(context.Context, string) inspection.RemoteResult { return native }, api: newGHWithTransport})
			defer s.Close()
			if kind == "valid" {
				if err != nil || calls != 1 || s.Repository().ID != 41 || !s.Budget().NetworkAttempted || s.Budget().RawBytes != int64(len(tidyRepositoryJSON())) {
					t.Fatalf("%+v %v calls=%d", s.Budget(), err, calls)
				}
			} else if err == nil || calls != 0 || s.Budget().NetworkAttempted || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatalf("calls=%d %+v %v", calls, s.Budget(), err)
			}
		})
	}
}
func TestTidySessionOneDeadlineAndParentCancellation(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(fmt.Sprint(apply), func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			native := tidyNativeFixture(t.TempDir())
			want := 30 * time.Second
			if apply {
				want = 60 * time.Second
			}
			s, err := openTidySession(parent, native.Identity.InputPath, TidySessionOptions{Online: true, Apply: apply, Remote: "origin"}, roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, tidyRepositoryJSON()), nil }), tidySessionDependencies{remotes: func(ctx context.Context, _ string) inspection.RemoteResult {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > want || time.Until(deadline) < want-time.Second {
					t.Fatal("wrong original deadline")
				}
				return native
			}, api: func(ctx context.Context, _, _ string, rt http.RoundTripper) (*ghAPI, func(), error) {
				client, err := newSDKClient("github.com", "fixture-secret", t.TempDir(), rt)
				return &ghAPI{host: "github.com", client: client}, func() {}, err
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			deadline, _ := s.ctx.Deadline()
			if err = s.RevalidateRepository(context.Background()); err != nil {
				t.Fatal(err)
			}
			after, _ := s.ctx.Deadline()
			if !deadline.Equal(after) {
				t.Fatal("deadline reset")
			}
			before := s.Budget().Requests
			cancel()
			if err = s.RevalidateRepository(context.Background()); err == nil || s.Budget().Requests != before {
				t.Fatalf("request after cancellation: %v", err)
			}
		})
	}
}
func TestTidySessionHTTPFailureMatrixAndRedaction(t *testing.T) {
	for _, kind := range []string{"redirect", "compression", "duplicate", "bad-json", "bad-utf8", "error", "overflow", "normalized-overflow", "transport-error", "cancellation"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
				calls++
				switch kind {
				case "redirect":
					r := response(302, "fixture-secret")
					r.Header.Set("Location", "https://evil.invalid/fixture-secret")
					return r, nil
				case "compression":
					r := response(200, tidyRepositoryJSON())
					r.Header.Set("Content-Encoding", "identity, gzip")
					return r, nil
				case "duplicate":
					return response(200, `{"id":41,"id":42,"node_id":"R_repo","full_name":"owner/repo","default_branch":"main"}`), nil
				case "bad-json":
					return response(200, `{"secret":"fixture-secret"`), nil
				case "bad-utf8":
					return response(200, string([]byte{255})), nil
				case "error":
					return response(403, `{"message":"fixture-secret"}`), nil
				case "overflow":
					return response(200, strings.Repeat("x", maxBytes+1)), nil
				case "normalized-overflow":
					return response(200, strings.Repeat("\x00", maxBytes/2+1)), nil
				case "transport-error":
					return nil, errors.New("fixture-secret")
				case "cancellation":
					<-req.Context().Done()
					return nil, req.Context().Err()
				}
				return nil, nil
			})
			ctx := context.Background()
			if kind == "cancellation" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 10*time.Millisecond)
				defer cancel()
			}
			err := s.RevalidateRepository(ctx)
			if err == nil || calls != 1 || s.Budget().Requests != 1 || !s.Budget().NetworkAttempted || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatalf("%s calls=%d stats=%+v %v", kind, calls, s.Budget(), err)
			}
			if kind == "error" && s.Budget().RawBytes != int64(len(`{"message":"fixture-secret"}`)) {
				t.Fatal("error body omitted from budget")
			}
		})
	}
}
func TestTidySessionRequestBudgetSharedAcrossMethodsAndFailures(t *testing.T) {
	s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) { return response(403, `{"message":"denied"}`), nil })
	for i := 0; i < tidyMaxRequests; i++ {
		if err := s.RevalidateRepository(context.Background()); err == nil {
			t.Fatal("error body accepted")
		}
	}
	before := s.Budget()
	if err := s.RevalidateRepository(context.Background()); err == nil || s.Budget() != before || before.Requests != 128 || before.RawBytes != int64(128*len(`{"message":"denied"}`)) {
		t.Fatalf("budget reset: %+v %v", s.Budget(), err)
	}
}
func TestTidySessionRawBudgetCountsDetectionByteAndNeverResets(t *testing.T) {
	ledger := &tidyHTTPLedger{}
	payload := strings.Repeat("x", maxBytes)
	for i := 0; i < 32; i++ {
		body := &tidyBudgetBody{ReadCloser: io.NopCloser(strings.NewReader(payload)), ledger: ledger}
		raw, err := io.ReadAll(body)
		if err != nil || len(raw) != maxBytes {
			t.Fatalf("exact budget %d %v", i, err)
		}
	}
	body := &tidyBudgetBody{ReadCloser: io.NopCloser(strings.NewReader("xy")), ledger: ledger}
	raw, err := io.ReadAll(body)
	if !errors.Is(err, ErrTidyLimit) || len(raw) != 1 || ledger.snapshot().RawBytes != tidyMaxRawBytes+1 {
		t.Fatalf("%d %+v %v", len(raw), ledger.snapshot(), err)
	}
	if err = ledger.request(); !errors.Is(err, ErrTidyLimit) {
		t.Fatal("exhausted ledger reset")
	}
}
func TestTidySessionGuardRejectsMutationAndEnterpriseTidyRoutes(t *testing.T) {
	for _, host := range []string{"github.com", "ghe.example"} {
		calls := 0
		client, err := newSDKClient(host, "fixture-secret", t.TempDir(), roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(200, `[]`), nil }))
		if err != nil {
			t.Fatal(err)
		}
		dest, prefix := apiDestination(host)
		for _, method := range []string{"GET", "POST", "DELETE"} {
			req, _ := http.NewRequest(method, "https://"+dest+prefix+"repos/owner/repo/branches?per_page=100&page=1", nil)
			res, err := client.Do(req)
			if res != nil {
				res.Body.Close()
			}
			if method == "GET" && host == "github.com" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatalf("%s %s admitted", host, method)
			}
		}
		want := 0
		if host == "github.com" {
			want = 1
		}
		if calls != want {
			t.Fatal("request escaped guard", calls)
		}
	}
}
func TestTidyPaginationMaliciousAndInconsistentLinks(t *testing.T) {
	endpoint := "repos/owner/repo/branches?per_page=100&page=1"
	for _, link := range []string{
		`<https://evil.invalid/repos/owner/repo/branches?per_page=100&page=2>; rel="next"`,
		`<https://api.github.com/repos/other/repo/branches?per_page=100&page=2>; rel="next"`,
		`<https://api.github.com/repos/owner/repo/branches?per_page=100&page=3>; rel="next"`,
		`<https://api.github.com/repos/owner/repo/branches?per_page=100&page=2&protected=false>; rel="next"`,
		`<https://api.github.com/repos/owner/repo/branches?per_page=100&page=2&page=2>; rel="next"`,
		`<https://api.github.com/repos/owner/repo/branches?per_page=100&page=02>; rel="next"`,
		`<https://api.github.com/repos/owner/repo/branches?per_page=100&page=2#bad>; rel="next"`,
		`<https://api.github.com/repos/owner/repo/branches?per_page=100&page=2>; rel="next"; x="y"`,
		`<https://api.github.com/repos/owner/repo/branches?per_page=100&page=2>; rel="next", <https://api.github.com/repos/owner/repo/branches?per_page=100&page=2>; rel="next"`,
	} {
		header := http.Header{}
		header.Set("Link", link)
		last := 0
		if _, err := tidyPagination(header, endpoint, 1, 100, &last); err == nil {
			t.Fatal("malicious link admitted", link)
		}
	}
	last := 0
	header := http.Header{}
	header.Set("Link", `<https://api.github.com/repos/owner/repo/branches?per_page=100&page=2>; rel="next"`)
	if _, err := tidyPagination(header, endpoint, 1, 3, &last); err == nil {
		t.Fatal("short page with next admitted")
	}
}

func TestTidySessionActualNativeRemoteQualificationBeforeFixtureAuth(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	// Sanitize before the first setup command, including direct test-binary runs.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			t.Setenv(key, "") // Register restoration before removing the variable.
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GH_CONFIG_DIR", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	target := t.TempDir()
	for _, args := range [][]string{{"init", "--object-format=sha1", "-b", "main", target}, {"-C", target, "remote", "add", "origin", "https://github.com/owner/repo.git"}} {
		cmd := exec.Command(git, args...)
		if raw, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git: %s %v", raw, err)
		}
	}
	marker := filepath.Join(t.TempDir(), "auth-calls")
	exe := filepath.Join(t.TempDir(), "gh")
	script := `#!/bin/sh
[ "$PWD" != "$TIDY_FIXTURE_TARGET" ] || exit 7
if [ "$1" = --version ];then echo 'gh version 2.102.0';exit 0;fi
[ "$1 $2 $3 $4" = 'auth token --hostname github.com' ] || exit 8
printf 'auth\n' >> "$TIDY_FIXTURE_CALLS"
echo fixture-secret
`
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_PATH", exe)
	t.Setenv("TIDY_FIXTURE_TARGET", target)
	t.Setenv("TIDY_FIXTURE_CALLS", marker)
	before := inspection.Inspect(context.Background(), target)
	if before.Status != "ok" {
		t.Fatalf("fixture native identity: %+v", before)
	}
	calls := 0
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Path == "/repos/owner/repo" {
			return response(200, tidyRepositoryJSON()), nil
		}
		return response(200, `[]`), nil
	})
	s, err := OpenTidySessionWithTransport(context.Background(), target, TidySessionOptions{Online: true, Remote: "origin", Repo: "owner/repo"}, rt)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RevalidateRepository(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Close()
	after := inspection.Inspect(context.Background(), target)
	if !reflect.DeepEqual(before, after) || calls != 2 {
		t.Fatalf("native identity changed or unexpected calls: %+v %+v %d", before, after, calls)
	}
	raw, err := os.ReadFile(marker)
	if err != nil || string(raw) != "auth\n" {
		t.Fatalf("auth lifecycle: %q %v", raw, err)
	}
	// The real entry point must refuse unknown ownership without running gh.
	failed, err := OpenTidySessionWithTransport(context.Background(), t.TempDir(), TidySessionOptions{Online: true, Remote: "origin"}, rt)
	if err == nil || failed.Budget().NetworkAttempted || calls != 2 {
		t.Fatal("failed native qualification reached auth/network")
	}
	raw, err = os.ReadFile(marker)
	if err != nil || string(raw) != "auth\n" {
		t.Fatal("native refusal executed gh")
	}
}

func TestTidyPaginationFullFinalPageUsesConsistentEarlierLastLink(t *testing.T) {
	last := 0
	first := http.Header{"Link": []string{`<https://api.github.com/repos/owner/repo/branches?per_page=100&page=2>; rel="next", <https://api.github.com/repos/owner/repo/branches?per_page=100&page=2>; rel="last"`}}
	if terminal, err := tidyPagination(first, "repos/owner/repo/branches?per_page=100&page=1", 1, 100, &last); err != nil || terminal {
		t.Fatalf("first %v %v", terminal, err)
	}
	final := http.Header{"Link": []string{`<https://api.github.com/repos/owner/repo/branches?per_page=100&page=1>; rel="prev", <https://api.github.com/repos/owner/repo/branches?per_page=100&page=1>; rel="first"`}}
	if terminal, err := tidyPagination(final, "repos/owner/repo/branches?per_page=100&page=2", 2, 100, &last); err != nil || !terminal {
		t.Fatalf("consistent authoritative final page refused: %v %v", terminal, err)
	}
}

func TestTidySessionCloseCancelsInflightRead(t *testing.T) {
	started := make(chan struct{})
	s := tidyFixtureSession(t, func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	result := make(chan error, 1)
	go func() { result <- s.RevalidateRepository(context.Background()) }()
	<-started
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Error("Close did not cancel the in-flight request")
		s.cancel()
		<-closed
	}
	if err := <-result; err == nil {
		t.Fatal("closed read reported complete success")
	}
}

func TestTidyRepositoryRequiresExactMemberNames(t *testing.T) {
	for _, key := range []string{"id", "node_id", "full_name", "default_branch"} {
		for _, only := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/alias-only=%v", key, only), func(t *testing.T) {
				raw := tidyAliasedMember(t, tidyRepositoryJSON(), []string{key}, only)
				s := tidyFixtureSession(t, func(*http.Request) (*http.Response, error) { return response(200, raw), nil })
				if err := s.RevalidateRepository(context.Background()); err == nil {
					t.Fatal("aliased repository member accepted", raw)
				}
			})
		}
	}
}
func TestTidyNativeFixtureIgnoresInheritedGitControls(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"git-dir", "all-controls", "caller-config"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			other := filepath.Join(root, "unrelated")
			home := filepath.Join(root, "caller-home")
			xdg := filepath.Join(root, "caller-xdg")
			for _, dir := range []string{home, xdg} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			env := []string{}
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				if !strings.HasPrefix(strings.ToUpper(key), "GIT_") && key != "HOME" && key != "XDG_CONFIG_HOME" {
					env = append(env, entry)
				}
			}
			env = append(env, "HOME="+home, "XDG_CONFIG_HOME="+xdg, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
			init := exec.Command(git, "init", "--object-format=sha1", "-b", "untouched", other)
			init.Env = env
			if raw, err := init.CombinedOutput(); err != nil {
				t.Fatalf("safe disposable setup: %s %v", raw, err)
			}
			before := tidyFixtureTreeBytes(t, other)
			childEnv := append([]string{}, env...)
			switch mode {
			case "git-dir":
				childEnv = append(childEnv, "GIT_DIR="+filepath.Join(other, ".git"))
			case "all-controls":
				childEnv = append(childEnv, "GIT_DIR="+filepath.Join(other, ".git"), "GIT_WORK_TREE="+other, "GIT_COMMON_DIR="+filepath.Join(other, ".git"), "GIT_INDEX_FILE="+filepath.Join(other, "external-index"), "GIT_OBJECT_DIRECTORY="+filepath.Join(other, ".git", "objects"), "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=remote.origin.url", "GIT_CONFIG_VALUE_0=https://example.invalid/unrelated")
			case "caller-config":
				config := []byte("[remote \"origin\"]\n url = https://example.invalid/unrelated\n")
				if err := os.WriteFile(filepath.Join(home, ".gitconfig"), config, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(xdg, "git"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(xdg, "git", "config"), config, 0600); err != nil {
					t.Fatal(err)
				}
				filtered := []string{}
				for _, entry := range childEnv {
					if !strings.HasPrefix(entry, "GIT_CONFIG_GLOBAL=") {
						filtered = append(filtered, entry)
					}
				}
				childEnv = filtered
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, binary, "-test.run=^TestTidySessionActualNativeRemoteQualificationBeforeFixtureAuth$", "-test.count=1", "-test.v")
			child.Env = childEnv
			raw, childErr := child.CombinedOutput()
			after := tidyFixtureTreeBytes(t, other)
			if !reflect.DeepEqual(before, after) {
				t.Error("native fixture modified the unrelated disposable repository")
			}
			if childErr != nil {
				t.Fatalf("fixture failed under inherited %s: %s %v", mode, raw, childErr)
			}
		})
	}
}
func tidyFixtureTreeBytes(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[rel] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestTidyNativeFixtureSkipsWhenGitIsUnavailable(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "PATH" && !strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			env = append(env, entry)
		}
	}
	env = append(env, "PATH="+t.TempDir())
	child := exec.Command(binary, "-test.run=^TestTidySessionActualNativeRemoteQualificationBeforeFixtureAuth$", "-test.count=1", "-test.v")
	child.Env = env
	raw, err := child.CombinedOutput()
	if err != nil || !strings.Contains(string(raw), "--- SKIP:") || !strings.Contains(string(raw), "git is unavailable") {
		t.Fatalf("missing-Git capability was not an explicit skip: %s %v", raw, err)
	}
}
