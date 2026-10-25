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

// allowedImports is the complete allow list of the pure core. Extend it only in a reviewed change
// that also extends scripts/check-architecture.py (STACKPUBLISH_ALLOWED_IMPORTS).
var allowedImports = map[string]bool{
	"crypto/sha256": true, "encoding/hex": true, "encoding/json": true, "fmt": true, "regexp": true, "sort": true,
	"strconv": true, "strings": true, "sync": true, "time": true, "unicode/utf8": true, attributionImport: true,
}

// timeValueNames are the only members of package time the core may mention: plain values, no clock.
var timeValueNames = map[string]bool{
	"Time": true, "Duration": true, "Nanosecond": true, "Microsecond": true, "Millisecond": true, "Second": true,
	"Minute": true, "Hour": true, "RFC3339": true, "RFC3339Nano": true,
}

// seamViolations reports every use that would let the pure core escape: an import outside the
// allow list, a dot or blank import, a member of time (under any local name) that is not a plain
// value, or a goroutine.
func seamViolations(name, source string) []string {
	file, err := parser.ParseFile(token.NewFileSet(), name, source, parser.SkipObjectResolution)
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	timeName := ""
	for _, spec := range file.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		if !allowedImports[path] {
			out = append(out, "import outside the allow list: "+path)
		}
		local := ""
		if spec.Name != nil {
			local = spec.Name.Name
		}
		if local == "." || local == "_" {
			out = append(out, "dot or blank import of "+path)
		}
		if path == "time" {
			timeName = "time"
			if local != "" {
				timeName = local
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GoStmt:
			out = append(out, "goroutine")
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && timeName != "" && id.Name == timeName && !timeValueNames[x.Sel.Name] {
				out = append(out, fmt.Sprintf("%s.%s is not a plain time value", timeName, x.Sel.Name))
			}
		}
		return true
	})
	return out
}

func TestSeamMutationProbesAreCaught(t *testing.T) {
	probes := map[string]string{
		"os import":            "package p\nimport \"os\"\n",
		"exec import":          "package p\nimport \"os/exec\"\n",
		"net import":           "package p\nimport \"net/http\"\n",
		"x/sys import":         "package p\nimport \"golang.org/x/sys/unix\"\n",
		"hosting import":       "package p\nimport \"github.com/wstein/sourcesteward/internal/hosting\"\n",
		"workspace import":     "package p\nimport \"github.com/wstein/sourcesteward/internal/workspace\"\n",
		"time.Now":             "package p\nimport \"time\"\nvar _ = time.Now()\n",
		"time.Since":           "package p\nimport \"time\"\nvar _ = time.Since(time.Time{})\n",
		"goroutine":            "package p\nfunc f() { go f() }\n",
		"os/user":              "package p\nimport \"os/user\"\nvar _, _ = user.Current()\n",
		"filepath glob":        "package p\nimport \"path/filepath\"\nvar _, _ = filepath.Glob(\"/etc/*\")\n",
		"template files":       "package p\nimport \"text/template\"\nvar _, _ = template.ParseFiles(\"/etc/passwd\")\n",
		"log":                  "package p\nimport \"log\"\nfunc f() { log.Print(1) }\n",
		"aliased time Now":     "package p\nimport t \"time\"\nvar _ = t.Now()\n",
		"aliased LoadLocation": "package p\nimport t \"time\"\nvar _, _ = t.LoadLocation(\"UTC\")\n",
		"dot time":             "package p\nimport . \"time\"\nvar _ = Now()\n",
		"context timeout":      "package p\nimport \"context\"\nvar _, _ = context.WithTimeout(context.Background(), 1)\n",
		"time Local":           "package p\nimport \"time\"\nvar _ = time.Local\n",
	}
	for name, source := range probes {
		if len(seamViolations("probe.go", source)) == 0 {
			t.Errorf("%s: the seam check did not catch the escape", name)
		}
	}
	allowed := "package p\nimport (\n\t\"fmt\"\n\ttm \"time\"\n\t\"" + attributionImport + "\"\n)\nvar _ tm.Duration = tm.Second\nvar _ = fmt.Sprint\n"
	if v := seamViolations("ok.go", allowed); len(v) != 0 {
		t.Errorf("listed std imports, time values and the attribution leaf are allowed: %v", v)
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
