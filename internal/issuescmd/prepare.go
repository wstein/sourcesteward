// SPDX-License-Identifier: EUPL-1.2

// Package issuescmd is `stw issues` (Spec 038): it validates the command line, resolves the
// repositories, runs the stateless issues cycle and prints its snapshot as a human table or as one
// JSON document. It mirrors internal/pullscmd and shares its repository discovery with it.
package issuescmd

import (
	"context"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/issues"
	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/pullscmd"
	"github.com/wstein/sourcesteward/internal/snapcmd"
	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/untrusted"
	"github.com/wstein/sourcesteward/internal/watch"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// Repository sources and scopes of the document: those of `stw pulls`.
const (
	SourceWorkspace = pullscmd.SourceWorkspace
	SourceRepo      = pullscmd.SourceRepo
	SourceOwner     = pullscmd.SourceOwner
	ScopeWorkspace  = pullscmd.ScopeWorkspace
	ScopeRepos      = pullscmd.ScopeRepos
	ScopeMixed      = pullscmd.ScopeMixed
)

const maxLabelChars = 50

var login = regexp.MustCompile(`\A[A-Za-z0-9-]{1,39}(\[bot\])?\z`)

// Flags are the parsed command line of `stw issues`.
type Flags struct {
	Repos         []string
	Workspace     bool
	WorkspacePath string
	Owners        []string
	Host          string
	Labels        []string
	Assignees     []string
	Authors       []string
	Limit         int
	Human         bool
	JSON          bool
}

// Params is a fully validated run.
type Params struct {
	Host          string
	Repos         []string          // workspace and --repo repositories, sorted, de-duplicated
	Sources       map[string]string // lower-case repository -> workspace|repo|owner
	Owners        []string          // distinct --owner values, in order
	Scope         string
	Skipped       []watch.Skipped
	WorkspaceRoot string
	Options       issues.Options // filters and row cap; Repos is filled by Execute
	Human         bool
	Out           io.Writer
	Diag          io.Writer // one line per problem (stderr); nil discards them

	Transport hosting.Transport // in-process HTTP fixture; nil means HTTPS
	Now       func() time.Time
	// Open replaces the hosting reader (tests). The target is the directory that bounds the gh
	// configuration check, like the repository does for the one-shot operations.
	Open func(ctx context.Context, target, host string) (Source, func(), error)
}

// validLabel: 1 to 50 characters of valid UTF-8, no control character, no leading or trailing space.
func validLabel(s string) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) < 1 || utf8.RuneCountInString(s) > maxLabelChars {
		return false
	}
	first, _ := utf8.DecodeRuneInString(s)
	last, _ := utf8.DecodeLastRuneInString(s)
	if unicode.IsSpace(first) || unicode.IsSpace(last) {
		return false
	}
	return !strings.ContainsFunc(s, unicode.IsControl)
}

// Prepare validates the flags and resolves the repositories. Every error is a usage error
// (exit 2): nothing has been read from the forge.
func Prepare(ctx context.Context, f Flags, out io.Writer) (Params, error) {
	return prepare(ctx, f, out, nil)
}

func prepare(ctx context.Context, f Flags, out io.Writer, discover func(context.Context, string) (stwsync.Workspace, error)) (Params, error) {
	var p Params
	host := f.Host
	if host == "" {
		host = watchcmd.DefaultHost
	}
	switch {
	case !watchcmd.ValidHost(host):
		return p, snapcmd.Usagef("--host %q is not a host name", host)
	case f.Human && f.JSON:
		return p, snapcmd.Usagef("--human and --json are mutually exclusive")
	case f.Limit < 1 || f.Limit > issues.MaxLimit:
		return p, snapcmd.Usagef("--limit %d is outside 1..%d", f.Limit, issues.MaxLimit)
	case f.WorkspacePath != "" && !f.Workspace:
		return p, snapcmd.Usagef("PATH is only valid with --workspace")
	}
	for _, l := range f.Labels {
		if !validLabel(l) {
			return p, snapcmd.Usagef("--label %q is not 1 to %d characters without control characters or surrounding space", l, maxLabelChars)
		}
	}
	for _, a := range f.Assignees {
		if a != issues.AssigneeMe && a != issues.AssigneeNone && !login.MatchString(a) {
			return p, snapcmd.Usagef("--assignee %q is not a GitHub login, @me or @none", a)
		}
	}
	for _, a := range f.Authors {
		if !login.MatchString(a) {
			return p, snapcmd.Usagef("--author %q is not a GitHub login", a)
		}
	}
	var owners []string
	seenOwner := map[string]bool{}
	for _, o := range f.Owners {
		if !hosting.ValidOwner(o) {
			return p, snapcmd.Usagef("--owner %q is not a GitHub login", o)
		}
		if k := strings.ToLower(o); !seenOwner[k] {
			seenOwner[k] = true
			owners = append(owners, o)
		}
	}
	for _, r := range f.Repos {
		if !untrusted.ValidRepo(r) {
			return p, snapcmd.Usagef("--repo %q is not OWNER/NAME", r)
		}
	}

	workspace := f.Workspace || (len(f.Repos) == 0 && len(owners) == 0)
	p = Params{Host: host, Owners: owners, Sources: map[string]string{}, Out: out, Human: snapcmd.TerminalHuman(f.Human, f.JSON, out),
		Options: issues.Options{Labels: f.Labels, Assignees: f.Assignees, Authors: f.Authors, Limit: f.Limit}}
	var repos []string
	if workspace {
		sel, err := watchcmd.Resolve(ctx, watchcmd.ResolveInput{Host: host, Repos: f.Repos, Workspace: true, WorkspacePath: f.WorkspacePath, Discover: discover})
		if err != nil {
			return p, err
		}
		repos, p.Skipped, p.WorkspaceRoot = sel.Repos, sel.Skipped, sel.WorkspaceRoot
		for _, r := range repos {
			p.Sources[strings.ToLower(r)] = SourceWorkspace
		}
	} else {
		repos = pulls.Dedupe(f.Repos)
	}
	for _, r := range f.Repos { // an explicit --repo names its repository
		p.Sources[strings.ToLower(r)] = SourceRepo
	}
	if len(repos) > watchcmd.MaxRepos {
		return p, snapcmd.Usagef("%d repositories exceed the limit of %d per run", len(repos), watchcmd.MaxRepos)
	}
	p.Repos = repos
	kinds := 0
	for _, on := range []bool{workspace, len(f.Repos) > 0, len(owners) > 0} {
		if on {
			kinds++
		}
	}
	switch {
	case kinds > 1:
		p.Scope = ScopeMixed
	case workspace:
		p.Scope = ScopeWorkspace
	default:
		p.Scope = ScopeRepos
	}
	return p, nil
}
