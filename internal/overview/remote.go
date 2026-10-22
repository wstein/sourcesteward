// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/wstein/sourcesteward/internal/untrusted"
)

// forgeHost is the one host of a run (Spec 037: `github.com`).
const forgeHost = "https://github.com/"

// releasingHint is the advice of a tag without release, which has no command.
const releasingHint = "see the repository's docs/releasing.md; the draft variable must be set before the tag is pushed"

// remoteItems maps what the remote sources read for one repository to its items. A repository without
// a valid OWNER/NAME has no remote items: none of its remote sources ran. Forge text enters titles
// only through untrusted.Esc, URLs only when https, commands only through buildCommand.
func remoteItems(r Repo) []Item {
	if r.Remote == nil || !untrusted.ValidRepo(r.Slug) {
		return nil
	}
	var items []Item
	items = append(items, readErrors(r)...)
	items = append(items, pullItems(r)...)
	items = append(items, draftItems(r)...)
	items = append(items, tagItems(r)...)
	items = append(items, mainItems(r)...)
	return items
}

// readErrors gives one read-error item per failed source (a skipped source, a deadline skip included, is no item).
func readErrors(r Repo) []Item {
	var items []Item
	for _, src := range Sources {
		st := r.Remote.Sources[src]
		if st.Outcome != OutcomeError {
			continue
		}
		it := newItem(ReadError, r.Slug, Subject{Kind: KindSource, Name: string(src)}, string(src), target{Slug: r.Slug, Source: src})
		code := untrusted.Esc(st.Code)
		it.Path, it.Title, it.Evidence = r.Local.Path, fmt.Sprintf("%s could not be read: %s", src, code), map[string]any{"source": string(src), "code": code}
		items = append(items, it)
	}
	return items
}

// pullItems applies the pull request rules in the order of the spec: a draft is never an item, a bot
// PR is pr-bot whatever its group, a human PR past the detail budget is pr-unread, the others follow
// their verdict group. A row marked not-read for any other reason is no item: its failed or skipped
// read is a read-error of the repository.
func pullItems(r Repo) []Item {
	var items []Item
	for _, p := range r.Remote.Pulls {
		var c Category
		var what string
		switch {
		case p.Draft:
			continue
		case p.Bot:
			c, what = PRBot, fmt.Sprintf("bot PR #%d (%s)", p.Number, untrusted.Esc(p.Author))
		case p.PastBudget:
			c, what = PRUnread, fmt.Sprintf("PR #%d was not read (detail budget spent)", p.Number)
		case p.Group == GroupReady:
			c, what = PRReady, fmt.Sprintf("PR #%d is ready to merge", p.Number)
		case p.Group == GroupConflict:
			c, what = PRConflict, fmt.Sprintf("PR #%d has conflicts", p.Number)
		case p.Group == GroupChecks:
			c, what = PRChecksPending, fmt.Sprintf("PR #%d is waiting for checks", p.Number)
		case p.Group == GroupBlocked && p.Reason != PRReasonNotRead:
			c, what = PRBlocked, fmt.Sprintf("PR #%d is blocked (%s)", p.Number, untrusted.Esc(p.Reason))
		default:
			continue
		}
		it := newItem(c, r.Slug, Subject{Kind: KindPR, Number: p.Number}, strconv.Itoa(p.Number), target{Slug: r.Slug, Number: p.Number})
		it.Path, it.URL = r.Local.Path, httpsURL(p.URL)
		it.Title = what + ": " + untrusted.Esc(p.Title)
		it.Evidence = map[string]any{"group": untrusted.Esc(p.Group)}
		if p.Reason != "" {
			it.Evidence["reason"] = untrusted.Esc(p.Reason)
		}
		if p.PastBudget {
			it.Evidence["past_budget"] = true
		}
		items = append(items, it)
	}
	return items
}

// draftItems gives one release-draft item per draft, only when the token can see drafts: otherwise the
// drafts are unknown, not zero, and the notice says so. GitHub allows several drafts with one tag name (and
// drafts without a usable one), so the id part is the tag only when it passes the ref validation and no other
// draft of the repository has it; otherwise it is the fingerprint of tag, name and creation time, all taken
// from the data, never from a clock. Two drafts that agree in all three are one finding and collapse in sortItems.
func draftItems(r Repo) []Item {
	if r.Remote.Visibility != WriteAccess {
		return nil
	}
	tags := map[string]int{}
	for _, d := range r.Remote.Drafts {
		tags[d.Tag]++
	}
	var items []Item
	for _, d := range r.Remote.Drafts {
		part := d.Tag
		if tags[d.Tag] != 1 || !validRef(d.Tag) {
			part = fingerprint(d.Tag + "\x00" + d.Name + "\x00" + d.CreatedAt.UTC().Format(time.RFC3339Nano))
		}
		it := newItem(ReleaseDraft, r.Slug, Subject{Kind: KindRelease, Tag: d.Tag}, part, target{Slug: r.Slug, Tag: d.Tag})
		it.Path, it.URL = r.Local.Path, httpsURL(d.URL)
		it.Title = fmt.Sprintf("release draft %s (%d assets) is waiting", untrusted.Esc(d.Tag), d.Assets)
		it.Evidence = map[string]any{"drafts_visibility": string(WriteAccess), "assets": d.Assets}
		items = append(items, it)
	}
	return items
}

