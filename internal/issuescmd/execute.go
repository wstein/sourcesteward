// SPDX-License-Identifier: EUPL-1.2

package issuescmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/issues"
	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/snapcmd"
	"github.com/wstein/sourcesteward/internal/untrusted"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// Source is what the command reads: the cycle's reader plus the issue owner search. It is
// *hosting.WatchReader built by hosting.NewIssuesReader, the only reader whose guard admits
// the issue search shape.
type Source interface {
	issues.Reader
	DiscoverIssueOwners(ctx context.Context, owners []string) []hosting.OwnerDiscovery
}

var _ Source = (*hosting.WatchReader)(nil)

// Execute reads the snapshot and prints it. The error is nil for exit 0, a usage error (exit 2)
// for an unusable repository set, and a *watchcmd.Failure with code 1 when the reader cannot be
// opened or when any repository, owner discovery or the viewer read failed (the output is still printed).
func Execute(ctx context.Context, p Params) error {
	open := p.Open
	if open == nil {
		open = func(ctx context.Context, target, host string) (Source, func(), error) {
			r, cleanup, err := hosting.NewIssuesReaderWithTransport(ctx, target, host, p.Transport)
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
		for _, d := range src.DiscoverIssueOwners(ctx, p.Owners) {
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
	res, err := issues.Run(ctx, src, o)
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
		subject := "run"
		switch {
		case e.Repo != nil:
			subject = *e.Repo
		case e.Owner != nil:
			subject = "owner " + *e.Owner
		}
		diag("%s: %s: %s", untrusted.Esc(subject), e.Code, untrusted.Esc(e.Message))
	}
	if n := viewerNotice(doc.Viewer); n != "" {
		diag("%s", n)
	}
	if len(doc.Errors) > 0 || doc.Summary.ReposFailed > 0 {
		return snapcmd.Failure(watchcmd.ExitFailure, "%s", problemSummary(doc))
	}
	return nil
}

func problemSummary(d *Document) string {
	var parts []string
	if n := d.Summary.ReposFailed; n > 0 {
		parts = append(parts, snapcmd.Plural(n, "repository", "repositories")+" could not be read")
	}
	if n := snapcmd.OwnerErrorCount(d.Errors); n > 0 {
		parts = append(parts, snapcmd.Plural(n, "owner problem", "owner problems"))
	}
	if n := runErrorCount(d); n > 0 {
		parts = append(parts, snapcmd.Plural(n, "run problem", "run problems"))
	}
	if len(parts) == 0 {
		parts = append(parts, "a read failed")
	}
	return strings.Join(parts, ", ")
}

func runErrorCount(d *Document) int {
	n := 0
	for _, e := range d.Errors {
		if e.Repo == nil && e.Owner == nil {
			n++
		}
	}
	return n
}

// viewerNotice says on stderr that the rule "assigned to you" is off: after a user-lookup skip and after a
// failed viewer read. No request made (not-github-origin) needs none, and the notice never reaches stdout.
func viewerNotice(v DocViewer) string {
	const tail = ": issues assigned to you are not flagged"
	switch {
	case v.State == issues.ViewerSkipped && v.Reason != nil && *v.Reason == issues.ReasonNoIdentity:
		return "notice no-identity: the token cannot read the authenticated user" + tail
	case v.State == issues.ViewerError && v.Error != nil:
		return "notice viewer-unknown: the authenticated user could not be read (" + untrusted.Esc(*v.Error) + ")" + tail
	}
	return ""
}
