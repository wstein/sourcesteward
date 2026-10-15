// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

const outputLimit = 64 * 1024

type capture struct {
	buffer   bytes.Buffer
	overflow bool
}

func (c *capture) Len() int       { return c.buffer.Len() }
func (c *capture) String() string { return c.buffer.String() }

func (c *capture) Write(p []byte) (int, error) {
	n := len(p)
	left := outputLimit - c.Len()
	if n > left {
		c.overflow = true
		p = p[:left]
	}
	c.buffer.Write(p)
	return n, nil
}

type runner func(context.Context, string, ...string) (string, int, error)

func gitEnvironment() []string {
	env := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			env = append(env, entry)
		}
	}
	return append(env, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_PAGER=cat")
}
func runGit(ctx context.Context, dir string, args ...string) (string, int, error) {
	c := exec.CommandContext(ctx, "git", append([]string{"--no-replace-objects", "-c", "core.fsmonitor=false"}, args...)...)
	c.Dir = dir
	c.Env = gitEnvironment()
	c.Stdin = strings.NewReader("")
	c.WaitDelay = time.Second
	var out, errout capture
	c.Stdout = &out
	c.Stderr = &errout
	err := c.Run()
	if ctx.Err() != nil {
		return "", -1, ctx.Err()
	}
	if out.overflow || errout.overflow {
		return "", -1, errors.New("Git output exceeded inspection limit")
	}
	if err == nil {
		return out.String(), 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out.String(), exit.ExitCode(), nil
	}
	return "", -1, err
}
