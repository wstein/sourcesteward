// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/statuscmd"
	"github.com/wstein/sourcesteward/internal/statusremote"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

// statusForge is a healthy, empty GitHub repository, or fails every read with err. It embeds a nil client
// on purpose: a read the status run must not make panics. It holds no credential.
type statusForge struct {
	statusremote.Client
	mu    sync.Mutex
	reads int
	err   error
}

func (f *statusForge) read() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	return f.err
}

func (f *statusForge) RepoInfo(_ context.Context, repo string) (hosting.RepoInfo, error) {
	return hosting.RepoInfo{DefaultBranch: "main", HTMLURL: "https://github.com/" + repo, Push: statusPtrTo(true), Admin: statusPtrTo(false), Maintain: statusPtrTo(false)}, f.read()
}
func (f *statusForge) ReleaseList(context.Context, string) ([]hosting.StatusRelease, error) {
	return nil, f.read()
}
func (f *statusForge) TagList(context.Context, string) ([]hosting.StatusTag, error) {
	return nil, f.read()
}
func (f *statusForge) ListOpenPullItems(context.Context, string, int) ([]hosting.PullListItem, error) {
	return nil, f.read()
}
func (f *statusForge) IssuesPage(context.Context, string, int) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{Status: 200, Body: []byte(`[]`)}, f.read()
}
func (f *statusForge) Viewer(context.Context) (string, error) { return "wstein", f.read() }
func (f *statusForge) Branch(context.Context, string, string, string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{Status: 200, Body: []byte(`{"name":"main","commit":{"sha":"` + strings.Repeat("a", 40) + `"}}`)}, f.read()
}
func (f *statusForge) CheckRuns(context.Context, string, string, int, string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{Status: 200, Body: []byte(`{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","output":{}}]}`)}, f.read()
}
func (f *statusForge) Statuses(context.Context, string, string, int, string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{Status: 200, Body: []byte(`[]`)}, f.read()
}

// onlineRun is one run of the command over fakes.
type onlineRun struct {
	out, errOut string
	err         error
	opened      int
	discovered  int
	locals      int
}

// runOnline runs `stw status ARGS` with the fake local status, workspace and forge. The workspace has one
// repository, the root, whose origin is a GitHub repository.
func runOnline(t *testing.T, local stwsync.StatusResult, forge statusremote.Client, openErr error, args ...string) onlineRun {
	t.Helper()
	var run onlineRun
	dir := t.TempDir()
	cmd := workspaceStatusCommandWith(statuscmd.Params{
		Now: func() time.Time { return time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC) },
		Local: func(context.Context, stwsync.StatusOptions) stwsync.StatusResult {
			run.locals++
			return local
		},
		Discover: func(context.Context, string) (stwsync.Workspace, error) {
			run.discovered++
			return stwsync.Workspace{Root: "/ws", Members: []stwsync.Member{{Name: ".", Path: dir, Kind: "root", Initialized: true, Remote: "https://github.com/acme/root.git"}}}, nil
		},
		Open: func(_ context.Context, _, host string) (statusremote.Client, func(), error) {
			run.opened++
			if host != "github.com" {
				t.Errorf("host %q", host)
			}
			return forge, func() {}, openErr
		},
	})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	run.err = cmd.ExecuteContext(context.Background())
	run.out, run.errOut = out.String(), errOut.String()
	return run
}

func rootOnly() stwsync.StatusResult {
	return stwsync.StatusResult{
		SchemaVersion: 1, Command: "status", Root: statusPtr("/ws"),
		Repos:   []stwsync.RepoStatus{{Name: ".", Path: "/ws", Kind: "root", State: "ok", Branch: statusPtr("main"), Upstream: statusPtr("origin/main"), Ahead: statusInt(0), Behind: statusInt(0), Reasons: []string{}, Worktrees: stwsync.WorktreeCounts{List: []stwsync.WorktreeInfo{}}}},
		Summary: stwsync.StatusSummary{Total: 1},
	}
}

