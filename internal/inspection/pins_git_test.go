// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func registryOf(t *testing.T, super string) pinsRegistry {
	t.Helper()
	run := newPinsRunner(context.Background())
	l, err := newPinsSuperLayout(run, super)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := pinsEnumerate(run, l)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func entryAt(t *testing.T, reg pinsRegistry, path string) pinsEntry {
	t.Helper()
	for _, e := range reg.Entries {
		if e.Path == path {
			return e
		}
	}
	t.Fatalf("no entry %q in %d entries", path, len(reg.Entries))
	return pinsEntry{}
}

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// gitStdin runs a fixture Git command with standard input (the fixture helper has none).
func gitStdin(t *testing.T, dir, input string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-c", "maintenance.auto=false", "-c", "gc.auto=0"}, args...)...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e.invalid", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e.invalid")
	c.Stdin = strings.NewReader(input)
	b, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s %v", args, b, err)
	}
	return strings.TrimSpace(string(b))
}

// gitHash stores text as a blob and returns its object name.
func gitHash(t *testing.T, dir, text string) string {
	t.Helper()
	c := exec.Command("git", "hash-object", "-w", "--stdin")
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	c.Stdin = strings.NewReader(text)
	b, err := c.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// newBareSuper is a superproject without submodules, a pinned commit to point gitlinks at and nothing else.
func newBareSuper(t *testing.T) (dir, oid string) {
	t.Helper()
	pinsIsolateHome(t)
	dir = pinsRealDir(t)
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(dir, "top.txt"), "top\n")
	git(t, dir, "add", "top.txt")
	git(t, dir, "commit", "-qm", "one")
	return dir, gitIn(t, dir, "rev-parse", "HEAD")
}

func gitlink(t *testing.T, dir, oid string, paths ...string) {
	t.Helper()
	var in bytes.Buffer
	for _, p := range paths {
		fmt.Fprintf(&in, "160000 %s\t%s\x00", oid, p)
	}
	gitStdin(t, dir, in.String(), "update-index", "--add", "-z", "--index-info")
}

func TestPinsRegistryRegisteredSubmodule(t *testing.T) {
	f := newPinsFixture(t)
	reg := registryOf(t, f.Super)
	if reg.HeadUnborn || str(reg.HeadCommit) == "<nil>" || len(reg.Entries) != 1 || reg.Truncated || reg.Total != 1 {
		t.Fatalf("%+v", reg)
	}
	e := entryAt(t, reg, "member")
	if !e.PathUTF8 || !e.GitlinkInHead || !e.GitlinkInIndex || str(e.HeadCommit) != f.Pin || str(e.IndexCommit) != f.Pin ||
		str(e.Name) != "member" || !e.GitmodulesEntry || str(e.URL) != f.Sub || e.Active == nil || !*e.Active || e.OrphanGitlink || e.EntryWithoutGitlink {
		t.Fatalf("%+v", e)
	}
	g := reg.Gitmodules
	if g.Source != "head" || g.HeadKind != "regular" || g.Worktree != "same" || g.StagedDiffers || g.Modified || g.RegistrationUnknown {
		t.Fatalf("%+v", g)
	}
}

func TestPinsRegistryStagedPinDiffers(t *testing.T) {
	f := newPinsFixture(t)
	member := filepath.Join(f.Super, "member")
	writeFile(t, filepath.Join(member, "file.txt"), "two\n")
	git(t, member, "commit", "-qam", "two")
	git(t, f.Super, "add", "member")
	e := entryAt(t, registryOf(t, f.Super), "member")
	if str(e.HeadCommit) != f.Pin || str(e.IndexCommit) == f.Pin || str(e.IndexCommit) != gitIn(t, member, "rev-parse", "HEAD") {
		t.Fatalf("%+v", e)
	}
}

