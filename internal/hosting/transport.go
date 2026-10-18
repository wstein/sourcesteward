// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"context"
	"errors"
	ghapi "github.com/cli/go-gh/v2/pkg/api"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxBytes = 1048576
const maxPages = 10
const maxRecords = 1000

type boundedBuffer struct {
	data     bytes.Buffer
	overflow bool
	limit    int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	limit := b.limit
	if limit == 0 {
		limit = maxBytes
	}
	left := limit - b.data.Len()
	if n > left {
		b.overflow = true
		p = p[:left]
	}
	b.data.Write(p)
	return n, nil
}

type ghAPI struct {
	executable, dir, host string
	env                   []string
	client                *http.Client
}

func ghEnvironment() []string {
	var env []string
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "GIT_") {
			continue
		}
		switch upper {
		case "DEBUG", "GH_NO_UPDATE_NOTIFIER", "GH_NO_EXTENSION_UPDATE_NOTIFIER", "GH_TELEMETRY", "GH_REPO", "GH_HOST", "GH_DEBUG", "GH_PAGER", "PAGER", "BROWSER", "GH_BROWSER", "EDITOR", "VISUAL", "GH_EDITOR", "GH_FORCE_TTY", "GH_PROMPT_DISABLED", "GH_ACCESSIBLE_PROMPTER", "CLICOLOR_FORCE", "FORCE_COLOR":
			continue
		}
		env = append(env, e)
	}
	return append(env, "GH_NO_UPDATE_NOTIFIER=1", "GH_NO_EXTENSION_UPDATE_NOTIFIER=1", "GH_TELEMETRY=0", "GH_PROMPT_DISABLED=1", "GH_PAGER=cat", "PAGER=cat", "NO_COLOR=1", "TERM=dumb")
}
func inside(path, root string) bool {
	rel, e := filepath.Rel(root, path)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func newGH(ctx context.Context, target, host string) (*ghAPI, func(), error) {
	return newGHWithTransport(ctx, target, host, nil)
}

// PrepareGHProcess establishes the existing trusted executable, filtered environment
// and external empty directory without acquiring a token or making a request.
// The returned cleanup owns the temporary directory.
func PrepareGHProcess(ctx context.Context, target, host string) (string, string, []string, func(), error) {
	api, cleanup, err := prepareGH(ctx, target, host)
	if err != nil {
		return "", "", nil, cleanup, err
	}
	return api.executable, api.dir, api.env, cleanup, nil
}
func prepareGH(ctx context.Context, target, host string) (*ghAPI, func(), error) {
	if !validHost(host) {
		return nil, func() {}, errors.New("invalid selected API host")
	}
	if !filepath.IsAbs(target) {
		return nil, func() {}, errors.New("gh preparation requires the qualified absolute repository root")
	}
	canonicalTarget, targetErr := filepath.EvalSymlinks(target)
	if targetErr != nil {
		return nil, func() {}, errors.New("cannot qualify the gh repository boundary")
	}
	targetInfo, targetErr := os.Stat(canonicalTarget)
	if targetErr != nil || !targetInfo.IsDir() {
		return nil, func() {}, errors.New("gh repository boundary must be an existing directory")
	}
	target = canonicalTarget
	executable := os.Getenv("GH_PATH")
	var err error
	if executable == "" {
		executable, err = exec.LookPath("gh")
	}
	if err != nil || !filepath.IsAbs(executable) {
		return nil, func() {}, errors.New("gh executable must be available from an absolute trusted caller path")
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil || inside(executable, target) {
		return nil, func() {}, errors.New("gh executable must be outside the inspected repository")
	}
	configRoot := os.Getenv("GH_CONFIG_DIR")
	if configRoot == "" {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			configRoot = filepath.Join(xdg, "gh")
		} else {
			configRoot = filepath.Join(os.Getenv("HOME"), ".config", "gh")
		}
	}
	for _, configured := range []string{os.Getenv("HOME"), os.Getenv("XDG_CONFIG_HOME"), configRoot, filepath.Join(configRoot, "hosts.yml"), filepath.Join(configRoot, "config.yml")} {
		if configured == "" {
			continue
		}
		resolved, err := resolveConfigurationPath(configured, 0)
		if err != nil || inside(resolved, target) {
			return nil, func() {}, errors.New("gh authentication configuration must be outside the inspected repository")
		}
	}

	base, err := filepath.EvalSymlinks("/tmp")
	if err != nil || inside(base, target) {
		return nil, func() {}, errors.New("cannot establish an external neutral temporary directory")
	}
	dir, err := os.MkdirTemp(base, "sourcesteward-hosting-")
	if err != nil {
		return nil, func() {}, errors.New("cannot prepare neutral gh execution directory")
	}
	cleanup := func() { os.RemoveAll(dir) }
	api := &ghAPI{executable: executable, dir: dir, host: host, env: ghEnvironment()}
	out, err := api.run(ctx, "--version")
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 || fields[0] != "gh" || fields[1] != "version" || !supportedGH(fields[2]) {
		cleanup()
		return nil, func() {}, errors.New("hosting reads require GitHub CLI 2.102.0 or newer")
	}
	return api, cleanup, nil
}
func newGHWithTransport(ctx context.Context, target, host string, rt http.RoundTripper) (*ghAPI, func(), error) {
	api, cleanup, err := prepareGH(ctx, target, host)
	if err != nil {
		return nil, cleanup, err
	}

	token, err := api.token(ctx)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	var owned *http.Transport
	if rt == nil {
		owned = newHTTPTransport()
		rt = owned
	}
	if owned != nil {
		previous := cleanup
		cleanup = func() { owned.CloseIdleConnections(); previous() }
	}
	api.client, err = newSDKClient(host, token, api.dir, rt)
	if err != nil {
		cleanup()
		return nil, func() {}, errors.New("cannot initialize bounded hosting transport")
	}
	return api, cleanup, nil
}
func supportedGH(s string) bool {
	p := strings.Split(s, ".")
	if len(p) < 3 {
		return false
	}
	a, e := strconv.Atoi(p[0])
	b, f := strconv.Atoi(p[1])
	c, g := strconv.Atoi(p[2])
	return e == nil && f == nil && g == nil && (a > 2 || (a == 2 && (b > 102 || (b == 102 && c >= 0))))
}
func (api *ghAPI) run(ctx context.Context, args ...string) ([]byte, error) {
	return api.runLimited(ctx, maxBytes, args...)
}
func (api *ghAPI) runLimited(parent context.Context, limit int, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, api.executable, args...)
	cmd.Dir = api.dir
	cmd.Env = api.env
	cmd.Stdin = strings.NewReader("")
	cmd.WaitDelay = time.Second
	out, errout := boundedBuffer{limit: limit}, boundedBuffer{limit: limit}
	cmd.Stdout = &out
	cmd.Stderr = &errout
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, errors.New("hosting read canceled or exceeded its deadline")
	}
	if out.overflow || errout.overflow {
		return nil, errors.New("gh output exceeded the one MiB limit")
	}
	if err != nil {
		return nil, errors.New("gh read failed; check selected-host authentication, access and rate limits")
	}
	return out.data.Bytes(), nil
}
func (api *ghAPI) token(parent context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	raw, err := api.runLimited(ctx, 16*1024, "auth", "token", "--hostname", api.host)
	if err != nil {
		return "", authenticationFailure("gh authentication unavailable; check selected-host credentials")
	}
	token := strings.TrimSuffix(string(raw), "\n")
	if len(token) == 0 {
		return "", authenticationFailure("gh authentication returned an invalid token")
	}
	for _, b := range []byte(token) {
		if b < 33 || b > 126 {
			return "", authenticationFailure("gh authentication returned an invalid token")
		}
	}
	return token, nil
}

