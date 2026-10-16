// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	left := maxBytes - b.data.Len()
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
	canonicalTarget, targetErr := filepath.EvalSymlinks(target)
	if targetErr == nil {
		target = canonicalTarget
	}
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
	cmd := exec.CommandContext(ctx, api.executable, args...)
	cmd.Dir = api.dir
	cmd.Env = api.env
	cmd.Stdin = strings.NewReader("")
	cmd.WaitDelay = time.Second
	var out, errout boundedBuffer
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
func (api *ghAPI) get(ctx context.Context, endpoint string) ([]byte, error) {
	if !strings.HasPrefix(endpoint, "repos/") || strings.ContainsAny(endpoint, "{}\r\n") {
		return nil, fmt.Errorf("invalid fixed API endpoint")
	}
	return api.run(ctx, "api", "--hostname", api.host, "--method", "GET", endpoint, "--header", "Accept: application/vnd.github+json", "--header", "X-GitHub-Api-Version: 2022-11-28")
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
