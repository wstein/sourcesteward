// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/stwsync"
)

// withSources is a repository whose remote sources ended as given.
func withSources(name, slug string, states map[Source]SourceState) Repo {
	return Repo{Local: repoOK(name, nil), Slug: slug, Remote: &Remote{Sources: states}}
}

func all(st SourceState) map[Source]SourceState {
	m := map[Source]SourceState{}
	for _, s := range Sources {
		m[s] = st
	}
	return m
}

func TestNotices(t *testing.T) {
	reset := time.Date(2026, 10, 22, 14, 5, 0, 0, time.FixedZone("x", 3600))
	cases := []struct {
		name    string
		in      Input
		want    []Notice
		degrade bool
	}{
		{"no token", Input{Repos: []Repo{
			withSources("b", "w/b", all(SourceState{Outcome: OutcomeSkipped, Reason: ReasonNoToken})),
			withSources("a", "w/a", all(SourceState{Outcome: OutcomeSkipped, Reason: ReasonNoToken})),
		}}, []Notice{{Code: "no-token", Source: "remote", Repos: []string{"w/a", "w/b"},
			Message: "no GitHub token (run: gh auth login): pull requests, releases and tags were not read"}}, true},
		{"deadline", Input{Deadline: 90 * time.Second, Repos: []Repo{
			withSources("a", "w/a", map[Source]SourceState{SourcePulls: {Outcome: OutcomeSkipped, Reason: ReasonDeadline}, SourceTags: {Outcome: OutcomeSkipped, Reason: ReasonDeadline}, SourceRepo: {Outcome: OutcomeOK}}),
			withSources("b", "w/b", map[Source]SourceState{SourcePulls: {Outcome: OutcomeSkipped, Reason: ReasonDeadline}}),
		}}, []Notice{{Code: "deadline", Source: "remote", Repos: []string{"w/a", "w/b"},
			Message: "deadline of 90 s reached: 3 sources of 2 repositories not read; raise --timeout"}}, true},
		{"workflow not found", Input{Repos: []Repo{
			withSources("a", "w/a", map[Source]SourceState{SourceCIRelease: {Outcome: OutcomeSkipped, Reason: ReasonNotFound}}),
		}}, []Notice{{Code: "not-found", Source: "ci-release", Repos: []string{"w/a"},
			Message: "release workflow not found in 1 repositories: runs not read"}}, true},
		{"not github", Input{Repos: []Repo{
			withSources("local-only", "", all(SourceState{Outcome: OutcomeSkipped, Reason: ReasonNotGitHub})),
			withSources("other", "", all(SourceState{Outcome: OutcomeSkipped, Reason: ReasonOtherHost})),
			withSources("bad", "", all(SourceState{Outcome: OutcomeSkipped, Reason: ReasonInvalidRepo})),
			withSources("nl", "", all(SourceState{Outcome: OutcomeSkipped, Reason: ReasonNotReadLocally})),
		}}, []Notice{
			{Code: "invalid-repo", Source: "remote", Repos: []string{"bad"}, Message: "invalid repository name: remote sources of 1 repositories were not read"},
			{Code: "not-github-origin", Source: "remote", Repos: []string{"local-only"}, Message: "origin is not a GitHub repository: remote sources of 1 repositories were not read"},
			{Code: "not-read-locally", Source: "remote", Repos: []string{"nl"}, Message: "repository was not read locally: remote sources of 1 repositories were not read"},
			{Code: "other-host", Source: "remote", Repos: []string{"other"}, Message: "origin is on another host: remote sources of 1 repositories were not read"},
		}, true},
		{"network", Input{Repos: []Repo{
			withSources("a", "w/a", map[Source]SourceState{SourcePulls: {Outcome: OutcomeError, Code: CodeNetwork}}),
			withSources("b", "w/b", map[Source]SourceState{SourcePulls: {Outcome: OutcomeError, Code: CodeNetwork}, SourceTags: {Outcome: OutcomeError, Code: CodeNetwork}}),
		}}, []Notice{
			{Code: "network", Source: "pulls", Repos: []string{"w/a", "w/b"}, Message: "network error: pull requests of 2 repositories not read"},
			{Code: "network", Source: "tags", Repos: []string{"w/b"}, Message: "network error: tags of 1 repositories not read"},
		}, true},
		{"rate limit with reset", Input{RateLimitReset: reset, Repos: []Repo{
			withSources("a", "w/a", map[Source]SourceState{SourcePulls: {Outcome: OutcomeError, Code: CodeRateLimited}, SourceTags: {Outcome: OutcomeError, Code: CodeRateLimited}}),
			withSources("b", "w/b", map[Source]SourceState{SourceCIMain: {Outcome: OutcomeError, Code: CodeRateLimited}}),
		}}, []Notice{{Code: "rate-limited", Source: "remote", Repos: []string{"w/a", "w/b"}, Message: "rate limit reached at 13:05 UTC: 2 repositories not read"}}, true},
		{"rate limit without reset", Input{Repos: []Repo{
			withSources("a", "w/a", map[Source]SourceState{SourcePulls: {Outcome: OutcomeError, Code: CodeRateLimited}}),
		}}, []Notice{{Code: "rate-limited", Source: "remote", Repos: []string{"w/a"}, Message: "rate limit reached: 1 repositories not read"}}, true},
		{"other error code, hostile", Input{Repos: []Repo{
			withSources("a", "w/a", map[Source]SourceState{SourceReleases: {Outcome: OutcomeError, Code: "x\ny\x1b[31m"}}),
		}}, nil, true},
		{"drafts not visible", Input{Repos: []Repo{{Local: repoOK("a", nil), Slug: "w/a", Remote: &Remote{Visibility: NoWriteAccess}}}},
			[]Notice{{Code: "drafts-not-visible", Source: "releases", Repos: []string{"w/a"}, Message: "release drafts of w/a not visible (token has no write access)"}}, false},
		{"drafts unknown", Input{Repos: []Repo{{Local: repoOK("a", nil), Slug: "w/a", Remote: &Remote{Visibility: Unknown}}}},
			[]Notice{{Code: "drafts-unknown", Source: "releases", Repos: []string{"w/a"}, Message: "release drafts of w/a unknown (write access not determined)"}}, false},
		{"all fine", Input{Repos: []Repo{withSources("a", "w/a", all(SourceState{Outcome: OutcomeOK})), {Local: repoOK("b", nil)}}}, nil, false},
		{"offline skip is silent", Input{Repos: []Repo{withSources("a", "w/a", all(SourceState{Outcome: OutcomeSkipped, Reason: ReasonOffline}))}}, nil, true},
		{"not-attempted skip is silent", Input{Repos: []Repo{withSources("a", "w/a", all(SourceState{Outcome: OutcomeSkipped, Reason: ReasonNotAttempted}))}}, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ov := Build(c.in)
			if c.name == "other error code, hostile" {
				if len(ov.Notices) != 1 || strings.ContainsAny(ov.Notices[0].Message, "\n\x1b") || !strings.Contains(ov.Notices[0].Message, "releases of 1 repositories not read") {
					t.Fatalf("%+v", ov.Notices)
				}
				return
			}
			if !reflect.DeepEqual(ov.Notices, c.want) {
				t.Fatalf("notices:\n got %+v\nwant %+v", ov.Notices, c.want)
			}
			if ov.Summary.Degraded != c.degrade || ov.Summary.Notices != len(c.want) && c.name != "other error code, hostile" {
				t.Errorf("summary %+v", ov.Summary)
			}
		})
	}
}

