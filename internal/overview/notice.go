// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"fmt"
	"sort"

	"github.com/wstein/sourcesteward/internal/untrusted"
)

// MaxNoticeRepos bounds the repository list of one notice; the message still counts every repository.
const MaxNoticeRepos = 256

// notices derives one notice per missing source (not per repository) from the outcomes of the sources,
// plus one per repository whose release drafts are not visible. The wording is deterministic and the
// result does not depend on the order of the repositories.
func notices(in Input) []Notice {
	type key struct{ code, source string }
	type group struct {
		repos map[string]bool
		pairs int // (repository, source) combinations
		code  string
		src   Source
	}
	groups := map[key]*group{}
	add := func(k key, repo string, code string, src Source) {
		g := groups[k]
		if g == nil {
			g = &group{repos: map[string]bool{}, code: code, src: src}
			groups[k] = g
		}
		g.repos[repo] = true
		g.pairs++
	}
	var out []Notice
	for _, r := range in.Repos {
		if r.Remote == nil {
			continue
		}
		name := repoName(r)
		for _, src := range Sources {
			st := r.Remote.Sources[src]
			switch st.Outcome {
			case OutcomeSkipped:
				switch st.Reason {
				case ReasonOffline, ReasonNotAttempted, ReasonIssuesDisabled: // nothing was read, or a fact about the repository; the notice of the source it needs says why
				case ReasonNotFound:
					add(key{ReasonNotFound, string(src)}, name, st.Reason, src)
				case ReasonNoToken, ReasonDeadline:
					add(key{st.Reason, "remote"}, name, st.Reason, src)
				default:
					add(key{untrusted.Esc(st.Reason), "remote"}, name, st.Reason, src)
				}
			case OutcomeError:
				if st.Code == CodeRateLimited {
					add(key{CodeRateLimited, "remote"}, name, st.Code, src)
				} else {
					add(key{untrusted.Esc(st.Code), string(src)}, name, st.Code, src)
				}
			}
		}
		if untrusted.ValidRepo(r.Slug) {
			switch r.Remote.Visibility {
			case NoWriteAccess:
				out = append(out, Notice{Code: "drafts-not-visible", Source: string(SourceReleases), Repos: []string{r.Slug},
					Message: "release drafts of " + r.Slug + " not visible (token has no write access)"})
			case Unknown:
				out = append(out, Notice{Code: "drafts-unknown", Source: string(SourceReleases), Repos: []string{r.Slug},
					Message: "release drafts of " + r.Slug + " unknown (write access not determined)"})
			}
		}
	}
	out = append(out, viewerNotices(in.Viewer)...)
	for k, g := range groups {
		out = append(out, Notice{Code: k.code, Source: k.source, Repos: capRepos(g.repos), Message: noticeMessage(in, k.code, g.code, g.src, len(g.repos), g.pairs)})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Message < b.Message
	})
	return out
}

// repoName is the name a notice uses: OWNER/NAME when the repository has a valid one, else its local name.
func repoName(r Repo) string {
	if untrusted.ValidRepo(r.Slug) {
		return r.Slug
	}
	return untrusted.Esc(r.Local.Name)
}

func capRepos(set map[string]bool) []string {
	repos := make([]string, 0, len(set))
	for r := range set {
		repos = append(repos, r)
	}
	sort.Strings(repos)
	if len(repos) > MaxNoticeRepos {
		repos = repos[:MaxNoticeRepos]
	}
	return repos
}

// sourceLabel names a source in a notice.
var sourceLabel = map[Source]string{
	SourceRepo: "repository data", SourcePulls: "pull requests", SourceReleases: "releases", SourceTags: "tags",
	SourceCIMain: "default branch CI", SourceCIRelease: "release workflow runs", SourceIssues: "issues",
}

// skipText names why the remote sources of a repository were skipped before any read.
var skipText = map[string]string{
	ReasonNotGitHub: "origin is not a GitHub repository", ReasonOtherHost: "origin is on another host",
	ReasonNotReadLocally: "repository was not read locally", ReasonInvalidRepo: "invalid repository name",
}

func noticeMessage(in Input, key, code string, src Source, repos, pairs int) string {
	switch key {
	case ReasonNoToken:
		return "no GitHub token (run: gh auth login): pull requests, releases and tags were not read"
	case ReasonDeadline:
		return fmt.Sprintf("deadline of %d s reached: %s of %s not read; raise --timeout", int(in.Deadline.Seconds()), count1(pairs, "source", "sources"), count1(repos, "repository", "repositories"))
	case ReasonNotFound:
		return fmt.Sprintf("release workflow not found in %s: runs not read", count1(repos, "repository", "repositories"))
	case CodeRateLimited:
		at := ""
		if !in.RateLimitReset.IsZero() {
			at = " at " + in.RateLimitReset.UTC().Format("15:04") + " UTC"
		}
		return "rate limit reached" + at + ": " + count1(repos, "repository", "repositories") + " not read"
	}
	if text, ok := skipText[code]; ok {
		return fmt.Sprintf("%s: remote sources of %s were not read", text, count1(repos, "repository", "repositories"))
	}
	label := sourceLabel[src]
	return fmt.Sprintf("%s: %s of %s not read", errorText(code), label, count1(repos, "repository", "repositories"))
}

// errorText is the lead of an error notice: `network error`, else the escaped error code.
func errorText(code string) string {
	if code == CodeNetwork {
		return "network error"
	}
	return untrusted.Esc(code)
}

// viewerNotices says that "assigned to you" is not flagged: when the token class cannot read the
// authenticated user (a skip) and when the read failed. A skip because no request was made (offline, no
// token) is already told by the sources.
func viewerNotices(v SourceState) []Notice {
	switch {
	case v.Outcome == OutcomeSkipped && v.Reason == ReasonNoIdentity:
		return []Notice{{Code: ReasonNoIdentity, Source: "viewer", Message: "the token cannot read the authenticated user: issues assigned to you are not flagged"}}
	case v.Outcome == OutcomeError:
		return []Notice{{Code: "viewer-unknown", Source: "viewer", Message: "the authenticated user could not be read (" + untrusted.Esc(v.Code) + "): issues assigned to you are not flagged"}}
	}
	return nil
}
