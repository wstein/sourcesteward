// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Every product invocation in this suite uses a fake. No product build, compiler,
// fetch, smoke script or promotion executable is started. build_process_test.go
// separately exercises only the trusted synthetic Go test executable.
type fakeBuildRunner struct {
	calls []BuildCommand
	run   func(context.Context, BuildCommand, io.Writer, io.Writer) (int, error)
}

func (f *fakeBuildRunner) Run(ctx context.Context, c BuildCommand, out, errout io.Writer) (int, error) {
	f.calls = append(f.calls, c)
	if f.run != nil {
		return f.run(ctx, c, out, errout)
	}
	return 0, nil
}

func TestBuildEnvironmentIsConstructed(t *testing.T) {
	b := Build{EnvInherit: []string{"PATH", "HOME"}, EnvSet: []string{"X={version}", "HOME=/owned"}}
	got, err := buildEnvironment(b, map[string]string{"version": "v1.2.3"}, []string{"PATH=/tools", "HOME=/old", "GITHUB_TOKEN=hidden", "SECRET=hidden"}, 17)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"HOME=/owned", "LC_ALL=C", "PATH=/tools", "SOURCE_DATE_EPOCH=17", "TZ=UTC", "X=v1.2.3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment %q, want %q", got, want)
	}
	for _, name := range []string{"GITHUB_TOKEN", "HOME_KEY", "ACTIONS_CACHE", "SSH_AUTH_SOCK", "SOURCE_DATE_EPOCH", "TZ", "LC_ALL"} {
		for _, b := range []Build{{EnvInherit: []string{name}}, {EnvSet: []string{name + "=x"}}} {
			if _, err := buildEnvironment(b, nil, nil, 17); err == nil {
				t.Errorf("accepted forbidden %s", name)
			}
		}
	}
}
func TestBuildWorkingDirectoryConfined(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../escape", outside, "link", "missing"} {
		if _, err := buildWorkingDir(root, path); err == nil {
			t.Errorf("accepted %s", path)
		}
	}
	if got, err := buildWorkingDir(root, "."); err != nil || got != root {
		t.Fatalf("%q %v", got, err)
	}
}
func TestBuildRunnerBoundsStreamsAndDeadline(t *testing.T) {
	fake := &fakeBuildRunner{run: func(ctx context.Context, _ BuildCommand, out, errout io.Writer) (int, error) {
		io.WriteString(out, strings.Repeat("x", (1<<20)+19))
		io.WriteString(errout, strings.Repeat("y", (1<<20)+31))
		if _, ok := ctx.Deadline(); !ok {
			t.Error("missing per-invocation deadline")
		}
		return 7, nil
	}}
	result := runBuildCommand(context.Background(), fake, BuildCommand{Argv: []string{"fake-only"}, Timeout: time.Second})
	if result.Exit != 7 || !result.StdoutTruncated || !result.StderrTruncated || len(result.Stdout) != 1<<20 || len(result.Stderr) != 1<<20 {
		t.Fatalf("bad bounded result %+v", result)
	}
}
func TestBuildSubstitutionDoesNotReexpand(t *testing.T) {
	if got := buildSubstitute("{a}:{b}", map[string]string{"a": "{b}", "b": "safe"}); got != "{b}:safe" {
		t.Fatal(got)
	}
}

