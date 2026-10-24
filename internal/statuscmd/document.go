// SPDX-License-Identifier: EUPL-1.2

package statuscmd

import (
	"time"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/statusremote"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

// The bounds of the strings of the document (contracts/status.md).
const (
	maxString  = 2048
	maxMessage = 512
)

// Document is the schema 2 document of `stw status --json` (Spec 037, contracts/status.md). Keys are
// never omitted: an absent value is null, an empty list is `[]`.
type Document struct {
	SchemaVersion int              `json:"schema_version"`
	Command       string           `json:"command"`
	Time          string           `json:"time"`
	Host          string           `json:"host"`
	Options       DocOptions       `json:"options"`
	Root          *string          `json:"root"`
	Note          *string          `json:"note"`
	Actions       []DocAction      `json:"actions"`
	Repos         []DocRepo        `json:"repos"`
	Sources       []DocSource      `json:"sources"`
	Notices       []DocNotice      `json:"notices"`
	Summary       DocSummary       `json:"summary"`
	Error         *stwsync.Failure `json:"error"`
}

// DocOptions echoes the options of the run; the JSON carries no icons.
type DocOptions struct {
	Fetch     bool    `json:"fetch"`
	Worktrees bool    `json:"worktrees"`
	Icons     *string `json:"icons"`
}

// DocAction is one action item.
type DocAction struct {
	ID       string         `json:"id"`
	Priority string         `json:"priority"`
	Category string         `json:"category"`
	Repo     string         `json:"repo"`
	Path     *string        `json:"path"`
	Title    string         `json:"title"`
	URL      *string        `json:"url"`
	Subject  DocSubject     `json:"subject"`
	Evidence map[string]any `json:"evidence"`
	Command  *string        `json:"command"`
	Effect   *string        `json:"effect"`
	Hint     *string        `json:"hint"`
}

// DocSubject is the thing an item is about; the keys that do not apply are null. Name is the part of
// the id that is neither a number nor a tag: the fact kind, the branch, the path of a gitlink or a
// worktree, the id of a source.
type DocSubject struct {
	Kind   string  `json:"kind"`
	Number *int    `json:"number"`
	Tag    *string `json:"tag"`
	SHA    *string `json:"sha"`
	Name   *string `json:"name"`
}

// DocRepo is the v1 repository object unchanged plus the remote half.
type DocRepo struct {
	stwsync.RepoStatus
	Remote DocRemote `json:"remote"`
}

// DocRemote is what the remote sources read for one repository.
type DocRemote struct {
	Repo    *string       `json:"repo"`
	State   string        `json:"state"`
	Reason  *string       `json:"reason"`
	Sources DocRepoSource `json:"sources"`
}

// DocRepoSource holds the seven remote sources of one repository.
type DocRepoSource struct {
	Repo      DocSourceRead `json:"repo"`
	Pulls     DocSourceRead `json:"pulls"`
	Releases  DocSourceRead `json:"releases"`
	Tags      DocSourceRead `json:"tags"`
	CIMain    DocSourceRead `json:"ci_main"`
	CIRelease DocSourceRead `json:"ci_release"`
	Issues    DocSourceRead `json:"issues"`
}

// DocSourceRead is the outcome of one source of one repository: its data when it ended ok, the error
// code when it failed and the skip reason when it was skipped.
type DocSourceRead struct {
	State  string  `json:"state"`
	Error  *string `json:"error"`
	Reason *string `json:"reason"`
	Data   any     `json:"data"`
}

// DocRepoData is the data of the `repo` source.
type DocRepoData struct {
	DefaultBranch    string  `json:"default_branch"`
	HTMLURL          *string `json:"html_url"`
	DraftsVisibility string  `json:"drafts_visibility"`
}

// DocPullsData is the data of the `pulls` source.
type DocPullsData struct {
	Open     int `json:"open"`
	Ready    int `json:"ready"`
	Checks   int `json:"checks"`
	Conflict int `json:"conflict"`
	Draft    int `json:"draft"`
	Blocked  int `json:"blocked"`
	Red      int `json:"red"`
	Bots     int `json:"bots"`
	NotRead  int `json:"not_read"`
}

// DocDraft is a release draft.
type DocDraft struct {
	Name      *string `json:"name"`
	Tag       string  `json:"tag"`
	CreatedAt *string `json:"created_at"`
	Assets    int     `json:"assets"`
	URL       *string `json:"url"`
}

// DocReleasesData is the data of the `releases` source.
type DocReleasesData struct {
	Drafts    []DocDraft `json:"drafts"`
	Truncated bool       `json:"truncated"`
}

// DocRun is the newest run of a release workflow.
type DocRun struct {
	Workflow   string  `json:"workflow"`
	Conclusion *string `json:"conclusion"`
	Status     *string `json:"status"`
	URL        *string `json:"url"`
}

// DocLatestTag is the highest-version tag of a release tag pattern.
type DocLatestTag struct {
	Pattern string `json:"pattern"`
	Tag     string `json:"tag"`
}

// DocUnreleasedTag is a tag that has no release.
type DocUnreleasedTag struct {
	Pattern string  `json:"pattern"`
	Tag     string  `json:"tag"`
	SHA     string  `json:"sha"`
	Run     *DocRun `json:"run"`
}

// DocTagsData is the data of the `tags` source.
type DocTagsData struct {
	Latest     []DocLatestTag     `json:"latest"`
	Unreleased []DocUnreleasedTag `json:"unreleased"`
}

// DocMainData is the data of the `ci_main` source.
type DocMainData struct {
	Branch       string  `json:"branch"`
	Head         string  `json:"head"`
	Class        *string `json:"class"`
	BehindOrigin *bool   `json:"behind_origin"`
}

// DocReleaseTag is a tag entry of the `ci_release` source with its runs.
type DocReleaseTag struct {
	Pattern    string   `json:"pattern"`
	Product    string   `json:"product"`
	Tag        string   `json:"tag"`
	SHA        string   `json:"sha"`
	Latest     bool     `json:"latest"`
	Unreleased bool     `json:"unreleased"`
	Workflow   string   `json:"workflow"`
	Runs       []DocRun `json:"runs"`
}

// DocIssuesData is the data of the `issues` source: the number of open issues, their number per priority
// (so Open is the sum of the four), the pull request entries of the list that were dropped and whether the
// repository has more open items than the page limit allows.
type DocIssuesData struct {
	Open         int  `json:"open"`
	Red          int  `json:"red"`
	Yellow       int  `json:"yellow"`
	Blue         int  `json:"blue"`
	Violet       int  `json:"violet"`
	PullsDropped int  `json:"pulls_dropped"`
	Truncated    bool `json:"truncated"`
}

// DocReleaseData is the data of the `ci_release` source.
type DocReleaseData struct {
	Tags []DocReleaseTag `json:"tags"`
}

// DocSource counts the repositories per outcome of one source.
type DocSource struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	Read     int    `json:"read"`
	Degraded int    `json:"degraded"`
	Skipped  int    `json:"skipped"`
}

