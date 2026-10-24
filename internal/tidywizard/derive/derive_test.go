// SPDX-License-Identifier: EUPL-1.2
package derive

import (
	"reflect"
	"testing"
)

func TestRemote(t *testing.T) {
	for _, c := range []struct {
		name    string
		remotes []string
		value   string
		source  string
		code    string
		cands   []string
	}{
		{"origin among several", []string{"upstream", "origin"}, "origin", "origin", "", nil},
		{"only origin", []string{"origin"}, "origin", "origin", "", nil},
		{"single other remote", []string{"fork"}, "fork", "single-remote", "", nil},
		{"two remotes without origin", []string{"upstream", "fork"}, "", "", "remote-ambiguous", []string{"fork", "upstream"}},
		{"no remote", nil, "", "", "remote-ambiguous", []string{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := Remote("repo", c.remotes)
			if c.code == "" {
				if r.Ambiguity != nil || r.Derived != (Derived{Name: "remote:repo", Value: c.value, Source: c.source}) {
					t.Fatalf("%+v", r)
				}
				return
			}
			if r.Ambiguity == nil || r.Ambiguity.Code != c.code || r.Ambiguity.Subject != "repo" || !reflect.DeepEqual(r.Ambiguity.Candidates, c.cands) || r.Derived.Value != "" {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestBase(t *testing.T) {
	refs := map[string]bool{"refs/remotes/origin/main": true, "refs/heads/trunk": true, "refs/heads/jjmain": true}
	for _, c := range []struct {
		name     string
		facts    RepoFacts
		remote   string
		override string
		want     Derived
		code     string
	}{
		{"symbolic HEAD present", RepoFacts{Name: "r", Backend: "git", RemoteHead: map[string]string{"origin": "refs/remotes/origin/main"}, Refs: refs}, "origin", "",
			Derived{Name: "base:r", Value: "refs/remotes/origin/main", Source: "refs/remotes/origin/HEAD"}, ""},
		{"symbolic HEAD missing", RepoFacts{Name: "r", Backend: "git", Refs: refs}, "origin", "", Derived{}, "base-ambiguous"},
		{"symbolic HEAD dangling", RepoFacts{Name: "r", Backend: "git", RemoteHead: map[string]string{"origin": "refs/remotes/origin/gone"}, Refs: refs}, "origin", "", Derived{}, "base-ambiguous"},
		{"HEAD of another remote is not used", RepoFacts{Name: "r", Backend: "git", RemoteHead: map[string]string{"upstream": "refs/remotes/origin/main"}, Refs: refs}, "origin", "", Derived{}, "base-ambiguous"},
		{"jj trunk", RepoFacts{Name: "j", Backend: "jj", Trunk: "refs/heads/jjmain", Refs: refs}, "origin", "",
			Derived{Name: "base:j", Value: "refs/heads/jjmain", Source: "jj trunk()"}, ""},
		{"jj without trunk", RepoFacts{Name: "j", Backend: "jj", Refs: refs}, "origin", "", Derived{}, "base-ambiguous"},
		{"jj trunk dangling", RepoFacts{Name: "j", Backend: "jj", Trunk: "refs/heads/none", Refs: refs}, "origin", "", Derived{}, "base-ambiguous"},
		{"override wins over a present HEAD", RepoFacts{Name: "r", Backend: "git", RemoteHead: map[string]string{"origin": "refs/remotes/origin/main"}, Refs: refs}, "origin", "refs/heads/trunk",
			Derived{Name: "base:r", Value: "refs/heads/trunk", Source: "--member-base", Overridden: true}, ""},
		{"override resolves an ambiguous base", RepoFacts{Name: "r", Backend: "git", Refs: refs}, "origin", "refs/heads/trunk",
			Derived{Name: "base:r", Value: "refs/heads/trunk", Source: "--member-base", Overridden: true}, ""},
		{"override naming a missing ref", RepoFacts{Name: "r", Backend: "git", Refs: refs}, "origin", "refs/heads/nope", Derived{}, "base-missing"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := Base(c.facts, c.remote, c.override)
			if c.code != "" {
				if r.Ambiguity == nil || r.Ambiguity.Code != c.code || r.Ambiguity.Subject != c.facts.Name || r.Derived.Value != "" {
					t.Fatalf("%+v", r)
				}
				return
			}
			if r.Ambiguity != nil || r.Derived != c.want {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestWorkspace(t *testing.T) {
	if r := Workspace("/flag", "/found"); r.Derived != (Derived{Name: "workspace", Value: "/flag", Source: "--workspace", Overridden: true}) || r.Ambiguity != nil {
		t.Fatalf("%+v", r)
	}
	if r := Workspace("", "/found"); r.Derived != (Derived{Name: "workspace", Value: "/found", Source: "discovery"}) || r.Ambiguity != nil {
		t.Fatalf("%+v", r)
	}
	if r := Workspace("", ""); r.Ambiguity == nil || r.Ambiguity.Code != "workspace-not-found" || r.Derived.Value != "" {
		t.Fatalf("%+v", r)
	}
}

func TestMembers(t *testing.T) {
	got, notice := Members([]string{"crewbook", "bundledock", "crewbook", "."})
	if !reflect.DeepEqual(got, []string{".", "crewbook", "bundledock"}) || notice != "" {
		t.Fatalf("%v %q", got, notice)
	}
	got, notice = Members(nil)
	if !reflect.DeepEqual(got, []string{"."}) || notice != "workspace-members-none" {
		t.Fatalf("%v %q", got, notice)
	}
}

func TestCaller(t *testing.T) {
	for _, c := range []struct {
		env, user string
		agent     bool
		want      Derived
	}{
		{"claude", "vscode", false, Derived{Name: "caller", Value: "claude", Source: "$STW_OWNER"}},
		{"", "vscode", false, Derived{Name: "caller", Value: "vscode", Source: "os-user"}},
		{"claude", "vscode", true, Derived{Name: "caller", Value: "claude", Source: "$STW_OWNER"}},
		{"", "vscode", true, Derived{Name: "caller", Source: "unset"}},
		{"", "", false, Derived{Name: "caller", Source: "unset"}},
	} {
		if got := Caller(c.env, c.user, c.agent); got != c.want {
			t.Errorf("%+v: %+v", c, got)
		}
	}
}

func TestProtected(t *testing.T) {
	trees := []Worktree{{Path: "/ws/repo", Primary: true}, {Path: "/ws/repo-wt/a"}, {Path: "/ws/repo-wt/b"}, {Path: "/ws/repo-wt/ab"}}
	got := Protected("/ws/repo-wt/a/internal/x", trees)
	want := []Protection{{Path: "/ws/repo", Reason: "primary-checkout"}, {Path: "/ws/repo-wt/a", Reason: "current-worktree"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	if got = Protected("/elsewhere", trees); !reflect.DeepEqual(got, want[:1]) {
		t.Fatalf("%+v", got)
	}
}
