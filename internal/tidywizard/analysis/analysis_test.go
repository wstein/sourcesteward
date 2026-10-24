// SPDX-License-Identifier: EUPL-1.2

package analysis

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
)

func member(name, path string) stwsync.Member {
	return stwsync.Member{Name: name, Path: path, Kind: "submodule", Initialized: true}
}

func refsOf(remotes map[string]string, refs ...string) stwsync.RepoRefs {
	r := stwsync.RepoRefs{Refs: map[string]bool{}, Remotes: []string{}, Heads: map[string]string{}}
	for _, ref := range refs {
		r.Refs[ref] = true
	}
	for remote, head := range remotes {
		r.Remotes = append(r.Remotes, remote)
		r.Heads[remote] = head
	}
	sortStrings(r.Remotes)
	return r
}

func sortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

type fakeWorld struct {
	ws      stwsync.Workspace
	refs    map[string]stwsync.RepoRefs
	refsErr map[string]error
	started string // the path Discover was asked about
}

func (f *fakeWorld) deps() Deps {
	return Deps{
		Discover: func(_ context.Context, path string) (stwsync.Workspace, error) {
			f.started = path
			if f.ws.Root == "" {
				return stwsync.Workspace{}, errors.New("a Git repository is needed: not a repository")
			}
			return f.ws, nil
		},
		Refs: func(_ context.Context, dir string) (stwsync.RepoRefs, error) {
			if err := f.refsErr[dir]; err != nil {
				return stwsync.RepoRefs{}, err
			}
			return f.refs[dir], nil
		},
		DefaultStateDir: func() (string, error) { return "", errors.New("no default") },
		StateEnv:        "",
	}
}

func world() *fakeWorld {
	root := "/ws"
	return &fakeWorld{
		ws: stwsync.Workspace{Root: root, Members: []stwsync.Member{
			{Name: ".", Path: root, Kind: "root", Initialized: true},
			member("a", root+"/a"), member("b", root+"/b"), member("c", root+"/c"),
		}},
		refs: map[string]stwsync.RepoRefs{
			root:        refsOf(map[string]string{"origin": "refs/remotes/origin/main"}, "refs/heads/main", "refs/remotes/origin/main"),
			root + "/a": refsOf(map[string]string{"origin": "refs/remotes/origin/main"}, "refs/remotes/origin/main"), // no local main
			root + "/b": refsOf(map[string]string{"origin": "", "upstream": ""}, "refs/heads/main"),                  // no symbolic head
			root + "/c": refsOf(map[string]string{"alpha": "refs/remotes/alpha/main", "beta": "refs/remotes/beta/main"}, "refs/heads/main"),
		},
		refsErr: map[string]error{},
	}
}

func specOf(p Prepared, name string) tidywizard.RepoSpec {
	for _, s := range p.Specs {
		if s.Name == name {
			return s
		}
	}
	return tidywizard.RepoSpec{Name: "<absent:" + name + ">"}
}

// The members are the workspace repository first and then the initialized members; the remote is
// origin, else the single remote; the base is the symbolic HEAD of the remote as a local branch when
// it exists, else the remote-tracking reference; a repository whose remote or base cannot be derived
// is skipped with the reason of the specification.
func TestPrepareDerivesRemoteAndBaseOfEveryMember(t *testing.T) {
	w := world()
	p, err := Prepare(context.Background(), Options{CWD: "/ws/a"}, w.deps())
	if err != nil {
		t.Fatal(err)
	}
	if w.started != "/ws/a" || p.Workspace != "/ws" {
		t.Errorf("discovered from %q, workspace %q", w.started, p.Workspace)
	}
	if !reflect.DeepEqual(p.Names, []string{".", "a", "b", "c"}) {
		t.Errorf("names %v", p.Names)
	}
	if s := specOf(p, "."); s.Skip != "" || s.Base != "refs/heads/main" || s.Remote != "origin" || s.Path != "/ws" {
		t.Errorf(".: %+v", s)
	}
	if s := specOf(p, "a"); s.Skip != "" || s.Base != "refs/remotes/origin/main" {
		t.Errorf("a (no local branch): %+v", s)
	}
	if s := specOf(p, "b"); s.Skip != "base-ambiguous" || s.Base != "" {
		t.Errorf("b (no symbolic HEAD): %+v", s)
	}
	if s := specOf(p, "c"); s.Skip != "remote-ambiguous" {
		t.Errorf("c (two remotes, no origin): %+v", s)
	}
	// Every derived value is listed with its source.
	var names []string
	for _, d := range p.Derived {
		names = append(names, d.Name)
	}
	for _, want := range []string{"workspace", "remote:.", "base:.", "remote:a", "base:a", "state-dirs"} {
		found := false
		for _, n := range names {
			found = found || n == want
		}
		if !found {
			t.Errorf("derived value %q missing in %v", want, names)
		}
	}
	if m := p.Meta["."]; m.Path != "/ws" || m.Backend != "git" || m.Base != "refs/heads/main" || m.Remote != "origin" {
		t.Errorf("meta %+v", m)
	}
	if m := p.Meta["b"]; m.Backend != "git" || m.Base != "" {
		t.Errorf("meta of a skipped repository %+v", m)
	}
}

