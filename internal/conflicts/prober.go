// SPDX-License-Identifier: EUPL-1.2

package conflicts

import (
	"context"
	"fmt"
	"strings"
)

// Prober answers the probe of one repository from a local checkout. It implements the prober of the
// pulls cycle; for any other repository it returns no findings, which the cycle reads as unknown.
type Prober struct {
	Repo   string // OWNER/NAME
	Remote string
	Git    Git
}

// Probe runs Detect when repo is the repository of the checkout.
func (p Prober) Probe(ctx context.Context, repo string, prs []PR) []Finding {
	if !strings.EqualFold(repo, p.Repo) {
		return nil
	}
	return Detect(ctx, p.Git, p.Remote, prs)
}

// ParseGitHubURL returns OWNER/NAME of a remote URL on host, or "" for anything else (another host,
// credentials in the URL, a local path, more or fewer than two path elements, odd characters).
func ParseGitHubURL(raw, host string) string {
	var path string
	switch {
	case strings.HasPrefix(raw, "https://"+host+"/"):
		path = strings.TrimPrefix(raw, "https://"+host+"/")
	case strings.HasPrefix(raw, "ssh://git@"+host+"/"):
		path = strings.TrimPrefix(raw, "ssh://git@"+host+"/")
	case strings.HasPrefix(raw, "git@"+host+":"):
		path = strings.TrimPrefix(raw, "git@"+host+":")
	default:
		return ""
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	owner, name, ok := strings.Cut(path, "/")
	if !ok || !repoPart(owner) || !repoPart(name) {
		return ""
	}
	return owner + "/" + name
}

func repoPart(s string) bool {
	if s == "" || len(s) > 100 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// RemoteRepo returns OWNER/NAME of the named remote of the checkout on host, "" when its URL is not a
// GitHub repository URL.
func (r *Runner) RemoteRepo(ctx context.Context, remote, host string) (string, error) {
	if !remoteName.MatchString(remote) {
		return "", fmt.Errorf("invalid remote name")
	}
	res, err := r.run(ctx, nil, "remote", "get-url", remote)
	if err != nil {
		return "", err
	}
	if res.code != 0 {
		return "", fmt.Errorf("remote %s is not configured", remote)
	}
	return ParseGitHubURL(strings.TrimSpace(string(res.out)), host), nil
}
