// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"testing"
)

// AllReasons is what the renderer must have a sentence for (Spec 044 SC-010): every reason code a
// candidate can carry. A reason added to the classifier without this list fails here.
func TestAllReasonsListsEveryReasonTheClassifierEmits(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "classify.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			// c.retain("reason", evidence)
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "retain" && len(x.Args) > 0 {
				if lit, ok := x.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					found[unquote(t, lit)] = true
				}
			}
		case *ast.AssignStmt:
			// c.Verdict, c.Reason = VerdictDecision, "locked-stw"  and  c.Reason = "unregistered"
			for i, lhs := range x.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Reason" || len(x.Rhs) != len(x.Lhs) {
					continue
				}
				if lit, ok := x.Rhs[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					found[unquote(t, lit)] = true
				}
			}
		}
		return true
	})
	// The reasons that do not appear as a literal in the classifier: the protections of the
	// derivation, the closed tracking list of the adapter and the native reasons of jj branches.
	for _, r := range []string{"primary-checkout", "current-worktree", "remote-counterpart", "recovery-reference", "lock-mismatch",
		// The adapter hands these codes of workspace admission to the classifier (Admission, Blocked).
		"retirement-parent-foreign"} {
		found[r] = true
	}
	for r := range trackingReasons {
		if r != "github-branch-absent" { // a proof, not a keep reason
			found[r] = true
		}
	}
	listed := map[string]bool{}
	for _, r := range AllReasons() {
		if listed[r] {
			t.Errorf("reason %q is listed twice", r)
		}
		listed[r] = true
	}
	var missing, extra []string
	for r := range found {
		if !listed[r] {
			missing = append(missing, r)
		}
	}
	for r := range listed {
		if !found[r] {
			extra = append(extra, r)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 || len(found) < 20 {
		t.Errorf("AllReasons does not match the classifier: missing %v, not emitted %v (found %d)", missing, extra, len(found))
	}
}

func TestAllReasonsReturnsACopy(t *testing.T) {
	a := AllReasons()
	a[0] = "changed"
	if AllReasons()[0] == "changed" {
		t.Fatal("the catalog is shared with the caller")
	}
}

func unquote(t *testing.T, lit *ast.BasicLit) string {
	t.Helper()
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
