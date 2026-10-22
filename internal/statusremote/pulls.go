// SPDX-License-Identifier: EUPL-1.2

// Package statusremote reads the remote sources of the online `stw status` (Spec 037) and turns what
// they read into the inputs of the action model in internal/overview. It sends no request of its own:
// every read goes through a reader the caller built (the status reader of internal/hosting) and the
// cycles of the commands that already own the semantics, so the figures of `stw status` and of
// `stw pulls` cannot drift apart.
package statusremote

import (
	"context"
	"strings"

	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// Host is the one forge host of a run (Spec 037).
const Host = "github.com"

// Origin is what the local half knows of one repository of the workspace: its name, the URL of its
// `origin` (empty when it has none) and whether the local status of the repository ended `ok`.
type Origin struct {
	Name     string
	Remote   string
	Readable bool
}

// Target is one repository of the run. Slug is its OWNER/NAME on Host, empty when the repository
// is skipped; Skip is then the skip reason of the remote sources.
type Target struct {
	Name string
	Slug string
	Skip string
}

// Select derives the repository of each origin, in the order of the input. A repository that could
// not be read locally, has no usable origin, has one on another host or one whose path is not
// OWNER/NAME is skipped with its reason: none of its remote sources runs and no request is made for it.
func Select(origins []Origin) []Target {
	targets := make([]Target, len(origins))
	for i, o := range origins {
		targets[i] = Target{Name: o.Name}
		targets[i].Slug, targets[i].Skip = slugOf(o)
	}
	return targets
}

func slugOf(o Origin) (slug, skip string) {
	if !o.Readable {
		return "", overview.ReasonNotReadLocally
	}
	host, _, ok := watchcmd.SplitRemote(o.Remote)
	switch {
	case !ok:
		return "", overview.ReasonNotGitHub
	case !strings.EqualFold(host, Host):
		return "", overview.ReasonOtherHost
	}
	slug, ok = watchcmd.ParseRemote(o.Remote, Host)
	if !ok { // the host matches, so the path is what ParseRemote refused
		return "", overview.ReasonInvalidRepo
	}
	return slug, ""
}

// Pulls is what the pulls source read for one repository. State is the outcome of the source, PRs
// the rows in the model's input form, Open the number of open pull requests the list reported (nil
// when the list was not read) and Counts the group counts of the rows, exactly those of `stw pulls`.
type Pulls struct {
	State  overview.SourceState
	Open   *int
	PRs    []overview.PR
	Counts pulls.Summary
}

// ReadPulls reads the open pull requests of every target that was not skipped with one pulls cycle
// and default options: the filters, the detail budget and the single bounded re-read of an unknown
// mergeability are those of `stw pulls`, and no request is made when every target is skipped. The
// result is keyed by Target.Name; repositories that share an origin share one read.
//
// A failed read is the error of that repository's source, a cycle cut short by the context is the
// skip `deadline` of the repositories it did not finish; both keep the rows that were read.
func ReadPulls(ctx context.Context, r pulls.Reader, targets []Target) map[string]Pulls {
	out := make(map[string]Pulls, len(targets))
	var slugs []string
	for _, t := range targets {
		if t.Skip != "" {
			out[t.Name] = Pulls{State: overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: t.Skip}}
			continue
		}
		slugs = append(slugs, t.Slug)
	}
	// Run returns no result only for a limit outside 1..500; the defaults never are, and with a result
	// its error is only the context's: the deadline marks of the result already carry it.
	res, _ := pulls.Run(ctx, r, pulls.Options{Repos: slugs})
	for _, t := range targets {
		if t.Skip != "" {
			continue
		}
		view, _ := res.Repo(t.Slug) // every slug was requested
		out[t.Name] = fromView(view)
	}
	return out
}

// fromView maps one repository of the cycle's result. A failure of the repository wins over a
// deadline mark: it was attempted and failed before the deadline.
func fromView(v pulls.RepoView) Pulls {
	p := Pulls{State: overview.SourceState{Outcome: overview.OutcomeOK}, Open: v.Status.Open, Counts: v.Summary}
	switch {
	case v.Status.Error != "":
		p.State = overview.SourceState{Outcome: overview.OutcomeError, Code: v.Status.Error}
	case v.Status.Deadline:
		p.State = overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline}
	}
	for _, row := range v.Rows {
		p.PRs = append(p.PRs, overview.PR{
			Number: row.Number, Title: row.Title, URL: row.URL, Author: row.Author, Bot: row.Bot, Draft: row.Draft,
			Group: row.Group, Reason: row.Reason, PastBudget: row.PastBudget,
		})
	}
	return p
}
