// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var errNativeSandboxUnavailable = errors.New("required native write/network sandbox unavailable")
var errNativeOutputLimit = errors.New("native command output exceeds 64 KiB limit")

const nativeOutputLimit = 64 * 1024

type nativeCapture struct {
	data     []byte
	overflow bool
}

func (c *nativeCapture) Write(p []byte) (int, error) {
	n := len(p)
	remaining := nativeOutputLimit - len(c.data)
	if len(p) > remaining {
		c.overflow = true
		p = p[:remaining]
	}
	c.data = append(c.data, p...)
	return n, nil
}

// sandboxEnvironment is also used by effective-configuration admission. Never
// inherit Git overrides, credential/loader variables, editors, or helper paths.
// Standard system/global config is intentionally still visible: the adapter
// refuses executable configuration rather than silently disabling hooks.
func sandboxEnvironment(bin string) []string {
	env := sandboxBaseEnvironment(bin)
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME"} {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	return env
}

// sandboxBaseEnvironment is the fixed part shared with the planning
// environment: nothing inherited, no home or configuration location.
func sandboxBaseEnvironment(bin string) []string {
	return []string{"PATH=" + filepath.Dir(bin), "GIT_EXEC_PATH=" + filepath.Dir(bin), "LANG=C", "LC_ALL=C", "TZ=UTC", "TERM=dumb", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat"}
}

func canonicalSandboxDir(path string) (string, error) {
	if len(path) > 4096 || !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return "", errors.New("sandbox directory must be absolute")
	}
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil || resolved != filepath.Clean(path) {
		return "", errors.New("sandbox directory must be canonical and exist")
	}
	st, e := os.Stat(resolved)
	if e != nil || !st.IsDir() {
		return "", errors.New("sandbox root must be a directory")
	}
	return resolved, nil
}

// runSandboxed allows only declared directory writes and denies network access.
// The fixed /dev/null and /dev/urandom device exceptions admit standard I/O
// and CSPRNG reads used by native Git; no directory or persistent data write is
// granted by these device exceptions.
// Granted directory roots themselves remain present: Linux bind mounts protect
// mount roots, and macOS denies unlinking each literal root while allowing children.
// Existing linked-worktree metadata is rebound read-only after broad parent
// grants. Linux bubblewrap does not constrain exec: adapters must admit native
// configuration/hooks before execution and disclose external-race/ABA limits.
// macOS additionally admits only the exact native binary, including its children.
func prepareSandboxed(ctx context.Context, bin string, args []string, cwd string, writeRoots, readOnlyRoots []string) (*exec.Cmd, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// A planning session (git_planning.go) pins one constant argument list and a
	// private environment on every command it covers; apply writers never carry it.
	session := planningSessionFrom(ctx)
	if session != nil {
		args = session.arguments(args)
	}
	total := len(bin) + len(cwd)
	if len(args) > 1024 {
		return nil, errors.New("too many native arguments")
	}
	for _, arg := range args {
		total += len(arg)
		if strings.ContainsRune(arg, 0) {
			return nil, errors.New("invalid native argument")
		}
	}
	for _, roots := range [][]string{writeRoots, readOnlyRoots} {
		for _, root := range roots {
			total += len(root)
		}
	}
	if total > 64*1024 {
		return nil, errors.New("native arguments exceed input limit")
	}
	if !filepath.IsAbs(bin) {
		return nil, errors.New("native executable must be absolute")
	}
	resolved, e := filepath.EvalSymlinks(bin)
	if e != nil || resolved != filepath.Clean(bin) {
		return nil, errors.New("native executable must be canonical")
	}
	st, e := os.Stat(bin)
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 {
		return nil, errors.New("native executable must be executable regular file")
	}
	cwd, e = canonicalSandboxDir(cwd)
	if e != nil {
		return nil, e
	}
	for i, roots := range [][]string{writeRoots, readOnlyRoots} {
		if len(roots) > 1024 {
			return nil, errors.New("too many sandbox roots")
		}
		for _, r := range roots {
			if i == 0 {
				if _, e := canonicalSandboxDir(r); e != nil {
					return nil, e
				}
			} else {
				if e := canonicalSandboxReadOnly(r); e != nil {
					return nil, e
				}
			}
		}
	}
	for _, r := range writeRoots {
		if r == "/" || bin == r || strings.HasPrefix(bin, r+string(filepath.Separator)) {
			return nil, errors.New("unsafe native write grant")
		}
	}
	var sandbox string
	var argv []string
	switch runtime.GOOS {
	case "darwin":
		sandbox = "/usr/bin/sandbox-exec"
		profile := `(version 1)(allow default)(deny file-write*)(allow file-write-data (literal "/dev/null"))(deny network*)(deny process-exec)(allow process-exec (literal ` + strconv.Quote(bin) + `))`
		for _, r := range writeRoots {
			profile += `(allow file-write* (subpath ` + strconv.Quote(r) + `))`
			profile += `(deny file-write-unlink (literal ` + strconv.Quote(r) + `))`
		}
		for _, r := range readOnlyRoots {
			profile += `(deny file-write* (subpath ` + strconv.Quote(r) + `))`
		}
		argv = append([]string{"-p", profile, bin}, args...)
	case "linux":
		sandbox, e = exec.LookPath("bwrap")
		if e == nil {
			sandbox, e = filepath.EvalSymlinks(sandbox)
		}
		if e != nil {
			return nil, errNativeSandboxUnavailable
		}
		argv = []string{"--ro-bind", "/", "/", "--dev-bind", "/dev/null", "/dev/null", "--dev-bind", "/dev/urandom", "/dev/urandom", "--unshare-net", "--die-with-parent", "--new-session", "--cap-drop", "ALL"}
		for _, r := range writeRoots {
			argv = append(argv, "--bind", r, r)
		}
		for _, r := range readOnlyRoots {
			argv = append(argv, "--ro-bind", r, r)
		}
		argv = append(argv, "--", bin)
		argv = append(argv, args...)
	default:
		return nil, errNativeSandboxUnavailable
	}
	c := exec.CommandContext(ctx, sandbox, argv...)
	c.Dir = cwd
	c.Env = sandboxEnvironment(bin)
	if session != nil {
		c.Env = session.environment(bin)
	}
	c.WaitDelay = time.Second
	return c, nil
}

