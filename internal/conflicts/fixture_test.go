// SPDX-License-Identifier: EUPL-1.2
package conflicts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-C", dir}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func lines(n int, mark map[int]string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if m, ok := mark[i]; ok {
			b.WriteString(m + "\n")
		} else {
			b.WriteString("line " + string(rune('a'+i%26)) + "\n")
		}
	}
	return b.String()
}

// repoFixture is a clone with a bare origin.
//
//	main:  base -> mainCommit (changes rules.md line 2 and notes.md line 1)
//	pr 1:  base -> pr1 (changes rules.md line 2 differently, notes.md line 9, own.md); head ref "feat-one"
//	pr 2:  pr1 -> pr2 (changes own.md); head ref "feat-two", base "feat-one"
//	pr 3:  base -> pr3 (changes only other.md); head ref "feat-three"
//	pr 4:  base -> pr4 (changes notes.md line 10: merges cleanly, overlaps main); head ref "feat-four"
type repoFixture struct {
	dir, origin                  string
	base, mainTip, head1, head2  string
	head3, head4, mainTouchRules string
}

func newRepoFixture(t *testing.T) *repoFixture {
	t.Helper()
	root := t.TempDir()
	f := &repoFixture{dir: filepath.Join(root, "work"), origin: filepath.Join(root, "origin.git")}
	git(t, root, "init", "-q", "--bare", "-b", "main", f.origin)
	git(t, root, "init", "-q", "-b", "main", f.dir)
	git(t, f.dir, "remote", "add", "origin", f.origin)
	write(t, f.dir, "rules.md", lines(12, nil))
	write(t, f.dir, "notes.md", lines(12, nil))
	write(t, f.dir, "own.md", lines(4, nil))
	write(t, f.dir, "other.md", lines(4, nil))
	git(t, f.dir, "add", ".")
	git(t, f.dir, "commit", "-q", "-m", "base")
	f.base = git(t, f.dir, "rev-parse", "HEAD")
	git(t, f.dir, "push", "-q", "origin", "main")

	git(t, f.dir, "checkout", "-q", "-b", "feat-one")
	write(t, f.dir, "rules.md", lines(12, map[int]string{2: "PR one rule"}))
	write(t, f.dir, "notes.md", lines(12, map[int]string{9: "PR one note"}))
	write(t, f.dir, "own.md", lines(4, map[int]string{1: "PR one own"}))
	git(t, f.dir, "commit", "-q", "-am", "pr one")
	f.head1 = git(t, f.dir, "rev-parse", "HEAD")
	git(t, f.dir, "push", "-q", "origin", "feat-one", "feat-one:refs/pull/1/head")

	git(t, f.dir, "checkout", "-q", "-b", "feat-two")
	write(t, f.dir, "own.md", lines(4, map[int]string{3: "PR two own"}))
	git(t, f.dir, "commit", "-q", "-am", "pr two")
	f.head2 = git(t, f.dir, "rev-parse", "HEAD")
	git(t, f.dir, "push", "-q", "origin", "feat-two:refs/pull/2/head", "feat-two")

	git(t, f.dir, "checkout", "-q", "main")
	git(t, f.dir, "checkout", "-q", "-b", "feat-three")
	write(t, f.dir, "other.md", lines(4, map[int]string{2: "PR three"}))
	git(t, f.dir, "commit", "-q", "-am", "pr three")
	f.head3 = git(t, f.dir, "rev-parse", "HEAD")
	git(t, f.dir, "push", "-q", "origin", "feat-three:refs/pull/3/head", "feat-three")

	git(t, f.dir, "checkout", "-q", "main")
	git(t, f.dir, "checkout", "-q", "-b", "feat-four")
	write(t, f.dir, "notes.md", lines(12, map[int]string{10: "PR four note"}))
	git(t, f.dir, "commit", "-q", "-am", "pr four")
	f.head4 = git(t, f.dir, "rev-parse", "HEAD")
	git(t, f.dir, "push", "-q", "origin", "feat-four:refs/pull/4/head", "feat-four")

	git(t, f.dir, "checkout", "-q", "main")
	write(t, f.dir, "rules.md", lines(12, map[int]string{2: "main rule"}))
	git(t, f.dir, "commit", "-q", "-am", "main changes rules")
	f.mainTouchRules = git(t, f.dir, "rev-parse", "HEAD")
	write(t, f.dir, "notes.md", lines(12, map[int]string{1: "main note"}))
	git(t, f.dir, "commit", "-q", "-am", "main changes notes")
	f.mainTip = git(t, f.dir, "rev-parse", "HEAD")
	git(t, f.dir, "push", "-q", "origin", "main")
	return f
}

func (f *repoFixture) prs() []PR {
	return []PR{
		{Number: 1, Base: "main", HeadRef: "feat-one", Head: f.head1},
		{Number: 2, Base: "feat-one", HeadRef: "feat-two", Head: f.head2},
		{Number: 3, Base: "main", HeadRef: "feat-three", Head: f.head3},
		{Number: 4, Base: "main", HeadRef: "feat-four", Head: f.head4},
	}
}