func TestPinsRegistryUnbornSuperprojectUsesTheStagedBlob(t *testing.T) {
	f := newPinsFixture(t)
	d := pinsRealDir(t)
	git(t, d, "init", "-q", "-b", "main")
	git(t, d, "-c", "protocol.file.allow=always", "submodule", "add", "-q", f.Sub, "member")
	reg := registryOf(t, d)
	e := entryAt(t, reg, "member")
	if !reg.HeadUnborn || reg.HeadCommit != nil || e.HeadCommit != nil || e.GitlinkInHead || !e.GitlinkInIndex || str(e.Name) != "member" || !e.GitmodulesEntry || e.OrphanGitlink {
		t.Fatalf("%+v %+v", reg, e)
	}
	if reg.Gitmodules.Source != "staged" || reg.Gitmodules.HeadKind != "unborn" || reg.Gitmodules.RegistrationUnknown {
		t.Fatalf("%+v", reg.Gitmodules)
	}
}

func TestPinsRegistryHeadGitmodulesNotRegularMeansUnknownRegistration(t *testing.T) {
	for _, kind := range []string{"symlink", "gitlink"} {
		t.Run(kind, func(t *testing.T) {
			d, oid := newBareSuper(t)
			outside := filepath.Join(pinsRealDir(t), "elsewhere-gitmodules")
			writeFile(t, outside, "[submodule \"evil\"]\n\tpath = evil\n\turl = https://example.invalid/x\n")
			if kind == "symlink" {
				// Git refuses to add a symlinked .gitmodules to the index, but a hostile repository can still
				// carry one in its HEAD tree: plant that commit with tree plumbing.
				link := gitHash(t, d, outside)
				top := gitIn(t, d, "rev-parse", "HEAD:top.txt")
				tree := gitStdin(t, d, "100644 blob "+top+"\ttop.txt\n120000 blob "+link+"\t.gitmodules\n160000 commit "+oid+"\treal\n", "mktree")
				commit := gitIn(t, d, "commit-tree", "-p", "HEAD", "-m", "symlinked registry", tree)
				git(t, d, "update-ref", "refs/heads/main", commit)
			} else {
				gitlink(t, d, oid, ".gitmodules")
				gitlink(t, d, oid, "real")
				git(t, d, "commit", "-qm", "registry is "+kind)
			}
			reg := registryOf(t, d)
			e := entryAt(t, reg, "real")
			g := reg.Gitmodules
			if !g.RegistrationUnknown || !g.Modified || g.HeadKind != kind || g.Worktree == "same" {
				t.Fatalf("%+v", g)
			}
			if e.OrphanGitlink || e.Name != nil || e.GitmodulesEntry {
				t.Fatalf("registration is unknown, no gitlink-without-entry may be claimed: %+v", e)
			}
			for _, x := range reg.Entries {
				if x.Path == "evil" || str(x.Name) == "evil" {
					t.Fatalf("the symlink target was followed: %+v", x)
				}
			}
		})
	}
}

func TestPinsRegistryOrphanGitlinkAndEntryWithoutGitlink(t *testing.T) {
	d, oid := newBareSuper(t)
	gitlink(t, d, oid, "orphan")
	git(t, d, "config", "-f", ".gitmodules", "submodule.ghost.path", "ghost")
	git(t, d, "config", "-f", ".gitmodules", "submodule.ghost.url", "https://example.invalid/ghost.git")
	git(t, d, "add", ".gitmodules")
	git(t, d, "commit", "-qm", "half registered")
	reg := registryOf(t, d)
	o, g := entryAt(t, reg, "orphan"), entryAt(t, reg, "ghost")
	if !o.OrphanGitlink || o.GitmodulesEntry || o.Name != nil || !o.GitlinkInHead {
		t.Fatalf("%+v", o)
	}
	if !g.EntryWithoutGitlink || !g.GitmodulesEntry || str(g.Name) != "ghost" || g.GitlinkInHead || g.GitlinkInIndex || g.HeadCommit != nil || str(g.URL) != "https://example.invalid/ghost.git" {
		t.Fatalf("%+v", g)
	}
	if len(reg.Entries) != 2 {
		t.Fatalf("each path is reported once: %+v", reg.Entries)
	}
}

