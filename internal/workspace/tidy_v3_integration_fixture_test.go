// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// proofFix is a temporary repository with a main branch and helpers for the
// integration proof tests. Every Git call of the fixture is isolated from the
// caller's configuration by fixtureTidyV3Git; the engine under test runs only
// through the planning session, as in production.
type proofFix struct {
	t    *testing.T
	dir  string
	main string
	// bin, when set, replaces the Git executable of the engine under test.
	bin string
}

func newProofFix(t *testing.T) *proofFix {
	t.Helper()
	dir, _ := fixtureTidyV3Git(t)
	f := &proofFix{t: t, dir: dir}
	f.main = f.git("symbolic-ref", "--short", "HEAD")
	return f
}

func (f *proofFix) git(args ...string) string {
	f.t.Helper()
	return gitFixture(f.t, f.dir, args...)
}

func (f *proofFix) write(path, content string) {
	f.t.Helper()
	full := filepath.Join(f.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// commit writes the files (path -> content), commits them and returns the commit.
func (f *proofFix) commit(msg string, files map[string]string) string {
	f.t.Helper()
	for p, c := range files {
		f.write(p, c)
	}
	f.git("add", "-A")
	f.git("commit", "-qm", msg)
	return f.git("rev-parse", "HEAD")
}

func (f *proofFix) branch(name, from string) {
	f.t.Helper()
	f.git("checkout", "-q", "-b", name, from)
}
func (f *proofFix) checkout(name string) { f.t.Helper(); f.git("checkout", "-q", name) }
func (f *proofFix) pick(oid string) string {
	f.t.Helper()
	f.git("cherry-pick", "--allow-empty", "--keep-redundant-commits", oid)
	return f.git("rev-parse", "HEAD")
}
func (f *proofFix) rev(ref string) string { f.t.Helper(); return f.git("rev-parse", ref) }

// verdict evaluates the tip against the base with the given policy, through the
// production planning session, observation and engine.
func (f *proofFix) verdict(base, tip string, policy V3ProofPolicy) integrationVerdict {
	f.t.Helper()
	v, err := f.verdictErr(base, tip, policy, defaultIntegrationEngine())
	if err != nil {
		f.t.Fatalf("evaluate: %v", err)
	}
	return v
}

func (f *proofFix) verdictErr(base, tip string, policy V3ProofPolicy, engine integrationEngine) (integrationVerdict, error) {
	f.t.Helper()
	return f.verdictWith(base, tip, policy, engine, true)
}

// verdictBare evaluates without the planning admission of the base tree, for
// fixtures the admission refuses (symlinks, more than 1,024 files, grafts).
func (f *proofFix) verdictBare(base, tip string, policy V3ProofPolicy) integrationVerdict {
	f.t.Helper()
	v, err := f.verdictWith(base, tip, policy, defaultIntegrationEngine(), false)
	if err != nil {
		f.t.Fatalf("evaluate: %v", err)
	}
	return v
}

func (f *proofFix) verdictWith(base, tip string, policy V3ProofPolicy, engine integrationEngine, observe bool) (integrationVerdict, error) {
	f.t.Helper()
	ctx, _, closeSession, err := withPlanningSession(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	defer closeSession()
	var id NativeIdentity
	var notes []V3ConfigNote
	if observe {
		observation, err := observeTidyV3Git(ctx, f.dir, f.rev(base))
		if err != nil {
			f.t.Fatalf("observe: %v", err)
		}
		id, notes = observation.Expected.Identity, observation.ConfigNotes
	} else if id, err = gitNativeIdentity(ctx, f.dir); err != nil {
		f.t.Fatal(err)
	}
	run, cleanup, err := engine.begin(ctx, id, notes)
	if err != nil {
		f.t.Fatal(err)
	}
	defer cleanup()
	if f.bin != "" {
		run.bin = f.bin
	}
	return run.evaluate(ctx, f.rev(base), f.rev(tip), policy)
}

func patchPolicy(accept bool) V3ProofPolicy {
	return V3ProofPolicy{Version: 1, Kinds: []string{"patch-id"}, AcceptWhitespaceDifferences: accept}
}

// fastImport creates commits quickly; the script is Git fast-import input.
func (f *proofFix) fastImport(script string) {
	f.t.Helper()
	c := exec.Command("git", "-C", f.dir, "fast-import", "--quiet")
	c.Stdin = strings.NewReader(script)
	if out, err := c.CombinedOutput(); err != nil {
		f.t.Fatalf("fast-import: %v %s", err, out)
	}
}

// importChain returns fast-import input for n commits on ref, each adding file
// p<i>, on top of parent (a full commit id); the clock keeps two chains distinct.
func importChain(prefix string, n int, ref, parent string, clock int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		content := fmt.Sprintf("content %s %d\n", prefix, i)
		fmt.Fprintf(&b, "commit %s\nmark :%d\ncommitter T <t@t> %d +0000\ndata 2\nc\n", ref, i+1, clock+i)
		if i == 0 {
			fmt.Fprintf(&b, "from %s\n", parent)
		}
		fmt.Fprintf(&b, "M 100644 inline p%d\ndata %d\n%s\n", i, len(content), content)
	}
	return b.String()
}

func osSymlink(target, link string) error { return os.Symlink(target, link) }
func osRemove(path string) error          { return os.Remove(path) }

// snapshotTree maps every file below dir (relative path to content) except the
// named top-level files, which Git may legitimately touch during fixture use.
func snapshotTree(t *testing.T, dir string, skip ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		for _, s := range skip {
			if rel == s {
				return nil
			}
		}
		if info.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// wrapGit writes an executable that behaves as Git except that merge-tree runs
// the given shell fragment instead.
func wrapGit(t *testing.T, mergeTree string) string {
	t.Helper()
	real, err := gitExecutable()
	if err != nil {
		t.Fatal(err)
	}
	dir := gitTempDir(t)
	path := filepath.Join(dir, "git")
	script := "#!/bin/sh\nfor a in \"$@\"; do if [ \"$a\" = merge-tree ]; then " + mergeTree + "; fi; done\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
