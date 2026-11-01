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

	"github.com/wstein/sourcesteward/internal/conflicts"
	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/reviewstate"
	"github.com/wstein/sourcesteward/internal/snapcmd"
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
	// Spec 046: NoReview skips the comment reads; ReviewStates and Hints keep matching rows.
	NoReview     bool
	ReviewStates []string
	Hints        []string
	// Mergeable prints the owner list instead of the groups (Spec 046).
	Mergeable bool
	// Stack prints the stack view instead of the groups (Spec 046).
	Stack bool
	// Spec 047: probe the base conflicts of one repository from a local checkout; RestackPlan adds the
	// plan and the brief and implies Conflicts. Checkout defaults to the current directory, Remote to origin.
	Conflicts   bool
	RestackPlan bool
	Checkout    string
	Remote      string
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
	Mergeable     bool
	Stack         bool
	Out           io.Writer
	Diag          io.Writer // one line per problem (stderr); nil discards them

	Transport hosting.Transport // in-process HTTP fixture; nil means HTTPS
	Now       func() time.Time
	// Open replaces the hosting reader (tests). The target is the directory that bounds the gh
	// configuration check, like the repository does for the one-shot operations.
	Open func(ctx context.Context, target, host string) (Source, func(), error)

	// Spec 047
	Conflicts   bool
	RestackPlan bool
	Checkout    string
	Remote      string
	// NewProber replaces the checkout prober (tests); nil selects the one built on the checkout.
	NewProber func(ctx context.Context, req ProberRequest) (pulls.ConflictProber, func(), error)
}

