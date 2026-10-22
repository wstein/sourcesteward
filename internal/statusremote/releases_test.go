// SPDX-License-Identifier: EUPL-1.2
package statusremote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

// fakeReleaseReader answers the three reads from fields and records the order of the calls.
type fakeReleaseReader struct {
	info    hosting.RepoInfo
	infoErr error
	rels    []hosting.StatusRelease
	relsErr error
	tags    []hosting.StatusTag
	tagsErr error
	calls   []string
	onCall  func(name string)
}

func (f *fakeReleaseReader) note(name string) {
	f.calls = append(f.calls, name)
	if f.onCall != nil {
		f.onCall(name)
	}
}

func (f *fakeReleaseReader) RepoInfo(context.Context, string) (hosting.RepoInfo, error) {
	f.note("repo")
	return f.info, f.infoErr
}

func (f *fakeReleaseReader) ReleaseList(context.Context, string) ([]hosting.StatusRelease, error) {
	f.note("releases")
	return f.rels, f.relsErr
}

func (f *fakeReleaseReader) TagList(context.Context, string) ([]hosting.StatusTag, error) {
	f.note("tags")
	return f.tags, f.tagsErr
}

func relTrue() *bool  { b := true; return &b }
func relFalse() *bool { b := false; return &b }

func writeAccessInfo() hosting.RepoInfo {
	return hosting.RepoInfo{DefaultBranch: "main", HTMLURL: "https://github.com/o/r", Admin: relFalse(), Maintain: relFalse(), Push: relTrue()}
}

func relState(o overview.Outcome, reason, code string) overview.SourceState {
	return overview.SourceState{Outcome: o, Reason: reason, Code: code}
}

func relWatchErr(code string) error { return &hosting.WatchError{Code: code, Message: "m"} }

var relOK = relState(overview.OutcomeOK, "", "")

func relSHA(c string) string { return strings.Repeat(c, 40) }

func TestReadReleasesAllSourcesOK(t *testing.T) {
	created := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	rd := &fakeReleaseReader{
		info: writeAccessInfo(),
		rels: []hosting.StatusRelease{
			{Draft: true, Name: "Next", Tag: "v1.1.0", URL: "https://github.com/o/r/releases/1", Assets: 4, CreatedAt: created},
			{Tag: "v1.0.0", URL: "https://github.com/o/r/releases/0"},
		},
		tags: []hosting.StatusTag{{Name: "v1.0.0", SHA: relSHA("a")}, {Name: "v1.1.0", SHA: relSHA("b")}, {Name: "v1.2.0", SHA: relSHA("c")}},
	}
	got := ReadReleases(context.Background(), rd, "o/r", nil)
	if !reflect.DeepEqual(rd.calls, []string{"repo", "releases", "tags"}) {
		t.Fatalf("%v", rd.calls)
	}
	for _, src := range []overview.Source{overview.SourceRepo, overview.SourceReleases, overview.SourceTags} {
		if got.Sources[src] != relOK {
			t.Errorf("%s: %+v", src, got.Sources[src])
		}
	}
	if len(got.Sources) != 3 {
		t.Fatalf("only its own sources: %v", got.Sources)
	}
	if got.DefaultBranch != "main" || got.HTMLURL != "https://github.com/o/r" || got.Visibility != overview.WriteAccess {
		t.Fatalf("%+v", got)
	}
	wantDrafts := []overview.Draft{{Name: "Next", Tag: "v1.1.0", URL: "https://github.com/o/r/releases/1", Assets: 4, CreatedAt: created}}
	if !reflect.DeepEqual(got.Drafts, wantDrafts) || got.Truncated {
		t.Fatalf("%+v", got.Drafts)
	}
	wantTags := []overview.ReleaseTag{{Pattern: "v*.*.*", Tag: "v1.2.0", SHA: relSHA("c"), Latest: true, Unreleased: true}}
	if !reflect.DeepEqual(got.Tags, wantTags) {
		t.Fatalf("%+v", got.Tags)
	}
}

func TestReadReleasesLatestTagThatIsReleasedStaysListed(t *testing.T) {
	rd := &fakeReleaseReader{info: writeAccessInfo(), rels: []hosting.StatusRelease{{Tag: "v1.0.0"}}, tags: []hosting.StatusTag{{Name: "v1.0.0", SHA: relSHA("a")}}}
	got := ReadReleases(context.Background(), rd, "o/r", nil)
	if !reflect.DeepEqual(got.Tags, []overview.ReleaseTag{{Pattern: "v*.*.*", Tag: "v1.0.0", SHA: relSHA("a"), Latest: true}}) {
		t.Fatalf("%+v", got.Tags)
	}
}

