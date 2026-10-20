// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// Spec 031 T015: merge-noop and no-net-change on real repositories.

func noopPolicy(kinds ...string) V3ProofPolicy { return V3ProofPolicy{Version: 1, Kinds: kinds} }

var noopOnly = noopPolicy("merge-noop")
var allProofs = noopPolicy("patch-id", "merge-noop", "no-net-change")

// landed builds a topic of two commits and a main that landed them either as
// cherry-picks (rebase-merge) or as one squashed commit.
func landedFix(t *testing.T, squash bool) *proofFix {
	f := newProofFix(t)
	mb := f.rev("HEAD")
	f.branch("topic", mb)
	c1 := f.commit("add a", map[string]string{"a.txt": "alpha\n"})
	c2 := f.commit("add b", map[string]string{"b.txt": "beta\n"})
	f.checkout(f.main)
	f.commit("unrelated", map[string]string{"u.txt": "unrelated\n"})
	if squash {
		f.commit("squash", map[string]string{"a.txt": "alpha\n", "b.txt": "beta\n"})
	} else {
		f.pick(c1)
		f.pick(c2)
	}
	return f
}

func TestMergeNoopRebaseMergeAndSquashMerge(t *testing.T) {
	for name, squash := range map[string]bool{"rebase-merge": false, "squash-merge": true} {
		t.Run(name, func(t *testing.T) {
			f := landedFix(t, squash)
			v := f.verdict(f.main, "topic", noopOnly)
			wantVerdict(t, v, "proven", "merge-noop")
			if len(v.Flags) != 0 || v.Matched != 0 {
				t.Fatalf("verdict %+v", v)
			}
		})
	}
	// A squash is not patch-equivalent: with every class selected merge-noop proves it.
	f := landedFix(t, true)
	wantVerdict(t, f.verdict(f.main, "topic", allProofs), "proven", "merge-noop")
	// A rebase-merge is patch-equivalent: the stronger class wins.
	r := landedFix(t, false)
	wantVerdict(t, r.verdict(r.main, "topic", allProofs), "proven", "patch-id")
}

// A revert after the merge: the patches still match, the content is gone.
func TestMergeNoopRevertAfterMergeIsNotNoopWhilePatchIDMatches(t *testing.T) {
	f := newProofFix(t)
	mb := f.rev("HEAD")
	f.branch("topic", mb)
	c := f.commit("feature", map[string]string{"a.txt": "alpha\n"})
	f.checkout(f.main)
	f.commit("unrelated", map[string]string{"u.txt": "unrelated\n"})
	f.pick(c)
	f.git("revert", "--no-edit", "HEAD")
	wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "needs-review", "", "would-add-content")
	wantVerdict(t, f.verdict(f.main, "topic", patchPolicy(false)), "proven", "patch-id")
}

func TestNoNetChangeIsDeletableAndNeverIntegrated(t *testing.T) {
	f := newProofFix(t)
	mb := f.rev("HEAD")
	f.branch("topic", mb)
	f.commit("wip", map[string]string{"w.txt": "wip\n"})
	f.git("revert", "--no-edit", "HEAD")
	f.checkout(f.main)
	f.commit("advance", map[string]string{"u.txt": "unrelated\n"})
	v := f.verdict(f.main, "topic", noopPolicy("no-net-change"))
	wantVerdict(t, v, "proven", "no-net-change")
	if integrationStrength[v.Class] != "nothing-unmerged" {
		t.Fatalf("strength %q", integrationStrength[v.Class])
	}
	// merge-noop requires B^{tree} != M^{tree}; this branch has nothing to merge.
	if g := f.verdict(f.main, "topic", noopOnly); g.Disposition == "proven" {
		t.Fatalf("merge-noop proved an empty net change: %+v", g)
	}
	wantVerdict(t, f.verdict(f.main, "topic", allProofs), "proven", "no-net-change")
}

