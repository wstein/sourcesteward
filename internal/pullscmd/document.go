// SPDX-License-Identifier: EUPL-1.2

package pullscmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/prviews"
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
	// OwnerQueue is null without --mergeable (Spec 046).
	OwnerQueue *[]DocQueueEntry `json:"owner_queue"`
	// Stack is null without --stack (Spec 046).
	Stack *DocStack `json:"stack"`
	limit int       // for the human renderer; not part of the contract
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
	// Spec 046 members (additive)
	Review       bool     `json:"review"`
	ReviewStates []string `json:"review_states"`
	Hints        []string `json:"hints"`
	Mergeable    bool     `json:"mergeable"`
	Stack        bool     `json:"stack"`
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
	// Spec 046 members (additive): the review state is the claim of a comment, see DocReview.Basis
	Review *DocReview `json:"review"`
	Hints  []string   `json:"hints"`
}

// DocReview is the review state of a row. Basis is always "comment": the verdict is what a comment
// says, checked only against the head and the commit date (Spec 046, Trust model).
type DocReview struct {
	Basis   string  `json:"basis"`
	State   string  `json:"state"`
	Verdict *string `json:"verdict"`
	Head    *string `json:"head"`
	PatchID *string `json:"patch_id"`
	At      *string `json:"at"`
	URL     *string `json:"url"`
	// Association is the author_association of the decisive comment; Ignored counts review-like
	// comments of other authors that were left out (Spec 046, Trust model).
	Association *string `json:"association"`
	Ignored     int     `json:"ignored"`
	Carried     bool    `json:"carried"`
	Edited      bool    `json:"edited"`
	Truncated   bool    `json:"truncated"`
}

// DocReviewCounts counts rows per review state; NotRead counts rows whose comments were not read.
type DocReviewCounts struct {
	None         int `json:"none"`
	Findings     int `json:"findings"`
	Clear        int `json:"clear"`
	ClearCarried int `json:"clear_carried"`
	ClearStale   int `json:"clear_stale"`
	Unparsed     int `json:"unparsed"`
	NotRead      int `json:"not_read"`
}

// DocHintCounts counts rows per hint.
type DocHintCounts struct {
	ClearNotReady        int `json:"clear_not_ready"`
	NoFinalReview        int `json:"no_final_review"`
	BaseNotDefault       int `json:"base_not_default"`
	ParentMergedRetarget int `json:"parent_merged_retarget"`
	// TruncatedReviewUnknown and ForeignReviewIgnored count the hints of the same names.
	TruncatedReviewUnknown int `json:"truncated_review_unknown"`
	ForeignReviewIgnored   int `json:"foreign_review_ignored"`
	ReadyMergeable         int `json:"ready_mergeable"`
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
	// Spec 046 members (additive)
	Review DocReviewCounts `json:"review"`
	Hints  DocHintCounts   `json:"hints"`
	// WaitingForParent counts the non-draft rows with the hint base-not-default (0 without --mergeable).
	WaitingForParent int `json:"waiting_for_parent"`
}

// DocStack is the stack view: the trees with a child (or an orphan base) and the number of single pull requests left out.
type DocStack struct {
	Trees   []*DocStackNode `json:"trees"`
	Singles int             `json:"singles"`
}

// DocStackNode is one pull request of a stack; Checks and Review are null when not read.
type DocStackNode struct {
	Repo     string          `json:"repo"`
	Number   int             `json:"number"`
	Title    string          `json:"title"`
	URL      *string         `json:"url"`
	Draft    bool            `json:"draft"`
	Group    string          `json:"group"`
	Checks   *string         `json:"checks"`
	Review   *string         `json:"review"`
	Hints    []string        `json:"hints"`
	Base     string          `json:"base"`
	HeadRef  string          `json:"head_ref"`
	Children []*DocStackNode `json:"children"`
}

