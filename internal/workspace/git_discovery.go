// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// appleGitLauncher is the macOS /usr/bin/git shim. It cannot run inside the required
// executable sandbox, so it is never a candidate.
const appleGitLauncher = "/usr/bin/git"

// gitFallbacks are fixed macOS locations tried after PATH and the Command Line Tools.
var gitFallbacks = []string{"/opt/homebrew/bin/git", "/usr/local/bin/git"}

// gitAttempt records one candidate. An empty Reason marks the chosen one.
type gitAttempt struct{ Path, Source, Reason string }

// gitDiscovery selects a native Git on macOS without the caller changing PATH. Order:
// PATH entries (never the Apple launcher), $(xcode-select -p)/usr/bin/git, Fallbacks.
// Every candidate must pass Probe, which runs `git --version` in the sandbox.
type gitDiscovery struct {
	PathDirs  []string
	Apple     string
	Developer func() (string, error)
	Fallbacks []string
	Probe     func(path string) error
}

func (d gitDiscovery) find() (string, []gitAttempt) {
	var attempts []gitAttempt
	seen := map[string]bool{}
	try := func(candidate, source string, listMissing bool) string {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			resolved, err = filepath.Abs(resolved)
		}
		st, serr := os.Stat(candidate)
		if err != nil || serr != nil {
			if listMissing {
				attempts = append(attempts, gitAttempt{candidate, source, "not found"})
			}
			return ""
		}
		if seen[resolved] {
			return ""
		}
		seen[resolved] = true
		switch {
		case resolved == d.Apple:
			attempts = append(attempts, gitAttempt{resolved, source, "Apple launcher; it cannot run inside the executable sandbox"})
		case !st.Mode().IsRegular() || st.Mode().Perm()&0o111 == 0:
			attempts = append(attempts, gitAttempt{resolved, source, "not an executable file"})
		default:
			if perr := d.Probe(resolved); perr != nil {
				attempts = append(attempts, gitAttempt{resolved, source, "probe failed: " + perr.Error()})
				return ""
			}
			attempts = append(attempts, gitAttempt{Path: resolved, Source: source})
			return resolved
		}
		return ""
	}
	for _, dir := range d.PathDirs {
		if !filepath.IsAbs(dir) {
			continue
		}
		if p := try(filepath.Join(dir, "git"), "PATH", false); p != "" {
			return p, attempts
		}
	}
	if d.Developer != nil {
		if dev, err := d.Developer(); err == nil && dev != "" {
			if p := try(filepath.Join(dev, "usr", "bin", "git"), "xcode-select", true); p != "" {
				return p, attempts
			}
		} else {
			attempts = append(attempts, gitAttempt{"$(xcode-select -p)/usr/bin/git", "xcode-select", "xcode-select -p gave no developer directory"})
		}
	}
	for _, c := range d.Fallbacks {
		if p := try(c, "fallback", true); p != "" {
			return p, attempts
		}
	}
	return "", attempts
}

// gitDiscoveryError names every candidate with its reason and one fix command.
func gitDiscoveryError(attempts []gitAttempt) error {
	const fix = "fix: brew install git (or xcode-select --install), then run stw again; PATH needs no change"
	var parts []string
	onlyMissing := true
	for _, a := range attempts {
		parts = append(parts, fmt.Sprintf("%s [%s]: %s", a.Path, a.Source, a.Reason))
		if a.Reason != "not found" && !strings.HasPrefix(a.Reason, "xcode-select") {
			onlyMissing = false
		}
	}
	list := strings.Join(parts, "; ")
	if onlyMissing {
		return failure("git-unavailable", "no native Git executable found; candidates tried: "+list+"; "+fix)
	}
	return failure("git-sandbox-unavailable", "no native Git executable can run inside the required executable sandbox; candidates tried: "+list+"; "+fix)
}

func xcodeDeveloperDir() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/xcode-select", "-p").Output()
	return strings.TrimSpace(string(out)), err
}

// probeGitInSandbox runs `git --version` in the same sandbox as every native command.
func probeGitInSandbox(bin string) error {
	cwd, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, code, err := runSandboxed(ctx, bin, []string{"--version"}, cwd, nil, nil)
	switch {
	case err != nil && errors.Is(err, errNativeSandboxUnavailable):
		return errors.New("sandbox could not start it")
	case err != nil:
		return err
	case code != 0:
		return fmt.Errorf("exit status %d", code)
	case !strings.HasPrefix(out, "git version "):
		return errors.New("unexpected --version output")
	}
	return nil
}

var (
	gitChosenMu sync.Mutex
	gitChosen   = map[string]string{}
)

// discoverNativeGit is the macOS lookup; the choice is cached per PATH value.
func discoverNativeGit() (string, error) {
	path := os.Getenv("PATH")
	gitChosenMu.Lock()
	defer gitChosenMu.Unlock()
	if p, ok := gitChosen[path]; ok {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p, nil
		}
	}
	d := gitDiscovery{PathDirs: filepath.SplitList(path), Apple: appleGitLauncher,
		Developer: xcodeDeveloperDir, Fallbacks: gitFallbacks, Probe: probeGitInSandbox}
	p, attempts := d.find()
	if p == "" {
		return "", gitDiscoveryError(attempts)
	}
	gitChosen[path] = p
	return p, nil
}