func TestNoticeRepoListIsCappedButCounted(t *testing.T) {
	var repos []Repo
	for i := 0; i < 300; i++ {
		name := "w/r" + string(rune('a'+i/26/26%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i%26))
		repos = append(repos, withSources(name, name, all(SourceState{Outcome: OutcomeSkipped, Reason: ReasonNoToken})))
	}
	n := Build(Input{Repos: repos}).Notices[0]
	if len(n.Repos) != MaxNoticeRepos || !sortedStrings(n.Repos) {
		t.Fatalf("%d repos", len(n.Repos))
	}
}

func sortedStrings(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}

func TestNoticesAreInvariantUnderShuffles(t *testing.T) {
	mkInput := func() Input {
		return Input{Deadline: time.Minute, Repos: []Repo{
			withSources("a", "w/a", map[Source]SourceState{SourcePulls: {Outcome: OutcomeError, Code: CodeNetwork}, SourceTags: {Outcome: OutcomeSkipped, Reason: ReasonDeadline}}),
			withSources("b", "w/b", all(SourceState{Outcome: OutcomeSkipped, Reason: ReasonNoToken})),
			withSources("c", "w/c", map[Source]SourceState{SourceCIRelease: {Outcome: OutcomeSkipped, Reason: ReasonNotFound}}),
			{Local: repoOK("d", nil), Slug: "w/d", Remote: &Remote{Visibility: Unknown}},
			withSources("e", "w/e", map[Source]SourceState{SourceReleases: {Outcome: OutcomeError, Code: "http-5xx"}}),
		}}
	}
	want := Build(mkInput()).Notices
	for seed := int64(0); seed < 50; seed++ {
		in := mkInput()
		rand.New(rand.NewSource(seed)).Shuffle(len(in.Repos), func(i, j int) { in.Repos[i], in.Repos[j] = in.Repos[j], in.Repos[i] })
		if got := Build(in).Notices; !reflect.DeepEqual(got, want) {
			t.Fatalf("seed %d:\n got %+v\nwant %+v", seed, got, want)
		}
	}
}