func isRuntimeFailure(err error) bool {
	var failure runtimeFailure
	return errors.As(err, &failure)
}

func TestStatusIsOnlineByDefault(t *testing.T) {
	forge := &statusForge{}
	run := runOnline(t, rootOnly(), forge, nil)
	if run.err != nil || run.opened != 1 || run.locals != 1 || run.discovered != 1 || forge.reads == 0 {
		t.Fatalf("%+v %d reads", run, forge.reads)
	}
	for _, want := range []string{"Needs action: nothing\n", "Repositories\n", "remote: no open PRs, main CI green, no release drafts", "issues: 0 open (0 red, 0 yellow)\n", "summary: nothing needs action | 1 repository | 0 notices\n"} {
		if !strings.Contains(run.out, want) {
			t.Errorf("missing %q in\n%s", want, run.out)
		}
	}
	if run.errOut != "" || strings.Contains(run.out, "\x1b") {
		t.Errorf("nothing on stderr, no colour or progress off a terminal: %q", run.errOut)
	}
	if !strings.Contains(run.out, "main  origin/main up to date  clean") {
		t.Errorf("the Spec 033 block: %s", run.out)
	}
}

func TestStatusOfflineSendsNoRequestAndReadsNoCredential(t *testing.T) {
	for _, args := range [][]string{{"--offline"}, {"--offline", "--worktrees"}, {"--offline", "--json"}} {
		forge := &statusForge{err: errors.New("no request may be sent")}
		run := runOnline(t, statusCLIResult(), forge, errors.New("no reader may be opened"), args...)
		if run.err != nil || run.opened != 0 || run.discovered != 0 || forge.reads != 0 || run.locals != 1 {
			t.Fatalf("%v: %+v %d", args, run, forge.reads)
		}
		name := map[bool]string{false: "status-offline.txt", true: "status-offline.json"}[args[len(args)-1] == "--json"]
		if len(args) == 2 && args[1] == "--worktrees" {
			name = "status-offline-worktrees.txt"
		}
		statusGolden(t, name, run.out)
	}
}

func TestStatusOfflineIconsChangeOnlyTheMarkers(t *testing.T) {
	ascii := runOnline(t, statusCLIResult(), nil, nil, "--offline", "--icons", "ascii")
	unicode := runOnline(t, statusCLIResult(), nil, nil, "--offline", "--icons", "unicode")
	nerd := runOnline(t, statusCLIResult(), nil, nil, "--offline", "--icons", "nerd")
	plain := runOnline(t, statusCLIResult(), nil, nil, "--offline")
	if ascii.err != nil || unicode.err != nil || nerd.err != nil || ascii.out != plain.out {
		t.Fatalf("an explicit ascii equals the default: %v %v %v", ascii.err, unicode.err, nerd.err)
	}
	if !strings.Contains(unicode.out, "▲ sub") || strings.ReplaceAll(unicode.out, "▲ ", "! ") != plain.out {
		t.Errorf("unicode:\n%s", unicode.out)
	}
	if !strings.Contains(nerd.out, " sub") || strings.ReplaceAll(nerd.out, " ", "! ") != plain.out {
		t.Errorf("nerd:\n%s", nerd.out)
	}
}

func TestStatusUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"--offline", "--timeout", "30s"}, {"--offline", "--timeout=90s"}, {"--timeout", "0s"}, {"--timeout", "999ms"}, {"--timeout", "11m"}, {"--timeout", "-1m"},
		{"--timeout", "soon"}, {"--icons", "emoji"}, {"--icons", "Unicode"}, {"--offline", "--icons", "emoji"}, {"a", "b"},
	} {
		forge := &statusForge{}
		run := runOnline(t, rootOnly(), forge, nil, args...)
		if run.err == nil || isRuntimeFailure(run.err) || run.out != "" || run.opened+run.locals+run.discovered != 0 || forge.reads != 0 {
			t.Errorf("%v: %v %+v", args, run.err, run)
		}
	}
	for _, args := range [][]string{{"--timeout", "1s"}, {"--timeout", "10m"}, {"--timeout", "2m", "--icons", "ascii"}, {"--offline", "--icons", "nerd"}} {
		if run := runOnline(t, rootOnly(), &statusForge{}, nil, args...); run.err != nil {
			t.Errorf("%v: %v", args, run.err)
		}
	}
}