func TestPrepareWorkspaceFlagMemberSelectionAndOverrides(t *testing.T) {
	w := world()
	// --workspace is where discovery starts; the workspace derivation says so.
	p, err := Prepare(context.Background(), Options{CWD: "/elsewhere", Workspace: "/ws"}, w.deps())
	if err != nil || w.started != "/ws" {
		t.Fatalf("%v started %q", err, w.started)
	}
	var ws derive.Derived
	for _, d := range p.Derived {
		if d.Name == "workspace" {
			ws = d
		}
	}
	if ws.Value != "/ws" || ws.Source != "--workspace" || !ws.Overridden {
		t.Errorf("%+v", ws)
	}
	// --member restricts the set; the workspace repository is only there if asked for.
	p, err = Prepare(context.Background(), Options{CWD: "/ws", Members: []string{"a"}}, w.deps())
	if err != nil || !reflect.DeepEqual(p.Names, []string{"a"}) {
		t.Fatalf("%v %v", err, p.Names)
	}
	// --member-base overrides one base, it must exist locally, else the repository is skipped base-missing.
	p, err = Prepare(context.Background(), Options{CWD: "/ws", MemberBases: []string{".=refs/remotes/origin/main", "a=refs/heads/nope"}}, w.deps())
	if err != nil {
		t.Fatal(err)
	}
	if s := specOf(p, "."); s.Base != "refs/remotes/origin/main" || s.Skip != "" {
		t.Errorf("override: %+v", s)
	}
	if s := specOf(p, "a"); s.Skip != "base-missing" {
		t.Errorf("override of a missing ref: %+v", s)
	}
	found := false
	for _, d := range p.Derived {
		found = found || d.Name == "base:." && d.Overridden && d.Source == "--member-base"
	}
	if !found {
		t.Errorf("the override is not listed as derived: %+v", p.Derived)
	}
}

func TestPrepareUsageErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		opt  Options
		code string
	}{
		"unknown member":            {Options{CWD: "/ws", Members: []string{"zzz"}}, "unknown-member"},
		"member-base for no member": {Options{CWD: "/ws", MemberBases: []string{"zzz=refs/heads/x"}}, "unknown-member"},
		"member-base without =":     {Options{CWD: "/ws", MemberBases: []string{"a"}}, "invalid-member-base"},
		"member-base empty ref":     {Options{CWD: "/ws", MemberBases: []string{"a="}}, "invalid-member-base"},
		"member-base twice":         {Options{CWD: "/ws", MemberBases: []string{"a=refs/heads/x", "a=refs/heads/y"}}, "invalid-member-base"},
	} {
		w := world()
		_, err := Prepare(context.Background(), tc.opt, w.deps())
		var ue *UsageError
		if !errors.As(err, &ue) || ue.Code != tc.code {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Outside a workspace: workspace-not-found, naming the reason, and nothing else is read.
	w := world()
	w.ws = stwsync.Workspace{}
	_, err := Prepare(context.Background(), Options{CWD: "/tmp/nowhere"}, w.deps())
	var ue *UsageError
	if !errors.As(err, &ue) || ue.Code != "workspace-not-found" || !strings.Contains(ue.Error(), "Git repository is needed") {
		t.Errorf("%v", err)
	}
}

// A repository whose references cannot be read is skipped as base-ambiguous with a notice carrying
// the error; an uninitialized member is not analysed and is named in a notice.
func TestPrepareReportsUnreadableAndUninitializedMembers(t *testing.T) {
	w := world()
	w.refsErr["/ws/a"] = errors.New("the references cannot be read: boom")
	w.ws.Members[2].Initialized = false // b
	p, err := Prepare(context.Background(), Options{CWD: "/ws"}, w.deps())
	if err != nil {
		t.Fatal(err)
	}
	if s := specOf(p, "a"); s.Skip != "base-ambiguous" {
		t.Errorf("%+v", s)
	}
	if !reflect.DeepEqual(p.Names, []string{".", "a", "c"}) {
		t.Errorf("names %v", p.Names)
	}
	joined := strings.Join(p.Notices, "\n")
	if !strings.Contains(joined, "a: the references cannot be read: boom") || !strings.Contains(joined, "member-uninitialized: b") {
		t.Errorf("notices %q", joined)
	}
	// A member without any remote cannot have a symbolic HEAD either.
	w = world()
	w.refs["/ws/b"] = refsOf(nil, "refs/heads/main")
	p, _ = Prepare(context.Background(), Options{CWD: "/ws"}, w.deps())
	if s := specOf(p, "b"); s.Skip != "base-ambiguous" || s.Remote != "" {
		t.Errorf("no remote: %+v", s)
	}
}

func TestPrepareOnlineAndStateDirectories(t *testing.T) {
	w := world()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"plans", "operations", "tasks"} {
		if err := os.Mkdir(filepath.Join(dir, k), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	p, err := Prepare(context.Background(), Options{CWD: "/ws", Online: true, StateDirs: []string{dir}}, w.deps())
	if err != nil {
		t.Fatal(err)
	}
	if s := specOf(p, "."); !s.Online || s.Remote != "origin" {
		t.Errorf("online: %+v", s)
	}
	if s := specOf(p, "b"); s.Online {
		t.Errorf("a skipped repository is not read online: %+v", s)
	}
	if len(p.Stores) != 1 || p.Stores[0].Path != dir || p.Stores[0].Issue != nil || p.PlanStore != dir {
		t.Errorf("stores %+v plan store %q", p.Stores, p.PlanStore)
	}
	p, _ = Prepare(context.Background(), Options{CWD: "/ws"}, w.deps())
	if s := specOf(p, "."); s.Online {
		t.Errorf("offline by default: %+v", s)
	}
	if p.PlanStore != "" {
		t.Errorf("no plan store without a valid state directory: %q", p.PlanStore)
	}
}

// git runs one Git command with a throwaway identity, for the end-to-end test.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.org", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// End to end over a real repository: discovery, local refs, derived base, the real adapter through
// fanout, and a linked worktree seen as a candidate.
func TestPrepareAndRunOverARealRepository(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bare, repo := filepath.Join(root, "origin.git"), filepath.Join(root, "ws")
	git(t, root, "init", "-q", "--bare", "-b", "main", bare)
	git(t, root, "clone", "-q", bare, repo)
	git(t, repo, "checkout", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "f.txt")
	git(t, repo, "commit", "-q", "-m", "one")
	git(t, repo, "push", "-q", "origin", "main")
	git(t, repo, "remote", "set-head", "origin", "main")
	git(t, repo, "worktree", "add", "-q", "-b", "feature/x", filepath.Join(root, "wt-x"))

	p, err := Prepare(context.Background(), Options{CWD: repo}, DefaultDeps())
	if err != nil {
		t.Fatal(err)
	}
	if s := specOf(p, "."); s.Skip != "" || s.Base != "refs/heads/main" || s.Remote != "origin" {
		t.Fatalf("%+v", s)
	}
	res := Run(context.Background(), p, tidywizard.Config{Caller: "tester", CWD: repo, Now: nowForTest()})
	if len(res.Repositories) != 1 || res.Repositories[0].State != "analysed" {
		t.Fatalf("%+v", res.Repositories)
	}
	sawWorktree := false
	for _, c := range res.Candidates {
		if c.Kind == tidywizard.KindWorktree && strings.HasSuffix(c.Path, "wt-x") {
			sawWorktree = true
		}
	}
	if !sawWorktree {
		t.Errorf("the linked worktree is not a candidate: %+v", res.Candidates)
	}
}

func nowForTest() time.Time { return time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC) }
