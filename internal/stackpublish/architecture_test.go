// SPDX-License-Identifier: EUPL-1.2

package stackpublish_test

import (
	"encoding/json"
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

// fmtValueNames are the only members of package fmt the core may use: value formatting, never the
// print, scan or Fprint families (stdout, stdin, writers).
var fmtValueNames = map[string]bool{"Errorf": true, "Sprintf": true, "Sprint": true, "Sprintln": true}

// seamViolations reports every use that would let the pure core escape: an import outside the
// allow list, a dot or blank import, a member of time (under any local name) that is not a plain
// value, or a goroutine.
func seamViolations(name, source string) []string {
	file, err := parser.ParseFile(token.NewFileSet(), name, source, parser.SkipObjectResolution)
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	timeName, fmtName := "", ""
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
		if path == "time" || path == "fmt" {
			name := path
			if local != "" {
				name = local
			}
			if path == "time" {
				timeName = name
			} else {
				fmtName = name
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GoStmt:
			out = append(out, "goroutine")
		case *ast.FuncDecl:
			if x.Recv == nil && x.Name.Name == "init" {
				out = append(out, "init function")
			}
		case *ast.SelectorExpr:
			if x.Sel.Name == "Local" || x.Sel.Name == "In" {
				out = append(out, "Local or In reads the local zone: "+x.Sel.Name)
			}
			if id, ok := x.X.(*ast.Ident); ok && fmtName != "" && id.Name == fmtName && !fmtValueNames[x.Sel.Name] {
				out = append(out, fmt.Sprintf("%s.%s is not value formatting", fmtName, x.Sel.Name))
			}
			if id, ok := x.X.(*ast.Ident); ok && timeName != "" && id.Name == timeName && !timeValueNames[x.Sel.Name] {
				out = append(out, fmt.Sprintf("%s.%s is not a plain time value", timeName, x.Sel.Name))
			}
		}
		return true
	})
	return out
}

// seamProbe is one row of the table shared with scripts/test_architecture.py: both checks must
// give the same verdict on the same source.
type seamProbe struct {
	Name      string `json:"name"`
	Source    string `json:"source"`
	Violation bool   `json:"violation"`
}

func TestSeamVerdictsMatchTheSharedProbeTable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "seam_probes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var probes []seamProbe
	if err := json.Unmarshal(raw, &probes); err != nil || len(probes) < 30 {
		t.Fatalf("probe table: %d rows, %v", len(probes), err)
	}
	for _, p := range probes {
		if got := len(seamViolations("probe.go", p.Source)) > 0; got != p.Violation {
			t.Errorf("%s: violation=%v, table says %v", p.Name, got, p.Violation)
		}
	}
}

func TestNoForeignSourceFilesInThePackage(t *testing.T) {
	for _, pattern := range []string{"*.s", "*.S", "*.c", "*.cc", "*.cpp", "*.h", "*.m", "*.f", "*.swig", "*.syso"} {
		if found, _ := filepath.Glob(pattern); len(found) > 0 {
			t.Errorf("the pure core has foreign sources: %v", found)
		}
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
