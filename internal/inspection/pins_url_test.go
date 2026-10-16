// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinsURLRedactsCredentials(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
		creds     bool
	}{
		{"https://user:secret@example.com/a/b.git", "https://example.com/a/b.git", true},
		{"https://secrettoken@example.com/x", "https://example.com/x", true},
		{"https://:secret@example.com", "https://example.com", true},
		{"https://user:secret@example.com:8443/x?y=1#f", "https://example.com:8443/x?y=1#f", true},
		{"http://a:b@c:d@host/x", "http://host/x", true},
		{"HTTPS://U:secret@Example.com/Path", "HTTPS://Example.com/Path", true},
		{"ssh://git:secret@example.com/x.git", "ssh://example.com/x.git", true},
		{"https://user:secret@", "https://", true},
		{"ssh://git@example.com/x.git", "ssh://git@example.com/x.git", false},
		{"git@github.com:o/r.git", "git@github.com:o/r.git", false},
		{"https://example.com/x", "https://example.com/x", false},
		{"../sibling", "../sibling", false},
		{"/srv/git/repo", "/srv/git/repo", false},
	} {
		info := pinsClassifyURL(tc.raw)
		if info.Redacted != tc.want || info.HasCredentials != tc.creds {
			t.Errorf("%q: redacted %q creds=%v, want %q %v", tc.raw, info.Redacted, info.HasCredentials, tc.want, tc.creds)
		}
		if strings.Contains(info.Redacted, "secret") {
			t.Errorf("%q leaks a secret: %q", tc.raw, info.Redacted)
		}
	}
	// Whatever the shape, a credential never survives, not even in an unparseable or unsupported URL.
	for _, raw := range []string{"ext::sh -c 'echo secret'", "fd::17/secret", "-oProxyCommand=secret", "https://us\ner:secret@host/x", "https://user:secret@host/\xff", "weird://u:secret@host/x"} {
		if r := pinsClassifyURL(raw).Redacted; strings.Contains(r, "secret") {
			t.Errorf("%q leaks: %q", raw, r)
		}
	}
	long := "https://user:secret@example.com/" + strings.Repeat("a", 5000)
	if i := pinsClassifyURL(long); strings.Contains(i.Redacted, "secret") || len(i.Redacted) > pinsMessageLimit || i.Valid {
		t.Errorf("long URL: valid=%v len=%d", i.Valid, len(i.Redacted))
	}
}

func TestPinsURLSchemeValidation(t *testing.T) {
	valid := []string{"https://example.com/x.git", "http://example.com/x", "ssh://git@example.com/x", "git://example.com/x", "git+ssh://example.com/x", "ssh+git://example.com/x", "file:///srv/x", "git@github.com:o/r.git", "/srv/x", "./x", "../x", "sibling"}
	for _, raw := range valid {
		if i := pinsClassifyURL(raw); !i.Valid || i.Problem != "" {
			t.Errorf("%q should be valid: %+v", raw, i)
		}
	}
	problems := map[string]string{}
	for _, raw := range []string{"ext::sh -c touch", "fd::7", "ftp://example.com/x", "javascript://example.com/x", "-oProxyCommand=x", "--upload-pack=x", "https://exa\x01mple.com", "https://example.com/a\nb", "https://example.com/\xff", "", "https://example.com/" + strings.Repeat("a", 5000), "https://", "://x"} {
		i := pinsClassifyURL(raw)
		if i.Valid || i.Problem == "" {
			t.Errorf("%q should be invalid: %+v", raw, i)
		}
		problems[i.Problem] = raw
	}
	if len(problems) < 6 {
		t.Errorf("problems are not distinct enough: %v", problems)
	}
	if got := pinsClassifyURL("ext::sh -c touch").Scheme; got != "ext" {
		t.Errorf("transport name %q", got)
	}
}