func fixtureBuildState(t *testing.T) buildState {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist/release"), 0755); err != nil {
		t.Fatal(err)
	}
	c := &Contract{Project: Project{Name: "fixture"}, Build: []Build{{ID: "fake", Scope: "per-target", Argv: []string{"fake-only", "{target}", "{output}", "{version_number}"}, WorkingDir: ".", TimeoutSeconds: 1, Network: "none"}}, Target: []Target{{ID: "linux", Kind: "file", Role: "artifact", Build: "fake", OS: "linux", Arch: "amd64", Output: "dist/release/fixture_{version_number}.txt"}}, Outputs: Outputs{Checksums: "SHA256SUMS", URLTemplate: defaultURLTemplate}, CI: CI{ReleaseWorkflow: ".github/workflows/release-build.yml"}}
	return buildState{root: root, commit: strings.Repeat("a", 40), version: "v1.2.3", epoch: 17, contract: c, configSHA: strings.Repeat("b", 64), repository: "owner/repo", stwVersion: "v0.1.0", run: RecordRun{Workflow: c.CI.ReleaseWorkflow, RunID: 1, RunAttempt: 1}}
}
func TestBuildCollectsCanonicalOutputsUsingOnlyFake(t *testing.T) {
	s := fixtureBuildState(t)
	out := filepath.Join(t.TempDir(), "new")
	f := &fakeBuildRunner{run: func(_ context.Context, c BuildCommand, _, _ io.Writer) (int, error) {
		if c.Argv[0] != "fake-only" {
			t.Fatal(c.Argv)
		}
		return 0, os.WriteFile(filepath.Join(s.root, c.Argv[2]), []byte("fixture bytes"), 0644)
	}}
	r := executeBuild(context.Background(), s, BuildOptions{Mode: "ci", Out: out}, f, nil)
	if r.Status != "ok" {
		t.Fatalf("%+v", r)
	}
	if len(f.calls) != 1 || !reflect.DeepEqual(f.calls[0].Argv, []string{"fake-only", "linux", "dist/release/fixture_1.2.3.txt", "1.2.3"}) {
		t.Fatal(f.calls)
	}
	raw, err := os.ReadFile(filepath.Join(out, "build-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	model, findings := ParseReleaseDocument(raw)
	if len(findings) > 0 {
		t.Fatal(findings)
	}
	m := model.(*BuildManifest)
	if m.Commit != s.commit || len(m.Assets) != 1 || m.Assets[0].File.Name != "fixture_1.2.3.txt" {
		t.Fatal(m)
	}
	checksums, err := os.ReadFile(filepath.Join(out, "SHA256SUMS"))
	if !strings.Contains(string(checksums), buildHash(raw)+"  build-manifest.json\n") {
		t.Fatal("manifest absent from SHA256SUMS", string(checksums))
	}
	if err != nil || !strings.HasSuffix(string(checksums), "  fixture_1.2.3.txt\n") {
		t.Fatalf("%q %v", checksums, err)
	}
}
func TestBuildRefusesExistingOutputBeforeFake(t *testing.T) {
	s := fixtureBuildState(t)
	f := &fakeBuildRunner{}
	r := executeBuild(context.Background(), s, BuildOptions{Mode: "ci", Out: t.TempDir()}, f, nil)
	if r.Error == nil || r.Error.Code != "output-exists" || len(f.calls) != 0 {
		t.Fatalf("%+v calls=%d", r, len(f.calls))
	}
}
func TestBuildRejectsSymlinkOutput(t *testing.T) {
	s := fixtureBuildState(t)
	outside := filepath.Join(t.TempDir(), "file")
	os.WriteFile(outside, []byte("outside"), 0644)
	f := &fakeBuildRunner{run: func(context.Context, BuildCommand, io.Writer, io.Writer) (int, error) {
		return 0, os.Symlink(outside, filepath.Join(s.root, "dist/release/fixture_1.2.3.txt"))
	}}
	r := executeBuild(context.Background(), s, BuildOptions{Mode: "ci", Out: filepath.Join(t.TempDir(), "new")}, f, nil)
	if r.Status == "ok" {
		t.Fatal("collected outside symlink")
	}
}
func TestBuildFilesOnlySkipsImageOnlyBuilds(t *testing.T) {
	s := fixtureBuildState(t)
	s.contract.Build = append(s.contract.Build, Build{ID: "image", Scope: "once", Argv: []string{"never-image"}, TimeoutSeconds: 1})
	s.contract.Target = append(s.contract.Target, Target{ID: "image", Kind: "oci-image", Build: "image"})
	f := &fakeBuildRunner{run: func(_ context.Context, c BuildCommand, _, _ io.Writer) (int, error) {
		if c.Argv[0] != "fake-only" {
			t.Fatal("image invocation", c)
		}
		return 0, os.WriteFile(filepath.Join(s.root, c.Argv[2]), []byte("fixture"), 0644)
	}}
	r := executeBuild(context.Background(), s, BuildOptions{Mode: "ci", FilesOnly: true, Out: filepath.Join(t.TempDir(), "new")}, f, nil)
	if r.Status != "ok" || len(f.calls) != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestBuildOnceAndNetworkLocks(t *testing.T) {
	s := fixtureBuildState(t)
	s.contract.Build[0].Scope = "once"
	s.contract.Build[0].Argv = []string{"fake-once", "{out_dir}"}
	s.contract.Build[0].Network = "fetch"
	s.contract.Build[0].NetworkHosts = []string{"example.org"}
	s.contract.Build[0].LockFiles = []string{"locks/input"}
	second := s.contract.Target[0]
	second.ID = "other"
	second.Output = "dist/release/other.txt"
	s.contract.Target = append(s.contract.Target, second)
	s.readTree = func(p string, _ int) ([]byte, error) {
		if p != "locks/input" {
			t.Fatal(p)
		}
		return []byte("locked"), nil
	}
	f := &fakeBuildRunner{run: func(_ context.Context, c BuildCommand, _, _ io.Writer) (int, error) {
		for _, name := range []string{"fixture_1.2.3.txt", "other.txt", "extra"} {
			if err := os.WriteFile(filepath.Join(c.Argv[1], name), []byte(name), 0644); err != nil {
				return -1, err
			}
		}
		return 0, nil
	}}
	out := filepath.Join(t.TempDir(), "new")
	r := executeBuild(context.Background(), s, BuildOptions{Mode: "ci", Out: out}, f, nil)
	if r.Status != "ok" || len(f.calls) != 1 || len(r.Unexpected) != 1 {
		t.Fatalf("%+v calls=%d", r, len(f.calls))
	}
	raw, _ := os.ReadFile(filepath.Join(out, "build-manifest.json"))
	doc, findings := ParseReleaseDocument(raw)
	if len(findings) > 0 {
		t.Fatal(findings)
	}
	m := doc.(*BuildManifest)
	if m.Builds[0].Network != "fetch" || m.Builds[0].LockFiles[0].SHA256 != buildHash([]byte("locked")) {
		t.Fatal(m.Builds)
	}
}
func TestBuildSmokeIsSeparateFakeInvocation(t *testing.T) {
	s := fixtureBuildState(t)
	s.contract.Target[0].SmokeArgv = []string{"fake-smoke", "{artifact}", "{out_dir}"}
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, "fixture_1.2.3.txt"), []byte("fixture"), 0644)
	f := &fakeBuildRunner{}
	r := executeBuild(context.Background(), s, BuildOptions{Mode: "ci", Smoke: "linux", Out: out}, f, nil)
	if r.Status != "ok" || len(f.calls) != 1 || f.calls[0].Argv[0] != "fake-smoke" || f.calls[0].Dir != s.root || f.calls[0].Argv[1] != filepath.Join(out, "fixture_1.2.3.txt") {
		t.Fatalf("%+v %+v", r, f.calls)
	}
}
func TestBuildFailsBeforeCollectingOnExitOrCancel(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		s := fixtureBuildState(t)
		ctx, stop := context.WithCancel(context.Background())
		f := &fakeBuildRunner{run: func(context.Context, BuildCommand, io.Writer, io.Writer) (int, error) {
			if cancel {
				stop()
			}
			return 9, nil
		}}
		r := executeBuild(ctx, s, BuildOptions{Mode: "ci", Out: filepath.Join(t.TempDir(), "new")}, f, nil)
		stop()
		want := "build-failed"
		if cancel {
			want = "canceled"
		}
		if r.Error == nil || r.Error.Code != want || len(r.Outputs) > 0 {
			t.Fatalf("%+v", r)
		}
	}
}
func TestBuildRefusesInvalidOCIResult(t *testing.T) {
	s := fixtureBuildState(t)
	s.contract.Target = []Target{{ID: "image", Kind: "oci-image", Role: "artifact", Build: "fake", Repository: "ghcr.io/owner/image", Platforms: []string{"linux/amd64"}, ResultFile: "dist/release/result.json"}}
	f := &fakeBuildRunner{run: func(context.Context, BuildCommand, io.Writer, io.Writer) (int, error) {
		return 0, os.WriteFile(filepath.Join(s.root, "dist/release/result.json"), []byte(`{"kind":"oci-build-result","index_digest":"short"}`), 0644)
	}}
	r := executeBuild(context.Background(), s, BuildOptions{Mode: "ci", Out: filepath.Join(t.TempDir(), "new")}, f, nil)
	if r.Status == "ok" {
		t.Fatal("invalid result accepted")
	}
}

