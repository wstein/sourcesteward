// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// projectExecutionViolations follows function references as well as calls: taking
// a runner entrypoint as a function value cannot hide a new path from a read-only
// command. The reviewed production seam remains package-private.
func projectExecutionViolations(files map[string]string) []string {
	edges := map[string][]string{}
	owners := map[string][]string{}
	violations := []string{}
	for name, text := range files {
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, text, 0)
		if err != nil {
			violations = append(violations, name+":parse")
			continue
		}
		collect := func(key string, node ast.Node) {
			owners[key] = append(owners[key], name)
			ast.Inspect(node, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					edges[key] = append(edges[key], id.Name)
				}
				return true
			})
		}
		for _, d := range f.Decls {
			switch declaration := d.(type) {
			case *ast.FuncDecl:
				if declaration.Body != nil {
					collect(declaration.Name.Name, declaration.Body)
				}
			case *ast.GenDecl:
				for _, spec := range declaration.Specs {
					if value, ok := spec.(*ast.ValueSpec); ok {
						for _, id := range value.Names {
							collect(id.Name, value)
						}
					}
				}
			}
		}
	}
	sinks := map[string]bool{"BuildRelease": true, "buildRelease": true, "executeBuild": true, "executeSmoke": true, "executePromote": true, "executeLocalVerify": true, "runBuildCommand": true, "projectRunner": true, "openBuildCheckoutView": true, "runBuildViewNative": true, "execBuildViewRoute": true, "buildViewExec": true}
	for fn, names := range owners {
		for _, name := range names {
			if strings.HasPrefix(filepath.Base(name), "build") {
				continue
			}
			seen := map[string]bool{}
			var reaches func(string) bool
			reaches = func(f string) bool {
				if sinks[f] {
					return true
				}
				if seen[f] {
					return false
				}
				seen[f] = true
				for _, next := range edges[f] {
					if reaches(next) {
						return true
					}
				}
				return false
			}
			if reaches(fn) {
				violations = append(violations, name+":"+fn)
			}
		}
	}
	return violations
}
func TestBuildExecutionOnlyReachableFromBuild(t *testing.T) {
	files, err := readTree(".")
	if err != nil {
		t.Fatal(err)
	}
	if bad := projectExecutionViolations(files); len(bad) > 0 {
		t.Fatal(bad)
	}
	// Registration and parsed build invocation are the only allowed CLI entry.
	entries, err := os.ReadDir("../cli")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") || entry.Name() == "release_build.go" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("../cli", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), entry.Name(), raw, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if s, ok := n.(*ast.SelectorExpr); ok && s.Sel.Name == "BuildRelease" {
				t.Errorf("project execution referenced from %s", entry.Name())
			}
			return true
		})
	}
}
func TestBuildBoundaryCatchesIndirectAndFunctionValueMutants(t *testing.T) {
	for _, text := range []string{"func Check(){executeBuild()}", "func Check(){bridge()};func bridge(){executeSmoke()}", "func Plan(){f:=BuildRelease;_ = f}", "func dispatch(){helper()};func helper(){r:=projectRunner{};_ = r}", "var hidden = BuildRelease", "func (*reader) Check(){runBuildCommand()}", "func Verify(){openBuildCheckoutView()}", "func Record(){bridge()};func bridge(){runBuildViewNative()}", "func Dispatch(){f:=execBuildViewRoute;_ = f}", "func Plan(){buildViewExec()}"} {
		if bad := projectExecutionViolations(map[string]string{"check.go": "package release;" + text}); len(bad) == 0 {
			t.Fatalf("missed %s", text)
		}
	}
	if bad := projectExecutionViolations(map[string]string{"check.go": "package release;func Check(){readOnly()};func readOnly(){}", "build.go": "package release;func BuildRelease(){runBuildCommand()}"}); len(bad) > 0 {
		t.Fatal(bad)
	}
}
