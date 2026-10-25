// SPDX-License-Identifier: EUPL-1.2

package stackpublish_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/attribcmd"
	"github.com/wstein/sourcesteward/internal/attribution"
	"github.com/wstein/sourcesteward/internal/cli"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// These tests characterize what exists today (Spec 016 T001). They pin the current behavior the
// new package must not change: they do not describe the proposed 016 surface.

var characterizationNow = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

func repoFile(t *testing.T, rel string) string {
	t.Helper()
	return filepath.Join("..", "..", filepath.FromSlash(rel))
}

func parseGo(t *testing.T, path string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func nonTestGoFiles(t *testing.T, dir string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(repoFile(t, dir), "*.go"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no sources in %s: %v", dir, err)
	}
	var out []string
	for _, p := range paths {
		if !strings.HasSuffix(p, "_test.go") {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// TestHostingNonGetMethodsAreConfinedToTheReleaseDispatchEdge pins the closed write surface of the
// current hosting package: GET everywhere, one fixed POST in dispatch.go for the release workflow,
// and one fixed POST in stack_write.go for the Spec 016 Ready mutation (the reviewed allowlist entry
// of T021). A further write file would change this set and need its own reviewed allowlist.
func TestHostingNonGetMethodsAreConfinedToTheReleaseDispatchEdge(t *testing.T) {
	writers := map[string][]string{}
	for _, path := range nonTestGoFiles(t, "internal/hosting") {
		ast.Inspect(parseGo(t, path), func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && id.Name == "http" && strings.HasPrefix(x.Sel.Name, "Method") && x.Sel.Name != "MethodGet" {
					writers[filepath.Base(path)] = append(writers[filepath.Base(path)], x.Sel.Name)
				}
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					if s, err := strconv.Unquote(x.Value); err == nil && (s == "POST" || s == "PUT" || s == "PATCH" || s == "DELETE") {
						writers[filepath.Base(path)] = append(writers[filepath.Base(path)], s)
					}
				}
			}
			return true
		})
	}
	if len(writers) != 2 || len(writers["dispatch.go"]) == 0 || len(writers["stack_write.go"]) == 0 {
		t.Fatalf("non-GET methods outside the fixed release dispatch and Ready write edges: %v", writers)
	}
	for _, file := range []string{"dispatch.go", "stack_write.go"} {
		for _, m := range writers[file] {
			if m != "MethodPost" {
				t.Errorf("%s uses %s; only the fixed POST is characterized", file, m)
			}
		}
	}
}

func agentOrigin(environ ...string) attribution.Origin {
	return attribcmd.OriginForRun(environ, false, false, characterizationNow)
}

// TestModeAndD5MatrixAsImplementedToday pins the Spec 041 mode kinds and every Spec 042 D5 row
// that a push or Ready gate must reuse unchanged.
func TestModeAndD5MatrixAsImplementedToday(t *testing.T) {
	recorded := &attribution.Origin{Kind: "claude", SessionID: "recorded-session"}
	read := func(rec workspace.TaskRecord, found bool, err error) func(string, string) (workspace.TaskRecord, bool, error) {
		return func(string, string) (workspace.TaskRecord, bool, error) { return rec, found, err }
	}
	human := attribcmd.OriginForRun([]string{"HOME=/h"}, true, true, characterizationNow)
	same := agentOrigin("CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=recorded-session")
	other := agentOrigin("CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=other-session")
	absent := agentOrigin("CLAUDECODE=1")
	codex := agentOrigin("CODEX_HOME=/x")
	if codex.Kind != "codex" || codex.SessionID != "" {
		t.Fatalf("a Codex session source is unmeasured and stays absent: %+v", codex)
	}
	cases := []struct {
		name    string
		current attribution.Origin
		rec     workspace.TaskRecord
		found   bool
		err     error
		want    string // "" allowed
	}{
		{"same session", same, workspace.TaskRecord{Origin: recorded}, true, nil, ""},
		{"different session", other, workspace.TaskRecord{Origin: recorded}, true, nil, attribution.ReasonMismatch},
		{"recorded session and current absent", absent, workspace.TaskRecord{Origin: recorded}, true, nil, attribution.ReasonUnknown},
		{"codex current without a session", codex, workspace.TaskRecord{Origin: recorded}, true, nil, attribution.ReasonUnknown},
		{"human with a recorded session", human, workspace.TaskRecord{Origin: recorded}, true, nil, ""},
		{"task without a record", other, workspace.TaskRecord{}, false, nil, ""},
		{"task record without a recorded session", other, workspace.TaskRecord{Origin: &attribution.Origin{Kind: "claude"}}, true, nil, ""},
		{"task record without origin", other, workspace.TaskRecord{}, true, nil, ""},
		{"unreadable record refuses an agent", other, workspace.TaskRecord{}, false, errors.New("unreadable"), "unreadable"},
	}
	for _, tc := range cases {
		err := attribcmd.RemoteWriteGate(read(tc.rec, tc.found, tc.err), "state", "task", tc.current)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: refused %v", tc.name, err)
		case tc.want != "" && err == nil:
			t.Errorf("%s: allowed, want %s", tc.name, tc.want)
		case tc.want != "" && !strings.Contains(err.Error(), tc.want):
			t.Errorf("%s: %v, want %s", tc.name, err, tc.want)
		}
		if err != nil && strings.Contains(err.Error(), "recorded-session") {
			t.Errorf("%s: the refusal carries a session id: %v", tc.name, err)
		}
	}
	if err := attribcmd.RemoteWriteGate(read(workspace.TaskRecord{}, false, errors.New("must not be read")), "state", "task", human); err != nil {
		t.Errorf("a human run is never blocked and reads nothing: %v", err)
	}
}