func TestBuildCIOnlyNeverReachesProjectRunner(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "false")
	for _, mode := range []string{"ci", "promote", "invalid"} {
		r := BuildRelease(context.Background(), BuildOptions{Mode: mode})
		if r.Error == nil || r.Error.Code != "ci-only" {
			t.Fatalf("%s: %+v", mode, r)
		}
	}
}
func TestBuildExportRejectsHostileTreeEntries(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", ".git/config", "Dir/../bad", "A", "a"} {
		t.Run(name, func(t *testing.T) {
			entries := []buildTreeEntry{{Name: name, Mode: "100644", ID: strings.Repeat("a", 40)}}
			if name == "A" || name == "a" {
				entries = append(entries, buildTreeEntry{Name: strings.ToUpper(name), Mode: "100644", ID: strings.Repeat("b", 40)})
			}
			err := exportBuildTree(t.TempDir(), entries, func(string) ([]byte, error) { return []byte("fixture"), nil })
			if err == nil {
				t.Fatalf("accepted %s", name)
			}
		})
	}
	for _, mode := range []string{"120000", "160000", "040000", "100600"} {
		if err := exportBuildTree(t.TempDir(), []buildTreeEntry{{Name: "unsafe", Mode: mode}}, func(string) ([]byte, error) { return nil, nil }); err == nil {
			t.Errorf("accepted mode %s", mode)
		}
	}
}
func TestBuildExportRegularModesAndExclusiveWrites(t *testing.T) {
	root := t.TempDir()
	entries := []buildTreeEntry{{Name: "dir/executable", Mode: "100755", ID: "fake"}}
	reader := func(string) ([]byte, error) { return []byte("fixture source only"), nil }
	if err := exportBuildTree(root, entries, reader); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(root, "dir/executable"))
	if err != nil || st.Mode().Perm() != 0755 {
		t.Fatalf("%v %v", st, err)
	}
	if err := exportBuildTree(root, entries, reader); err == nil {
		t.Fatal("overwrote exported source")
	}
}