var fixedEndpoint = regexp.MustCompile(`^repos/[A-Za-z0-9_-][A-Za-z0-9_.-]*/[A-Za-z0-9_-][A-Za-z0-9_.-]*/(pulls/[1-9][0-9]*|commits/[0-9a-f]{40}/(check-runs|statuses)\?per_page=100&page=([1-9]|10))$`)

func apiDestination(host string) (string, string) {
	if host == "github.com" || strings.HasSuffix(host, ".ghe.com") {
		return "api." + host, "/"
	}
	return host, "/api/v3/"
}

var errBodyLimit = errors.New("hosting response exceeded the one MiB limit")

type cappedBody struct {
	io.ReadCloser
	remaining int
	overflow  bool
}

func (b *cappedBody) Read(p []byte) (int, error) {
	if b.overflow {
		return 0, errBodyLimit
	}
	if len(p) == 0 {
		return 0, nil
	}
	if b.remaining == 0 {
		var probe [1]byte
		n, e := b.ReadCloser.Read(probe[:])
		if n > 0 {
			b.overflow = true
			return 0, errBodyLimit
		}
		return 0, e
	}
	if len(p) > b.remaining {
		p = p[:b.remaining]
	}
	n, e := b.ReadCloser.Read(p)
	b.remaining -= n
	return n, e
}

