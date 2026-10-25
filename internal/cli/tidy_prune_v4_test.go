// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/workspace"
)

type pruneCLIRepo struct{ source, state, gitdir, name, destination, moved string }

func pruneCLIGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func pruneCLIRepository(t *testing.T) pruneCLIRepo {
	t.Helper()
	physical := func() string {
		p, e := filepath.EvalSymlinks(t.TempDir())
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	home := physical()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("STW_AGENT", "1")
	t.Setenv("STW_OWNER", "codex")
	r := pruneCLIRepo{source: physical(), state: physical()}
	if e := os.Chmod(r.state, 0700); e != nil {
		t.Fatal(e)
	}
	pruneCLIGit(t, r.source, "init", "-q", "-b", "main")
	if e := os.WriteFile(filepath.Join(r.source, "keep"), []byte("source\n"), 0600); e != nil {
		t.Fatal(e)
	}
	pruneCLIGit(t, r.source, "add", ".")
	pruneCLIGit(t, r.source, "commit", "-qm", "fixture")
	r.destination = filepath.Join(physical(), "task")
	p := workspace.PlanCreate(context.Background(), workspace.PlanOptions{Path: r.source, Base: "HEAD", Destination: r.destination, Task: "prune-contract4", Owner: "codex", StateDir: r.state})
	if p.Status != "ok" || p.Plan == nil {
		t.Fatalf("creation plan: %+v", p)
	}
	a := workspace.Apply(context.Background(), workspace.ApplyOptions{StateDir: r.state, PlanID: p.Plan.PlanID, Authorize: true})
	if a.Status != "ok" || a.Task == nil {
		t.Fatalf("creation apply: %+v", a)
	}
	r.gitdir = a.Task.Native.GitDir
	r.name = filepath.Base(r.gitdir)
	r.moved = r.destination + "-moved"
	if e := os.Rename(r.destination, r.moved); e != nil {
		t.Fatal(e)
	}
	return r
}

func pruneV4Invoke(t *testing.T, r pruneCLIRepo, extra ...string) (int, map[string]any, string) {
	t.Helper()
	var out, diag bytes.Buffer
	args := []string{"tidy", "plan", r.source, "--contract", "4", "--prune-registration", r.name, "--registration-gitdir", r.gitdir, "--state-dir", r.state, "--json"}
	args = append(args, extra...)
	code := Run(context.Background(), "dev", args, &out, &diag)
	var value map[string]any
	if out.Len() > 0 {
		if e := json.Unmarshal(out.Bytes(), &value); e != nil {
			t.Fatalf("invalid result JSON: %v: %s", e, out.Bytes())
		}
	}
	return code, value, diag.String()
}

func pruneNativeBytes(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if e := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if d.Type().IsRegular() {
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			out[strings.TrimPrefix(p, root)] = string(b)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	return out
}

func TestPruneV4CLIStoresOnlyExactOwnedPlan(t *testing.T) {
	r := pruneCLIRepository(t)
	before := pruneNativeBytes(t, r.source)
	moved := pruneNativeBytes(t, r.moved)
	code, result, diag := pruneV4Invoke(t, r)
	if code != 0 || result["schema_version"] != float64(4) || result["operation"] != "registration-prune" || result["status"] != "ok" {
		t.Fatalf("plan: code=%d result=%v diagnostic=%s", code, result, diag)
	}
	p, ok := result["plan"].(map[string]any)
	if !ok {
		t.Fatal("missing typed plan")
	}
	id, _ := p["plan_id"].(string)
	digest, _ := p["digest"].(string)
	if len(id) != 32 || len(digest) != 64 {
		t.Fatalf("identities: %v", p)
	}
	raw, e := os.ReadFile(filepath.Join(r.state, "tidy-registration-v4", "plans", id+".json"))
	if e != nil {
		t.Fatal(e)
	}
	var stored map[string]any
	if e = json.Unmarshal(raw, &stored); e != nil || !reflect.DeepEqual(p, stored) {
		t.Fatalf("stored plan differs: %v", e)
	}
	reg := p["registration"].(map[string]any)
	if reg["name"] != r.name || reg["gitdir"] != r.gitdir || reg["scope"] != "own" {
		t.Fatalf("selection/scope lost: %v", reg)
	}
	expected := p["expected"].(map[string]any)
	mount, ok := expected["common_dir_mount"].(map[string]any)
	if !ok || mount["mount"] == "" || mount["mount"] == nil {
		t.Fatalf("common-directory mount unbound: %v", expected)
	}
	if !reflect.DeepEqual(before, pruneNativeBytes(t, r.source)) || !reflect.DeepEqual(moved, pruneNativeBytes(t, r.moved)) {
		t.Fatal("planner changed protected native/user bytes")
	}
	for _, name := range []string{"journals", "payloads", "restores"} {
		if _, e = os.Lstat(filepath.Join(r.state, "tidy-registration-v4", name)); !os.IsNotExist(e) {
			t.Fatalf("planning wrote recovery/effect namespace %s: %v", name, e)
		}
	}
	refs := pruneCLIGit(t, r.source, "for-each-ref", "--format=%(refname)", "refs/stw/recovery/registration-prune")
	if refs != "" {
		t.Fatalf("planning created recovery ref: %s", refs)
	}
}

func TestPruneV4CLIRefusesUnprovenOrChangedSelection(t *testing.T) {
	for _, kind := range []string{"foreign-scope", "wrong-gitdir", "destination-present", "unlocked"} {
		t.Run(kind, func(t *testing.T) {
			r := pruneCLIRepository(t)
			reason := ""
			switch kind {
			case "foreign-scope":
				t.Setenv("STW_OWNER", "another-agent")
				reason = "prune-scope-refused"
			case "wrong-gitdir":
				r.gitdir += "-other"
				reason = "prune-selection-refused"
			case "destination-present":
				if e := os.Mkdir(r.destination, 0700); e != nil {
					t.Fatal(e)
				}
				reason = "prune-destination-present"
			case "unlocked":
				if e := os.Remove(filepath.Join(r.gitdir, "locked")); e != nil {
					t.Fatal(e)
				}
				reason = "prune-lock-refused"
			}
			before := pruneNativeBytes(t, r.source)
			code, value, diag := pruneV4Invoke(t, r)
			encoded, _ := json.Marshal(value)
			if kind == "foreign-scope" && !strings.Contains(string(encoded), "Agent prune planning requires own task-record ownership.") {
				t.Fatalf("wrong ownership refusal: %s", encoded)
			}
			if code != 1 || value["schema_version"] != float64(4) || value["status"] != "refused" || !strings.Contains(string(encoded), reason) {
				t.Fatalf("%s: code=%d result=%v diagnostic=%s; want exact %s refusal", kind, code, value, diag, reason)
			}
			if !reflect.DeepEqual(before, pruneNativeBytes(t, r.source)) {
				t.Fatal("refusal changed source")
			}
			if _, e := os.Lstat(filepath.Join(r.state, "tidy-registration-v4")); !os.IsNotExist(e) {
				t.Fatalf("refused plan wrote namespace: %v", e)
			}
		})
	}
}

func TestPruneV4CLIRejectsAmbiguousAndLegacyGrammar(t *testing.T) {
	for _, tc := range []struct {
		name   string
		flags  []string
		reason string
	}{
		{"implicit-contract", []string{"--prune-registration=x"}, "explicit --contract 4"},
		{"missing-selector", []string{"--contract=4"}, "are required"},
		{"duplicate-name", []string{"--contract=4", "--prune-registration=x", "--prune-registration=x"}, "must occur exactly once"},
		{"duplicate-contract", []string{"--contract=4", "--contract=4"}, "must occur exactly once"},
		{"old-task", []string{"--contract=4", "--task=old"}, "not valid for contract-4"},
		{"old-retirement", []string{"--contract=4", "--retire-worktree=old"}, "unknown flag: --retire-worktree"},
		{"online", []string{"--contract=4", "--online"}, "not valid for contract-4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := Run(context.Background(), "dev", append([]string{"tidy", "plan"}, tc.flags...), &out, &stderr)
			if code != 2 || !strings.Contains(stderr.String(), tc.reason) {
				t.Fatalf("exit=%d stdout=%q stderr=%q; want usage refusal %q", code, out.String(), stderr.String(), tc.reason)
			}
		})
	}
}
