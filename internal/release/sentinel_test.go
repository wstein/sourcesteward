// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Effect sentinels (FR-016, SC-005, T008): `check` must leave the repository byte for byte as it
// was, run no hook, filter, textconv or fsmonitor, start only the allowlisted Git invocations,
// and touch no network.

type entryState struct {
	mode  fs.FileMode
	size  int64
	mtime int64
	sum   string
}

// snapshot records every entry below root, .git and .jj included, with content hashes and
// modification times.
func snapshot(t *testing.T, root string) map[string]entryState {
	t.Helper()
	out := map[string]entryState{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		state := entryState{mode: info.Mode(), size: info.Size(), mtime: info.ModTime().UnixNano()}
		switch {
		case info.Mode().IsRegular():
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(raw)
			state.sum = hex.EncodeToString(sum[:])
		case info.Mode()&os.ModeSymlink != 0:
			target, _ := os.Readlink(path)
			state.sum = "link:" + target
		}
		out[rel] = state
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func category(rel string) string {
	switch {
	case rel == ".git/index":
		return "index"
	case rel == ".git/HEAD":
		return "HEAD"
	case rel == ".git/config":
		return "configuration"
	case strings.HasPrefix(rel, ".git/refs") || rel == ".git/packed-refs":
		return "refs"
	case strings.HasPrefix(rel, ".git/logs"):
		return "reflog"
	case strings.HasPrefix(rel, ".jj"):
		return "jj metadata"
	case strings.HasPrefix(rel, ".git"):
		return "git metadata"
	}
	return "working tree"
}

func differences(before, after map[string]entryState) []string {
	var out []string
	for rel, b := range before {
		a, ok := after[rel]
		switch {
		case !ok:
			out = append(out, category(rel)+": removed "+rel)
		case a != b:
			out = append(out, category(rel)+": changed "+rel)
		}
	}
	for rel := range after {
		if _, ok := before[rel]; !ok {
			out = append(out, category(rel)+": new file "+rel)
		}
	}
	sort.Strings(out)
	return out
}

// plantSentinels makes every Git extension point write a marker file outside the repository if it
// is ever run, and leaves the index stale for a modified file.
func plantSentinels(t *testing.T, dir, markers string) {
	t.Helper()
	script := func(name string) string {
		path := filepath.Join(markers, "bin-"+name)
		body := "#!/bin/sh\ntouch " + filepath.Join(markers, "fired-"+name) + "\ncat\n"
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, hook := range []string{"pre-commit", "post-checkout", "post-merge", "post-index-change", "reference-transaction", "fsmonitor-watchman", "pre-auto-gc", "post-rewrite"} {
		path := filepath.Join(dir, ".git", "hooks", hook)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\ntouch "+filepath.Join(markers, "fired-hook-"+hook)+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{
		"core.fsmonitor":       script("fsmonitor"),
		"core.pager":           script("pager"),
		"core.editor":          script("editor"),
		"core.sshCommand":      script("ssh"),
		"core.askPass":         script("askpass"),
		"filter.evil.clean":    script("clean"),
		"filter.evil.smudge":   script("smudge"),
		"filter.evil.process":  script("process"),
		"filter.evil.required": "true",
		"diff.evil.textconv":   script("textconv"),
		"diff.external":        script("external-diff"),
		"credential.helper":    script("credential"),
		"gc.auto":              "1",
	} {
		git(t, dir, "config", key, value)
	}
	write(t, dir, ".gitattributes", []byte("* filter=evil diff=evil\n"))
	write(t, dir, ".git/info/attributes", []byte("* filter=evil diff=evil\n"))
}

// makeIndexStale commits a file and rewrites it with the same content and a different time, so
// the index is stat-dirty and a command that refreshed it would rewrite it.
func makeIndexStale(t *testing.T, dir string) {
	t.Helper()
	write(t, dir, "stale.txt", []byte("one\n"))
	commitAll(t, dir)
	write(t, dir, "stale.txt", []byte("one\n"))
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "stale.txt"), future, future); err != nil {
		t.Fatal(err)
	}
}

// logGit puts a recording git in front of PATH and returns the log path.
func logGit(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "git.log")
	body := "#!/bin/sh\nprintf 'ARGS %s\\n' \"$*\" >> '" + log + "'\nprintf 'ENV %s\\n' \"$(env | grep '^GIT_' | sort | tr '\\n' ' ')\" >> '" + log + "'\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

var detectionGit = map[string]bool{"version": true, "rev-parse": true, "for-each-ref": true, "show-ref": true, "symbolic-ref": true}
var readGit = map[string]bool{"cat-file": true, "ls-tree": true}

// gitSubcommand skips the global options of an invocation.
func gitSubcommand(line string) string {
	fields := strings.Fields(line)
	for i := 0; i < len(fields); i++ {
		switch f := fields[i]; {
		case f == "-c":
			i++
		case strings.HasPrefix(f, "-"):
		default:
			return f
		}
	}
	return ""
}

// listener counts connection attempts to a local address used as every proxy.
func listener(t *testing.T) (*atomic.Int64, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback listener: %v", err)
	}
	var count atomic.Int64
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			count.Add(1)
			c.Close()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return &count, "http://" + ln.Addr().String()
}

