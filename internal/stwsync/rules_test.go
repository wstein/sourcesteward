// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	ownerName  = "Olive Owner"
	ownerEmail = "owner@example.com"
	owner      = ownerName + " <" + ownerEmail + ">"
	noreply    = ownerName + " <12345+owner@users.noreply.github.com>"
	stranger   = "Sam Stranger <sam@example.net>"
)

// commitAs commits FILE with explicit author and committer identities, set per command and never in a configuration.
func commitAs(t *testing.T, dir, file, content, author, committer string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, filepath.FromSlash(file))), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, filepath.FromSlash(file)), content)
	an, ae := splitIdent(author)
	cn, ce := splitIdent(committer)
	for _, args := range [][]string{{"add", "--", file}, {"commit", "-q", "-m", "change " + file}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME="+an, "GIT_AUTHOR_EMAIL="+ae, "GIT_COMMITTER_NAME="+cn, "GIT_COMMITTER_EMAIL="+ce)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func dump(r *Rules) string {
	raw, _ := json.Marshal(r)
	return string(raw)
}

func splitIdent(s string) (name, email string) {
	n, e, _ := strings.Cut(s, " <")
	return n, strings.TrimSuffix(e, ">")
}

// rulesFixture is a bare origin, a checkout of it ("rules") and a second clone that publishes new commits.
type rulesFixture struct {
	root, bare, checkout, pusher, home string
}

func newRulesFixture(t *testing.T) *rulesFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &rulesFixture{root: root, bare: filepath.Join(root, "agent-rules.git"), checkout: filepath.Join(root, "rules"), pusher: filepath.Join(root, "pusher"), home: filepath.Join(root, "home")}
	runGit(t, root, "init", "-q", "--bare", "-b", "main", f.bare)
	runGit(t, root, "clone", "-q", f.bare, f.pusher)
	runGit(t, f.pusher, "checkout", "-q", "-b", "main")
	commitAs(t, f.pusher, "README.md", "one\n", owner, owner)
	runGit(t, f.pusher, "push", "-q", "origin", "main")
	runGit(t, root, "clone", "-q", f.bare, f.checkout)
	if err := os.MkdirAll(filepath.Join(f.home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

// publish commits FILE to origin/main through the pusher clone.
func (f *rulesFixture) publish(t *testing.T, file, author, committer string) {
	t.Helper()
	commitAs(t, f.pusher, file, "new "+file+"\n", author, committer)
	runGit(t, f.pusher, "push", "-q", "origin", "main")
}

func (f *rulesFixture) options(extra func(*Options)) Options {
	o := Options{Rules: false, RulesPath: f.checkout, RulesPathSource: "flag", OwnerIdentities: []string{owner}, AllowedCommitters: []string{noreply}, Home: f.home}
	if extra != nil {
		extra(&o)
	}
	return o
}

func (f *rulesFixture) report(t *testing.T, o Options) *Rules {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	s := &session{opts: o}
	r := s.rules(context.Background(), env{bin, f.root}, nil)
	if r == nil {
		t.Fatal("no rules report")
	}
	return r
}

func (f *rulesFixture) head(t *testing.T) string { return runGit(t, f.checkout, "rev-parse", "HEAD") }

func TestRulesStates(t *testing.T) {
	t.Run("current", func(t *testing.T) {
		f := newRulesFixture(t)
		r := f.report(t, f.options(nil))
		if r.State != RulesCurrent || r.Behind != 0 || r.Ahead != 0 || r.Source != "flag" || r.AsOfLastFetch {
			t.Fatalf("%s", dump(r))
		}
	})
	t.Run("behind counts new commits and changes nothing", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "a.md", owner, owner)
		f.publish(t, "b.md", owner, owner)
		before := f.head(t)
		r := f.report(t, f.options(nil))
		if r.State != RulesBehind || r.Behind != 2 || r.Ahead != 0 || r.Outcome != OutcomeNone {
			t.Fatalf("%s", dump(r))
		}
		if f.head(t) != before {
			t.Fatal("a display run moved HEAD")
		}
		if _, err := os.Stat(filepath.Join(f.checkout, "a.md")); err == nil {
			t.Fatal("a display run changed the worktree")
		}
	})
	t.Run("diverged", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "remote.md", owner, owner)
		commitAs(t, f.checkout, "local.md", "x\n", owner, owner)
		if r := f.report(t, f.options(nil)); r.State != RulesDiverged || r.Ahead != 1 || r.Behind != 1 {
			t.Fatalf("%s", dump(r))
		}
	})
	t.Run("ahead", func(t *testing.T) {
		f := newRulesFixture(t)
		commitAs(t, f.checkout, "local.md", "x\n", owner, owner)
		if r := f.report(t, f.options(nil)); r.State != RulesAhead || r.Ahead != 1 {
			t.Fatalf("%s", dump(r))
		}
	})
	t.Run("dirty", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "remote.md", owner, owner)
		write(t, filepath.Join(f.checkout, "README.md"), "edited\n")
		if r := f.report(t, f.options(nil)); r.State != RulesDirty {
			t.Fatalf("%s", dump(r))
		}
	})
	t.Run("not on the default branch", func(t *testing.T) {
		f := newRulesFixture(t)
		runGit(t, f.checkout, "checkout", "-q", "-b", "topic")
		if r := f.report(t, f.options(nil)); r.State != RulesNotDefault || r.Branch == nil || *r.Branch != "topic" {
			t.Fatalf("%s", dump(r))
		}
	})
	t.Run("detached", func(t *testing.T) {
		f := newRulesFixture(t)
		runGit(t, f.checkout, "checkout", "-q", "--detach")
		if r := f.report(t, f.options(nil)); r.State != RulesNotDefault || r.Branch != nil {
			t.Fatalf("%s", dump(r))
		}
	})
	t.Run("not found", func(t *testing.T) {
		f := newRulesFixture(t)
		for name, path := range map[string]string{"missing": filepath.Join(f.root, "nope"), "not a repository": f.home, "subdirectory": filepath.Join(f.checkout, ".git")} {
			r := f.report(t, f.options(func(o *Options) { o.RulesPath = path }))
			if r.State != RulesNotFound || r.Reason == nil {
				t.Fatalf("%s: %s", name, dump(r))
			}
		}
	})
	t.Run("nothing requested and nothing found is silent", func(t *testing.T) {
		f := newRulesFixture(t)
		bin, _ := exec.LookPath("git")
		s := &session{opts: Options{Home: f.home}}
		if r := s.rules(context.Background(), env{bin, f.root}, nil); r != nil {
			t.Fatalf("%s", dump(r))
		}
	})
	t.Run("fetch failure reports the last fetch", func(t *testing.T) {
		f := newRulesFixture(t)
		runGit(t, f.checkout, "remote", "set-url", "origin", filepath.Join(f.root, "gone.git"))
		r := f.report(t, f.options(nil))
		if r.State != RulesCurrent || !r.AsOfLastFetch {
			t.Fatalf("%s", dump(r))
		}
	})
}

