// SPDX-License-Identifier: EUPL-1.2
package statusremote

import (
	"context"
	"errors"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// MaxDrafts is the number of release drafts of one repository that the overview keeps.
const MaxDrafts = 100

// Reader is the part of the status reader the release sources use; *hosting.WatchReader is one.
type Reader interface {
	RepoInfo(ctx context.Context, repo string) (hosting.RepoInfo, error)
	ReleaseList(ctx context.Context, repo string) ([]hosting.StatusRelease, error)
	TagList(ctx context.Context, repo string) ([]hosting.StatusTag, error)
}

// Releases is what the `repo`, `releases` and `tags` sources read for one repository. A field is
// meaningful only when its source ended `ok` (see Sources).
type Releases struct {
	Sources       map[overview.Source]overview.SourceState
	DefaultBranch string
	HTMLURL       string
	// Visibility says whether the token can see drafts; Drafts is empty unless it is write-access,
	// and an empty list of any other visibility never means "no drafts".
	Visibility overview.Visibility
	Drafts     []overview.Draft
	// Truncated is true when the repository has more than MaxDrafts drafts.
	Truncated bool
	// Tags holds the unreleased tags (Unreleased true) and the latest tag of each pattern, without
	// the workflow run, which the CI source adds.
	Tags []overview.ReleaseTag
}

// ReadReleases reads `repos/OWNER/NAME`, its releases and its tags, in this order and one request at a
// time, and applies the draft visibility and the unreleased-tag rule over patterns (the release tag
// patterns of the repository's products; none means the default). The sources are independent: a
// failed repository read leaves the visibility unknown but the releases and tags are still read; a
// failed releases read leaves the tags with their latest tag only, because without the releases
// nothing can be called unreleased. The first rate-limit error stops the reads of this repository.
// A context that is done, before or during a read, makes the unread sources `skipped` with reason
// `deadline`; any other failure is an `error` with the code of the reader.
func ReadReleases(ctx context.Context, rd Reader, slug string, patterns []string) Releases {
	out := Releases{Sources: map[overview.Source]overview.SourceState{}, Visibility: overview.Unknown}
	if !untrusted.ValidRepo(slug) {
		for _, src := range releaseSources {
			out.Sources[src] = overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonInvalidRepo}
		}
		return out
	}
	stopped := ""
	read := func(src overview.Source, do func() error) bool {
		if ctx.Err() != nil {
			out.Sources[src] = overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline}
			return false
		}
		if stopped != "" {
			out.Sources[src] = overview.SourceState{Outcome: overview.OutcomeError, Code: stopped}
			return false
		}
		if err := do(); err != nil {
			out.Sources[src] = failure(ctx, err)
			if code := out.Sources[src].Code; code == overview.CodeRateLimited {
				stopped = code
			}
			return false
		}
		out.Sources[src] = overview.SourceState{Outcome: overview.OutcomeOK}
		return true
	}
	read(overview.SourceRepo, func() error {
		info, err := rd.RepoInfo(ctx, slug)
		if err == nil {
			out.DefaultBranch, out.HTMLURL, out.Visibility = info.DefaultBranch, info.HTMLURL, visibility(info)
		}
		return err
	})
	var releases []hosting.StatusRelease
	releasesOK := read(overview.SourceReleases, func() (err error) {
		releases, err = rd.ReleaseList(ctx, slug)
		return err
	})
	var tags []hosting.StatusTag
	if read(overview.SourceTags, func() (err error) {
		tags, err = rd.TagList(ctx, slug)
		return err
	}) {
		found := hosting.FindTags(tags, releases, patterns)
		if !releasesOK {
			found.Unreleased = nil
		}
		out.Tags = releaseTags(found)
	}
	if releasesOK && out.Visibility == overview.WriteAccess {
		out.Drafts, out.Truncated = drafts(releases)
	}
	return out
}

// Apply stores what was read into the remote data of the repository: the default branch and page, the
// visibility, drafts (and whether they were cut) and tags, and the states of the three sources next to the states other sources already wrote.
func (r Releases) Apply(into *overview.Remote) {
	if into.Sources == nil {
		into.Sources = map[overview.Source]overview.SourceState{}
	}
	for src, st := range r.Sources {
		into.Sources[src] = st
	}
	into.DefaultBranch, into.HTMLURL = r.DefaultBranch, r.HTMLURL
	into.Visibility, into.Drafts, into.DraftsTruncated, into.Tags = r.Visibility, r.Drafts, r.Truncated, r.Tags
}

var releaseSources = []overview.Source{overview.SourceRepo, overview.SourceReleases, overview.SourceTags}

// failure classifies a failed read: a done context is a deadline skip (nothing was wrong with the
// request), everything else an error with the reader's code.
func failure(ctx context.Context, err error) overview.SourceState {
	if ctx.Err() != nil {
		return overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline}
	}
	var we *hosting.WatchError
	if errors.As(err, &we) {
		return overview.SourceState{Outcome: overview.OutcomeError, Code: we.Code}
	}
	return overview.SourceState{Outcome: overview.OutcomeError, Code: hosting.CodeMalformed}
}

func visibility(info hosting.RepoInfo) overview.Visibility {
	switch info.DraftsVisibility() {
	case hosting.DraftsWriteAccess:
		return overview.WriteAccess
	case hosting.DraftsNoWriteAccess:
		return overview.NoWriteAccess
	}
	return overview.Unknown
}

// drafts keeps the first MaxDrafts drafts in the order GitHub gave them.
func drafts(releases []hosting.StatusRelease) ([]overview.Draft, bool) {
	var out []overview.Draft
	for _, rel := range releases {
		if !rel.Draft {
			continue
		}
		if len(out) == MaxDrafts {
			return out, true
		}
		out = append(out, overview.Draft{Name: rel.Name, Tag: rel.Tag, URL: rel.URL, Assets: rel.Assets, CreatedAt: rel.CreatedAt})
	}
	return out, false
}

// releaseTags lists the unreleased tags and then the latest tags that are not unreleased.
func releaseTags(found hosting.TagFindings) []overview.ReleaseTag {
	var out []overview.ReleaseTag
	listed := map[string]bool{}
	for _, t := range found.Unreleased {
		listed[t.Name] = true
		out = append(out, overview.ReleaseTag{Tag: t.Name, SHA: t.SHA, Unreleased: true})
	}
	for _, t := range found.Latest {
		if !listed[t.Name] {
			out = append(out, overview.ReleaseTag{Tag: t.Name, SHA: t.SHA})
		}
	}
	return out
}
