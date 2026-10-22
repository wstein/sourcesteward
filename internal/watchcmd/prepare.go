// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/release"
	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/watch"
)

// Defaults of Spec 035.
const (
	DefaultInterval       = 60 * time.Second
	DefaultTimeout        = 590 * time.Second
	DefaultPendingTimeout = 45 * time.Minute
	DefaultDetailEvery    = 10
	DefaultHost           = "github.com"
)

var hostName = regexp.MustCompile(`\A[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]{1,5})?\z`)

// Flags are the parsed command line of `stw watch`; the *Set fields say whether a flag was given
// (flags override the config file).
type Flags struct {
	Repos         []string
	Workspace     bool
	WorkspacePath string
	Config        string
	Interval      string
	IntervalSet   bool
	StateDir      string
	Host          string
	Once          bool
	Stream        bool
	UntilEvent    []string
	Timeout       string
	TimeoutSet    bool
	Baseline      bool
	Human         bool
	MaxIdle       string
	MaxIdleSet    bool
	PendingTO     string
	PendingTOSet  bool
	DetailEvery   int
	DetailSet     bool
	Status        bool
	JSON          bool
}

// Prepared is a validated invocation: either a run or a status report.
type Prepared struct {
	Run    Params
	Status StatusParams
	IsStat bool
}

// Prepare validates the flags, loads the config file and resolves the repositories. Every error is a
// usage error (exit 2): nothing has been read from the forge or written to a state directory.
func Prepare(ctx context.Context, f Flags, stdout io.Writer) (Prepared, error) {
	var out Prepared
	if !watch.Supported {
		return out, usagef("stw watch is not supported on this platform")
	}
	host := f.Host
	if host == "" {
		host = DefaultHost
	}
	if !hostName.MatchString(host) {
		return out, usagef("--host %q is not a host name", host)
	}
	if f.StateDir != "" && !filepath.IsAbs(f.StateDir) {
		return out, usagef("--state-dir must be an absolute path")
	}
	if f.WorkspacePath != "" && !f.Workspace {
		return out, usagef("PATH is only valid with --workspace")
	}
	modes := 0
	for _, on := range []bool{f.Once, f.Stream, len(f.UntilEvent) > 0} {
		if on {
			modes++
		}
	}
	if modes > 1 {
		return out, usagef("--once, --stream and --until-event are mutually exclusive")
	}
	if f.JSON && !f.Status {
		return out, usagef("--json is only valid with --status")
	}
	if f.Status {
		if modes > 0 || f.TimeoutSet || f.Baseline || f.Human || f.MaxIdleSet || f.PendingTOSet || f.DetailSet || f.IntervalSet {
			return out, usagef("--status only takes --state-dir, --json and the repository selection of the run it reports on")
		}
	}
	cfg, err := loadConfig(ctx, f)
	if err != nil {
		return out, err
	}
	sel, err := Resolve(ctx, ResolveInput{Host: host, Repos: f.Repos, Workspace: f.Workspace, WorkspacePath: f.WorkspacePath, Config: cfg})
	if err != nil {
		if f.Status && f.StateDir != "" {
			sel = Selection{} // the report only needs the directory
		} else {
			return out, err
		}
	}
	if f.Status {
		out.IsStat = true
		out.Status = StatusParams{StateDir: StateDirFor(f.StateDir, host, sel.WorkspaceRoot, sel.Repos), JSON: f.JSON, Out: stdout}
		return out, nil
	}
	p := Params{Mode: ModeStream, Interval: DefaultInterval, DetailEvery: DefaultDetailEvery, PendingTimeout: DefaultPendingTimeout,
		MaxOpenPRs: DefaultMaxOpenPRs, Baseline: f.Baseline, Human: f.Human, Host: host, StateDir: f.StateDir,
		Repos: sel.Repos, Skipped: sel.Skipped, WorkspaceRoot: sel.WorkspaceRoot, Out: stdout}
	if cfg != nil {
		if cfg.Interval > 0 {
			p.Interval = cfg.Interval
		}
		if cfg.DetailEvery > 0 {
			p.DetailEvery = cfg.DetailEvery
		}
		if cfg.MaxOpenPRs > 0 {
			p.MaxOpenPRs = cfg.MaxOpenPRs
		}
		p.IgnoreAuthors, p.IgnoreLabels, p.StackPatterns = cfg.IgnoreAuthors, cfg.IgnoreLabels, cfg.StackPatterns
	}
	if f.IntervalSet {
		if p.Interval, err = ParseInterval(f.Interval); err != nil {
			return out, usagef("--interval: %v", err)
		}
	}
	if f.DetailSet {
		if f.DetailEvery < 1 {
			return out, usagef("--detail-every must be at least 1")
		}
		p.DetailEvery = f.DetailEvery
	}
	if f.PendingTOSet {
		d, err := time.ParseDuration(f.PendingTO)
		if err != nil || d < 0 {
			return out, usagef("--pending-timeout %q is not a duration of 0 or more", f.PendingTO)
		}
		p.PendingTimeout = d
	}
	switch {
	case f.Once:
		p.Mode = ModeOnce
	case len(f.UntilEvent) > 0:
		p.Mode = ModeUntil
		p.UntilKinds = map[string]bool{}
		for _, k := range f.UntilEvent {
			if !knownKind(k) {
				return out, usagef("--until-event: %q is not an event kind", k)
			}
			p.UntilKinds[k] = true
		}
		p.Timeout = DefaultTimeout
	}
	if f.TimeoutSet {
		if p.Mode != ModeUntil {
			return out, usagef("--timeout is only valid with --until-event")
		}
		d, err := time.ParseDuration(f.Timeout)
		if err != nil || d <= 0 {
			return out, usagef("--timeout %q must be a duration above 0", f.Timeout)
		}
		p.Timeout = d
	}
	if f.MaxIdleSet {
		if p.Mode != ModeStream {
			return out, usagef("--max-idle is only valid with --stream")
		}
		d, err := time.ParseDuration(f.MaxIdle)
		if err != nil || d < 0 {
			return out, usagef("--max-idle %q is not a duration of 0 or more", f.MaxIdle)
		}
		p.MaxIdle = d
	}
	out.Run = p
	return out, nil
}

