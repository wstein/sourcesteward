// SPDX-License-Identifier: EUPL-1.2
package readiness

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/inspection"
)

func evaluatorPolicy(t *testing.T) Policy {
	t.Helper()
	p, err := ParsePolicy(designFile(t, "examples/policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func mustEvaluate(t *testing.T, p Policy, s Sample) Result {
	t.Helper()
	r, err := Evaluate(p, s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func evaluatorSample() Sample {
	pass := Component{State: "pass", Complete: true}
	return Sample{ObservedAt: "2026-10-18T19:20:21Z", Backend: "git",
		Identity:   json.RawMessage(`{"schema_version":1,"status":"ok","backend":"git","input_path":".","evidence":[],"repository":{"git_dir":"/r/.git","common_dir":"/r/.git","worktree":"/r","bare":false,"linked_worktree":false,"object_format":"sha1","head":{"state":"branch","ref":"refs/heads/main","oid":"abc"}},"diagnostics":[]}`),
		Components: Components{Review: pass, CI: pass, Constraints: pass, Deployment: pass},
		Boundary:   Boundary{Complete: true},
	}
}
func TestEvaluateResultContractAndNativeIdentity(t *testing.T) {
	// Contract: schema_version / kind = 1 / ready-check; observed_at is RFC3339
	// UTC supplied by the operation; runtime_attestation is always unavailable.
	for _, native := range []inspection.Result{
		{SchemaVersion: 1, Status: "ok", Backend: "git", InputPath: "/git", Evidence: []inspection.Evidence{}, Diagnostics: []inspection.Diagnostic{}, Repository: &inspection.Repository{GitDir: "/git/.git", CommonDir: "/git/.git", Head: inspection.Head{State: "detached"}}},
		{SchemaVersion: 2, Status: "ok", Backend: "jj", InputPath: "/jj", Evidence: []inspection.Evidence{}, Diagnostics: []inspection.Diagnostic{}, JJRepository: &inspection.JJRepository{WorkspaceRoot: "/jj", ChangeID: "change", CommitID: "commit", OperationID: "operation", Observation: inspection.JJObservation{WorkingCopy: "not-snapshotted", Limitation: "sampled"}}},
	} {
		t.Run(native.Backend, func(t *testing.T) {
			s := evaluatorSample()
			s.Backend = native.Backend
			s.Identity = jsonBytes(t, native)
			s.ReviewEvidence.RuntimeAttestation = "verified"
			s.PullRequest = &PullRequest{Number: 17, HeadRepositoryID: nil, BaseRepositoryID: 42, Draft: true}
			s.GitHubReported = GitHubReported{MergeableState: "blocked"}
			r := mustEvaluate(t, evaluatorPolicy(t), s).Value()
			if r.SchemaVersion != 1 || r.Kind != "ready-check" || r.ObservedAt != s.ObservedAt || r.Decision != "eligible" {
				t.Fatalf("contract: %+v", r)
			}
			if r.ReviewEvidence.RuntimeAttestation != "unavailable" || (r.Backend == nil || *r.Backend != native.Backend) || !bytes.Equal(r.Identity, s.Identity) {
				t.Fatalf("native/result metadata changed: %+v", r)
			}
			raw := object(t, jsonBytes(t, r))
			pr := raw["pull_request"].(map[string]any)
			gh := raw["github_reported"].(map[string]any)
			if pr["head_repository_id"] != nil || gh["mergeable"] != nil || gh["rebaseable"] != nil || gh["mergeable_state"] != "blocked" {
				t.Fatalf("nullable raw metadata: %v %v", pr, gh)
			}
		})
	}
}
func TestEvaluateReductionAndCompleteness(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Sample)
		want   string
	}{
		{"all-pass", func(s *Sample) {}, "eligible"},
		{"unknown", func(s *Sample) { s.Components.CI.State = "unknown" }, "indeterminate"},
		{"incomplete-pass", func(s *Sample) { s.Components.Review.Complete = false }, "indeterminate"},
		{"incomplete-boundary", func(s *Sample) { s.Boundary.Complete = false }, "indeterminate"},
		{"blocked-retains-unknown", func(s *Sample) { s.Components.Review.State = "fail"; s.Components.CI.State = "unknown" }, "blocked"},
		{"operational-error-wins", func(s *Sample) {
			s.Components.Review.State = "fail"
			s.Components.CI.State = "unknown"
			s.OperationalError = true
		}, "error"},
		{"zero-components", func(s *Sample) { s.Components = Components{} }, "indeterminate"},
		{"invalid-review-not-required", func(s *Sample) { s.Components.Review.State = "not-required" }, "indeterminate"},
		{"invalid-ci-not-required", func(s *Sample) { s.Components.CI.State = "not-required" }, "indeterminate"},
		{"invalid-constraints-not-required", func(s *Sample) { s.Components.Constraints.State = "not-required" }, "indeterminate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := evaluatorSample()
			tc.mutate(&s)
			r := mustEvaluate(t, evaluatorPolicy(t), s).Value()
			if r.Decision != tc.want {
				t.Fatalf("got %s want %s", r.Decision, tc.want)
			}
			if tc.name == "blocked-retains-unknown" && r.Components.CI.State != "unknown" {
				t.Fatal("unknown lost")
			}
			if (s.OperationalError || !s.Boundary.Complete) && r.Components.Review.Complete {
				t.Fatal("collection failure retained complete review")
			}
		})
	}
}

func TestEvaluateEachRequiredComponentAndErrorPrecedence(t *testing.T) {
	for index, name := range []string{"review", "ci", "constraints", "deployment"} {
		for _, state := range []string{"fail", "unknown", "unrecognized"} {
			t.Run(name+"/"+state, func(t *testing.T) {
				s := evaluatorSample()
				components := []*Component{&s.Components.Review, &s.Components.CI, &s.Components.Constraints, &s.Components.Deployment}
				components[index].State = state
				r := mustEvaluate(t, evaluatorPolicy(t), s).Value()
				want := "indeterminate"
				if state == "fail" {
					want = "blocked"
				}
				if r.Decision != want {
					t.Fatalf("got %s want %s", r.Decision, want)
				}
				s.OperationalError = true
				r = mustEvaluate(t, evaluatorPolicy(t), s).Value()
				if r.Decision != "error" || r.Components.Review.Complete || r.Boundary.Complete {
					t.Fatal("global error must revoke completeness and win reduction")
				}
			})
		}
	}
}

func TestEvaluatePreservesNullableRawBooleansAndBoundsState(t *testing.T) {
	yes, no := true, false
	repoID := int64(123)
	s := evaluatorSample()
	s.PullRequest = &PullRequest{HeadRepositoryID: &repoID}
	s.GitHubReported = GitHubReported{Mergeable: &yes, Rebaseable: &no, MergeableState: strings.Repeat("x", 2048)}
	result := mustEvaluate(t, evaluatorPolicy(t), s)
	r := result.Value()
	if r.GitHubReported.Mergeable == nil || !*r.GitHubReported.Mergeable || r.GitHubReported.Rebaseable == nil || *r.GitHubReported.Rebaseable || r.PullRequest.HeadRepositoryID == nil || *r.PullRequest.HeadRepositoryID != repoID {
		t.Fatal("raw nullable metadata changed")
	}
	// Aggregate mergeable_state and raw booleans are metadata at this layer;
	// future explicit constraint predicates, not this display field, use them.
	if r.Decision != "eligible" {
		t.Fatal("scaffold inferred constraints from raw metadata")
	}
	yes, no, repoID = false, true, 0
	*r.GitHubReported.Mergeable = false
	*r.GitHubReported.Rebaseable = true
	*r.PullRequest.HeadRepositoryID = 0
	fresh := result.Value()
	if !*fresh.GitHubReported.Mergeable || *fresh.GitHubReported.Rebaseable || *fresh.PullRequest.HeadRepositoryID != 123 {
		t.Fatal("result aliases input or returned metadata pointers")
	}
	for _, value := range []string{strings.Repeat("x", 2049), string([]byte{0xff})} {
		s.GitHubReported.MergeableState = value
		if _, err := Evaluate(evaluatorPolicy(t), s); err == nil {
			t.Fatal("invalid raw mergeable_state accepted")
		}
	}
}
func TestEvaluateDeploymentNotRequiredOnlyForUndeployedPolicy(t *testing.T) {
	raw := object(t, designFile(t, "examples/policy.json"))
	for _, profile := range []string{"none", "app"} {
		deployment := raw["deployment"].(map[string]any)
		deployment["profile"] = profile
		p, err := ParsePolicy(jsonBytes(t, raw))
		if err != nil {
			t.Fatal(err)
		}
		s := evaluatorSample()
		s.Components.Deployment.State = "not-required"
		r := mustEvaluate(t, p, s).Value()
		want := "eligible"
		if profile != "none" {
			want = "indeterminate"
		}
		if r.Decision != want {
			t.Fatalf("%s: %s", profile, r.Decision)
		}
	}
}
func TestEvaluateTimestampAndEarlyFailures(t *testing.T) {
	for _, stamp := range []string{"", "2026-10-18T19:20:21-00:00", "2026-10-18T19:20:21+01:00", "2026-02-29T00:00:00Z", "2026-10-18T24:00:00Z", "2026-13-18T00:00:00Z", "2026-10-18T00:00:60Z", "2026-10-18T00:00:00Z\n"} {
		s := evaluatorSample()
		s.ObservedAt = stamp
		if r, err := Evaluate(evaluatorPolicy(t), s); err == nil || len(r.RawJSON()) != 0 {
			t.Fatalf("invalid timestamp %q accepted", stamp)
		}
	}
	for _, stamp := range []string{"2024-02-29T23:59:59Z", "2026-10-18T19:20:21.123456789Z", "2026-10-18T19:20:21+00:00"} {
		s := evaluatorSample()
		s.ObservedAt = stamp
		if r := mustEvaluate(t, evaluatorPolicy(t), s).Value(); r.Decision != "eligible" || r.ObservedAt != stamp {
			t.Fatalf("valid timestamp %q: %+v", stamp, r)
		}
	}
	for _, raw := range []string{"", "null", "[]", "true", "{", `{"backend":"jj"}`, `{"backend":"git","backend":"jj"}`} {
		s := evaluatorSample()
		s.Identity = json.RawMessage(raw)
		if r, err := Evaluate(evaluatorPolicy(t), s); err == nil || len(r.RawJSON()) != 0 {
			t.Fatalf("identity %q accepted", raw)
		}
	}
	s := evaluatorSample()
	s.Backend = ""
	s.Identity = nil
	s.OperationalError = true
	r := mustEvaluate(t, evaluatorPolicy(t), s).Value()
	if r.Decision != "error" || r.Identity != nil || r.Backend != nil {
		t.Fatal("early failure identity must be nullable")
	}
	s = evaluatorSample()
	s.Backend = "gitbutler"
	if _, err := Evaluate(evaluatorPolicy(t), s); err == nil {
		t.Fatal("unsupported native backend")
	}
	if _, err := Evaluate(Policy{}, evaluatorSample()); err == nil {
		t.Fatal("zero policy accepted")
	}
}
func TestEvaluateDeterministicReasonsAndDefensiveSnapshots(t *testing.T) {
	s := evaluatorSample()
	s.Components.Review.Reasons = []Reason{
		{Code: "review-not-clear", Subject: "z", Sources: []SourceReference{{Source: "statuses", Field: "id"}}},
		{Code: "review-missing", Subject: "β"}, {Code: "repository-mismatch", Subject: "repo"}, {Code: "review-missing", Subject: "a"},
	}
	s.Sources = []Source{{Name: "statuses", Outcome: "ok", ObservationIdentities: []string{"11"}}}
	r := mustEvaluate(t, evaluatorPolicy(t), s)
	before := r.RawJSON()
	reasons := r.Value().Components.Review.Reasons
	if got := []string{reasons[0].Code, reasons[1].Subject, reasons[2].Subject, reasons[3].Code}; !reflect.DeepEqual(got, []string{"repository-mismatch", "a", "β", "review-not-clear"}) {
		t.Fatal(got)
	}
	s.Identity[0] = '['
	s.Sources[0].ObservationIdentities[0] = "changed"
	s.Components.Review.Reasons[0].Sources[0].Field = "changed"
	v := r.Value()
	v.Identity[0] = '['
	v.Sources[0].ObservationIdentities[0] = "changed"
	v.Components.Review.Reasons[3].Sources[0].Field = "changed"
	before[0] = '['
	if r.RawJSON()[0] != '{' || r.Value().Sources[0].ObservationIdentities[0] != "11" || r.Value().Components.Review.Reasons[3].Sources[0].Field != "id" {
		t.Fatal("result aliases supplied or returned mutable values")
	}
	s = evaluatorSample()
	s.Components.Review.Reasons = []Reason{{Code: "review-missing", Subject: "b"}, {Code: "review-missing", Subject: "a"}}
	first := mustEvaluate(t, evaluatorPolicy(t), s).RawJSON()
	s.Components.Review.Reasons[0], s.Components.Review.Reasons[1] = s.Components.Review.Reasons[1], s.Components.Review.Reasons[0]
	if !bytes.Equal(first, mustEvaluate(t, evaluatorPolicy(t), s).RawJSON()) {
		t.Fatal("input ordering changed result")
	}
}

// Readiness imports no adapters. fmt is restricted to value-formatting APIs;
// allowing its whole standard-library dependency closure would expose stdout.
func readinessASTError(text string) error {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "source.go", text, parser.ParseComments)
	if err != nil {
		return err
	}
	if err = readinessSyntaxError(file); err != nil {
		return err
	}
	return readinessPublicAPIError(set, []*ast.File{file})
}
func readinessSyntaxError(f *ast.File) error {
	allowed := map[string]bool{"bytes": true, "crypto/sha256": true, "encoding/json": true, "fmt": true, "io": true, "regexp": true, "sort": true, "strconv": true, "strings": true, "unicode/utf8": true}
	aliases := map[string]bool{}
	for _, i := range f.Imports {
		p, _ := strconv.Unquote(i.Path.Value)
		if !allowed[p] {
			return &architectureError{"effect-capable import"}
		}
		if p == "fmt" {
			name := "fmt"
			if i.Name != nil {
				name = i.Name.Name
			}
			if name == "." || name == "_" {
				return &architectureError{"fmt dot/blank import"}
			}
			aliases[name] = true
		}
	}
	var failure error
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			target := call.Fun
			for {
				paren, ok := target.(*ast.ParenExpr)
				if !ok {
					break
				}
				target = paren.X
			}
			if id, ok := target.(*ast.Ident); ok && (id.Name == "print" || id.Name == "println") {
				failure = &architectureError{"built-in output"}
			}
		}
		if d, ok := n.(*ast.FuncDecl); ok && d.Body == nil {
			failure = &architectureError{"bodyless function"}
		}
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && aliases[id.Name] && sel.Sel.Name != "Errorf" && sel.Sel.Name != "Sprintf" {
				failure = &architectureError{"fmt effect-capable selector"}
			}
		}
		return true
	})
	return failure
}

