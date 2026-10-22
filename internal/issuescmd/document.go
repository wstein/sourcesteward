// SPDX-License-Identifier: EUPL-1.2

package issuescmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/issues"
	"github.com/wstein/sourcesteward/internal/snapcmd"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

const maxString = 2048

// Document is the one JSON document of `stw issues --json` (schemas/v1/issues.schema.json).
// Field order is the order of the contract; no key is ever omitted.
type Document struct {
	SchemaVersion int                `json:"schema_version"`
	Command       string             `json:"command"`
	Time          string             `json:"time"`
	Host          string             `json:"host"`
	Viewer        DocViewer          `json:"viewer"`
	Options       DocOptions         `json:"options"`
	Repos         []DocRepo          `json:"repos"`
	Issues        DocIssues          `json:"issues"`
	Summary       DocSummary         `json:"summary"`
	Errors        []snapcmd.DocError `json:"errors"`
	limit         int                // for the human renderer; not part of the contract
}

// DocViewer is the outcome of the one `user` read.
type DocViewer struct {
	State  string  `json:"state"`
	Login  *string `json:"login"`
	Reason *string `json:"reason"`
	Error  *string `json:"error"`
}

// DocOptions echoes the selection of the run.
type DocOptions struct {
	Scope     string   `json:"scope"`
	Owners    []string `json:"owners"`
	Labels    []string `json:"labels"`
	Assignees []string `json:"assignees"`
	Authors   []string `json:"authors"`
	Limit     int      `json:"limit"`
}

// DocRepo is one repository of the run.
type DocRepo struct {
	Repo         string  `json:"repo"`
	Source       string  `json:"source"`
	State        string  `json:"state"`
	Reason       *string `json:"reason"`
	Open         *int    `json:"open"`
	PullsDropped *int    `json:"pulls_dropped"`
	Error        *string `json:"error"`
}

// DocIssues holds the rows of each priority in the fixed key order.
type DocIssues struct {
	Red    []DocRow `json:"red"`
	Yellow []DocRow `json:"yellow"`
	Blue   []DocRow `json:"blue"`
	Violet []DocRow `json:"violet"`
}

// DocRow is one issue.
type DocRow struct {
	Repo        string   `json:"repo"`
	Number      int      `json:"number"`
	Title       string   `json:"title"`
	URL         *string  `json:"url"`
	Author      string   `json:"author"`
	Bot         bool     `json:"bot"`
	CreatedAt   *string  `json:"created_at"`
	AgeDays     *int     `json:"age_days"`
	Labels      []string `json:"labels"`
	LabelsTotal int      `json:"labels_total"`
	Assignees   []string `json:"assignees"`
	Comments    *int     `json:"comments"`
	Priority    string   `json:"priority"`
	Reason      string   `json:"reason"`
	Command     *string  `json:"command"`
	Effect      *string  `json:"effect"`
}

// DocCounts is the number of issues per priority.
type DocCounts struct {
	Red    int `json:"red"`
	Yellow int `json:"yellow"`
	Blue   int `json:"blue"`
	Violet int `json:"violet"`
}

// DocSummary counts the rows and repositories.
type DocSummary struct {
	Total       int       `json:"total"`
	Red         int       `json:"red"`
	Yellow      int       `json:"yellow"`
	Blue        int       `json:"blue"`
	Violet      int       `json:"violet"`
	Omitted     int       `json:"omitted"`
	Counts      DocCounts `json:"counts"`
	Bots        int       `json:"bots"`
	ReposRead   int       `json:"repos_read"`
	ReposFailed int       `json:"repos_failed"`
}

func buildDocument(p Params, res *issues.Result, sources map[string]string, ownerErrs []snapcmd.DocError) *Document {
	d := &Document{SchemaVersion: 1, Command: "issues", Time: res.Time.UTC().Format(time.RFC3339), Host: snapcmd.Cut(p.Host, maxString), limit: res.Limit}
	d.Viewer = DocViewer{State: res.Viewer.State, Login: snapcmd.StrPtr(res.Viewer.Login), Reason: snapcmd.StrPtr(res.Viewer.Reason), Error: snapcmd.StrPtr(res.Viewer.Error)}
	d.Options = DocOptions{Scope: p.Scope, Owners: snapcmd.Strings0(p.Owners), Labels: snapcmd.Strings0(p.Options.Labels), Assignees: snapcmd.Strings0(p.Options.Assignees),
		Authors: snapcmd.Strings0(p.Options.Authors), Limit: res.Limit}
	d.Repos = []DocRepo{}
	for _, st := range res.Repos {
		r := DocRepo{Repo: st.Repo, Source: sources[strings.ToLower(st.Repo)], State: st.State, Reason: snapcmd.StrPtr(st.Reason), Open: st.Open, PullsDropped: st.PullsDropped, Error: snapcmd.StrPtr(st.Error)}
		if r.Source == "" {
			r.Source = SourceRepo
		}
		d.Repos = append(d.Repos, r)
	}
	d.Issues = DocIssues{Red: []DocRow{}, Yellow: []DocRow{}, Blue: []DocRow{}, Violet: []DocRow{}}
	for _, r := range res.Rows {
		row := docRow(r)
		switch r.Priority {
		case issues.Red:
			d.Issues.Red = append(d.Issues.Red, row)
		case issues.Yellow:
			d.Issues.Yellow = append(d.Issues.Yellow, row)
		case issues.Blue:
			d.Issues.Blue = append(d.Issues.Blue, row)
		default:
			d.Issues.Violet = append(d.Issues.Violet, row)
		}
	}
	s := res.Summary
	d.Summary = DocSummary{Total: s.Total, Red: s.Red, Yellow: s.Yellow, Blue: s.Blue, Violet: s.Violet, Omitted: s.Omitted,
		Counts: DocCounts{Red: s.Counts.Red, Yellow: s.Counts.Yellow, Blue: s.Counts.Blue, Violet: s.Counts.Violet},
		Bots:   s.Bots, ReposRead: s.ReposRead, ReposFailed: s.ReposFailed}
	d.Errors = []snapcmd.DocError{}
	for _, e := range res.Errors {
		d.Errors = append(d.Errors, snapcmd.DocError{Repo: snapcmd.StrPtr(e.Repo), Code: e.Code, Message: snapcmd.Cut(e.Message, snapcmd.MaxMessage)})
	}
	for _, e := range ownerErrs {
		e.Message = snapcmd.Cut(e.Message, snapcmd.MaxMessage)
		d.Errors = append(d.Errors, e)
	}
	return d
}

