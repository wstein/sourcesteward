// SPDX-License-Identifier: EUPL-1.2

package pullscmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/snapcmd"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// Document is the one JSON document of `stw pulls --json` (schemas/v1/pulls.schema.json).
// Field order is the order of the contract; no key is ever omitted.
type Document struct {
	SchemaVersion int                `json:"schema_version"`
	Command       string             `json:"command"`
	Time          string             `json:"time"`
	Host          string             `json:"host"`
	Options       DocOptions         `json:"options"`
	Repos         []DocRepo          `json:"repos"`
	Groups        DocGroups          `json:"groups"`
	Summary       DocSummary         `json:"summary"`
	Errors        []snapcmd.DocError `json:"errors"`
	limit         int                // for the human renderer; not part of the contract
}

// DocOptions echoes the selection of the run.
type DocOptions struct {
	Scope        string   `json:"scope"`
	Owners       []string `json:"owners"`
	NoBots       bool     `json:"no_bots"`
	BotsOnly     bool     `json:"bots_only"`
	Authors      []string `json:"authors"`
	DetailDrafts bool     `json:"detail_drafts"`
	Limit        int      `json:"limit"`
}

// DocRepo is one repository of the run.
type DocRepo struct {
	Repo   string  `json:"repo"`
	Source string  `json:"source"`
	Open   *int    `json:"open"`
	Listed int     `json:"listed"`
	Error  *string `json:"error"`
}

// DocGroups holds the rows of each group in the fixed key order.
type DocGroups struct {
	Ready    []DocRow `json:"ready"`
	Checks   []DocRow `json:"checks"`
	Conflict []DocRow `json:"conflict"`
	Draft    []DocRow `json:"draft"`
	Blocked  []DocRow `json:"blocked"`
}

// DocRow is one pull request.
type DocRow struct {
	Repo           string   `json:"repo"`
	Number         int      `json:"number"`
	Title          string   `json:"title"`
	URL            *string  `json:"url"`
	Author         string   `json:"author"`
	Bot            bool     `json:"bot"`
	Draft          bool     `json:"draft"`
	CreatedAt      *string  `json:"created_at"`
	AgeDays        *int     `json:"age_days"`
	Base           string   `json:"base"`
	HeadRef        string   `json:"head_ref"`
	Head           *string  `json:"head"`
	Fork           bool     `json:"fork"`
	Labels         []string `json:"labels"`
	Checks         *string  `json:"checks"`
	Failing        []string `json:"failing"`
	Mergeable      *bool    `json:"mergeable"`
	MergeableState *string  `json:"mergeable_state"`
	Rebaseable     *bool    `json:"rebaseable"`
	Reviewers      []string `json:"reviewers"`
	Group          string   `json:"group"`
	Reason         *string  `json:"reason"`
	Ready          bool     `json:"ready"`
	Detail         bool     `json:"detail"`
}

// DocSummary counts the rows and repositories.
type DocSummary struct {
	Total       int `json:"total"`
	Ready       int `json:"ready"`
	Checks      int `json:"checks"`
	Conflict    int `json:"conflict"`
	Draft       int `json:"draft"`
	Blocked     int `json:"blocked"`
	Bots        int `json:"bots"`
	NotRead     int `json:"not_read"`
	ReposRead   int `json:"repos_read"`
	ReposFailed int `json:"repos_failed"`
}

func buildDocument(p Params, res *pulls.Result, sources map[string]string, ownerErrs []snapcmd.DocError) *Document {
	d := &Document{SchemaVersion: 1, Command: "pulls", Time: res.Time.UTC().Format(time.RFC3339), Host: snapcmd.Cut(p.Host, pulls.MaxString), limit: res.Limit}
	d.Options = DocOptions{Scope: p.Scope, Owners: snapcmd.Strings0(p.Owners), NoBots: p.Options.NoBots, BotsOnly: p.Options.BotsOnly, Authors: snapcmd.Strings0(p.Options.Authors),
		DetailDrafts: p.Options.DetailDrafts, Limit: res.Limit}
	d.Repos = []DocRepo{}
	for _, st := range res.Repos {
		r := DocRepo{Repo: st.Repo, Source: sources[strings.ToLower(st.Repo)], Open: st.Open, Listed: st.Listed, Error: snapcmd.StrPtr(st.Error)}
		if r.Source == "" {
			r.Source = SourceRepo
		}
		d.Repos = append(d.Repos, r)
	}
	d.Groups = DocGroups{Ready: []DocRow{}, Checks: []DocRow{}, Conflict: []DocRow{}, Draft: []DocRow{}, Blocked: []DocRow{}}
	for _, r := range res.Rows {
		row := docRow(r)
		switch r.Group {
		case pulls.GroupReady:
			d.Groups.Ready = append(d.Groups.Ready, row)
		case pulls.GroupChecks:
			d.Groups.Checks = append(d.Groups.Checks, row)
		case pulls.GroupConflict:
			d.Groups.Conflict = append(d.Groups.Conflict, row)
		case pulls.GroupDraft:
			d.Groups.Draft = append(d.Groups.Draft, row)
		default:
			d.Groups.Blocked = append(d.Groups.Blocked, row)
		}
	}
	s := res.Summary
	d.Summary = DocSummary{Total: s.Total, Ready: s.Ready, Checks: s.Checks, Conflict: s.Conflict, Draft: s.Draft, Blocked: s.Blocked, Bots: s.Bots,
		NotRead: s.NotRead, ReposRead: s.ReposRead, ReposFailed: s.ReposFailed}
	d.Errors = []snapcmd.DocError{}
	for _, e := range res.Errors {
		repo := e.Repo
		d.Errors = append(d.Errors, snapcmd.DocError{Repo: &repo, Code: e.Code, Message: snapcmd.Cut(e.Message, snapcmd.MaxMessage)})
	}
	d.Errors = append(d.Errors, ownerErrs...)
	return d
}