func knownKind(k string) bool {
	for _, kind := range watch.Kinds {
		if kind == k {
			return true
		}
	}
	return false
}

// loadConfig reads --config, else (only when no --repo and no --workspace was given) the
// `.stw/watch.toml` of the workspace that contains the current directory, if it exists. The implicit
// file is project content: it must be a regular file below a real .stw directory, never a symlink or a
// FIFO (the guard of .stw/release.toml). A path named by --config is the user's choice and may be a symlink.
func loadConfig(ctx context.Context, f Flags) (*Config, error) {
	path, implicit := f.Config, false
	if path == "" {
		if len(f.Repos) > 0 || f.Workspace {
			return nil, nil
		}
		ws, err := stwsync.Discover(ctx, ".")
		if err != nil {
			return nil, nil
		}
		dir := filepath.Join(ws.Root, ".stw")
		path, implicit = filepath.Join(dir, "watch.toml"), true
		if _, err := os.Lstat(path); err != nil {
			return nil, nil
		}
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
			return nil, usagef("%s is not a real directory; watch.toml is not read", dir)
		}
	}
	raw, err := readConfigFile(path, implicit)
	if err != nil {
		return nil, usagef("--config: %v", err)
	}
	c, err := ParseConfig(raw)
	if err != nil {
		return nil, usagef("%s: %v", path, strings.TrimPrefix(err.Error(), "watch.toml: "))
	}
	return &c, nil
}

// readConfigFile reads at most MaxContractBytes of a regular file without ever blocking on a FIFO.
func readConfigFile(path string, noLinks bool) ([]byte, error) {
	flags := os.O_RDONLY | openNonBlock
	if noLinks {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is a symbolic link or not a regular file", path)
		}
		flags |= openNoFollow
	}
	file, err := os.OpenFile(path, flags, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	raw, err := io.ReadAll(io.LimitReader(file, release.MaxContractBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s", path)
	}
	return raw, nil
}
