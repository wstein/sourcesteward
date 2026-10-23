// SPDX-License-Identifier: EUPL-1.2

package statuscmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/statusremote"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

var runTime = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

// workspaceLocal is a local status of three repositories: the root with a GitHub origin, a submodule
// on another host and one that cannot be read.
func workspaceLocal() stwsync.StatusResult {
	res := stwsync.StatusResult{
		SchemaVersion: 1, Command: "status", Root: ptr("/ws"),
		Repos: []stwsync.RepoStatus{
			{Name: ".", Path: "/ws", Kind: "root", State: "ok", Branch: ptr("main"), Reasons: []string{}},
			{Name: "sub", Path: "/ws/sub", Kind: "submodule", State: "ok", Branch: ptr("main"), Reasons: []string{}},
			{Name: "broken", Path: "/ws/broken", Kind: "submodule", State: "error", Reasons: []string{}, Error: &stwsync.Failure{Code: "status-read-failed", Message: "cannot read"}},
		},
		Summary: stwsync.StatusSummary{Total: 3, Errors: 1},
	}
	for i := range res.Repos { // as stwsync reports them: the worktree list is never null and a submodule has a gitlink
		res.Repos[i].Worktrees.List = []stwsync.WorktreeInfo{}
		if res.Repos[i].Kind == "submodule" {
			res.Repos[i].Gitlink = &stwsync.Gitlink{State: "uninitialized"}
		}
	}
	return res
}

func workspaceMembers(dir string) stwsync.Workspace {
	return stwsync.Workspace{Root: "/ws", Members: []stwsync.Member{
		{Name: ".", Path: dir, Kind: "root", Initialized: true, Remote: "https://github.com/acme/root.git"},
		{Name: "sub", Path: dir, Kind: "submodule", Initialized: true, Remote: "https://example.org/x/y.git"},
	}}
}

// forgeFake answers every read of a healthy, empty repository, or fails every read with err. It
// embeds a nil client on purpose: a read the status run must not make panics.
type forgeFake struct {
	statusremote.Client
	mu    sync.Mutex
	calls []string
	err   error
	draft bool
}

func (f *forgeFake) enter(call string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return f.err
}

func (f *forgeFake) RepoInfo(_ context.Context, repo string) (hosting.RepoInfo, error) {
	return hosting.RepoInfo{DefaultBranch: "main", HTMLURL: "https://github.com/" + repo, Push: ptr(true), Admin: ptr(false), Maintain: ptr(false)}, f.enter("repo " + repo)
}

func (f *forgeFake) ReleaseList(_ context.Context, repo string) ([]hosting.StatusRelease, error) {
	if err := f.enter("releases " + repo); err != nil {
		return nil, err
	}
	if f.draft {
		return []hosting.StatusRelease{{Draft: true, Tag: "v2.0.0", Name: "Two", URL: "https://github.com/" + repo + "/releases/9", Assets: 3}}, nil
	}
	return nil, nil
}

func (f *forgeFake) TagList(_ context.Context, repo string) ([]hosting.StatusTag, error) {
	return nil, f.enter("tags " + repo)
}

func (f *forgeFake) ListOpenPullItems(_ context.Context, repo string, _ int) ([]hosting.PullListItem, error) {
	return nil, f.enter("list " + repo)
}

func (f *forgeFake) Branch(_ context.Context, repo, _, _ string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{Status: 200, Body: []byte(`{"name":"main","commit":{"sha":"` + strings.Repeat("a", 40) + `"}}`)}, f.enter("branch " + repo)
}

func (f *forgeFake) CheckRuns(_ context.Context, repo, _ string, _ int, _ string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{Status: 200, Body: []byte(`{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{}}]}`)}, f.enter("check-runs " + repo)
}

func (f *forgeFake) Statuses(_ context.Context, repo, _ string, _ int, _ string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{Status: 200, Body: []byte(`[]`)}, f.enter("statuses " + repo)
}

func (f *forgeFake) IssuesPage(_ context.Context, repo string, _ int) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{Status: 200, Body: []byte(`[]`)}, f.enter("issues " + repo)
}

