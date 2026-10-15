// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestJJRetirementNativeLifecycle(t *testing.T) {
	requireJJ(t)
	for _, shape := range []string{"standalone", "colocated"} {
		t.Run(shape, func(t *testing.T) {
			root, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			source := filepath.Join(root, "source")
			jjFixture(t, root, "--config", "git.colocate="+map[string]string{"standalone": "false", "colocated": "true"}[shape], "git", "init", source)
			requireJJSandbox(t, source)
			data := []byte{0, 255, 1, 2}
			os.WriteFile(filepath.Join(source, "binary"), data, 0700)
			jjFixture(t, source, "describe", "-m", "base")
			jjFixture(t, source, "bookmark", "create", "main")
			other := filepath.Join(root, "unrelated")
			jjFixture(t, source, "--config", "git.colocate=false", "workspace", "add", "--name", "unrelated", other)
			base, e := ObserveJJWorkspace(context.Background(), source, "main")
			if e != nil {
				t.Fatal(e)
			}
			target := filepath.Join(root, "task")
			os.Mkdir(target, 0700)
			made, e := CreateJJWorkspace(context.Background(), source, base, "task", target)
			if e != nil || !made.SourceUnchanged {
				t.Fatalf("create %+v %v", made, e)
			}
			state, e := ObserveJJRetirement(context.Background(), source, "main", target, "task", base.Source.RepositoryDir)
			if e != nil {
				t.Fatal(e)
			}
			if state.Proof != "empty-working-copy-integrated-parent" || len(state.StoredFiles) != 1 || !state.StoredFiles[0].Executable || state.StoredFiles[0].Size != 4 {
				t.Fatalf("%+v", state)
			}
			before := snapshot(t, target)
			otherBefore := snapshot(t, other)
			gitBefore := snapshot(t, state.Expected.GitDir)
			sourceWC := snapshot(t, filepath.Join(source, ".jj", "working_copy"))
			recovery, fresh, o, e := PrepareJJRetirementRecovery(context.Background(), source, state.Expected, state.Target, "stw-recovery-retire?")
			if e != nil || !o.Complete {
				t.Fatalf("pin %+v %v", o, e)
			}
			o, e = ForgetJJRetirement(context.Background(), source, fresh, state.Target, recovery)
			if e != nil || !o.Complete || o.Expected == nil {
				t.Fatalf("forget %+v %v", o, e)
			}
			if !reflect.DeepEqual(before, snapshot(t, target)) || !reflect.DeepEqual(otherBefore, snapshot(t, other)) || !reflect.DeepEqual(gitBefore, snapshot(t, state.Expected.GitDir)) || !reflect.DeepEqual(sourceWC, snapshot(t, filepath.Join(source, ".jj", "working_copy"))) {
				t.Fatal("forget changed files or source metadata")
			}
			if _, e := VerifyJJWorkspace(context.Background(), source, "task", target, base.Source.RepositoryDir); e == nil {
				t.Fatal("forgotten registration remains")
			}
		})
	}
}
