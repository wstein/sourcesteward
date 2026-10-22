// SPDX-License-Identifier: EUPL-1.2

package statuscmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/statusremote"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

var (
	okSource   = overview.SourceState{Outcome: overview.OutcomeOK}
	fixtureSHA = strings.Repeat("3f2a9c1", 6)[:40]
)

func allSources(st overview.SourceState) map[overview.Source]overview.SourceState {
	m := map[overview.Source]overview.SourceState{}
	for _, s := range overview.Sources {
		m[s] = st
	}
	return m
}

// fixtureReport is a workspace of four repositories: sourcesteward (everything read, red main, a draft,
// a failed release run), crewbook (drafts not visible, a PR with conflicts), workharbor (two bot PRs,
// the CI of main cut by the deadline) and one that cannot be read locally.
func fixtureReport() Report {
	local := stwsync.StatusResult{
		SchemaVersion: 1, Command: "status", Root: ptr("/ws"), Options: stwsync.StatusEcho{Worktrees: true},
		Repos: []stwsync.RepoStatus{
			{Name: ".", Path: "/ws", Kind: "root", State: "ok", Branch: ptr("main"), Upstream: ptr("origin/main"), Ahead: ptr(0), Behind: ptr(2),
				Changes: stwsync.Changes{Unstaged: 2}, Stash: 1, Attention: true, Reasons: []string{"unstaged", "behind"}},
			{Name: "crewbook", Path: "/ws/crewbook", Kind: "submodule", State: "ok", Branch: ptr("main"), Upstream: ptr("origin/main"), Ahead: ptr(0), Behind: ptr(0), Reasons: []string{}},
			{Name: "workharbor", Path: "/ws/workharbor", Kind: "submodule", State: "ok", Branch: ptr("main"), Upstream: ptr("origin/main"), Ahead: ptr(0), Behind: ptr(0), Reasons: []string{}},
			{Name: "gone", Path: "/ws/gone", Kind: "submodule", State: "error", Reasons: []string{}, Error: &stwsync.Failure{Code: "status-read-failed", Message: "cannot read the repository status"}},
		},
		Summary: stwsync.StatusSummary{Total: 4, Attention: 1, Errors: 1},
	}
	for i := range local.Repos { // as stwsync reports them: no worktree list is null, a submodule has a gitlink
		local.Repos[i].Worktrees.List = []stwsync.WorktreeInfo{}
		if local.Repos[i].Kind == "submodule" {
			local.Repos[i].Gitlink = &stwsync.Gitlink{Recorded: ptr(fixtureSHA), CheckedOut: ptr(fixtureSHA), State: "ok"}
		}
	}
	failedRun := overview.Run{Workflow: "release.yml", Conclusion: "failure", Status: "completed", URL: "https://github.com/wstein/sourcesteward/actions/runs/7"}
	greenRun := overview.Run{Workflow: "release.yml", Conclusion: "success", Status: "completed", URL: "https://github.com/wstein/sourcesteward/actions/runs/5"}
	behind := true
	steward := statusremote.RepoResult{
		Name: ".", Slug: "wstein/sourcesteward",
		Remote: &overview.Remote{
			Sources: allSources(okSource), DefaultBranch: "main", HTMLURL: "https://github.com/wstein/sourcesteward", Visibility: overview.WriteAccess,
			Pulls: []overview.PR{
				{Number: 57, Title: "build(deps): bump cobra", URL: "https://github.com/wstein/sourcesteward/pull/57", Author: "alice", Group: overview.GroupReady},
				{Number: 58, Title: "draft work", URL: "https://github.com/wstein/sourcesteward/pull/58", Author: "alice", Draft: true, Group: overview.GroupDraft},
				{Number: 59, Title: "Fix the tests", URL: "https://github.com/wstein/sourcesteward/pull/59", Author: "bob", Group: overview.GroupBlocked, Reason: "checks-red"},
			},
			Drafts: []overview.Draft{{Name: "Release one", Tag: "v1.0.0", URL: "https://github.com/wstein/sourcesteward/releases/tag/untagged-1", Assets: 4, CreatedAt: time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)}},
			Tags: []overview.ReleaseTag{
				{Pattern: "v*.*.*", Tag: "v1.1.0", SHA: strings.Repeat("b", 40), Latest: true, Unreleased: true},
				{Pattern: "v*.*.*", Tag: "v1.0.0", SHA: strings.Repeat("a", 40), Latest: false},
			},
			Main: &overview.Main{Branch: "main", Head: fixtureSHA, Class: "red", BehindOrigin: &behind},
		},
		Pulls: &statusremote.Pulls{State: okSource, Open: ptr(3), Counts: pulls.Summary{Total: 3, Ready: 1, Draft: 1, Blocked: 1},
			PRs: []overview.PR{
				{Number: 57, Group: overview.GroupReady}, {Number: 58, Draft: true, Group: overview.GroupDraft}, {Number: 59, Group: overview.GroupBlocked, Reason: "checks-red"},
			}},
		CIRelease: &statusremote.CIRelease{Tags: []statusremote.CIReleaseTag{
			{Pattern: "v*.*.*", Product: "sourcesteward", Tag: "v1.1.0", SHA: strings.Repeat("b", 40), Latest: true, Unreleased: true, Workflow: "release.yml", Runs: []overview.Run{failedRun}},
			{Pattern: "v*.*.*", Product: "sourcesteward", Tag: "v1.0.0", SHA: strings.Repeat("a", 40), Workflow: "release.yml", Runs: []overview.Run{greenRun}},
		}},
	}
	steward.Remote.Tags = append(steward.Remote.Tags, steward.CIRelease.ReleaseTags()...)
	crewbook := statusremote.RepoResult{
		Name: "crewbook", Slug: "wstein/crewbook",
		Remote: &overview.Remote{
			Sources: allSources(okSource), DefaultBranch: "main", HTMLURL: "https://github.com/wstein/crewbook", Visibility: overview.NoWriteAccess,
			Pulls: []overview.PR{{Number: 12, Title: "Update docs", URL: "https://github.com/wstein/crewbook/pull/12", Author: "carol", Group: overview.GroupConflict}},
			Main:  &overview.Main{Branch: "main", Head: strings.Repeat("c", 40), Class: "green"},
		},
		Pulls:     &statusremote.Pulls{State: okSource, Open: ptr(1), Counts: pulls.Summary{Total: 1, Conflict: 1}, PRs: []overview.PR{{Number: 12, Group: overview.GroupConflict}}},
		CIRelease: &statusremote.CIRelease{},
	}
	deadline := overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline}
	harborSources := allSources(okSource)
	harborSources[overview.SourceCIMain], harborSources[overview.SourceCIRelease] = deadline, deadline
	harbor := statusremote.RepoResult{
		Name: "workharbor", Slug: "wstein/workharbor",
		Remote: &overview.Remote{
			Sources: harborSources, DefaultBranch: "main", HTMLURL: "https://github.com/wstein/workharbor", Visibility: overview.WriteAccess,
			Pulls: []overview.PR{
				{Number: 3, Title: "bump a", URL: "https://github.com/wstein/workharbor/pull/3", Author: "dependabot[bot]", Bot: true, Group: overview.GroupReady},
				{Number: 4, Title: "bump b", URL: "https://github.com/wstein/workharbor/pull/4", Author: "dependabot[bot]", Bot: true, Group: overview.GroupChecks},
			},
		},
		Pulls: &statusremote.Pulls{State: okSource, Open: ptr(2), Counts: pulls.Summary{Total: 2, Ready: 1, Checks: 1, Bots: 2}, PRs: []overview.PR{{Number: 3, Bot: true, Group: overview.GroupReady}, {Number: 4, Bot: true, Group: overview.GroupChecks}}},
	}
	gone := statusremote.RepoResult{Name: "gone", Remote: &overview.Remote{Sources: allSources(overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonNotReadLocally})}}
	res := statusremote.Result{Repos: []statusremote.RepoResult{steward, crewbook, harbor, gone}, Timeout: 90 * time.Second}
	return Report{Time: time.Date(2026, 10, 22, 9, 30, 0, 0, time.UTC), Local: local, Remote: res, Overview: overview.Build(res.Input("/ws", local.Repos))}
}

