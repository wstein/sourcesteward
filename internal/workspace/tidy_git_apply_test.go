// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitTidyScopedMetadataProtectsSiblingFiles(t *testing.T) {
	d := fixtureGit(t)
	id, e := gitNativeIdentity(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	gitFixture(t, d, "branch", "deletable", "HEAD")
	gitFixture(t, d, "branch", "keep", "HEAD")
	for _, ref := range []string{"refs/heads/deletable", "refs/stw/recovery/plan/candidate"} {
		protection, e := gitTidyWriteProtection(id, []string{ref})
		if e != nil {
			t.Fatal(e)
		}
		foundConfig, foundKeep := false, false
		for _, p := range protection {
			if p == filepath.Join(id.RepositoryDir, "config") {
				foundConfig = true
			}
			if sibling := filepath.Join(id.RepositoryDir, "refs", "heads", "keep"); p == sibling || strings.HasPrefix(sibling, p+string(filepath.Separator)) {
				foundKeep = true
			}
		}
		if !foundConfig || !foundKeep {
			t.Fatalf("mutable ref grant exposed siblings: %v", protection)
		}
	}
}

func TestGitTidySandboxReadOnlyRegularFile(t *testing.T) {
	d := gitTempDir(t)
	file := filepath.Join(d, "protected")
	if e := os.WriteFile(file, []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	bin, e := filepath.EvalSymlinks("/bin/sh")
	if e != nil {
		t.Fatal(e)
	}
	_, code, e := runSandboxed(context.Background(), bin, []string{"-c", "printf changed > '" + file + "'"}, d, []string{d}, []string{file})
	if e != nil {
		t.Fatal(e)
	}
	if code == 0 {
		t.Fatal("protected file writable")
	}
	got, e := os.ReadFile(file)
	if e != nil || string(got) != "keep" {
		t.Fatal("protected contents changed")
	}
}

func TestGitTidyWriteProtectionRefusesCollisionsAndSymlinks(t *testing.T) {
	d := fixtureGit(t)
	id, e := gitNativeIdentity(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	for _, ref := range []string{"refs/heads/x.lock", "refs/heads/../config", "HEAD", "refs/heads/"} {
		if _, e := gitTidyWriteProtection(id, []string{ref}); e == nil {
			t.Fatalf("unsafe target admitted %q", ref)
		}
	}
	outside := gitTempDir(t)
	if e := os.Symlink(outside, filepath.Join(id.RepositoryDir, "refs", "foreign")); e != nil {
		t.Fatal(e)
	}
	if _, e := gitTidyWriteProtection(id, []string{"refs/heads/x"}); e == nil {
		t.Fatal("symlink metadata admitted")
	}
	if _, e := os.Stat(filepath.Join(outside, "x")); !os.IsNotExist(e) {
		t.Fatal("foreign metadata changed")
	}
}

func gitTidyApplyFixture(t *testing.T) (string, TidyPlanRecord, TidyCandidate) {
	d := fixtureGit(t)
	gitFixture(t, d, "branch", "deletable", "HEAD")
	gitFixture(t, d, "branch", "keep", "HEAD")
	gitFixture(t, d, "commit", "--allow-empty", "-qm", "advance base")
	gitFixture(t, d, "pack-refs", "--all")
	o, e := (gitTidyAdapter{}).Observe(context.Background(), d, "HEAD")
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range o.Candidates {
		if c.Name == "refs/heads/deletable" {
			return d, TidyPlanRecord{PlanID: "native-packed", Backend: "git", SourcePath: d, Expected: o.Expected}, c
		}
	}
	t.Fatal("missing fixture candidate")
	return "", TidyPlanRecord{}, TidyCandidate{}
}

func TestGitTidyPackedDeletePreservesRecoveryAndSiblings(t *testing.T) {
	d, p, c := gitTidyApplyFixture(t)
	before := map[string][]byte{}
	for _, name := range []string{"HEAD", "config", "index"} {
		data, e := os.ReadFile(filepath.Join(d, ".git", name))
		if e != nil {
			t.Fatal(e)
		}
		before[name] = data
	}
	recovery, expected, out, e := (gitTidyAdapter{}).PrepareRecovery(context.Background(), p, c)
	if e != nil || !out.Complete || !out.Attempted {
		t.Fatalf("prepare: %+v %v", out, e)
	}
	out, e = (gitTidyAdapter{}).Delete(context.Background(), p, c, recovery, expected)
	if e != nil || !out.Complete || out.Expected == nil {
		t.Fatalf("delete: %+v %v", out, e)
	}
	refs := gitFixture(t, d, "for-each-ref", "--format=%(refname) %(objectname)")
	if strings.Contains(refs, c.Name+" ") || !strings.Contains(refs, recovery.Name+" "+c.CommitID) || !strings.Contains(refs, "refs/heads/keep "+c.CommitID) {
		t.Fatalf("unexpected native refs %s", refs)
	}
	for name, want := range before {
		data, e := os.ReadFile(filepath.Join(d, ".git", name))
		if e != nil || string(data) != string(want) {
			t.Fatalf("source %s changed", name)
		}
	}
}

func TestGitTidyRecoveryCollisionAndStaleDeleteRefuse(t *testing.T) {
	d, p, c := gitTidyApplyFixture(t)
	r, expected, _, e := (gitTidyAdapter{}).PrepareRecovery(context.Background(), p, c)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, _, e := (gitTidyAdapter{}).PrepareRecovery(context.Background(), p, c); e == nil {
		t.Fatal("recovery collision admitted")
	}
	gitFixture(t, d, "update-ref", c.Name, p.Expected.Identity.CommitID)
	if out, e := (gitTidyAdapter{}).Delete(context.Background(), p, c, r, expected); e == nil || out.Attempted {
		t.Fatalf("stale deletion attempted: %+v %v", out, e)
	}
	if got := gitFixture(t, d, "rev-parse", c.Name); got != p.Expected.Identity.CommitID {
		t.Fatal("stale target deleted")
	}
}

func TestGitTidyRefusesSymbolicRecoveryAtSameOID(t *testing.T) {
	d, p, c := gitTidyApplyFixture(t)
	r, expected, _, e := (gitTidyAdapter{}).PrepareRecovery(context.Background(), p, c)
	if e != nil {
		t.Fatal(e)
	}
	gitFixture(t, d, "symbolic-ref", r.Name, "refs/heads/keep")
	if out, e := (gitTidyAdapter{}).Delete(context.Background(), p, c, r, expected); e == nil || out.Attempted {
		t.Fatalf("symbolic recovery admitted %+v %v", out, e)
	}
	if got := gitFixture(t, d, "rev-parse", c.Name); got != c.CommitID {
		t.Fatal("target deleted without independent recovery root")
	}
}

func TestGitTidyRefusesHardlinkedMutableReflogBeforeRecovery(t *testing.T) {
	d, p, c := gitTidyApplyFixture(t)
	sentinel := filepath.Join(gitTempDir(t), "outside-reflog")
	log := filepath.Join(d, ".git", "logs", "refs", "heads", "deletable")
	if e := os.Link(log, sentinel); e != nil {
		t.Fatal(e)
	}
	before, e := os.ReadFile(sentinel)
	if e != nil {
		t.Fatal(e)
	}
	r, _, out, e := (gitTidyAdapter{}).PrepareRecovery(context.Background(), p, c)
	if e == nil || out.Attempted {
		t.Fatalf("hardlinked target log admitted before recovery %+v %v", out, e)
	}
	if strings.Contains(gitFixture(t, d, "for-each-ref", "--format=%(refname)"), r.Name) {
		t.Fatal("native recovery created despite unsafe mutable metadata")
	}
	after, e := os.ReadFile(sentinel)
	if e != nil || string(after) != string(before) {
		t.Fatal("outside hardlink changed")
	}
}

func TestGitTidyDetectsUnrelatedSymbolicRefRetargetAtSameOID(t *testing.T) {
	d, p, c := gitTidyApplyFixture(t)
	gitFixture(t, d, "update-ref", "refs/remotes/origin/one", c.CommitID)
	gitFixture(t, d, "update-ref", "refs/remotes/origin/two", c.CommitID)
	gitFixture(t, d, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/one")
	o, e := (gitTidyAdapter{}).Observe(context.Background(), d, "HEAD")
	if e != nil {
		t.Fatal(e)
	}
	p.Expected = o.Expected
	r, expected, _, e := (gitTidyAdapter{}).PrepareRecovery(context.Background(), p, c)
	if e != nil {
		t.Fatal(e)
	}
	gitFixture(t, d, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/two")
	if out, e := (gitTidyAdapter{}).Delete(context.Background(), p, c, r, expected); e == nil || out.Attempted {
		t.Fatalf("unrelated sameOID symbolic retarget ignored: %+v %v", out, e)
	}
	if got := gitFixture(t, d, "rev-parse", c.Name); got != c.CommitID {
		t.Fatal("target deleted using stale full reference evidence")
	}
}