func TestStatusExitCodes(t *testing.T) {
	network := &hosting.WatchError{Code: hosting.CodeNetwork, Message: "boom"}
	failed := runOnline(t, rootOnly(), &statusForge{err: network}, nil)
	if !isRuntimeFailure(failed.err) || !strings.Contains(failed.out, "Notices") || !strings.Contains(failed.out, "network error") {
		t.Errorf("an attempted request that failed is exit 1 with the partial output: %v\n%s", failed.err, failed.out)
	}
	forge := &statusForge{err: network}
	noToken := runOnline(t, rootOnly(), forge, fmt.Errorf("gh: %w", hosting.ErrAuthentication))
	if noToken.err != nil || forge.reads != 0 || !strings.Contains(noToken.out, "no GitHub token (run: gh auth login)") {
		t.Errorf("no usable token is exit 0 with a notice and no request: %v %d\n%s", noToken.err, forge.reads, noToken.out)
	}
	attention := statusCLIResult()
	if run := runOnline(t, attention, &statusForge{}, nil); isRuntimeFailure(run.err) && attention.Summary.Errors == 0 {
		t.Errorf("attention is not a failure: %v", run.err)
	}
	unreadable := rootOnly()
	unreadable.Repos[0].State, unreadable.Repos[0].Error = "error", &stwsync.Failure{Code: "status-read-failed", Message: "cannot read"}
	unreadable.Summary.Errors = 1
	if run := runOnline(t, unreadable, &statusForge{}, nil); !isRuntimeFailure(run.err) || !strings.Contains(run.out, "cannot read") {
		t.Errorf("a repository that cannot be read is exit 1: %v", run.err)
	}
	broken := stwsync.StatusResult{Repos: []stwsync.RepoStatus{}, Error: &stwsync.Failure{Code: "status-not-a-repository", Message: "no"}}
	if run := runOnline(t, broken, nil, nil); !isRuntimeFailure(run.err) || run.out != "" || run.opened != 0 {
		t.Errorf("a failed local read prints nothing in text: %v %q", run.err, run.out)
	}
	run := runOnline(t, broken, nil, nil, "--json")
	var doc map[string]any
	if !isRuntimeFailure(run.err) || json.Unmarshal([]byte(run.out), &doc) != nil || doc["schema_version"] != float64(2) || doc["error"] == nil {
		t.Errorf("a failed local read prints the document in JSON: %v %q", run.err, run.out)
	}
}