func TestPinsURLNormalization(t *testing.T) {
	same := [][]string{
		{"https://example.com/o/r.git", "https://EXAMPLE.com/o/r", "https://user:pw@example.com/o/r/", "https://example.com:443/o/r.git"},
		{"ssh://git@example.com/o/r.git", "git@example.com:o/r.git", "ssh://example.com:22/o/r", "git+ssh://example.com/o/r"},
		{"file:///srv/r", "/srv/r", "/srv/r/"},
	}
	for _, group := range same {
		want := pinsClassifyURL(group[0]).Normalized
		if want == "" {
			t.Fatalf("%q is not comparable", group[0])
		}
		for _, raw := range group[1:] {
			if got := pinsClassifyURL(raw).Normalized; got != want {
				t.Errorf("%q normalizes to %q, want %q", raw, got, want)
			}
		}
	}
	a := pinsClassifyURL("https://example.com/o/r").Normalized
	for _, other := range []string{"http://example.com/o/r", "https://example.com/o/other", "https://other.example/o/r", "ssh://example.com/o/r"} {
		if pinsClassifyURL(other).Normalized == a {
			t.Errorf("%q must not equal %q", other, "https://example.com/o/r")
		}
	}
	for _, raw := range []string{"../sibling", "./x", "sibling", "ext::x", ""} {
		if n := pinsClassifyURL(raw).Normalized; n != "" {
			t.Errorf("%q is not comparable but normalized to %q", raw, n)
		}
	}
}

func TestPinsURLMatchRemote(t *testing.T) {
	reg := pinsClassifyURL("https://example.com/o/r.git")
	r := func(name, url string) pinsRemote { return pinsRemote{Name: name, URL: url} }
	for _, tc := range []struct {
		name    string
		remotes []pinsRemote
		match   string
		remote  string
	}{
		{"one equal", []pinsRemote{r("origin", "https://example.com/o/r")}, "match", "origin"},
		{"equal among others", []pinsRemote{r("fork", "https://example.com/me/r"), r("origin", "https://EXAMPLE.com/o/r.git")}, "match", "origin"},
		{"several equal", []pinsRemote{r("a", "https://example.com/o/r"), r("b", "https://example.com/o/r.git")}, "unknown", ""},
		{"one remote with two equal urls is one remote", []pinsRemote{r("a", "https://example.com/o/r"), r("a", "https://example.com/o/r.git")}, "match", "a"},
		{"sole remote differs", []pinsRemote{r("origin", "https://example.com/other/r")}, "mismatch", "origin"},
		{"several remotes none equal", []pinsRemote{r("a", "https://example.com/x/r"), r("b", "https://example.com/y/r")}, "unknown", ""},
		{"no remotes", nil, "unknown", ""},
		{"sole remote not comparable", []pinsRemote{r("origin", "../sibling")}, "unknown", ""},
	} {
		m := pinsMatchRemote(reg, tc.remotes)
		if m.Match != tc.match || (tc.remote == "") != (m.RemoteName == nil) || (tc.remote != "" && *m.RemoteName != tc.remote) {
			t.Errorf("%s: %+v", tc.name, m)
		}
		if (m.Match == "unknown") != (m.RemoteURL == nil) {
			t.Errorf("%s: remote url presence: %+v", tc.name, m)
		}
	}
	// URLs reported for a mismatch are redacted too.
	m := pinsMatchRemote(reg, []pinsRemote{r("origin", "https://u:secret@example.com/other/r")})
	if m.Match != "mismatch" || strings.Contains(*m.RemoteURL, "secret") {
		t.Errorf("%+v", m)
	}
	// A registered URL that is relative, invalid or unsupported never matches.
	for _, raw := range []string{"../sibling", "ext::x", ""} {
		if m := pinsMatchRemote(pinsClassifyURL(raw), []pinsRemote{r("origin", "https://example.com/o/r")}); m.Match != "unknown" {
			t.Errorf("%q: %+v", raw, m)
		}
	}
}

