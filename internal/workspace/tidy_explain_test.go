// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func explainCtx() ExplainContext {
	return ExplainContext{Source: "/repo", Base: "refs/heads/main", Contract3: true}
}

// Issue 388: every retain reason has a one-sentence text and a next command.
func TestEveryExplainedReasonHasATextAndANextCommand(t *testing.T) {
	for _, reason := range ExplainedReasons {
		e := ExplainRetention(explainCtx(), "refs/heads/topic", strings.Repeat("a", 40), reason, []string{"/ws/topic"}, []string{"partial-match"})
		if e.Reason != reason || e.Name != "refs/heads/topic" {
			t.Errorf("%s: %+v", reason, e)
		}
		if !strings.HasPrefix(e.Text, "It stays ") || !strings.HasSuffix(e.Text, ".") || strings.Count(e.Text, ". ") != 0 || strings.Contains(e.Text, "for the reason") {
			t.Errorf("%s: text is not one specific sentence: %q", reason, e.Text)
		}
		if !strings.HasPrefix(e.NextCommand, "stw ") && !strings.HasPrefix(e.NextCommand, "git ") {
			t.Errorf("%s: next command %q", reason, e.NextCommand)
		}
	}
	e := ExplainRetention(explainCtx(), "refs/heads/topic", "", "made-up", nil, nil)
	if e.Text == "" || e.NextCommand == "" {
		t.Errorf("an unknown reason has no fallback: %+v", e)
	}
}

func TestExplainNamesTheMissingProofByPlanKind(t *testing.T) {
	ctx := explainCtx()
	ctx.PRProof = "unavailable-offline"
	off := ExplainRetention(ctx, "refs/heads/t", "", "integration-not-proven", nil, nil)
	if !strings.Contains(off.Text, "offline") || !strings.HasSuffix(off.NextCommand, "--online") {
		t.Errorf("offline: %+v", off)
	}
	ctx.PRProof = "available"
	on := ExplainRetention(ctx, "refs/heads/t", "", "integration-not-proven", nil, nil)
	if strings.Contains(on.Text, "offline") || !strings.HasPrefix(on.NextCommand, "git -C /repo log --oneline refs/heads/main..refs/heads/t") {
		t.Errorf("online: %+v", on)
	}
	ctx.Contract3, ctx.PRProof = false, ""
	v1 := ExplainRetention(ctx, "refs/heads/t", "", "integration-not-proven", nil, nil)
	if !strings.Contains(v1.Text, "ancestry only") || !strings.Contains(v1.NextCommand, "--contract 3") {
		t.Errorf("default plan: %+v", v1)
	}
	review := ExplainRetention(explainCtx(), "refs/heads/t", "", "needs-review", nil, []string{"partial-match", "whitespace-differs"})
	if !strings.Contains(review.Text, "(partial-match, whitespace-differs)") || !strings.Contains(review.NextCommand, "--report-dir") {
		t.Errorf("review: %+v", review)
	}
	used := ExplainRetention(ctx, "refs/heads/t", "", "workspace-used", []string{"/ws/a b"}, nil)
	if !strings.Contains(used.Text, "'/ws/a b'") {
		t.Errorf("workspace path not quoted: %+v", used)
	}
}

func TestShellWordQuotes(t *testing.T) {
	for in, want := range map[string]string{"refs/heads/a-b_c.1": "refs/heads/a-b_c.1", "a b": "'a b'", "it's": `'it'\''s'`, "": "''", "$(x)": "'$(x)'"} {
		if got := shellWord(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

// The reasons a real plan produces are all explained specifically, and ExplainV3 covers retained candidates only.
func TestExplainV3CoversTheRetainedCandidatesOfARealPlan(t *testing.T) {
	dir, stale, live, base := missingWorkspaceFixture(t)
	_ = stale
	gitFixture(t, dir, "branch", "unmerged-work", "HEAD~2")
	gitFixture(t, dir, "checkout", "-q", "unmerged-work")
	if err := os.WriteFile(filepath.Join(dir, "unmerged.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, dir, "add", "unmerged.txt")
	gitFixture(t, dir, "commit", "-qm", "unmerged")
	gitFixture(t, dir, "checkout", "-q", strings.TrimPrefix(base, "refs/heads/"))
	r := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: base, StateDir: privateDir(t), ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("plan: %+v", r.Diagnostics)
	}
	got := map[string]V3Explanation{}
	for _, e := range ExplainV3(*r.Plan) {
		got[e.Name] = e
	}
	retained := 0
	for _, c := range r.Plan.Candidates {
		if c.Decision != "retain" {
			if _, ok := got[c.Name]; ok {
				t.Errorf("explained a deleted candidate %s", c.Name)
			}
			continue
		}
		retained++
		e, ok := got[c.Name]
		if !ok || e.Reason != c.Reason || strings.Contains(e.Text, "for the reason") || e.NextCommand == "" {
			t.Errorf("%s (%s): %+v", c.Name, c.Reason, e)
		}
	}
	if retained < 3 || len(got) != retained {
		t.Fatalf("retained %d, explained %d", retained, len(got))
	}
	if e := got["refs/heads/live"]; !strings.Contains(e.Text, live) {
		t.Errorf("worktree path missing: %+v", e)
	}
}
