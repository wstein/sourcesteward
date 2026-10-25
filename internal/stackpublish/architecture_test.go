// SPDX-License-Identifier: EUPL-1.2

package stackpublish_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const attributionImport = "github.com/wstein/sourcesteward/internal/attribution"

var forbiddenImports = map[string]bool{
	"os": true, "os/exec": true, "os/signal": true, "syscall": true, "plugin": true, "unsafe": true,
	"io/ioutil": true, "io/fs": true, "net": true, "crypto/tls": true,
}

var clockCalls = map[string]bool{
	"Now": true, "Sleep": true, "After": true, "AfterFunc": true, "Tick": true,
	"NewTimer": true, "NewTicker": true, "Since": true, "Until": true,
}

// seamViolations reports every use that would let the pure core escape: a forbidden import, a
// module import other than the attribution leaf, a clock read or a goroutine.
func seamViolations(name, source string) []string {
	file, err := parser.ParseFile(token.NewFileSet(), name, source, parser.SkipObjectResolution)
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	for _, spec := range file.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		switch {
		case forbiddenImports[path] || (strings.HasPrefix(path, "net/") && path != "net/url") || strings.HasPrefix(path, "golang.org/x/"):
			out = append(out, "forbidden import "+path)
		case strings.HasPrefix(path, "github.com/wstein/sourcesteward") && path != attributionImport:
			out = append(out, "module import outside the attribution leaf: "+path)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GoStmt:
			out = append(out, "goroutine")
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && id.Name == "time" && clockCalls[x.Sel.Name] {
				out = append(out, fmt.Sprintf("clock call time.%s", x.Sel.Name))
			}
		}
		return true
	})
	return out
}

func TestSeamMutationProbesAreCaught(t *testing.T) {
	probes := map[string]string{
		"os import":        "package p\nimport \"os\"\n",
		"exec import":      "package p\nimport \"os/exec\"\n",
		"net import":       "package p\nimport \"net/http\"\n",
		"x/sys import":     "package p\nimport \"golang.org/x/sys/unix\"\n",
		"hosting import":   "package p\nimport \"github.com/wstein/sourcesteward/internal/hosting\"\n",
		"workspace import": "package p\nimport \"github.com/wstein/sourcesteward/internal/workspace\"\n",
		"time.Now":         "package p\nimport \"time\"\nvar _ = time.Now()\n",
		"time.Since":       "package p\nimport \"time\"\nvar _ = time.Since(time.Time{})\n",
		"goroutine":        "package p\nfunc f() { go f() }\n",
	}
	for name, source := range probes {
		if len(seamViolations("probe.go", source)) == 0 {
			t.Errorf("%s: the seam check did not catch the escape", name)
		}
	}
	allowed := "package p\nimport (\n\t\"time\"\n\t\"net/url\"\n\t\"" + attributionImport + "\"\n)\nvar _ time.Duration\nvar _ url.URL\n"
	if v := seamViolations("ok.go", allowed); len(v) != 0 {
		t.Errorf("types, net/url and the attribution leaf are allowed: %v", v)
	}
}

func TestPackageSourcesStayInsideTheSeam(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no sources: %v", err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if v := seamViolations(path, string(src)); len(v) != 0 {
			t.Errorf("%s leaves the pure seam: %v", path, v)
		}
	}
}