func TestPinsRegistryActiveState(t *testing.T) {
	f := newPinsFixture(t)
	active := func() *bool { return entryAt(t, registryOf(t, f.Super), "member").Active }
	if a := active(); a == nil || !*a {
		t.Fatal("initialized submodule is active")
	}
	git(t, f.Super, "config", "submodule.member.active", "false")
	if a := active(); a == nil || *a {
		t.Fatal("explicitly inactive")
	}
	git(t, f.Super, "config", "--unset", "submodule.member.active")
	git(t, f.Super, "config", "--unset", "submodule.member.url")
	if a := active(); a == nil || *a {
		t.Fatal("never initialized and not selected is inactive")
	}
	git(t, f.Super, "config", "submodule.active", ".")
	if a := active(); a != nil {
		t.Fatal("a submodule.active pathspec is not evaluated: unknown")
	}
}

func TestPinsRegistryWorktreeGitmodulesNeverFollowedAndDifferenceReported(t *testing.T) {
	f := newPinsFixture(t)
	gm := filepath.Join(f.Super, ".gitmodules")
	orig, _ := os.ReadFile(gm)
	check := func(want string, modified bool) pinsRegistry {
		t.Helper()
		reg := registryOf(t, f.Super)
		if reg.Gitmodules.Worktree != want || reg.Gitmodules.Modified != modified || reg.Gitmodules.Source != "head" {
			t.Fatalf("want %s modified=%v: %+v", want, modified, reg.Gitmodules)
		}
		return reg
	}
	check("same", false)
	// A different URL in the worktree: registration still comes from the HEAD blob.
	writeFile(t, gm, strings.ReplaceAll(string(orig), f.Sub, "https://example.invalid/changed.git"))
	reg := check("different", true)
	if str(entryAt(t, reg, "member").URL) != f.Sub {
		t.Fatal("the worktree file must not replace the HEAD registration")
	}
	// Whitespace and comment changes are not a registration difference.
	writeFile(t, gm, "# comment\n"+strings.ReplaceAll(string(orig), "\t", "    "))
	check("same", false)
	// A symlink is reported and never followed, even when its target holds identical bytes.
	target := filepath.Join(f.Root, "same-bytes")
	writeFile(t, target, string(orig))
	rmAll(t, gm)
	if err := os.Symlink(target, gm); err != nil {
		t.Fatal(err)
	}
	check("symlink", true)
	// Absent and unreadable.
	rmAll(t, gm)
	check("absent", true)
	if os.Geteuid() != 0 {
		writeFile(t, gm, string(orig))
		mustOS(t, os.Chmod(gm, 0))
		check("unreadable", true)
		mustOS(t, os.Chmod(gm, 0o600))
	}
	// A staged difference is reported against the HEAD blob.
	writeFile(t, gm, string(orig)+"[submodule \"extra\"]\n\tpath = extra\n\turl = https://example.invalid/e\n")
	git(t, f.Super, "add", ".gitmodules")
	reg = registryOf(t, f.Super)
	if !reg.Gitmodules.StagedDiffers || !reg.Gitmodules.Modified || reg.Gitmodules.Worktree != "different" {
		t.Fatalf("%+v", reg.Gitmodules)
	}
	for _, e := range reg.Entries {
		if e.Path == "extra" {
			t.Fatalf("a path registered only in the staged blob is not an entry: %+v", e)
		}
	}
}

func TestPinsRegistryUnusualNames(t *testing.T) {
	d, oid := newBareSuper(t)
	paths := map[string]string{ // name -> path
		"space name":  "dir with space/sub",
		"newline":     "nl\nx",
		"-dash":       "-dash",
		"Case":        "Case",
		"case":        "case",
		"quote\"name": "quote\"path",
		"ünï":         "ünï/∂",
	}
	var all []string
	for name, path := range paths {
		git(t, d, "config", "-f", ".gitmodules", "submodule."+name+".path", path)
		git(t, d, "config", "-f", ".gitmodules", "submodule."+name+".url", "https://example.invalid/"+strings.ReplaceAll(name, " ", "-"))
		all = append(all, path)
	}
	gitlink(t, d, oid, all...)
	git(t, d, "add", ".gitmodules")
	git(t, d, "commit", "-qm", "names")
	reg := registryOf(t, d)
	if len(reg.Entries) != len(paths) {
		t.Fatalf("%d entries: %+v", len(reg.Entries), reg.Entries)
	}
	got := []string{}
	for _, e := range reg.Entries {
		got = append(got, e.Path)
		if e.OrphanGitlink || e.EntryWithoutGitlink || !e.GitlinkInHead || !e.GitmodulesEntry {
			t.Errorf("%q: %+v", e.Path, e)
		}
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("entries are not sorted by path bytes: %q", got)
	}
	for name, path := range paths {
		if e := entryAt(t, reg, path); str(e.Name) != name {
			t.Errorf("path %q: name %q, want %q", path, str(e.Name), name)
		}
	}
}

