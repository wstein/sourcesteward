// SPDX-License-Identifier: EUPL-1.2
package hosting

import "testing"

func TestRemoteResolution(t *testing.T) {
	for _, raw := range []string{"https://github.com/one/repo.git", "git@github.com:one/repo.git", "ssh://git@github.com/one/repo.git"} {
		c, e := parseRemote("origin", raw, "github.com")
		if e != nil || c == nil || c.Owner != "one" || c.Name != "repo" {
			t.Fatalf("%q %+v %v", raw, c, e)
		}
	}
	for _, raw := range []string{"https://token@github.com/a/b", "https://github.com/a/b?token=x", "https://github.com/a/b#x", "ssh://password@github.com/a/b", "https://github.com/a/b/c", "https://github.com/a/%62"} {
		if _, e := parseRemote("origin", raw, "github.com"); e == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	c, e := parseRemote("gitlab", "https://gitlab.com/a/b", "github.com")
	if e != nil || c != nil {
		t.Fatalf("unrelatedhost %+v %v", c, e)
	}
	cs := []Candidate{{"origin", "github.com", "one", "repo"}, {"upstream", "github.com", "two", "repo"}}
	if _, e := selectRepository(Options{Kind: "repo"}, cs); e == nil {
		t.Fatal("ambiguity accepted")
	}
	if r, e := selectRepository(Options{Kind: "repo", Remote: "upstream"}, cs); e != nil || r.Owner != "two" {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e := selectRepository(Options{Kind: "repo", Remote: "origin", Repo: "two/repo"}, cs); e == nil {
		t.Fatal("contradicting selectors accepted")
	}
	if r, e := selectRepository(Options{Kind: "repo", Repo: "other/repo"}, nil); e != nil || r.Owner != "other" {
		t.Fatalf("explicit withoutremotes %+v %v", r, e)
	}
}
func TestUnsupportedRemoteExclusionAndEmptyQueryRefusal(t *testing.T) {
	for _, raw := range []string{"/local/repo", "file:///local/repo", "bob@gitlab.com:a/b", "https://gitlab.com/a/b"} {
		c, e := parseRemote("other", raw, "github.com")
		if e != nil || c != nil {
			t.Fatalf("unrelated %q %+v %v", raw, c, e)
		}
	}
	for _, raw := range []string{"https://github.com/a/b?", "https://github.com/a/b#", "git://github.com/a/b"} {
		if _, e := parseRemote("origin", raw, "github.com"); e == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
