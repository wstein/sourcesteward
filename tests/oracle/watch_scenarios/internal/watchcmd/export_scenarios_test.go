// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/watch"
	"github.com/wstein/sourcesteward/internal/watchexport"
)

func TestExportScenarios(t *testing.T) {
	dir, err := watchexport.OutDir()
	if err != nil {
		t.Skip("STW_EXPORT_DIR not set: " + err.Error())
	}
	exportObserverScenarios(t, dir)
	exportCmdVectors(t, dir)
	exportRunLoops(t, dir)
}

func put(t *testing.T, dir string, v *watchexport.VectorDoc) {
	t.Helper()
	if err := watchexport.Write(dir, v.Name, v); err != nil {
		t.Fatal(err)
	}
}

func exportCmdVectors(t *testing.T, dir string) {
	// ---- human lines ----
	{
		v := watchexport.NewVectors("human-lines-golden", "human_line", "The `--human` line of every kind: HH:MM:SS (UTC), repo#pr, kind, a summary; SHAs shortened to 7 characters. The restack-needed line carries Go's git command (owner decision F1: kept as in Go).",
			nil, "watchcmd/human_test.go:TestHumanLinesGolden")
		r := s("wstein/sourcesteward")
		a, b := sha40("3"), sha40("9")
		tr := true
		evs := []struct {
			name string
			e    watch.Event
		}{
			{"new-pr", ev("new-pr", r, n(41), &watch.NewPRDetails{Title: "docs(035): stw watch", Author: "wstein", Draft: true, Base: "main", HeadRef: "docs/x", Head: a})},
			{"head-changed", ev("head-changed", r, n(41), &watch.HeadChangedDetails{Old: a, New: b})},
			{"checks-green", ev("checks-green", r, n(41), &watch.ChecksGreenDetails{Head: a, Base: "main", Total: 5})},
			{"checks-red one", ev("checks-red", r, n(41), &watch.ChecksRedDetails{Head: a, Failing: []string{"docs-check"}, FailingTotal: 1, Shape: "real", Base: "main"})},
			{"checks-red many", ev("checks-red", r, n(41), &watch.ChecksRedDetails{Head: a, Failing: []string{"a", "b"}, FailingTotal: 5, Shape: "unknown", Base: "main"})},
			{"checks-pending-timeout", ev("checks-pending-timeout", r, n(41), &watch.PendingTimeoutDetails{Head: a, Pending: []string{"ci"}, Since: "2026-10-22T09:00:00Z"})},
			{"mergeable behind", ev("mergeable-changed", r, n(41), &watch.MergeableDetails{Mergeable: &tr, State: "behind", Behind: true, Base: "main"})},
			{"mergeable conflicting", ev("mergeable-changed", r, n(41), &watch.MergeableDetails{Mergeable: new(bool), State: "dirty", Conflicting: true, Base: "main"})},
			{"ready-state-changed", ev("ready-state-changed", r, n(41), &watch.ReadyDetails{From: "draft", To: "ready"})},
			{"base-changed", ev("base-changed", r, n(41), &watch.BaseChangedDetails{Old: "feat/x", New: "main"})},
			{"merged", ev("merged", r, n(41), &watch.MergedDetails{MergeCommit: &b, Base: "main", Head: a, HeadRef: "docs/x", MethodHint: "rebase-or-squash"})},
			{"merged unknown commit", ev("merged", r, n(41), &watch.MergedDetails{Base: "main", Head: a, MethodHint: "unknown"})},
			{"closed", ev("closed", r, n(41), &watch.ClosedDetails{Head: a, Base: "main"})},
			{"restack-needed with command", ev("restack-needed", r, n(42), &watch.RestackDetails{MergedPR: 41, Base: "main", Reason: "contains-merged-commits", OldParent: a, NewParent: &b, Branch: "feat/035-impl",
				Argv: []string{"git", "-c", "rebase.updateRefs=false", "-c", "rerere.enabled=false", "rebase", "--onto", b, a, "feat/035-impl"}})},
			{"restack-needed without command", ev("restack-needed", r, n(42), &watch.RestackDetails{MergedPR: 41, Base: "main", Reason: "child-of-merged-branch", OldParent: a, Branch: "someone:feat"})},
			{"repos-changed", ev("repos-changed", nil, nil, &watch.ReposChangedDetails{Added: []string{"o/a"}, Removed: []string{"o/b", "o/c"}, Skipped: []watch.Skipped{{Path: "x", Reason: "r"}}})},
			{"watch-error", ev("watch-error", r, nil, &watch.WatchErrorDetails{Code: "network", Message: "HTTP read failed", Failures: 3, RetryInSeconds: 240, Repo: r})},
			{"watch-error run level", ev("watch-error", nil, nil, &watch.WatchErrorDetails{Code: "rate-limited", Message: "wait", Failures: 1, RetryInSeconds: 60})},
			{"watch-recovered", ev("watch-recovered", r, nil, &watch.WatchRecoveredDetails{AfterFailures: 3, Repo: r})},
			{"watch-stalled", ev("watch-stalled", r, nil, &watch.WatchStalledDetails{Since: "2026-10-22T09:00:00Z", ThresholdSeconds: 600, Repo: "wstein/sourcesteward"})},
		}
		for _, k := range evs {
			raw, _ := json.Marshal(k.e)
			v.Add(k.name, map[string]any{"event": json.RawMessage(raw)}, map[string]string{"line": strings.TrimSuffix(string(Human(k.e)), "\n")})
		}
		put(t, dir, v)
	}
	{
		v := watchexport.NewVectors("human-lines-untrusted-text", "human_line_properties", "Text from the forge in a `--human` line never carries raw control characters, bidi or line separators, and a line is one line and bounded. The exact escaping is Go's (`untrusted.Esc`); the Rust port escapes differently (deviation D6), so only the properties are the contract.",
			[]string{"go_line"}, "watchcmd/human_test.go:TestHumanEscapesControlCharactersAndBoundsText", "watchcmd/human_test.go:TestHumanEscapesEveryTextField")
		r := s("o/r")
		bad := "x\x1b[31m\u009b ‮\x7f"
		a := bad + strings.Repeat("0", 40)
		tr := true
		cases := []struct {
			name string
			e    watch.Event
		}{
			{"new-pr title with escape, newline, bidi", ev("new-pr", r, n(1), &watch.NewPRDetails{Title: "a\x1b[31mred\nline‮\x00", Author: "al\rice", Base: "main", Head: sha40("1")})},
			{"new-pr title of 500 characters", ev("new-pr", r, n(1), &watch.NewPRDetails{Title: strings.Repeat("é", 500), Author: "a", Base: "main", Head: sha40("1")})},
			{"unknown kind", watch.Event{Kind: "future-kind", Time: "garbage", Details: nil}},
			{"new-pr every field", ev("new-pr", r, n(1), &watch.NewPRDetails{Title: bad, Author: bad, Base: bad, HeadRef: bad, Head: a})},
			{"checks-red every field", ev("checks-red", r, n(1), &watch.ChecksRedDetails{Head: a, Failing: []string{bad}, FailingTotal: 1, Shape: bad, Base: bad})},
			{"checks-pending-timeout every field", ev("checks-pending-timeout", r, n(1), &watch.PendingTimeoutDetails{Head: a, Pending: []string{bad}, Since: bad})},
			{"mergeable-changed every field", ev("mergeable-changed", r, n(1), &watch.MergeableDetails{Mergeable: &tr, State: bad, Base: bad})},
			{"ready-state-changed every field", ev("ready-state-changed", r, n(1), &watch.ReadyDetails{From: bad, To: bad})},
			{"base-changed every field", ev("base-changed", r, n(1), &watch.BaseChangedDetails{Old: bad, New: bad})},
			{"merged every field", ev("merged", r, n(1), &watch.MergedDetails{MergeCommit: &a, Base: bad, Head: a, HeadRef: bad, MethodHint: bad})},
			{"closed every field", ev("closed", r, n(1), &watch.ClosedDetails{Head: a, Base: bad})},
			{"restack-needed every field", ev("restack-needed", r, n(2), &watch.RestackDetails{MergedPR: 1, Base: bad, Reason: bad, OldParent: a, NewParent: &a, Branch: bad, Argv: []string{"git"}})},
			{"watch-error every field", ev("watch-error", r, nil, &watch.WatchErrorDetails{Code: bad, Message: bad})},
			{"watch-stalled every field", ev("watch-stalled", r, nil, &watch.WatchStalledDetails{Since: bad, Repo: bad})},
		}
		for _, k := range cases {
			line := string(Human(k.e))
			body := strings.TrimSuffix(line, "\n")
			clean := true
			for _, c := range body {
				if c < 0x20 || c == 0x7f || (c >= 0x80 && c <= 0x9f) || c == 0x2028 || c == 0x202e {
					clean = false
				}
			}
			raw, _ := json.Marshal(k.e)
			v.Add(k.name, map[string]any{"event": json.RawMessage(raw)}, map[string]any{"single_line": strings.Count(line, "\n") == 1 && strings.HasSuffix(line, "\n"),
				"no_raw_control_characters": clean, "at_most_400_runes": len([]rune(line)) <= 400, "go_line": body})
		}
		put(t, dir, v)
	}

	// ---- config ----
	{
		v := watchexport.NewVectors("watch-config-parse", "parse_config", "The strict profile of `.stw/watch.toml`: every key read; unknown keys and tables, wrong types, duplicate keys, a missing or different schema_version, bad repo names, out-of-range numbers and invalid TOML are errors (usage, exit 2).",
			[]string{"go_message"}, "watchcmd/config_test.go:TestParseConfigReadsEveryKey", "watchcmd/config_test.go:TestParseConfigIsStrict")
		type out struct {
			OK            bool     `json:"ok"`
			Repos         []string `json:"repos"`
			IntervalSecs  int      `json:"interval_seconds"`
			DetailEvery   int      `json:"detail_every"`
			MaxOpenPRs    int      `json:"max_open_prs"`
			IgnoreAuthors []string `json:"ignore_authors"`
			IgnoreLabels  []string `json:"ignore_labels"`
			StackPatterns []string `json:"stack_patterns"`
			GoMessage     string   `json:"go_message"`
		}
		parse := func(doc string) out {
			c, err := ParseConfig([]byte(doc))
			if err != nil {
				return out{GoMessage: err.Error(), Repos: []string{}, IgnoreAuthors: []string{}, IgnoreLabels: []string{}, StackPatterns: []string{}}
			}
			return out{OK: true, Repos: nonNilS(c.Repos), IntervalSecs: int(c.Interval / time.Second), DetailEvery: c.DetailEvery, MaxOpenPRs: c.MaxOpenPRs,
				IgnoreAuthors: nonNilS(c.IgnoreAuthors), IgnoreLabels: nonNilS(c.IgnoreLabels), StackPatterns: nonNilS(c.StackPatterns)}
		}
		add := func(name, doc string) { v.Add(name, map[string]string{"toml": doc}, parse(doc)) }
		add("every key", fullConfig)
		add("minimal", "schema_version = 1\n")
		cases := map[string]string{
			"no schema_version":    "repos = [\"a/b\"]\n",
			"wrong version":        "schema_version = 2\n",
			"unknown key":          "schema_version = 1\nbogus = 1\n",
			"unknown ignore key":   "schema_version = 1\n[ignore]\nbots = [\"x\"]\n",
			"unknown shape key":    "schema_version = 1\n[shape]\ncheck_names = [\"x\"]\n",
			"unknown table":        "schema_version = 1\n[other]\na = 1\n",
			"duplicate key":        "schema_version = 1\nschema_version = 1\n",
			"repos wrong type":     "schema_version = 1\nrepos = \"a/b\"\n",
			"bad repo":             "schema_version = 1\nrepos = [\"a\"]\n",
			"interval not string":  "schema_version = 1\ninterval = 60\n",
			"interval too short":   "schema_version = 1\ninterval = \"5s\"\n",
			"interval garbage":     "schema_version = 1\ninterval = \"soon\"\n",
			"detail_every zero":    "schema_version = 1\ndetail_every = 0\n",
			"max_open_prs zero":    "schema_version = 1\nmax_open_prs = 0\n",
			"max_open_prs over":    "schema_version = 1\nmax_open_prs = 2001\n",
			"empty pattern":        "schema_version = 1\n[shape]\nstack_patterns = [\"\"]\n",
			"authors wrong type":   "schema_version = 1\n[ignore]\nauthors = 1\n",
			"invalid toml profile": "schema_version = 1\nrepos = [\n",
			"max_open_prs 2000":    "schema_version = 1\nmax_open_prs = 2000\n",
			"interval 10s":         "schema_version = 1\ninterval = \"10s\"\n",
		}
		for _, k := range sortedKeysOf(cases) {
			add(k, cases[k])
		}
		put(t, dir, v)
	}

	// ---- remotes and hosts ----
	{
		v := watchexport.NewVectors("watch-parse-remote", "parse_remote", "OWNER/NAME of a remote URL for a host (https, ssh://, scp-like; .git and a trailing slash removed; a local path, another host, extra path segments and `..` refused).", nil,
			"watchcmd/resolve_test.go:TestParseRemoteAcceptsTheGitHubSpellings")
		type in struct {
			Remote string `json:"remote"`
			Host   string `json:"host"`
		}
		for _, u := range []string{"https://github.com/wstein/anchorage.git", "https://github.com/wstein/anchorage", "https://github.com/wstein/anchorage/", "https://user@github.com/wstein/a.b.git",
			"git@github.com:wstein/a.git", "ssh://git@github.com/wstein/a.git", "ssh://git@github.com:22/wstein/a.git", "https://GitHub.com/wstein/a", "https://github.com/wstein/a.git/",
			"  https://github.com/wstein/a.git\n", "", "/srv/git/a.git", "../a", "https://example.org/wstein/a", "https://github.com/wstein", "https://github.com/wstein/a/b",
			"git@gitlab.com:wstein/a.git", "https://github.com/wstein/../a", "https://github.com.evil.example/wstein/a", "file:///github.com/wstein/a"} {
			got, ok := ParseRemote(u, "github.com")
			v.Add(u, in{u, "github.com"}, map[string]any{"ok": ok, "repo": got})
		}
		got, ok := ParseRemote("git@ghe.example.com:team/x.git", "ghe.example.com")
		v.Add("enterprise host", in{"git@ghe.example.com:team/x.git", "ghe.example.com"}, map[string]any{"ok": ok, "repo": got})
		put(t, dir, v)
	}
	{
		v := watchexport.NewVectors("watch-split-remote", "split_remote", "Host and path of a remote URL without judging either; a local path or an unsupported scheme is refused.", nil,
			"watchcmd/resolve_test.go:TestSplitRemoteReturnsHostAndPathWithoutJudgingThem")
		for _, u := range []string{"https://github.com/wstein/a.git", "git@gitlab.com:team/x", "ssh://git@ghe.example.com:22/t/x", "https://github.com/wstein/../a", "  https://GitHub.com/wstein/a/\n",
			"https://github.com/wstein/a/b/c.git", "", "/srv/git/a.git", "../a", "file:///github.com/wstein/a", "ftp://github.com/a/b"} {
			h, p, ok := SplitRemote(u)
			v.Add(u, map[string]string{"remote": u}, map[string]any{"ok": ok, "host": h, "path": p})
		}
		put(t, dir, v)
	}
	{
		v := watchexport.NewVectors("watch-valid-host", "valid_host", "A host name with an optional port for `--host`.", nil, "watchcmd/prepare_test.go:TestValidHost")
		for _, h := range []string{"github.com", "ghe.example.com:8443", "a", "", "-x.com", "a b", "x.com/", "x.com:123456", "x.com:65535", "a..b", "a.b-"} {
			v.Add(h, map[string]string{"host": h}, map[string]bool{"valid": ValidHost(h)})
		}
		put(t, dir, v)
	}

	// ---- resolve ----
	{
		v := watchexport.NewVectors("watch-resolve-repositories", "resolve_repositories", "The repository set of a run: --repo flags, then the workspace members with a GitHub remote on the host, then the config repos, in this order, without duplicates (case-insensitive); everything else in the workspace is skipped and reported with a reason; no source, a bad name, a failing discovery, a workspace without a GitHub repository and more than 256 repositories are usage errors.",
			[]string{"go_message", "skipped_reason"}, "watchcmd/resolve_test.go:TestResolveUnionsFlagsWorkspaceAndConfigInOrder", "watchcmd/resolve_test.go:TestResolveUsageErrors")
		type member struct {
			Name        string `json:"name"`
			Initialized bool   `json:"initialized"`
			Remote      string `json:"remote"`
		}
		type in struct {
			Host           string   `json:"host"`
			Repos          []string `json:"repo_flags"`
			Workspace      bool     `json:"workspace"`
			ConfigRepos    []string `json:"config_repos"`
			HasConfig      bool     `json:"has_config"`
			Members        []member `json:"workspace_members"`
			WorkspaceRoot  string   `json:"workspace_root"`
			DiscoveryError bool     `json:"discovery_fails"`
		}
		type skip struct {
			Path   string `json:"path"`
			Reason string `json:"skipped_reason"`
		}
		type out struct {
			OK            bool     `json:"ok"`
			Repos         []string `json:"repos"`
			WorkspaceRoot string   `json:"workspace_root"`
			Skipped       []skip   `json:"skipped"`
			GoMessage     string   `json:"go_message"`
		}
		run := func(i in) out {
			ri := ResolveInput{Host: i.Host, Repos: i.Repos, Workspace: i.Workspace}
			if i.HasConfig {
				ri.Config = &Config{Repos: i.ConfigRepos}
			}
			var ms []stwsync.Member
			for _, m := range i.Members {
				ms = append(ms, stwsync.Member{Name: m.Name, Initialized: m.Initialized, Remote: m.Remote})
			}
			ri.Discover = func(context.Context, string) (stwsync.Workspace, error) {
				if i.DiscoveryError {
					return stwsync.Workspace{}, errors.New("not a repository")
				}
				return stwsync.Workspace{Root: i.WorkspaceRoot, Members: ms}, nil
			}
			sel, err := Resolve(context.Background(), ri)
			o := out{Repos: nonNilS(sel.Repos), WorkspaceRoot: sel.WorkspaceRoot, Skipped: []skip{}}
			for _, sk := range sel.Skipped {
				o.Skipped = append(o.Skipped, skip{sk.Path, sk.Reason})
			}
			if err != nil {
				if !errors.Is(err, ErrUsage) {
					t.Fatalf("not a usage error: %v", err)
				}
				o.GoMessage = err.Error()
				return o
			}
			o.OK = true
			return o
		}
		add := func(name string, i in) {
			if i.Repos == nil {
				i.Repos = []string{}
			}
			if i.ConfigRepos == nil {
				i.ConfigRepos = []string{}
			}
			if i.Members == nil {
				i.Members = []member{}
			}
			v.Add(name, i, run(i))
		}
		add("flags, workspace and config in order, duplicates removed", in{Host: "github.com", Repos: []string{"x/first", "O/A"}, Workspace: true, HasConfig: true, ConfigRepos: []string{"c/one", "x/FIRST"}, WorkspaceRoot: "/ws",
			Members: []member{{".", true, "https://github.com/o/root.git"}, {"a", true, "git@github.com:o/a.git"}, {"gl", true, "https://gitlab.com/o/gl"}, {"loc", true, "/srv/git/loc"}, {"bare", true, ""}, {"off", false, ""}}})
		add("no source", in{Host: "github.com"})
		add("bad repo flag", in{Host: "github.com", Repos: []string{"nonsense"}})
		add("discovery fails", in{Host: "github.com", Workspace: true, DiscoveryError: true})
		add("a workspace without any GitHub repository", in{Host: "github.com", Workspace: true, WorkspaceRoot: "/ws", Members: []member{{".", true, "/x"}}})
		var many []string
		for i := 0; i < 257; i++ {
			many = append(many, "o/r"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune('a'+i/26)))
		}
		add("more than 256 repositories", in{Host: "github.com", Repos: many})
		add("exactly 256 repositories", in{Host: "github.com", Repos: many[:256]})
		add("a config without repos and a workspace without GitHub members is accepted only with repos", in{Host: "github.com", Workspace: true, HasConfig: true, ConfigRepos: []string{"c/one"}, WorkspaceRoot: "/ws", Members: []member{{".", true, "/x"}}})
		put(t, dir, v)
	}

	// ---- prepare (flags and config text) ----
	exportPrepare(t, dir)

	// ---- pulls/N decode ----
	{
		v := watchexport.NewVectors("watch-decode-pull-detail", "decode_pull_detail", "The fields read from pulls/N: mergeable (null = not computed), mergeable_state, rebaseable, head sha, and the fork flag (head repository missing or different from the base repository, case-insensitive); a body that is not an object is malformed.", nil,
			"watchcmd/pullsread_test.go:TestDecodePullDetailReadsHeadSHAAndRebaseable", "watchcmd/pullsread_test.go:TestDecodePullDetailNullsAndFork")
		for name, body := range map[string]string{
			"complete":             `{"mergeable":true,"mergeable_state":"clean","rebaseable":false,"head":{"sha":"` + sha40("c") + `","repo":{"full_name":"o/r"}},"base":{"repo":{"full_name":"O/R"}}}`,
			"nulls and fork":       `{"mergeable":null,"mergeable_state":"unknown","rebaseable":null,"head":{"repo":null},"base":{"repo":{"full_name":"o/r"}}}`,
			"array":                `[1]`,
			"base repo missing":    `{"mergeable":true,"mergeable_state":"clean","head":{"repo":{"full_name":"o/r"}},"base":{}}`,
			"different repository": `{"head":{"repo":{"full_name":"x/fork"}},"base":{"repo":{"full_name":"o/r"}}}`,
		} {
			d, err := DecodePullDetail([]byte(body))
			var e any = map[string]any{"ok": true, "mergeable": d.Mergeable, "mergeable_state": d.MergeableState, "rebaseable": d.Rebaseable, "head_sha": d.HeadSHA, "fork": d.Fork}
			if err != nil {
				var we *hosting.WatchError
				code := ""
				if errors.As(err, &we) {
					code = we.Code
				}
				e = map[string]any{"ok": false, "error_code": code}
			}
			v.Add(name, map[string]string{"body": body}, e)
		}
		sortCases2(v)
		put(t, dir, v)
	}
}