func TestMergeNoopConflictAndWouldAddContent(t *testing.T) {
	f := newProofFix(t)
	f.commit("seed", map[string]string{"f.txt": "one\ntwo\nthree\n"})
	mb := f.rev("HEAD")
	f.branch("topic", mb)
	f.commit("topic edit", map[string]string{"f.txt": "one\nTOPIC\nthree\n", "new.txt": "new\n"})
	f.checkout(f.main)
	f.commit("base edit", map[string]string{"f.txt": "one\nBASE\nthree\n"})
	wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "needs-review", "", "conflict")
	g := newProofFix(t)
	gb := g.rev("HEAD")
	g.branch("topic", gb)
	g.commit("topic adds", map[string]string{"new.txt": "new\n"})
	g.checkout(g.main)
	g.commit("advance", map[string]string{"u.txt": "u\n"})
	wantVerdict(t, g.verdict(g.main, "topic", noopOnly), "needs-review", "", "would-add-content")
}

func TestMergeNoopGitlinkChangeNeverRunsMergeTree(t *testing.T) {
	f := newProofFix(t)
	mb := f.rev("HEAD")
	f.branch("topic", mb)
	f.git("update-index", "--add", "--cacheinfo", "160000,"+strings.Repeat("1", 40)+",sub")
	f.git("commit", "-qm", "gitlink")
	f.checkout(f.main)
	f.commit("advance", map[string]string{"u.txt": "u\n"})
	wantVerdict(t, f.verdictBare(f.main, "topic", noopOnly), "needs-review", "", "gitlink-change")
	// A gitlink on the base side counts too.
	g := newProofFix(t)
	gb := g.rev("HEAD")
	g.branch("topic", gb)
	g.commit("topic", map[string]string{"t.txt": "t\n"})
	g.checkout(g.main)
	g.git("update-index", "--add", "--cacheinfo", "160000,"+strings.Repeat("2", 40)+",sub")
	g.git("commit", "-qm", "base gitlink")
	wantVerdict(t, g.verdictBare(g.main, "topic", noopOnly), "needs-review", "", "gitlink-change")
}

// merge-tree must not run when a merge driver is configured: the configuration
// note raises merge-driver and merge-tree never runs the helper.
func TestMergeNoopMergeDriverConfigurationBlocksMergeTree(t *testing.T) {
	f := landedFix(t, true)
	marker := filepath.Join(gitTempDir(t), "marker")
	helper := filepath.Join(gitTempDir(t), "driver")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\necho run >> '"+marker+"'\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.git("config", "merge.x.driver", helper+" %O %A %B")
	f.git("config", "filter.y.clean", helper)
	wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "needs-review", "", "merge-driver")
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the merge driver helper ran")
	}
}

// Union repro (C28): the branch deletes a line the base changed. Without the
// attribute that is a conflict; with merge=union in the base tree, the branch
// tree or the merge base tree, Git would merge cleanly to the base tree, so the
// tip must never be proven.
func unionFix(t *testing.T, where string, attrs string) *proofFix {
	f := newProofFix(t)
	seed := map[string]string{"f.txt": "a\nb\nc\n"}
	if where == "merge-base" {
		seed[".gitattributes"] = attrs
	}
	f.commit("seed", seed)
	mb := f.rev("HEAD")
	f.branch("topic", mb)
	topic := map[string]string{"f.txt": "a\nc\n"}
	if where == "branch" {
		topic[".gitattributes"] = attrs
	}
	f.commit("topic deletes b", topic)
	if where == "merge-base" {
		f.git("rm", "-q", ".gitattributes")
		f.git("commit", "-qm", "drop attributes on the branch")
	}
	f.checkout(f.main)
	base := map[string]string{"f.txt": "a\nb2\nc\n"}
	if where == "base" {
		base[".gitattributes"] = attrs
	}
	f.commit("base changes b", base)
	if where == "merge-base" {
		f.git("rm", "-q", ".gitattributes")
		f.git("commit", "-qm", "drop attributes on the base")
	}
	return f
}

