// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package inspection

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hostile plants every executable hook point Git offers in one repository: hooks, an fsmonitor, a hooks
// path, clean/smudge/process filters selected by attributes, an external diff, a textconv, a credential
// helper, an editor, a pager, an ssh command, aliases and an included configuration file. Every planted
// command appends its name to one log; a repository audit must leave that log empty.
type hostile struct {
	log string
	dir string
}

func newHostile(t *testing.T) hostile {
	t.Helper()
	dir := pinsRealDir(t)
	return hostile{log: filepath.Join(dir, "executed.log"), dir: dir}
}

// script writes an executable that records its own name and exits 0, and returns its path.
func (h hostile) script(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(h.dir, "bin-"+name)
	writeFile(t, p, "#!/bin/sh\nprintf '%s\\n' '"+name+"' >> '"+h.log+"'\nexit 0\n")
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func (h hostile) executed() []string {
	b, err := os.ReadFile(h.log)
	if err != nil || len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

var hostileHooks = []string{"pre-commit", "prepare-commit-msg", "commit-msg", "post-commit", "post-checkout", "post-merge", "post-rewrite", "pre-push", "pre-rebase", "post-index-change", "reference-transaction", "fsmonitor-watchman", "pre-auto-gc", "post-update", "push-to-checkout"}

// plant configures hostile behaviour in the repository at gitDir (worktree workTree). It writes the
// repository's own config file and hooks directory, exactly where untrusted content would.
func (h hostile) plant(t *testing.T, gitDir, workTree string) {
	t.Helper()
	cfg := filepath.Join(gitDir, "config")
	set := func(key, value string) {
		t.Helper()
		git(t, h.dir, "config", "--file", cfg, key, value)
	}
	hooks := filepath.Join(gitDir, "hooks")
	for _, name := range hostileHooks {
		p := filepath.Join(hooks, name)
		writeFile(t, p, "#!/bin/sh\nprintf '%s\\n' 'hook:"+name+"' >> '"+h.log+"'\nexit 0\n")
		os.Chmod(p, 0o755)
	}
	hooksPath := filepath.Join(h.dir, "evil-hooks-"+filepath.Base(gitDir))
	for _, name := range hostileHooks {
		p := filepath.Join(hooksPath, name)
		writeFile(t, p, "#!/bin/sh\nprintf '%s\\n' 'hookspath:"+name+"' >> '"+h.log+"'\nexit 0\n")
		os.Chmod(p, 0o755)
	}
	set("core.hooksPath", hooksPath)
	set("core.fsmonitor", h.script(t, "fsmonitor"))
	set("core.editor", h.script(t, "editor"))
	set("core.pager", h.script(t, "pager"))
	set("core.sshCommand", h.script(t, "ssh"))
	set("core.askPass", h.script(t, "askpass"))
	set("credential.helper", "!"+h.script(t, "credential"))
	set("diff.external", h.script(t, "external-diff"))
	set("diff.evil.textconv", h.script(t, "textconv"))
	set("diff.evil.command", h.script(t, "diff-driver"))
	set("filter.evil.clean", h.script(t, "filter-clean"))
	set("filter.evil.smudge", h.script(t, "filter-smudge"))
	set("filter.evil.process", h.script(t, "filter-process"))
	set("filter.evil.required", "true")
	set("merge.evil.driver", h.script(t, "merge-driver"))
	// Aliases cannot replace built-ins; they are planted for every command the audit runs anyway.
	for _, a := range []string{"rev-parse", "ls-tree", "ls-files", "config", "cat-file", "merge-base", "rev-list", "for-each-ref", "symbolic-ref", "diff-index"} {
		set("alias."+a, "!"+h.script(t, "alias-"+a))
	}
	included := filepath.Join(h.dir, "included-"+filepath.Base(gitDir)+".cfg")
	writeFile(t, included, "[core]\n\tfsmonitor = "+h.script(t, "included-fsmonitor")+"\n")
	set("include.path", included)
	// Attributes select the filter and diff driver for every path, from the worktree and from info/.
	writeFile(t, filepath.Join(gitDir, "info", "attributes"), "* filter=evil diff=evil merge=evil\n")
	if workTree != "" {
		writeFile(t, filepath.Join(workTree, ".gitattributes"), "* filter=evil diff=evil merge=evil\n")
	}
}

// statDirty makes every tracked file stat-dirty without changing its content: a status would compare the
// content, running the clean filter. One file also really differs for the diff-driver control.
func statDirty(t *testing.T, workTree string) {
	t.Helper()
	filepath.WalkDir(workTree, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() && !strings.Contains(p, "/.git") {
			old := time.Now().Add(-time.Hour)
			os.Chtimes(p, old, old)
		}
		return nil
	})
}

// hostileGlobalConfig puts the same planted behaviour in the user's global configuration.
func (h hostile) hostileGlobalConfig(t *testing.T) {
	t.Helper()
	home := os.Getenv("HOME")
	writeFile(t, filepath.Join(home, ".gitconfig"), "[core]\n\tfsmonitor = "+h.script(t, "global-fsmonitor")+"\n\thooksPath = "+filepath.Join(h.dir, "evil-hooks-global")+"\n[filter \"evil\"]\n\tclean = "+h.script(t, "global-clean")+"\n[credential]\n\thelper = !"+h.script(t, "global-credential")+"\n")
	for _, name := range hostileHooks {
		p := filepath.Join(h.dir, "evil-hooks-global", name)
		writeFile(t, p, "#!/bin/sh\nprintf '%s\\n' 'global-hook:"+name+"' >> '"+h.log+"'\n")
		os.Chmod(p, 0o755)
	}
}

// allowlisted runs every command of the normative process allowlist (research.md) against one validated
// submodule, plus the registry and remote reads, through the production runner.
func allowlisted(t *testing.T, run *pinsRunner, repo pinsGitRepo, pin string) {
	t.Helper()
	calls := [][]string{
		{"rev-parse", "HEAD"},
		{"symbolic-ref", "-q", "HEAD"},
		{"cat-file", "-e", pin + "^{commit}"},
		{"merge-base", pin, "HEAD"},
		{"rev-list", "--count", "--max-count=10001", pin + "..HEAD"},
		{"rev-list", "--count", "--max-count=10001", "HEAD.." + pin},
		{"for-each-ref", "--contains", pin, "--count=64", "--format=%(refname)%09%(symref)", "refs/remotes/"},
		{"ls-tree", "HEAD", ".gitmodules"},
		{"diff-index", "--cached", "--no-ext-diff", "--no-textconv", "HEAD"},
		{"ls-files", "--stage", "--debug", "-z"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	}
	for _, args := range calls {
		out, err := run.run(pinsCall{Repo: repo, Args: args})
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if out.Exit > 1 {
			t.Fatalf("%v exited %d: %s", args, out.Exit, out.Stderr)
		}
	}
	if _, err := pinsReadRemotes(run, repo); err != nil {
		t.Fatal(err)
	}
}

func TestPinsHostileConfigurationNeverExecutes(t *testing.T) {
	f := newPinsFixture(t)
	h := newHostile(t)
	member := filepath.Join(f.Super, "member")
	memberGit := filepath.Join(f.Super, ".git", "modules", "member")
	// A tracked file that really differs in the submodule, and a stat-dirty one in both repositories.
	writeFile(t, filepath.Join(member, "file.txt"), "changed\n")
	h.plant(t, memberGit, member)
	h.plant(t, filepath.Join(f.Super, ".git"), f.Super)
	h.hostileGlobalConfig(t)
	statDirty(t, f.Super)
	statDirty(t, member)

	// Control: the same repository with ordinary Git options does run planted commands, so an empty log
	// below means the audit prevented them and not that the fixture is inert.
	for _, args := range [][]string{{"status"}, {"diff", "HEAD"}, {"commit", "--allow-empty", "-qm", "x"}, {"add", "-A"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, "git", args...)
		c.WaitDelay = time.Second
		c.Dir = member
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e")
		_ = c.Run()
	}
	ran := strings.Join(h.executed(), " ")
	for _, want := range []string{"fsmonitor", "filter-process", "hookspath:pre-commit"} {
		if !strings.Contains(ran, want) {
			t.Fatalf("control did not run %q (ran: %q): the fixture is not hostile enough", want, ran)
		}
	}
	os.Remove(h.log)
	statDirty(t, f.Super)
	statDirty(t, member)

	run := newPinsRunner(context.Background())
	l, err := newPinsSuperLayout(run, f.Super)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := pinsEnumerate(run, l)
	if err != nil {
		t.Fatal(err)
	}
	e := entryAt(t, reg, "member")
	res, refusal, err := newPinsSafety(l, run).resolve(pinsSubmoduleSpec{Path: "member", Name: *e.Name})
	if err != nil || refusal != nil || res.Dirs == nil {
		t.Fatalf("hostile configuration is data and must not refuse a confined submodule: %v %v", refusal, err)
	}
	allowlisted(t, run, pinsGitRepo{GitDir: res.Dirs.GitDir, WorkTree: res.Dirs.WorkTree}, *e.HeadCommit)
	allowlisted(t, run, pinsGitRepo{GitDir: l.GitDir, WorkTree: l.WorkTree}, gitIn(t, f.Super, "rev-parse", "HEAD"))
	if got := h.executed(); len(got) != 0 {
		t.Fatalf("planted commands ran: %v", got)
	}
}

func TestPinsHostileSubmodulesAreRefusedAndOthersStillReported(t *testing.T) {
	f := newPinsFixture(t)
	h := newHostile(t)
	elsewhere := filepath.Join(f.Root, "elsewhere")
	for _, name := range []string{"pointer", "linked", "worktreecfg", "inplace-alt"} {
		git(t, f.Super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", f.Sub, name)
	}
	git(t, f.Super, "commit", "-qm", "more submodules")
	pin := f.Pin

	// 1. A .git pointer to a hostile repository outside the superproject.
	outside := filepath.Join(f.Root, "outside-gitdir")
	git(t, f.Root, "clone", "-q", "--bare", f.Sub, outside)
	h.plant(t, outside, "")
	writeFile(t, filepath.Join(f.Super, "pointer", ".git"), "gitdir: "+outside+"\n")
	// 2. A symlinked submodule directory leading to a repository with a valid-looking .git.
	mkdirs(t, elsewhere)
	git(t, f.Root, "clone", "-q", f.Sub, filepath.Join(elsewhere, "clone"))
	h.plant(t, filepath.Join(elsewhere, "clone", ".git"), filepath.Join(elsewhere, "clone"))
	os.RemoveAll(filepath.Join(f.Super, "linked"))
	if err := os.Symlink(filepath.Join(elsewhere, "clone"), filepath.Join(f.Super, "linked")); err != nil {
		t.Fatal(err)
	}
	// 3. A hostile core.worktree in the submodule's own Git directory.
	git(t, f.Root, "config", "--file", filepath.Join(f.Super, ".git", "modules", "worktreecfg", "config"), "core.worktree", elsewhere)
	// 4. An in-place .git directory whose alternates lead outside.
	git(t, f.Root, "clone", "-q", f.Sub, filepath.Join(f.Super, "inplace-alt-tmp"))
	os.RemoveAll(filepath.Join(f.Super, "inplace-alt"))
	os.Rename(filepath.Join(f.Super, "inplace-alt-tmp"), filepath.Join(f.Super, "inplace-alt"))
	writeFile(t, filepath.Join(f.Super, "inplace-alt", ".git", "objects", "info", "alternates"), outside+"/objects\n")
	h.hostileGlobalConfig(t)

	gitLog, _ := pinsSentinels(t)
	run := newPinsRunner(context.Background())
	l, err := newPinsSuperLayout(run, f.Super)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := pinsEnumerate(run, l)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := pinsResolveAll(newPinsSafety(l, run), reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 5 {
		t.Fatalf("every submodule is reported: %d", len(resolved))
	}
	byPath := map[string]pinsResolvedEntry{}
	for _, r := range resolved {
		byPath[r.Entry.Path] = r
	}
	for _, p := range []string{"pointer", "linked", "worktreecfg", "inplace-alt"} {
		r := byPath[p]
		if r.Refusal == nil || r.Refusal.Code != "submodule-path-unsafe" || r.Resolved.Dirs != nil {
			t.Errorf("%s must be refused as unsafe: %+v", p, r)
		}
	}
	ok := byPath["member"]
	if ok.Refusal != nil || ok.Resolved.Dirs == nil {
		t.Fatalf("the healthy submodule must still be reported: %+v", ok)
	}
	allowlisted(t, run, pinsGitRepo{GitDir: ok.Resolved.Dirs.GitDir, WorkTree: ok.Resolved.Dirs.WorkTree}, pin)
	if got := h.executed(); len(got) != 0 {
		t.Fatalf("planted commands ran: %v", got)
	}
	for _, line := range gitLog() {
		for _, bad := range []string{outside, elsewhere, "pointer", "linked", "worktreecfg", "inplace-alt"} {
			if strings.Contains(line, bad) {
				// worktreecfg and inplace-alt reach the config --file read only after path checks passed
				// and before the core.worktree comparison; only escaped locations must never be named.
				if bad == outside || bad == elsewhere || bad == "pointer" || bad == "linked" {
					t.Errorf("a Git process was started against a refused location (%s): %s", bad, line)
				}
			}
		}
	}
}
