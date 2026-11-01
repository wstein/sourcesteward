// SPDX-License-Identifier: EUPL-1.2

package pullscmd

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/wstein/sourcesteward/internal/conflicts"
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
	// Spec 047 member (additive): null without --conflicts
	Conflicts *DocConflicts `json:"conflicts"`
	limit     int           // for the human renderer; not part of the contract
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
	// Spec 047 members (additive)
	Conflicts   bool `json:"conflicts"`
	RestackPlan bool `json:"restack_plan"`
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
	// Spec 047 member (additive): null when the base conflicts were not probed
	BaseConflict *DocBaseConflict `json:"base_conflict"`
}

// DocBaseConflict is the probe merge of a row against its base (Spec 047).
type DocBaseConflict struct {
	State     string    `json:"state"`
	Reason    *string   `json:"reason"`
	Base      string    `json:"base"`
	BaseSHA   *string   `json:"base_sha"`
	MergeBase *string   `json:"merge_base"`
	Files     []DocFile `json:"files"`
	Overlap   []string  `json:"overlap"`
	Truncated bool      `json:"truncated"`
	BlockedBy *int      `json:"blocked_by"`
}

// DocFile is a conflicting file and the base commit that last touched it.
type DocFile struct {
	Path       string  `json:"path"`
	BaseCommit *string `json:"base_commit"`
}

// DocConflicts is the stack view and the restack plans (Spec 047).
type DocConflicts struct {
	Remote string             `json:"remote"`
	Stacks []DocConflictStack `json:"stacks"`
	Plans  []DocPlan          `json:"plans"`
}

// DocConflictStack is a tree of pull requests of one repository (Spec 047).
type DocConflictStack struct {
	Repo   string     `json:"repo"`
	Root   int        `json:"root"`
	Levels []DocLevel `json:"levels"`
}

// DocLevel is one pull request of a stack.
type DocLevel struct {
	Number   int     `json:"number"`
	Depth    int     `json:"depth"`
	Parent   *int    `json:"parent"`
	Head     *string `json:"head"`
	Clear    string  `json:"clear"`
	CI       string  `json:"ci"`
	Conflict string  `json:"conflict"`
	Status   *string `json:"status"`
}

// DocPlan is the restack plan of one stack with its brief.
type DocPlan struct {
	Repo    string    `json:"repo"`
	Remote  string    `json:"remote"`
	Root    int       `json:"root"`
	Steps   []DocStep `json:"steps"`
	GhStack []string  `json:"gh_stack"`
	Brief   string    `json:"brief"`
}

