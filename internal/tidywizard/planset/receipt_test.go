// SPDX-License-Identifier: EUPL-1.2

package planset

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// confirmedSet gives every confirmation a fresh handle, as separate /dev/tty opens do.
func confirmedSet(t *testing.T, candidates ...tidywizard.Candidate) (string, Set, *Receipt) {
	t.Helper()
	if len(candidates) == 0 {
		candidates = []tidywizard.Candidate{foreign(tidywizard.KindBranch, "r", "f")}
	}
	request := Request{Candidates: candidates, Chosen: ids(candidates...), Mode: human}
	preview, err := assemble(context.Background(), request, &fakePlanner{})
	if err != nil {
		t.Fatal(err)
	}
	words := confirmWords(preview.Items)
	var terminals []*fakeTTY
	opens := 0
	request.Confirm = func() (io.ReadWriteCloser, error) {
		input := ""
		if opens > 0 && opens <= len(words) {
			input = words[opens-1] + " " + preview.PlanID[:6] + "\n"
		}
		opens++
		terminal := &fakeTTY{in: strings.NewReader(input)}
		terminals = append(terminals, terminal)
		return terminal, nil
	}
	set, receipt, err := Build(context.Background(), request, &fakePlanner{})
	if err != nil || receipt == nil || receipt.state == nil {
		t.Fatalf("Build = %+v, %v, %v; want a confirmed receipt", set, receipt, err)
	}
	if opens != 1+len(words) {
		t.Fatalf("Build opened %d terminals, want precheck plus %d words", opens, len(words))
	}
	for i, word := range words {
		if got := terminals[i+1].out.String(); strings.Count(got, "Foreign items (") != 1 || !strings.Contains(got, "Foreign items ("+word+")") {
			t.Fatalf("word %q was not asked exactly once: %q", word, got)
		}
	}
	dir := stateDir(t)
	if err := Store(dir, set); err != nil {
		t.Fatal(err)
	}
	return dir, set, receipt
}

func receiptRequest(dir string, set Set, receipt *Receipt, applier Applier) ApplyRequest {
	r := req(dir, set, human, applier)
	r.Receipt = receipt
	return r
}

func requireReceiptPrompt(t *testing.T, request ApplyRequest) {
	t.Helper()
	opens := 0
	request.Confirm = func() (io.ReadWriteCloser, error) {
		opens++
		return &fakeTTY{in: strings.NewReader("wrong\n")}, nil
	}
	applier := &fakeApplier{}
	request.Applier = applier
	res := Apply(context.Background(), request)
	if res.Status != "refused" || res.Code != CodeConfirmationRefused || opens != 1 || len(applier.calls) != 0 {
		t.Fatalf("want typed prompt and no effect: %+v, opens=%d, effects=%d", res, opens, len(applier.calls))
	}
}

func TestReceiptSkipsEachConfirmedWordExactlyOnce(t *testing.T) {
	claude := foreign(tidywizard.KindBranch, "r", "claude")
	claude.Owner = "claude"
	unknown := cand(tidywizard.KindBranch, "r", "unknown", tidywizard.VerdictSafe, "", owner.SourceUnknown, owner.ScopeUnknown)
	dir, set, receipt := confirmedSet(t, foreign(tidywizard.KindBranch, "r", "codex"), claude, unknown)
	if !slices.Equal(receipt.state.words, []string{"claude", "codex", "unknown"}) {
		t.Fatalf("confirmed words = %v", receipt.state.words)
	}
	if digest := receipt.state.digest; len(digest) != 64 || digest[:12] != set.PlanID {
		t.Fatalf("receipt must bind the full digest, got %q for %q", digest, set.PlanID)
	}
	app := &fakeApplier{}
	r := receiptRequest(dir, set, receipt, app)
	r.Confirm = func() (io.ReadWriteCloser, error) {
		t.Error("Apply asked a word already confirmed by Build")
		return nil, owner.ErrNoTerminal
	}
	if res := Apply(context.Background(), r); res.Status != "ok" || len(app.calls) != 1 {
		t.Fatalf("same-run apply: %+v, effects=%d", res, len(app.calls))
	}
	requireReceiptPrompt(t, r)
}

func TestReceiptNilZeroAndCopiesCannotSkipAgain(t *testing.T) {
	for _, name := range []string{"nil", "zero", "same pointer", "copy before use", "copy after use"} {
		t.Run(name, func(t *testing.T) {
			dir, set, receipt := confirmedSet(t)
			var next *Receipt
			switch name {
			case "nil":
			case "zero":
				next = &Receipt{}
			default:
				if name == "copy before use" {
					copy := *receipt
					next = &copy
				}
				if res := Apply(context.Background(), receiptRequest(dir, set, receipt, &fakeApplier{})); res.Status != "ok" {
					t.Fatalf("first use: %+v", res)
				}
				if name == "same pointer" {
					next = receipt
				} else if name == "copy after use" {
					copy := *receipt
					next = &copy
				}
			}
			requireReceiptPrompt(t, receiptRequest(dir, set, next, nil))
		})
	}
}

