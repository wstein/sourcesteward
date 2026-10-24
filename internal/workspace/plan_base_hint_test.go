// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func refusedBase(t *testing.T, source, base string) string {
	t.Helper()
	state := gitTempDir(t)
	if e := os.Chmod(state, 0700); e != nil {
		t.Fatal(e)
	}
	got := PlanCreate(context.Background(), PlanOptions{Path: source, Base: base, Destination: filepath.Join(gitTempDir(t), "task"), Task: "hint", Owner: "fixture", StateDir: state})
	if got.Status == "ok" || len(got.Diagnostics) != 1 || got.Diagnostics[0].Code != "git-base-refused" {
		t.Fatalf("%q: %+v", base, got)
	}
	return got.Diagnostics[0].Message
}

func requireText(t *testing.T, base, message string, contains, absent []string) {
	t.Helper()
	for _, want := range contains {
		if !strings.Contains(message, want) {
			t.Fatalf("%s: message %q lacks %q", base, message, want)
		}
	}
	for _, no := range absent {
		if strings.Contains(message, no) {
			t.Fatalf("%s: message %q has %q", base, message, no)
		}
	}
}

func TestRefusedGitBaseNamesAcceptedFormsAndSuggestsTheQualifiedRef(t *testing.T) {
	source := fixtureGit(t)
	gitFixture(t, source, "update-ref", "refs/remotes/origin/main", "HEAD")
	gitFixture(t, source, "update-ref", "refs/heads/topic", "HEAD")
	head := gitFixture(t, source, "rev-parse", "HEAD")
	for _, tc := range []struct {
		base     string
		contains []string
		absent   []string
	}{
		{"origin/main", []string{"full commit id (40 hex digits)", "refs/remotes/origin/main", "Given: \"origin/main\"", "Did you mean refs/remotes/origin/main?"}, nil},
		{"topic", []string{"full commit id", "Did you mean refs/heads/topic?"}, nil},
		{"no/such-ref", []string{"full commit id", "refs/remotes/origin/main", "Given: \"no/such-ref\""}, []string{"Did you mean"}},
		{head[:12], []string{"abbreviated", "full commit id " + head}, []string{"Did you mean"}},
		{"refs/bad..name", []string{"not a valid literal fully qualified reference", "Given:"}, []string{"Did you mean"}},
	} {
		requireText(t, tc.base, refusedBase(t, source, tc.base), tc.contains, tc.absent)
	}
}

func TestRefusedGitBaseNamesEveryMatchingQualifiedRef(t *testing.T) {
	source := fixtureGit(t)
	gitFixture(t, source, "update-ref", "refs/heads/rel", "HEAD")
	gitFixture(t, source, "update-ref", "refs/tags/rel", "HEAD")
	requireText(t, "rel", refusedBase(t, source, "rel"), []string{"Did you mean one of refs/heads/rel, refs/tags/rel?"}, nil)
}

func TestRefusedGitBaseTreatsAHexadecimalBranchNameAsARef(t *testing.T) {
	source := fixtureGit(t)
	head := gitFixture(t, source, "rev-parse", "HEAD")
	other := "cafe"
	if strings.HasPrefix(head, other) {
		other = "face"
	}
	gitFixture(t, source, "update-ref", "refs/heads/"+other, "HEAD")
	m := refusedBase(t, source, other)
	requireText(t, other, m, []string{"Did you mean refs/heads/" + other + "?"}, []string{"abbreviated"})
}

func TestRefusedGitBaseNeverEchoesUnprintableInput(t *testing.T) {
	source := fixtureGit(t)
	for _, base := range []string{"bad\nref", "bad\x1bref", "bad\u202eref"} {
		m := refusedBase(t, source, base)
		requireText(t, base, m, []string{"full commit id"}, []string{"bad\nref", "\x1b", "\u202e"})
		if strings.Contains(base, "\n") && strings.Contains(m, "Given:") {
			t.Fatalf("newline base echoed: %q", m)
		}
	}
}

func TestRefusedGitBaseNamesTheSha256LengthOfASha256Repository(t *testing.T) {
	source := gitTempDir(t)
	useSharedFixtureHome(t)
	isolateFixtureHome(t)
	gitFixture(t, source, "init", "-q", "--object-format=sha256")
	gitFixture(t, source, "-c", "user.name=F", "-c", "user.email=f@example.invalid", "commit", "-q", "--allow-empty", "-m", "x")
	requireText(t, "nope", refusedBase(t, source, "nope"), []string{"full commit id (64 hex digits)"}, []string{"40 hex"})
}
