// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/inspection"
)

func planFrom(t *testing.T, source, caller string) Result {
	t.Helper()
	return planFromBase(t, source, caller, "HEAD")
}

func planFromBase(t *testing.T, source, caller, base string) Result {
	t.Helper()
	state := gitTempDir(t)
	if e := os.Chmod(state, 0700); e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(gitTempDir(t), "task")
	return PlanCreate(context.Background(), PlanOptions{Path: source, CallerDir: caller, Base: base, Destination: dest, Task: "ctx", Owner: "fixture", StateDir: state})
}

func TestPlanNamesSourceResolvedBaseAndCallerOnAnotherWorktree(t *testing.T) {
	source := fixtureGit(t)
	other := filepath.Join(gitTempDir(t), "other")
	gitFixture(t, source, "worktree", "add", "-q", "--detach", other)
	gitFixture(t, other, "commit", "-q", "--allow-empty", "-m", "other work")
	sourceHead := gitFixture(t, source, "rev-parse", "HEAD")
	otherHead := gitFixture(t, other, "rev-parse", "HEAD")
	r := planFrom(t, source, other)
	if r.Status != "ok" || r.Source == nil {
		t.Fatalf("plan %+v", r)
	}
	s := r.Source
	if s.SourcePath != r.Plan.SourcePath || s.BaseSelector != "HEAD" || s.BaseCommit != sourceHead || s.SourceHead != sourceHead {
		t.Fatalf("source facts %+v want head %s", s, sourceHead)
	}
	realOther, _ := filepath.EvalSymlinks(other)
	if s.CallerDirectory != realOther || !s.CallerDiffers || s.CallerHead != otherHead {
		t.Fatalf("caller facts %+v want %s %s", s, realOther, otherHead)
	}
	if strings.Contains(s.Notice, `"`) {
		t.Fatalf("notice carries its own quotes, the renderer quotes once: %q", s.Notice)
	}
	if strings.Contains(s.Notice, "than the source") || !strings.Contains(s.Notice, "as the source") {
		t.Fatalf("notice wording %q", s.Notice)
	}
	for _, want := range []string{sourceHead, otherHead, realOther, r.Plan.SourcePath} {
		if !strings.Contains(s.Notice, want) {
			t.Fatalf("notice %q lacks %q", s.Notice, want)
		}
	}
}

func TestPlanSameCheckoutUnrelatedAndMissingCallerGiveNoNotice(t *testing.T) {
	source := fixtureGit(t)
	sub := filepath.Join(source, "sub")
	if e := os.Mkdir(sub, 0700); e != nil {
		t.Fatal(e)
	}
	stranger := fixtureGit(t)
	for name, caller := range map[string]string{"same": source, "subdirectory": sub, "unrelated repository": stranger, "not a repository": t.TempDir()} {
		r := planFrom(t, source, caller)
		if r.Status != "ok" || r.Source == nil {
			t.Fatalf("%s: %+v", name, r)
		}
		if r.Source.CallerDiffers || r.Source.Notice != "" {
			t.Fatalf("%s: unexpected notice %+v", name, r.Source)
		}
	}
}

func TestPlanDefaultsTheCallerToTheCurrentDirectory(t *testing.T) {
	source := fixtureGit(t)
	other := filepath.Join(gitTempDir(t), "other")
	gitFixture(t, source, "worktree", "add", "-q", "--detach", other)
	t.Chdir(other)
	r := planFrom(t, source, "")
	if r.Status != "ok" || r.Source == nil || !r.Source.CallerDiffers || r.Source.Notice == "" {
		t.Fatalf("no notice from the working directory: %+v", r)
	}
}

// The base commit is the resolved base, not the source HEAD: they differ here.
func TestPlanReportsTheResolvedBaseCommitNotTheSourceHead(t *testing.T) {
	source := fixtureGit(t)
	first := gitFixture(t, source, "rev-parse", "HEAD")
	gitFixture(t, source, "commit", "-q", "--allow-empty", "-m", "second")
	head := gitFixture(t, source, "rev-parse", "HEAD")
	gitFixture(t, source, "update-ref", "refs/heads/older", first)
	for _, base := range []string{first, "refs/heads/older"} {
		r := planFromBase(t, source, "", base)
		if r.Status != "ok" || r.Source == nil {
			t.Fatalf("%s: %+v", base, r)
		}
		if r.Source.BaseSelector != base || r.Source.BaseCommit != first || r.Source.SourceHead != head || r.Source.BaseCommit == r.Source.SourceHead {
			t.Fatalf("%s: %+v want base %s head %s", base, r.Source, first, head)
		}
	}
}

func TestSourceContextNoticesAnotherJJWorkspaceOfTheSameRepository(t *testing.T) {
	root := gitTempDir(t)
	repo, src, other, stranger := filepath.Join(root, "repo"), filepath.Join(root, "src"), filepath.Join(root, "other"), filepath.Join(root, "stranger")
	for _, d := range []string{repo, src, other, stranger} {
		if e := os.Mkdir(d, 0700); e != nil {
			t.Fatal(e)
		}
	}
	callers := map[string]*inspection.JJRepository{
		other:    {RepositoryDir: repo, WorkspaceRoot: other, CommitID: strings.Repeat("c", 40)},
		src:      {RepositoryDir: repo, WorkspaceRoot: src, CommitID: strings.Repeat("d", 40)},
		stranger: {RepositoryDir: stranger, WorkspaceRoot: stranger, CommitID: strings.Repeat("e", 40)},
	}
	c := coordinator{inspect: func(_ context.Context, path string) inspection.Result {
		return inspection.Result{Status: "ok", Backend: "jj", JJRepository: callers[path]}
	}}
	p := MutationPlan{Backend: "jj", SourcePath: src, Expected: Snapshot{BaseSelector: "@", BaseCommit: strings.Repeat("b", 40), Identity: NativeIdentity{Backend: "jj", RepositoryDir: repo, WorkspaceRoot: src, CommitID: strings.Repeat("a", 40)}}}
	got := c.sourceContext(context.Background(), other, p)
	if !got.CallerDiffers || got.CallerHead != strings.Repeat("c", 40) || got.CallerCheckout != other || !strings.Contains(got.Notice, strings.Repeat("a", 40)) || !strings.Contains(got.Notice, strings.Repeat("c", 40)) {
		t.Fatalf("jj notice: %+v", got)
	}
	for name, caller := range map[string]string{"same workspace": src, "other repository": stranger} {
		if got = c.sourceContext(context.Background(), caller, p); got.CallerDiffers || got.Notice != "" {
			t.Fatalf("%s: %+v", name, got)
		}
	}
	// A git caller never counts for a jj plan.
	g := coordinator{inspect: func(context.Context, string) inspection.Result {
		return inspection.Result{Status: "ok", Backend: "git"}
	}}
	if got = g.sourceContext(context.Background(), other, p); got.CallerDiffers {
		t.Fatalf("backend mismatch: %+v", got)
	}
}

func TestSourceContextAndNextCallKeysAppearOnPlanResultsOnly(t *testing.T) {
	for _, command := range []string{"workspace-apply", "workspace-handoff", "workspace-retire-plan"} {
		raw, err := json.Marshal(newResult(command))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "source_context") {
			t.Fatalf("%s carries source_context: %s", command, raw)
		}
	}
}