func TestRulesFromWorkspaceMember(t *testing.T) {
	f := newRulesFixture(t)
	w := newWorkspace(t)
	runGit(t, w.ws, "submodule", "add", "-q", f.bare, "agent-rules")
	bin, _ := exec.LookPath("git")
	repos := []Repo{{Name: ".", Path: w.ws, Kind: "root"}, {Name: "agent-rules", Path: filepath.Join(w.ws, "agent-rules"), Kind: "submodule"}, {Name: "a", Path: filepath.Join(w.ws, "a"), Kind: "submodule"}}
	s := &session{opts: Options{Home: f.home}}
	r := s.rules(context.Background(), env{bin, w.ws}, repos)
	if r == nil || r.State != RulesCurrent || r.Source != "workspace" || r.Path == nil || !samePath(*r.Path, filepath.Join(w.ws, "agent-rules")) {
		t.Fatalf("%s", dump(r))
	}
	// the flag wins over the workspace member
	s = &session{opts: Options{Home: f.home, RulesPath: f.checkout, RulesPathSource: "flag"}}
	if r = s.rules(context.Background(), env{bin, w.ws}, repos); r.Source != "flag" || !samePath(*r.Path, f.checkout) {
		t.Fatalf("%s", dump(r))
	}
}

func TestRulesUpdate(t *testing.T) {
	update := func(o *Options) { o.Rules = true }
	t.Run("fast-forward", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "a.md", owner, owner)
		f.publish(t, "claude/CLAUDE.md", owner, noreply)
		f.publish(t, "claude/skills/x/SKILL.md", owner, owner)
		f.publish(t, "install.sh", owner, owner)
		f.publish(t, "hooks/pre.sh", owner, owner)
		old := f.head(t)
		want := runGit(t, f.pusher, "rev-parse", "HEAD")
		r := f.report(t, f.options(update))
		if r.Outcome != OutcomeFastForwarded || r.OldOID == nil || *r.OldOID != old || r.NewOID == nil || *r.NewOID != want {
			t.Fatalf("%s", dump(r))
		}
		if f.head(t) != want {
			t.Fatal("HEAD is not the origin tip")
		}
		if parents := strings.Fields(runGit(t, f.checkout, "rev-list", "--parents", "-n1", "HEAD")); len(parents) != 2 {
			t.Fatalf("a merge commit was made: %v", parents)
		}
		if len(r.Commits) != 5 || r.Commits[0].Subject == "" {
			t.Fatalf("commits %+v", r.Commits)
		}
		sensitive := map[string]bool{}
		for _, file := range r.Files {
			sensitive[file.Path] = file.Sensitive
		}
		for path, want := range map[string]bool{"a.md": false, "claude/CLAUDE.md": true, "claude/skills/x/SKILL.md": true, "install.sh": true, "hooks/pre.sh": true} {
			if got, ok := sensitive[path]; !ok || got != want {
				t.Fatalf("%s: sensitive=%v present=%v", path, got, ok)
			}
		}
		joined := strings.Join(r.Notes, "\n")
		if !strings.Contains(joined, "applies to NEW sessions; running agents keep their loaded rules") {
			t.Fatalf("notes %q", joined)
		}
		if after := f.report(t, f.options(nil)); after.State != RulesCurrent {
			t.Fatalf("%s", dump(after))
		}
	})
	t.Run("up to date needs no change", func(t *testing.T) {
		f := newRulesFixture(t)
		if r := f.report(t, f.options(update)); r.Outcome != OutcomeUpToDate {
			t.Fatalf("%s", dump(r))
		}
	})
	t.Run("dry run lists and changes nothing", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "claude/CLAUDE.md", owner, owner)
		old := f.head(t)
		r := f.report(t, f.options(func(o *Options) { o.Rules, o.Plan = true, true }))
		if r.Outcome != OutcomeWouldFastForward || len(r.Commits) != 1 || len(r.Files) != 1 || !r.Files[0].Sensitive {
			t.Fatalf("%s", dump(r))
		}
		if f.head(t) != old {
			t.Fatal("a dry run moved HEAD")
		}
	})
	refuse := func(t *testing.T, f *rulesFixture, want string) {
		t.Helper()
		old := f.head(t)
		state := worktreeState(t, f.checkout)
		r := f.report(t, f.options(update))
		if r.Outcome != OutcomeRefused || r.Reason == nil || !strings.Contains(*r.Reason, want) {
			t.Fatalf("want a refusal containing %q: %s", want, dump(r))
		}
		if f.head(t) != old || worktreeState(t, f.checkout) != state {
			t.Fatal("a refused run changed the checkout")
		}
	}
	t.Run("refuses a dirty worktree", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "a.md", owner, owner)
		write(t, filepath.Join(f.checkout, "README.md"), "edited\n")
		refuse(t, f, "uncommitted changes")
	})
	t.Run("refuses a colliding untracked file", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "a.md", owner, owner)
		write(t, filepath.Join(f.checkout, "a.md"), "mine\n")
		refuse(t, f, "untracked")
	})
	t.Run("an untracked file that does not collide is no obstacle", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "a.md", owner, owner)
		write(t, filepath.Join(f.checkout, "scratch.txt"), "mine\n")
		if r := f.report(t, f.options(update)); r.Outcome != OutcomeFastForwarded {
			t.Fatalf("%s", dump(r))
		}
	})
	t.Run("refuses another branch", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "a.md", owner, owner)
		runGit(t, f.checkout, "checkout", "-q", "-b", "topic")
		refuse(t, f, "not on the default branch")
	})
	t.Run("refuses a diverged checkout", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "remote.md", owner, owner)
		commitAs(t, f.checkout, "local.md", "x\n", owner, owner)
		refuse(t, f, "diverged")
	})
	t.Run("refuses a foreign author", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "a.md", stranger, owner)
		refuse(t, f, "author")
	})
	t.Run("refuses a foreign committer", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "a.md", owner, stranger)
		refuse(t, f, "committer")
	})
	t.Run("one foreign commit among owner commits is enough", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "a.md", owner, owner)
		f.publish(t, "b.md", stranger, stranger)
		f.publish(t, "c.md", owner, owner)
		refuse(t, f, "sam@example.net")
	})
	t.Run("the noreply committer is refused unless configured", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "a.md", owner, noreply)
		old := f.head(t)
		r := f.report(t, f.options(func(o *Options) { o.Rules, o.AllowedCommitters = true, nil }))
		if r.Outcome != OutcomeRefused || f.head(t) != old {
			t.Fatalf("%s", dump(r))
		}
		if r = f.report(t, f.options(update)); r.Outcome != OutcomeFastForwarded {
			t.Fatalf("%s", dump(r))
		}
	})
	t.Run("an allowed committer is no allowed author", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "a.md", noreply, noreply)
		refuse(t, f, "author")
	})
	t.Run("refuses without any owner identity", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "a.md", owner, owner)
		old := f.head(t)
		r := f.report(t, f.options(func(o *Options) { o.Rules, o.OwnerIdentities, o.AllowedCommitters = true, nil, nil }))
		if r.Outcome != OutcomeRefused || f.head(t) != old {
			t.Fatalf("%s", dump(r))
		}
	})
	t.Run("the default owner identity is the Git configuration of the workspace", func(t *testing.T) {
		f := newRulesFixture(t)
		f.publish(t, "a.md", owner, owner)
		runGit(t, f.root, "init", "-q", "ws")
		ws := filepath.Join(f.root, "ws")
		runGit(t, ws, "config", "user.name", ownerName)
		runGit(t, ws, "config", "user.email", ownerEmail)
		bin, _ := exec.LookPath("git")
		s := &session{opts: f.options(func(o *Options) { o.Rules, o.OwnerIdentities, o.AllowedCommitters = true, nil, nil })}
		if r := s.rules(context.Background(), env{bin, ws}, nil); r.Outcome != OutcomeFastForwarded {
			t.Fatalf("%s", dump(r))
		}
	})
}