// DocStep restacks one pull request; Push is run only after the review of the new head.
type DocStep struct {
	Number  int      `json:"number"`
	HeadRef string   `json:"head_ref"`
	OldHead string   `json:"old_head"`
	Why     string   `json:"why"`
	Skip    *string  `json:"skip"`
	Rebase  []string `json:"rebase"`
	Verify  []string `json:"verify"`
	Push    *string  `json:"push"`
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
	// Spec 047 member (additive)
	BaseConflicts int `json:"base_conflicts"`
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
		Mergeable: p.Mergeable, Stack: p.Stack, Review: res.ReviewOn, ReviewStates: snapcmd.Strings0(p.Options.ReviewStates), Hints: snapcmd.Strings0(p.Options.Hints),
		Conflicts: res.ConflictsOn, RestackPlan: p.RestackPlan && res.ConflictsOn}
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
			ForeignReviewIgnored: s.Hints.ForeignReviewIgnored, ReadyMergeable: s.Hints.ReadyMergeable},
		BaseConflicts: s.BaseConflicts}
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
	if res.ConflictsOn {
		d.Conflicts = docConflicts(p, res)
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
	if bc := r.BaseConflict; bc != nil {
		row.BaseConflict = docBaseConflict(bc)
	}
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
			if r.BaseConflict != nil && r.BaseConflict.State == conflicts.StateConflicts {
				b.WriteString("  conflicts-with-base")
			}
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	if d.Conflicts != nil {
		writeConflicts(&b, d)
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
	if s.BaseConflicts > 0 {
		if s.BaseConflicts == 1 {
			b.WriteString("1 pull request conflicts with its base\n")
		} else {
			fmt.Fprintf(&b, "%d pull requests conflict with their base\n", s.BaseConflicts)
		}
	}
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

func shaPtr(s string) *string {
	if pulls.ValidSHA(s) {
		return &s
	}
	return nil
}

func docBaseConflict(f *conflicts.Finding) *DocBaseConflict {
	d := &DocBaseConflict{State: f.State, Reason: snapcmd.StrPtr(f.Reason), Base: f.Base, BaseSHA: shaPtr(f.BaseSHA), MergeBase: shaPtr(f.MergeBase),
		Files: []DocFile{}, Overlap: snapcmd.Strings0(f.Overlap), Truncated: f.Truncated}
	for _, fc := range f.Files {
		d.Files = append(d.Files, DocFile{Path: fc.Path, BaseCommit: shaPtr(fc.BaseCommit)})
	}
	if f.BlockedBy > 0 {
		n := f.BlockedBy
		d.BlockedBy = &n
	}
	return d
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

func docConflicts(p Params, res *pulls.Result) *DocConflicts {
	d := &DocConflicts{Remote: p.Remote, Stacks: []DocConflictStack{}, Plans: []DocPlan{}}
	for _, rc := range res.Conflicts {
		for _, st := range rc.Stacks {
			ds := DocConflictStack{Repo: rc.Repo, Root: st.Root, Levels: []DocLevel{}}
			for _, l := range st.Levels {
				dl := DocLevel{Number: l.Number, Depth: l.Depth, Head: shaPtr(l.Head), Clear: l.Clear, CI: l.CI, Conflict: l.Conflict, Status: snapcmd.StrPtr(l.Status)}
				if l.Parent > 0 {
					n := l.Parent
					dl.Parent = &n
				}
				ds.Levels = append(ds.Levels, dl)
			}
			d.Stacks = append(d.Stacks, ds)
		}
		for _, pl := range rc.Plans {
			dp := DocPlan{Repo: pl.Repo, Remote: pl.Remote, Root: pl.Root, Steps: []DocStep{}, GhStack: snapcmd.Strings0(pl.GhStack), Brief: pl.Brief}
			for _, s := range pl.Steps {
				dp.Steps = append(dp.Steps, DocStep{Number: s.Number, HeadRef: s.HeadRef, OldHead: s.OldHead, Why: s.Why, Skip: snapcmd.StrPtr(s.Skip),
					Rebase: snapcmd.Strings0(s.Rebase), Verify: snapcmd.Strings0(s.Verify), Push: snapcmd.StrPtr(s.Push)})
			}
			d.Plans = append(d.Plans, dp)
		}
	}
	return d
}

// writeConflicts prints the base conflicts, the early warnings, the stacks and the briefs of a run
// with --conflicts. Every untrusted field goes through untrusted.Esc.
func writeConflicts(b *strings.Builder, d *Document) {
	var conflicting, warned []DocRow
	for _, rows := range [][]DocRow{d.Groups.Ready, d.Groups.Checks, d.Groups.Conflict, d.Groups.Draft, d.Groups.Blocked} {
		for _, r := range rows {
			if r.BaseConflict == nil {
				continue
			}
			switch {
			case r.BaseConflict.State == conflicts.StateConflicts:
				conflicting = append(conflicting, r)
			case len(r.BaseConflict.Overlap) > 0:
				warned = append(warned, r)
			}
		}
	}
	byNumber := func(rows []DocRow) {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Number < rows[j].Number })
	}
	byNumber(conflicting)
	byNumber(warned)
	if len(conflicting) > 0 {
		fmt.Fprintf(b, "base conflicts (%d)\n", len(conflicting))
		for _, r := range conflicting {
			bc := r.BaseConflict
			var files []string
			for _, f := range bc.Files {
				one := untrusted.Esc(f.Path)
				if f.BaseCommit != nil {
					one += fmt.Sprintf(" (last changed on %s by %s)", untrusted.Esc(bc.Base), (*f.BaseCommit)[:12])
				}
				files = append(files, one)
			}
			more := ""
			if bc.Truncated {
				more = ", more not listed"
			}
			fmt.Fprintf(b, "%s#%d conflicts with %s: %s%s\n", untrusted.Esc(r.Repo), r.Number, untrusted.Esc(bc.Base), strings.Join(files, ", "), more)
		}
		b.WriteByte('\n')
	}
	if len(warned) > 0 {
		fmt.Fprintf(b, "early warning (%d)\n", len(warned))
		for _, r := range warned {
			names := make([]string, len(r.BaseConflict.Overlap))
			for i, n := range r.BaseConflict.Overlap {
				names[i] = untrusted.Esc(n)
			}
			fmt.Fprintf(b, "%s#%d: %s also changed %s\n", untrusted.Esc(r.Repo), r.Number, untrusted.Esc(r.BaseConflict.Base), strings.Join(names, ", "))
		}
		b.WriteByte('\n')
	}
	if c := d.Conflicts; c != nil {
		if len(c.Stacks) > 0 {
			b.WriteString("stacks\n")
			for _, st := range c.Stacks {
				fmt.Fprintf(b, "%s\n", untrusted.Esc(st.Repo))
				for _, l := range st.Levels {
					head := "-"
					if l.Head != nil {
						head = (*l.Head)[:12]
					}
					status := ""
					if l.Status != nil {
						status = "  " + *l.Status
					}
					fmt.Fprintf(b, "%s#%d  %s  clear %s  ci %s  %s%s\n", strings.Repeat("  ", l.Depth+1), l.Number, head, l.Clear, l.CI, l.Conflict, status)
				}
			}
			b.WriteByte('\n')
		}
		for _, pl := range c.Plans {
			b.WriteString(pl.Brief)
			b.WriteByte('\n')
		}
	}
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