func TestReadReleasesPatternsAreTheCallersChoice(t *testing.T) {
	rd := &fakeReleaseReader{info: writeAccessInfo(), rels: []hosting.StatusRelease{{Tag: "bdock/1.0.0"}}, tags: []hosting.StatusTag{{Name: "bdock/1.0.0", SHA: relSHA("a")}, {Name: "bdock/1.1.0", SHA: relSHA("b")}, {Name: "v5.0.0", SHA: relSHA("c")}}}
	got := ReadReleases(context.Background(), rd, "o/r", []string{"bdock/*"})
	if !reflect.DeepEqual(got.Tags, []overview.ReleaseTag{{Pattern: "bdock/*", Tag: "bdock/1.1.0", SHA: relSHA("b"), Latest: true, Unreleased: true}}) {
		t.Fatalf("%+v", got.Tags)
	}
}

// Drafts exist in the answer but the token cannot be trusted to see all of them: none are reported
// and the visibility says why, so that the output never states "0 drafts".
func TestReadReleasesDraftsOnlyWithWriteAccess(t *testing.T) {
	draft := []hosting.StatusRelease{{Draft: true, Tag: "v1.1.0"}}
	for name, tc := range map[string]struct {
		info hosting.RepoInfo
		err  error
		want overview.Visibility
	}{
		"no write access": {hosting.RepoInfo{DefaultBranch: "main", HTMLURL: "u", Admin: relFalse(), Maintain: relFalse(), Push: relFalse()}, nil, overview.NoWriteAccess},
		"unknown":         {hosting.RepoInfo{DefaultBranch: "main", HTMLURL: "u"}, nil, overview.Unknown},
		"repo failed":     {hosting.RepoInfo{}, relWatchErr(hosting.CodeHTTP5xx), overview.Unknown},
	} {
		got := ReadReleases(context.Background(), &fakeReleaseReader{info: tc.info, infoErr: tc.err, rels: draft}, "o/r", nil)
		if got.Visibility != tc.want || len(got.Drafts) != 0 {
			t.Errorf("%s: %s %+v", name, got.Visibility, got.Drafts)
		}
		if got.Sources[overview.SourceReleases] != relOK {
			t.Errorf("%s: the releases are still read: %+v", name, got.Sources)
		}
	}
}