// DocNotice is a message about a missing source.
type DocNotice struct {
	Code    string   `json:"code"`
	Source  string   `json:"source"`
	Repos   []string `json:"repos"`
	Message string   `json:"message"`
}

// DocCounts are the item counts per priority.
type DocCounts struct {
	Red    int `json:"red"`
	Yellow int `json:"yellow"`
	Green  int `json:"green"`
	Blue   int `json:"blue"`
	Violet int `json:"violet"`
}

// DocSummary is the summary of the run.
type DocSummary struct {
	Repos      int       `json:"repos"`
	Attention  int       `json:"attention"`
	Errors     int       `json:"errors"`
	Priorities DocCounts `json:"priorities"`
	Degraded   bool      `json:"degraded"`
	Failed     bool      `json:"failed"`
	Notices    int       `json:"notices"`
}

// Document builds the schema 2 document of a report. Every string is cut at 2048 bytes (notice
// messages at 512) on a UTF-8 boundary.
func (r Report) Document() Document {
	d := Document{
		SchemaVersion: 2, Command: "status", Time: r.Time.UTC().Format(time.RFC3339), Host: statusremote.Host,
		Options: DocOptions{Fetch: r.Local.Options.Fetch, Worktrees: r.Local.Options.Worktrees},
		Root:    cutPtr(r.Local.Root), Note: cutPtr(r.Local.Note),
		Actions: make([]DocAction, len(r.Overview.Items)), Repos: make([]DocRepo, len(r.Local.Repos)),
		Notices: make([]DocNotice, len(r.Overview.Notices)), Error: r.Local.Error,
		Sources: r.sources(), Summary: summary(r.Overview.Summary),
	}
	for i, it := range r.Overview.Items {
		d.Actions[i] = action(it)
	}
	for i, rp := range r.Local.Repos {
		d.Repos[i] = DocRepo{RepoStatus: rp, Remote: remote(r.Remote.Repos[i])}
	}
	for i, n := range r.Overview.Notices {
		d.Notices[i] = DocNotice{Code: cut(n.Code, maxString), Source: n.Source, Repos: n.Repos, Message: cut(n.Message, maxMessage)}
		if n.Repos == nil { // the notice of the viewer is about the run, not about a repository
			d.Notices[i].Repos = []string{}
		}
	}
	return d
}

