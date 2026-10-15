// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestInspectContract(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
		json bool
	}{{[]string{"inspect", "--help"}, 0, false}, {[]string{"inspect", "--json", t.TempDir()}, 1, true}, {[]string{"inspect", t.TempDir()}, 1, false}, {[]string{"inspect", "--json", "a", "b"}, 2, false}, {[]string{"inspect", "--bogus"}, 2, false}} {
		var out, err bytes.Buffer
		code := Run(context.Background(), "dev", tc.args, &out, &err)
		if code != tc.code {
			t.Fatalf("%v code%d out%q err%q", tc.args, code, out.String(), err.String())
		}
		if tc.json {
			var r map[string]any
			if json.Unmarshal(out.Bytes(), &r) != nil || r["status"] != "error" || r["schema_version"] != float64(1) {
				t.Fatalf("%s", out.String())
			}
		} else if tc.code != 0 && out.Len() != 0 {
			t.Fatalf("partial stdout%q", out.String())
		}
		if tc.code != 0 && strings.Count(err.String(), "\n") != 1 {
			t.Fatalf("diagnostic%q", err.String())
		}
	}
}
func TestInspectSuccessOutputFailure(t *testing.T) {
	d := t.TempDir()
	c := exec.Command("git", "init", "-q", "-b", "main", d)
	if b, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%s %v", b, err)
	}
	for _, mode := range []bool{false, true} {
		args := []string{"inspect", d}
		if mode {
			args = append(args, "--json")
		}
		var diagnostic bytes.Buffer
		w := &failOnWrite{fail: 1}
		code := Run(context.Background(), "dev", args, w, &diagnostic)
		if code != 1 || w.calls != 1 || w.buffer.Len() != 0 || strings.Count(diagnostic.String(), "\n") != 1 {
			t.Fatalf("%d %q", code, diagnostic.String())
		}
	}
}