type sentinelScenario struct {
	name   string
	files  map[string][]byte
	commit bool // check the commit instead of the working tree
	status string
	setup  func(t *testing.T, dir string)
}

func gitScenarios(t *testing.T) []sentinelScenario {
	broken := []byte("a.b = 1\n")
	return []sentinelScenario{
		{name: "valid working tree", files: map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")}, status: "ok"},
		{name: "valid commit", files: map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")}, commit: true, status: "ok"},
		{name: "profile violation", files: map[string][]byte{".stw/release.toml": broken}, status: "findings"},
		{name: "profile violation in a commit", files: map[string][]byte{".stw/release.toml": broken}, commit: true, status: "findings"},
		{name: "missing contract", files: map[string][]byte{"README": []byte("x")}, status: "refused"},
		{name: "two products", files: map[string][]byte{".stw/release.zlib.toml": exampleBytes(t, "mirror-zlib"), ".stw/release.busybox.toml": exampleBytes(t, "mirror-busybox")}, commit: true, status: "ok"},
		{name: "allowlist overlap with an existing file", files: map[string][]byte{".stw/release.toml": []byte(strings.Replace(string(exampleBytes(t, "go-cli")), `paths = ["docs/**", "README.md", "llms.txt"]`, `paths = ["scripts/**"]`, 1)), "scripts/release-package.sh": []byte("x")}, status: "findings"},
		{name: "symlinked contract", files: map[string][]byte{"real.toml": exampleBytes(t, "go-cli")}, status: "findings", setup: func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, ".stw"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("../real.toml", filepath.Join(dir, ".stw", "release.toml")); err != nil {
				t.Fatal(err)
			}
		}},
	}
}

func TestCheckHasNoEffectOnAGitRepository(t *testing.T) {
	for _, s := range gitScenarios(t) {
		t.Run(s.name, func(t *testing.T) {
			dir := repoWith(t, s.files)
			if s.setup != nil {
				s.setup(t, dir)
			}
			markers := t.TempDir()
			makeIndexStale(t, dir)
			plantSentinels(t, dir, markers)
			commit := git(t, dir, "rev-parse", "HEAD")
			// Hostile Git variables of the caller must not reach the reads.
			for name, value := range map[string]string{"GIT_DIR": "/nonexistent", "GIT_WORK_TREE": "/nonexistent", "GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.hooksPath", "GIT_CONFIG_VALUE_0": markers, "GIT_EXTERNAL_DIFF": "/bin/false", "GIT_INDEX_FILE": "/nonexistent", "GIT_NAMESPACE": "x"} {
				t.Setenv(name, value)
			}
			count, proxy := listener(t)
			for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
				t.Setenv(name, proxy)
			}
			log := logGit(t)
			before := snapshot(t, dir)
			options := CheckOptions{Path: dir}
			if s.commit {
				options.Commit = commit
			}
			result := run(t, options)
			after := snapshot(t, dir)
			if result.Status != s.status {
				t.Fatalf("status %q, want %q (%+v %q)", result.Status, s.status, result.Error, codes(result))
			}
			if d := differences(before, after); len(d) != 0 {
				t.Fatalf("check changed the repository:\n%s", strings.Join(d, "\n"))
			}
			if fired, _ := filepath.Glob(filepath.Join(markers, "fired-*")); len(fired) != 0 {
				t.Fatalf("a hook, filter, textconv or fsmonitor ran: %v", fired)
			}
			if n := count.Load(); n != 0 {
				t.Fatalf("%d network connection attempts", n)
			}
			raw, _ := os.ReadFile(log)
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			for i := 0; i+1 < len(lines); i += 2 {
				args, env := strings.TrimPrefix(lines[i], "ARGS "), strings.TrimPrefix(lines[i+1], "ENV ")
				sub := gitSubcommand(args)
				if !detectionGit[sub] && !(s.commit && readGit[sub]) {
					t.Errorf("unexpected git invocation %q", args)
				}
				if readGit[sub] {
					for _, want := range []string{"--no-replace-objects", "--no-optional-locks", "core.fsmonitor=false", "core.hooksPath=/dev/null"} {
						if !strings.Contains(args, want) {
							t.Errorf("%q lacks %s", args, want)
						}
					}
					for _, want := range []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0"} {
						if !strings.Contains(env, want) {
							t.Errorf("%q ran without %s (%s)", args, want, env)
						}
					}
					for _, bad := range []string{"GIT_DIR=", "GIT_WORK_TREE=", "GIT_CONFIG_COUNT=", "GIT_EXTERNAL_DIFF=", "GIT_INDEX_FILE=", "GIT_NAMESPACE="} {
						if strings.Contains(env, bad) {
							t.Errorf("%q inherited %s", args, bad)
						}
					}
				}
			}
			if s.commit && !strings.Contains(string(raw), "cat-file") {
				t.Error("the commit form read nothing through cat-file")
			}
			if !s.commit && (strings.Contains(string(raw), "cat-file") || strings.Contains(string(raw), "ls-tree")) {
				t.Error("the working-tree form started a read it does not need")
			}
		})
	}
}

