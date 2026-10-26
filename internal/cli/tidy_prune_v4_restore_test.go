//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestTidyPruneV4RestoreExplicitDispatch(t *testing.T) {
	repo := pruneCLIRepository(t)
	code, planned, diag := pruneV4Invoke(t, repo)
	if code != 0 {
		t.Fatalf("plan %d %s", code, diag)
	}
	id := planned["plan"].(map[string]any)["plan_id"].(string)
	var out, err bytes.Buffer
	code = Run(context.Background(), "dev", []string{"tidy", "apply", "--contract", "4", "--plan", id, "--state-dir", repo.state, "--authorize", "--json"}, &out, &err)
	if code != 0 {
		t.Fatalf("apply %d %s %s", code, out.String(), err.String())
	}
	for _, apply := range []bool{false, true} {
		out.Reset()
		err.Reset()
		args := []string{"tidy", "restore", "--contract", "4", "--plan", id, "--state-dir", repo.state, "--json"}
		if apply {
			args = append(args, "--apply", "--authorize")
		}
		code = Run(context.Background(), "dev", args, &out, &err)
		var value map[string]any
		if e := json.Unmarshal(out.Bytes(), &value); e != nil || code != 0 || value["command"] != "tidy-restore" || value["operation"] != "registration-restore" || value["status"] != "ok" {
			t.Fatalf("restore apply=%v exit=%d result=%s err=%s decode=%v", apply, code, out.String(), err.String(), e)
		}
		_, e := os.Lstat(repo.gitdir)
		if !apply && !os.IsNotExist(e) {
			t.Fatal("inspection wrote registration", e)
		}
		if apply && e != nil {
			t.Fatal("authorized restore missing", e)
		}
	}
}
func TestTidyPruneV4RestoreRejectsMixedGrammar(t *testing.T) {
	for _, extra := range [][]string{{"--apply"}, {"--authorize"}, {"--as", "refs/heads/alias"}, {"--action", "x"}, {"--timeout", "1s"}, {"--limit", "2"}, {"--contract", "4"}, {"--plan", "0123456789abcdef0123456789abcdef"}} {
		args := append([]string{"tidy", "restore", "--contract", "4", "--plan", "0123456789abcdef0123456789abcdef", "--state-dir", "/tmp/no-restore-state", "--json"}, extra...)
		var out, err bytes.Buffer
		code := Run(context.Background(), "dev", args, &out, &err)
		if code != 2 || err.Len() == 0 {
			t.Fatalf("usage reason missing args=%v exit=%d out=%s err=%s", extra, code, out.String(), err.String())
		}
	}
}

func TestTidyPruneV4RestoreHumanDisclosesLimitations(t *testing.T) {
	repo := pruneCLIRepository(t)
	code, planned, diag := pruneV4Invoke(t, repo)
	if code != 0 {
		t.Fatalf("plan %d %s", code, diag)
	}
	id := planned["plan"].(map[string]any)["plan_id"].(string)
	var out, err bytes.Buffer
	code = Run(context.Background(), "dev", []string{"tidy", "apply", "--contract", "4", "--plan", id, "--state-dir", repo.state, "--authorize", "--json"}, &out, &err)
	if code != 0 {
		t.Fatalf("apply %d %s", code, err.String())
	}
	out.Reset()
	err.Reset()
	code = Run(context.Background(), "dev", []string{"tidy", "restore", "--contract", "4", "--plan", id, "--state-dir", repo.state}, &out, &err)
	if code != 0 || !strings.Contains(out.String(), "Only administrative metadata is restored") || !strings.Contains(out.String(), "<owner-selected-found-path>") || strings.Contains(out.String(), `\u003c`) {
		t.Fatalf("human limitations omitted: exit=%d out=%s err=%s", code, out.String(), err.String())
	}
}
