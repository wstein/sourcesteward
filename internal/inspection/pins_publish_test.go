// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type publishCase struct {
	run   *pinsRunner
	repo  pinsGitRepo
	dirs  pinsSubmoduleDirs
	facts pinsRepoFacts
	f     pinsFixture
}

func newPublishCase(t *testing.T) publishCase {
	t.Helper()
	f := newPinsFixture(t)
	run, repo, d := validatedMember(t, f)
	return publishCase{run: run, repo: repo, dirs: d, f: f}
}

func (c *publishCase) publish(t *testing.T, pin string) pinsPublication {
	t.Helper()
	facts, err := pinsReadFacts(c.run, c.dirs)
	if err != nil {
		t.Fatal(err)
	}
	c.facts = facts
	p, err := pinsPublish(c.run, c.repo, c.dirs, facts, pin)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func refNames(p pinsPublication) []string {
	var out []string
	for _, r := range p.Refs {
		out = append(out, r.Remote+" "+r.Ref)
	}
	return out
}

func TestPinsPublicationReachableFromARemoteTrackingRef(t *testing.T) {
	c := newPublishCase(t)
	p := c.publish(t, c.f.Pin)
	// origin/HEAD is a symref that also contains the pin: it must not appear or use a slot.
	if p.Status != "reachable" || p.Truncated || p.Reason != "" || strings.Join(refNames(p), ",") != "origin refs/remotes/origin/main" {
		t.Fatalf("%+v", p)
	}
}

func TestPinsPublicationUnreachableWhenOnlyLocalRefsContainThePin(t *testing.T) {
	c := newPublishCase(t)
	local := commitIn(t, c.repo.WorkTree, "local-only")
	git(t, c.repo.WorkTree, "branch", "local-branch")
	git(t, c.repo.WorkTree, "tag", "local-tag")
	git(t, c.repo.WorkTree, "tag", "-a", "-m", "annotated", "local-annotated")
	p := c.publish(t, local)
	if p.Status != "unreachable" || len(p.Refs) != 0 || p.Reason != "" || p.Truncated {
		t.Fatalf("local branches and tags must never count: %+v", p)
	}
	// The published ancestor stays reachable.
	if p := c.publish(t, c.f.Pin); p.Status != "reachable" {
		t.Fatalf("%+v", p)
	}
}

func TestPinsPublicationAbsentOrNonCommitObjectIsUnknown(t *testing.T) {
	c := newPublishCase(t)
	for name, oid := range map[string]string{
		"absent": strings.Repeat("ab", 20),
		"blob":   gitIn(t, c.repo.WorkTree, "rev-parse", "HEAD:file.txt"),
		"tree":   gitIn(t, c.repo.WorkTree, "rev-parse", "HEAD^{tree}"),
	} {
		p := c.publish(t, oid)
		if p.Status != "unknown" || p.Reason == "" || !p.ObjectAbsent || len(p.Refs) != 0 {
			t.Fatalf("%s: %+v", name, p)
		}
	}
}

func TestPinsPublicationForkOnlyContainment(t *testing.T) {
	c := newPublishCase(t)
	wt := c.repo.WorkTree
	local := commitIn(t, wt, "fork-only")
	git(t, wt, "remote", "add", "fork", "https://example.invalid/fork.git")
	git(t, wt, "update-ref", "refs/remotes/fork/topic", local)
	p := c.publish(t, local)
	if p.Status != "reachable" || strings.Join(refNames(p), ",") != "fork refs/remotes/fork/topic" {
		t.Fatalf("%+v", p)
	}
}

func TestPinsPublicationMultiRemoteNamesEachRefsRemote(t *testing.T) {
	c := newPublishCase(t)
	wt := c.repo.WorkTree
	git(t, wt, "remote", "add", "fork", "https://example.invalid/fork.git")
	git(t, wt, "remote", "add", "team/a", "https://example.invalid/team-a.git")
	for _, ref := range []string{"refs/remotes/fork/main", "refs/remotes/team/a/release/1.0", "refs/remotes/stray/x"} {
		git(t, wt, "update-ref", ref, c.f.Pin)
	}
	p := c.publish(t, c.f.Pin)
	want := "fork refs/remotes/fork/main,origin refs/remotes/origin/main,stray refs/remotes/stray/x,team/a refs/remotes/team/a/release/1.0"
	if p.Status != "reachable" || strings.Join(refNames(p), ",") != want {
		t.Fatalf("%v", refNames(p))
	}
}

func TestPinsPublicationNamesAtMostSixteenRefsAndFlagsTruncation(t *testing.T) {
	c := newPublishCase(t)
	wt := c.repo.WorkTree
	for i := 0; i < 15; i++ { // origin/main makes 16 real refs
		git(t, wt, "update-ref", fmt.Sprintf("refs/remotes/origin/b%02d", i), c.f.Pin)
	}
	p := c.publish(t, c.f.Pin)
	if p.Status != "reachable" || len(p.Refs) != 16 || p.Truncated {
		t.Fatalf("exactly 16 is not truncated: %d %v", len(p.Refs), p.Truncated)
	}
	git(t, wt, "update-ref", "refs/remotes/origin/zzz", c.f.Pin)
	p = c.publish(t, c.f.Pin)
	if len(p.Refs) != 16 || !p.Truncated {
		t.Fatalf("%d %v", len(p.Refs), p.Truncated)
	}
	// Symbolic refs filtered out do not use up the 16.
	for i := 0; i < 20; i++ {
		git(t, wt, "symbolic-ref", fmt.Sprintf("refs/remotes/origin/AAA%02d", i), "refs/remotes/origin/main")
	}
	p = c.publish(t, c.f.Pin)
	if len(p.Refs) != 16 || !p.Truncated {
		t.Fatalf("symbolic refs used up slots: %d %v", len(p.Refs), p.Truncated)
	}
	for _, r := range p.Refs {
		if strings.Contains(r.Ref, "AAA") || strings.HasSuffix(r.Ref, "/HEAD") {
			t.Fatalf("symbolic ref listed: %v", r)
		}
	}
}

func TestPinsPublicationNonUTF8RefIsEscapedPerFieldNotRefused(t *testing.T) {
	c := newPublishCase(t)
	// Git accepts non-UTF-8 bytes in a ref name; the result must carry an escaped form, never fail whole.
	bad := filepath.Join(c.repo.GitDir, "refs", "remotes", "origin", "caf\xe9")
	mustOS(t, os.WriteFile(bad, []byte(c.f.Pin+"\n"), 0o600))
	p := c.publish(t, c.f.Pin)
	found := false
	for _, r := range p.Refs {
		found = found || r.Ref == `refs/remotes/origin/caf\xe9`
	}
	if !found {
		t.Fatalf("%v", refNames(p))
	}
}

func TestPinsPublicationLastFetchIsAdvisoryAndReadOnly(t *testing.T) {
	c := newPublishCase(t)
	if p := c.publish(t, c.f.Pin); p.LastFetch != nil {
		t.Fatalf("no FETCH_HEAD, no time: %v", *p.LastFetch)
	}
	fh := filepath.Join(c.repo.GitDir, "FETCH_HEAD")
	writeFile(t, fh, "ignored\n")
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	mustOS(t, os.Chtimes(fh, when, when))
	p := c.publish(t, c.f.Pin)
	if p.LastFetch == nil || *p.LastFetch != "2026-01-02T03:04:05Z" {
		t.Fatalf("%v", p.LastFetch)
	}
	// A symlinked FETCH_HEAD is never followed.
	rmAll(t, fh)
	mustOS(t, os.Symlink("/etc/hostname", fh))
	if p := c.publish(t, c.f.Pin); p.LastFetch != nil {
		t.Fatalf("followed a symlink: %v", *p.LastFetch)
	}
}

func TestPinsPublicationStartsOnlyAllowlistedGitCommands(t *testing.T) {
	c := newPublishCase(t)
	gitLog, _ := pinsSentinels(t)
	c.publish(t, c.f.Pin)
	requireAllowlisted(t, gitLog(), "cat-file -e <OID>^{commit}", "for-each-ref --contains <OID> --count=64 --format=%(refname)%09%(symref)%00 refs/remotes/")
}

func TestPinsPublicationChecksTheObjectBeforeAskingForContainment(t *testing.T) {
	c := newPublishCase(t)
	gitLog, _ := pinsSentinels(t)
	c.publish(t, strings.Repeat("cd", 20))
	for _, l := range gitLog() {
		if strings.Contains(l, "for-each-ref") {
			t.Fatalf("containment asked for an absent object (exit 129 would look like a failure): %s", l)
		}
	}
}

func TestPinsPublicationCancellationIsAnError(t *testing.T) {
	c := newPublishCase(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pinsPublish(newPinsRunner(ctx), c.repo, c.dirs, pinsRepoFacts{}, c.f.Pin); err == nil {
		t.Fatal("cancellation became an answer")
	}
}

func TestPinsPublicationShallowCannotClaimUnreachable(t *testing.T) {
	f := newPinsFixture(t)
	git(t, f.Sub, "config", "uploadpack.allowAnySHA1InWant", "true")
	c2 := commitIn(t, f.Sub, "two")
	commitIn(t, f.Sub, "three")
	dst, repo := cloneIn(t, f.Sub, "--depth", "1", "--branch", "main")
	// What `git submodule update --depth 1` does for an older pin: fetch it by name, shallow.
	git(t, dst, "fetch", "-q", "--depth", "1", "origin", c2)
	run := newPinsRunner(context.Background())
	d := pinsSubmoduleDirs{GitDir: repo.GitDir, WorkTree: repo.WorkTree, gitBase: dst, gitRel: ".git"}
	facts, err := pinsReadFacts(run, d)
	if err != nil || !facts.Shallow {
		t.Fatalf("%+v %v", facts, err)
	}
	p, err := pinsPublish(run, repo, d, facts, c2)
	if err != nil || p.Status != "unknown" || !strings.Contains(p.Reason, "shallow") || p.ObjectAbsent {
		t.Fatalf("a cut history is not evidence of absence: %+v %v", p, err)
	}
	// A positive stays positive.
	head := mustHead(t, run, repo)
	if p, _ = pinsPublish(run, repo, d, facts, *head.Commit); p.Status != "reachable" {
		t.Fatalf("%+v", p)
	}
}

// --no-replace-objects covers refs/replace only; info/grafts rewrites parents too, and with it a local
// commit could be made to look contained in a remote-tracking ref.
func TestPinsPublicationGraftsAreNeverHonored(t *testing.T) {
	c := newPublishCase(t)
	wt := c.repo.WorkTree
	local := commitIn(t, wt, "local-only")
	git(t, wt, "reset", "-q", "--hard", c.f.Pin)
	writeFile(t, filepath.Join(c.repo.GitDir, "info", "grafts"), c.f.Pin+" "+local+"\n")
	if out := git(t, wt, "-c", "advice.graftFileDeprecated=false", "rev-list", "--count", "refs/remotes/origin/main"); strings.TrimSpace(out) != "2" {
		t.Skipf("this Git does not honor info/grafts (%s): nothing to defend", strings.TrimSpace(out))
	}
	p := c.publish(t, local)
	if p.Status != "unreachable" {
		t.Fatalf("a graft made a local commit look published: %+v", p)
	}
	_, rel := relationOf(t, c.run, c.repo, c.dirs, local)
	if rel.Relation != "behind" { // HEAD is the pin, an ancestor of the local commit once grafts are ignored
		t.Fatalf("%+v", rel)
	}
}

func TestPinsPublicationRetainsOnlyTheRefsItCanName(t *testing.T) {
	c := newPublishCase(t)
	wt := c.repo.WorkTree
	for i := 0; i < 50; i++ {
		git(t, wt, "update-ref", fmt.Sprintf("refs/remotes/origin/%s%02d", strings.Repeat("r", 150), i), c.f.Pin)
	}
	c.run.budget.retained = 0
	p := c.publish(t, c.f.Pin)
	if len(p.Refs) != 16 || !p.Truncated {
		t.Fatalf("%d %v", len(p.Refs), p.Truncated)
	}
	if c.run.budget.retained > 17*200 {
		t.Fatalf("charged the shared budget for refs it cannot name: %d bytes", c.run.budget.retained)
	}
}

// craftedCommitGraph writes a commit-graph and makes it claim that `child` has no parent.
func craftedCommitGraph(t *testing.T, wt, gitDir, child string) {
	t.Helper()
	git(t, wt, "commit-graph", "write", "--reachable")
	path := filepath.Join(gitDir, "objects", "info", "commit-graph")
	b, err := os.ReadFile(path)
	mustOS(t, err)
	n := int(b[6])
	chunk := func(id string) int {
		for i := 0; i < n; i++ {
			e := 8 + i*12
			if string(b[e:e+4]) == id {
				var off uint64
				for _, c := range b[e+4 : e+12] {
					off = off<<8 | uint64(c)
				}
				return int(off)
			}
		}
		t.Fatalf("no %s chunk", id)
		return 0
	}
	oidl, cdat := chunk("OIDL"), chunk("CDAT")
	raw, err := hex.DecodeString(child)
	mustOS(t, err)
	pos := -1
	for i := 0; oidl+i*len(raw)+len(raw) <= cdat; i++ {
		if bytes.Equal(b[oidl+i*len(raw):oidl+(i+1)*len(raw)], raw) {
			pos = i
		}
	}
	if pos < 0 {
		t.Fatal("commit not in the graph")
	}
	p1 := cdat + pos*(len(raw)+16) + len(raw)
	copy(b[p1:p1+4], []byte{0x70, 0, 0, 0}) // GRAPH_PARENT_NONE
	mustOS(t, os.Chmod(path, 0o644))
	mustOS(t, os.WriteFile(path, b, 0o644))
}

func TestPinsPublicationCommitGraphIsNeverTrusted(t *testing.T) {
	c := newPublishCase(t)
	wt := c.repo.WorkTree
	tip := commitIn(t, wt, "two")
	git(t, wt, "update-ref", "refs/remotes/origin/main", tip)
	craftedCommitGraph(t, wt, c.repo.GitDir, tip)
	// Control: plain Git trusts the forged graph and loses the connection.
	if out := git(t, wt, "for-each-ref", "--contains", c.f.Pin, "--format=%(refname)", "refs/remotes/origin/main"); strings.TrimSpace(out) != "" {
		t.Skipf("this Git does not use the forged graph (%q): nothing to defend", out)
	}
	if p := c.publish(t, c.f.Pin); p.Status != "reachable" {
		t.Fatalf("a forged commit-graph changed the answer: %+v", p)
	}
	_, rel := relationOf(t, c.run, c.repo, c.dirs, c.f.Pin)
	if rel.Relation != "behind" && rel.Relation != "ahead" && rel.Relation != "same" {
		t.Fatalf("%+v", rel)
	}
}
