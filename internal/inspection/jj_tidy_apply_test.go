// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestJJTidyApplyNativeRecovery(t *testing.T) {
	requireJJ(t)
	for _, shape := range []string{"standalone", "colocated"} {
		t.Run(shape, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			repo := filepath.Join(root, "repo")
			jjFixture(t, root, "--config", "git.colocate="+map[string]string{"standalone": "false", "colocated": "true"}[shape], "git", "init", repo)
			requireJJSandbox(t, repo)
			os.WriteFile(filepath.Join(repo, "tracked"), []byte("stored\n"), 0600)
			jjFixture(t, repo, "describe", "-m", "stored")
			jjFixture(t, repo, "bookmark", "create", `"foo?"`)
			jjFixture(t, repo, "bookmark", "create", `"foo?bar"`)
			jjFixture(t, repo, "new")
			jjFixture(t, repo, "bookmark", "create", "main")
			os.WriteFile(filepath.Join(repo, "tracked"), []byte("dirty\n"), 0600)
			os.WriteFile(filepath.Join(repo, "untracked"), []byte("preserve\n"), 0600)
			before, err := ObserveJJTidy(context.Background(), repo, "main")
			if err != nil {
				t.Fatal(err)
			}
			var target JJTidyCandidate
			for _, c := range before.Candidates {
				if c.Name == "foo?" {
					target = c
				}
			}
			if target.Decision != "delete" {
				t.Fatalf("%+v", target)
			}
			recovery, expected, outcome, err := PrepareJJTidyRecovery(context.Background(), repo, before.Expected, target, "stw-recovery-test")
			if err != nil || !outcome.Attempted || !outcome.Complete {
				t.Fatalf("recovery %+v %+v %v", recovery, outcome, err)
			}
			if recovery.CommitID != target.CommitID || recovery.ChangeID != target.ChangeID || expected.Source.OperationID == before.Expected.Source.OperationID {
				t.Fatalf("%+v %+v", recovery, expected)
			}
			outcome, err = DeleteJJTidyBookmark(context.Background(), repo, expected, target, recovery)
			if err != nil || !outcome.Complete {
				t.Fatalf("delete %+v %v", outcome, err)
			}
			after, err := ObserveJJTidy(context.Background(), repo, "main")
			if err != nil {
				t.Fatal(err)
			}
			refs := map[string]JJTidyCandidate{}
			for _, c := range after.Candidates {
				refs[c.Name] = c
			}
			if _, exists := refs[target.Name]; exists || refs["foo?bar"].CommitID != target.CommitID || refs[recovery.Name].CommitID != target.CommitID {
				t.Fatalf("exact cleanup failed: %+v", refs)
			}
			if after.Expected.Source.CommitID != before.Expected.Source.CommitID || after.Expected.Source.ChangeID != before.Expected.Source.ChangeID || after.Expected.SafetyDigest != before.Expected.SafetyDigest || after.Expected.RegistrationDigest != before.Expected.RegistrationDigest {
				t.Fatal("source/registrations changed")
			}
			for p, want := range map[string]string{"tracked": "dirty\n", "untracked": "preserve\n"} {
				b, e := os.ReadFile(filepath.Join(repo, p))
				if e != nil || string(b) != want {
					t.Fatalf("disk %s %q %v", p, b, e)
				}
			}
			staleOutcome, e := DeleteJJTidyBookmark(context.Background(), repo, expected, target, recovery)
			if e == nil || staleOutcome.Attempted {
				t.Fatalf("stale retried %+v %v", staleOutcome, e)
			}
		})
	}
}

func TestJJTidyOperationDeltaValidation(t *testing.T) {
	op := strings.Repeat("a", 128)
	next := strings.Repeat("b", 128)
	for _, parents := range [][]string{{op}, {next}, {op, next}, {}} {
		err := validateJJTidyOperationParents(op, next, parents)
		if (err == nil) != reflect.DeepEqual(parents, []string{op}) {
			t.Fatalf("accepted unexpected operation ancestry %v: %v", parents, err)
		}
	}
}
