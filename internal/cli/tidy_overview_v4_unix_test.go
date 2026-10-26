// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

type overviewFileWitness struct {
	info  os.FileInfo
	bytes string
}

func overviewTree(t *testing.T, root string) map[string]overviewFileWitness {
	t.Helper()
	out := map[string]overviewFileWitness{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		var data []byte
		if info.Mode().IsRegular() {
			data, err = os.ReadFile(p)
		} else if info.Mode()&os.ModeSymlink != 0 {
			var target string
			target, err = os.Readlink(p)
			data = []byte(target)
		}
		if err != nil {
			return err
		}
		out[p] = overviewFileWitness{info: info, bytes: string(data)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func overviewUnchanged(t *testing.T, before, after map[string]overviewFileWitness) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("tree entry count changed: %d -> %d", len(before), len(after))
	}
	for path, b := range before {
		a, ok := after[path]
		if !ok || !os.SameFile(b.info, a.info) || b.info.Mode() != a.info.Mode() || b.info.Size() != a.info.Size() || !b.info.ModTime().Equal(a.info.ModTime()) || b.bytes != a.bytes {
			t.Fatalf("read-only overview changed %s", path)
		}
	}
}

func TestTidyOverviewV4NativeReadOnlyMissingRegistration(t *testing.T) {
	r := pruneCLIRepository(t)
	pruneCLIGit(t, r.source, "remote", "add", "origin", "https://example.invalid/fixture.git")
	pruneCLIGit(t, r.source, "update-ref", "refs/remotes/origin/main", "HEAD")
	pruneCLIGit(t, r.source, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	t.Setenv("STW_STATE_DIR", r.state)
	before := []map[string]overviewFileWitness{overviewTree(t, r.source), overviewTree(t, r.state), overviewTree(t, r.moved)}
	var out, diag bytes.Buffer
	code := Run(context.Background(), "dev", []string{"tidy", "--workspace", r.source, "--state-dir", r.state, "--contract", "4", "--json"}, &out, &diag)
	if code != 0 {
		t.Fatalf("native overview code=%d diagnostics=%s output=%s", code, diag.String(), out.String())
	}
	d := overviewDocument(t, out.String())
	if d.SchemaVersion != 4 || d.PlanID != "" || len(d.RegistrationPrunes) != 1 {
		t.Fatalf("native overview: %+v", d)
	}
	row := d.RegistrationPrunes[0]
	if row.Name != r.name || row.GitDir != r.gitdir || row.Destination != r.destination || row.Reason != "registration-prune-ready" || row.Proof != "owned-missing-registration" || len(row.Effects) != 1 || row.Effects[0] != "remove-registration" || len(row.EvidenceDigest) != 64 {
		t.Fatalf("native proof lost: %+v", row)
	}
	primary := false
	for _, c := range d.Candidates {
		if string(c.Kind) == "worktree" && c.Path == r.source {
			primary = true
		}
		if c.Path == r.destination {
			t.Fatalf("registration counted again as legacy row: %+v", c)
		}
	}
	if !primary {
		t.Fatal("primary checkout disappeared from legacy candidates")
	}
	for i, root := range []string{r.source, r.state, r.moved} {
		overviewUnchanged(t, before[i], overviewTree(t, root))
	}
}
