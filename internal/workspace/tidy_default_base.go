// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"strings"
)

const (
	originHeadRef     = "refs/remotes/origin/HEAD"
	originNamespace   = "refs/remotes/origin/"
	setOriginHeadHint = "git remote set-head origin -a"
)

// DefaultTidyBase returns the integration base `stw tidy plan` uses without --base (Spec 053, issue
// 389): the target of the symbolic reference refs/remotes/origin/HEAD, for example
// refs/remotes/origin/main. It reads references through the sandboxed native Git runner, makes no
// network call and writes nothing. When origin/HEAD is missing, not symbolic, dangling or outside the
// origin namespace, the error names the command that sets it. A path outside a Git repository
// keeps the old requirement of an explicit --base.
func DefaultTidyBase(ctx context.Context, path string) (string, error) {
	missing := errors.New("no --base given and " + originHeadRef + " is not set to a branch that exists; run `" + setOriginHeadHint + "` (needs the remote origin) or pass --base REF")
	bin, err := gitExecutable()
	if err != nil {
		return "", err
	}
	ctx, _, closeSession, err := withPlanningSession(ctx)
	if err != nil {
		return "", err
	}
	defer closeSession()
	out, code, err := runSandboxed(ctx, bin, []string{"--no-replace-objects", "rev-parse", "--git-dir"}, path, nil, nil)
	if err != nil {
		return "", err
	}
	if code != 0 || strings.TrimSpace(out) == "" {
		return "", errors.New("--base is required: the default base needs a Git repository whose " + originHeadRef + " is set")
	}
	out, code, err = runSandboxed(ctx, bin, []string{"--no-replace-objects", "symbolic-ref", "--quiet", originHeadRef}, path, nil, nil)
	if err != nil {
		return "", err
	}
	target := strings.TrimSuffix(out, "\n")
	if code != 0 || !strings.HasPrefix(target, originNamespace) || len(target) == len(originNamespace) || !validText(target) || target == originHeadRef {
		return "", missing
	}
	_, code, err = runSandboxed(ctx, bin, []string{"--no-replace-objects", "rev-parse", "--verify", "--quiet", target + "^{commit}"}, path, nil, nil)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", missing
	}
	return target, nil
}