func docRow(r issues.Row) DocRow {
	row := DocRow{Repo: r.Repo, Number: r.Number, Title: r.Title, URL: snapcmd.StrPtr(r.URL), Author: r.Author, Bot: r.Bot, AgeDays: r.AgeDays,
		Labels: snapcmd.Strings0(r.Labels), LabelsTotal: r.LabelsTotal, Assignees: snapcmd.Strings0(r.Assignees), Comments: r.Comments, Priority: r.Priority, Reason: r.Reason,
		Command: snapcmd.StrPtr(r.Command), Effect: snapcmd.StrPtr(r.Effect)}
	if !r.CreatedAt.IsZero() {
		at := r.CreatedAt.UTC().Format(time.RFC3339)
		row.CreatedAt = &at
	}
	return row
}

// writeJSON prints the document as one line.
func writeJSON(w io.Writer, d *Document) error { return json.NewEncoder(w).Encode(d) }

// writeHuman prints one section per non-empty priority, the run hint under every action row and a
// final summary line. Every untrusted field goes through untrusted.Esc (control characters
// escaped, 120 characters).
func writeHuman(w io.Writer, d *Document) error {
	var b strings.Builder
	sections := []struct {
		heading string
		rows    []DocRow
	}{{"RED", d.Issues.Red}, {"YELLOW", d.Issues.Yellow}, {"BLUE", d.Issues.Blue}, {"VIOLET", d.Issues.Violet}}
	for _, s := range sections {
		if len(s.rows) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%s (%d)\n", s.heading, len(s.rows))
		for _, r := range s.rows {
			writeHumanRow(&b, r)
		}
		b.WriteByte('\n')
	}
	s := d.Summary
	fmt.Fprintf(&b, "%s in %s, %d red, %d yellow\n", snapcmd.Plural(s.Total, "open issue", "open issues"), snapcmd.Plural(s.ReposRead, "repository", "repositories"), s.Red, s.Yellow)
	if s.Omitted > 0 {
		fmt.Fprintf(&b, "%d omitted (past --limit %d)\n", s.Omitted, d.limit)
	}
	if s.ReposFailed > 0 {
		fmt.Fprintf(&b, "%s could not be read\n", snapcmd.Plural(s.ReposFailed, "repository", "repositories"))
	}
	if n := snapcmd.OwnerErrorCount(d.Errors); n > 0 {
		fmt.Fprintf(&b, "%s with owner discovery\n", snapcmd.Plural(n, "problem", "problems"))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeHumanRow(b *strings.Builder, r DocRow) {
	age := "-"
	if r.AgeDays != nil {
		age = fmt.Sprintf("%dd", *r.AgeDays)
	}
	author := untrusted.Esc(r.Author)
	if r.Bot && !strings.HasSuffix(r.Author, "[bot]") {
		author += "[bot]"
	}
	fmt.Fprintf(b, "%s#%d  %s  %s", untrusted.Esc(r.Repo), r.Number, age, author)
	if len(r.Labels) > 0 {
		names := escAll(r.Labels)
		if more := r.LabelsTotal - len(r.Labels); more > 0 {
			names = append(names, fmt.Sprintf("+%d more", more))
		}
		fmt.Fprintf(b, "  [labels: %s]", strings.Join(names, ", "))
	}
	if len(r.Assignees) > 0 {
		fmt.Fprintf(b, "  [assigned: %s]", strings.Join(escAll(r.Assignees), ", "))
	}
	fmt.Fprintf(b, "  %s", untrusted.Esc(r.Title))
	fmt.Fprintf(b, "  (%s)", r.Reason)
	b.WriteByte('\n')
	if r.Command != nil {
		fmt.Fprintf(b, "  run: %s\n", *r.Command)
	}
}

func escAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = untrusted.Esc(s)
	}
	return out
}