func TestSummary(t *testing.T) {
	in := richInput()
	ov := Build(in)
	if !reflect.DeepEqual(ov.Items, collect(in)) {
		t.Fatal("Build must return the sorted items")
	}
	want := Counts{}
	for _, it := range ov.Items {
		switch it.Priority {
		case Red:
			want.Red++
		case Yellow:
			want.Yellow++
		case Green:
			want.Green++
		case Blue:
			want.Blue++
		case Violet:
			want.Violet++
		}
	}
	s := ov.Summary
	if s.Priorities != want || s.Priorities == (Counts{}) {
		t.Errorf("priorities %+v, want %+v", s.Priorities, want)
	}
	if s.Repos != 3 || s.Errors != 1 || s.Notices != len(ov.Notices) {
		t.Errorf("summary %+v", s)
	}
	if !s.Failed || !s.Degraded {
		t.Errorf("a failed tags read must set failed and degraded: %+v", s)
	}
}

func TestSummaryAttentionHasTheSpec033Meaning(t *testing.T) {
	dirty := repoOK("d", func(r *stwsync.RepoStatus) {
		r.Attention = true
		r.Reasons = []string{"untracked"}
		r.Changes.Untracked = 1
	})
	ov := Build(Input{Repos: []Repo{{Local: dirty}, {Local: repoOK("c", nil)}}})
	if ov.Summary.Attention != 1 || ov.Summary.Repos != 2 || ov.Summary.Errors != 0 || ov.Summary.Failed || ov.Summary.Degraded {
		t.Fatalf("%+v", ov.Summary)
	}
}

func TestBuildOfNothing(t *testing.T) {
	ov := Build(Input{})
	if len(ov.Items) != 0 || len(ov.Notices) != 0 || ov.Summary != (Summary{}) {
		t.Fatalf("%+v", ov)
	}
}

func TestDeadlineSkipsAreNoItemsAndNoFailure(t *testing.T) {
	ov := Build(Input{Deadline: 90 * time.Second, Repos: []Repo{withSources("a", "w/a", all(SourceState{Outcome: OutcomeSkipped, Reason: ReasonDeadline}))}})
	if len(ov.Items) != 0 || ov.Summary.Failed || !ov.Summary.Degraded {
		t.Fatalf("%+v", ov)
	}
}
