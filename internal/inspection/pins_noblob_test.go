// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// noBlobGit puts a `git` first on PATH that records every call and fails `config --blob` with exit 128,
// the way Git built with a signed char does when a .gitmodules blob holds the byte 0xFF (x86_64; arm64
// builds never show it). The code under test must not depend on that command at all.
func noBlobGit(t *testing.T) (calls func() []string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	dir, log := t.TempDir(), filepath.Join(t.TempDir(), "calls.log")
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\ncase \"$*\" in *' config '*--blob*) echo 'error: bad config line 1 in blob' >&2; echo 'fatal: error processing config file(s)' >&2; exit 128 ;; esac\nexec '" + real + "' \"$@\"\n"
	mustOS(t, os.WriteFile(filepath.Join(dir, "git"), []byte(body), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() []string {
		b, _ := os.ReadFile(log)
		return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	}
}

func requireNoBlobCall(t *testing.T, calls []string) {
	t.Helper()
	saw := 0
	for _, c := range calls {
		if strings.Contains(c, "--blob") {
			t.Errorf("config --blob was used on untrusted bytes: %s", c)
		}
		if strings.Contains(c, "cat-file blob") {
			saw++
		}
	}
	if saw == 0 {
		t.Error("the blob was never read with cat-file")
	}
}

func TestPinsRegistryNeverUsesConfigBlobAndHandlesTheByte0xFF(t *testing.T) {
	f := newPinsFixture(t)
	writeFile(t, filepath.Join(f.Super, ".gitmodules"), "[submodule \"n\xff\"]\n\tpath = member\n\turl = "+f.Sub+"\n")
	git(t, f.Super, "add", ".gitmodules")
	git(t, f.Super, "commit", "-qm", "odd name")
	// A differing staged copy makes the registry read both blobs.
	writeFile(t, filepath.Join(f.Super, ".gitmodules"), "[submodule \"n\xff\"]\n\tpath = member\n\turl = "+f.Sub+"\n\tbranch = main\n")
	git(t, f.Super, "add", ".gitmodules")
	calls := noBlobGit(t)
	r := auditOf(t, f.Super)
	if r.Status != "ok" {
		t.Fatalf("%s %+v", r.Status, r.Error)
	}
	s := subOf(t, r, "member")
	if s.Name == nil || *s.Name != `n\xff` || s.Refusal == nil || s.Refusal.Code != "submodule-path-unsafe" {
		t.Fatalf("%v %+v", s.Name, s.Refusal)
	}
	requireNoBlobCall(t, calls())
	requireFinding(t, r, "gitmodules-modified", "warning", "")
}

func TestPinsRegistryUnbornStagedBlobNeverUsesConfigBlob(t *testing.T) {
	f := newPinsFixture(t)
	d := pinsRealDir(t)
	git(t, d, "init", "-q", "-b", "main")
	git(t, d, "-c", "protocol.file.allow=always", "submodule", "add", "-q", f.Sub, "member")
	calls := noBlobGit(t)
	r := auditOf(t, d)
	if r.Status == "error" {
		t.Fatalf("%+v", r.Error)
	}
	if s := subOf(t, r, "member"); s.Name == nil {
		t.Fatalf("the staged registration was not read: %+v", s)
	}
	requireNoBlobCall(t, calls())
}

func TestReposDiscoverNeverUsesConfigBlobAndHandlesTheByte0xFF(t *testing.T) {
	reposHermetic(t)
	repo := reposNewRepo(t, filepath.Join(reposTempDir(t), "super"))
	writeFile(t, filepath.Join(repo, ".gitmodules"), "[submodule \"binary\"]\n\tpath = bad\xffname\n[submodule \"n\xff\"]\n\tpath = other\n")
	git(t, repo, "add", ".gitmodules")
	git(t, repo, "commit", "-qm", "registration")
	calls := noBlobGit(t)
	parent := reposRun([]string{repo}, true).Entries[0]
	if parent.Discovery.Status != "ok" || parent.Discovery.Ignored != 2 {
		t.Fatalf("discovery %+v notes %q", parent.Discovery, parent.Notes)
	}
	requireNoBlobCall(t, calls())
}