func docRow(r pulls.Row) DocRow {
	row := DocRow{Repo: r.Repo, Number: r.Number, Title: r.Title, URL: snapcmd.StrPtr(r.URL), Author: r.Author, Bot: r.Bot, Draft: r.Draft, AgeDays: r.AgeDays,
		Base: r.Base, HeadRef: r.HeadRef, Fork: r.Fork, Labels: snapcmd.Strings0(r.Labels), Checks: r.Checks, Failing: snapcmd.Strings0(r.Failing), Mergeable: r.Mergeable,
		MergeableState: r.MergeableState, Rebaseable: r.Rebaseable, Reviewers: snapcmd.Strings0(r.Reviewers), Group: r.Group, Reason: snapcmd.StrPtr(r.Reason), Ready: r.Ready, Detail: r.Detail}
	if pulls.ValidSHA(r.Head) {
		head := r.Head
		row.Head = &head
	}
	if !r.CreatedAt.IsZero() {
		at := r.CreatedAt.UTC().Format(time.RFC3339)
		row.CreatedAt = &at
	}
	return row
}

// writeJSON prints the document as one line.
func writeJSON(w io.Writer, d *Document) error { return json.NewEncoder(w).Encode(d) }

var humanGroups = []string{pulls.GroupReady, pulls.GroupChecks, pulls.GroupConflict, pulls.GroupDraft, pulls.GroupBlocked}

// writeHuman prints one section per non-empty group and a final summary line. Every untrusted
// field goes through untrusted.Esc (control characters escaped, 120 characters).
func writeHuman(w io.Writer, d *Document) error {
	var b strings.Builder
	lists := map[string][]DocRow{pulls.GroupReady: d.Groups.Ready, pulls.GroupChecks: d.Groups.Checks, pulls.GroupConflict: d.Groups.Conflict, pulls.GroupDraft: d.Groups.Draft, pulls.GroupBlocked: d.Groups.Blocked}
	dash := func(s *string) string {
		if s == nil {
			return "-"
		}
		return untrusted.Esc(*s)
	}
	for _, g := range humanGroups {
		rows := lists[g]
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%s (%d)\n", g, len(rows))
		for _, r := range rows {
			age := "-"
			if r.AgeDays != nil {
				age = fmt.Sprintf("%dd", *r.AgeDays)
			}
			author := untrusted.Esc(r.Author)
			if r.Bot && !strings.HasSuffix(r.Author, "[bot]") {
				author += "[bot]"
			}
			fmt.Fprintf(&b, "%s#%d  %s  %s  %s  %s", untrusted.Esc(r.Repo), r.Number, age, author, dash(r.Checks), dash(r.MergeableState))
			if len(r.Reviewers) > 0 {
				names := make([]string, len(r.Reviewers))
				for i, n := range r.Reviewers {
					names[i] = untrusted.Esc(n)
				}
				fmt.Fprintf(&b, "  [reviewers: %s]", strings.Join(names, ", "))
			}
			fmt.Fprintf(&b, "  %s", untrusted.Esc(r.Title))
			if g == pulls.GroupBlocked && r.Reason != nil {
				fmt.Fprintf(&b, "  (%s)", untrusted.Esc(*r.Reason))
			}
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	s := d.Summary
	fmt.Fprintf(&b, "%s in %s, %d ready\n", snapcmd.Plural(s.Total, "open pull request", "open pull requests"), snapcmd.Plural(s.ReposRead, "repository", "repositories"), s.Ready)
	if s.NotRead > 0 {
		fmt.Fprintf(&b, "%d not read (past --limit %d)\n", s.NotRead, d.limit)
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
