// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
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
	var found, bad []string
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
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			uses, problems := syncOperationUses(rel, src)
			found = append(found, uses...)
			bad = append(bad, problems...)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(bad) > 0 {
		t.Fatalf("imports of the sync package that hide its use: %q", bad)
	}
	sort.Strings(found)
	if got := strings.Join(found, ","); got != "internal/cli/sync.go,internal/cli/sync.go" {
		t.Fatalf("the sync operation is named in %q; only the two uses in internal/cli/sync.go (Sync, Restore) are allowed", got)
	}
}

// syncOperationUses returns the places in one Go source file that name stwsync.Sync or stwsync.Restore, and the imports of
// the sync package that would hide such a use from this check: a dot import (a bare Sync(...)), a blank import and any
// alias (the check looks for the package name stwsync). Only a plain import is accepted.
func syncOperationUses(name string, src []byte) (uses, problems []string) {
	const pkg = "github.com/wstein/sourcesteward/internal/stwsync"
	file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, []string{name + ": " + err.Error()}
	}
	imported := false
	for _, imp := range file.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p != pkg {
			continue
		}
		if imp.Name != nil {
			problems = append(problems, name+": import of the sync package as "+imp.Name.Name)
			continue
		}
		imported = true
	}
	if !imported {
		return nil, problems
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "stwsync" && (sel.Sel.Name == "Sync" || sel.Sel.Name == "Restore") {
				uses = append(uses, name)
			}
		}
		return true
	})
	return uses, problems
}

func TestSyncBoundaryScannerSeesEveryWayToNameTheOperation(t *testing.T) {
	const imp = "github.com/wstein/sourcesteward/internal/stwsync"
	for name, tc := range map[string]struct {
		src       string
		uses, bad int
	}{
		"plain use of Sync":    {"package p\nimport \"" + imp + "\"\nvar _ = stwsync.Sync\n", 1, 0},
		"plain use of Restore": {"package p\nimport \"" + imp + "\"\nvar _ = stwsync.Restore\n", 1, 0},
		"status types only":    {"package p\nimport \"" + imp + "\"\nvar _ stwsync.RepoStatus\n", 0, 0},
		"dot import":           {"package p\nimport . \"" + imp + "\"\nvar _ = Sync\n", 0, 1},
		"alias import":         {"package p\nimport s \"" + imp + "\"\nvar _ = s.Sync\n", 0, 1},
		"blank import":         {"package p\nimport _ \"" + imp + "\"\n", 0, 1},
		"another package":      {"package p\nimport \"fmt\"\nvar _ = fmt.Sprint\n", 0, 0},
	} {
		uses, bad := syncOperationUses("x.go", []byte(tc.src))
		if len(uses) != tc.uses || len(bad) != tc.bad {
			t.Errorf("%s: uses %v bad %v, want %d and %d", name, uses, bad, tc.uses, tc.bad)
		}
	}
}