// failedRun reports whether a release workflow run ended without success: failure, cancelled or timed_out.
func failedRun(run *Run) bool {
	return run != nil && (run.Conclusion == "failure" || run.Conclusion == "cancelled" || run.Conclusion == "timed_out")
}

// tagFinding is what is known about one tag after merging its entries.
type tagFinding struct {
	tag, sha   string
	unreleased bool
	failed     *Run
}

// tagItems gives exactly one item per tag: release-run-failed when its newest release run failed
// (released or not, latest or unreleased), else tag-without-release for an unreleased tag. Entries of
// the same tag merge in a fixed order, so the result does not depend on the order of the input.
func tagItems(r Repo) []Item {
	entries := append([]ReleaseTag(nil), r.Remote.Tags...)
	sort.SliceStable(entries, func(i, j int) bool { return tagKey(entries[i]) < tagKey(entries[j]) })
	var order []string
	byTag := map[string]*tagFinding{}
	for _, e := range entries {
		f := byTag[e.Tag]
		if f == nil {
			f = &tagFinding{tag: e.Tag, sha: e.SHA}
			byTag[e.Tag] = f
			order = append(order, e.Tag)
		}
		f.unreleased = f.unreleased || e.Unreleased
		if f.failed == nil && failedRun(e.Run) {
			f.failed = e.Run
		}
	}
	var items []Item
	for _, name := range order {
		f := byTag[name]
		s := Subject{Kind: KindTag, Tag: f.tag, SHA: f.sha}
		var it Item
		switch {
		case f.failed != nil:
			it = newItem(ReleaseRunFailed, r.Slug, s, idToken(f.tag, validRef), target{Slug: r.Slug, SHA: f.sha})
			it.URL = httpsURL(f.failed.URL)
			it.Title = fmt.Sprintf("release workflow run of %s %s", untrusted.Esc(f.tag), untrusted.Esc(f.failed.Conclusion))
			it.Evidence = map[string]any{"conclusion": untrusted.Esc(f.failed.Conclusion), "released": !f.unreleased}
		case f.unreleased:
			it = newItem(TagWithoutRelease, r.Slug, s, idToken(f.tag, validRef), target{Slug: r.Slug})
			it.Title = fmt.Sprintf("tag %s has no release", untrusted.Esc(f.tag))
			it.Hint = releasingHint
		default:
			continue
		}
		it.Path = r.Local.Path
		items = append(items, it)
	}
	return items
}

// tagKey orders the entries of one tag: failed runs first (smallest URL), then the rest by sha.
func tagKey(e ReleaseTag) string {
	failed := "1"
	url := ""
	if failedRun(e.Run) {
		failed, url = "0", e.Run.URL
	}
	return e.Tag + "\x00" + failed + "\x00" + url + "\x00" + e.SHA + "\x00" + strconv.FormatBool(e.Unreleased)
}

// mainItems gives main-red for class red and main-behind-origin when the local default branch lacks origin's head.
func mainItems(r Repo) []Item {
	m := r.Remote.Main
	if m == nil {
		return nil
	}
	var items []Item
	s := Subject{Kind: KindBranch, Name: m.Branch, SHA: m.Head}
	if m.Class == "red" {
		it := newItem(MainRed, r.Slug, s, idToken(m.Branch, validRef), target{Slug: r.Slug, Branch: m.Branch})
		it.Path, it.URL = r.Local.Path, forgeHost+r.Slug+"/actions"
		it.Title = fmt.Sprintf("default branch CI is red (%s, %s)", untrusted.Esc(m.Branch), untrusted.Esc(short(m.Head)))
		it.Evidence = map[string]any{"class": "red"}
		items = append(items, it)
	}
	if m.BehindOrigin != nil && *m.BehindOrigin {
		it := newItem(MainBehindOrigin, r.Slug, s, idToken(m.Branch, validRef), target{Slug: r.Slug, Path: r.Local.Path})
		it.Path, it.URL = r.Local.Path, forgeHost+r.Slug
		it.Title = fmt.Sprintf("origin has commits of %s that the local clone lacks", untrusted.Esc(m.Branch))
		items = append(items, it)
	}
	return items
}