// ProberRequest is what the prober of a run needs: the checkout, its remote and the repository the
// remote must point to.
type ProberRequest struct{ Dir, Remote, Repo, Host string }

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
	case f.NoBots && f.BotsOnly:
		return p, snapcmd.Usagef("--no-bots and --bots-only are mutually exclusive")
	case f.Human && f.JSON:
		return p, snapcmd.Usagef("--human and --json are mutually exclusive")
	case f.Limit < 1 || f.Limit > pulls.MaxLimit:
		return p, snapcmd.Usagef("--limit %d is outside 1..%d", f.Limit, pulls.MaxLimit)
	case f.WorkspacePath != "" && !f.Workspace:
		return p, snapcmd.Usagef("PATH is only valid with --workspace")
	case f.Stack && (f.Mergeable || f.NoReview || len(f.ReviewStates) > 0 || len(f.Hints) > 0):
		return p, snapcmd.Usagef("--stack cannot be combined with --mergeable, --no-review, --review or --hint")
	case f.Mergeable && (f.NoReview || len(f.ReviewStates) > 0 || len(f.Hints) > 0):
		return p, snapcmd.Usagef("--mergeable cannot be combined with --no-review, --review or --hint")
	case f.NoReview && (len(f.ReviewStates) > 0 || len(f.Hints) > 0):
		return p, snapcmd.Usagef("--no-review cannot be combined with --review or --hint")
	case !(f.Conflicts || f.RestackPlan) && (f.Checkout != "" || f.Remote != ""):
		return p, snapcmd.Usagef("--checkout and --remote need --conflicts or --restack-plan")
	case f.Remote != "" && !conflicts.ValidRemote(f.Remote):
		return p, snapcmd.Usagef("--remote %q is not a git remote name", f.Remote)
	case (f.Conflicts || f.RestackPlan) && len(f.Owners) > 0:
		return p, snapcmd.Usagef("--conflicts and --restack-plan work on one repository, not on --owner discovery")
	}
	for _, st := range f.ReviewStates {
		if !contains(reviewstate.States, st) {
			return p, snapcmd.Usagef("--review %q is not one of %s", st, strings.Join(reviewstate.States, ", "))
		}
	}
	for _, h := range f.Hints {
		if !contains(reviewstate.AllHints, h) {
			return p, snapcmd.Usagef("--hint %q is not one of %s", h, strings.Join(reviewstate.AllHints, ", "))
		}
	}
	for _, a := range f.Authors {
		if !authorName.MatchString(a) {
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
	p = Params{Host: host, Owners: owners, Sources: map[string]string{}, Out: out, Human: snapcmd.TerminalHuman(f.Human, f.JSON, out), Mergeable: f.Mergeable, Stack: f.Stack,
		Options: pulls.Options{NoBots: f.NoBots, BotsOnly: f.BotsOnly, Authors: f.Authors, DetailDrafts: f.DetailDrafts, Limit: f.Limit,
			Review: !f.NoReview, ReviewStates: f.ReviewStates, Hints: f.Hints},
		Conflicts: f.Conflicts || f.RestackPlan, RestackPlan: f.RestackPlan, Checkout: f.Checkout, Remote: f.Remote}
	if p.Conflicts && p.Remote == "" {
		p.Remote = "origin"
	}
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
	if p.Conflicts && len(repos) != 1 {
		return p, snapcmd.Usagef("--conflicts and --restack-plan work on one repository; got %d", len(repos))
	}
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
			return snapcmd.Failure(watchcmd.ExitFailure, "hosting-api-unavailable: cannot read the working directory")
		}
		target = wd
	}
	src, cleanup, err := open(ctx, target, p.Host)
	if err != nil {
		return snapcmd.Failure(watchcmd.ExitFailure, "hosting-api-unavailable: %v", err)
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
	var errs []snapcmd.DocError
	if len(p.Owners) > 0 {
		for _, d := range src.DiscoverOwners(ctx, p.Owners) {
			errs = append(errs, snapcmd.OwnerErrors(d)...)
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
		return snapcmd.Usagef("%d repositories exceed the limit of %d per run", len(repos), watchcmd.MaxRepos)
	}
	o := p.Options
	o.Repos, o.Now = repos, p.Now
	if p.Conflicts {
		dir := p.Checkout
		if dir == "" {
			dir = target // the workspace root, else the current directory
		}
		newProber := p.NewProber
		if newProber == nil {
			newProber = defaultProber
		}
		prober, closeProber, err := newProber(ctx, ProberRequest{Dir: dir, Remote: p.Remote, Repo: repos[0], Host: p.Host})
		if err != nil {
			return err
		}
		defer closeProber()
		o.Conflicts, o.Remote, o.RestackPlan = prober, p.Remote, p.RestackPlan
	}
	res, err := pulls.Run(ctx, src, o)
	if err != nil {
		return snapcmd.Failure(watchcmd.ExitFailure, "%v", err)
	}
	doc := buildDocument(p, res, sources, errs)
	var werr error
	if p.Human {
		werr = writeHuman(p.Out, doc)
	} else {
		werr = writeJSON(p.Out, doc)
	}
	if werr != nil {
		return snapcmd.Failure(watchcmd.ExitFailure, "cannot write the output")
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
		return snapcmd.Failure(watchcmd.ExitFailure, "%s", problemSummary(doc))
	}
	return nil
}

// defaultProber builds the prober on the checkout. The checkout must be a directory whose remote
// points to the repository of the run; anything else is a usage error before any probe.
func defaultProber(ctx context.Context, req ProberRequest) (pulls.ConflictProber, func(), error) {
	if info, err := os.Stat(req.Dir); err != nil || !info.IsDir() {
		return nil, func() {}, snapcmd.Usagef("the checkout %q is not a directory", untrusted.Esc(req.Dir))
	}
	r := conflicts.NewRunner(req.Dir)
	got, err := r.RemoteRepo(ctx, req.Remote, req.Host)
	if err != nil {
		return nil, func() {}, snapcmd.Usagef("the checkout %q: %v", untrusted.Esc(req.Dir), err)
	}
	if !strings.EqualFold(got, req.Repo) {
		return nil, func() {}, snapcmd.Usagef("remote %s of the checkout %q points to %q, not to %s", req.Remote, untrusted.Esc(req.Dir), untrusted.Esc(got), req.Repo)
	}
	return conflicts.Prober{Repo: req.Repo, Remote: req.Remote, Git: r}, r.Close, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func problemSummary(d *Document) string {
	var parts []string
	if n := d.Summary.ReposFailed; n > 0 {
		parts = append(parts, snapcmd.Plural(n, "repository", "repositories")+" could not be read")
	}
	if n := snapcmd.OwnerErrorCount(d.Errors); n > 0 {
		parts = append(parts, snapcmd.Plural(n, "owner problem", "owner problems"))
	}
	if len(parts) == 0 {
		parts = append(parts, "a read failed")
	}
	return strings.Join(parts, ", ")
}