// Pure libraries can invoke caller-defined methods or Reader/Writer callbacks.
// Check the complete public value graph, not just its top-level signature or
// exported fields. Local aliases, promoted methods and recursive types are
// resolved by go/types; private implementation interfaces remain permitted.
func readinessPublicAPIError(set *token.FileSet, files []*ast.File) error {
	config := types.Config{Importer: importer.Default()}
	info := types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	pkg, err := config.Check("readiness", set, files, &info)
	if err != nil {
		return err
	}
	var rawMessage types.Type
	for _, imported := range pkg.Imports() {
		if imported.Path() == "encoding/json" {
			rawMessage = imported.Scope().Lookup("RawMessage").Type()
		}
	}
	seen := map[types.Type]bool{}
	var value func(types.Type) error
	var signature func(*types.Signature) error
	signature = func(sig *types.Signature) error {
		if sig.TypeParams() != nil && sig.TypeParams().Len() > 0 {
			return &architectureError{"public generic function"}
		}
		for i := 0; i < sig.Params().Len(); i++ {
			if err := value(sig.Params().At(i).Type()); err != nil {
				return err
			}
		}
		for i := 0; i < sig.Results().Len(); i++ {
			result := sig.Results().At(i).Type()
			// Built-in error outputs remain possible only after the package-local
			// concrete implementation audit below. They can expose additional
			// capabilities through assertions, fields or exported methods.
			if types.Identical(result, types.Universe.Lookup("error").Type()) {
				continue
			}
			if err := value(result); err != nil {
				return err
			}
		}
		return nil
	}
	value = func(t types.Type) error {
		t = types.Unalias(t)
		if seen[t] {
			return nil
		}
		seen[t] = true
		switch v := t.(type) {
		case *types.Basic:
			if v.Kind() == types.UnsafePointer {
				return &architectureError{"unsafe public value"}
			}
			return nil
		case *types.Pointer:
			return value(v.Elem())
		case *types.Slice:
			return value(v.Elem())
		case *types.Array:
			return value(v.Elem())
		case *types.Map:
			if err := value(v.Key()); err != nil {
				return err
			}
			return value(v.Elem())
		case *types.Struct:
			for i := 0; i < v.NumFields(); i++ {
				if err := value(v.Field(i).Type()); err != nil {
					return err
				}
			}
			return nil
		case *types.Named:
			if owner := v.Obj().Pkg(); owner != nil && owner != pkg {
				// RawMessage is the existing operation-owned JSON byte snapshot, with
				// fixed standard-library methods; no arbitrary implementation can enter.
				if rawMessage != nil && types.Identical(v, rawMessage) {
					return nil
				}
				return &architectureError{"foreign public value capability"}
			}
			if v.TypeParams() != nil && v.TypeParams().Len() > 0 && v.TypeArgs().Len() == 0 {
				return &architectureError{"unbound public generic type"}
			}
			if err := value(v.Underlying()); err != nil {
				return err
			}
			for _, receiver := range []types.Type{v, types.NewPointer(v)} {
				methods := types.NewMethodSet(receiver)
				for i := 0; i < methods.Len(); i++ {
					method := methods.At(i)
					if method.Obj().Exported() {
						if err := signature(method.Type().(*types.Signature)); err != nil {
							return err
						}
					}
				}
			}
			return nil
		default:
			return &architectureError{"non-concrete public input or result"}
		}
	}
	for _, name := range pkg.Scope().Names() {
		object := pkg.Scope().Lookup(name)
		if !object.Exported() {
			continue
		}
		if function, ok := object.(*types.Func); ok {
			if err := signature(function.Type().(*types.Signature)); err != nil {
				return err
			}
		} else if err := value(object.Type()); err != nil {
			return err
		}
	}
	// An error result hides its concrete method set from the public signature.
	// Audit all local implementations, even private ones, and anonymous or
	// instantiated values in expressions. This is deliberately conservative:
	// no body-flow inference is needed to decide which factory returns a type.
	errorInterface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	audited := map[types.Type]bool{}
	auditError := func(t types.Type) error {
		t = types.Unalias(t)
		if audited[t] {
			return nil
		}
		audited[t] = true
		if _, isInterface := t.Underlying().(*types.Interface); isInterface {
			return nil
		}
		base := t
		for {
			pointer, ok := base.(*types.Pointer)
			if !ok {
				break
			}
			base = types.Unalias(pointer.Elem())
		}
		switch concrete := base.(type) {
		case *types.Named:
			if concrete.Obj().Pkg() != pkg {
				return nil
			}
		case *types.Struct: // anonymous error values can promote an embedded Error method
		default:
			return nil
		}
		if types.Implements(t, errorInterface) || types.Implements(types.NewPointer(t), errorInterface) {
			return value(t)
		}
		return nil
	}
	for _, name := range pkg.Scope().Names() {
		if object, ok := pkg.Scope().Lookup(name).(*types.TypeName); ok {
			if err := auditError(object.Type()); err != nil {
				return err
			}
		}
	}
	for _, expression := range info.Types {
		if err := auditError(expression.Type); err != nil {
			return err
		}
	}
	return nil
}