func TestBuildProductionPreflightWithFakeRunnerOnly(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "example/test")
	t.Setenv("GITHUB_RUN_ID", "1")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	for _, scenario := range []string{"clean", "dirty", "head-mismatch", "main-absent", "post-dirty"} {
		t.Run(scenario, func(t *testing.T) {
			dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli"), "tracked": []byte("original")})
			commit := commitAll(t, dir)
			git(t, dir, "remote", "add", "origin", "https://github.com/example/test.git")
			if scenario != "main-absent" {
				git(t, dir, "update-ref", "refs/remotes/origin/main", commit)
			}
			if scenario == "dirty" {
				write(t, dir, "tracked", []byte("dirty"))
			}
			subject := commit
			if scenario == "head-mismatch" {
				write(t, dir, "new", []byte("new"))
				commitAll(t, dir)
			}
			f := &fakeBuildRunner{run: func(_ context.Context, c BuildCommand, _, _ io.Writer) (int, error) {
				if len(c.Argv) != 6 || c.Argv[1] != "scripts/release-package.sh" {
					t.Fatalf("unexpected fake argv %q", c.Argv)
				}
				if scenario == "post-dirty" {
					write(t, dir, "tracked", []byte("modified by fake"))
				}
				p := filepath.Join(dir, c.Argv[5])
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					return -1, err
				}
				return 0, os.WriteFile(p, []byte("FAKE compiler output"), 0644)
			}}
			r := buildRelease(context.Background(), BuildOptions{Mode: "ci", Path: dir, Commit: subject, Version: "v1.2.3", STWVersion: "v1.0.0", Out: filepath.Join(t.TempDir(), "new")}, f, nil)
			if scenario == "clean" {
				if r.Status != "ok" || len(f.calls) != 3 {
					t.Fatalf("%+v calls=%d", r, len(f.calls))
				}
			} else {
				want := "checkout-mismatch"
				if scenario == "main-absent" {
					want = "gate-failed"
				}
				if r.Error == nil || r.Error.Code != want {
					t.Fatalf("%s %+v", scenario, r)
				}
				if scenario != "post-dirty" && len(f.calls) != 0 {
					t.Fatalf("preflight executed fake %d times", len(f.calls))
				}
			}
		})
	}
}
func TestBuildPromotePassesOnlyRecordedDigestAndPropagatesToolRefusal(t *testing.T) {
	raw, err := os.ReadFile("../../specs/026-release-process/examples/oci-bundle.record.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, findings := ParseReleaseDocument(raw)
	if len(findings) > 0 {
		t.Fatal(findings)
	}
	record := doc.(*ReleaseRecord)
	c, findings, err := ValidateContract(loadExample(t, "oci-bundle"), ValidateOptions{Product: "toolbox"})
	if err != nil || c == nil {
		t.Fatalf("%v %v", err, findings)
	}
	s := fixtureBuildState(t)
	s.contract, s.repository, s.version = c, record.Repository, record.Version
	f := &fakeBuildRunner{}
	for i := 0; i < 2; i++ {
		r := executePromote(context.Background(), s, BuildOptions{Mode: "promote"}, record, f, []string{"PATH=/fake", "GITHUB_TOKEN=hidden"})
		if r.Status != "ok" {
			t.Fatalf("%+v", r)
		}
	}
	if len(f.calls) != 2 {
		t.Fatal(f.calls)
	}
	image := record.Assets[0].Image
	want := []string{"crane", "tag", image.Repository + "@" + image.IndexDigest, record.Version}
	for _, call := range f.calls {
		if !reflect.DeepEqual(call.Argv, want) {
			t.Fatalf("%q want %q", call.Argv, want)
		}
		if strings.Contains(strings.Join(call.Env, "\n"), "TOKEN") {
			t.Fatal(call.Env)
		}
	}
	f.run = func(context.Context, BuildCommand, io.Writer, io.Writer) (int, error) { return 1, nil }
	if r := executePromote(context.Background(), s, BuildOptions{Mode: "promote"}, record, f, nil); r.Error == nil || r.Error.Code != "promote-failed" {
		t.Fatalf("%+v", r)
	}
}

func TestBuildDeadlineCancelsFakeAndStopsSequence(t *testing.T) {
	f := &fakeBuildRunner{run: func(ctx context.Context, _ BuildCommand, _, _ io.Writer) (int, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	}}
	start := time.Now()
	r := runBuildCommand(context.Background(), f, BuildCommand{Argv: []string{"fake-never-executed"}, Timeout: time.Millisecond})
	if r.err == nil || r.Exit != -1 || time.Since(start) > time.Second {
		t.Fatalf("%+v", r)
	}
}
func TestBuildLocalVerifyExportsTreeAndComparesFakeHashes(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "example/test")
	t.Setenv("GITHUB_RUN_ID", "1")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli"), "tracked": []byte("original")})
	commit := commitAll(t, dir)
	git(t, dir, "remote", "add", "origin", "https://github.com/example/test.git")
	git(t, dir, "update-ref", "refs/remotes/origin/main", commit)
	fake := &fakeBuildRunner{run: func(_ context.Context, c BuildCommand, _, _ io.Writer) (int, error) {
		p := filepath.Join(c.Dir, c.Argv[5])
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return -1, err
		}
		return 0, os.WriteFile(p, []byte("FAKE deterministic artifact"), 0644)
	}}
	out := filepath.Join(t.TempDir(), "ci")
	r := buildRelease(context.Background(), BuildOptions{Mode: "ci", Path: dir, Commit: commit, Version: "v1.2.3", Out: out}, fake, nil)
	if r.Status != "ok" {
		t.Fatalf("%+v", r)
	}
	t.Setenv("GITHUB_ACTIONS", "false")
	before := snapshot(t, dir)
	local := &fakeBuildRunner{run: fake.run}
	r = buildRelease(context.Background(), BuildOptions{Mode: "local-verify", Path: dir, Commit: commit, Version: "v1.2.3", Record: filepath.Join(out, "build-manifest.json")}, local, nil)
	if r.Status != "ok" || len(local.calls) != 3 {
		t.Fatalf("%+v calls%d", r, len(local.calls))
	}
	for _, c := range local.calls {
		if c.Dir == dir {
			t.Fatal("built inside original checkout")
		}
		if _, err := os.Lstat(c.Dir); !os.IsNotExist(err) {
			t.Fatal("export was retained", err)
		}
	}
	if changes := differences(before, snapshot(t, dir)); len(changes) > 0 {
		t.Fatal(changes)
	}
	local.run = func(_ context.Context, c BuildCommand, _, _ io.Writer) (int, error) {
		p := filepath.Join(c.Dir, c.Argv[5])
		os.MkdirAll(filepath.Dir(p), 0755)
		return 0, os.WriteFile(p, []byte("DIFFERENT fake artifact"), 0644)
	}
	r = buildRelease(context.Background(), BuildOptions{Mode: "local-verify", Path: dir, Commit: commit, Version: "v1.2.3", Record: filepath.Join(out, "build-manifest.json")}, local, nil)
	if r.Status != "mismatch" || r.Error == nil || r.Error.Code != "artifact-mismatch" {
		t.Fatalf("%+v", r)
	}
	write(t, dir, ".git/info/grafts", []byte{})
	graftFake := &fakeBuildRunner{}
	r = buildRelease(context.Background(), BuildOptions{Mode: "local-verify", Path: dir, Commit: commit, Version: "v1.2.3", Record: filepath.Join(out, "build-manifest.json")}, graftFake, nil)
	if r.Error == nil || r.Error.Code != "export-unsafe" || len(graftFake.calls) != 0 {
		t.Fatalf("grafts: %+v calls%d", r, len(graftFake.calls))
	}
}
func TestBuildSmokeAcceptsArtifactLargerThanProcessOutputCap(t *testing.T) {
	s := fixtureBuildState(t)
	s.contract.Target[0].SmokeArgv = []string{"fake-smoke", "{artifact}"}
	out := t.TempDir()
	f, err := os.Create(filepath.Join(out, "fixture_1.2.3.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(2 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	fake := &fakeBuildRunner{}
	r := executeSmoke(context.Background(), s, BuildOptions{Mode: "ci", Smoke: "linux", Out: out}, fake, nil)
	if r.Status != "ok" || len(fake.calls) != 1 {
		t.Fatalf("%+v", r)
	}
}
func TestBuildProductionCancellationReportsCanceled(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	commit := commitAll(t, dir)
	git(t, dir, "remote", "add", "origin", "https://github.com/example/test.git")
	git(t, dir, "update-ref", "refs/remotes/origin/main", commit)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &fakeBuildRunner{run: func(context.Context, BuildCommand, io.Writer, io.Writer) (int, error) {
		cancel()
		return -1, context.Canceled
	}}
	r := buildRelease(ctx, BuildOptions{Mode: "ci", Path: dir, Commit: commit, Version: "v1.2.3", Out: filepath.Join(t.TempDir(), "new")}, fake, nil)
	if r.Error == nil || r.Error.Code != "canceled" {
		t.Fatalf("%+v", r)
	}
}
func TestBuildExecutableUsesOnlyConstructedPATH(t *testing.T) {
	host := t.TempDir()
	declared := t.TempDir()
	for _, dir := range []string{host, declared} {
		os.WriteFile(filepath.Join(dir, "fake-tool"), []byte("fixture; never executed"), 0755)
	}
	t.Setenv("PATH", host)
	c := BuildCommand{Argv: []string{"fake-tool"}, Env: []string{"PATH=" + declared}, Dir: t.TempDir()}
	got, err := buildExecutable(c)
	if err != nil || got != filepath.Join(declared, "fake-tool") {
		t.Fatalf("%q %v", got, err)
	}
	c.Env = nil
	if _, err := buildExecutable(c); err == nil {
		t.Fatal("used inherited PATH")
	}
}
func TestBuildHumanOutputNotesDiscardedStreams(t *testing.T) {
	r := newBuildResult("ci")
	r.Builds = []BuildExecution{{ID: "fake", Invocations: []BuildInvocationResult{{StdoutTruncated: true, StderrTruncated: true}}}}
	text := r.Text()
	if !strings.Contains(text, "stdout truncated") || !strings.Contains(text, "stderr truncated") {
		t.Fatal(text)
	}
}

func TestBuildLocalVerifyRejectsTrustedOCITargetBeforeExport(t *testing.T) {
	for _, imageOnly := range []bool{false, true} {
		for _, filesOnly := range []bool{false, true} {
			s := fixtureBuildState(t)
			if imageOnly {
				s.contract.Target = nil
			}
			s.contract.Target = append(s.contract.Target, Target{ID: "image", Kind: "oci-image", Build: "image"})
			record := &BuildManifest{Commit: s.commit, Version: s.version, Assets: []RecordAsset{}}
			fake := &fakeBuildRunner{}
			r := executeLocalVerify(context.Background(), s, BuildOptions{Mode: "local-verify", FilesOnly: filesOnly}, record, nil, fake, nil)
			if r.Error == nil || r.Error.Code != "unsupported-target" || len(fake.calls) != 0 {
				t.Fatalf("%+v calls%d", r, len(fake.calls))
			}
		}
	}
}
