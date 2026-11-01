// SPDX-License-Identifier: EUPL-1.2
package lintprcmd

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/lintpr"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// Document is the one JSON document of `stw lint pr --json` (schemas/v1/lint-pr.schema.json). Keys are
// never omitted.
type Document struct {
	SchemaVersion int          `json:"schema_version"`
	Command       string       `json:"command"`
	Time          string       `json:"time"`
	Host          string       `json:"host"`
	Repo          string       `json:"repo"`
	PullRequest   int          `json:"pull_request"`
	Head          *string      `json:"head"`
	Options       DocOptions   `json:"options"`
	Rules         []DocRule    `json:"rules"`
	Findings      []DocFinding `json:"findings"`
	Summary       DocSummary   `json:"summary"`
	Truncated     DocTruncated `json:"truncated"`
}

// DocOptions echoes the identities the run compared against.
type DocOptions struct {
	OwnerIdentities []string `json:"owner_identities"`
	OwnerSource     string   `json:"owner_source"`
}

// DocRule is one row of the rule table.
type DocRule struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
	Source   string `json:"source"`
}

// DocLocation says where a finding is: the kind, the commit SHA or comment ID, the line.
type DocLocation struct {
	Kind string  `json:"kind"`
	ID   *string `json:"id"`
	Line *int    `json:"line"`
}

// DocFinding is one violation.
type DocFinding struct {
	Rule     string      `json:"rule"`
	Severity string      `json:"severity"`
	Location DocLocation `json:"location"`
	Message  string      `json:"message"`
}

// DocSummary counts what was read and found.
type DocSummary struct {
	Commits  int  `json:"commits"`
	Comments int  `json:"comments"`
	Errors   int  `json:"errors"`
	Warnings int  `json:"warnings"`
	Passed   bool `json:"passed"`
}

// DocTruncated says that a list was read only up to its page bound.
type DocTruncated struct {
	Commits  bool `json:"commits"`
	Comments bool `json:"comments"`
}

func buildDocument(p Params, in lintpr.Input, head string, tr truncation, fs []lintpr.Finding, now time.Time) *Document {
	d := &Document{SchemaVersion: 1, Command: "lint pr", Time: now.UTC().Format(time.RFC3339), Host: p.Host, Repo: p.Repo, PullRequest: p.PR,
		Options:   DocOptions{OwnerIdentities: []string{}, OwnerSource: p.OwnerSource},
		Rules:     []DocRule{},
		Findings:  []DocFinding{},
		Summary:   DocSummary{Commits: len(in.Commits), Comments: len(in.Comments)},
		Truncated: DocTruncated{Commits: tr.Commits, Comments: tr.Comments}}
	if head != "" {
		d.Head = &head
	}
	for _, o := range p.Owners {
		d.Options.OwnerIdentities = append(d.Options.OwnerIdentities, o.Name+" <"+o.Email+">")
	}
	for _, r := range lintpr.Rules {
		d.Rules = append(d.Rules, DocRule{ID: r.ID, Severity: string(r.Severity), Summary: r.Summary, Source: r.Source})
	}
	for _, f := range fs {
		loc := DocLocation{Kind: f.Location.Kind}
		if f.Location.ID != "" {
			id := f.Location.ID
			loc.ID = &id
		}
		if f.Location.Line > 0 {
			line := f.Location.Line
			loc.Line = &line
		}
		d.Findings = append(d.Findings, DocFinding{Rule: f.Rule, Severity: string(f.Severity), Location: loc, Message: f.Message})
		if f.Severity == lintpr.SeverityError {
			d.Summary.Errors++
		} else {
			d.Summary.Warnings++
		}
	}
	d.Summary.Passed = d.Summary.Errors == 0
	return d
}

func writeHuman(w io.Writer, d *Document) error {
	var b strings.Builder
	head := "-"
	if d.Head != nil {
		head = (*d.Head)[:12]
	}
	verdict := "pass"
	if !d.Summary.Passed {
		verdict = "FAIL"
	}
	fmt.Fprintf(&b, "%s#%d head %s: %s, %d errors, %d warnings (%d commits, %d comments read)\n", d.Repo, d.PullRequest, head, verdict,
		d.Summary.Errors, d.Summary.Warnings, d.Summary.Commits, d.Summary.Comments)
	for _, f := range d.Findings {
		where := f.Location.Kind
		if f.Location.ID != nil {
			id := *f.Location.ID
			if len(id) > 12 {
				id = id[:12]
			}
			where += " " + id
		}
		if f.Location.Line != nil {
			where += ":" + strconv.Itoa(*f.Location.Line)
		}
		fmt.Fprintf(&b, "%-7s %-22s %s: %s\n", f.Severity, f.Rule, where, untrusted.Esc(f.Message))
	}
	if d.Truncated.Commits {
		b.WriteString("note: only the first 300 commits were read\n")
	}
	if d.Truncated.Comments {
		b.WriteString("note: only the first 300 comments were read\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}