func TestPinsRegistryMoreThanTheLimitIsTruncatedAndCounted(t *testing.T) {
	d, oid := newBareSuper(t)
	var paths []string
	for i := 0; i < 260; i++ {
		paths = append(paths, fmt.Sprintf("m%03d", i))
	}
	gitlink(t, d, oid, paths...)
	git(t, d, "commit", "-qm", "many")
	reg := registryOf(t, d)
	if !reg.Truncated || reg.Total != 260 || len(reg.Entries) != 256 || reg.Entries[0].Path != "m000" || reg.Entries[255].Path != "m255" {
		t.Fatalf("truncated=%v total=%d entries=%d", reg.Truncated, reg.Total, len(reg.Entries))
	}
}

func TestPinsRegistryRetainedBudgetIsAPinsLimit(t *testing.T) {
	f := newPinsFixture(t)
	run := newPinsRunner(context.Background())
	l, err := newPinsSuperLayout(run, f.Super)
	if err != nil {
		t.Fatal(err)
	}
	run.budget.limit = 60
	_, err = pinsEnumerate(run, l)
	var limit *pinsLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("want a pins limit, got %v", err)
	}
}

// pinsAllowlist is the normative process allowlist of specs/021-submodule-pin-audit/research.md, as the
// argument shape after the isolation prefix and the explicit --git-dir/--work-tree. A path after --file is
// shown as <PATH> and the symbolic ref as <REF>.
var pinsAllowlist = []string{
	"rev-parse --path-format=absolute --git-dir --git-path modules",
	"config --no-includes --bool --get extensions.submodulePathConfig",
	"rev-parse --verify --quiet HEAD",
	"symbolic-ref -q HEAD",
	"for-each-ref --format=%(refname) -- <REF>",
	"ls-tree -r -z --full-tree <OID>",
	"ls-files --stage -z",
	"cat-file blob <OID>:.gitmodules",
	"cat-file blob :.gitmodules",
	"config --file - --null --list --no-includes",
	"config --local --null --list --no-includes",
	"config --file <PATH> --null --list --no-includes",
	// WI3 evidence invocations (research.md), after the registry and validation ones above.
	"cat-file -e <OID>^{commit}",
	"merge-base <OID> <OID>",
	"rev-list --count --max-count=10001 <OID>..<OID> --",
	"for-each-ref --contains <OID> --count=64 --format=%(refname)%09%(symref)%00 refs/remotes/",
	"ls-tree <OID> .gitmodules",
	"diff-index --cached --no-ext-diff --no-textconv -z <OID> --",
	"ls-files --stage --debug -z",
	"ls-files --others --exclude-standard -z",
	"symbolic-ref -q refs/remotes/<REMOTE>/HEAD",
	"rev-parse --verify --quiet refs/remotes/<REMOTE>/<BRANCH>^{commit}",
}

var (
	pinsOIDToken      = regexp.MustCompile(`[0-9a-f]{64}|[0-9a-f]{40}`)
	pinsRemoteHeadTok = regexp.MustCompile(`^refs/remotes/.+/HEAD$`)
	pinsRemoteTipTok  = regexp.MustCompile(`^refs/remotes/[^ ]+/[^ /]+\^\{commit\}$`)
)