func TestMergeNoopMergeAttributesInAnyTreeBlockMergeTree(t *testing.T) {
	control := unionFix(t, "none", "")
	wantVerdict(t, control.verdict(control.main, "topic", noopOnly), "needs-review", "", "conflict")
	attrs := map[string]string{
		"union":          "* merge=union\n",
		"binary macro":   "* binary\n",
		"unset merge":    "* -merge\n",
		"merge=binary":   "*.txt merge=binary\n",
		"user macro":     "[attr]m merge=union\n* m\n",
		"tab separated":  "*.txt\ttext\tmerge=union\n",
		"quoted pattern": "\"a b.txt\" merge=union\n",
		"unspecify":      "* !merge\n",
	}
	for name, attr := range attrs {
		for _, where := range []string{"base", "branch", "merge-base"} {
			t.Run(name+"/"+where, func(t *testing.T) {
				f := unionFix(t, where, attr)
				wantVerdict(t, f.verdictBare(f.main, "topic", noopOnly), "needs-review", "", "merge-attribute")
			})
		}
	}
	t.Run("clean attributes without merge or binary keep evaluating", func(t *testing.T) {
		f := landedFix(t, true)
		f.checkout("topic")
		f.commit("attrs", map[string]string{".gitattributes": "*.txt text eol=lf\n# merge=union in a comment\n"})
		f.checkout(f.main)
		f.commit("attrs landed", map[string]string{".gitattributes": "*.txt text eol=lf\n# merge=union in a comment\n"})
		wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "proven", "merge-noop")
	})
	t.Run("worktree attributes never apply", func(t *testing.T) {
		// Unpinned, merge-tree reads the worktree file and merges this cleanly.
		f := unionFix(t, "none", "")
		f.write(".gitattributes", "* merge=union\n")
		c := exec.Command("git", "-C", f.dir, "merge-tree", "--write-tree", "--no-messages", f.main, "topic")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("the fixture must bite (the worktree attribute merges cleanly): %v %s", err, out)
		}
		wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "needs-review", "", "conflict")
	})
	t.Run("global and XDG attributes never apply", func(t *testing.T) {
		f := unionFix(t, "none", "")
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if err := os.MkdirAll(filepath.Join(xdg, "git"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(xdg, "git", "attributes"), []byte("* merge=union\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		// The user's own Git would merge this cleanly; the proof must not.
		c := exec.Command("git", "-C", f.dir, "merge-tree", "--write-tree", "--no-messages", f.main, "topic")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("the fixture must bite (union merges cleanly): %v %s", err, out)
		}
		wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "needs-review", "", "conflict")
	})
}

func TestMergeNoopAttributeSourceVariants(t *testing.T) {
	prepare := func(t *testing.T) (*proofFix, string) {
		f := landedFix(t, true)
		return f, filepath.Join(f.dir, ".git", "info", "attributes")
	}
	t.Run("content line", func(t *testing.T) {
		f, path := prepare(t)
		f.write(".git/info/attributes", "* merge=union\n")
		wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "needs-review", "", "attribute-source")
		_ = path
	})
	t.Run("macro use", func(t *testing.T) {
		f, _ := prepare(t)
		f.write(".git/info/attributes", "[attr]m merge=union\n")
		wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "needs-review", "", "attribute-source")
	})
	t.Run("comment only and absent are ignored", func(t *testing.T) {
		f, _ := prepare(t)
		wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "proven", "merge-noop")
		f.write(".git/info/attributes", "# comment\n\n   \n")
		wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "proven", "merge-noop")
	})
	t.Run("symlink", func(t *testing.T) {
		f, path := prepare(t)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/dev/null", path); err != nil {
			t.Fatal(err)
		}
		wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "needs-review", "", "attribute-source")
	})
	t.Run("special file", func(t *testing.T) {
		// Git itself blocks reading a FIFO as info/attributes, so only the
		// classification (which never opens the file) is testable.
		f, path := prepare(t)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Skip("fifo unavailable: ", err)
		}
		if !infoAttributesActive(filepath.Join(f.dir, ".git")) {
			t.Fatal("a special file must count as active attribute source")
		}
	})
	t.Run("over 1 MiB", func(t *testing.T) {
		f, _ := prepare(t)
		f.write(".git/info/attributes", strings.Repeat("#", 1<<20+1)+"\n")
		wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "needs-review", "", "attribute-source")
	})
	t.Run("through a linked worktree", func(t *testing.T) {
		f, _ := prepare(t)
		f.write(".git/info/attributes", "* merge=union\n")
		linked := filepath.Join(gitTempDir(t), "linked")
		f.git("worktree", "add", "-q", "-b", "linked-branch", linked, f.main)
		g := &proofFix{t: t, dir: linked, main: f.main}
		wantVerdict(t, g.verdict(f.main, "topic", noopOnly), "needs-review", "", "attribute-source")
	})
}