type guardedTransport struct {
	next         http.RoundTripper
	host, prefix string
}

func (t guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil || req.ContentLength != 0 || (req.Host != "" && req.Host != t.host) || req.URL.ForceQuery || req.Method != http.MethodGet || req.URL.Scheme != "https" || req.URL.Host != t.host || req.URL.User != nil || req.URL.Fragment != "" || req.URL.RawPath != "" || req.URL.Opaque != "" || !strings.HasPrefix(req.URL.Path, t.prefix) || !fixedReadEndpoint(strings.TrimPrefix(req.URL.Path, t.prefix)+func() string {
		if req.URL.RawQuery != "" {
			return "?" + req.URL.RawQuery
		}
		return ""
	}()) {
		return nil, errors.New("hosting request refused outside fixed read-only destination")
	}
	res, err := t.next.RoundTrip(req)
	if err != nil {
		if res != nil && res.Body != nil {
			res.Body.Close()
		}
		return nil, errors.New("hosting HTTP read failed")
	}
	if res == nil || res.Body == nil {
		return nil, errors.New("hosting HTTP response is unavailable")
	}
	for _, value := range res.Header.Values("Content-Encoding") {
		for _, enc := range strings.Split(value, ",") {
			if enc = strings.ToLower(strings.TrimSpace(enc)); enc != "" && enc != "identity" {
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
func newHTTPTransport() *http.Transport {
	return &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true, MaxIdleConns: 100, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ExpectContinueTimeout: time.Second, MaxResponseHeaderBytes: 32 * 1024, DisableCompression: true}
}
func newSDKClient(host, token, dir string, rt http.RoundTripper) (*http.Client, error) {
	destination, prefix := apiDestination(host)
	if rt == nil {
		rt = newHTTPTransport()
	}
	return newSDKClientWithGuard(host, token, dir, guardedTransport{next: rt, host: destination, prefix: prefix})
}

func newSDKClientWithGuard(host, token, dir string, guard http.RoundTripper) (*http.Client, error) {
	destination, _ := apiDestination(host)
	return ghapi.NewHTTPClient(ghapi.ClientOptions{Host: host, APIHost: destination, AuthToken: token, Transport: guard, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("hosting redirects are unsupported") }, LogIgnoreEnv: true, CacheDir: dir, SkipDefaultHeaders: true, Headers: map[string]string{"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28", "User-Agent": "SourceSteward"}})
}
func (api *ghAPI) get(ctx context.Context, endpoint string) ([]byte, error) {
	if !fixedReadEndpoint(endpoint) {
		return nil, errors.New("invalid fixed API endpoint")
	}
	host, prefix := apiDestination(api.host)
	u := url.URL{Scheme: "https", Host: host, Path: prefix + strings.SplitN(endpoint, "?", 2)[0]}
	if _, query, ok := strings.Cut(endpoint, "?"); ok {
		u.RawQuery = query
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("cannot prepare hosting read")
	}
	res, err := api.client.Do(req)
	if err != nil {
		if errors.Is(err, errBodyLimit) {
			return nil, errBodyLimit
		}
		return nil, errors.New("hosting HTTP read failed; check selected-host authentication, access and rate limits")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil || len(body) > maxBytes {
		if len(body) > maxBytes || errors.Is(err, errBodyLimit) {
			return nil, errBodyLimit
		}
		return nil, errors.New("hosting response exceeded its bounds or could not be read")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, errors.New("hosting HTTP read failed; check selected-host authentication, access and rate limits")
	}
	return body, nil
}

// Resolve existing symlinks even when a final credential file or directory is absent.
// EvalSymlinks alone cannot classify a dangling repository-linked config path.
func resolveConfigurationPath(path string, depth int) (string, error) {
	if depth > 40 || !filepath.IsAbs(path) {
		return "", errors.New("invalid configuration path")
	}
	path = filepath.Clean(path)
	parts := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
	current := string(filepath.Separator)
	for i, part := range parts {
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return filepath.Join(append([]string{current}, parts[i+1:]...)...), nil
		}
		if err != nil {
			return "", err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			linked, err := os.Readlink(current)
			if err != nil {
				return "", err
			}
			if !filepath.IsAbs(linked) {
				linked = filepath.Join(filepath.Dir(current), linked)
			}
			return resolveConfigurationPath(filepath.Join(append([]string{linked}, parts[i+1:]...)...), depth+1)
		}
	}
	return current, nil
}
