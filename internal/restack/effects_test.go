// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEffectsListDescendantRefsWorktreesAndHooks(t *testing.T) {
	s := rebaseMerged(t)
	s.git("branch", "grandchild", s.oldHead)
	s.git("branch", "unrelated", s.base)
	other := filepath.Join(filepath.Dir(s.dir), "other")
	s.git("worktree", "add", "-q", other, "unrelated")
	hooks := filepath.Join(s.dir, ".git", "hooks")
	marker := filepath.Join(t.TempDir(), "ran")
	script := "#!/bin/sh\ntouch '" + marker + "'\n"
	for _, name := range []string{"pre-rebase", "post-rewrite"} {
		if err := os.WriteFile(filepath.Join(hooks, name), []byte(script), 0o000); err != nil {
			t.Fatal(err)
		}
	}
	res := s.plan(t)
	if res.Status != StatusOK {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
	ef := res.Effects
	if len(ef.AffectedRefs) != 1 || ef.AffectedRefs[0].Ref != "refs/heads/grandchild" || ef.AffectedRefs[0].OID != s.oldHead || !ef.AffectedRefs[0].ContainsOldHead {
		t.Fatalf("affected refs %+v", ef.AffectedRefs)
	}
	states := map[string]string{}
	for _, w := range ef.Worktrees {
		states[w.Path] = w.State
	}
	if states[s.dir] != "target" || states[other] != "other" || len(states) != 2 {
		t.Fatalf("worktrees %+v", ef.Worktrees)
	}
	present := map[string]bool{}
	for _, h := range ef.Hooks {
		present[h.Name] = h.Present
		if h.RunBySTW || h.WouldRunByPorcelainRebase != h.Present {
			t.Fatalf("hook %+v", h)
		}
	}
	if len(ef.Hooks) != 4 || !present["pre-rebase"] || !present["post-rewrite"] || present["reference-transaction"] || present["post-checkout"] {
		t.Fatalf("hooks %+v", ef.Hooks)
	}
	if ef.Writes != (Writes{Objects: "quarantine-only"}) {
		t.Fatalf("writes %+v", ef.Writes)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a hook ran")
	}
	// An affected ref makes the outcome need attention.
	if res.Outcome == OutcomeClean {
		t.Fatal("a descendant ref must keep the outcome out of evidence-clean")
	}
}

func TestLeaseAndResyncTexts(t *testing.T) {
	s := rebaseMerged(t)
	s.git("branch", "feat/x", s.oldHead)
	o := s.options()
	o.Branch = "refs/heads/feat/x"
	res := Plan(context.Background(), o)
	if res.Status != StatusOK || res.Texts.LeasePush == nil || res.Texts.Resync == nil {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
	lp, rs := res.Texts.LeasePush, res.Texts.Resync
	wantPush := "git push --force-with-lease='refs/heads/feat/x:" + s.oldHead + "' 'origin' \"${NEW_FULL_SHA:?set to the full new commit id}:refs/heads/feat/x\""
	if lp.Text != wantPush || lp.ObservedOldSHA != s.oldHead || lp.NewSHAKnown || lp.Assignment != "NEW_FULL_SHA=   # set to the full 40-hex id of the new branch tip before running the next line" {
		t.Fatalf("lease %+v", lp)
	}
	if rs.Fetch != "git fetch --prune" || rs.RebaseOnto != "git rebase --onto 'origin/feat/x' '"+s.oldHead+"' 'feat/x'" ||
		rs.ResetHard != "git switch 'feat/x' && git reset --hard 'origin/feat/x'" || !strings.Contains(rs.DestructiveNote, "DESTRUCTIVE") {
		t.Fatalf("resync %+v", rs)
	}
	for _, text := range []string{lp.Text, rs.Fetch, rs.RebaseOnto, rs.ResetHard} {
		if strings.ContainsAny(text, "<>") {
			t.Errorf("unquoted angle bracket in %q", text)
		}
		if strings.Contains(strings.ReplaceAll(text, "--force-with-lease='refs", ""), "--force") {
			t.Errorf("--force in %q", text)
		}
	}
}

func TestTextsAreOmittedForUnsafeValues(t *testing.T) {
	for _, name := range []string{"a'b", "a$b", "a`b", "a\"b", "a\\b", "a;b", "a&b", "a|b", "a(b)", "a{b}", "a!b", "a#b", "a<b", "aéb"} {
		s := rebaseMerged(t)
		if _, code := s.gitCode("check-ref-format", "refs/heads/"+name); code != 0 {
			continue // not a valid ref name at all
		}
		s.git("branch", name, s.oldHead)
		o := s.options()
		o.Branch = "refs/heads/" + name
		res := Plan(context.Background(), o)
		if res.Status != StatusOK || res.Texts.LeasePush != nil || res.Texts.Resync != nil {
			t.Errorf("%q: texts emitted: %+v", name, res.Texts)
			continue
		}
		found := false
		for _, d := range res.Diagnostics {
			found = found || d.Code == "text-omitted"
		}
		if !found {
			t.Errorf("%q: no diagnostic %+v", name, res.Diagnostics)
		}
	}
}
