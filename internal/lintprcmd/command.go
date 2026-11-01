// SPDX-License-Identifier: EUPL-1.2

// Package lintprcmd is `stw lint pr` (Spec 050): it reads one pull request (GET only), applies the rule
// table of package lintpr and prints the findings. It writes nothing to the forge or to disk.
package lintprcmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/lintpr"
	"github.com/wstein/sourcesteward/internal/snapcmd"
	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/untrusted"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// Where the owner identities of a run come from.
const (
	OwnerSourceFlag      = "flag"
	OwnerSourceGitConfig = "git-config"
)

// ExitFindings is the exit status of a run that found a violation of severity error (a failed read is 1, a
// usage error 2), like the strict findings of the other commands.
const ExitFindings = 3

// Page bounds: 3 commit pages of 100 and 10 comment pages of 30.
const (
	maxCommitPages  = 3
	maxCommentPages = hosting.LintCommentsMaxPage
)

// Source is what the command reads. It is *hosting.WatchReader built by hosting.NewLintReader.
type Source interface {
	PullDetail(ctx context.Context, repo string, number int, etag string) (hosting.WatchResponse, error)
	PullCommits(ctx context.Context, repo string, number, page int, etag string) (hosting.WatchResponse, error)
	LintComments(ctx context.Context, repo string, number, page int) (hosting.WatchResponse, error)
}

var _ Source = (*hosting.WatchReader)(nil)

// Flags are the parsed command line of `stw lint pr`.
type Flags struct {
	PR            int
	Repo          string
	WorkspacePath string
	Host          string
	Owners        []string // "Name <email>", repeatable
	Human, JSON   bool
}

// Deps are the seams of Prepare: workspace discovery and the default owner identity.
type Deps struct {
	Discover    func(context.Context, string) (stwsync.Workspace, error)
	GitIdentity func(ctx context.Context, dir string) (lintpr.Identity, bool)
}

// Params is a fully validated run.
type Params struct {
	Host, Repo    string
	PR            int
	Owners        []lintpr.Identity
	OwnerSource   string
	WorkspaceRoot string
	Human         bool
	Out           io.Writer
	Diag          io.Writer

	Transport hosting.Transport // in-process HTTP fixture; nil means HTTPS
	Now       func() time.Time
	// Open replaces the hosting reader (tests). The target is the directory that bounds the gh configuration check.
	Open func(ctx context.Context, target, host string) (Source, func(), error)
}

var identityFlag = regexp.MustCompile(`\A\s*([^<>\s](?:[^<>]*[^<>\s])?)\s*<([^<>\s@]+@[^<>\s@]+)>\s*\z`)

// Prepare validates the flags. Every error is a usage error (exit 2): nothing has been read from the forge.
func Prepare(ctx context.Context, f Flags, out io.Writer) (Params, error) {
	return prepare(ctx, f, out, Deps{})
}

func prepare(ctx context.Context, f Flags, out io.Writer, d Deps) (Params, error) {
	var p Params
	host := f.Host
	if host == "" {
		host = watchcmd.DefaultHost
	}
	switch {
	case !watchcmd.ValidHost(host):
		return p, snapcmd.Usagef("--host %q is not a host name", host)
	case f.PR < 1:
		return p, snapcmd.Usagef("the pull request number %d is not a positive number", f.PR)
	case f.Human && f.JSON:
		return p, snapcmd.Usagef("--human and --json are mutually exclusive")
	case f.Repo != "" && !untrusted.ValidRepo(f.Repo):
		return p, snapcmd.Usagef("--repo %q is not OWNER/NAME", f.Repo)
	case f.WorkspacePath != "" && f.Repo != "":
		return p, snapcmd.Usagef("PATH and --repo are mutually exclusive")
	}
	p = Params{Host: host, PR: f.PR, Repo: f.Repo, Out: out, Human: snapcmd.TerminalHuman(f.Human, f.JSON, out)}
	if f.Repo == "" {
		sel, err := watchcmd.Resolve(ctx, watchcmd.ResolveInput{Host: host, Workspace: true, WorkspacePath: f.WorkspacePath, Discover: d.Discover})
		if err != nil {
			return p, err
		}
		if len(sel.Repos) != 1 {
			return p, snapcmd.Usagef("the workspace has %d GitHub repositories; name the one with --repo OWNER/NAME", len(sel.Repos))
		}
		p.Repo, p.WorkspaceRoot = sel.Repos[0], sel.WorkspaceRoot
	}
	for _, raw := range f.Owners {
		m := identityFlag.FindStringSubmatch(raw)
		if m == nil {
			return p, snapcmd.Usagef("--owner-identity %q is not \"Name <email>\"", raw)
		}
		p.Owners = append(p.Owners, lintpr.Identity{Name: m[1], Email: m[2]})
	}
	p.OwnerSource = OwnerSourceFlag
	if len(p.Owners) == 0 {
		dir := p.WorkspaceRoot
		if dir == "" {
			dir = "."
		}
		read := d.GitIdentity
		if read == nil {
			read = gitConfigIdentity
		}
		id, ok := read(ctx, dir)
		if !ok {
			return p, snapcmd.Usagef("no owner identity: pass --owner-identity \"Name <email>\" or set user.name and user.email in the Git configuration of %s", dir)
		}
		p.Owners, p.OwnerSource = []lintpr.Identity{id}, OwnerSourceGitConfig
	}
	return p, nil
}

