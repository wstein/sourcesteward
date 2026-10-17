// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gitmodulesOfSize returns a valid .gitmodules of exactly n bytes: one entry, padded with comment lines.
func gitmodulesOfSize(t *testing.T, n int, path, url string) string {
	t.Helper()
	head := "[submodule \"member\"]\n\tpath = " + path + "\n\turl = " + url + "\n"
	pad := n - len(head)
	if pad < 0 {
		t.Fatal("too small")
	}
	var b strings.Builder
	b.WriteString(head)
	for pad > 0 {
		line := "#" + strings.Repeat("p", 70) + "\n"
		if pad < len(line) {
			line = "#" + strings.Repeat("p", pad-2) + "\n"
			if pad == 1 {
				line = "\n"
			}
		}
		b.WriteString(line)
		pad -= len(line)
	}
	if b.Len() != n {
		t.Fatalf("built %d bytes, want %d", b.Len(), n)
	}
	return b.String()
}

func commitGitmodules(t *testing.T, f pinsFixture, content string) {
	t.Helper()
	writeFile(t, filepath.Join(f.Super, ".gitmodules"), content)
	git(t, f.Super, "add", ".gitmodules")
	git(t, f.Super, "commit", "-qm", "gitmodules")
}

// The registry accepts a .gitmodules up to 1 MiB (the pins limit), well beyond the runner's 64 KiB record
// limit, and rejects 1 MiB + 1 as unparseable (registration unknown), never as pins-limit.
func TestPinsRegistryReadsGitmodulesUpToOneMiB(t *testing.T) {
	for _, tc := range []struct {
		size    int
		parsed  bool
		comment string
	}{{65535, true, ""}, {65536, true, ""}, {65537, true, "just over the record limit"}, {70000, true, ""}, {1 << 20, true, "exactly the limit"}, {1<<20 + 1, false, "one over"}} {
		f := newPinsFixture(t)
		commitGitmodules(t, f, gitmodulesOfSize(t, tc.size, "member", f.Sub))
		reg := registryOf(t, f.Super)
		e := entryAt(t, reg, "member")
		if tc.parsed != (e.URL != nil && *e.URL == f.Sub) || tc.parsed == reg.Gitmodules.RegistrationUnknown {
			t.Errorf("size %d: url=%v unknown=%v kind=%s", tc.size, e.URL, reg.Gitmodules.RegistrationUnknown, reg.Gitmodules.HeadKind)
		}
		if !tc.parsed && reg.Gitmodules.HeadKind != "unparseable" {
			t.Errorf("size %d: kind %s", tc.size, reg.Gitmodules.HeadKind)
		}
	}
}

func TestPinsAuditBigGitmodulesIsCompleteAndOversizedIsUnknownRegistration(t *testing.T) {
	f := newPinsFixture(t)
	commitGitmodules(t, f, gitmodulesOfSize(t, 70000, "member", f.Sub))
	settleIndexOf(t, f)
	r := auditOf(t, f.Super)
	if r.Status != "ok" || !r.Boundary.Complete {
		t.Fatalf("a valid 70,000 byte .gitmodules must not be a limit: %s %+v", r.Status, r.Findings)
	}
	requireNoFinding(t, r, "pins-limit")
	f = newPinsFixture(t)
	commitGitmodules(t, f, gitmodulesOfSize(t, 1<<20+1, "member", f.Sub))
	r = auditOf(t, f.Super)
	requireFinding(t, r, "gitmodules-modified", "warning", "")
	requireNoFinding(t, r, "pins-limit")
}

// Exact accounting: a blob of exactly the limit is read, one byte more is not.
func TestPinsConfigFromObjectBoundaryIsExact(t *testing.T) {
	f := newPinsFixture(t)
	run := newPinsRunner(context.Background())
	repo := pinsGitRepo{GitDir: filepath.Join(f.Super, ".git"), WorkTree: f.Super}
	for _, limit := range []int{200, 65536, 1 << 20} {
		for delta, want := range map[int]bool{-1: true, 0: true, 1: false} {
			commitGitmodules(t, f, gitmodulesOfSize(t, limit+delta, "member", f.Sub))
			_, ok, err := pinsConfigFromObject(run, repo, "", "HEAD:.gitmodules", limit)
			if err != nil || ok != want {
				t.Errorf("limit %d, blob %d: ok=%v err=%v, want %v", limit, limit+delta, ok, err, want)
			}
		}
	}
}

// A blob that cannot be read (here: the loose object is gone) fails closed instead of reading as an empty
// configuration.
func TestPinsMissingGitmodulesBlobFailsClosed(t *testing.T) {
	f := newPinsFixture(t)
	oid := gitIn(t, f.Super, "rev-parse", "HEAD:.gitmodules")
	obj := filepath.Join(f.Super, ".git", "objects", oid[:2], oid[2:])
	if _, err := os.Stat(obj); err != nil {
		t.Skipf("the blob is not loose: %v", err)
	}
	mustOS(t, os.Chmod(filepath.Dir(obj), 0o755))
	mustOS(t, os.Remove(obj))
	reg := registryOf(t, f.Super)
	if reg.Gitmodules.HeadKind != "unparseable" || !reg.Gitmodules.RegistrationUnknown {
		t.Fatalf("%+v", reg.Gitmodules)
	}
	for _, e := range reg.Entries {
		if e.GitmodulesEntry || e.OrphanGitlink {
			t.Fatalf("registration invented from nothing: %+v", e)
		}
	}
}
