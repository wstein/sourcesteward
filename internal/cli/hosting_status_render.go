// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/wstein/sourcesteward/internal/hosting"
)

// Every remote or local string is written with %q so that titles, refs and URLs
// can never reach the terminal as control or bidirectional characters.
func prEvidenceLine(p *hosting.PRStatusPullRequest) string {
	return fmt.Sprintf("PR %d lifecycle=%q draft=%t title=%q head=%q@%q base=%q@%q merged=%t merged_at=%q closed_at=%q merge_commit=%q result=%q",
		p.Number, p.Lifecycle, p.Draft, p.Title, hostingNullable(p.HeadRepository), p.HeadSHA, p.BaseRef, p.BaseSHA, p.Merged, hostingNullable(p.MergedAt), hostingNullable(p.ClosedAt), hostingNullable(p.MergeCommitSHA), hostingNullable(p.ResultSHA))
}

func renderPRStatus(w io.Writer, r hosting.PRStatusResult, compact bool) error {
	var text strings.Builder
	if compact {
		fmt.Fprintf(&text, "status=%q stop=%q expectation=%q", r.Status, r.Observation.Stop, r.Observation.Expectation)
		if r.PullRequest != nil {
			fmt.Fprintf(&text, " %s", prEvidenceLine(r.PullRequest))
		}
		for _, d := range r.Diagnostics {
			fmt.Fprintf(&text, " diagnostic=%q:%q", d.Code, d.Message)
		}
		text.WriteString("\n")
		_, err := io.WriteString(w, text.String())
		return err
	}
	fmt.Fprintf(&text, "Hosting: %q status=%q backend=%q\nObserved: %q sampled: %q\nComplete: %t; network reads: %t; requests: %d\nStop: %q\nExpectation: %q expected head: %q\n", r.Kind, r.Status, r.Backend, r.ObservedAt, hostingNullable(r.SampledAt), r.Boundary.Complete, r.Boundary.Network, r.Boundary.Requests, r.Observation.Stop, r.Observation.Expectation, hostingNullable(r.Observation.ExpectedHead))
	writeIdentity(&text, r.Identity, r.Repository, r.Candidates)
	if p := r.PullRequest; p != nil {
		fmt.Fprintf(&text, "PR: %d title=%q state=%q draft=%t\nHead: %q %q at %q\nBase: %q %q at %q\nURL: %q\nLifecycle: %q merged=%t merged_at=%q closed_at=%q\nReported merge commit: %q (method-dependent GitHub value)\nResult: %q\n",
			p.Number, p.Title, p.State, p.Draft, hostingNullable(p.HeadRepository), p.HeadRef, p.HeadSHA, p.BaseRepository, p.BaseRef, p.BaseSHA, p.URL, p.Lifecycle, p.Merged, hostingNullable(p.MergedAt), hostingNullable(p.ClosedAt), hostingNullable(p.MergeCommitSHA), hostingNullable(p.ResultSHA))
	}
	for _, d := range r.Diagnostics {
		fmt.Fprintf(&text, "Diagnostic: %q %q\n", d.Code, d.Message)
	}
	for _, v := range r.Boundary.Limitations {
		fmt.Fprintf(&text, "Limitation: %q\n", v)
	}
	_, err := io.WriteString(w, text.String())
	return err
}

func renderLookup(w io.Writer, r hosting.LookupResult, compact bool) error {
	var text strings.Builder
	fmt.Fprintf(&text, "PRs for %q: status=%q count=%d truncated=%t requests=%d\n", r.Query.HeadSHA, r.Status, r.Count, r.Truncated, r.Boundary.Requests)
	if !compact {
		fmt.Fprintf(&text, "Observed: %q sampled: %q backend=%q\n", r.ObservedAt, hostingNullable(r.SampledAt), r.Backend)
		writeIdentity(&text, r.Identity, r.Repository, r.Candidates)
	}
	for _, p := range r.PullRequests {
		pr := p.PRStatusPullRequest
		fmt.Fprintf(&text, "%s head_match=%t\n", prEvidenceLine(&pr), p.HeadMatch)
	}
	for _, d := range r.Diagnostics {
		fmt.Fprintf(&text, "Diagnostic: %q %q\n", d.Code, d.Message)
	}
	if !compact {
		for _, v := range r.Boundary.Limitations {
			fmt.Fprintf(&text, "Limitation: %q\n", v)
		}
	}
	_, err := io.WriteString(w, text.String())
	return err
}
