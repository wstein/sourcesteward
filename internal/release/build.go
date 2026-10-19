// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// BuildCommand is the complete, shell-free invocation passed to the runner.
// It contains only the constructed environment, never the inherited environment.
type BuildCommand struct {
	Argv, Env []string
	Dir       string
	Timeout   time.Duration
}

// buildRunner is private: tests inject a fake and production uses projectRunner only
// through BuildRelease. Read-only commands cannot select or configure this runner.
type buildRunner interface {
	Run(context.Context, BuildCommand, io.Writer, io.Writer) (int, error)
}
type projectRunner struct{}

func (projectRunner) Run(ctx context.Context, c BuildCommand, stdout, stderr io.Writer) (int, error) {
	if len(c.Argv) == 0 {
		return -1, errors.New("empty project argv")
	}
	executable, err := buildExecutable(c)
	if err != nil {
		return -1, err
	}
	cmd := exec.CommandContext(ctx, executable, c.Argv[1:]...)
	cmd.Args[0] = c.Argv[0] // Path is resolved, but argv remains exactly the declared vector.
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = c.Dir, c.Env, stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	// WaitDelay can fail after the leader exits while descendants still own the
	// output pipes. Cancel is not called in that case, so clean the whole group.
	if err != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	if ctx.Err() != nil {
		return -1, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

type BuildInvocationResult struct {
	Target          *string `json:"target"`
	Exit            int     `json:"exit"`
	DurationSeconds float64 `json:"duration_seconds"`
	StdoutTruncated bool    `json:"stdout_truncated"`
	StderrTruncated bool    `json:"stderr_truncated"`
	Stdout          string  `json:"stdout"`
	Stderr          string  `json:"stderr"`
	err             error
}
type boundedBuildStream struct {
	bytes     []byte
	truncated bool
}

func (w *boundedBuildStream) Write(p []byte) (int, error) {
	n := len(p)
	remain := (1 << 20) - len(w.bytes)
	if len(p) > remain {
		w.truncated = true
		p = p[:remain]
	}
	w.bytes = append(w.bytes, p...)
	return n, nil
}
func runBuildCommand(ctx context.Context, runner buildRunner, c BuildCommand) BuildInvocationResult {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	var out, errout boundedBuildStream
	start := time.Now()
	exit, err := runner.Run(ctx, c, &out, &errout)
	if ctx.Err() != nil {
		err = ctx.Err()
		exit = -1
	}
	return BuildInvocationResult{Exit: exit, DurationSeconds: time.Since(start).Seconds(), StdoutTruncated: out.truncated, StderrTruncated: errout.truncated, Stdout: string(out.bytes), Stderr: string(errout.bytes), err: err}
}
func buildSubstitute(text string, values map[string]string) string {
	return ExpandPlaceholders(text, values)
}

func buildEnvironment(b Build, values map[string]string, inherited []string, epoch int64) ([]string, error) {
	env := map[string]string{}
	parent := map[string]string{}
	for _, entry := range inherited {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			parent[key] = value
		}
	}
	for _, name := range b.EnvInherit {
		if forbiddenEnvironmentName(name) != "" {
			return nil, fmt.Errorf("forbidden environment name %s", name)
		}
		if value, ok := parent[name]; ok {
			env[name] = value
		}
	}
	for _, entry := range b.EnvSet {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || forbiddenEnvironmentName(name) != "" {
			return nil, errors.New("invalid or forbidden environment entry")
		}
		env[name] = buildSubstitute(value, values)
	}
	env["SOURCE_DATE_EPOCH"], env["TZ"], env["LC_ALL"] = strconv.FormatInt(epoch, 10), "UTC", "C"
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name+"="+env[name])
	}
	return result, nil
}

// buildWorkingDir rejects every symlink component, including the final directory.
func buildWorkingDir(root, path string) (string, error) {
	if path == "" {
		path = "."
	}
	if filepath.IsAbs(path) || strings.Contains(path, "\\") || strings.IndexByte(path, 0) >= 0 {
		return "", errors.New("unsafe working directory")
	}
	current := root
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return "", errors.New("unsafe working directory")
		}
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("working directory must be a real directory")
		}
	}
	return current, nil
}

// buildOpenRegular avoids blocking on FIFOs and rejects a replaced final symlink.
func buildOpenRegular(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}