// pinsShape reduces one recorded invocation to its allowlist shape and reports whether the explicit
// repository flags were present.
func pinsShape(t *testing.T, line string) (shape string, explicit bool) {
	t.Helper()
	prefix := strings.Join(pinsIsolation(), " ") + " "
	rest, ok := strings.CutPrefix(line, prefix)
	if !ok {
		t.Fatalf("invocation does not begin with the isolation prefix: %s", line)
	}
	f := strings.Fields(rest)
	if len(f) > 0 && strings.HasPrefix(f[0], "--git-dir=") {
		explicit, f = true, f[1:]
		if len(f) > 0 && strings.HasPrefix(f[0], "--work-tree=") {
			f = f[1:]
		}
	}
	for i := range f {
		if f[i] == "--file" && i+1 < len(f) && f[i+1] != "-" {
			f[i+1] = "<PATH>"
		}
		if f[i] == "--" && i+1 < len(f) {
			f[i+1] = "<REF>"
		}
		f[i] = pinsOIDToken.ReplaceAllString(f[i], "<OID>")
		switch {
		case pinsRemoteHeadTok.MatchString(f[i]):
			f[i] = "refs/remotes/<REMOTE>/HEAD"
		case pinsRemoteTipTok.MatchString(f[i]):
			f[i] = "refs/remotes/<REMOTE>/<BRANCH>^{commit}"
		}
	}
	return strings.Join(f, " "), explicit
}

func requireAllowlisted(t *testing.T, lines []string, wantSeen ...string) {
	t.Helper()
	allowed := map[string]bool{}
	for _, a := range pinsAllowlist {
		allowed[a] = true
	}
	seen := map[string]bool{}
	for _, line := range lines {
		if line == "" {
			continue
		}
		shape, explicit := pinsShape(t, line)
		if !allowed[shape] {
			t.Errorf("invocation outside the process allowlist: %s", shape)
		}
		seen[shape] = true
		if !explicit && shape != pinsAllowlist[0] && shape != pinsAllowlist[11] {
			t.Errorf("no explicit --git-dir: %s", line)
		}
	}
	for _, w := range wantSeen {
		if !seen[w] {
			t.Errorf("expected invocation never ran: %s (saw %v)", w, seen)
		}
	}
}

func TestPinsRegistryStartsOnlyAllowlistedGitCommands(t *testing.T) {
	f := newPinsFixture(t)
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
	if err != nil || resolved[0].Resolved.Dirs == nil {
		t.Fatalf("%+v %v", resolved, err)
	}
	if _, err := pinsReadRemotes(run, pinsGitRepo{GitDir: resolved[0].Resolved.Dirs.GitDir, WorkTree: resolved[0].Resolved.Dirs.WorkTree}); err != nil {
		t.Fatal(err)
	}
	requireAllowlisted(t, gitLog(), pinsAllowlist[0], pinsAllowlist[1], pinsAllowlist[2], pinsAllowlist[5], pinsAllowlist[6], pinsAllowlist[7],
		pinsAllowlist[9], pinsAllowlist[10], pinsAllowlist[11])
}

func TestPinsRegistryUnbornStartsOnlyAllowlistedGitCommands(t *testing.T) {
	pinsIsolateHome(t)
	d := pinsRealDir(t)
	git(t, d, "init", "-q", "-b", "main")
	gitLog, _ := pinsSentinels(t)
	registryOf(t, d)
	requireAllowlisted(t, gitLog(), pinsAllowlist[2], pinsAllowlist[3], pinsAllowlist[4])
}

