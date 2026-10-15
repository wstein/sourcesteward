// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestInspectHelpIsBackendLocal(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var out, diagnostic bytes.Buffer
	if code := Run(context.Background(), "dev", []string{"inspect", "--help"}, &out, &diagnostic); code != 0 || diagnostic.Len() != 0 || !strings.Contains(out.String(), "native jj") {
		t.Fatalf("%d %q %q", code, out.String(), diagnostic.String())
	}
}

func TestInspectJJHumanAndJSON(t *testing.T) {
	if _, e := exec.LookPath("jj"); e != nil {
		if os.Getenv("STW_REQUIRE_JJ") == "1" {
			t.Fatal("mandatory jj missing")
		}
		t.Skip("jj unavailable")
	}
	root := t.TempDir()
	d := filepath.Join(root, "<>& space\n日本語")
	c := exec.Command("jj", "git", "init", d)
	c.Env = append(os.Environ(), "JJ_CONFIG=")
	if b, e := c.CombinedOutput(); e != nil {
		t.Fatalf("%s %v", b, e)
	}
	d, _ = filepath.EvalSymlinks(d)
	var out, diagnostic bytes.Buffer
	code := Run(context.Background(), "dev", []string{"inspect", d, "--json"}, &out, &diagnostic)
	var r map[string]any
	if e := json.Unmarshal(out.Bytes(), &r); e != nil {
		t.Fatal(e)
	}
	if code != 0 && os.Getenv("STW_REQUIRE_JJ") != "1" {
		t.Skip("native sandbox unavailable")
	}
	if code != 0 || r["schema_version"] != float64(2) || r["backend"] != "jj" || r["repository"] != nil || diagnostic.Len() != 0 {
		t.Fatalf("%d %s %s", code, out.String(), diagnostic.String())
	}
	out.Reset()
	diagnostic.Reset()
	code = Run(context.Background(), "dev", []string{"inspect", d}, &out, &diagnostic)
	if code != 0 || diagnostic.Len() != 0 || !strings.Contains(out.String(), "Workspace: "+strconv.Quote(d)) || !strings.Contains(out.String(), "Change ID: ") || !strings.Contains(out.String(), "Operation ID: ") || !strings.Contains(out.String(), "were not snapshotted") {
		t.Fatalf("%d %q %q", code, out.String(), diagnostic.String())
	}
	if strings.Contains(out.String(), "Git directory:") {
		t.Fatal("synthetic Git fields in native result")
	}
}

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