func TestMergeNoopAndNoNetChangeHardStopsAndAmbiguousBase(t *testing.T) {
	f := landedFix(t, true)
	f.write(".git/info/grafts", f.rev("HEAD")+"\n")
	for _, kinds := range [][]string{{"merge-noop"}, {"no-net-change"}} {
		wantVerdict(t, f.verdictBare(f.main, "topic", noopPolicy(kinds...)), "retained", "", "history-override")
	}
	g := newProofFix(t)
	root := g.rev("HEAD")
	g.branch("x", root)
	g.commit("x0", map[string]string{"x.txt": "x\n"})
	g.branch("y", root)
	g.commit("y0", map[string]string{"y.txt": "y\n"})
	g.git("merge", "-qm", "x into y", "x")
	g.checkout("x")
	g.git("merge", "-qm", "y0 into x", g.rev("y~1"))
	for _, kinds := range [][]string{{"merge-noop"}, {"no-net-change"}, {"patch-id", "merge-noop", "no-net-change"}} {
		wantVerdict(t, g.verdict("x", "y", noopPolicy(kinds...)), "needs-review", "", "ambiguous-merge-base")
	}
}

// merge-tree writes objects only into the private directory: the repository's
// object directory, refs and packed-refs are byte-identical after the proof.
func TestMergeNoopLeavesObjectsRefsAndPackedRefsUntouched(t *testing.T) {
	f := landedFix(t, true)
	f.git("pack-refs", "--all")
	snap := func() map[string]string { return snapshotTree(t, filepath.Join(f.dir, ".git"), "index", "FETCH_HEAD") }
	before := snap()
	wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "proven", "merge-noop")
	g := unionFix(t, "none", "")
	wantVerdict(t, g.verdict(g.main, "topic", noopOnly), "needs-review", "", "conflict")
	if !reflect.DeepEqual(before, snap()) {
		t.Fatal("the proof changed the repository")
	}
}

