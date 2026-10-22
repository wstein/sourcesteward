// SPDX-License-Identifier: EUPL-1.2

package pullscmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/untrusted"
	"github.com/wstein/sourcesteward/internal/watch"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// Source is what the command reads: the cycle's reader plus the owner search. It is
// *hosting.WatchReader built by hosting.NewPullsReader, the only reader whose guard admits
// the search shape.
type Source interface {
	pulls.Reader
	DiscoverOwners(ctx context.Context, owners []string) []hosting.OwnerDiscovery
}

var _ Source = (*hosting.WatchReader)(nil)

// Repository sources of the document.
const (
	SourceWorkspace = "workspace"
	SourceRepo      = "repo"
	SourceOwner     = "owner"
)

// Scopes of the document's options.
const (
	ScopeWorkspace = "workspace"
	ScopeRepos     = "repos"
	ScopeMixed     = "mixed"
)

var authorName = regexp.MustCompile(`\A[A-Za-z0-9-]{1,39}(\[bot\])?\z`)

// Flags are the parsed command line of `stw pulls`.
type Flags struct {
	Repos         []string
	Workspace     bool
	WorkspacePath string
	Owners        []string
	Host          string
	NoBots        bool
	BotsOnly      bool
	Authors       []string
	DetailDrafts  bool
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
	Options       pulls.Options // filters, budget and parallelism; Repos is filled by Execute
	Human         bool
	Out           io.Writer
	Diag          io.Writer // one line per problem (stderr); nil discards them

	Transport hosting.Transport // in-process HTTP fixture; nil means HTTPS
	Now       func() time.Time
	// Open replaces the hosting reader (tests). The target is the directory that bounds the gh
	// configuration check, like the repository does for the one-shot operations.
	Open func(ctx context.Context, target, host string) (Source, func(), error)
}

func usagef(format string, args ...any) error {
	return fmt.Errorf("%w: %s", watchcmd.ErrUsage, fmt.Sprintf(format, args...))
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
		return p, usagef("--host %q is not a host name", host)
	case f.NoBots && f.BotsOnly:
		return p, usagef("--no-bots and --bots-only are mutually exclusive")
	case f.Human && f.JSON:
		return p, usagef("--human and --json are mutually exclusive")
	case f.Limit < 1 || f.Limit > pulls.MaxLimit:
		return p, usagef("--limit %d is outside 1..%d", f.Limit, pulls.MaxLimit)
	case f.WorkspacePath != "" && !f.Workspace:
		return p, usagef("PATH is only valid with --workspace")
	}
	for _, a := range f.Authors {
		if !authorName.MatchString(a) {
			return p, usagef("--author %q is not a GitHub login", a)
		}
	}
	var owners []string
	seenOwner := map[string]bool{}
	for _, o := range f.Owners {
		if !hosting.ValidOwner(o) {
			return p, usagef("--owner %q is not a GitHub login", o)
		}
		if k := strings.ToLower(o); !seenOwner[k] {
			seenOwner[k] = true
			owners = append(owners, o)
		}
	}
	for _, r := range f.Repos {
		if !untrusted.ValidRepo(r) {
			return p, usagef("--repo %q is not OWNER/NAME", r)
		}
	}

	workspace := f.Workspace || (len(f.Repos) == 0 && len(owners) == 0)
	p = Params{Host: host, Owners: owners, Sources: map[string]string{}, Out: out, Human: terminalHuman(f, out),
		Options: pulls.Options{NoBots: f.NoBots, BotsOnly: f.BotsOnly, Authors: f.Authors, DetailDrafts: f.DetailDrafts, Limit: f.Limit}}
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
		return p, usagef("%d repositories exceed the limit of %d per run", len(repos), watchcmd.MaxRepos)
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