func (f *forgeFake) Viewer(context.Context) (string, error) { return "wstein", f.enter("user") }

func (f *forgeFake) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// runWith runs the online status over the fixture workspace with the given forge and open error.
func runWith(t *testing.T, forge *forgeFake, openErr error, progress *bytes.Buffer) (Report, *openRecord) {
	t.Helper()
	rec := &openRecord{}
	p := Params{
		Status: stwsync.StatusOptions{Path: "."}, Timeout: time.Minute, Now: func() time.Time { return runTime },
		Local:    func(context.Context, stwsync.StatusOptions) stwsync.StatusResult { return workspaceLocal() },
		Discover: func(context.Context, string) (stwsync.Workspace, error) { return workspaceMembers(t.TempDir()), nil },
		Open: func(_ context.Context, target, host string) (statusremote.Client, func(), error) {
			rec.target, rec.host, rec.opened = target, host, rec.opened+1
			if openErr != nil {
				return nil, func() {}, openErr
			}
			return forge, func() { rec.closed++ }, nil
		},
	}
	if progress != nil {
		p.Progress = progress
	}
	rep, err := Run(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	return rep, rec
}

type openRecord struct {
	target, host   string
	opened, closed int
}

func TestRunReadsTheGitHubRepositoryOnly(t *testing.T) {
	forge := &forgeFake{draft: true}
	rep, rec := runWith(t, forge, nil, nil)
	if rep.NoToken() {
		t.Fatal("a token was found")
	}
	if rec.opened != 1 || rec.closed != 1 || rec.target != "/ws" || rec.host != "github.com" {
		t.Fatalf("%+v", rec)
	}
	for _, c := range forge.calls {
		if !strings.HasSuffix(c, " acme/root") && c != "user" {
			t.Errorf("a repository that is no GitHub origin was read: %s", c)
		}
	}
	if !rep.Time.Equal(runTime) || len(rep.Remote.Repos) != 3 {
		t.Fatalf("%v %d", rep.Time, len(rep.Remote.Repos))
	}
	root, sub, broken := rep.Remote.Repos[0], rep.Remote.Repos[1], rep.Remote.Repos[2]
	if root.Slug != "acme/root" || root.Remote.Sources[overview.SourcePulls].Outcome != overview.OutcomeOK {
		t.Fatalf("%+v", root)
	}
	if sub.Remote.Sources[overview.SourcePulls].Reason != overview.ReasonOtherHost {
		t.Fatalf("%+v", sub.Remote.Sources)
	}
	if broken.Remote.Sources[overview.SourcePulls].Reason != overview.ReasonNotReadLocally {
		t.Fatalf("%+v", broken.Remote.Sources)
	}
	var drafts, repoErr bool
	for _, it := range rep.Overview.Items {
		drafts = drafts || it.Category == overview.ReleaseDraft
		repoErr = repoErr || it.Category == overview.RepoError
	}
	if !drafts || !repoErr {
		t.Fatalf("the model must combine the remote and local findings: %+v", rep.Overview.Items)
	}
}

func TestRunWithoutAUsableTokenReadsNothing(t *testing.T) {
	forge := &forgeFake{}
	rep, rec := runWith(t, forge, fmt.Errorf("gh: %w", hosting.ErrAuthentication), nil)
	if forge.requests() != 0 || rec.opened != 1 {
		t.Fatalf("%d requests, %+v", forge.requests(), rec)
	}
	if !rep.NoToken() {
		t.Fatal("the report must say that no token was found")
	}
	var noToken []overview.Notice
	for _, n := range rep.Overview.Notices {
		if n.Code == overview.ReasonNoToken {
			noToken = append(noToken, n)
		}
	}
	if len(noToken) != 1 || len(noToken[0].Repos) != 1 || noToken[0].Repos[0] != "acme/root" {
		t.Fatalf("one notice for the one repository that could be read: %+v", rep.Overview.Notices)
	}
	if rep.Overview.Summary.Failed || !rep.Overview.Summary.Degraded {
		t.Fatalf("a missing token is a skip: %+v", rep.Overview.Summary)
	}
}

func TestFailureIsAnAttemptedRequestThatFailed(t *testing.T) {
	network := &hosting.WatchError{Code: hosting.CodeNetwork, Message: "boom"}
	rep, _ := runWith(t, &forgeFake{err: network}, nil, nil)
	if !rep.Overview.Summary.Failed {
		t.Fatalf("%+v", rep.Overview.Summary)
	}
	local := workspaceLocal()
	local.Repos, local.Summary.Errors = local.Repos[:2], 0
	rep.Local = local
	if err := rep.Failure(); err == nil || !strings.Contains(err.Error(), "remote read failed") {
		t.Fatalf("%v", err)
	}
	healthy, _ := runWith(t, &forgeFake{}, nil, nil)
	healthy.Local = local
	if err := healthy.Failure(); err != nil {
		t.Fatalf("items never matter: %v", err)
	}
}

func TestFailureNamesTheLocalCause(t *testing.T) {
	rep := Report{Local: workspaceLocal()}
	if err := rep.Failure(); err == nil || err.Error() != "status: 1 repository cannot be read" {
		t.Fatalf("%v", err)
	}
	rep.Local.Summary.Errors = 2
	if err := rep.Failure(); err == nil || err.Error() != "status: 2 repositories cannot be read" {
		t.Fatalf("%v", err)
	}
	rep.Local.Error = &stwsync.Failure{Code: "status-not-a-repository", Message: "no"}
	if err := rep.Failure(); err == nil || err.Error() != "status: status-not-a-repository: no" {
		t.Fatalf("%v", err)
	}
}

func TestRunWithAnUnopenableReaderIsAFailedAttempt(t *testing.T) {
	const canary = "ghp_CANARY0123456789"
	rep, _ := runWith(t, &forgeFake{}, errors.New("gh failed with token "+canary), nil)
	if !rep.Overview.Summary.Failed {
		t.Fatalf("%+v", rep.Overview.Summary)
	}
	if text := fmt.Sprintf("%+v", rep.Overview); strings.Contains(text, canary) {
		t.Fatalf("the error text of the reader reached the model: %s", text)
	}
}

func TestRunWithAFailedLocalReadReadsNothingRemote(t *testing.T) {
	opened := false
	rep, err := Run(t.Context(), Params{
		Local: func(context.Context, stwsync.StatusOptions) stwsync.StatusResult {
			return stwsync.StatusResult{Repos: []stwsync.RepoStatus{}, Error: &stwsync.Failure{Code: "status-not-a-repository", Message: "no"}}
		},
		Discover: func(context.Context, string) (stwsync.Workspace, error) {
			t.Error("no discovery without a workspace")
			return stwsync.Workspace{}, nil
		},
		Open: func(context.Context, string, string) (statusremote.Client, func(), error) {
			opened = true
			return nil, func() {}, nil
		},
	})
	if err != nil || opened || len(rep.Remote.Repos) != 0 || rep.Failure() == nil || rep.Time.IsZero() {
		t.Fatalf("%v %v %+v", err, opened, rep)
	}
}

func TestRunReportsAWorkspaceWhoseRemotesCannotBeRead(t *testing.T) {
	_, err := Run(t.Context(), Params{
		Local: func(context.Context, stwsync.StatusOptions) stwsync.StatusResult { return workspaceLocal() },
		Discover: func(context.Context, string) (stwsync.Workspace, error) {
			return stwsync.Workspace{}, errors.New("git is gone")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "git is gone") {
		t.Fatalf("%v", err)
	}
}

func TestRunUsesTheWorkspaceRootOfTheDiscoveryWithoutALocalRoot(t *testing.T) {
	local := workspaceLocal()
	local.Root = nil
	rec := &openRecord{}
	_, err := Run(t.Context(), Params{
		Local:    func(context.Context, stwsync.StatusOptions) stwsync.StatusResult { return local },
		Discover: func(context.Context, string) (stwsync.Workspace, error) { return workspaceMembers(t.TempDir()), nil },
		Open: func(_ context.Context, target, _ string) (statusremote.Client, func(), error) {
			rec.target = target
			return &forgeFake{}, func() {}, nil
		},
	})
	if err != nil || rec.target != "/ws" {
		t.Fatalf("%v %+v", err, rec)
	}
}

func TestProgressIsOneRewrittenLineThatIsCleared(t *testing.T) {
	var progress bytes.Buffer
	forge := &forgeFake{}
	_, rec := runWith(t, forge, nil, &progress)
	if rec.opened != 1 || rec.closed != 1 {
		t.Fatalf("the reader is released once, with the progress line in front of it: %+v", rec)
	}
	got := progress.String()
	if n := forge.requests(); !strings.Contains(got, fmt.Sprintf("\rreading remote: %d requests", n)) || !strings.HasSuffix(got, "\r\x1b[K") {
		t.Fatalf("%d requests: %q", n, got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("the progress is one line: %q", got)
	}
	var quiet bytes.Buffer
	runWith(t, &forgeFake{}, fmt.Errorf("%w", hosting.ErrAuthentication), &quiet)
	if quiet.Len() != 0 {
		t.Fatalf("no request, no progress: %q", quiet.String())
	}
}

func TestProgressCountsEveryKindOfRequest(t *testing.T) {
	var out bytes.Buffer
	c := progressClient{Client: &everyRead{}, line: &progressLine{w: &out}}
	ctx := t.Context()
	_, _ = c.ListOpenPulls(ctx, "o/r", hosting.PullListState{}, 1)
	_, _ = c.PullDetail(ctx, "o/r", 1, "")
	_, _ = c.Branch(ctx, "o/r", "main", "")
	_, _ = c.Commit(ctx, "o/r", "abc", "")
	_, _ = c.PullCommits(ctx, "o/r", 1, 1, "")
	_, _ = c.CheckRuns(ctx, "o/r", "abc", 1, "")
	_, _ = c.Statuses(ctx, "o/r", "abc", 1, "")
	_, _ = c.RateLimit(ctx)
	_, _ = c.ListOpenPullItems(ctx, "o/r", 1)
	_, _ = c.WorkflowRuns(ctx, "o/r", "release.yml", "abc")
	_, _ = c.RepoInfo(ctx, "o/r")
	_, _ = c.ReleaseList(ctx, "o/r")
	_, _ = c.TagList(ctx, "o/r")
	_, _ = c.IssuesPage(ctx, "o/r", 1)
	_, _ = c.Viewer(ctx)
	if !strings.HasSuffix(out.String(), "reading remote: 15 requests") {
		t.Fatalf("%q", out.String())
	}
	c.line.clear()
	c.line.clear()
	if strings.Count(out.String(), "\x1b[K") != 1 {
		t.Fatalf("clear once: %q", out.String())
	}
}

// everyRead answers every read with nothing.
type everyRead struct{ statusremote.Client }

func (everyRead) ListOpenPulls(context.Context, string, hosting.PullListState, int) (hosting.PullListResult, error) {
	return hosting.PullListResult{}, nil
}
func (everyRead) PullDetail(context.Context, string, int, string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{}, nil
}
func (everyRead) Branch(context.Context, string, string, string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{}, nil
}
func (everyRead) Commit(context.Context, string, string, string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{}, nil
}
func (everyRead) PullCommits(context.Context, string, int, int, string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{}, nil
}
func (everyRead) CheckRuns(context.Context, string, string, int, string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{}, nil
}
func (everyRead) Statuses(context.Context, string, string, int, string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{}, nil
}
func (everyRead) RateLimit(context.Context) (hosting.RateInfo, error) { return hosting.RateInfo{}, nil }
func (everyRead) ListOpenPullItems(context.Context, string, int) ([]hosting.PullListItem, error) {
	return nil, nil
}
func (everyRead) WorkflowRuns(context.Context, string, string, string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{}, nil
}
func (everyRead) RepoInfo(context.Context, string) (hosting.RepoInfo, error) {
	return hosting.RepoInfo{}, nil
}
func (everyRead) IssuesPage(context.Context, string, int) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{}, nil
}
func (everyRead) Viewer(context.Context) (string, error) { return "", nil }
func (everyRead) ReleaseList(context.Context, string) ([]hosting.StatusRelease, error) {
	return nil, nil
}
func (everyRead) TagList(context.Context, string) ([]hosting.StatusTag, error) { return nil, nil }

// gitWorkspace is a real repository with a GitHub origin, for the defaults that read Git.
func gitWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	identity := []string{"-c", "user.name=t", "-c", "user.email=t@example.org"}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"remote", "add", "origin", "https://github.com/acme/root.git"}, append(identity, "commit", "-q", "--allow-empty", "-m", "init")} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestRunDefaultsReadTheRealWorkspace(t *testing.T) {
	dir := gitWorkspace(t)
	forge := &forgeFake{}
	rep, err := Run(t.Context(), Params{
		Status: stwsync.StatusOptions{Path: dir},
		Open: func(context.Context, string, string) (statusremote.Client, func(), error) {
			return forge, func() {}, nil
		},
	})
	if err != nil || rep.Local.Error != nil || rep.Time.IsZero() {
		t.Fatalf("%v %+v", err, rep.Local.Error)
	}
	if root := rep.Remote.Repos[0]; root.Slug != "acme/root" || forge.requests() == 0 {
		t.Fatalf("%+v %d", root, forge.requests())
	}
	if _, err := Run(t.Context(), Params{Status: stwsync.StatusOptions{Path: t.TempDir()}}); err != nil {
		t.Fatalf("a directory outside any repository is a failed local read, not an error: %v", err)
	}
}

func TestTheDefaultReaderIsTheStatusReader(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("HOME", t.TempDir())
	client, cleanup, err := Params{}.opener(t.TempDir())(t.Context())
	if err == nil || client != nil {
		t.Fatalf("no gh, no reader: %v %v", client, err)
	}
	cleanup()
}

// blockingForge never answers: every read waits for the deadline of the run.
type blockingForge struct{ statusremote.Client }

func (blockingForge) RepoInfo(ctx context.Context, _ string) (hosting.RepoInfo, error) {
	<-ctx.Done()
	return hosting.RepoInfo{}, ctx.Err()
}
func (blockingForge) ReleaseList(ctx context.Context, _ string) ([]hosting.StatusRelease, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (blockingForge) TagList(ctx context.Context, _ string) ([]hosting.StatusTag, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (blockingForge) IssuesPage(ctx context.Context, _ string, _ int) (hosting.WatchResponse, error) {
	<-ctx.Done()
	return hosting.WatchResponse{}, ctx.Err()
}
func (blockingForge) Viewer(ctx context.Context) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}
func (blockingForge) ListOpenPullItems(ctx context.Context, _ string, _ int) ([]hosting.PullListItem, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestADeadlineThatCutsTheReadsIsNoFailure(t *testing.T) {
	local := workspaceLocal()
	local.Repos, local.Summary.Errors = local.Repos[:1], 0
	rep, err := Run(t.Context(), Params{
		Timeout:  10 * time.Millisecond,
		Local:    func(context.Context, stwsync.StatusOptions) stwsync.StatusResult { return local },
		Discover: func(context.Context, string) (stwsync.Workspace, error) { return workspaceMembers(t.TempDir()), nil },
		Open: func(context.Context, string, string) (statusremote.Client, func(), error) {
			return blockingForge{}, func() {}, nil
		},
	})
	if err != nil || rep.Failure() != nil || rep.Overview.Summary.Failed || !rep.Overview.Summary.Degraded {
		t.Fatalf("%v %v %+v", err, rep.Failure(), rep.Overview.Summary)
	}
	var deadline bool
	for _, n := range rep.Overview.Notices {
		deadline = deadline || n.Code == overview.ReasonDeadline
	}
	if !deadline {
		t.Fatalf("a deadline skip is named by a notice: %+v", rep.Overview.Notices)
	}
}