func summary(s overview.Summary) DocSummary {
	p := s.Priorities
	return DocSummary{
		Repos: s.Repos, Attention: s.Attention, Errors: s.Errors, Degraded: s.Degraded, Failed: s.Failed, Notices: s.Notices,
		Priorities: DocCounts{Red: p.Red, Yellow: p.Yellow, Green: p.Green, Blue: p.Blue, Violet: p.Violet},
	}
}

func action(it overview.Item) DocAction {
	a := DocAction{
		ID: it.ID, Priority: string(it.Priority), Category: string(it.Category), Repo: it.Repo, Path: nullable(it.Path),
		Title: cut(it.Title, maxString), URL: nullable(overview.HTTPSURL(it.URL)), Evidence: cutEvidence(it.Evidence),
		Command: nullable(it.Command), Effect: nullable(string(it.Effect)), Hint: nullable(it.Hint),
		Subject: DocSubject{Kind: string(it.Subject.Kind), Tag: nullable(it.Subject.Tag), SHA: nullable(it.Subject.SHA), Name: nullable(cut(it.Subject.Name, maxString))},
	}
	if it.Subject.Number != 0 {
		a.Subject.Number = &it.Subject.Number
	}
	return a
}

// cutEvidence bounds the strings of the evidence of an item, and the strings in its lists, to 2048 bytes.
// The evidence of the model is not changed.
func cutEvidence(ev map[string]any) map[string]any {
	if ev == nil {
		return nil
	}
	out := make(map[string]any, len(ev))
	for k, v := range ev {
		switch v := v.(type) {
		case string:
			out[k] = cut(v, maxString)
		case []string:
			cuts := make([]string, len(v))
			for i, e := range v {
				cuts[i] = cut(e, maxString)
			}
			out[k] = cuts
		default:
			out[k] = v
		}
	}
	return out
}

// outcome is the outcome of a source for the counts of the document. A source that is skipped because
// the repository has its issues disabled or has no release workflow has read everything there is: it is a
// fact about the repository (not applicable), as it is for the summary of the action model.
func outcome(src overview.Source, st overview.SourceState) overview.Outcome {
	if st.FactOfRepository(src) {
		return overview.OutcomeOK
	}
	return st.Outcome
}

// remote summarises the sources of one repository: ok when all are, skipped (with the reason of
// the first) when none was read and none failed, error when every one failed, degraded otherwise.
func remote(rr statusremote.RepoResult) DocRemote {
	ok, skipped, failed := 0, 0, 0
	reason := ""
	for _, src := range overview.Sources {
		switch st := rr.Remote.Sources[src]; outcome(src, st) {
		case overview.OutcomeOK:
			ok++
		case overview.OutcomeSkipped:
			if skipped == 0 {
				reason = st.Reason
			}
			skipped++
		case overview.OutcomeError:
			failed++
		}
	}
	out := DocRemote{Repo: nullable(rr.Slug), State: "degraded", Sources: sourceReads(rr)}
	switch {
	case ok == len(overview.Sources):
		out.State = "ok"
	case skipped == len(overview.Sources):
		out.State, out.Reason = "skipped", nullable(reason)
	case failed == len(overview.Sources):
		out.State = "error"
	}
	return out
}

