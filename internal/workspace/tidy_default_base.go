// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"strings"

	"github.com/wstein/sourcesteward/internal/inspection"
)

const (
	originHeadRef     = "refs/remotes/origin/HEAD"
	originNamespace   = "refs/remotes/origin/"
	setOriginHeadHint = "git remote set-head origin -a"
)

// defaultTidyInspect is the ownership inspection of the path; a variable only for tests.
var defaultTidyInspect = inspection.Inspect

// DefaultTidyBase returns the integration base `stw tidy plan` uses without --base (Spec 053, issue
// 389): the target of the symbolic reference refs/remotes/origin/HEAD, for example
// refs/remotes/origin/main. It reads references through the sandboxed native Git runner, makes no
// network call and writes nothing. When origin/HEAD is missing, not symbolic, dangling or outside the
// origin namespace, the error names the command that sets it. A path outside a Git repository,
// or in a jj repository, keeps the old requirement of an explicit --base.
func DefaultTidyBase(ctx context.Context, path string) (string, error) {
	missing := errors.New("no --base given and origin/HEAD is not set to an existing branch below " + originNamespace + "; run `" + setOriginHeadHint + "` (needs the remote origin) or pass --base REF")
	bin, err := gitExecutable()
	if err != nil {
		return "", err
	}
	ctx, _, closeSession, err := withPlanningSession(ctx)
	if err != nil {
		return "", err
	}
	defer closeSession()
	notARepository := errors.New("--base is required: the default base needs a Git repository whose " + originHeadRef + " is set")
	source, err := canonicalSource(path)
	if err != nil {
		return "", notARepository
	}
	path = source
	// Only the Git backend gets a default base: a jj repository, colocated or not, keeps requiring
	// --base (Spec 053 section 4).
	if identity := defaultTidyInspect(ctx, source); identity.Backend != "git" {
		return "", notARepository
	}
	out, code, err := runSandboxed(ctx, bin, []string{"--no-replace-objects", "rev-parse", "--git-dir"}, path, nil, nil)
	if err != nil {
		return "", err
	}
	if code != 0 || strings.TrimSpace(out) == "" {
		return "", notARepository
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