// Without a usable token the output is exactly that of --offline plus the notices (Spec 037): no Needs action
// section, no remote lines, no second summary. The JSON keeps the skipped states of the schema.
func TestStatusWithoutATokenPrintsTheOfflineViewAndTheNotice(t *testing.T) {
	forge := &statusForge{err: errors.New("no request may be sent")}
	open := fmt.Errorf("gh: %w", hosting.ErrAuthentication)
	offline := runOnline(t, statusCLIResult(), forge, open, "--offline")
	for _, args := range [][]string{nil, {"--icons", "nerd"}} {
		run := runOnline(t, statusCLIResult(), forge, open, args...)
		want := offline.out
		if len(args) > 0 {
			want = runOnline(t, statusCLIResult(), forge, open, "--offline", "--icons", "nerd").out
		}
		if run.err != nil || forge.reads != 0 || !strings.HasPrefix(run.out, want) {
			t.Fatalf("%v: %v, %d reads, not the offline output followed by the notices:\n%s", args, run.err, forge.reads, run.out)
		}
		notices := strings.TrimPrefix(run.out, want)
		if !strings.HasPrefix(notices, "\nNotices\n  ") || !strings.Contains(notices, "no GitHub token (run: gh auth login)") {
			t.Errorf("%v: %q", args, notices)
		}
		for _, unwanted := range []string{"Needs action", "remote:", "| 0 notices", "Repositories\n"} {
			if strings.Contains(run.out, unwanted) {
				t.Errorf("%v: %q belongs to the online layout", args, unwanted)
			}
		}
	}
	plain := runOnline(t, statusCLIResult(), forge, open)
	statusGolden(t, "status-no-token.txt", plain.out)

	var doc map[string]any
	js := runOnline(t, statusCLIResult(), forge, open, "--json")
	if json.Unmarshal([]byte(js.out), &doc) != nil || doc["schema_version"] != float64(2) {
		t.Fatalf("%q", js.out)
	}
	remote := doc["repos"].([]any)[0].(map[string]any)["remote"].(map[string]any)
	if remote["state"] != "skipped" || remote["reason"] != "no-token" {
		t.Errorf("the JSON keeps the skipped states: %v", remote)
	}
}

// blockingForge never answers its first reads: they end only when the deadline of the run cancels them.
type blockingForge struct{ statusForge }

func (*blockingForge) RepoInfo(ctx context.Context, _ string) (hosting.RepoInfo, error) {
	<-ctx.Done()
	return hosting.RepoInfo{}, ctx.Err()
}

func (*blockingForge) ListOpenPullItems(ctx context.Context, _ string, _ int) ([]hosting.PullListItem, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// The value of --timeout is the overall deadline of the remote half: the run lasts exactly that long (the
// bubble's clock is fake) and the notice names it; without the flag the default of 90 s applies.
func TestStatusTimeoutIsTheDeadlineOfTheRemoteHalf(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want time.Duration
	}{"flag": {[]string{"--timeout", "7s"}, 7 * time.Second}, "default": {nil, 90 * time.Second}} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				run := runOnline(t, rootOnly(), &blockingForge{}, nil, tc.args...)
				if got := time.Since(start); got != tc.want {
					t.Errorf("the remote half ended after %v, want %v", got, tc.want)
				}
				if run.err != nil || !strings.Contains(run.out, fmt.Sprintf("deadline of %d s reached", int(tc.want.Seconds()))) {
					t.Errorf("a deadline skip is exit 0 with its notice: %v\n%s", run.err, run.out)
				}
			})
		})
	}
}

func TestStatusJSONIsSchemaTwoOnlineAndSchemaOneOffline(t *testing.T) {
	run := runOnline(t, rootOnly(), &statusForge{}, nil, "--json", "--icons", "nerd")
	var doc map[string]any
	if run.err != nil || json.Unmarshal([]byte(run.out), &doc) != nil || doc["schema_version"] != float64(2) || strings.Count(run.out, "\n") != 1 {
		t.Fatalf("%v %s", run.err, run.out)
	}
	if doc["options"].(map[string]any)["icons"] != nil {
		t.Error("the JSON carries no icons")
	}
	if strings.Contains(run.out, "\x1b") {
		t.Error("no escape in the JSON")
	}
	offline := runOnline(t, rootOnly(), nil, nil, "--json", "--offline")
	if err := json.Unmarshal([]byte(offline.out), &doc); err != nil || doc["schema_version"] != float64(1) {
		t.Fatalf("%v %s", err, offline.out)
	}
}

