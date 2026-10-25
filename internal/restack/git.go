// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// The Git runner of the plan. Every command is a fixed argv started directly (no shell) in the
// worktree root with an environment built from scratch: no inherited GIT_* variable, no global or
// system configuration, no hooks, pager, editor, prompt, external diff or network. Object writes
// go to the private quarantine directory; the repository objects are alternates for reading.

const (
	commandOutputLimit = 8 << 20
	patchOutputLimit   = 32 << 20
)

// errOutputBound is returned when a command writes more than its documented limit.
var errOutputBound = errors.New("command output exceeds its bound")

// configArgs pin every setting on which output parsing or the replay depends and disable the
// helpers a repository configuration could otherwise start.
var configArgs = []string{
	"-c", "core.hooksPath=" + os.DevNull,
	"-c", "core.fsmonitor=false",
	"-c", "core.autocrlf=false",
	"-c", "core.quotePath=true",
	"-c", "submodule.recurse=false",
	"-c", "merge.renormalize=false",
	"-c", "merge.renames=true",
	"-c", "merge.directoryRenames=conflict",
	"-c", "merge.renameLimit=1000",
	"-c", "diff.renames=false",
	"-c", "diff.external=",
	"-c", "diff.algorithm=myers",
	"-c", "diff.indentHeuristic=true",
	"-c", "diff.noprefix=false",
	"-c", "diff.mnemonicPrefix=false",
	"-c", "diff.context=3",
	"-c", "diff.interHunkContext=0",
	"-c", "diff.colorMoved=false",
	"-c", "diff.wsErrorHighlight=none",
	"-c", "diff.orderFile=" + os.DevNull,
	"-c", "diff.relative=false",
	"-c", "diff.ignoreSubmodules=none",
	"-c", "gc.auto=0",
	"-c", "maintenance.auto=false",
	"-c", "protocol.ext.allow=never",
	"-c", "log.showSignature=false",
	"-c", "log.decorate=no",
	"-c", "log.mailmap=false",
	"-c", "color.ui=never",
	"-c", "i18n.logOutputEncoding=UTF-8",
}

// Fixed identity of the quarantine replay commits (FR-015). They are never pushed and carry no
// claim about the branch.
const (
	replayCommitterName  = "stw-restack-plan"
	replayCommitterEmail = "restack-plan@invalid"
	replayCommitterDate  = "1970-01-01T00:00:00Z"
)

// emptyTree is the id of the empty tree of a SHA-1 repository. Git knows it without an object in
// the store. It is the default attribute source of every prediction and evidence command
// (issue 241): none reads a .gitattributes file of the worktree, the index or the repository,
// which belong to the owner and are in no sampled commit. A command that needs attributes names
// the sampled tree itself; only the dirty check (runInWorktree) reads the real worktree,
// because it reports its state.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

type gitRunner struct {
	bin, dir, home string
	objectDir      string // private quarantine
	alternates     string // repository objects, read only
	now            func() time.Time
	// worktreeAttributes lets a command that reports the REAL worktree state (git status) read the
	// attributes of the worktree: the empty-tree pin would change what counts as modified.
	worktreeAttributes bool
}

type gitOutput struct {
	out  []byte
	code int
}

func (g *gitRunner) environment(extra []string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"), "LC_ALL=C", "HOME=" + g.home, "XDG_CONFIG_HOME=" + g.home,
		"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_SYSTEM=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_ATTR_NOSYSTEM=1",
		"GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_PAGER=cat", "PAGER=cat", "GIT_EDITOR=:", "GIT_ASKPASS=",
	}
	if !g.worktreeAttributes {
		env = append(env, "GIT_ATTR_SOURCE="+emptyTree)
	}
	if g.objectDir != "" {
		env = append(env, "GIT_OBJECT_DIRECTORY="+g.objectDir, "GIT_ALTERNATE_OBJECT_DIRECTORIES="+g.alternates)
	}
	return append(env, extra...)
}

// start runs one command; plain omits the configuration pins (used only to read the
// configuration itself, where command-line entries would be listed too).
func (g *gitRunner) start(ctx context.Context, plain bool, in []byte, extra []string, limit int64, args ...string) (gitOutput, error) {
	if err := ctx.Err(); err != nil {
		return gitOutput{}, err
	}
	full := []string{"--no-optional-locks", "--no-replace-objects", "--no-pager"}
	if !plain {
		full = append(full, configArgs...)
	}
	cmd := exec.CommandContext(ctx, g.bin, append(full, args...)...)
	cmd.Dir = g.dir
	cmd.Env = g.environment(extra)
	cmd.Stdin = bytes.NewReader(in)
	cmd.WaitDelay = time.Second
	out := &limitedBuffer{limit: limit}
	cmd.Stdout = out
	cmd.Stderr = &limitedBuffer{limit: 64 << 10, discard: true}
	err := cmd.Run()
	if ctx.Err() != nil {
		return gitOutput{}, ctx.Err()
	}
	if out.over {
		return gitOutput{}, errOutputBound
	}
	var exit *exec.ExitError
	switch {
	case err == nil:
		return gitOutput{out.Bytes(), 0}, nil
	case errors.As(err, &exit):
		return gitOutput{out.Bytes(), exit.ExitCode()}, nil
	}
	return gitOutput{}, err
}

// runInWorktree runs a command that must see the real worktree state, attributes included.
func (g *gitRunner) runInWorktree(ctx context.Context, args ...string) (gitOutput, error) {
	u := *g
	u.worktreeAttributes = true
	return u.start(ctx, false, nil, nil, commandOutputLimit, args...)
}

func (g *gitRunner) run(ctx context.Context, args ...string) (gitOutput, error) {
	return g.start(ctx, false, nil, nil, commandOutputLimit, args...)
}

// must runs a command whose exit status has to be 0 and returns its output.
func (g *gitRunner) must(ctx context.Context, args ...string) ([]byte, error) {
	o, err := g.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	if o.code != 0 {
		return nil, errCommandFailed
	}
	return o.out, nil
}

var errCommandFailed = errors.New("git command failed")

// text runs a command that prints one line and returns it without the line end.
func (g *gitRunner) text(ctx context.Context, args ...string) (string, error) {
	out, err := g.must(ctx, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit   int64
	over    bool
	discard bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if left := b.limit - int64(b.Len()); int64(len(p)) > left {
		b.over = !b.discard
		p = p[:max(left, 0)]
	}
	b.Buffer.Write(p)
	return n, nil
}

// locateGit finds the Git executable on PATH; it starts nothing.
func locateGit() (string, error) { return exec.LookPath("git") }