// Attribute-scan bounds: at most 1,024 tree records, 64 files of 1 MiB.
func TestMergeNoopAttributeScanBounds(t *testing.T) {
	t.Run("more than 1,024 leaves", func(t *testing.T) {
		f := newProofFix(t)
		root := f.rev("HEAD")
		f.fastImport(importChain("m", 1025, "refs/heads/topic", root, 1000000) + importChain("n", 1, "refs/heads/baseline", root, 2000000))
		wantVerdict(t, f.verdictBare("baseline", "topic", noopOnly), "needs-review", "", "proof-limit")
	})
	attrFiles := func(n int, size int) string {
		var b strings.Builder
		root := "0000000000000000000000000000000000000000"
		_ = root
		for i := 0; i < n; i++ {
			content := "# " + strings.Repeat("x", size-3) + "\n"
			fmt.Fprintf(&b, "M 100644 inline d%d/.gitattributes\ndata %d\n%s\n", i, len(content), content)
		}
		return b.String()
	}
	import1 := func(branch string, files string, clock int, parent string) string {
		return fmt.Sprintf("commit refs/heads/%s\ncommitter T <t@t> %d +0000\ndata 1\nc\nfrom %s\n%s\n", branch, clock, parent, files)
	}
	for name, c := range map[string]struct {
		n, size int
		want    string
	}{"64 files of 1 MiB": {64, 1 << 20, "proven"}, "65 files": {65, 100, "needs-review"}, "one file over 1 MiB": {1, 1<<20 + 1, "needs-review"}} {
		t.Run(name, func(t *testing.T) {
			f := newProofFix(t)
			root := f.rev("HEAD")
			f.fastImport(import1("topic", attrFiles(c.n, c.size)+"M 100644 inline new.txt\ndata 2\nn\n", 1000000, root) + import1("baseline", attrFiles(c.n, c.size)+"M 100644 inline new.txt\ndata 2\nn\n", 2000000, root))
			v := f.verdictBare("baseline", "topic", noopOnly)
			if v.Disposition != c.want {
				t.Fatalf("verdict %+v", v)
			}
			if c.want == "needs-review" {
				wantVerdict(t, v, "needs-review", "", "proof-limit")
			}
		})
	}
}

func TestIntegrationCatFileBatchBounds(t *testing.T) {
	oid := strings.Repeat("a", 40)
	batch := func(size int, content string) io.Reader {
		return strings.NewReader(fmt.Sprintf("%s blob %d\n%s\n", oid, size, content))
	}
	files, err := parseCatFileBatch(batch(1<<20, strings.Repeat("x", 1<<20)), []string{oid}, 40)
	if err != nil || len(files[oid]) != 1<<20 {
		t.Fatalf("1 MiB accepted: %v", err)
	}
	if _, err = parseCatFileBatch(batch(1<<20+1, strings.Repeat("x", 1<<20+1)), []string{oid}, 40); err != errIntegrationLimit {
		t.Fatalf("1 MiB + 1: %v", err)
	}
	if _, err = parseCatFileBatch(strings.NewReader(oid+" missing\n"), []string{oid}, 40); err != errIntegrationLimit {
		t.Fatalf("missing object: %v", err)
	}
	if _, err = parseCatFileBatch(strings.NewReader(oid+" tree 3\nabc\n"), []string{oid}, 40); err != errIntegrationLimit {
		t.Fatalf("non-blob: %v", err)
	}
	if _, err = parseCatFileBatch(batch(5, "abc"), []string{oid}, 40); err != errIntegrationLimit {
		t.Fatalf("short content: %v", err)
	}
	if _, err = parseCatFileBatch(strings.NewReader(strings.Repeat("b", 40)+" blob 1\nx\n"), []string{oid}, 40); err != errIntegrationLimit {
		t.Fatalf("unrequested object: %v", err)
	}
	wide := strings.Repeat("c", 64)
	if got, err := parseCatFileBatch(strings.NewReader(wide+" blob 2\nhi\n"), []string{wide}, 64); err != nil || string(got[wide]) != "hi" {
		t.Fatalf("sha256 width: %v", err)
	}
}

func TestScanMergeAttributesIsConservative(t *testing.T) {
	yes := []string{"* merge=union", "*.go merge", "*.go -merge", "*.go !merge", "* binary", "* -binary", "[attr]m merge=union", "[attr]b binary", "a text   merge=ours", "\"a b\" merge=x", "*.c\ttext\tbinary", "  * merge=union  ", "* MERGE=union", "*.go crlf=input\r\n*.x merge=union"}
	for _, line := range yes {
		if !scanMergeAttributes([][]byte{[]byte(line + "\n")}) {
			t.Errorf("not detected: %q", line)
		}
	}
	no := []string{"", "# merge=union", "*.go text eol=lf", "*.go mergeable", "*.go mymerge=x", "merge.go text", "[attr]other text", "*.go diff=go linguist-generated"}
	for _, line := range no {
		if scanMergeAttributes([][]byte{[]byte(line + "\n")}) {
			t.Errorf("false positive: %q", line)
		}
	}
	if !scanMergeAttributes([][]byte{[]byte("*.go text\n"), []byte("* merge=union\n")}) {
		t.Error("second file not scanned")
	}
}