// TestHiddenRestackIncompleteStillExitsThree pins the CURRENT mapping of the hidden Spec 015 v1
// planner (incomplete is exit 3) separately from the decided revision-2 target (1). Changing it
// is the explicit 015 revision-2 task; this test is then updated in that change, not here.
func TestHiddenRestackIncompleteStillExitsThree(t *testing.T) {
	file := parseGo(t, repoFile(t, "internal/cli/root.go"))
	var found bool
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "exitStatus" {
			return true
		}
		stmts := fn.Body.List
		for i, s := range stmts {
			decl, ok := s.(*ast.DeclStmt)
			if !ok || i+1 >= len(stmts) {
				continue
			}
			spec, ok := decl.Decl.(*ast.GenDecl).Specs[0].(*ast.ValueSpec)
			if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "incomplete" {
				continue
			}
			ifStmt, ok := stmts[i+1].(*ast.IfStmt)
			if !ok {
				t.Fatal("exitStatus no longer has the errors.As block after the restackIncomplete declaration")
			}
			ret := ifStmt.Body.List[0].(*ast.ReturnStmt)
			call := ret.Results[0].(*ast.CallExpr)
			if lit, ok := call.Args[2].(*ast.BasicLit); !ok || lit.Value != "3" {
				t.Errorf("hidden restack plan incomplete no longer maps to exit 3: %v", call.Args[2])
			}
			found = true
		}
		return false
	})
	if !found {
		t.Fatal("restackIncomplete mapping not found in exitStatus")
	}
}

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cli.Run(context.Background(), "dev", args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// TestRestackStaysHiddenAndNoStackCommandExists pins that the hidden planner is not advertised and
// that no `stack` command exists before this feature adds one; bare help emits no JSON result.
func TestRestackStaysHiddenAndNoStackCommandExists(t *testing.T) {
	code, out, _ := run(t, "--help")
	if code != 0 || strings.Contains(out, "restack") || strings.Contains(out, "stack ") || strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("root help exit %d advertises a restack/stack command or emits JSON:\n%s", code, out)
	}
	if code, out, _ = run(t, "stack", "publish"); code != 2 || out != "" {
		t.Fatalf("an unknown `stack` command must be usage 2 with no result object: exit %d %q", code, out)
	}
	if code, out, _ = run(t, "restack", "plan"); code != 2 || out != "" {
		t.Fatalf("restack plan without its required flags is usage 2 before any operation: exit %d %q", code, out)
	}
}

// TestRestackReplayDoesNotYetPinAttributeSource characterizes the separately tracked issue #241
// (replay attributes come from the original worktree). It claims no repair: when #241 lands this
// test is replaced by that change's own red-first test.
func TestRestackReplayDoesNotYetPinAttributeSource(t *testing.T) {
	for _, path := range nonTestGoFiles(t, "internal/restack") {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{"--attr-source", "GIT_ATTR_SOURCE"} {
			if bytes.Contains(raw, []byte(marker)) {
				t.Errorf("%s mentions %s: issue #241 appears fixed; replace this characterization", filepath.Base(path), marker)
			}
		}
	}
}

// TestPublishedSchemasAreUntouchedAndNoForgeEffectSchemaExists pins that this feature publishes no
// schema (SC-003): the restack-plan v1 bytes stay frozen and no schema or fixture names the
// UNPUBLISHED forge-effect-plan kind.
func TestPublishedSchemasAreUntouchedAndNoForgeEffectSchemaExists(t *testing.T) {
	raw, err := os.ReadFile(repoFile(t, "schemas/v1/restack-plan.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != "da0bdc60273a30a0f2e39482f534cb80f15f619451f2167ad0780cc44a2b5b48" {
		t.Errorf("restack-plan v1 schema bytes changed: %s", got)
	}
	for _, dir := range []string{"schemas", "internal/cli/testdata"} {
		err := filepath.WalkDir(repoFile(t, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
				return err
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if bytes.Contains(body, []byte("forge-effect-plan")) {
				t.Errorf("%s names the unpublished forge-effect-plan kind", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestTheNodeIdGrammarsOfTheActionAndTheWriteEdgeAreEqual ties the two copies of the pull request
// node id grammar: both literals are extracted and compared once the anchors are normalized.
func TestTheNodeIdGrammarsOfTheActionAndTheWriteEdgeAreEqual(t *testing.T) {
	re := regexp.MustCompile("nodeIDGrammar\\s*=\\s*regexp\\.MustCompile\\(`([^`]+)`\\)")
	grammar := func(path string) string {
		raw, err := os.ReadFile(repoFile(t, path))
		if err != nil {
			t.Fatal(err)
		}
		m := re.FindStringSubmatch(string(raw))
		if m == nil {
			t.Fatalf("no nodeIDGrammar literal in %s", path)
		}
		return strings.NewReplacer(`^`, "", `$`, "", `\A`, "", `\z`, "").Replace(m[1])
	}
	a, b := grammar("internal/stackpublish/ready_action.go"), grammar("internal/hosting/stack_write.go")
	if a != b || a != "[A-Za-z0-9_=-]{4,128}" {
		t.Fatalf("grammars differ: %q %q", a, b)
	}
}