func sourceReads(rr statusremote.RepoResult) DocRepoSource {
	read := func(src overview.Source, data func() any) DocSourceRead {
		st := rr.Remote.Sources[src]
		out := DocSourceRead{State: string(st.Outcome)}
		switch st.Outcome {
		case overview.OutcomeOK:
			out.Data = data()
		case overview.OutcomeError:
			out.Error = nullable(cut(st.Code, maxString))
		case overview.OutcomeSkipped:
			out.Reason = nullable(cut(st.Reason, maxString))
		}
		return out
	}
	return DocRepoSource{
		Repo:      read(overview.SourceRepo, func() any { return repoData(rr.Remote) }),
		Pulls:     read(overview.SourcePulls, func() any { return PullsData(rr) }),
		Releases:  read(overview.SourceReleases, func() any { return releasesData(rr.Remote) }),
		Tags:      read(overview.SourceTags, func() any { return tagsData(rr) }),
		CIMain:    read(overview.SourceCIMain, func() any { return mainData(rr.Remote.Main) }),
		CIRelease: read(overview.SourceCIRelease, func() any { return releaseData(rr.CIRelease) }),
		Issues:    read(overview.SourceIssues, func() any { return issuesData(rr.Issues) }),
	}
}

func repoData(r *overview.Remote) DocRepoData {
	return DocRepoData{DefaultBranch: cut(r.DefaultBranch, maxString), HTMLURL: nullable(overview.HTTPSURL(r.HTMLURL)), DraftsVisibility: string(r.Visibility)}
}

// PullsData is the data of the `pulls` source: the group counts of `stw pulls`, and `red`, the blocked rows whose checks are red.
func PullsData(rr statusremote.RepoResult) DocPullsData {
	c := rr.Pulls.Counts
	d := DocPullsData{Open: c.Total, Ready: c.Ready, Checks: c.Checks, Conflict: c.Conflict, Draft: c.Draft, Blocked: c.Blocked, Bots: c.Bots, NotRead: c.NotRead}
	if rr.Pulls.Open != nil {
		d.Open = *rr.Pulls.Open
	}
	for _, p := range rr.Pulls.PRs {
		if p.Group == overview.GroupBlocked && p.Reason == "checks-red" {
			d.Red++
		}
	}
	return d
}

func releasesData(r *overview.Remote) DocReleasesData {
	d := DocReleasesData{Drafts: make([]DocDraft, len(r.Drafts)), Truncated: r.DraftsTruncated}
	for i, dr := range r.Drafts {
		d.Drafts[i] = DocDraft{Name: nullable(cut(dr.Name, maxString)), Tag: cut(dr.Tag, maxString), Assets: dr.Assets, URL: nullable(overview.HTTPSURL(dr.URL))}
		if !dr.CreatedAt.IsZero() {
			d.Drafts[i].CreatedAt = nullable(dr.CreatedAt.UTC().Format(time.RFC3339))
		}
	}
	return d
}

// tagsData lists the tag entries of the tags source. When the release runs were read they carry the
// run of each unreleased tag; otherwise the entries of the overview have none.
func tagsData(rr statusremote.RepoResult) DocTagsData {
	d := DocTagsData{Latest: []DocLatestTag{}, Unreleased: []DocUnreleasedTag{}}
	add := func(pattern, tag, sha string, latest, unreleased bool, run *overview.Run) {
		if latest {
			d.Latest = append(d.Latest, DocLatestTag{Pattern: cut(pattern, maxString), Tag: cut(tag, maxString)})
		}
		if unreleased {
			d.Unreleased = append(d.Unreleased, DocUnreleasedTag{Pattern: cut(pattern, maxString), Tag: cut(tag, maxString), SHA: sha, Run: docRun(run)})
		}
	}
	if rr.CIRelease == nil {
		for _, t := range rr.Remote.Tags {
			add(t.Pattern, t.Tag, t.SHA, t.Latest, t.Unreleased, t.Run)
		}
		return d
	}
	for _, t := range rr.CIRelease.Tags {
		var run *overview.Run
		if len(t.Runs) > 0 {
			run = &t.Runs[0]
		}
		add(t.Pattern, t.Tag, t.SHA, t.Latest, t.Unreleased, run)
	}
	return d
}

