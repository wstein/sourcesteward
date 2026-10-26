// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestTidyPruneV4ApplyExplicitDispatch(t *testing.T) {
	repo := pruneCLIRepository(t)
	code, planned, diag := pruneV4Invoke(t, repo)
	if code != 0 {
		t.Fatalf("plan %d %s %+v", code, diag, planned)
	}
	plan := planned["plan"].(map[string]any)
	id := plan["plan_id"].(string)
	for _, authorize := range []bool{false, true} {
		args := []string{"tidy", "apply", "--contract", "4", "--plan", id, "--state-dir", repo.state, "--json"}
		if authorize {
			args = append(args, "--authorize")
		}
		var out, err bytes.Buffer
		code = Run(context.Background(), "dev", args, &out, &err)
		var value map[string]any
		if e := json.Unmarshal(out.Bytes(), &value); e != nil {
			t.Fatalf("versioned result: %v %s stderr=%s", e, out.String(), err.String())
		}
		if !authorize {
			if code != 1 || value["status"] != "refused" || value["diagnostics"].([]any)[0].(map[string]any)["code"] != "authorization-required" {
				t.Fatalf("refusal %d %+v", code, value)
			}
		} else if code != 0 || value["status"] != "ok" || value["schema_version"] != float64(4) {
			t.Fatalf("apply %d %+v stderr %s", code, value, err.String())
		}
	}
	if _, e := os.Lstat(repo.gitdir); !os.IsNotExist(e) {
		t.Fatal("registration not removed", e)
	}
	if _, e := os.Stat(repo.moved); e != nil {
		t.Fatal("moved user checkout changed", e)
	}
}
func TestTidyPruneV4ApplyRejectsMixedGrammar(t *testing.T) {
	for _, extra := range [][]string{{"--yes"}, {"--online"}, {"--contract", "4"}, {"--plan", "0123456789abcdef0123456789abcdef"}, {"--timeout", "1s"}, {"--human"}} {
		args := append([]string{"tidy", "apply", "--contract", "4", "--plan", "0123456789abcdef0123456789abcdef", "--state-dir", "/tmp/no-state", "--json"}, extra...)
		var out, err bytes.Buffer
		code := Run(context.Background(), "dev", args, &out, &err)
		if code != 2 || err.Len() == 0 {
			t.Fatalf("usage refusal %v: %d %s %s", extra, code, out.String(), err.String())
		}
	}
}
