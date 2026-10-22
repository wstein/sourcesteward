// SPDX-License-Identifier: EUPL-1.2

package statusremote

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
)

func TestReleasesAreReadPerRepositoryInOrderAndReachTheOverviewInputs(t *testing.T) {
	w := newWorld()
	w.addRepo("o/a", true)
	w.rels["o/a"] = append(w.rels["o/a"], hosting.StatusRelease{Draft: true, Name: "Next", Tag: "v1.2.0", URL: "https://github.com/o/a/releases/2", Assets: 3})
	w.addRepo("o/b", false)
	res := Collect(context.Background(), []Repository{repository("o/a"), repository("o/b")}, (&opener{client: w}).options())
	a := res.Repos[0]
	for _, src := range []overview.Source{overview.SourceRepo, overview.SourceReleases, overview.SourceTags} {
		if got := sourceState(t, a, src); got != okState {
			t.Errorf("%s: %+v", src, got)
		}
	}
	if a.Remote.DefaultBranch != "main" || a.Remote.HTMLURL != "https://github.com/o/a" || a.Remote.Visibility != overview.WriteAccess {
		t.Fatalf("%+v", a.Remote)
	}
	if len(a.Remote.Drafts) != 1 || a.Remote.Drafts[0].Tag != "v1.2.0" || a.Remote.Drafts[0].Assets != 3 {
		t.Fatalf("%+v", a.Remote.Drafts)
	}
	want := []overview.ReleaseTag{{Pattern: "v*.*.*", Tag: "v1.1.0", SHA: sha("2"), Latest: true, Unreleased: true}}
	if !reflect.DeepEqual(a.Remote.Tags, want) {
		t.Fatalf("%+v", a.Remote.Tags)
	}
	// one repository after the other inside a source: repo, releases, tags of one repository in this order
	calls := w.requests()
	for _, repo := range []string{"o/a", "o/b"} {
		var order []string
		for _, c := range calls {
			if strings.HasSuffix(c, " "+repo) && (strings.HasPrefix(c, "repo ") || strings.HasPrefix(c, "releases ") || strings.HasPrefix(c, "tags ")) {
				order = append(order, strings.Fields(c)[0])
			}
		}
		if fmt.Sprint(order) != "[repo releases tags]" {
			t.Errorf("%s: %v", repo, order)
		}
	}
}

// The release tag patterns come from the release contracts of the clone, not from a default.
func TestTheTagPatternsComeFromTheContractsOfTheClone(t *testing.T) {
	dir := t.TempDir()
	ciWriteContract(t, dir, "release.zlib.toml", ciContractText("zlib", "zlib-v{version}", ciProductPattern("zlib"), "z.yml"))
	w := newWorld()
	w.addRepo("o/a", false)
	w.tags["o/a"] = []hosting.StatusTag{{Name: "zlib-v1.0.0", SHA: sha("3")}, {Name: "v9.0.0", SHA: sha("4")}}
	w.rels["o/a"] = nil
	r := repository("o/a")
	r.Dir = dir
	res := Collect(context.Background(), []Repository{r}, (&opener{client: w}).options())
	got := res.Repos[0].Remote.Tags
	if len(got) != 1 || got[0].Pattern != "zlib-v*" || got[0].Tag != "zlib-v1.0.0" {
		t.Fatalf("%+v", got)
	}
}