func TestRulesInstallDetection(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(t *testing.T, f *rulesFixture)
		want  string
		hint  bool
	}{
		"absent": {func(t *testing.T, f *rulesFixture) {}, "absent", false},
		"copy": {func(t *testing.T, f *rulesFixture) {
			write(t, filepath.Join(f.home, ".claude", "CLAUDE.md"), "copy\n")
		}, "copy", true},
		"symlink to the checkout": {func(t *testing.T, f *rulesFixture) {
			if err := os.MkdirAll(filepath.Join(f.checkout, "claude"), 0o755); err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(f.checkout, "claude", "CLAUDE.md"), "rules\n")
			if err := os.Symlink(filepath.Join(f.checkout, "claude", "CLAUDE.md"), filepath.Join(f.home, ".claude", "CLAUDE.md")); err != nil {
				t.Fatal(err)
			}
		}, "symlink", false},
		"symlink elsewhere": {func(t *testing.T, f *rulesFixture) {
			write(t, filepath.Join(f.root, "other.md"), "x\n")
			if err := os.Symlink(filepath.Join(f.root, "other.md"), filepath.Join(f.home, ".claude", "CLAUDE.md")); err != nil {
				t.Fatal(err)
			}
		}, "copy", true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newRulesFixture(t)
			tc.setup(t, f)
			f.publish(t, "a.md", owner, owner)
			before := snapshotTree(t, f.home)
			r := f.report(t, f.options(func(o *Options) { o.Rules = true }))
			if r.Install != tc.want {
				t.Fatalf("install %q, want %q", r.Install, tc.want)
			}
			hint := strings.Contains(strings.Join(r.Notes, "\n"), "run install.sh --dry-run first (owner action)")
			if hint != tc.hint {
				t.Fatalf("hint=%v want %v: %q", hint, tc.hint, r.Notes)
			}
			after := snapshotTree(t, f.home)
			if len(after) != len(before) {
				t.Fatal("the home directory was written")
			}
			for k, v := range before {
				if after[k] != v {
					t.Fatalf("%s changed", k)
				}
			}
		})
	}
}

