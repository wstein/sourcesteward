// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestCoreImportsNoNetworkProcessOrWriteHelper is the source half of US10: the core is read-only towards
// every forge because it cannot reach one. It imports no network, process or hosting package.
func TestCoreImportsNoNetworkProcessOrWriteHelper(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p == "net" || strings.HasPrefix(p, "net/") || p == "os/exec" || strings.Contains(p, "internal/hosting") ||
				strings.Contains(p, "internal/cli") || strings.Contains(p, "go-gh") || p == "crypto/tls" {
				t.Errorf("%s imports %s", f, p)
			}
		}
	}
}

// TestNothingOutsideTheStateDirectoryIsWritten runs a cycle and lists the directory: only the files of the layout.
func TestNothingOutsideTheStateDirectoryIsWritten(t *testing.T) {
	r := newRig(t)
	r.must(ok(mk(1)))
	r.must(ok(mk(1), mk(2)))
	entries, _ := os.ReadDir(r.dir)
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = true
	}
	for _, want := range []string{"lock", "state.json", "journal.jsonl", "heartbeat"} {
		if !got[want] {
			t.Errorf("missing %s", want)
		}
		delete(got, want)
	}
	if len(got) != 0 {
		t.Fatalf("unexpected files: %v", got)
	}
}
