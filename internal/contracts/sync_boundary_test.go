// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// FR-012 (Spec 032): the sync operation is a manual command. Outside its own package, only the sync command file
// may name stwsync.Sync or stwsync.Restore; every other command package (status, overview, pulls, issues, watch,
// tidy ...) imports stwsync for discovery and status types but never starts a sync, so nothing invokes it
// automatically.
func TestOnlyTheSyncCommandInvokesTheSyncOperation(t *testing.T) {
	base := root(t)
	const pkg = "github.com/wstein/sourcesteward/internal/stwsync"
	var found []string
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(base, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(base, path)
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if rel == "internal/stwsync" || d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("%s: %v", rel, err)
			}
			local := ""
			for _, imp := range file.Imports {
				if p, _ := strconv.Unquote(imp.Path.Value); p == pkg {
					local = "stwsync"
					if imp.Name != nil {
						local = imp.Name.Name
					}
				}
			}
			if local == "" || local == "_" {
				return nil
			}
			ast.Inspect(file, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == local && (sel.Sel.Name == "Sync" || sel.Sel.Name == "Restore") {
						found = append(found, rel)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(found)
	if got := strings.Join(found, ","); got != "internal/cli/sync.go,internal/cli/sync.go" {
		t.Fatalf("the sync operation is named in %q; only the two uses in internal/cli/sync.go (Sync, Restore) are allowed", got)
	}
}