func TestPinsReadRemotesFromTheSubmoduleOwnConfiguration(t *testing.T) {
	f := newPinsFixture(t)
	repo := memberRepo(f)
	git(t, f.Super, "-C", "member", "remote", "add", "fork", "https://u:secret@example.com/me/sub.git")
	git(t, f.Super, "-C", "member", "remote", "add", "dotted.name", "https://example.com/dotted")
	remotes, err := pinsReadRemotes(newPinsRunner(context.Background()), repo)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range remotes {
		got[r.Name] = r.URL
	}
	if len(got) != 3 || got["origin"] != f.Sub || got["fork"] != "https://u:secret@example.com/me/sub.git" || got["dotted.name"] != "https://example.com/dotted" {
		t.Fatalf("%v", got)
	}
}

// networkSentinel registers a git-remote-sentinel helper on PATH that records every invocation. Git runs
// such a helper for a sentinel:// URL, so a contacted URL is observable. control proves the sentinel works.
func networkSentinel(t *testing.T) (log func() []string, control func(dir string)) {
	t.Helper()
	dir, logs := t.TempDir(), t.TempDir()
	logPath := filepath.Join(logs, "helper.log")
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + logPath + "'\nexit 1\n"
	for _, name := range []string{"git-remote-sentinel"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	read := func() []string {
		b, err := os.ReadFile(logPath)
		if err != nil || len(b) == 0 {
			return nil
		}
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}
	return read, func(dir string) {
		c := exec.Command("git", "ls-remote", "sentinel://host/repo")
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		_ = c.Run()
	}
}

func TestPinsNetworkSentinelIsNeverContacted(t *testing.T) {
	f := newPinsFixture(t)
	log, control := networkSentinel(t)
	// Control: a Git command that does contact the URL runs the helper, so the sentinel is not blind.
	control(f.Super)
	if len(log()) == 0 {
		t.Fatal("the network sentinel did not observe a deliberate contact")
	}
	// Re-arm: a fresh sentinel for the audited run.
	log, _ = networkSentinel(t)
	marker := filepath.Join(f.Root, "executed")
	hostile := []string{"sentinel://host/repo", "ext::sh -c 'touch " + marker + "'", "https://user:secret@sentinel.invalid/x"}
	git(t, f.Super, "config", "-f", ".gitmodules", "submodule.member.url", hostile[0])
	git(t, f.Super, "add", ".gitmodules")
	git(t, f.Super, "commit", "-qm", "register a helper url")
	member := filepath.Join(f.Super, "member")
	git(t, member, "remote", "set-url", "origin", hostile[0])
	git(t, member, "remote", "add", "extra", hostile[1])
	git(t, member, "remote", "add", "creds", hostile[2])

	run := newPinsRunner(context.Background())
	l, err := newPinsSuperLayout(run, f.Super)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := pinsEnumerate(run, l)
	if err != nil {
		t.Fatal(err)
	}
	e := entryAt(t, reg, "member")
	info := pinsClassifyURL(*e.URL)
	if info.Valid || info.Problem == "" {
		t.Fatalf("an unsupported scheme is classified, never contacted: %+v", info)
	}
	res, refusal, err := newPinsSafety(l, run).resolve(pinsSubmoduleSpec{Path: e.Path, Name: *e.Name})
	if err != nil || refusal != nil || res.Dirs == nil {
		t.Fatalf("%+v %v %v", res, refusal, err)
	}
	remotes, err := pinsReadRemotes(run, pinsGitRepo{GitDir: res.Dirs.GitDir, WorkTree: res.Dirs.WorkTree})
	if err != nil || len(remotes) != 3 {
		t.Fatalf("%v %v", remotes, err)
	}
	_ = pinsMatchRemote(info, remotes)
	if got := log(); len(got) != 0 {
		t.Fatalf("a remote helper was contacted: %v", got)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("an ext:: URL was executed")
	}
}
