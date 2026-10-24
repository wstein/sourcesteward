// SPDX-License-Identifier: EUPL-1.2
package derive

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// privateRoot is an existing 0700 directory outside any repository.
func privateRoot(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(p, 0700); err != nil {
		t.Fatal(err)
	}
	return p
}

// storeWith creates a private store whose tasks directory holds one creation record per task, owner by task.
func storeWith(t *testing.T, owners map[string]string) string {
	t.Helper()
	p := privateRoot(t)
	for _, kind := range []string{"plans", "operations", "tasks"} {
		if err := os.Mkdir(filepath.Join(p, kind), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for task, owner := range owners {
		b, err := json.Marshal(workspace.TaskRecord{SchemaVersion: 1, TaskID: task, Owner: owner, Backend: "git"})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(p, "tasks", task+".json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func issueCode(err error) string {
	var i *Issue
	if errors.As(err, &i) {
		return i.Code
	}
	return ""
}

func TestDefaultStateDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	t.Setenv("HOME", "/home/u")
	if got, err := DefaultStateDir(); err != nil || got != "/xdg/state/stw/workspace" {
		t.Fatalf("%q %v", got, err)
	}
	t.Setenv("XDG_STATE_HOME", "")
	if got, err := DefaultStateDir(); err != nil || got != "/home/u/.local/state/stw/workspace" {
		t.Fatalf("%q %v", got, err)
	}
	t.Setenv("XDG_STATE_HOME", "relative/state")
	if _, err := DefaultStateDir(); issueCode(err) != "state-dir-unsafe" {
		t.Fatalf("relative XDG_STATE_HOME accepted: %v", err)
	}
}

func TestEnsureStateDirCreatesPrivateDirectories(t *testing.T) {
	dir := filepath.Join(privateRoot(t), "a", "b", "workspace")
	if err := EnsureStateDir(dir); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{dir, filepath.Dir(dir), filepath.Dir(filepath.Dir(dir))} {
		if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0700 {
			t.Fatalf("%s: %v %v", p, st, err)
		}
	}
	if err := EnsureStateDir(dir); err != nil {
		t.Fatalf("an existing safe store: %v", err)
	}
}

func TestEnsureStateDirRefusals(t *testing.T) {
	root := privateRoot(t)
	open := filepath.Join(root, "open")
	if err := os.Mkdir(open, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	linkedParent := filepath.Join(link, "child")
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	jjRepo := filepath.Join(root, "jjrepo")
	if err := os.MkdirAll(filepath.Join(jjRepo, ".jj"), 0700); err != nil {
		t.Fatal(err)
	}
	gitFile := filepath.Join(root, "linked-checkout")
	if err := os.Mkdir(gitFile, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitFile, ".git"), []byte("gitdir: /elsewhere\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"mode 0755":              open,
		"symlink":                link,
		"symlinked parent":       linkedParent,
		"inside a repository":    filepath.Join(repo, "state"),
		"inside a jj repository": filepath.Join(jjRepo, "state"),
		"inside a linked tree":   filepath.Join(gitFile, "state"),
		"not a directory":        file,
		"relative":               "relative/state",
		"unclean":                root + "/x/../y",
		"NUL":                    root + "/a\x00b",
		"exceeds the text bound": filepath.Join(root, strings.Repeat("x", 4097)),
	} {
		if err := EnsureStateDir(path); issueCode(err) != "state-dir-unsafe" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(repo, "state")); err == nil {
		t.Error("a refused store was created inside the repository")
	}
}

func TestEnsureStateDirRefusesAnotherOwner(t *testing.T) {
	st, err := os.Lstat("/root")
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0700 || os.Geteuid() == 0 {
		t.Skip("no root-owned 0700 directory to probe")
	}
	if err = EnsureStateDir("/root"); issueCode(err) != "state-dir-unsafe" {
		t.Fatalf("%v", err)
	}
}

func TestResolveStateDir(t *testing.T) {
	envDir := filepath.Join(privateRoot(t), "env")
	xdg := privateRoot(t)
	t.Setenv("STW_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", xdg)
	if got, err := ResolveStateDir("/explicit/as/today"); err != nil || got != "/explicit/as/today" {
		t.Fatalf("an explicit directory is returned unchanged: %q %v", got, err)
	}
	def := filepath.Join(xdg, "stw", "workspace")
	if got, err := ResolveStateDir(""); err != nil || got != def {
		t.Fatalf("%q %v", got, err)
	}
	if st, err := os.Stat(def); err != nil || st.Mode().Perm() != 0700 {
		t.Fatalf("default not created 0700: %v %v", st, err)
	}
	t.Setenv("STW_STATE_DIR", envDir)
	if got, err := ResolveStateDir(""); err != nil || got != envDir {
		t.Fatalf("$STW_STATE_DIR overrides the default: %q %v", got, err)
	}
	if err := os.Chmod(envDir, 0755); err != nil {
		t.Fatal(err)
	}
	if got, err := ResolveStateDir(""); err == nil || issueCode(err) != "state-dir-unsafe" || got != "" {
		t.Fatalf("unsafe $STW_STATE_DIR: %q %v", got, err)
	}
}

func TestStoresOrderSkipAndCounts(t *testing.T) {
	a := storeWith(t, map[string]string{"t1": "claude", "t2": "codex"})
	b := storeWith(t, map[string]string{"t3": "claude"})
	env := storeWith(t, nil)
	def := filepath.Join(privateRoot(t), "stw", "workspace") // missing: not a store, not an error
	open := privateRoot(t)
	if err := os.Chmod(open, 0755); err != nil {
		t.Fatal(err)
	}
	got := Stores([]string{a, open, b, a}, env, def)
	want := []Store{
		{Path: a, Source: "--state-dir", Records: 2},
		{Path: open, Source: "--state-dir", Issue: &Issue{Code: "state-dir-unsafe"}},
		{Path: b, Source: "--state-dir", Records: 1},
		{Path: env, Source: "$STW_STATE_DIR"},
	}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i].Path != want[i].Path || got[i].Source != want[i].Source || got[i].Records != want[i].Records || (got[i].Issue == nil) != (want[i].Issue == nil) || (got[i].Issue != nil && got[i].Issue.Code != want[i].Issue.Code) {
			t.Errorf("%d: %+v", i, got[i])
		}
	}
	if p, ok := PlanStore(got); !ok || p.Path != a {
		t.Fatalf("the plan store is the first valid store: %+v", p)
	}
	if _, ok := PlanStore(got[1:2]); ok {
		t.Fatal("an unsafe store is not a plan store")
	}
	if !HasUnsafe(got) || HasUnsafe(got[:1]) {
		t.Fatal("an unsafe store must make the run end with exit 1")
	}
}

func TestStoresUseTheSharedDefaultAndSkipAMissingFlagDirectory(t *testing.T) {
	def := storeWith(t, map[string]string{"t1": "claude"})
	got := Stores(nil, "", def)
	if len(got) != 1 || got[0].Source != "default" || got[0].Records != 1 || got[0].Issue != nil {
		t.Fatalf("%+v", got)
	}
	missing := filepath.Join(privateRoot(t), "absent")
	got = Stores([]string{missing}, "", "")
	if len(got) != 1 || got[0].Issue == nil || got[0].Issue.Code != "state-dir-unsafe" {
		t.Fatalf("an explicit directory that is missing is reported: %+v", got)
	}
}

func TestStoresRefuseSymlinkAndRepositoryStores(t *testing.T) {
	root := privateRoot(t)
	real := storeWith(t, nil)
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(repo, "state")
	if err := os.Mkdir(inside, 0700); err != nil {
		t.Fatal(err)
	}
	for _, s := range Stores([]string{link, inside}, "", "") {
		if s.Issue == nil || s.Issue.Code != "state-dir-unsafe" {
			t.Errorf("%+v", s)
		}
	}
}

func TestLookupTask(t *testing.T) {
	a := storeWith(t, map[string]string{"only-a": "claude", "same": "codex", "differs": "claude"})
	b := storeWith(t, map[string]string{"only-b": "codex", "same": "codex", "differs": "codex"})
	stores := Stores([]string{a, b}, "", "")

	r := LookupTask(stores, "only-a")
	if r.Ambiguity != nil || r.Record.Owner != "claude" || r.Derived != (Derived{Name: "owner:only-a", Value: "claude", Source: "task-record"}) {
		t.Fatalf("%+v", r)
	}
	if r = LookupTask(stores, "only-b"); r.Ambiguity != nil || r.Record.Owner != "codex" {
		t.Fatalf("a record found in a second store: %+v", r)
	}
	if r = LookupTask(stores, "same"); r.Ambiguity != nil || r.Record.Owner != "codex" {
		t.Fatalf("equal records in two stores are one record: %+v", r)
	}
	r = LookupTask(stores, "differs")
	if r.Ambiguity == nil || r.Ambiguity.Code != "registration-record-ambiguous" || r.Ambiguity.Subject != "differs" || !reflect.DeepEqual(r.Ambiguity.Candidates, []string{a, b}) || r.Derived.Value != "" {
		t.Fatalf("%+v", r)
	}
	r = LookupTask(stores, "nowhere")
	if r.Ambiguity == nil || r.Ambiguity.Code != "registration-record-missing" || r.Ambiguity.Hint != "pass the --state-dir that created it" {
		t.Fatalf("%+v", r)
	}
	if r = LookupTask(nil, "only-a"); r.Ambiguity == nil || r.Ambiguity.Code != "registration-record-missing" {
		t.Fatalf("no store at all: %+v", r)
	}
}

func TestLookupSkipsUnsafeStoresAndStoresWithoutRecords(t *testing.T) {
	good := storeWith(t, map[string]string{"t": "claude"})
	open := storeWith(t, map[string]string{"t": "codex"})
	if err := os.Chmod(open, 0755); err != nil {
		t.Fatal(err)
	}
	fresh := privateRoot(t) // a created default without record directories yet
	r := LookupTask(Stores([]string{open, fresh, good}, "", ""), "t")
	if r.Ambiguity != nil || r.Record.Owner != "claude" {
		t.Fatalf("%+v", r)
	}
}

func TestStoresDerived(t *testing.T) {
	a := storeWith(t, map[string]string{"t1": "claude", "t2": "claude"})
	open := privateRoot(t)
	if err := os.Chmod(open, 0755); err != nil {
		t.Fatal(err)
	}
	d := StoresDerived(Stores([]string{a, open}, "", ""))
	want := Derived{Name: "state-dirs", Value: a + " (2 records); " + open + " (skipped: state-dir-unsafe)", Source: "--state-dir", Overridden: true}
	if d != want {
		t.Fatalf("%+v", d)
	}
	def := storeWith(t, nil)
	if d = StoresDerived(Stores(nil, "", def)); d.Overridden || d.Source != "default" || d.Value != def+" (0 records)" {
		t.Fatalf("%+v", d)
	}
}

func TestLookupTaskUnreadableRecordIsNeverAbsent(t *testing.T) {
	a := storeWith(t, map[string]string{"t": "claude"})
	b := storeWith(t, map[string]string{"t": "codex"})
	if err := os.Chmod(filepath.Join(a, "tasks", "t.json"), 0644); err != nil {
		t.Fatal(err)
	}
	r := LookupTask(Stores([]string{a, b}, "", ""), "t")
	if r.Ambiguity == nil || r.Ambiguity.Code != "registration-record-ambiguous" || r.Derived.Value != "" || !reflect.DeepEqual(r.Ambiguity.Candidates, []string{b, a}) {
		t.Fatalf("%+v", r)
	}
	if r = LookupTask(Stores([]string{a}, "", ""), "t"); r.Ambiguity == nil || r.Ambiguity.Code != "registration-record-ambiguous" {
		t.Fatalf("an unreadable record alone is not a missing one: %+v", r)
	}
}

func TestLookupSkipsStoresSkippedByThePathChecks(t *testing.T) {
	root := privateRoot(t)
	good := storeWith(t, map[string]string{"t": "claude"})
	real := storeWith(t, map[string]string{"t": "codex"})
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(repo, "store")
	for _, kind := range []string{"plans", "operations", "tasks"} {
		if err := os.MkdirAll(filepath.Join(inside, kind), 0700); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := json.Marshal(workspace.TaskRecord{SchemaVersion: 1, TaskID: "t", Owner: "other", Backend: "git"})
	if err := os.WriteFile(filepath.Join(inside, "tasks", "t.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	r := LookupTask(Stores([]string{link, inside, good}, "", ""), "t")
	if r.Ambiguity != nil || r.Record.Owner != "claude" {
		t.Fatalf("a skipped store contributed a record: %+v", r)
	}
}

func TestStoresReportAnUnreadableRecordDirectory(t *testing.T) {
	p := storeWith(t, map[string]string{"t": "claude"})
	if err := os.Chmod(filepath.Join(p, "tasks"), 0755); err != nil {
		t.Fatal(err)
	}
	got := Stores([]string{p}, "", "")
	if len(got) != 1 || got[0].Issue == nil || got[0].Issue.Code != "state-dir-unsafe" {
		t.Fatalf("%+v", got)
	}
}