// The sentinel machinery must be able to see an effect: a command that refreshes the index fails it.
func TestSentinelsDetectAnIndexRefresh(t *testing.T) {
	dir := repoWith(t, map[string][]byte{"a": []byte("x")})
	makeIndexStale(t, dir)
	before := snapshot(t, dir)
	git(t, dir, "update-index", "--refresh")
	d := differences(before, snapshot(t, dir))
	if !strings.Contains(strings.Join(d, "\n"), "index: changed .git/index") {
		t.Fatalf("differences %q", d)
	}
	before = snapshot(t, dir)
	write(t, dir, "new.txt", []byte("n"))
	if d := differences(before, snapshot(t, dir)); !strings.Contains(strings.Join(d, "\n"), "new file new.txt") {
		t.Fatalf("differences %q", d)
	}
}

func TestSentinelsDetectAFiredHook(t *testing.T) {
	dir := repoWith(t, map[string][]byte{"a": []byte("x")})
	markers := t.TempDir()
	plantSentinels(t, dir, markers)
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir, cmd.Env = dir, gitEnv()
	_ = cmd.Run() // the required filter fails, which is how it shows it ran
	if fired, _ := filepath.Glob(filepath.Join(markers, "fired-*")); len(fired) == 0 {
		t.Fatal("the planted hooks and filters do not fire, so the sentinel proves nothing")
	}
}

func TestCheckCancellationLeavesNoProcessBehind(t *testing.T) {
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	commit := commitAll(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Check(ctx, CheckOptions{Path: dir, Commit: commit})
	if r.Status != "error" || r.Error.Code != "canceled" {
		t.Fatalf("%+v", r)
	}
	_ = fmt.Sprint(r)
}