type architectureError struct{ message string }

func (e *architectureError) Error() string { return e.message }
func TestReadinessSourceArchitecture(t *testing.T) {
	set := token.NewFileSet()
	packages := map[string][]*ast.File{}
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(set, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		if err = readinessSyntaxError(file); err != nil {
			t.Errorf("%s: %v", path, err)
		}
		packages[filepath.Dir(path)] = append(packages[filepath.Dir(path)], file)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for path, files := range packages {
		if err := readinessPublicAPIError(set, files); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}
func TestReadinessArchitectureRejectsEffectSelectors(t *testing.T) {
	for _, code := range []string{`import "fmt";func f(){fmt.Print("x")}`, `import f "fmt";var out=f.Printf`, `import . "fmt";func f(){Println("x")}`, `import _ "fmt"`, `func clock() int64`, "import `os`", `import hidden "example.org/helper"`, `func f(){println("external output")}`, `func f(){print("external output")}`} {
		if readinessASTError("package readiness;"+code) == nil {
			t.Fatalf("accepted %s", code)
		}
	}
	for _, code := range []string{`import f "fmt";func format(){_ = f.Sprintf("x")}`, `func pure() int {return 1}`} {
		if err := readinessASTError("package readiness;" + code); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadinessArchitectureRejectsPublicCapabilities(t *testing.T) {
	cases := map[string]string{
		"callback":               `func Indirect(f func()) { f() }`,
		"reader":                 `import "io";func Indirect(r io.Reader) { _,_ = io.ReadAll(r) }`,
		"writer":                 `import "io";func Indirect(w io.Writer) { _,_ = io.WriteString(w,"output") }`,
		"marshaler":              `import "encoding/json";func Indirect(v json.Marshaler) { _,_ = json.Marshal(v) }`,
		"stringer":               `import "fmt";func Indirect(v interface{String() string}) { _ = fmt.Sprintf("%s",v) }`,
		"nested-callback":        `type private struct { Next *private; Call func() };type Public struct { Rows []map[string]*private }`,
		"nested-alias-interface": `import "io";type private = io.Reader;type Public struct { Rows []*private }`,
		"exported-variable":      `var Callback func()`,
		"returned-function":      `func Factory() func(func()) { return func(f func()){f()} }`,
		"public-method":          `type Public struct{};func(Public) Invoke(f func()){f()}`,
		"hidden-type-method":     `type private struct{};func(private) Invoke(f func()){f()};type Public struct{ Value private }`,
		"generic-input":          `func Invoke[T interface{String() string}](v T) {v.String()}`,
		"generic-public-type":    `type Public[T any] struct{ Value T }`,
	}
	for name, code := range cases {
		t.Run(name, func(t *testing.T) {
			if readinessASTError("package readiness;"+code) == nil {
				t.Fatal("accepted public effect capability")
			}
		})
	}
	for name, code := range map[string]string{
		"recursive-concrete":  `type private struct { Next *private; Data []byte };type Public struct { Rows []map[string]*private };func Consume(v Public) Public{return v}`,
		"concrete-formatting": `import "fmt";func Format(v string) string{return fmt.Sprintf("%s",v)}`,
		"concrete-json":       `import "encoding/json";func Encode(v struct{Value string})([]byte,error){return json.Marshal(v)}`,
		"raw-message":         `import "encoding/json";type Public struct{ Value json.RawMessage };func Copy(v Public) Public {return v}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := readinessASTError("package readiness;" + code); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestReadinessArchitectureRejectsParenthesizedOutput(t *testing.T) {
	for _, builtin := range []string{"print", "println"} {
		for _, target := range []string{"(" + builtin + ")", "(((" + builtin + ")))"} {
			if readinessASTError("package readiness;func f(){"+target+"(\"output\")}") == nil {
				t.Fatalf("accepted %s", target)
			}
		}
	}
}

func TestReadinessArchitectureRejectsErrorMediatedCapabilities(t *testing.T) {
	for name, code := range map[string]string{
		"value-method":       `type private struct{};func(private)Error()string{return "diagnostic"};func(private)Invoke(f func()){f()};func New()error{return private{}}`,
		"pointer-method":     `type private struct{};func(*private)Error()string{return "diagnostic"};func(*private)Invoke(f func()){f()};func New()error{return &private{}}`,
		"alias-result":       `type private struct{};func(private)Error()string{return "diagnostic"};func(private)Invoke(f func()){f()};type diagnostic=error;func New()diagnostic{return private{}}`,
		"named-alias":        `type private struct{Invoke func()};func(private)Error()string{return "diagnostic"};type alias=private;func New()error{return alias{}}`,
		"callback-field":     `type private struct{Invoke func()};func(private)Error()string{return "diagnostic"};func New()error{return private{}}`,
		"nested-field":       `type nested struct{Invoke func()};type private struct{Next *private; Nested []map[string]*nested};func(private)Error()string{return "diagnostic"};func New()error{return private{}}`,
		"nested-reader":      `import "io";type private struct{Sources []io.Reader};func(private)Error()string{return "diagnostic"};func New()error{return private{}}`,
		"generic-instance":   `type private[T any] struct{Value T};func(private[T])Error()string{return "diagnostic"};func New()error{return private[func()]{}}`,
		"generic-method":     `type private[T any] struct{};func(private[T])Error()string{return "diagnostic"};func(private[T])Invoke(f func()){f()};func New()error{return private[string]{}}`,
		"anonymous-instance": `func New()error{return struct{error;Invoke func()}{}}`,
		"method-proxy":       `type proxy struct{};func(proxy)Invoke(f func()){f()};type private struct{proxy};func(private)Error()string{return "diagnostic"};func New()error{return private{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if readinessASTError("package readiness;"+code) == nil {
				t.Fatal("accepted error-mediated capability")
			}
		})
	}
	for name, code := range map[string]string{
		"standard-error":          `import "fmt";func New()error{return fmt.Errorf("diagnostic")}`,
		"standard-concrete-error": `import "encoding/json";func New()error{return &json.SyntaxError{Offset:1}}`,
		"pure-value-error":        `type private struct{Message string};func(e private)Error()string{return e.Message};func New()error{return private{"diagnostic"}}`,
		"pure-pointer-error":      `type private struct{Message string};func(e *private)Error()string{return e.Message};func New()error{return &private{"diagnostic"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := readinessASTError("package readiness;" + code); err != nil {
				t.Fatal(err)
			}
		})
	}
}