func issuesData(i *statusremote.Issues) DocIssuesData {
	c := i.Counts
	return DocIssuesData{Open: i.Open, Red: c.Red, Yellow: c.Yellow, Blue: c.Blue, Violet: c.Violet, PullsDropped: i.Dropped, Truncated: i.Truncated}
}

func mainData(m *overview.Main) DocMainData {
	return DocMainData{Branch: cut(m.Branch, maxString), Head: m.Head, Class: nullable(m.Class), BehindOrigin: m.BehindOrigin}
}

func releaseData(c *statusremote.CIRelease) DocReleaseData {
	d := DocReleaseData{Tags: make([]DocReleaseTag, len(c.Tags))}
	for i, t := range c.Tags {
		runs := make([]DocRun, len(t.Runs))
		for j := range t.Runs {
			runs[j] = *docRun(&t.Runs[j])
		}
		d.Tags[i] = DocReleaseTag{
			Pattern: cut(t.Pattern, maxString), Product: cut(t.Product, maxString), Tag: cut(t.Tag, maxString), SHA: t.SHA,
			Latest: t.Latest, Unreleased: t.Unreleased, Workflow: cut(t.Workflow, maxString), Runs: runs,
		}
	}
	return d
}

func docRun(r *overview.Run) *DocRun {
	if r == nil {
		return nil
	}
	return &DocRun{Workflow: cut(r.Workflow, maxString), Conclusion: nullable(r.Conclusion), Status: nullable(r.Status), URL: nullable(overview.HTTPSURL(r.URL))}
}

// sources counts, for every source id, the repositories per outcome. The local source reads every
// repository whose state is ok; an uninitialized one is skipped and one that cannot be read is degraded.
func (r Report) sources() []DocSource {
	local := DocSource{ID: "local"}
	for _, rp := range r.Local.Repos {
		switch rp.State {
		case "ok":
			local.Read++
		case "error":
			local.Degraded++
		default:
			local.Skipped++
		}
	}
	out := []DocSource{local.withState()}
	for _, src := range overview.Sources {
		s := DocSource{ID: string(src)}
		for _, rr := range r.Remote.Repos {
			switch outcome(src, rr.Remote.Sources[src]) {
			case overview.OutcomeOK:
				s.Read++
			case overview.OutcomeError:
				s.Degraded++
			default:
				s.Skipped++
			}
		}
		out = append(out, s.withState())
	}
	return append(out, r.viewerSource().withState())
}

// viewerSource counts the one `user` read of the run, which is no repository's: read when it answered,
// degraded when it failed, skipped when it was skipped (the token class cannot read it, no request was
// made, no repository is on GitHub); a report that has no remote half (a failed local read) made no attempt
// and counts nothing.
func (r Report) viewerSource() DocSource {
	s := DocSource{ID: "viewer"}
	switch r.Remote.Viewer.Outcome {
	case overview.OutcomeOK:
		s.Read++
	case overview.OutcomeError:
		s.Degraded++
	case overview.OutcomeSkipped:
		s.Skipped++
	}
	return s
}

// withState sets the state of a source from its counts: ok when everything was read, skipped when
// nothing was read and nothing failed, error when everything failed, degraded otherwise.
func (s DocSource) withState() DocSource {
	switch {
	case s.Degraded == 0 && s.Skipped == 0:
		s.State = "ok"
	case s.Read == 0 && s.Skipped == 0:
		s.State = "error"
	case s.Read == 0 && s.Degraded == 0:
		s.State = "skipped"
	default:
		s.State = "degraded"
	}
	return s
}

// nullable is null for the empty string.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func cutPtr(s *string) *string {
	if s == nil {
		return nil
	}
	c := cut(*s, maxString)
	return &c
}

// cut shortens s to at most n bytes without splitting a UTF-8 sequence.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