// Execute reads the pull request, prints the document and returns nil for exit 0, a *watchcmd.Failure
// with code 3 for a finding of severity error or an incomplete read (a list cut at its bound) and code 1 for a failed read (no document is printed then).
func Execute(ctx context.Context, p Params) error {
	open := p.Open
	if open == nil {
		open = func(ctx context.Context, target, host string) (Source, func(), error) {
			r, cleanup, err := hosting.NewLintReaderWithTransport(ctx, target, host, p.Transport)
			if err != nil {
				return nil, func() {}, err
			}
			return r, cleanup, nil
		}
	}
	target := p.WorkspaceRoot
	if target == "" {
		wd, err := os.Getwd()
		if err != nil {
			return snapcmd.Failure(watchcmd.ExitFailure, "hosting-api-unavailable: cannot read the working directory")
		}
		target = wd
	}
	src, cleanup, err := open(ctx, target, p.Host)
	if err != nil {
		return snapcmd.Failure(watchcmd.ExitFailure, "hosting-api-unavailable: %v", err)
	}
	defer cleanup()
	in, head, trunc, err := read(ctx, src, p)
	if err != nil {
		return snapcmd.Failure(watchcmd.ExitFailure, "%s", readError(err))
	}
	findings := lintpr.Lint(in, lintpr.Config{Owners: p.Owners})
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	doc := buildDocument(p, in, head, trunc, findings, now())
	var werr error
	if p.Human {
		werr = writeHuman(p.Out, doc)
	} else {
		werr = json.NewEncoder(p.Out).Encode(doc)
	}
	if werr != nil {
		return snapcmd.Failure(watchcmd.ExitFailure, "cannot write the output")
	}
	if doc.Summary.Errors == 0 && !doc.Summary.Passed {
		return snapcmd.Failure(ExitFindings, "incomplete read: %s not read in full (see truncated); the result is not a pass", incompleteLists(doc.Truncated))
	}
	if doc.Summary.Errors > 0 {
		return snapcmd.Failure(ExitFindings, "%s, %s", snapcmd.Plural(doc.Summary.Errors, "error", "errors"), snapcmd.Plural(doc.Summary.Warnings, "warning", "warnings"))
	}
	return nil
}

func incompleteLists(t DocTruncated) string {
	switch {
	case t.Commits && t.Comments:
		return "commits and comments were"
	case t.Commits:
		return "commits were"
	}
	return "comments were"
}

func readError(err error) string {
	if we, ok := err.(*hosting.WatchError); ok {
		return fmt.Sprintf("%s: %s", we.Code, we.Message)
	}
	return untrusted.Esc(err.Error())
}

type truncation struct{ Commits, Comments bool }

// read gathers the input: the pull request, its commits (pages 1..3) and its comments (pages 1..10).
func read(ctx context.Context, src Source, p Params) (lintpr.Input, string, truncation, error) {
	var tr truncation
	in := lintpr.Input{PR: p.PR}
	res, err := src.PullDetail(ctx, p.Repo, p.PR, "")
	if err != nil {
		return in, "", tr, err
	}
	title, body, head, commitCount, err := decodeDetail(res.Body)
	if err != nil {
		return in, "", tr, err
	}
	in.Title, in.Body = title, body
	for page := 1; page <= maxCommitPages; page++ {
		res, err := src.PullCommits(ctx, p.Repo, p.PR, page, "")
		if err != nil {
			return in, "", tr, err
		}
		items, err := decodeCommits(res.Body)
		if err != nil {
			return in, "", tr, err
		}
		in.Commits = append(in.Commits, items...)
		more, err := hasMore(res.Link, page)
		if err != nil {
			return in, "", tr, err
		}
		if !more {
			break
		}
		if page == maxCommitPages {
			tr.Commits = true
		}
	}
	// The commits endpoint lists at most 250 commits whatever the page bound: a count below the one the pull
	// request reports means commits were not read.
	if commitCount > len(in.Commits) {
		tr.Commits = true
	}
	for page := 1; page <= maxCommentPages; page++ {
		res, err := src.LintComments(ctx, p.Repo, p.PR, page)
		if err != nil {
			return in, "", tr, err
		}
		items, err := decodeComments(res.Body)
		if err != nil {
			return in, "", tr, err
		}
		in.Comments = append(in.Comments, items...)
		more, err := hasMore(res.Link, page)
		if err != nil {
			return in, "", tr, err
		}
		if !more {
			break
		}
		if page == maxCommentPages {
			tr.Comments = true
		}
	}
	return in, head, tr, nil
}

// hasMore says whether the Link header announces the page after page. The URL in the header is never requested.
func hasMore(link string, page int) (bool, error) {
	next, more, err := hosting.LinkNextPage(link)
	if err != nil {
		return false, err
	}
	if more && next != page+1 {
		return false, fmt.Errorf("malformed-response: Link next is not the following page")
	}
	return more, nil
}
