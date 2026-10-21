// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	outputLimit = 4 << 20
	gitTimeout  = 5 * time.Minute
)

var errOutputLimit = errors.New("git output exceeds the bound")

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > outputLimit {
		return 0, errOutputLimit
	}
	return b.Buffer.Write(p)
}

// repositoryVariables select a repository or its storage and would redirect a command.
var repositoryVariables = map[string]bool{
	"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true, "GIT_COMMON_DIR": true,
	"GIT_OBJECT_DIRECTORY": true, "GIT_ALTERNATE_OBJECT_DIRECTORIES": true, "GIT_NAMESPACE": true, "GIT_PREFIX": true,
}

func gitEnvironment() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !repositoryVariables[name] {
			env = append(env, kv)
		}
	}
	// No prompt, no index refresh by read commands, and a stable message language.
	return append(env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
}

// git runs one Git command with a fixed argument vector in dir. Arguments never come from repository
// content except as the value after a fixed option or after "--end-of-options". The exit code is
// part of the result for any normal exit; an error means Git could not run or was cut off.
func git(ctx context.Context, bin, dir string, args ...string) (gitOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	// gc.auto and maintenance.auto keep a fetch from leaving a background process behind.
	full := append([]string{"-c", "gc.auto=0", "-c", "maintenance.auto=false", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, bin, full...)
	cmd.Env = gitEnvironment()
	var out, diagnostic limitedBuffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	err := cmd.Run()
	result := gitOutput{Out: out.String(), Diagnostic: diagnostic.String()}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		result.Code = exit.ExitCode()
		return result, nil
	}
	return result, err
}

// gitOutput is the outcome of a normal Git exit: standard output, standard error and the exit code.
type gitOutput struct {
	Out, Diagnostic string
	Code            int
}