// merge-tree must fail closed: an exit other than 0 or 1, extra output on a
// clean exit or a malformed result tree is proof-limit, never a proof.
func TestMergeNoopMergeTreeFailsClosed(t *testing.T) {
	f := landedFix(t, true)
	wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "proven", "merge-noop")
	baseTree := f.git("rev-parse", f.main+"^{tree}")
	cases := map[string]string{
		"exit 2":                       "echo " + baseTree + "; exit 2",
		"exit 128":                     "exit 128",
		"extra stdout on a clean exit": "echo " + baseTree + "; echo extra; exit 0",
		"no output on a clean exit":    "exit 0",
		"malformed tree":               "echo not-a-tree; exit 0",
		"conflict exit without a tree": "exit 1",
	}
	for name, fragment := range cases {
		t.Run(name, func(t *testing.T) {
			f.bin = wrapGit(t, fragment)
			v := f.verdict(f.main, "topic", noopOnly)
			wantVerdict(t, v, "needs-review", "", "proof-limit")
		})
	}
}

// The private object directory may be handed out through a symbolic link.
func TestMergeNoopObjectDirectoryThroughASymlink(t *testing.T) {
	f := landedFix(t, true)
	real := gitTempDir(t)
	link := filepath.Join(gitTempDir(t), "via-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	engine := defaultIntegrationEngine()
	engine.makeObjectDir = func() (string, error) { return os.MkdirTemp(link, "objects-") }
	v, err := f.verdictWith(f.main, "topic", noopOnly, engine, true)
	if err != nil {
		t.Fatal(err)
	}
	wantVerdict(t, v, "proven", "merge-noop")
	if entries, _ := os.ReadDir(real); len(entries) != 0 {
		t.Fatalf("the object directory was not removed: %v", entries)
	}
}

// The proofs run under the private constant environment: a merge driver or a
// clean/smudge filter (git-lfs installs them system wide) defined in the host's
// global, XDG or system configuration cannot run and must not block merge-noop.
// Only the repository's own configuration decides merge-driver.
func TestMergeNoopIgnoresHostConfigurationDrivers(t *testing.T) {
	hostConfig := "[merge \"host\"]\n\tdriver = true %O %A %B\n[filter \"lfs\"]\n\tclean = git-lfs clean -- %f\n\tsmudge = git-lfs smudge -- %f\n\tprocess = git-lfs filter-process\n\trequired = true\n[merge]\n\trenormalize = true\n"
	for name, place := range map[string]func(t *testing.T){
		"global": func(t *testing.T) { writeHostFile(t, filepath.Join(os.Getenv("HOME"), ".gitconfig"), hostConfig) },
		"xdg": func(t *testing.T) {
			writeHostFile(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "git", "config"), hostConfig)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := landedFix(t, true)
			t.Setenv("GIT_CONFIG_GLOBAL", "") // restored on cleanup
			os.Unsetenv("GIT_CONFIG_GLOBAL")
			place(t)
			// The fixture bites: the host configuration is visible to plain Git.
			if out := f.git("config", "--get-all", "filter.lfs.clean"); out == "" {
				t.Fatal("the host configuration is not in effect for ordinary Git")
			}
			wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "proven", "merge-noop")
			// The same driver in the repository's own configuration still blocks.
			f.git("config", "merge.host.driver", "true")
			wantVerdict(t, f.verdict(f.main, "topic", noopOnly), "needs-review", "", "merge-driver")
		})
	}
}

func writeHostFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