// exec.Command resolves a bare argv[0] against os.Getenv("PATH") before Env is
// assigned. Resolve it explicitly so an undeclared host tool cannot be selected.
func buildExecutable(c BuildCommand) (string, error) {
	if len(c.Argv) == 0 || c.Argv[0] == "" {
		return "", errors.New("empty executable")
	}
	name := c.Argv[0]
	if strings.ContainsRune(name, os.PathSeparator) {
		return name, nil
	}
	search := ""
	present := false
	for _, entry := range c.Env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && key == "PATH" {
			search, present = value, true
		}
	}
	if !present {
		return "", errors.New("bare executable requires declared PATH")
	}
	for _, dir := range filepath.SplitList(search) {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(c.Dir, dir)
		}
		candidate := filepath.Join(dir, name)
		st, err := os.Stat(candidate)
		if err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("executable %s absent from declared PATH", name)
}

// These are fixed native Git reads, separate from the project runner. Streaming
// prevents source-tree volume from becoming an invented export capability cut.
type buildGitStderr struct {
	bytes    []byte
	overflow bool
	cancel   context.CancelFunc
}

func (w *buildGitStderr) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (64 << 10) - len(w.bytes)
	if len(p) > remaining {
		p = p[:remaining]
		w.overflow = true
		w.cancel()
	}
	w.bytes = append(w.bytes, p...)
	return n, nil
}
func withBuildGitRead(ctx context.Context, root string, args []string, consume func(io.Reader) error) error {
	return withBuildGitReadPolicy(ctx, root, args, false, consume)
}
func withBuildGitReadPolicy(ctx context.Context, root string, args []string, strictDiagnostics bool, consume func(io.Reader) error) error {
	ctx, cancel := context.WithTimeout(ctx, gitDeadline)
	defer cancel()
	cmd := gitCommand(ctx, root, args...)
	stderr := buildGitStderr{cancel: cancel}
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	readErr := consume(stdout)
	if readErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if stderr.overflow {
		return fmt.Errorf("%w: native stderr exceeds limit", errReleaseLimit)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ctx.Err()
	}
	if strictDiagnostics && len(stderr.bytes) != 0 {
		return errors.New("native configuration read reported diagnostics")
	}
	if readErr != nil {
		return readErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return waitErr
}
func withBuildGitBatch(ctx context.Context, root string, consume func(*bufio.Reader, io.Writer) error) error {
	ctx, cancel := context.WithTimeout(ctx, gitDeadline)
	defer cancel()
	cmd := gitCommand(ctx, root, "cat-file", "--batch")
	stderr := buildGitStderr{cancel: cancel}
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(stdout, 64<<10)
	readErr := consume(reader, stdin)
	closeErr := stdin.Close()
	if readErr != nil || closeErr != nil {
		cancel()
	} else {
		extra, err := reader.ReadByte()
		if err != io.EOF {
			readErr = fmt.Errorf("unexpected export batch tail %x: %v", extra, err)
			cancel()
		}
	}
	waitErr := cmd.Wait()
	if stderr.overflow {
		return fmt.Errorf("%w: native stderr exceeds limit", errReleaseLimit)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ctx.Err()
	}
	if readErr != nil {
		return readErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if closeErr != nil {
		return closeErr
	}
	return waitErr
}

func hashBuildGitBlob(ctx context.Context, root, id string) (string, error) {
	hash := sha256.New()
	err := withBuildGitRead(ctx, root, []string{"cat-file", "blob", id}, func(reader io.Reader) error { _, err := io.Copy(hash, reader); return err })
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Diff may consult repository-local conversion drivers even with hooks and
// fsmonitor disabled. Inspect effective configuration (including include and
// worktree files) before asking Git to compare checkout contents. Empty driver
// definitions are inert. Nonempty drivers require project execution and are
// refused here rather than run with the native-read environment.
func checkBuildConversionHelpers(ctx context.Context, root string) error {
	records := 0
	err := withBuildGitReadPolicy(ctx, root, []string{"config", "--includes", "--null", "--get-regexp", `^filter\..*\.(clean|process)$`}, true, func(reader io.Reader) error {
		var err error
		records, err = inspectBuildConversionConfig(reader)
		return err
	})
	var exit *exec.ExitError
	if records == 0 && errors.As(err, &exit) && exit.ExitCode() == 1 {
		return nil // git config reports no matching keys with exit 1.
	}
	return err
}

// Retain only the fixed prefix and suffix of each key. Configuration volume and
// driver values never accumulate in memory; one nonempty value is decisive.
func inspectBuildConversionConfig(reader io.Reader) (int, error) {
	r := bufio.NewReader(reader)
	records := 0
	for {
		prefix, suffix := "", ""
		length := 0
		for {
			b, err := r.ReadByte()
			if err == io.EOF && length == 0 {
				return records, nil
			}
			if err != nil || b == 0 {
				return records, errors.New("malformed checkout conversion helper configuration")
			}
			if b == '\n' {
				break
			}
			length++
			if len(prefix) < len("filter.") {
				prefix += string(b)
			}
			suffix += string(b)
			if len(suffix) > len(".process") {
				suffix = suffix[1:]
			}
		}
		if prefix != "filter." || length <= len("filter.")+len(".clean") || (!strings.HasSuffix(suffix, ".clean") && suffix != ".process") {
			return records, errors.New("malformed checkout conversion helper configuration")
		}
		value, err := r.ReadByte()
		if err != nil {
			return records, errors.New("unterminated checkout conversion helper configuration")
		}
		records++
		if value != 0 {
			return records, errors.New("configured checkout conversion helper requires explicit project execution")
		}
	}
}

// Git's root diff can recurse into gitlink checkouts to observe their dirtiness.
// Complete helper screening throughout those initialized checkouts before the
// first comparison; a child must not get an unscreened native environment.
func checkBuildCheckoutHelpers(ctx context.Context, root string) error {
	if err := checkBuildConversionHelpers(ctx, root); err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	if err := checkBuildWorktreeRoot(ctx, root); err != nil {
		return err
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return withBuildGitRead(ctx, root, []string{"ls-files", "--stage", "-z", "--"}, func(reader io.Reader) error {
		listing := bufio.NewReaderSize(reader, 64<<10)
		for {
			record, err := listing.ReadSlice(0)
			if err == io.EOF && len(record) == 0 {
				return nil
			}
			if err != nil {
				return errors.New("invalid tracked checkout metadata")
			}
			meta, name, ok := strings.Cut(string(record[:len(record)-1]), "\t")
			fields := strings.Fields(meta)
			if !ok || len(fields) != 3 || !IsFullCommitID(fields[1]) || fields[2] != "0" {
				return errors.New("unresolved or invalid tracked checkout metadata")
			}
			if fields[0] != "160000" {
				continue
			}
			if !filepath.IsLocal(name) {
				return errors.New("unsafe nested checkout path")
			}
			if _, err := dir.Lstat(filepath.Join(name, ".git")); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return err
			}
			child := filepath.Join(root, name)
			actual, err := filepath.EvalSymlinks(child)
			if err != nil {
				return err
			}
			if actual != child {
				return errors.New("nested checkout path traverses a symlink")
			}
			if err := checkBuildCheckoutHelpers(ctx, child); err != nil {
				return err
			}
		}
	})
}

// Bind native index interpretation to the same canonical checkout whose paths
// we screen. In particular, a nested repository may configure a different
// worktree; its index paths must never be joined to the caller's directory.
func checkBuildWorktreeRoot(ctx context.Context, root string) error {
	return withBuildGitReadPolicy(ctx, root, []string{"rev-parse", "--path-format=absolute", "--show-toplevel"}, true, func(reader io.Reader) error {
		raw, err := io.ReadAll(io.LimitReader(reader, 65537))
		if err != nil {
			return err
		}
		if len(raw) > 65536 {
			return fmt.Errorf("%w: native worktree path exceeds limit", errReleaseLimit)
		}
		if len(raw) == 0 || raw[len(raw)-1] != '\n' {
			return errors.New("invalid native worktree root")
		}
		path := string(raw[:len(raw)-1])
		if !filepath.IsAbs(path) {
			return errors.New("native worktree root is not absolute")
		}
		actual, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		if actual != root {
			return errors.New("effective Git worktree differs from the expected checkout path")
		}
		return nil
	})
}

// Final metadata opens must not follow a swapped symlink or block on a FIFO.
// Parent Root acquisition is descriptor-relative; its descriptor stays pinned.
//
// os.Root resolves in-root symlinks itself, so the O_NOFOLLOW flag alone does not
// refuse a final symlink. The opened descriptor is therefore bound to the name
// again: a final component that is (or became) a symlink, or that is not the
// opened file, is refused.
func openBuildViewFile(parent *os.Root, name string) (*os.File, error) {
	file, err := parent.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	named, err := parent.Lstat(name)
	if err != nil || named.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, named) {
		file.Close()
		return nil, errors.New("checkout metadata final component is not the opened file")
	}
	return file, nil
}

// runBuildViewNative retains an unreaped direct leader until its entire owned
// group and both output streams reach terminal state. exec.CommandContext/Run
// would reap before descendant cleanup, losing the identity that pins the PGID.
var buildViewDrainGrace = 500 * time.Millisecond

type buildViewNativeHooks struct {
	pipes   func(*os.File, *os.File)
	started func(int)
}

func runBuildViewNative(parent context.Context, c buildViewCommand, out io.Writer, testHooks ...buildViewNativeHooks) (int, []byte, error) {
	var hooks buildViewNativeHooks
	if len(testHooks) > 0 {
		hooks = testHooks[0]
	}
	ctx, cancel := context.WithTimeout(parent, gitDeadline)
	defer cancel()
	cmd := exec.Command(c.Executable, c.Args...)
	cmd.Dir, cmd.Env, cmd.Stdin = c.Dir, c.Env, c.Stdin
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return -1, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdout.Close()
		return -1, nil, err
	}
	if err := ctx.Err(); err != nil {
		stdout.Close()
		stderr.Close()
		return -1, nil, err
	}
	if hooks.pipes != nil {
		hooks.pipes(cmd.Stdout.(*os.File), cmd.Stderr.(*os.File))
	}
	if err := cmd.Start(); err != nil {
		stdout.Close()
		stderr.Close()
		return -1, nil, err
	}
	pid := cmd.Process.Pid
	if hooks.started != nil {
		hooks.started(pid)
	}
	diagnostic := &buildViewDiagnostic{cancel: cancel}
	streams := make(chan error, 2)
	go func() { _, e := io.CopyBuffer(out, stdout, make([]byte, 32<<10)); stdout.Close(); streams <- e }()
	go func() { _, e := io.CopyBuffer(diagnostic, stderr, make([]byte, 16<<10)); stderr.Close(); streams <- e }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	drained := 0
	var streamErr, cleanupErr error
	killed, closed := false, false
	var killedAt time.Time
	for {
		if ctx.Err() != nil || streamErr != nil {
			if !killed {
				e := syscall.Kill(-pid, syscall.SIGKILL)
				if e != nil && !errors.Is(e, syscall.ESRCH) {
					if cleanupErr == nil {
						cleanupErr = errors.New("native group cleanup signal failed")
					}
				} else {
					killed, killedAt = true, time.Now()
				}
			}
		}
		// A descendant that left the group (setsid) can keep an output pipe open for
		// good. After the kill and a short grace the read ends are closed, so the
		// wait is bounded; the job is refused, never accepted with partial output.
		if killed && !closed && time.Since(killedAt) > buildViewDrainGrace {
			closed = true
			stdout.Close()
			stderr.Close()
			cleanupErr = errors.Join(cleanupErr, errors.New("native output held open outside the group"))
		}
		state, e := observeBuildViewGroup(pid)
		if e != nil { // Observation failure never authorizes a reap or lease release.
			if cleanupErr == nil {
				cleanupErr = errors.New("native group observation incomplete")
			}
			cancel()
		} else if state.leaderTerminal {
			if state.members == 1 && drained == 2 {
				break
			}
			if state.members > 1 && !killed {
				cleanupErr = errors.Join(cleanupErr, errors.New("native group outlived its leader"))
				cancel()
			}
		}
		select {
		case e := <-streams:
			drained++
			if e != nil {
				streamErr = errors.Join(streamErr, e)
				cancel()
			}
		case <-ticker.C:
		}
	}
	// No signal is sent to this numeric PGID after the following reap.
	waitErr := cmd.Wait()
	if diagnostic.overflow {
		return -1, diagnostic.prefix, fmt.Errorf("%w: native diagnostics exceed limit", errReleaseLimit)
	}
	if parent.Err() != nil {
		return -1, diagnostic.prefix, parent.Err()
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return -1, diagnostic.prefix, ctx.Err()
	}
	if streamErr != nil || cleanupErr != nil {
		return -1, diagnostic.prefix, errors.Join(streamErr, cleanupErr)
	}
	var exit *exec.ExitError
	if errors.As(waitErr, &exit) {
		return exit.ExitCode(), diagnostic.prefix, nil
	}
	if waitErr != nil {
		return -1, diagnostic.prefix, waitErr
	}
	return 0, diagnostic.prefix, nil
}

type buildViewDiagnostic struct {
	prefix   []byte
	overflow bool
	cancel   context.CancelFunc
}

func (w *buildViewDiagnostic) Write(p []byte) (int, error) {
	n := len(p)
	left := (64 << 10) - len(w.prefix)
	if len(p) > left {
		p = p[:left]
		w.overflow = true
		w.cancel()
	}
	w.prefix = append(w.prefix, p...)
	return n, nil
}

// Trusted route replacement retains the native parent's owned process group.
// It never resolves argv through PATH or starts an independent child/session.
func execBuildViewRoute(c buildViewCommand) error {
	return syscall.Exec(c.Executable, append([]string{c.Executable}, c.Args...), c.Env)
}