func TestRulesNeverWritesGitConfiguration(t *testing.T) {
	f := newRulesFixture(t)
	f.publish(t, "a.md", owner, owner)
	read := func() string {
		raw, err := os.ReadFile(filepath.Join(f.checkout, ".git", "config"))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	before := read()
	if r := f.report(t, f.options(func(o *Options) { o.Rules = true })); r.Outcome != OutcomeFastForwarded {
		t.Fatalf("%s", dump(r))
	}
	if read() != before {
		t.Fatal("the Git configuration of the checkout changed")
	}
}

func TestRulesOptionsValidation(t *testing.T) {
	for name, o := range map[string]Options{
		"leading dash":       {RulesPath: "-x"},
		"dot dot":            {RulesPath: "a/../b"},
		"NUL":                {RulesPath: "a\x00b"},
		"identity no email":  {OwnerIdentities: []string{"nope"}},
		"identity newline":   {OwnerIdentities: []string{"A\nB <a@b.c>"}},
		"committer no email": {AllowedCommitters: []string{"x"}},
		"flag source":        {RulesPath: "a", RulesPathSource: "other"},
	} {
		if err := ValidateOptions(o); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for name, o := range map[string]Options{
		"space in path": {RulesPath: "/tmp/my rules/agent-rules"},
		"identity":      {OwnerIdentities: []string{owner}, AllowedCommitters: []string{noreply}},
	} {
		if err := ValidateOptions(o); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSyncReportsRulesInTheResult(t *testing.T) {
	f := newRulesFixture(t)
	w := newWorkspace(t)
	f.publish(t, "a.md", owner, owner)
	r := Sync(context.Background(), Options{Path: w.ws, Plan: true, RulesPath: f.checkout, RulesPathSource: "flag", Home: f.home})
	if r.Error != nil || r.Rules == nil || r.Rules.State != RulesCurrent || !r.Rules.AsOfLastFetch {
		t.Fatalf("%+v %s", r.Error, dump(r.Rules))
	}
	if r.ExitCode() != 0 {
		t.Fatal("a stale rules checkout is no failure")
	}
	r = Sync(context.Background(), Options{Path: w.ws, Rules: true, RulesPath: f.checkout, RulesPathSource: "flag", Home: f.home, OwnerIdentities: []string{owner}})
	if r.Rules == nil || r.Rules.Outcome != OutcomeFastForwarded || !r.Options.Rules {
		t.Fatalf("%s", dump(r.Rules))
	}
	t.Setenv("STW_RULES_PATH", "")
	if none := Sync(context.Background(), Options{Path: w.ws, Plan: true, Home: f.home}); none.Rules != nil {
		t.Fatalf("%s", dump(none.Rules))
	}
}

func memberWorkspace(t *testing.T, f *rulesFixture) *workspace {
	t.Helper()
	w := newWorkspace(t)
	runGit(t, w.ws, "submodule", "add", "-q", f.bare, "agent-rules")
	runGit(t, w.ws, "commit", "-q", "-m", "add agent-rules")
	f.publish(t, "claude/CLAUDE.md", stranger, stranger) // a commit the identity guard would refuse
	return w
}

// A normal sync never moves the agent-rules checkout: only --rules does, and only after the identity check.
func TestOrdinarySyncLeavesTheAgentRulesMemberAlone(t *testing.T) {
	for name, tc := range map[string]struct {
		opts func(f *rulesFixture, w *workspace) Options
		path func(f *rulesFixture, w *workspace) string
	}{
		"member": {func(f *rulesFixture, w *workspace) Options { return Options{Path: w.ws, Home: f.home} },
			func(f *rulesFixture, w *workspace) string { return filepath.Join(w.ws, "agent-rules") }},
		"member with --submodules": {func(f *rulesFixture, w *workspace) Options {
			return Options{Path: w.ws, Home: f.home, Submodules: true}
		},
			func(f *rulesFixture, w *workspace) string { return filepath.Join(w.ws, "agent-rules") }},
	} {
		t.Run(name, func(t *testing.T) {
			f := newRulesFixture(t)
			w := memberWorkspace(t, f)
			o := tc.opts(f, w)
			path := tc.path(f, w)
			before := runGit(t, path, "rev-parse", "HEAD")
			r := Sync(context.Background(), o)
			if r.Error != nil {
				t.Fatal(r.Error.Message)
			}
			if got := runGit(t, path, "rev-parse", "HEAD"); got != before {
				t.Fatalf("an ordinary sync moved the rules checkout to %s", got)
			}
			if rp := repoByName(t, r)["agent-rules"]; rp.Result != ResultSkippedRules {
				t.Fatalf("result %q", rp.Result)
			}
		})
	}
}

func TestRulesUpdateIsJournaled(t *testing.T) {
	f := newRulesFixture(t)
	w := newWorkspace(t)
	old := f.head(t)
	f.publish(t, "a.md", owner, owner)
	want := runGit(t, f.pusher, "rev-parse", "HEAD")
	r := Sync(context.Background(), Options{Path: w.ws, Rules: true, RulesPath: f.checkout, RulesPathSource: "flag", Home: f.home, OwnerIdentities: []string{owner}})
	if r.Rules == nil || r.Rules.Outcome != OutcomeFastForwarded || r.RunID == nil {
		t.Fatalf("%s run=%v", dump(r.Rules), r.RunID)
	}
	raw, err := os.ReadFile(filepath.Join(f.checkout, ".git", "stw", "sync", *r.RunID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec journalRecord
	if err := json.Unmarshal(raw, &rec); err != nil || len(rec.Entries) != 1 {
		t.Fatalf("%v %s", err, raw)
	}
	en := rec.Entries[0]
	if en.State != stateCompleted || en.OldOID != old || en.NewOID != want || en.Ref != "refs/heads/main" {
		t.Fatalf("%+v", en)
	}
	if got := runGit(t, f.checkout, "rev-parse", en.RecoveryRef); got != old {
		t.Fatalf("recovery ref %s", got)
	}
	// a refusal leaves no journal
	f.publish(t, "b.md", stranger, stranger)
	r = Sync(context.Background(), Options{Path: w.ws, Rules: true, RulesPath: f.checkout, RulesPathSource: "flag", Home: f.home, OwnerIdentities: []string{owner}})
	if r.Rules.Outcome != OutcomeRefused || r.RunID != nil {
		t.Fatalf("%s run=%v", dump(r.Rules), r.RunID)
	}
}

func TestStatusReportsRulesOnRequestOnly(t *testing.T) {
	f := newRulesFixture(t)
	w := newWorkspace(t)
	f.publish(t, "a.md", owner, owner)
	before := f.head(t)
	off := Status(context.Background(), StatusOptions{Path: w.ws})
	if off.Rules != nil {
		t.Fatalf("unrequested: %s", dump(off.Rules))
	}
	if raw, _ := json.Marshal(off); strings.Contains(string(raw), `"rules"`) {
		t.Fatal("the unrequested status document changed")
	}
	// requested without --fetch: the local origin state only, nothing fetched, nothing changed
	r := Status(context.Background(), StatusOptions{Path: w.ws, Rules: true, RulesPath: f.checkout, RulesPathSource: "flag", Home: f.home})
	if r.Rules == nil || r.Rules.State != RulesCurrent || !r.Rules.AsOfLastFetch {
		t.Fatalf("%s", dump(r.Rules))
	}
	// requested with --fetch: sees the new commit and still changes nothing
	r = Status(context.Background(), StatusOptions{Path: w.ws, Fetch: true, Rules: true, RulesPath: f.checkout, RulesPathSource: "flag", Home: f.home})
	if r.Rules == nil || r.Rules.State != RulesBehind || r.Rules.Behind != 1 || r.Rules.Outcome != OutcomeNone {
		t.Fatalf("%s", dump(r.Rules))
	}
	if f.head(t) != before {
		t.Fatal("status changed the rules checkout")
	}
	// requested but not found
	r = Status(context.Background(), StatusOptions{Path: w.ws, Rules: true, Home: f.home})
	if r.Rules == nil || r.Rules.State != RulesNotFound {
		t.Fatalf("%s", dump(r.Rules))
	}
}

// otherMember adds the submodule "docs" (a remote not named agent-rules), advances its remote and returns its path.
func otherMember(t *testing.T, f *rulesFixture, w *workspace) string {
	t.Helper()
	other := filepath.Join(f.root, "other.git")
	runGit(t, f.root, "clone", "-q", "--bare", f.bare, other)
	runGit(t, w.ws, "submodule", "add", "-q", other, "docs")
	runGit(t, w.ws, "commit", "-q", "-m", "add docs")
	adv := filepath.Join(f.root, "adv-other")
	runGit(t, f.root, "clone", "-q", other, adv)
	commitAs(t, adv, "docs.md", "x\n", owner, owner)
	runGit(t, adv, "push", "-q", "origin", "main")
	return filepath.Join(w.ws, "docs")
}

// The checkout named by --rules-path or STW_RULES_PATH is left alone by the ordinary sync even when its remote has another name.
func TestOrdinarySyncLeavesTheNamedRulesPathAlone(t *testing.T) {
	for _, mode := range []string{"flag", "env", "symlink", "flag with --submodules"} {
		t.Run(mode, func(t *testing.T) {
			f := newRulesFixture(t)
			w := newWorkspace(t)
			path := otherMember(t, f, w)
			before := runGit(t, path, "rev-parse", "HEAD")
			o := Options{Path: w.ws, Home: f.home, RulesPath: path, RulesPathSource: "flag"}
			switch mode {
			case "env":
				t.Setenv("STW_RULES_PATH", path)
				o = WithRulesEnvironment(Options{Path: w.ws, Home: f.home})
			case "symlink":
				link := filepath.Join(f.root, "link")
				if err := os.Symlink(path, link); err != nil {
					t.Fatal(err)
				}
				o.RulesPath = link
			case "flag with --submodules":
				o.Submodules = true
			}
			r := Sync(context.Background(), o)
			if r.Error != nil {
				t.Fatal(r.Error.Message)
			}
			if got := runGit(t, path, "rev-parse", "HEAD"); got != before {
				t.Fatalf("moved to %s", got)
			}
			if rp := repoByName(t, r)["docs"]; rp.Result != ResultSkippedRules {
				t.Fatalf("result %q", rp.Result)
			}
		})
	}
}

// An ordinary sync (and --plan) fetches nothing in the agent-rules checkout; only --rules does.
func TestOrdinarySyncDoesNotFetchTheAgentRulesCheckout(t *testing.T) {
	f := newRulesFixture(t)
	w := memberWorkspace(t, f)
	member := filepath.Join(w.ws, "agent-rules")
	ref := func() string { return runGit(t, member, "rev-parse", "refs/remotes/origin/main") }
	before := ref()
	for name, o := range map[string]Options{"run": {Path: w.ws, Home: f.home}, "plan": {Path: w.ws, Home: f.home, Plan: true}, "submodules": {Path: w.ws, Home: f.home, Submodules: true}} {
		r := Sync(context.Background(), o)
		if r.Error != nil || r.Rules == nil || r.Rules.State != RulesCurrent || !r.Rules.AsOfLastFetch {
			t.Fatalf("%s: %v %s", name, r.Error, dump(r.Rules))
		}
		if got := ref(); got != before {
			t.Fatalf("%s: the remote-tracking ref moved to %s", name, got)
		}
	}
	r := Sync(context.Background(), Options{Path: w.ws, Home: f.home, Rules: true, OwnerIdentities: []string{owner}})
	if r.Rules == nil || r.Rules.AsOfLastFetch || ref() == before {
		t.Fatalf("--rules fetches: %s", dump(r.Rules))
	}
}

func TestOrdinarySyncStillListsTheSubmodulesOfTheSkippedRulesRepository(t *testing.T) {
	f := newRulesFixture(t)
	w := newWorkspace(t)
	plain := Sync(context.Background(), Options{Path: w.ws, Plan: true, Home: f.home})
	r := Sync(context.Background(), Options{Path: w.ws, Plan: true, Home: f.home, RulesPath: w.ws, RulesPathSource: "flag"})
	if r.Error != nil || len(r.Repos) != len(plain.Repos) || len(r.Repos) < 5 {
		t.Fatalf("%v %d repositories, plain %d", r.Error, len(r.Repos), len(plain.Repos))
	}
	by := repoByName(t, r)
	if by["."].Result != ResultSkippedRules || by["a"].Result == ResultSkippedRules {
		t.Fatalf("%+v %+v", by["."], by["a"])
	}
}

func TestRulesFetchHonorsTheRefspecCheck(t *testing.T) {
	f := newRulesFixture(t)
	runGit(t, f.checkout, "config", "remote.origin.fetch", "+refs/heads/*:refs/mirror/*")
	f.publish(t, "a.md", owner, owner)
	r := f.report(t, f.options(nil))
	if !r.AsOfLastFetch {
		t.Fatalf("fetched through an unsafe refspec: %s", dump(r))
	}
	if out, err := exec.Command("git", "-C", f.checkout, "rev-parse", "--verify", "-q", "refs/mirror/main").Output(); err == nil {
		t.Fatalf("a local ref was written: %s", out)
	}
}

func TestRulesRefusesWhileAnOperationIsInProgress(t *testing.T) {
	f := newRulesFixture(t)
	f.publish(t, "a.md", owner, owner)
	if err := os.MkdirAll(filepath.Join(f.checkout, ".git", "rebase-merge"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := f.head(t)
	r := f.report(t, f.options(func(o *Options) { o.Rules = true }))
	if r.Outcome != OutcomeRefused || r.State != RulesDirty || r.Reason == nil || !strings.Contains(*r.Reason, "rebase is in progress") || f.head(t) != old {
		t.Fatalf("%s", dump(r))
	}
}

func TestRulesRefusesMoreNewCommitsThanItCanCheck(t *testing.T) {
	old := rulesCommitLimit
	rulesCommitLimit = 2
	t.Cleanup(func() { rulesCommitLimit = old })
	f := newRulesFixture(t)
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		f.publish(t, name, owner, owner)
	}
	before := f.head(t)
	r := f.report(t, f.options(func(o *Options) { o.Rules = true }))
	if r.Outcome != OutcomeRefused || r.Reason == nil || !strings.Contains(*r.Reason, "more than 2 new commits") || f.head(t) != before {
		t.Fatalf("%s", dump(r))
	}
}

// If the checkout moves between the checks and the merge, merge --ff-only fails and makes no merge commit.
func TestRulesMergeIsFastForwardOnly(t *testing.T) {
	f := newRulesFixture(t)
	f.publish(t, "a.md", owner, owner)
	rulesBeforeMerge = func(dir string) { commitAs(t, dir, "raced.md", "x\n", owner, owner) }
	t.Cleanup(func() { rulesBeforeMerge = nil })
	r := f.report(t, f.options(func(o *Options) { o.Rules = true }))
	if r.Outcome != OutcomeFailed {
		t.Fatalf("%s", dump(r))
	}
	if parents := strings.Fields(runGit(t, f.checkout, "rev-list", "--parents", "-n1", "HEAD")); len(parents) != 2 {
		t.Fatalf("a merge commit was made: %v", parents)
	}
}

// A post-merge hook runs after the merge (Spec 032) and can move HEAD: the result must not claim the checked commit then.
func TestRulesFailsWhenAHookMovesHeadAfterTheMerge(t *testing.T) {
	f := newRulesFixture(t)
	f.publish(t, "a.md", owner, owner)
	hookScript(t, filepath.Join(f.checkout, ".git"), "post-merge", "git -c user.name=X -c user.email=x@x commit -q --allow-empty -m unchecked\n")
	checked := runGit(t, f.pusher, "rev-parse", "HEAD")
	r := f.report(t, f.options(func(o *Options) { o.Rules = true }))
	moved := f.head(t)
	if moved == checked {
		t.Fatal("the hook did not move HEAD")
	}
	if r.Outcome != OutcomeFailed || r.NewOID != nil || r.Reason == nil || !strings.Contains(*r.Reason, checked[:7]) || !strings.Contains(*r.Reason, moved[:7]) {
		t.Fatalf("%s", dump(r))
	}
	for _, n := range r.Notes {
		if strings.Contains(n, "applies to NEW sessions") {
			t.Fatalf("success note on a failed update: %q", n)
		}
	}
}

// The message names where the invalid path came from.
func TestRulesPathErrorsNameTheirSource(t *testing.T) {
	for _, tc := range []struct{ source, bad, want string }{
		{"env", "../x", "STW_RULES_PATH"}, {"env", "-x", "STW_RULES_PATH"}, {"env", "a\x00b", "STW_RULES_PATH"},
		{"flag", "../x", "--rules-path"}, {"flag", "-x", "--rules-path"}, {"flag", "a\x00b", "--rules-path"},
	} {
		err := ValidateOptions(Options{RulesPath: tc.bad, RulesPathSource: tc.source})
		if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("%s %q: %v", tc.source, tc.bad, err)
		}
		if tc.source == "env" && err != nil && strings.Contains(err.Error(), "--rules-path") {
			t.Errorf("%q: the environment error names the flag: %v", tc.bad, err)
		}
	}
}