// A failing source leaves the others of its repository and every other repository intact, and a source that
// did not end ok has no data (a failed repo read in particular leaves the draft visibility out, so the
// output states no "drafts unknown" on top of the read error).
func TestAFailingSourceHurtsOnlyItself(t *testing.T) {
	w := newWorld()
	for _, r := range []string{"o/a", "o/b", "o/c"} {
		w.addRepo(r, true)
	}
	w.errs["repo o/a"] = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "m"}
	w.errs["releases o/b"] = &hosting.WatchError{Code: hosting.CodePageTooLarge, Message: "m"}
	res := Collect(context.Background(), []Repository{repository("o/a"), repository("o/b"), repository("o/c")}, (&opener{client: w}).options())
	a, b, c := res.Repos[0], res.Repos[1], res.Repos[2]
	if sourceState(t, a, overview.SourceRepo) != failed(hosting.CodeHTTP5xx) || sourceState(t, a, overview.SourceReleases) != okState || sourceState(t, a, overview.SourceTags) != okState {
		t.Fatalf("%+v", a.Remote.Sources)
	}
	if a.Remote.DefaultBranch != "" || a.Remote.HTMLURL != "" || a.Remote.Visibility != "" || len(a.Remote.Tags) != 1 {
		t.Fatalf("data of a failed repo source: %+v", a.Remote)
	}
	if sourceState(t, b, overview.SourceReleases) != failed(hosting.CodePageTooLarge) || sourceState(t, b, overview.SourceTags) != okState || sourceState(t, b, overview.SourceRepo) != okState {
		t.Fatalf("%+v", b.Remote.Sources)
	}
	if b.Remote.Drafts != nil || b.Remote.DraftsTruncated || b.Remote.DefaultBranch != "main" {
		t.Fatalf("%+v", b.Remote)
	}
	for _, src := range []overview.Source{overview.SourceRepo, overview.SourceReleases, overview.SourceTags} {
		if got := sourceState(t, c, src); got != okState {
			t.Errorf("neighbour %s: %+v", src, got)
		}
	}
}

func TestAFailedTagsReadHasNoTags(t *testing.T) {
	w := newWorld()
	w.addRepo("o/a", true)
	w.errs["tags o/a"] = &hosting.WatchError{Code: hosting.CodeNetwork, Message: "m"}
	res := Collect(context.Background(), []Repository{repository("o/a")}, (&opener{client: w}).options())
	if sourceState(t, res.Repos[0], overview.SourceTags) != failed(hosting.CodeNetwork) || res.Repos[0].Remote.Tags != nil {
		t.Fatalf("%+v", res.Repos[0].Remote)
	}
}

func TestTheDeadlineSkipsTheReleaseSourcesThatWereNotFinished(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newWorld()
		w.addRepo("o/a", true)
		w.hook = func(ctx context.Context, call string) error {
			if strings.HasPrefix(call, "releases ") {
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		}
		res := Collect(context.Background(), []Repository{repository("o/a")}, (&opener{client: w}).options())
		a := res.Repos[0]
		if sourceState(t, a, overview.SourceRepo) != okState {
			t.Errorf("repo kept: %+v", a.Remote.Sources)
		}
		for _, src := range []overview.Source{overview.SourceReleases, overview.SourceTags} {
			if got := sourceState(t, a, src); got != skipped(overview.ReasonDeadline) {
				t.Errorf("%s: %+v", src, got)
			}
		}
		if a.Remote.DefaultBranch != "main" || a.Remote.Tags != nil {
			t.Fatalf("%+v", a.Remote)
		}
		if w.count("tags ") != 0 {
			t.Fatal("a request was sent after the deadline")
		}
	})
}

func TestAtMostFourRepositoriesAreInFlightInTheReleaseSources(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newWorld()
		var repos []Repository
		for i := 0; i < 10; i++ {
			slug := fmt.Sprintf("o/r%d", i)
			w.addRepo(slug, false)
			repos = append(repos, repository(slug))
		}
		held, release := atomic.Int32{}, make(chan struct{})
		w.hook = func(_ context.Context, call string) error {
			if strings.HasPrefix(call, "repo ") {
				held.Add(1)
				<-release
			}
			return nil
		}
		done := make(chan Result)
		go func() { done <- Collect(context.Background(), repos, (&opener{client: w}).options()) }()
		synctest.Wait()
		if got := held.Load(); got != 4 {
			t.Fatalf("%d repositories in flight, want exactly 4", got)
		}
		close(release)
		res := <-done
		for _, r := range res.Repos {
			if got := sourceState(t, r, overview.SourceTags); got != okState {
				t.Errorf("%s: %+v", r.Name, got)
			}
		}
	})
}
