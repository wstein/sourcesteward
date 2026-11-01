// SPDX-License-Identifier: EUPL-1.2
package readycheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/snapcmd"
	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/untrusted"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// Flags are the parsed command line of `stw ready-check`.
type Flags struct {
	Repo  string
	Host  string
	Human bool
	JSON  bool
}

// Params is a fully validated run.
type Params struct {
	Input         Input
	WorkspaceRoot string
	Human         bool
	Out           io.Writer
	Review        ReviewFunc // nil until the review-state parser is wired (see ReviewUnavailable)

	Transport hosting.Transport // in-process HTTP fixture; nil means HTTPS
	Now       func() time.Time
	// Open replaces the hosting reader (tests).
	Open func(ctx context.Context, target, host string) (Source, func(), error)
}

// ReviewUnavailable is the review source of a build without the Spec 046 parser: it fails, so the
// review condition is unknown and the pull request is never reported ready.
func ReviewUnavailable(context.Context, string, int, string) (Review, error) {
	return Review{}, errors.New("the review-state parser (Spec 046) is not part of this build")
}

// Prepare validates the arguments; every error is a usage error (exit 2) and nothing is read.
func Prepare(ctx context.Context, f Flags, args []string, out io.Writer) (Params, error) {
	return prepare(ctx, f, args, out, nil)
}

func prepare(ctx context.Context, f Flags, args []string, out io.Writer, discover func(context.Context, string) (stwsync.Workspace, error)) (Params, error) {
	var p Params
	if len(args) != 1 {
		return p, snapcmd.Usagef("NUMBER is required: stw ready-check NUMBER")
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 || args[0] != strconv.Itoa(n) {
		return p, snapcmd.Usagef("NUMBER must be a positive decimal integer")
	}
	host := f.Host
	if host == "" {
		host = watchcmd.DefaultHost
	}
	switch {
	case !watchcmd.ValidHost(host):
		return p, snapcmd.Usagef("--host %q is not a host name", host)
	case f.Human && f.JSON:
		return p, snapcmd.Usagef("--human and --json are mutually exclusive")
	case f.Repo != "" && !untrusted.ValidRepo(f.Repo):
		return p, snapcmd.Usagef("--repo %q is not OWNER/NAME", f.Repo)
	}
	p = Params{Input: Input{Repo: f.Repo, Number: n, Host: host}, Out: out, Human: snapcmd.TerminalHuman(f.Human, f.JSON, out)}
	if f.Repo == "" {
		sel, err := watchcmd.Resolve(ctx, watchcmd.ResolveInput{Host: host, Workspace: true, Discover: discover})
		if err != nil {
			return p, err
		}
		if len(sel.Repos) != 1 {
			return p, snapcmd.Usagef("%d repositories in the workspace; name one with --repo OWNER/NAME", len(sel.Repos))
		}
		p.Input.Repo, p.WorkspaceRoot = sel.Repos[0], sel.WorkspaceRoot
	}
	return p, nil
}

// Execute reads, prints the document and returns nil only when every condition passes. A pull
// request that is not ready is a *watchcmd.Failure with code 1 after the document was printed.
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
	src, cleanup, err := open(ctx, target, p.Input.Host)
	if err != nil {
		return snapcmd.Failure(watchcmd.ExitFailure, "hosting-api-unavailable: %v", err)
	}
	defer cleanup()
	now := p.Now
	if now == nil {
		now = time.Now
	}
	d := Run(ctx, src, p.Review, p.Input, now)
	var werr error
	if p.Human {
		werr = writeHuman(p.Out, d)
	} else {
		werr = json.NewEncoder(p.Out).Encode(d)
	}
	if werr != nil {
		return snapcmd.Failure(watchcmd.ExitFailure, "cannot write the output")
	}
	if d.Ready {
		return nil
	}
	var bad []string
	for _, c := range d.Conditions {
		if c.Status != StatusPass {
			bad = append(bad, c.ID+" "+c.Status)
		}
	}
	return snapcmd.Failure(watchcmd.ExitFailure, "%s#%d is not ready: %s", untrusted.Esc(d.Repo), d.Number, strings.Join(bad, ", "))
}

func writeHuman(w io.Writer, d Document) error {
	var b strings.Builder
	head, draft := "-", "-"
	if d.Head != nil {
		head = (*d.Head)[:7]
	}
	if d.Draft != nil {
		draft = strconv.FormatBool(*d.Draft)
	}
	fmt.Fprintf(&b, "%s#%d head %s, draft %s\n", untrusted.Esc(d.Repo), d.Number, head, draft)
	for _, c := range d.Conditions {
		fmt.Fprintf(&b, "  %-10s %-7s %s\n", c.ID, c.Status, c.Evidence)
	}
	for _, e := range d.Errors {
		fmt.Fprintf(&b, "  read error (%s): %s: %s\n", e.Source, e.Code, untrusted.Esc(e.Message))
	}
	answer := "no"
	if d.Ready {
		answer = "yes"
	}
	fmt.Fprintf(&b, "Ready: %s\n", answer)
	_, err := io.WriteString(w, b.String())
	return err
}