func sortCases2(v *watchexport.VectorDoc) {
	sort.Slice(v.Cases, func(i, j int) bool { return v.Cases[i].Name < v.Cases[j].Name })
}

func exportPrepare(t *testing.T, dir string) {
	v := watchexport.NewVectors("watch-prepare-flags", "prepare_flags", "The validated run of `stw watch` from its flags (and a config file given as text): defaults (stream, 60 s, detail every 10, pending timeout 45 min, 2000 open PRs), `--until-event` (timeout 590 s), the flags that exclude each other and every usage error (exit 2: nothing read, nothing written), the config merge (flags win), and the state directory of `--status`.",
		[]string{"go_message"}, "watchcmd/prepare_test.go:TestPrepareDefaults", "watchcmd/prepare_test.go:TestPrepareRejectsInvalidCombinations", "watchcmd/prepare_test.go:TestPrepareMergesTheConfigAndLetsFlagsOverrideIt", "watchcmd/prepare_test.go:TestPrepareStatusResolvesTheStateDirectory")
	type fl struct {
		Repos             []string `json:"repo"`
		Once              bool     `json:"once"`
		Stream            bool     `json:"stream"`
		UntilEvent        []string `json:"until_event"`
		Timeout           *string  `json:"timeout"`
		Interval          *string  `json:"interval"`
		DetailEvery       *int     `json:"detail_every"`
		MaxIdle           *string  `json:"max_idle"`
		PendingTO         *string  `json:"pending_timeout"`
		StateDir          string   `json:"state_dir"`
		Host              string   `json:"host"`
		Baseline          bool     `json:"baseline"`
		Human             bool     `json:"human"`
		Status            bool     `json:"status"`
		JSON              bool     `json:"json"`
		WorkspacePath     string   `json:"workspace_path"`
		ConfigTOML        *string  `json:"config_file_content"`
		MissingConfigFile bool     `json:"config_file_missing"`
	}
	type run struct {
		Mode          string   `json:"mode"`
		IntervalSecs  int      `json:"interval_seconds"`
		DetailEvery   int      `json:"detail_every"`
		PendingSecs   int      `json:"pending_timeout_seconds"`
		MaxOpenPRs    int      `json:"max_open_prs"`
		Host          string   `json:"host"`
		Human         bool     `json:"human"`
		Baseline      bool     `json:"baseline"`
		Repos         []string `json:"repos"`
		UntilKinds    []string `json:"until_kinds"`
		TimeoutSecs   int      `json:"timeout_seconds"`
		MaxIdleSecs   int      `json:"max_idle_seconds"`
		IgnoreAuthors []string `json:"ignore_authors"`
		IgnoreLabels  []string `json:"ignore_labels"`
		StackPatterns []string `json:"stack_patterns"`
	}
	type out struct {
		OK                       bool    `json:"ok"`
		IsStatus                 bool    `json:"is_status"`
		Run                      *run    `json:"run"`
		StatusStateDirIsAbsolute bool    `json:"status_state_dir_is_absolute"`
		StatusStateDirExplicit   *string `json:"status_state_dir_explicit"`
		GoMessage                string  `json:"go_message"`
	}
	str := func(s string) *string { return &s }
	num := func(i int) *int { return &i }
	do := func(f fl) out {
		flags := Flags{Repos: f.Repos, Once: f.Once, Stream: f.Stream, UntilEvent: f.UntilEvent, StateDir: f.StateDir, Host: f.Host, Baseline: f.Baseline, Human: f.Human,
			Status: f.Status, JSON: f.JSON, WorkspacePath: f.WorkspacePath}
		if f.Timeout != nil {
			flags.Timeout, flags.TimeoutSet = *f.Timeout, true
		}
		if f.Interval != nil {
			flags.Interval, flags.IntervalSet = *f.Interval, true
		}
		if f.DetailEvery != nil {
			flags.DetailEvery, flags.DetailSet = *f.DetailEvery, true
		}
		if f.MaxIdle != nil {
			flags.MaxIdle, flags.MaxIdleSet = *f.MaxIdle, true
		}
		if f.PendingTO != nil {
			flags.PendingTO, flags.PendingTOSet = *f.PendingTO, true
		}
		if f.WorkspacePath != "" {
			flags.Workspace = false
		}
		if f.ConfigTOML != nil {
			p := filepath.Join(t.TempDir(), "watch.toml")
			os.WriteFile(p, []byte(*f.ConfigTOML), 0o644)
			flags.Config = p
		}
		if f.MissingConfigFile {
			flags.Config = filepath.Join(t.TempDir(), "none.toml")
		}
		p, err := Prepare(context.Background(), flags, &bytes.Buffer{})
		if err != nil {
			if !errors.Is(err, ErrUsage) {
				t.Fatalf("not a usage error: %v", err)
			}
			return out{GoMessage: strings.ReplaceAll(err.Error(), filepath.Dir(flags.Config), "<dir>")}
		}
		if p.IsStat {
			o := out{OK: true, IsStatus: true, StatusStateDirIsAbsolute: filepath.IsAbs(p.Status.StateDir)}
			if f.StateDir != "" {
				o.StatusStateDirExplicit = str(p.Status.StateDir)
			}
			return o
		}
		r := p.Run
		kinds := []string{}
		for k := range r.UntilKinds {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		return out{OK: true, Run: &run{Mode: r.Mode, IntervalSecs: int(r.Interval / time.Second), DetailEvery: r.DetailEvery, PendingSecs: int(r.PendingTimeout / time.Second), MaxOpenPRs: r.MaxOpenPRs,
			Host: r.Host, Human: r.Human, Baseline: r.Baseline, Repos: nonNilS(r.Repos), UntilKinds: kinds, TimeoutSecs: int(r.Timeout / time.Second), MaxIdleSecs: int(r.MaxIdle / time.Second),
			IgnoreAuthors: nonNilS(r.IgnoreAuthors), IgnoreLabels: nonNilS(r.IgnoreLabels), StackPatterns: nonNilS(r.StackPatterns)}}
	}
	add := func(name string, f fl) {
		if f.Repos == nil {
			f.Repos = []string{}
		}
		if f.UntilEvent == nil {
			f.UntilEvent = []string{}
		}
		v.Add(name, f, do(f))
	}
	oneRepo := []string{"o/a"}
	add("defaults: stream", fl{Repos: []string{"o/a", "o/b"}})
	add("until-event with two kinds", fl{Repos: oneRepo, UntilEvent: []string{"merged", "checks-green"}})
	add("once, human, baseline", fl{Repos: oneRepo, Once: true, Human: true, Baseline: true})
	add("once and stream", fl{Repos: oneRepo, Once: true, Stream: true})
	add("once and until-event", fl{Repos: oneRepo, Once: true, UntilEvent: []string{"merged"}})
	add("unknown kind (review-state-changed is reserved)", fl{Repos: oneRepo, UntilEvent: []string{"review-state-changed"}})
	add("timeout without until-event", fl{Repos: oneRepo, Timeout: str("5m")})
	add("timeout zero", fl{Repos: oneRepo, UntilEvent: []string{"merged"}, Timeout: str("0")})
	add("timeout garbage", fl{Repos: oneRepo, UntilEvent: []string{"merged"}, Timeout: str("soon")})
	add("timeout 5m with until-event", fl{Repos: oneRepo, UntilEvent: []string{"merged"}, Timeout: str("5m")})
	add("interval 9s is too short", fl{Repos: oneRepo, Interval: str("9s")})
	add("interval 10s", fl{Repos: oneRepo, Interval: str("10s")})
	add("detail-every zero", fl{Repos: oneRepo, DetailEvery: num(0)})
	add("detail-every 3", fl{Repos: oneRepo, DetailEvery: num(3)})
	add("max-idle with once", fl{Repos: oneRepo, Once: true, MaxIdle: str("5m")})
	add("max-idle with stream", fl{Repos: oneRepo, MaxIdle: str("150s")})
	add("negative pending timeout", fl{Repos: oneRepo, PendingTO: str("-1s")})
	add("pending timeout 0 disables", fl{Repos: oneRepo, PendingTO: str("0")})
	add("relative state dir", fl{Repos: oneRepo, StateDir: "state"})
	add("absolute state dir", fl{Repos: oneRepo, StateDir: "/state/dir"})
	add("bad host", fl{Repos: oneRepo, Host: "git hub.com/x"})
	add("bad repo", fl{Repos: []string{"nope"}})
	add("json without status", fl{Repos: oneRepo, JSON: true})
	add("status with once", fl{Repos: oneRepo, Status: true, Once: true})
	add("status with baseline", fl{Repos: oneRepo, Status: true, Baseline: true})
	add("status with interval", fl{Repos: oneRepo, Status: true, Interval: str("30s")})
	add("status with human", fl{Repos: oneRepo, Status: true, Human: true})
	add("path without workspace", fl{Repos: oneRepo, WorkspacePath: "/x"})
	add("missing config file", fl{MissingConfigFile: true})
	add("no source", fl{})
	add("status with state dir", fl{Status: true, JSON: true, StateDir: "/state/dir"})
	add("status from repositories", fl{Status: true, Repos: []string{"o/a", "o/b"}})
	add("status without directory or repositories", fl{Status: true})
	cfg := "schema_version = 1\nrepos = [\"wstein/crewbook\", \"wstein/sourcesteward\"]\ninterval = \"90s\"\ndetail_every = 4\nmax_open_prs = 500\n[ignore]\nauthors = [\"dependabot[bot]\"]\nlabels  = [\"no-watch\"]\n[shape]\nstack_patterns = [\"PR must target repository main\"]\n"
	add("config merged, flag repo first", fl{Repos: []string{"flag/first"}, ConfigTOML: &cfg})
	add("flags override the config", fl{ConfigTOML: &cfg, Interval: str("30s"), DetailEvery: num(2)})
	bad := "schema_version = 1\nbogus = 1\n"
	add("invalid config file is a usage error", fl{ConfigTOML: &bad})
	put(t, dir, v)
}