func TestPinsRegistryReadsConfiguredGitDirWithSubmodulePathConfig(t *testing.T) {
	requireSubmodulePathConfig(t)
	f := newPinsFixture(t)
	git(t, f.Super, "config", "core.repositoryformatversion", "1")
	git(t, f.Super, "config", "extensions.submodulePathConfig", "true")
	git(t, f.Super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", "--name", "x/y", f.Sub, "slashy")
	git(t, f.Super, "commit", "-qm", "slashy")
	e := entryAt(t, registryOf(t, f.Super), "slashy")
	if e.ConfiguredGitDir == nil {
		t.Skip("this Git does not store submodule.<name>.gitdir (before 2.56): exercised on the current Git only")
	}
	if str(e.Name) != "x/y" || *e.ConfiguredGitDir != ".git/modules/x%2fy" {
		t.Fatalf("%+v", e)
	}
	// And the registry feeds the safety validation end to end.
	run := newPinsRunner(context.Background())
	l, _ := newPinsSuperLayout(run, f.Super)
	res, refusal, err := newPinsSafety(l, run).resolve(pinsSubmoduleSpec{Path: e.Path, Name: *e.Name, ConfiguredGitDir: e.ConfiguredGitDir})
	if err != nil || refusal != nil || res.Dirs == nil || res.Dirs.GitDir != filepath.Join(l.ModulesDir, "x%2fy") {
		t.Fatalf("%+v %v %v", res, refusal, err)
	}
}

func enumerateErr(t *testing.T, super string) error {
	t.Helper()
	run := newPinsRunner(context.Background())
	l, err := newPinsSuperLayout(run, super)
	if err != nil {
		return err
	}
	_, err = pinsEnumerate(run, l)
	return err
}

func TestPinsRegistryUnbornNeedsAMissingBranchRefNotACorruptOne(t *testing.T) {
	d := pinsRealDir(t)
	pinsIsolateHome(t)
	git(t, d, "init", "-q", "-b", "main")
	if err := enumerateErr(t, d); err != nil {
		t.Fatalf("a missing branch ref is unborn: %v", err)
	}
	ref := filepath.Join(d, ".git", "refs", "heads", "main")
	writeFile(t, ref, "garbage\n")
	if err := enumerateErr(t, d); err == nil {
		t.Fatal("a corrupt branch ref is not an unborn HEAD")
	}
	rmAll(t, ref)
	// A detached HEAD that does not resolve is corruption too.
	writeFile(t, filepath.Join(d, ".git", "HEAD"), "garbage\n")
	if err := enumerateErr(t, d); err == nil {
		t.Fatal("an unreadable HEAD is not an unborn HEAD")
	}
}

func TestPinsRegistryUnbornWithUnreadableStagedGitmodulesIsUnknownRegistration(t *testing.T) {
	for _, kind := range []string{"gitlink", "conflict"} {
		t.Run(kind, func(t *testing.T) {
			pinsIsolateHome(t)
			d := pinsRealDir(t)
			git(t, d, "init", "-q", "-b", "main")
			oid := gitHash(t, d, "x")
			if kind == "gitlink" {
				gitStdin(t, d, "160000 "+oid+"\t.gitmodules\x00", "update-index", "--add", "-z", "--index-info")
			} else {
				gitStdin(t, d, "100644 "+oid+" 1\t.gitmodules\x00100644 "+oid+" 2\t.gitmodules\x00", "update-index", "--add", "-z", "--index-info")
			}
			gitlink(t, d, "1111111111111111111111111111111111111111", "real")
			reg := registryOf(t, d)
			if !reg.Gitmodules.RegistrationUnknown || entryAt(t, reg, "real").OrphanGitlink {
				t.Fatalf("%+v %+v", reg.Gitmodules, reg.Entries)
			}
			if kind == "conflict" && !reg.Gitmodules.StagedDiffers {
				t.Fatal("a conflicted staged .gitmodules differs")
			}
		})
	}
}

func TestPinsRegistryStagedOnlyRegistrationRules(t *testing.T) {
	f := newPinsFixture(t)
	// A staged submodule addition (gitlink and .gitmodules both staged) is not an orphan.
	git(t, f.Super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", f.Sub, "added")
	reg := registryOf(t, f.Super)
	e := entryAt(t, reg, "added")
	if e.OrphanGitlink || !e.StagedGitmodulesEntry || e.GitmodulesEntry || !e.GitlinkInIndex || e.GitlinkInHead || !reg.Gitmodules.StagedDiffers {
		t.Fatalf("%+v", e)
	}
	// A path registered only in the staged blob with no gitlink anywhere is dropped and not counted.
	git(t, f.Super, "config", "-f", ".gitmodules", "submodule.phantom.path", "phantom")
	git(t, f.Super, "config", "-f", ".gitmodules", "submodule.phantom.url", "https://example.invalid/p")
	git(t, f.Super, "add", ".gitmodules")
	reg = registryOf(t, f.Super)
	for _, x := range reg.Entries {
		if x.Path == "phantom" {
			t.Fatalf("a staged-only registration without a gitlink must be dropped: %+v", x)
		}
	}
	if reg.Total != 2 || len(reg.Entries) != 2 {
		t.Fatalf("total %d entries %d", reg.Total, len(reg.Entries))
	}
}

func TestPinsRegistryReadsConfigurationWithoutIncludes(t *testing.T) {
	f := newPinsFixture(t)
	inc := filepath.Join(f.Root, "inc.cfg")
	writeFile(t, inc, "[submodule \"injected\"]\n\tpath = injected\n\turl = https://example.invalid/i\n[submodule \"member\"]\n\tactive = false\n")
	// In the local configuration: an include must not import activity or names.
	git(t, f.Super, "config", "include.path", inc)
	reg := registryOf(t, f.Super)
	if a := entryAt(t, reg, "member").Active; a == nil || !*a {
		t.Fatalf("an included file changed activity: %v", a)
	}
	// In the HEAD .gitmodules blob: an absolute include.
	git(t, f.Super, "config", "--unset", "include.path")
	git(t, f.Super, "config", "-f", ".gitmodules", "include.path", inc)
	git(t, f.Super, "add", ".gitmodules")
	git(t, f.Super, "commit", "-qm", "include")
	reg = registryOf(t, f.Super)
	if len(reg.Entries) != 1 {
		t.Fatalf("an include in the registration blob imported entries: %+v", reg.Entries)
	}
	if reg.Gitmodules.Worktree != "same" {
		t.Fatalf("the worktree comparison must not follow the include either: %+v", reg.Gitmodules)
	}
	// And in a submodule's own remotes.
	member := filepath.Join(f.Super, "member")
	git(t, member, "config", "--file", filepath.Join(f.Super, ".git", "modules", "member", "config"), "include.path", filepath.Join(f.Root, "rem.cfg"))
	writeFile(t, filepath.Join(f.Root, "rem.cfg"), "[remote \"injected\"]\n\turl = https://example.invalid/r\n")
	remotes, err := pinsReadRemotes(newPinsRunner(context.Background()), memberRepo(f))
	if err != nil || len(remotes) != 1 || remotes[0].Name != "origin" {
		t.Fatalf("%v %v", remotes, err)
	}
	// The layout read of the extension flag disables includes too.
	writeFile(t, filepath.Join(f.Root, "ext.cfg"), "[extensions]\n\tsubmodulePathConfig = true\n")
	git(t, f.Super, "config", "include.path", filepath.Join(f.Root, "ext.cfg"))
	if l, err := newPinsSuperLayout(newPinsRunner(context.Background()), f.Super); err != nil || l.PathConfig {
		t.Fatalf("%+v %v", l, err)
	}
}

func TestPinsRegistryConflictAmbiguousAndBoolean(t *testing.T) {
	d, oid := newBareSuper(t)
	gitlink(t, d, oid, "shared")
	git(t, d, "config", "-f", ".gitmodules", "submodule.a.path", "shared")
	git(t, d, "config", "-f", ".gitmodules", "submodule.b.path", "shared")
	git(t, d, "add", ".gitmodules")
	git(t, d, "commit", "-qm", "two names")
	e := entryAt(t, registryOf(t, d), "shared")
	if !e.AmbiguousName || str(e.Name) != "a" {
		t.Fatalf("%+v", e)
	}
	// A conflicted staged .gitmodules differs from HEAD.
	blob := gitHash(t, d, "[submodule \"a\"]\n\tpath = other\n")
	gitStdin(t, d, "100644 "+blob+" 1\t.gitmodules\x00100644 "+blob+" 2\t.gitmodules\x00", "update-index", "--force-remove", "-z", "--index-info")
	reg := registryOf(t, d)
	if reg.Gitmodules.StagedKind != "conflict" || !reg.Gitmodules.StagedDiffers {
		t.Fatalf("%+v", reg.Gitmodules)
	}
	for in, want := range map[string]*bool{"true": ptrBool(true), "yes": ptrBool(true), "on": ptrBool(true), "1": ptrBool(true), "false": ptrBool(false), "no": ptrBool(false), "off": ptrBool(false), "0": ptrBool(false), "": ptrBool(false), "maybe": nil, "2": nil} {
		got := pinsParseBool(pinsConfigKV{Value: in, HasValue: true})
		if (got == nil) != (want == nil) || got != nil && *got != *want {
			t.Errorf("bool %q: %v want %v", in, got, want)
		}
	}
	if got := pinsParseBool(pinsConfigKV{}); got == nil || !*got {
		t.Error("a valueless key is true")
	}
}

func ptrBool(b bool) *bool { return &b }

func TestPinsRegistryLinkedWorktreeWithSubmodulePathConfigIsRefusedDistinctly(t *testing.T) {
	requireSubmodulePathConfig(t)
	f := newPinsFixture(t)
	git(t, f.Super, "config", "core.repositoryformatversion", "1")
	git(t, f.Super, "config", "extensions.submodulePathConfig", "true")
	git(t, f.Super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", "--name", "x/y", f.Sub, "slashy")
	git(t, f.Super, "commit", "-qm", "slashy")
	wt := filepath.Join(f.Root, "linked")
	git(t, f.Super, "worktree", "add", "-q", wt)
	// Git 2.56 itself cannot create the submodule in a linked worktree with this extension (it resolves the
	// configured gitdir against the linked worktree's .git file); a hand-made pointer is refused with the
	// distinct "not confined" message because that value leaves the worktree's modules directory.
	mkdirs(t, filepath.Join(wt, "slashy"))
	writeFile(t, filepath.Join(wt, "slashy", ".git"), "gitdir: ../../super/.git/modules/x%2fy\n")
	run := newPinsRunner(context.Background())
	l, err := newPinsSuperLayout(run, wt)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := pinsEnumerate(run, l)
	if err != nil {
		t.Fatal(err)
	}
	e := entryAt(t, reg, "slashy")
	_, refusal, err := newPinsSafety(l, run).resolve(pinsSubmoduleSpec{Path: "slashy", Name: *e.Name, ConfiguredGitDir: e.ConfiguredGitDir})
	if err != nil || refusal == nil || refusal.Code != "submodule-path-unsafe" || !strings.Contains(refusal.Message, "not confined under the modules directory") {
		t.Fatalf("%v %v", refusal, err)
	}
}

func TestPinsRegistryEmptyOrValuelessGitmodulesAndWorktreePresence(t *testing.T) {
	d, oid := newBareSuper(t)
	gm := filepath.Join(d, ".gitmodules")
	// 1. No .gitmodules in HEAD, but an empty .gitmodules file appears in the worktree -> "different".
	writeFile(t, gm, "# comment only\n")
	if g := registryOf(t, d).Gitmodules; g.Worktree != "different" || !g.Modified {
		t.Fatalf("untracked empty .gitmodules in worktree: %+v", g)
	}
	// 2. Commit the empty .gitmodules to HEAD, then delete it from the worktree -> "absent".
	git(t, d, "add", ".gitmodules")
	git(t, d, "commit", "-qm", "empty gitmodules")
	rmAll(t, gm)
	if g := registryOf(t, d).Gitmodules; g.Worktree != "absent" || !g.Modified {
		t.Fatalf("deleted empty .gitmodules in worktree: %+v", g)
	}
	// 3. Valueless or empty path/url/branch/gitdir keys must not fabricate phantom entries or activity.
	writeFile(t, gm, "[submodule \"no-path\"]\n\tpath\n\turl = https://example.invalid/np\n[submodule \"empty-path\"]\n\tpath =\n\turl = https://example.invalid/ep\n[submodule \"real\"]\n\tpath = real\n\turl\n\tbranch\n")
	gitlink(t, d, oid, "real")
	git(t, d, "add", ".gitmodules")
	git(t, d, "commit", "-qm", "valueless keys")
	// Inject valueless submodule.real.url and submodule.real.gitdir into .git/config.
	cfgPath := filepath.Join(d, ".git", "config")
	origCfg, _ := os.ReadFile(cfgPath)
	writeFile(t, cfgPath, string(origCfg)+"[submodule \"real\"]\n\turl\n\tgitdir\n")
	reg := registryOf(t, d)
	if len(reg.Entries) != 1 || reg.Total != 1 {
		t.Fatalf("want only 'real', got %+v", reg.Entries)
	}
	e := entryAt(t, reg, "real")
	if e.URL != nil || e.Branch != nil || e.ConfiguredGitDir != nil || e.Active == nil || *e.Active {
		t.Fatalf("valueless url/branch/gitdir must be ignored: %+v", e)
	}
}
