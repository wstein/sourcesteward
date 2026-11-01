// SPDX-License-Identifier: EUPL-1.2
package lintprcmd

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/lintpr"
)

// gitConfigIdentity reads user.name and user.email with two read-only `git config --get` calls in dir.
// It never writes a configuration.
func gitConfigIdentity(ctx context.Context, dir string) (lintpr.Identity, bool) {
	git, err := exec.LookPath("git")
	if err != nil {
		return lintpr.Identity{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	get := func(key string) string {
		cmd := exec.CommandContext(ctx, git, "-C", dir, "config", "--get", key)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	id := lintpr.Identity{Name: get("user.name"), Email: get("user.email")}
	return id, id.Name != "" && id.Email != ""
}