// DocQueueEntry is one entry of the owner list: a pull request that can be merged now, in merge order.
type DocQueueEntry struct {
	Position int     `json:"position"`
	Repo     string  `json:"repo"`
	Number   int     `json:"number"`
	Title    string  `json:"title"`
	URL      *string `json:"url"`
	Head     *string `json:"head"`
	Review   string  `json:"review"`
	Reason   string  `json:"reason"`
	Then     []int   `json:"then"`
}

func buildDocument(p Params, res *pulls.Result, sources map[string]string, ownerErrs []snapcmd.DocError) *Document {
	d := &Document{SchemaVersion: 1, Command: "pulls", Time: res.Time.UTC().Format(time.RFC3339), Host: snapcmd.Cut(p.Host, pulls.MaxString), limit: res.Limit}
	d.Options = DocOptions{Scope: p.Scope, Owners: snapcmd.Strings0(p.Owners), NoBots: p.Options.NoBots, BotsOnly: p.Options.BotsOnly, Authors: snapcmd.Strings0(p.Options.Authors),
		DetailDrafts: p.Options.DetailDrafts, Limit: res.Limit,
		Mergeable: p.Mergeable, Stack: p.Stack, Review: res.ReviewOn, ReviewStates: snapcmd.Strings0(p.Options.ReviewStates), Hints: snapcmd.Strings0(p.Options.Hints)}
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
		NotRead: s.NotRead, ReposRead: s.ReposRead, ReposFailed: s.ReposFailed,
		Review: DocReviewCounts{None: s.Review.None, Findings: s.Review.Findings, Clear: s.Review.Clear, ClearCarried: s.Review.ClearCarried,
			ClearStale: s.Review.ClearStale, Unparsed: s.Review.Unparsed, NotRead: s.Review.NotRead},
		Hints: DocHintCounts{ClearNotReady: s.Hints.ClearNotReady, NoFinalReview: s.Hints.NoFinalReview, BaseNotDefault: s.Hints.BaseNotDefault,
			ParentMergedRetarget: s.Hints.ParentMergedRetarget, TruncatedReviewUnknown: s.Hints.TruncatedReviewUnknown,
			ForeignReviewIgnored: s.Hints.ForeignReviewIgnored, ReadyMergeable: s.Hints.ReadyMergeable}}
	if p.Mergeable {
		entries, waiting := prviews.OwnerQueue(res.Rows)
		q := make([]DocQueueEntry, len(entries))
		for i, e := range entries {
			q[i] = DocQueueEntry{Position: e.Position, Repo: e.Repo, Number: e.Number, Title: e.Title, URL: snapcmd.StrPtr(e.URL), Review: e.Review, Reason: e.Reason, Then: e.Then}
			if pulls.ValidSHA(e.Head) {
				head := e.Head
				q[i].Head = &head
			}
		}
		d.OwnerQueue, d.Summary.WaitingForParent = &q, waiting
	}
	if p.Stack {
		f := prviews.Stacks(res.Rows)
		d.Stack = &DocStack{Trees: make([]*DocStackNode, len(f.Trees)), Singles: f.Singles}
		for i, n := range f.Trees {
			d.Stack.Trees[i] = docStackNode(n)
		}
	}
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
		MergeableState: r.MergeableState, Rebaseable: r.Rebaseable, Reviewers: snapcmd.Strings0(r.Reviewers), Group: r.Group, Reason: snapcmd.StrPtr(r.Reason), Ready: r.Ready, Detail: r.Detail,
		Hints: snapcmd.Strings0(r.Hints)}
	if rv := r.Review; rv != nil {
		d := &DocReview{Basis: "comment", State: rv.State, Verdict: snapcmd.StrPtr(rv.Verdict), PatchID: snapcmd.StrPtr(rv.PatchID), URL: snapcmd.StrPtr(rv.URL),
			Association: snapcmd.StrPtr(rv.Association), Ignored: rv.Ignored, Carried: rv.Carried, Edited: rv.Edited, Truncated: rv.Truncated}
		if pulls.ValidSHA(rv.Head) {
			head := rv.Head
			d.Head = &head
		}
		if !rv.At.IsZero() {
			at := rv.At.UTC().Format(time.RFC3339)
			d.At = &at
		}
		row.Review = d
	}
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
	if d.OwnerQueue != nil {
		return writeQueue(w, d)
	}
	if d.Stack != nil {
		return writeStack(w, d)
	}
	var b strings.Builder
	lists := map[string][]DocRow{pulls.GroupReady: d.Groups.Ready, pulls.GroupChecks: d.Groups.Checks, pulls.GroupConflict: d.Groups.Conflict, pulls.GroupDraft: d.Groups.Draft, pulls.GroupBlocked: d.Groups.Blocked}
	dash := func(s *string) string {
		if s == nil {
			return "-"
		}
		return untrusted.Esc(*s)
	}
	reviewed := d.Options.Review && hasReview(d)
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
			if reviewed {
				fmt.Fprintf(&b, "  %s", reviewWord(r.Review))
			}
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
			if len(r.Hints) > 0 {
				fmt.Fprintf(&b, "  {%s}", strings.Join(r.Hints, ", "))
			}
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	s := d.Summary
	if reviewed {
		rv, h := s.Review, s.Hints
		if line := counts([]namedCount{{"clear", rv.Clear}, {"clear-carried", rv.ClearCarried}, {"findings", rv.Findings}, {"clear-stale", rv.ClearStale}, {"none", rv.None}, {"unparsed", rv.Unparsed}, {"not read", rv.NotRead}}); line != "" {
			fmt.Fprintf(&b, "review: %s\n", line)
		}
		if line := counts([]namedCount{{"clear-not-ready", h.ClearNotReady}, {"no-final-review", h.NoFinalReview}, {"base-not-default", h.BaseNotDefault}, {"parent-merged-retarget", h.ParentMergedRetarget}, {"truncated-review-unknown", h.TruncatedReviewUnknown}, {"foreign-review-ignored", h.ForeignReviewIgnored}, {"ready-mergeable", h.ReadyMergeable}}); line != "" {
			fmt.Fprintf(&b, "hints: %s\n", line)
		}
	}
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