// terminalHuman: --human, else --json, else human exactly when stdout is a terminal.
func terminalHuman(f Flags, out io.Writer) bool {
	switch {
	case f.Human:
		return true
	case f.JSON:
		return false
	}
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// failure is a terminal error with the exit code it maps to.
func failure(code int, format string, args ...any) *watchcmd.Failure {
	return &watchcmd.Failure{Code: code, Err: fmt.Errorf(format, args...)}
}

// Execute reads the snapshot and prints it. The error is nil for exit 0, a usage error (exit 2)
// for an unusable repository set, and a *watchcmd.Failure with code 1 when the reader cannot be
// opened or when any repository or owner discovery failed (the output is still printed).
func Execute(ctx context.Context, p Params) error {
	open := p.Open
	if open == nil {
		open = func(ctx context.Context, target, host string) (Source, func(), error) {
			r, cleanup, err := hosting.NewPullsReaderWithTransport(ctx, target, host, p.Transport)
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
			return failure(watchcmd.ExitFailure, "hosting-api-unavailable: cannot read the working directory")
		}
		target = wd
	}
	src, cleanup, err := open(ctx, target, p.Host)
	if err != nil {
		return failure(watchcmd.ExitFailure, "hosting-api-unavailable: %v", err)
	}
	defer cleanup()
	diag := func(format string, args ...any) {
		if p.Diag != nil {
			fmt.Fprintf(p.Diag, format+"\n", args...)
		}
	}
	for _, s := range p.Skipped {
		diag("skipped %s: %s", untrusted.Esc(s.Path), untrusted.Esc(s.Reason))
	}

	repos := append([]string{}, p.Repos...)
	sources := map[string]string{}
	for k, v := range p.Sources {
		sources[k] = v
	}
	var errs []DocError
	if len(p.Owners) > 0 {
		for _, d := range src.DiscoverOwners(ctx, p.Owners) {
			errs = append(errs, ownerErrors(d)...)
			for _, r := range d.Repos {
				if k := strings.ToLower(r); sources[k] == "" {
					sources[k] = SourceOwner
					repos = append(repos, r)
				}
			}
		}
		repos = pulls.Dedupe(repos)
	}
	if len(repos) > watchcmd.MaxRepos {
		return usagef("%d repositories exceed the limit of %d per run", len(repos), watchcmd.MaxRepos)
	}
	o := p.Options
	o.Repos, o.Now = repos, p.Now
	res, err := pulls.Run(ctx, src, o)
	if err != nil {
		return failure(watchcmd.ExitFailure, "%v", err)
	}
	doc := buildDocument(p, res, sources, errs)
	var werr error
	if p.Human {
		werr = writeHuman(p.Out, doc)
	} else {
		werr = writeJSON(p.Out, doc)
	}
	if werr != nil {
		return failure(watchcmd.ExitFailure, "cannot write the output")
	}
	for _, e := range doc.Errors {
		subject := ""
		switch {
		case e.Repo != nil:
			subject = *e.Repo
		case e.Owner != nil:
			subject = "owner " + *e.Owner
		}
		diag("%s: %s: %s", untrusted.Esc(subject), e.Code, untrusted.Esc(e.Message))
	}
	if len(doc.Errors) > 0 || doc.Summary.ReposFailed > 0 {
		return failure(watchcmd.ExitFailure, "%s", problemSummary(doc))
	}
	return nil
}

func problemSummary(d *Document) string {
	var parts []string
	if n := d.Summary.ReposFailed; n > 0 {
		parts = append(parts, plural(n, "repository", "repositories")+" could not be read")
	}
	if n := ownerErrorCount(d); n > 0 {
		parts = append(parts, plural(n, "owner problem", "owner problems"))
	}
	if len(parts) == 0 {
		parts = append(parts, "a read failed")
	}
	return strings.Join(parts, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// ownerErrors turns one discovery into error entries. Results with an unusable repository_url
// are always reported as malformed-response, also when the search was incomplete or too large.
func ownerErrors(d hosting.OwnerDiscovery) []DocError {
	var out []DocError
	owner := d.Owner
	add := func(code, msg string) {
		out = append(out, DocError{Owner: &owner, Code: code, Message: cut(msg, maxMessage)})
	}
	if d.Code != "" {
		add(d.Code, d.Message)
	}
	if d.Ignored > 0 && !(d.Code == hosting.CodeMalformed && d.Message == hosting.MessageIgnoredResults) {
		add(hosting.CodeMalformed, fmt.Sprintf("%s ignored: unusable repository", plural(d.Ignored, "search result", "search results")))
	}
	return out
}