func TestReceiptConcurrentCopiesShareOneUse(t *testing.T) {
	dir, set, receipt := confirmedSet(t)
	copy := *receipt
	start := make(chan struct{})
	results := make(chan ApplyResult, 2)
	var wg sync.WaitGroup
	for _, r := range []*Receipt{receipt, &copy} {
		wg.Add(1)
		go func(r *Receipt) {
			defer wg.Done()
			<-start
			results <- Apply(context.Background(), receiptRequest(dir, set, r, &fakeApplier{}))
		}(r)
	}
	close(start)
	wg.Wait()
	close(results)
	ok, prompted := 0, 0
	for result := range results {
		if result.Status == "ok" {
			ok++
		} else if result.Code == CodeConfirmationRefused {
			prompted++
		} else {
			t.Errorf("unexpected result: %+v", result)
		}
	}
	if ok != 1 || prompted != 1 {
		t.Fatalf("concurrent copies: %d skips, %d prompts; want one each", ok, prompted)
	}
}

func TestReceiptConsumedBeforeEveryRefusal(t *testing.T) {
	for _, name := range []string{"not found", "invalid set", "invalid mode evidence", "agent mode", "scope missing", "digest mismatch", "not authorized"} {
		t.Run(name, func(t *testing.T) {
			dir, set, receipt := confirmedSet(t)
			request := receiptRequest(dir, set, receipt, &fakeApplier{})
			want := CodeConfirmationRefused
			restore := func() {}
			switch name {
			case "not found":
				request.PlanID = "absent"
				want = CodePlanSetNotFound
			case "invalid set":
				path := filepath.Join(dir, "tidy-plan-sets", set.PlanID+".json")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
				restore = func() {
					if err := os.WriteFile(path, raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
				want = CodePlanSetInvalid
			case "invalid mode evidence":
				request.Mode.Mode = "invalid"
				want = CodeModeEvidenceInvalid
			case "agent mode":
				request.Mode = agent
				want = CodeScopeNotAuthorized
			case "scope missing":
				forgedDir, forged := storeForged(t, set, func(s *Set) { s.AuthorizedScopes = nil })
				request.StateDirs, request.PlanID = []string{forgedDir}, forged.PlanID
				want = CodeScopeNotAuthorized
			case "digest mismatch":
				otherDir, other, _ := confirmedSet(t, foreign(tidywizard.KindBranch, "r", "other"))
				request.StateDirs, request.PlanID = []string{otherDir}, other.PlanID
			case "not authorized":
				request.Authorize = false
			}
			if result := Apply(context.Background(), request); result.Code != want || result.Status != "refused" {
				t.Fatalf("first refusal: %+v, want %s", result, want)
			}
			restore()
			requireReceiptPrompt(t, receiptRequest(dir, set, receipt, nil))
		})
	}
}

func TestReceiptDigestAndWordsAreIndependentBindings(t *testing.T) {
	for _, name := range []string{"full digest suffix", "missing word", "extra word", "wrong word", "nil words"} {
		t.Run(name, func(t *testing.T) {
			dir, set, receipt := confirmedSet(t)
			switch name {
			case "full digest suffix":
				// Keep the full short plan id and every word correct. Comparing only that id
				// (or the six-character typed prefix) must not authorize this receipt.
				last := byte('0')
				if receipt.state.digest[63] == last {
					last = '1'
				}
				receipt.state.digest = receipt.state.digest[:63] + string(last)
			case "missing word", "nil words":
				receipt.state.words = []string{}
				if name == "nil words" {
					receipt.state.words = nil
				}
			case "extra word":
				receipt.state.words = []string{"claude", "codex"}
			case "wrong word":
				receipt.state.words = []string{"unknown"}
			}
			requireReceiptPrompt(t, receiptRequest(dir, set, receipt, nil))
		})
	}
}

func TestReceiptCannotReplaceModeScopeOrApplyAuthorization(t *testing.T) {
	for _, name := range []string{"agent", "missing scope", "authorize false"} {
		t.Run(name, func(t *testing.T) {
			dir, set, receipt := confirmedSet(t)
			request := receiptRequest(dir, set, receipt, nil)
			if name == "authorize false" {
				request.Authorize = false
				requireReceiptPrompt(t, request)
				return
			}
			if name == "agent" {
				request.Mode = agent
			} else {
				dir, set = storeForged(t, set, func(s *Set) { s.AuthorizedScopes = nil })
				// Match both receipt bindings so the scope gate is the only cause of refusal.
				receipt.state.digest = set.computeDigest()
				request.StateDirs, request.PlanID = []string{dir}, set.PlanID
			}
			app := &fakeApplier{}
			request.Applier = app
			request.Confirm = func() (io.ReadWriteCloser, error) {
				t.Error("a refused scope or mode reached the confirm loop")
				return nil, owner.ErrNoTerminal
			}
			if result := Apply(context.Background(), request); result.Code != CodeScopeNotAuthorized || len(app.calls) != 0 {
				t.Fatalf("gate bypassed: %+v, effects=%d", result, len(app.calls))
			}
		})
	}
}

func TestReceiptPreservesStaleSubPlanRefusal(t *testing.T) {
	dir, set, receipt := confirmedSet(t)
	app := &fakeApplier{results: map[string]workspace.V3Result{set.SubPlans[0].ID: refusedWith("evidence-changed")}}
	if result := Apply(context.Background(), receiptRequest(dir, set, receipt, app)); result.Status != "failed" || len(result.Outcomes) != 1 || result.Outcomes[0].Status != StatusStale || result.Outcomes[0].Code != "evidence-changed" {
		t.Fatalf("stale evidence was not rechecked: %+v", result)
	}
	requireReceiptPrompt(t, receiptRequest(dir, set, receipt, nil))
}

func TestBuildReturnsNoReceiptWithoutEveryConfirmedWord(t *testing.T) {
	for _, name := range []string{"own only", "agent own", "agent foreign", "wrong word", "second word wrong", "no terminal", "planner error", "empty selection"} {
		t.Run(name, func(t *testing.T) {
			candidate := foreign(tidywizard.KindBranch, "r", "f")
			request := Request{Candidates: []tidywizard.Candidate{candidate}, Chosen: ids(candidate), Mode: human, Confirm: ttyWith("wrong\n")}
			planner := &fakePlanner{}
			wantError := true
			switch name {
			case "own only", "agent own":
				candidate = own(tidywizard.KindBranch, "r", "own")
				request.Candidates, request.Chosen = []tidywizard.Candidate{candidate}, ids(candidate)
				request.Confirm = nil
				wantError = false
				if name == "agent own" {
					request.Mode = agent
				}
			case "agent foreign":
				request.Mode = agent
			case "second word wrong":
				other := foreign(tidywizard.KindBranch, "r", "claude")
				other.Owner = "claude"
				request.Candidates = append(request.Candidates, other)
				request.Chosen = ids(request.Candidates...)
				prefix := planIDFor(t, request.Candidates, request.Chosen, human)
				opens := 0
				request.Confirm = func() (io.ReadWriteCloser, error) {
					opens++
					input := "wrong\n"
					if opens == 2 {
						input = "claude " + prefix + "\n"
					}
					return &fakeTTY{in: strings.NewReader(input)}, nil
				}
			case "no terminal":
				request.Confirm = noTTY
			case "planner error":
				planner.err = errors.New("cannot plan")
			case "empty selection":
				request.Chosen = nil
			}
			_, receipt, err := Build(context.Background(), request, planner)
			if receipt != nil || (err != nil) != wantError {
				t.Fatalf("receipt=%v, err=%v, want nil receipt and error=%t", receipt, err, wantError)
			}
		})
	}
}

func TestReceiptHasOneSharedPointerAndOneBuildAllocation(t *testing.T) {
	typeOf := reflect.TypeFor[Receipt]()
	if typeOf.NumField() != 1 || typeOf.Field(0).IsExported() || typeOf.Field(0).Type.Kind() != reflect.Pointer {
		t.Fatalf("Receipt must contain exactly one hidden state pointer: %v", typeOf)
	}
	stateName := typeOf.Field(0).Type.Elem().Name()
	if stateName == "" || ast.IsExported(stateName) {
		t.Fatalf("receipt state must be an unexported named type: %q", stateName)
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	allocations := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			ast.Inspect(declaration, func(node ast.Node) bool {
				var allocatedType ast.Expr
				switch node := node.(type) {
				case *ast.CompositeLit:
					allocatedType = node.Type
				case *ast.CallExpr:
					if ident, ok := node.Fun.(*ast.Ident); ok && ident.Name == "new" && len(node.Args) == 1 {
						allocatedType = node.Args[0]
					}
				case *ast.ValueSpec:
					allocatedType = node.Type
				}
				if ident, ok := allocatedType.(*ast.Ident); ok && ident.Name == stateName {
					allocations++
					function, ok := declaration.(*ast.FuncDecl)
					if !ok || function.Name.Name != "Build" || function.Recv != nil {
						t.Errorf("%s allocates receipt state outside Build", path)
					}
				}
				return true
			})
		}
	}
	if allocations != 1 {
		t.Fatalf("receipt state allocations = %d, want exactly one in Build", allocations)
	}
}