// hasReview reports whether any row carries a review: a reader without comment support shows no column.
func hasReview(d *Document) bool {
	for _, rows := range [][]DocRow{d.Groups.Ready, d.Groups.Checks, d.Groups.Conflict, d.Groups.Draft, d.Groups.Blocked} {
		for _, r := range rows {
			if r.Review != nil {
				return true
			}
		}
	}
	return false
}

// reviewWord is the REVIEW column: the state, "-" when the comments were not read.
func reviewWord(r *DocReview) string {
	switch {
	case r == nil:
		return "-"
	case r.State == "clear-carried":
		return "clear(carried)"
	}
	return r.State
}

type namedCount struct {
	name string
	n    int
}

// counts joins the non-zero counts as "N name, ...".
func counts(in []namedCount) string {
	var parts []string
	for _, c := range in {
		if c.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c.n, c.name))
		}
	}
	return strings.Join(parts, ", ")
}

// writeQueue prints the owner list: the entries in merge order, each with its reason and the pull
// requests that follow it, then a summary line. Untrusted text is escaped.
func writeQueue(w io.Writer, d *Document) error {
	var b strings.Builder
	q := *d.OwnerQueue
	if len(q) == 0 {
		b.WriteString("nothing can be merged now\n")
	} else {
		fmt.Fprintf(&b, "merge now (%d)\n", len(q))
		for _, e := range q {
			fmt.Fprintf(&b, "%d. %s#%d  %s\n   %s\n", e.Position, untrusted.Esc(e.Repo), e.Number, untrusted.Esc(e.Title), untrusted.Esc(e.Reason))
			if len(e.Then) > 0 {
				nums := make([]string, len(e.Then))
				for i, n := range e.Then {
					nums[i] = fmt.Sprintf("#%d", n)
				}
				fmt.Fprintf(&b, "   then: %s\n", strings.Join(nums, ", "))
			}
		}
		b.WriteByte('\n')
		fmt.Fprintf(&b, "%d can be merged now", len(q))
		if n := d.Summary.WaitingForParent; n > 0 {
			fmt.Fprintf(&b, ", %d waiting for a parent", n)
		}
		b.WriteByte('\n')
	}
	if n := d.Summary.ReposFailed; n > 0 {
		fmt.Fprintf(&b, "%s could not be read\n", snapcmd.Plural(n, "repository", "repositories"))
	}
	if n := snapcmd.OwnerErrorCount(d.Errors); n > 0 {
		fmt.Fprintf(&b, "%s with owner discovery\n", snapcmd.Plural(n, "problem", "problems"))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func docStackNode(n *prviews.Node) *DocStackNode {
	d := &DocStackNode{Repo: n.Repo, Number: n.Number, Title: n.Title, URL: snapcmd.StrPtr(n.URL), Draft: n.Draft, Group: n.Group, Checks: snapcmd.StrPtr(n.Checks),
		Review: snapcmd.StrPtr(n.Review), Hints: snapcmd.Strings0(n.Hints), Base: n.Base, HeadRef: n.HeadRef, Children: make([]*DocStackNode, len(n.Children))}
	for i, c := range n.Children {
		d.Children[i] = docStackNode(c)
	}
	return d
}

// writeStack prints the stack view: one tree per root, children drawn with "|-" and "`-", then the
// number of single pull requests left out. Untrusted text is escaped.
func writeStack(w io.Writer, d *Document) error {
	var b strings.Builder
	dash := func(s *string) string {
		if s == nil {
			return "-"
		}
		return untrusted.Esc(*s)
	}
	var line func(n *DocStackNode, root bool)
	line = func(n *DocStackNode, root bool) {
		fmt.Fprintf(&b, "%s#%d  %s  checks %s  review %s  %s", untrusted.Esc(n.Repo), n.Number, n.Group, dash(n.Checks), dash(n.Review), untrusted.Esc(n.Title))
		if root && hasString(n.Hints, "base-not-default") {
			fmt.Fprintf(&b, "  (base %s)", untrusted.Esc(n.Base))
		}
		if len(n.Hints) > 0 {
			fmt.Fprintf(&b, "  {%s}", strings.Join(n.Hints, ", "))
		}
		b.WriteByte('\n')
	}
	var children func(n *DocStackNode, prefix string)
	children = func(n *DocStackNode, prefix string) {
		for i, c := range n.Children {
			last := i == len(n.Children)-1
			branch, next := "|- ", "|  "
			if last {
				branch, next = "`- ", "   "
			}
			b.WriteString(prefix + branch)
			line(c, false)
			children(c, prefix+next)
		}
	}
	st := d.Stack
	if len(st.Trees) == 0 {
		b.WriteString("no stacked pull requests\n")
	}
	for i, t := range st.Trees {
		if i > 0 {
			b.WriteByte('\n')
		}
		line(t, true)
		children(t, "")
	}
	if st.Singles > 0 {
		fmt.Fprintf(&b, "\n%s not shown\n", snapcmd.Plural(st.Singles, "single pull request", "single pull requests"))
	}
	if n := d.Summary.ReposFailed; n > 0 {
		fmt.Fprintf(&b, "%s could not be read\n", snapcmd.Plural(n, "repository", "repositories"))
	}
	if n := snapcmd.OwnerErrorCount(d.Errors); n > 0 {
		fmt.Fprintf(&b, "%s with owner discovery\n", snapcmd.Plural(n, "problem", "problems"))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func hasString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