func marshal(t *testing.T, d Document) []byte {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	if err := enc.Encode(d); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestDocumentMatchesTheGolden(t *testing.T) {
	got := marshal(t, fixtureReport().Document())
	path := filepath.Join("testdata", "document.json")
	if os.Getenv("STW_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the document differs from testdata/document.json (STW_UPDATE_GOLDEN=1 rewrites it after review)\n%s", got)
	}
}

func decode(t *testing.T, d Document) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(marshal(t, d), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func requireKeys(t *testing.T, what string, m map[string]any, want ...string) {
	t.Helper()
	if len(m) != len(want) {
		t.Errorf("%s has the keys %v, want %v", what, keys(m), want)
	}
	for _, k := range want {
		if _, ok := m[k]; !ok {
			t.Errorf("%s lacks the key %q (keys are never omitted)", what, k)
		}
	}
}

func TestDocumentNeverOmitsAKey(t *testing.T) {
	m := decode(t, fixtureReport().Document())
	requireKeys(t, "document", m, "schema_version", "command", "time", "host", "options", "root", "note", "actions", "repos", "sources", "notices", "summary", "error")
	if m["schema_version"] != float64(2) || m["command"] != "status" || m["host"] != "github.com" || m["time"] != "2026-10-22T09:30:00Z" {
		t.Fatalf("%v", m)
	}
	requireKeys(t, "options", m["options"].(map[string]any), "fetch", "worktrees", "icons")
	if m["options"].(map[string]any)["icons"] != nil || m["note"] != nil || m["error"] != nil {
		t.Fatal("icons, note and error are null")
	}
	for _, a := range m["actions"].([]any) {
		item := a.(map[string]any)
		requireKeys(t, "action", item, "id", "priority", "category", "repo", "path", "title", "url", "subject", "evidence", "command", "effect", "hint")
		requireKeys(t, "subject", item["subject"].(map[string]any), "kind", "number", "tag", "sha", "name")
		if (item["command"] == nil) != (item["effect"] == nil) {
			t.Errorf("%v: effect is null exactly when command is", item["id"])
		}
		if u, ok := item["url"].(string); ok && !strings.HasPrefix(u, "https://") {
			t.Errorf("%v: %s is not https", item["id"], u)
		}
	}
	for _, r := range m["repos"].([]any) {
		repo := r.(map[string]any)
		requireKeys(t, "repo", repo, "name", "path", "kind", "state", "branch", "detached", "head", "upstream", "upstream_gone", "ahead", "behind", "changes", "operation",
			"stash", "branches", "worktrees", "gitlink", "fetch_error", "attention", "reasons", "error", "remote")
		remote := repo["remote"].(map[string]any)
		requireKeys(t, "remote", remote, "repo", "state", "reason", "sources")
		sources := remote["sources"].(map[string]any)
		requireKeys(t, "remote.sources", sources, "repo", "pulls", "releases", "tags", "ci_main", "ci_release")
		for name, s := range sources {
			requireKeys(t, name, s.(map[string]any), "state", "error", "reason", "data")
		}
	}
	for _, s := range m["sources"].([]any) {
		requireKeys(t, "source", s.(map[string]any), "id", "state", "read", "degraded", "skipped")
	}
	for _, n := range m["notices"].([]any) {
		requireKeys(t, "notice", n.(map[string]any), "code", "source", "repos", "message")
	}
	requireKeys(t, "summary", m["summary"].(map[string]any), "repos", "attention", "errors", "priorities", "degraded", "failed", "notices")
	requireKeys(t, "priorities", m["summary"].(map[string]any)["priorities"].(map[string]any), "red", "yellow", "green", "blue", "violet")
}

func TestDocumentDataOfEachSource(t *testing.T) {
	m := decode(t, fixtureReport().Document())
	repos := m["repos"].([]any)
	steward := repos[0].(map[string]any)["remote"].(map[string]any)
	if steward["repo"] != "wstein/sourcesteward" || steward["state"] != "ok" || steward["reason"] != nil {
		t.Fatalf("%v", steward)
	}
	src := steward["sources"].(map[string]any)
	requireKeys(t, "repo data", src["repo"].(map[string]any)["data"].(map[string]any), "default_branch", "html_url", "drafts_visibility")
	PullsData := src["pulls"].(map[string]any)["data"].(map[string]any)
	requireKeys(t, "pulls data", PullsData, "open", "ready", "checks", "conflict", "draft", "blocked", "red", "bots", "not_read")
	if PullsData["open"] != float64(3) || PullsData["red"] != float64(1) || PullsData["blocked"] != float64(1) {
		t.Errorf("%v", PullsData)
	}
	releases := src["releases"].(map[string]any)["data"].(map[string]any)
	requireKeys(t, "releases data", releases, "drafts", "truncated")
	requireKeys(t, "draft", releases["drafts"].([]any)[0].(map[string]any), "name", "tag", "created_at", "assets", "url")
	tags := src["tags"].(map[string]any)["data"].(map[string]any)
	requireKeys(t, "tags data", tags, "latest", "unreleased")
	unreleased := tags["unreleased"].([]any)
	if len(unreleased) != 1 || len(tags["latest"].([]any)) != 1 {
		t.Fatalf("one entry per (pattern, tag), not one per read: %v", tags)
	}
	requireKeys(t, "unreleased tag", unreleased[0].(map[string]any), "pattern", "tag", "sha", "run")
	run := unreleased[0].(map[string]any)["run"].(map[string]any)
	requireKeys(t, "run", run, "workflow", "conclusion", "status", "url")
	if run["conclusion"] != "failure" {
		t.Errorf("the failed run of the unreleased tag: %v", run)
	}
	requireKeys(t, "main data", src["ci_main"].(map[string]any)["data"].(map[string]any), "branch", "head", "class", "behind_origin")
	ciTags := src["ci_release"].(map[string]any)["data"].(map[string]any)["tags"].([]any)
	requireKeys(t, "release tag", ciTags[0].(map[string]any), "pattern", "product", "tag", "sha", "latest", "unreleased", "workflow", "runs")

	harbor := repos[2].(map[string]any)["remote"].(map[string]any)
	if harbor["state"] != "degraded" || harbor["sources"].(map[string]any)["ci_main"].(map[string]any)["state"] != "skipped" || harbor["sources"].(map[string]any)["ci_main"].(map[string]any)["data"] != nil {
		t.Errorf("%v", harbor)
	}
	if ci := harbor["sources"].(map[string]any)["ci_main"].(map[string]any); ci["reason"] != "deadline" || ci["error"] != nil {
		t.Errorf("a skipped source says why: %v", ci)
	}
	if ok := src["pulls"].(map[string]any); ok["reason"] != nil || ok["error"] != nil {
		t.Errorf("an ok source has neither reason nor error: %v", ok)
	}
	gone := repos[3].(map[string]any)["remote"].(map[string]any)
	if gone["state"] != "skipped" || gone["reason"] != "not-read-locally" || gone["repo"] != nil {
		t.Errorf("%v", gone)
	}
}

func TestDocumentTagsWithoutTheReleaseRunsHaveNoRun(t *testing.T) {
	rep := fixtureReport()
	rr := rep.Remote.Repos[0]
	rr.CIRelease = nil
	rr.Remote = &overview.Remote{Sources: allSources(okSource), Tags: []overview.ReleaseTag{{Pattern: "v*.*.*", Tag: "v1.1.0", SHA: strings.Repeat("b", 40), Latest: true, Unreleased: true}}}
	data := tagsData(rr)
	if len(data.Latest) != 1 || len(data.Unreleased) != 1 || data.Unreleased[0].Run != nil {
		t.Fatalf("%+v", data)
	}
}

func TestDocumentOfAFailedLocalRead(t *testing.T) {
	rep := Report{Time: runTime, Local: stwsync.StatusResult{Repos: []stwsync.RepoStatus{}, Error: &stwsync.Failure{Code: "status-not-a-repository", Message: "no"}}}
	rep.Overview = overview.Build(overview.Input{})
	m := decode(t, rep.Document())
	if m["error"].(map[string]any)["code"] != "status-not-a-repository" || len(m["repos"].([]any)) != 0 || len(m["actions"].([]any)) != 0 || m["root"] != nil {
		t.Fatalf("%v", m)
	}
	for _, s := range m["sources"].([]any) {
		if s.(map[string]any)["state"] != "ok" {
			t.Errorf("a source that read nothing and failed nothing is ok: %v", s)
		}
	}
}

func TestDocumentKeepsTheV1RepositoryMembersUnchanged(t *testing.T) {
	rep := fixtureReport()
	var v1 []map[string]any
	if err := json.Unmarshal(mustJSON(t, rep.Local.Repos), &v1); err != nil {
		t.Fatal(err)
	}
	repos := decode(t, rep.Document())["repos"].([]any)
	for i, r := range repos {
		repo := r.(map[string]any)
		delete(repo, "remote")
		if !bytes.Equal(mustJSON(t, repo), mustJSON(t, v1[i])) {
			t.Errorf("repository %d changed:\n%s\n%s", i, mustJSON(t, repo), mustJSON(t, v1[i]))
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDocumentCutsEveryStringOnAUTF8Boundary(t *testing.T) {
	long := strings.Repeat("é", 2000) // 4000 bytes, every rune two bytes
	rep := fixtureReport()
	steward := rep.Remote.Repos[0]
	steward.Remote.Drafts = []overview.Draft{{Name: long, Tag: long, URL: "https://github.com/x/y/" + long, Assets: 1}}
	steward.Remote.Main.Branch = long
	rep.Local.Root = &long
	rep.Local.Note = &long
	rep.Overview.Items[0].Title = long
	rep.Overview.Notices = append(rep.Overview.Notices, overview.Notice{Code: long, Source: "remote", Repos: []string{"a/b"}, Message: long})
	d := rep.Document()
	check := func(what, s string) {
		t.Helper()
		if len(s) > maxString {
			t.Errorf("%s has %d bytes", what, len(s))
		}
		if !utf8Valid(s) {
			t.Errorf("%s is cut inside a rune", what)
		}
	}
	check("root", *d.Root)
	check("note", *d.Note)
	check("title", d.Actions[0].Title)
	if n := d.Notices[len(d.Notices)-1]; len(n.Message) > maxMessage || !utf8Valid(n.Message) || len(n.Code) > maxString {
		t.Errorf("notice: %d %d", len(n.Message), len(n.Code))
	}
	draft := steward.Remote.Drafts[0]
	got := releasesData(steward.Remote).Drafts[0]
	if len(*got.Name) > maxString || len(got.Tag) > maxString || got.URL != nil || !utf8Valid(got.Tag) || draft.Name != long {
		t.Errorf("draft: %d %d %v", len(*got.Name), len(got.Tag), got.URL)
	}
	if b := mainData(steward.Remote.Main).Branch; len(b) > maxString || !utf8Valid(b) {
		t.Errorf("branch: %d", len(b))
	}
	if cut("abc", 0) != "" || cut("abc", 3) != "abc" || cut("aé", 2) != "a" || cut("aé", 3) != "aé" {
		t.Error("cut")
	}
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "") == s }

func TestSourceStateFollowsTheCounts(t *testing.T) {
	for _, tc := range []struct {
		read, degraded, skipped int
		want                    string
	}{
		{0, 0, 0, "ok"}, {4, 0, 0, "ok"}, {0, 3, 0, "error"}, {0, 0, 3, "skipped"}, {2, 1, 0, "degraded"}, {2, 0, 1, "degraded"}, {0, 1, 1, "degraded"},
	} {
		if got := (DocSource{Read: tc.read, Degraded: tc.degraded, Skipped: tc.skipped}).withState().State; got != tc.want {
			t.Errorf("%+v: %s, want %s", tc, got, tc.want)
		}
	}
}

func TestRemoteStateFollowsTheSources(t *testing.T) {
	failed := overview.SourceState{Outcome: overview.OutcomeError, Code: "network"}
	skipped := overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonNoToken}
	crewbook := fixtureReport().Remote.Repos[1]
	if got := remote(crewbook); got.State != "ok" || got.Reason != nil || got.Repo == nil {
		t.Errorf("ok: %+v", got)
	}
	for name, tc := range map[string]struct {
		sources map[overview.Source]overview.SourceState
		state   string
		reason  any
	}{
		"skipped":  {allSources(skipped), "skipped", overview.ReasonNoToken},
		"error":    {allSources(failed), "error", nil},
		"degraded": {map[overview.Source]overview.SourceState{overview.SourceRepo: okSource, overview.SourcePulls: failed, overview.SourceReleases: skipped}, "degraded", nil},
	} {
		got := remote(statusremote.RepoResult{Remote: &overview.Remote{Sources: tc.sources}})
		var reason any
		if got.Reason != nil {
			reason = *got.Reason
		}
		if got.State != tc.state || reason != tc.reason {
			t.Errorf("%s: %s %v", name, got.State, reason)
		}
	}
}

func TestDocumentEscapesTerminalControlsAndNeverHoldsRawBytes(t *testing.T) {
	rep := fixtureReport()
	evil := "x\x1b[31m\u202e\nrm"
	rep.Remote.Repos[0].Remote.Pulls[0].Title = evil
	rep.Overview = overview.Build(rep.Remote.Input("/ws", rep.Local.Repos))
	out := marshal(t, rep.Document())
	for _, bad := range []string{"\x1b", "\u202e"} {
		if bytes.Contains(out, []byte(bad)) {
			t.Errorf("raw %q in the document", bad)
		}
	}
}

func TestDocumentCountsTheRepositoriesOfEverySource(t *testing.T) {
	rep := fixtureReport()
	rep.Local.Repos[1].State = "uninitialized"
	rep.Remote.Repos[1].Remote.Sources[overview.SourcePulls] = overview.SourceState{Outcome: overview.OutcomeError, Code: "network"}
	want := map[string]DocSource{
		"local":      {ID: "local", State: "degraded", Read: 2, Degraded: 1, Skipped: 1},
		"pulls":      {ID: "pulls", State: "degraded", Read: 2, Degraded: 1, Skipped: 1},
		"ci-main":    {ID: "ci-main", State: "degraded", Read: 2, Skipped: 2},
		"repo":       {ID: "repo", State: "degraded", Read: 3, Skipped: 1},
		"releases":   {ID: "releases", State: "degraded", Read: 3, Skipped: 1},
		"tags":       {ID: "tags", State: "degraded", Read: 3, Skipped: 1},
		"ci-release": {ID: "ci-release", State: "degraded", Read: 2, Skipped: 2},
	}
	got := rep.Document().Sources
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for _, s := range got {
		if s != want[s.ID] {
			t.Errorf("%s: %+v, want %+v", s.ID, s, want[s.ID])
		}
	}
}

// The part of an id that is neither a number nor a tag is the subject's name: the fact kind, the branch,
// the path of a gitlink or worktree, the id of a source; it is null for the kinds that have none.
func TestDocumentSubjectName(t *testing.T) {
	names := map[string]any{}
	for _, a := range decode(t, fixtureReport().Document())["actions"].([]any) {
		item := a.(map[string]any)
		names[item["category"].(string)] = item["subject"].(map[string]any)["name"]
	}
	want := map[string]any{"local-changes": "dirty", "local-behind": "behind", "main-red": "main", "repo-error": nil, "pr-ready": nil, "release-draft": nil, "release-run-failed": nil}
	for cat, name := range want {
		if got, ok := names[cat]; !ok || got != name {
			t.Errorf("%s: name %v (present %v), want %v", cat, got, ok, name)
		}
	}
}

func TestDocumentSourceErrorCarriesOnlyItsCode(t *testing.T) {
	rep := fixtureReport()
	rep.Remote.Repos[1].Remote.Sources[overview.SourceTags] = overview.SourceState{Outcome: overview.OutcomeError, Code: "http-5xx"}
	tags := decode(t, rep.Document())["repos"].([]any)[1].(map[string]any)["remote"].(map[string]any)["sources"].(map[string]any)["tags"].(map[string]any)
	if tags["state"] != "error" || tags["error"] != "http-5xx" || tags["reason"] != nil || tags["data"] != nil {
		t.Fatalf("%v", tags)
	}
}
