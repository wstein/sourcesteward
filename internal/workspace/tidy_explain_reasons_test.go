// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// deletionReasons are the reasons the planners give to candidates that are deleted; they need no
// retention explanation.
var deletionReasons = map[string]bool{
	"native-ancestry": true, "integrated": true, "github-merged-head": true, "github-branch-absent": true,
	"reviewed-integrated": true, "reviewed-drop": true, "clean": true, "patch-id": true, "ancestor": true,
	"merge-noop": true, "no-net-change": true, "integrated-by-native-ancestry": true,
}

// plannerReasons returns every string literal (or string constant) a planner file assigns to a
// Reason or ProtectedReason field.
func plannerReasons(t *testing.T, files ...string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	consts := map[string]string{}
	var parsed []*ast.File
	names := []string{}
	for _, pattern := range files {
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) == 0 {
			t.Fatalf("no planner files for %s", pattern)
		}
		for _, name := range matches {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, name, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			parsed = append(parsed, f)
			names = append(names, name)
			ast.Inspect(f, func(n ast.Node) bool {
				if v, ok := n.(*ast.ValueSpec); ok {
					for i, id := range v.Names {
						if i < len(v.Values) {
							if l, ok := v.Values[i].(*ast.BasicLit); ok && l.Kind == token.STRING {
								consts[id.Name], _ = strconv.Unquote(l.Value)
							}
						}
					}
				}
				return true
			})
		}
	}
	value := func(e ast.Expr) (string, bool) {
		switch x := e.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				s, err := strconv.Unquote(x.Value)
				return s, err == nil
			}
		case *ast.Ident:
			s, ok := consts[x.Name]
			return s, ok
		}
		return "", false
	}
	isReason := func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.Ident:
			return x.Name == "Reason" || x.Name == "ProtectedReason"
		case *ast.SelectorExpr:
			return x.Sel.Name == "Reason" || x.Sel.Name == "ProtectedReason"
		}
		return false
	}
	out := map[string]string{}
	for i, f := range parsed {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.KeyValueExpr:
				if isReason(x.Key) {
					if s, ok := value(x.Value); ok {
						out[s] = names[i]
					}
				}
			case *ast.AssignStmt:
				if len(x.Lhs) == len(x.Rhs) {
					for j := range x.Lhs {
						if isReason(x.Lhs[j]) {
							if s, ok := value(x.Rhs[j]); ok {
								out[s] = names[i]
							}
						}
					}
				}
			}
			return true
		})
	}
	return out
}

// Issue 388: every retain reason the Git and jj planners can produce has its own explanation, so no
// candidate falls back to the generic text. The list is checked against the planner code, not against
// itself.
func TestExplainedReasonsCoverEveryReasonThePlannersProduce(t *testing.T) {
	found := plannerReasons(t, "tidy_git.go", "tidy_v3_proof.go", "tidy_v3_refs.go", "tidy_v3_tracking.go", "tidy_v3_integration_plan.go", "tidy_v3_decisions.go", "tidy_v3_report_plan.go", "tidy_v3_integration_model.go", "../inspection/jj_tidy.go")
	for _, must := range []string{"reviewed-keep", "not-integrated", "remote-counterpart", "recovery-reference", "ambiguous-target", "needs-review", "integration-not-proven", "workspace-used", "remote-source-present"} {
		if _, ok := found[must]; !ok {
			t.Errorf("the scan does not see the planner reason %q: the test would be vacuous", must)
		}
	}
	explained := map[string]bool{}
	for _, r := range ExplainedReasons {
		explained[r] = true
	}
	missing := []string{}
	for reason, file := range found {
		if !explained[reason] && !deletionReasons[reason] {
			missing = append(missing, reason+" ("+filepath.Base(file)+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("planner reasons without an explanation: %v", missing)
	}
}

// Explanations of jj candidates name jj commands, not git commands.
func TestExplainJJCandidatesGetJJCommands(t *testing.T) {
	ctx := ExplainContext{Source: "/repo", Base: "main", Backend: "jj"}
	for _, reason := range []string{"selected-base", "workspace-used", "not-integrated", "remote-counterpart", "recovery-reference", "ambiguous-target"} {
		e := ExplainRetention(ctx, "topic", "", reason, []string{"/ws"}, nil)
		if !strings.HasPrefix(e.NextCommand, "jj -R /repo ") || strings.Contains(e.NextCommand, "git") {
			t.Errorf("%s: %+v", reason, e)
		}
	}
	e := ExplainRetention(ExplainContext{Source: "/repo", Base: "main"}, "refs/heads/t", "", "workspace-used", nil, nil)
	if !strings.HasPrefix(e.NextCommand, "git -C /repo ") {
		t.Errorf("git plan: %+v", e)
	}
}