func runSandboxed(ctx context.Context, bin string, args []string, cwd string, writeRoots, readOnlyRoots []string) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	c, e := prepareSandboxed(ctx, bin, args, cwd, writeRoots, readOnlyRoots)
	if e != nil {
		return "", -1, e
	}
	c.Stdin = strings.NewReader("")
	var out, errout nativeCapture
	c.Stdout = &out
	c.Stderr = &errout
	e = c.Run()
	if ctx.Err() != nil {
		return "", -1, ctx.Err()
	}
	if out.overflow || errout.overflow {
		return "", -1, errNativeOutputLimit
	}
	code, e := nativeExitResult(e, errout.data)
	if e != nil {
		return "", -1, e
	}
	return string(out.data), code, nil
}

func nativeExitResult(e error, stderr []byte) (int, error) {
	if e == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(e, &exit) {
		diag := string(stderr)
		if strings.Contains(diag, "sandbox-exec:") || strings.Contains(diag, "bwrap:") {
			return -1, errNativeSandboxUnavailable
		}
		return exit.ExitCode(), nil
	}
	if errors.Is(e, exec.ErrNotFound) || errors.Is(e, os.ErrNotExist) || errors.Is(e, os.ErrPermission) {
		return -1, errNativeSandboxUnavailable
	}
	return -1, e
}

var _ io.Writer = (*nativeCapture)(nil)

// Read-only grants may protect an existing canonical regular file as well as a
// directory. Native packed-ref updates need a writable parent; immutable sibling
// files are individually protected instead of inheriting that write grant.
func canonicalSandboxReadOnly(path string) error {
	if !filepath.IsAbs(path) || len(path) > 4096 || strings.ContainsRune(path, 0) {
		return errors.New("invalid read-only sandbox root")
	}
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil || resolved != filepath.Clean(path) {
		return errors.New("read-only sandbox root must be canonical and exist")
	}
	st, e := os.Lstat(path)
	if e != nil || (!st.IsDir() && !st.Mode().IsRegular()) {
		return errors.New("read-only sandbox root must be an ordinary file or directory")
	}
	return nil
}