func TestReadReleasesMaintainAndAdminSeeDrafts(t *testing.T) {
	for name, info := range map[string]hosting.RepoInfo{
		"maintain": {DefaultBranch: "m", HTMLURL: "u", Admin: relFalse(), Maintain: relTrue(), Push: relFalse()},
		"admin":    {DefaultBranch: "m", HTMLURL: "u", Admin: relTrue(), Maintain: relFalse(), Push: relFalse()},
	} {
		got := ReadReleases(context.Background(), &fakeReleaseReader{info: info, rels: []hosting.StatusRelease{{Draft: true, Tag: "v1"}}}, "o/r", nil)
		if got.Visibility != overview.WriteAccess || len(got.Drafts) != 1 {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

func TestReadReleasesCapsDraftsAtOneHundred(t *testing.T) {
	var rels []hosting.StatusRelease
	for i := 0; i < MaxDrafts+1; i++ {
		rels = append(rels, hosting.StatusRelease{Draft: true, Tag: fmt.Sprintf("v1.0.%d", i)})
	}
	got := ReadReleases(context.Background(), &fakeReleaseReader{info: writeAccessInfo(), rels: rels}, "o/r", nil)
	if len(got.Drafts) != MaxDrafts || !got.Truncated || got.Drafts[0].Tag != "v1.0.0" {
		t.Fatalf("%d %v", len(got.Drafts), got.Truncated)
	}
	exact := ReadReleases(context.Background(), &fakeReleaseReader{info: writeAccessInfo(), rels: rels[:MaxDrafts]}, "o/r", nil)
	if len(exact.Drafts) != MaxDrafts || exact.Truncated {
		t.Fatalf("%d %v", len(exact.Drafts), exact.Truncated)
	}
}

func TestReadReleasesSourcesAreIndependent(t *testing.T) {
	rd := &fakeReleaseReader{info: writeAccessInfo(), relsErr: relWatchErr(hosting.CodeTooManyReleases), tags: []hosting.StatusTag{{Name: "v1.0.0", SHA: relSHA("a")}, {Name: "v1.1.0", SHA: relSHA("b")}}}
	got := ReadReleases(context.Background(), rd, "o/r", nil)
	if got.Sources[overview.SourceReleases] != relState(overview.OutcomeError, "", hosting.CodeTooManyReleases) || got.Sources[overview.SourceTags] != relOK || got.Sources[overview.SourceRepo] != relOK {
		t.Fatalf("%+v", got.Sources)
	}
	// Without the releases nothing is unreleased: only the latest tag is known.
	if !reflect.DeepEqual(got.Tags, []overview.ReleaseTag{{Pattern: "v*.*.*", Tag: "v1.1.0", SHA: relSHA("b"), Latest: true}}) || len(got.Drafts) != 0 {
		t.Fatalf("%+v", got)
	}
	tagsFail := ReadReleases(context.Background(), &fakeReleaseReader{info: writeAccessInfo(), rels: []hosting.StatusRelease{{Draft: true, Tag: "v1"}}, tagsErr: relWatchErr(hosting.CodePageTooLarge)}, "o/r", nil)
	if tagsFail.Sources[overview.SourceTags] != relState(overview.OutcomeError, "", hosting.CodePageTooLarge) || len(tagsFail.Drafts) != 1 || tagsFail.Tags != nil {
		t.Fatalf("%+v", tagsFail)
	}
}

func TestReadReleasesErrorCodesPassThrough(t *testing.T) {
	for _, code := range []string{hosting.CodeUnauthorized, hosting.CodeForbidden, hosting.CodeHTTP5xx, hosting.CodeMalformed, hosting.CodePageTooLarge, hosting.CodeRepoMoved, hosting.CodeNetwork, hosting.CodeTooManyReleases} {
		got := ReadReleases(context.Background(), &fakeReleaseReader{info: writeAccessInfo(), relsErr: relWatchErr(code)}, "o/r", nil)
		if got.Sources[overview.SourceReleases] != relState(overview.OutcomeError, "", code) {
			t.Errorf("%s: %+v", code, got.Sources[overview.SourceReleases])
		}
	}
	got := ReadReleases(context.Background(), &fakeReleaseReader{info: writeAccessInfo(), relsErr: errors.New("boom")}, "o/r", nil)
	if got.Sources[overview.SourceReleases] != relState(overview.OutcomeError, "", hosting.CodeMalformed) {
		t.Fatalf("%+v", got.Sources)
	}
}

// A per-request timeout is a network failure of an attempted request: only the overall context
// makes a read a deadline skip.
func TestReadReleasesRequestTimeoutIsNotTheDeadline(t *testing.T) {
	timeout := &hosting.WatchError{Code: hosting.CodeNetwork, Message: "m", Status: 0}
	got := ReadReleases(context.Background(), &fakeReleaseReader{info: writeAccessInfo(), relsErr: errors.Join(timeout, context.DeadlineExceeded)}, "o/r", nil)
	if got.Sources[overview.SourceReleases] != relState(overview.OutcomeError, "", hosting.CodeNetwork) || got.Sources[overview.SourceTags] != relOK {
		t.Fatalf("%+v", got.Sources)
	}
}

func TestReadReleasesRateLimitStopsTheRepository(t *testing.T) {
	rd := &fakeReleaseReader{info: writeAccessInfo(), infoErr: relWatchErr(hosting.CodeRateLimited)}
	got := ReadReleases(context.Background(), rd, "o/r", nil)
	limited := relState(overview.OutcomeError, "", overview.CodeRateLimited)
	if got.Sources[overview.SourceRepo] != limited || got.Sources[overview.SourceReleases] != limited || got.Sources[overview.SourceTags] != limited {
		t.Fatalf("%+v", got.Sources)
	}
	if !reflect.DeepEqual(rd.calls, []string{"repo"}) {
		t.Fatalf("requests after the rate limit: %v", rd.calls)
	}
	mid := &fakeReleaseReader{info: writeAccessInfo(), relsErr: relWatchErr(hosting.CodeRateLimited)}
	got = ReadReleases(context.Background(), mid, "o/r", nil)
	if got.Sources[overview.SourceRepo] != relOK || got.Sources[overview.SourceReleases] != limited || got.Sources[overview.SourceTags] != limited || !reflect.DeepEqual(mid.calls, []string{"repo", "releases"}) {
		t.Fatalf("%+v %v", got.Sources, mid.calls)
	}
}

func TestReadReleasesDoneContextSendsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rd := &fakeReleaseReader{info: writeAccessInfo()}
	got := ReadReleases(ctx, rd, "o/r", nil)
	skipped := relState(overview.OutcomeSkipped, overview.ReasonDeadline, "")
	if got.Sources[overview.SourceRepo] != skipped || got.Sources[overview.SourceReleases] != skipped || got.Sources[overview.SourceTags] != skipped || len(rd.calls) != 0 {
		t.Fatalf("%+v %v", got.Sources, rd.calls)
	}
	if got.Visibility != overview.Unknown || len(got.Drafts) != 0 {
		t.Fatalf("%+v", got)
	}
}

// The deadline passes while the releases are read: that read was cancelled, so it is a skip and
// not an error; what finished before is kept and what was not started is not sent.
func TestReadReleasesDeadlineDuringARead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rd := &fakeReleaseReader{info: writeAccessInfo(), relsErr: relWatchErr(hosting.CodeNetwork)}
	rd.onCall = func(name string) {
		if name == "releases" {
			cancel()
		}
	}
	got := ReadReleases(ctx, rd, "o/r", nil)
	skipped := relState(overview.OutcomeSkipped, overview.ReasonDeadline, "")
	if got.Sources[overview.SourceRepo] != relOK || got.Sources[overview.SourceReleases] != skipped || got.Sources[overview.SourceTags] != skipped {
		t.Fatalf("%+v", got.Sources)
	}
	if got.DefaultBranch != "main" || got.Visibility != overview.WriteAccess {
		t.Fatalf("the repository read is kept: %+v", got)
	}
	if !reflect.DeepEqual(rd.calls, []string{"repo", "releases"}) {
		t.Fatalf("%v", rd.calls)
	}
}

func TestReadReleasesInvalidRepoSendsNothing(t *testing.T) {
	for _, slug := range []string{"", "o", "o/r/x", "o/../r", "o/r;rm", "-/r/"} {
		rd := &fakeReleaseReader{info: writeAccessInfo()}
		got := ReadReleases(context.Background(), rd, slug, nil)
		bad := relState(overview.OutcomeSkipped, overview.ReasonInvalidRepo, "")
		if len(rd.calls) != 0 || got.Sources[overview.SourceRepo] != bad || got.Sources[overview.SourceReleases] != bad || got.Sources[overview.SourceTags] != bad {
			t.Errorf("%q: %+v %v", slug, got.Sources, rd.calls)
		}
	}
}

func TestApplyKeepsTheStatesOfOtherSources(t *testing.T) {
	into := &overview.Remote{Sources: map[overview.Source]overview.SourceState{overview.SourcePulls: relOK}}
	ReadReleases(context.Background(), &fakeReleaseReader{info: writeAccessInfo(), rels: []hosting.StatusRelease{{Draft: true, Tag: "v1"}}}, "o/r", nil).Apply(into)
	if into.Sources[overview.SourcePulls] != relOK || into.Sources[overview.SourceReleases] != relOK || into.Visibility != overview.WriteAccess || len(into.Drafts) != 1 {
		t.Fatalf("%+v", into)
	}
	var empty overview.Remote
	ReadReleases(context.Background(), &fakeReleaseReader{info: writeAccessInfo()}, "o/r", nil).Apply(&empty)
	if empty.Sources[overview.SourceRepo] != relOK {
		t.Fatalf("%+v", empty)
	}
}

// The real status reader on an injected transport and a fixture gh: no credentials of the developer.
func TestReadReleasesWithTheStatusReader(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "gh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ];then echo 'gh version 2.102.0';exit 0;fi\necho fixturetoken\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_PATH", script)
	bodies := map[string]string{
		"/repos/o/r": `{"default_branch":"main","html_url":"https://github.com/o/r","permissions":{"admin":false,"maintain":false,"push":true,"triage":true,"pull":true}}`,
		"/repos/o/r/releases?per_page=100&page=1": `[{"draft":true,"name":"n","tag_name":"v1.1.0","created_at":"2026-10-01T08:00:00Z","html_url":"https://github.com/o/r/releases/1","assets":[{"id":1}]},{"draft":false,"tag_name":"v1.0.0","html_url":"u","assets":[]}]`,
		"/repos/o/r/tags?per_page=100&page=1":     `[{"name":"v1.2.0","commit":{"sha":"` + relSHA("c") + `"}},{"name":"v1.0.0","commit":{"sha":"` + relSHA("a") + `"}}]`,
	}
	var seen []string
	rt := relRoundTrip(func(req *http.Request) (*http.Response, error) {
		key := req.URL.Path
		if req.URL.RawQuery != "" {
			key += "?" + req.URL.RawQuery
		}
		seen = append(seen, req.Method+" "+key)
		body, found := bodies[key]
		status := 200
		if !found {
			status, body = 404, `{}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: relBody(body)}, nil
	})
	rd, cleanup, err := hosting.NewStatusReaderWithTransport(context.Background(), target, "github.com", rt)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	got := ReadReleases(context.Background(), rd, "o/r", nil)
	if got.Visibility != overview.WriteAccess || len(got.Drafts) != 1 || got.Drafts[0].Assets != 1 || len(got.Tags) != 1 || !got.Tags[0].Unreleased || got.Tags[0].Tag != "v1.2.0" {
		t.Fatalf("%+v", got)
	}
	want := []string{"GET /repos/o/r", "GET /repos/o/r/releases?per_page=100&page=1", "GET /repos/o/r/tags?per_page=100&page=1"}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("%v", seen)
	}
}

type relRoundTrip func(*http.Request) (*http.Response, error)

func (f relRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func relBody(s string) io.ReadCloser { return io.NopCloser(strings.NewReader(s)) }

func TestApplyCarriesWhatTheSourceComputed(t *testing.T) {
	var rels []hosting.StatusRelease
	for i := 0; i < MaxDrafts+1; i++ {
		rels = append(rels, hosting.StatusRelease{Draft: true, Tag: fmt.Sprintf("v1.0.%d", i)})
	}
	var into overview.Remote
	ReadReleases(context.Background(), &fakeReleaseReader{info: writeAccessInfo(), rels: rels}, "o/r", nil).Apply(&into)
	if !into.DraftsTruncated || len(into.Drafts) != MaxDrafts || into.DefaultBranch != "main" || into.HTMLURL != "https://github.com/o/r" {
		t.Fatalf("%+v", into)
	}
	var whole overview.Remote
	ReadReleases(context.Background(), &fakeReleaseReader{info: writeAccessInfo(), rels: rels[:3]}, "o/r", nil).Apply(&whole)
	if whole.DraftsTruncated {
		t.Fatal("not truncated")
	}
}

// A draft without a usable tag or page yields no suggested command and no URL, through the real
// model: the item may exist (the owner should know about it) but never offers `gh release edit ”`.
func TestDraftWithoutTagOrURLYieldsNoCommand(t *testing.T) {
	rd := &fakeReleaseReader{info: writeAccessInfo(), rels: []hosting.StatusRelease{
		{Draft: true, Name: "no tag", URL: "https://github.com/o/r/releases/1"},
		{Draft: true, Tag: "v1.1.0"},
		{Draft: true, Tag: "-rf", URL: "http://example.com/x"},
		{Draft: true, Tag: "v1.2.0 x", URL: "https://github.com/o/r/releases/2"},
		{Draft: true, Tag: "v1.3.0", URL: "https://github.com/o/r/releases/3"},
	}}
	into := &overview.Remote{}
	ReadReleases(context.Background(), rd, "o/r", nil).Apply(into)
	got := overview.Build(overview.Input{Root: "/w", Repos: []overview.Repo{{Local: stwsync.RepoStatus{Name: "r", Path: "r", Kind: "root", State: "ok", Reasons: []string{}}, Slug: "o/r", Remote: into}}})
	want := map[string][2]string{ // tag: command, URL
		"":         {"", "https://github.com/o/r/releases/1"},
		"v1.1.0":   {"gh release edit v1.1.0 -R o/r --draft=false", ""},
		"-rf":      {"", ""},
		"v1.2.0 x": {"", "https://github.com/o/r/releases/2"},
		"v1.3.0":   {"gh release edit v1.3.0 -R o/r --draft=false", "https://github.com/o/r/releases/3"},
	}
	seen := 0
	for _, it := range got.Items {
		if it.Category != overview.ReleaseDraft {
			continue
		}
		seen++
		w, ok := want[it.Subject.Tag]
		if !ok || it.Command != w[0] || it.URL != w[1] || (it.Command == "") != (it.Effect == "") {
			t.Errorf("%q: command %q effect %q url %q", it.Subject.Tag, it.Command, it.Effect, it.URL)
		}
	}
	if seen != len(want) {
		t.Fatalf("%d draft items", seen)
	}
}
