// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGitCompleteMetadataExistingConsumers(t *testing.T) {
	// The public operations apply their own fixed 10 s and 60 s deadlines, which
	// a 695-entry tree can exceed when other packages saturate the machine. The
	// deadlines are production policy, so quick runs skip this load-sensitive case.
	if testing.Short() {
		t.Skip("load-sensitive 695-entry fixture under fixed operation deadlines")
	}
	d := fixtureGitLargeMetadata(t)
	// Native v4 prefix compression keeps registration index metadata within its
	// unchanged 64 KiB cap; this does not change full ls-tree evidence dimensions.
	gitFixture(t, d, "config", "index.version", "4")
	gitFixture(t, d, "update-index", "--index-version", "4")
	ctx := context.Background()
	state := gitTempDir(t)
	if e := os.Chmod(state, 0700); e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(gitTempDir(t), "owned")
	planned := PlanCreate(ctx, PlanOptions{Path: d, Base: "HEAD", Destination: dest, Task: "metadata", Owner: "fixture", StateDir: state})
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatalf("creation plan: %+v", planned)
	}
	created := Apply(ctx, ApplyOptions{StateDir: state, PlanID: planned.Plan.PlanID, Authorize: true})
	if created.Status != "ok" || created.Task == nil {
		t.Fatalf("creation apply: %+v", created)
	}
	// Existing reference tidy and owned retirement must see the same complete
	// selected-base metadata. Native index/content/manifest caps remain separate.
	if _, e := (gitTidyAdapter{}).Observe(ctx, d, "HEAD"); e != nil {
		t.Fatalf("reference tidy: %v", e)
	}
	observed, e := (gitRetirementAdapter{}).Observe(ctx, d, "HEAD", *created.Task, *planned.Plan)
	if e != nil || !observed.Manifest.Complete || !observed.Metadata.Complete {
		t.Fatalf("owned target/base admission: %v", e)
	}
	indexInfo, e := os.Stat(filepath.Join(created.Task.Native.GitDir, "index"))
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("Git %s index-v4=%d bytes; tree=67877 bytes/695 leaves; old metadata cap=%d", gitFixture(t, d, "--version"), indexInfo.Size(), nativeOutputLimit)
	if len(observed.Manifest.Entries) != 696 {
		t.Fatalf("manifest omitted leaves: %d", len(observed.Manifest.Entries))
	}
	// Existing public v1/v2 operation dispatch remains unchanged. The 10 s bound of one
	// PlanTidy call is a production deadline; this fixture needs about 2 s on a developer
	// machine but over 10 s on a shared two-core runner under -race, so only the bound of
	// this test is widened.
	previous := tidyPlanTimeout
	tidyPlanTimeout = 120 * time.Second
	t.Cleanup(func() { tidyPlanTimeout = previous })
	refs := PlanTidy(ctx, TidyPlanOptions{Path: d, Base: "HEAD"})
	if refs.Status != "ok" || refs.SchemaVersion != 1 {
		t.Fatalf("old reference result: %+v", refs)
	}
	retired := PlanTidy(ctx, TidyPlanOptions{Path: d, Base: "HEAD", Task: "metadata", Owner: "fixture", StateDir: state, RemoveRegistration: true, RemoveFiles: dest})
	if retired.Status != "ok" || retired.SchemaVersion != 2 {
		t.Fatalf("old retirement result: %+v", retired)
	}
}

func TestGitCompleteMetadataFinalAttributeChangeHasNoEffects(t *testing.T) {
	for _, value := range []string{"active", "unset"} {
		t.Run(value, func(t *testing.T) {
			d := fixtureGitLargeMetadata(t)
			before, e := (gitAdapter{}).Observe(context.Background(), d, "HEAD")
			if e != nil {
				t.Fatal(e)
			}
			entries, e := os.ReadDir(d)
			if e != nil {
				t.Fatal(e)
			}
			final := entries[len(entries)-1].Name()
			attr := final + " filter=active\n"
			if value == "unset" {
				attr = final + " -filter\n"
			}
			if e = os.WriteFile(filepath.Join(d, ".git", "info", "attributes"), []byte(attr), 0600); e != nil {
				t.Fatal(e)
			}
			dest := gitTempDir(t)
			out, e := (gitAdapter{}).Create(context.Background(), MutationPlan{TaskID: "stale-final", SourcePath: d, Destination: dest, Expected: before})
			if e == nil || len(out.Effects) != 0 {
				t.Fatalf("changed final filter was actionable: %+v %v", out, e)
			}
			left, e := os.ReadDir(dest)
			if e != nil || len(left) != 0 {
				t.Fatal("refusal changed destination", e)
			}
		})
	}
}

func TestGitCompleteMetadataKeepsIndependentRegistrationCap(t *testing.T) {
	d := fixtureGitLargeMetadata(t)
	dest := filepath.Join(gitTempDir(t), "default-index")
	gitFixture(t, d, "worktree", "add", "--detach", "--lock", "--reason", "stw:metadata", dest, "HEAD")
	bin, e := gitExecutable()
	if e != nil {
		t.Fatal(e)
	}
	id, e := gitNativeIdentity(context.Background(), dest)
	if e != nil {
		t.Fatal(e)
	}
	st, e := os.Stat(filepath.Join(id.GitDir, "index"))
	if e != nil {
		t.Fatal(e)
	}
	if st.Size() <= nativeOutputLimit {
		t.Fatalf("fixture index must exercise independent cap: %d", st.Size())
	}
	if _, e = gitRetirementStoredFiles(context.Background(), bin, id); e != nil {
		t.Fatalf("complete tree/index collector: %v", e)
	}
	if _, e = gitRetirementMetadata(context.Background(), id.GitDir); e == nil {
		t.Fatal("oversized linked registration index was admitted")
	}
	t.Logf("Git %s default index=%d bytes; complete tree passes; separate metadata cap=%d refuses", gitFixture(t, d, "--version"), st.Size(), nativeOutputLimit)
}