func TestStatusOnlineNeverLeaksAToken(t *testing.T) {
	const canary = "ghp_CANARY0123456789"
	for _, args := range [][]string{{}, {"--json"}} {
		run := runOnline(t, rootOnly(), &statusForge{}, errors.New("gh failed with token "+canary), args...)
		if !isRuntimeFailure(run.err) {
			t.Errorf("%v: a reader that cannot be opened is a failed attempt: %v", args, run.err)
		}
		for what, text := range map[string]string{"stdout": run.out, "stderr": run.errOut, "error": fmt.Sprint(run.err)} {
			if strings.Contains(text, canary) {
				t.Errorf("%v: the token reached %s", args, what)
			}
		}
	}
}

func TestStatusProgressStaysOffStdoutAndOffAPipe(t *testing.T) {
	run := runOnline(t, rootOnly(), &statusForge{}, nil)
	if run.errOut != "" || strings.Contains(run.out, "reading remote") {
		t.Errorf("no progress to a pipe: %q %q", run.errOut, run.out)
	}
}

// The progress goes to a stderr terminal only; stdout is a pipe here and must stay clean.
func TestStatusProgressGoesToAStderrTerminal(t *testing.T) {
	tty, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tty.Close() }()
	cmd := workspaceStatusCommandWith(statuscmd.Params{
		Local: func(context.Context, stwsync.StatusOptions) stwsync.StatusResult { return rootOnly() },
		Discover: func(context.Context, string) (stwsync.Workspace, error) {
			return stwsync.Workspace{Root: "/ws", Members: []stwsync.Member{{Name: ".", Path: t.TempDir(), Initialized: true, Remote: "https://github.com/acme/root.git"}}}, nil
		},
		Open: func(_ context.Context, _, _ string) (statusremote.Client, func(), error) {
			return &statusForge{}, func() {}, nil
		},
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(tty)
	cmd.SetArgs(nil)
	if err := cmd.ExecuteContext(context.Background()); err != nil || strings.Contains(out.String(), "reading remote") {
		t.Fatalf("%v %q", err, out.String())
	}
}

func TestStatusReportsAnOutputThatCannotBeWritten(t *testing.T) {
	for _, args := range [][]string{{"--offline"}, {"--offline", "--json"}, {}, {"--json"}} {
		cmd := workspaceStatusCommandWith(statuscmd.Params{
			Local:    func(context.Context, stwsync.StatusOptions) stwsync.StatusResult { return rootOnly() },
			Discover: func(context.Context, string) (stwsync.Workspace, error) { return stwsync.Workspace{Root: "/ws"}, nil },
			Open: func(context.Context, string, string) (statusremote.Client, func(), error) {
				return nil, func() {}, fmt.Errorf("%w", hosting.ErrAuthentication)
			},
		})
		cmd.SetOut(failingWriter{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(context.Background()); !isRuntimeFailure(err) {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestStatusStopsWithACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, args := range [][]string{{"--offline"}, {}} {
		cmd := workspaceStatusCommandWith(statuscmd.Params{
			Local:    func(context.Context, stwsync.StatusOptions) stwsync.StatusResult { return rootOnly() },
			Discover: func(context.Context, string) (stwsync.Workspace, error) { return stwsync.Workspace{Root: "/ws"}, nil },
			Open: func(context.Context, string, string) (statusremote.Client, func(), error) {
				return nil, func() {}, fmt.Errorf("%w", hosting.ErrAuthentication)
			},
		})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(ctx); !isRuntimeFailure(err) || !strings.Contains(err.Error(), "context canceled") {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestStatusReportsAWorkspaceWhoseRemotesCannotBeRead(t *testing.T) {
	cmd := workspaceStatusCommandWith(statuscmd.Params{
		Local: func(context.Context, stwsync.StatusOptions) stwsync.StatusResult { return rootOnly() },
		Discover: func(context.Context, string) (stwsync.Workspace, error) {
			return stwsync.Workspace{}, errors.New("git is gone")
		},
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(nil)
	if err := cmd.ExecuteContext(context.Background()); !isRuntimeFailure(err) || out.Len() != 0 {
		t.Fatalf("%v %q", err, out.String())
	}
}

func TestStatusReadsTheRealWorkspaceByDefault(t *testing.T) {
	dir := t.TempDir() // not a repository: the local read fails before any remote read
	for _, args := range [][]string{{"--offline", dir}, {dir}} {
		cmd := workspaceStatusCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		err := cmd.ExecuteContext(context.Background())
		if !isRuntimeFailure(err) || !strings.Contains(err.Error(), "status-not-a-repository") || out.Len() != 0 {
			t.Errorf("%v: %v %q", args, err, out.String())
		}
	}
}

// Without a usable token the marks of the output are those of the offline output for the same flags: the plain
// ones by default and the glyphs of --icons when it is given, so a UTF-8 terminal that asked for unicode or
// nerd glyphs sees them before the notices too.
func TestStatusWithoutATokenKeepsTheIconMarksOfTheOfflineOutput(t *testing.T) {
	forge := &statusForge{err: errors.New("no request may be sent")}
	open := fmt.Errorf("gh: %w", hosting.ErrAuthentication)
	for set, mark := range map[string]string{"unicode": "▲ sub", "nerd": " sub", "ascii": "! sub"} {
		offline := runOnline(t, statusCLIResult(), forge, open, "--offline", "--icons", set)
		run := runOnline(t, statusCLIResult(), forge, open, "--icons", set)
		if run.err != nil || !strings.HasPrefix(run.out, offline.out) || !strings.Contains(run.out, mark) {
			t.Errorf("%s: %v\n%s", set, run.err, run.out)
		}
	}
}

// --worktrees lists the linked worktrees in the output without a token exactly as it does offline.
func TestStatusWithoutATokenListsTheWorktreesLikeTheOfflineOutput(t *testing.T) {
	forge := &statusForge{err: errors.New("no request may be sent")}
	open := fmt.Errorf("gh: %w", hosting.ErrAuthentication)
	offline := runOnline(t, statusCLIResult(), forge, open, "--offline", "--worktrees")
	run := runOnline(t, statusCLIResult(), forge, open, "--worktrees")
	if run.err != nil || forge.reads != 0 || !strings.HasPrefix(run.out, offline.out) || !strings.Contains(run.out, "worktree /wt/one") {
		t.Fatalf("%v\n%s", run.err, run.out)
	}
	if notices := strings.TrimPrefix(run.out, offline.out); !strings.HasPrefix(notices, "\nNotices\n  ") {
		t.Errorf("%q", notices)
	}
}

// With colour the output without a token is the coloured offline output followed by the notices, and stripping
// the colour leaves the plain text. The command colours only on a terminal, so the renderer is called directly.
func TestStatusWithoutATokenColoursLikeTheOfflineOutput(t *testing.T) {
	rep := statuscmd.Report{Local: statusCLIResult(), Overview: overview.Overview{Notices: []overview.Notice{{Code: "no-token", Message: "no GitHub token (run: gh auth login)"}}}}
	var coloured, plain, offline bytes.Buffer
	for _, c := range []struct {
		w     *bytes.Buffer
		color bool
	}{{&coloured, true}, {&plain, false}} {
		if err := renderStatusNoToken(c.w, rep, true, c.color, asciiStatusMarks); err != nil {
			t.Fatal(err)
		}
	}
	if err := renderStatusText(&offline, rep.Local, true, true, asciiStatusMarks); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(coloured.String(), "\x1b[") || !strings.HasPrefix(coloured.String(), offline.String()) {
		t.Errorf("the coloured offline output is the start of the output:\n%s", coloured.String())
	}
	if got := ansi.ReplaceAllString(coloured.String(), ""); got != plain.String() {
		t.Errorf("stripping the colour must leave the plain text:\n%s\n%s", got, plain.String())
	}
	if !strings.HasSuffix(coloured.String(), "\nNotices\n  no GitHub token (run: gh auth login)\n") {
		t.Errorf("%q", coloured.String())
	}
}
