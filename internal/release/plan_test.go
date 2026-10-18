// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/inspection"
)

func planRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "skill-pack")})
	commit := commitAll(t, dir)
	git(t, dir, "remote", "add", "upstream", "https://github.com/example/skills.git")
	git(t, dir, "update-ref", "refs/remotes/upstream/main", commit)
	return dir, commit
}

func TestCommitPlanGateMutations(t *testing.T) {
	for _, tc := range []struct {
		name, config, version, want, outcome string
		setup                                func(*testing.T, string)
	}{
		{name: "no readiness", config: "go-cli", version: "v1.2.3", want: "readiness-missing", outcome: "not-applicable"},
		{name: "invalid version", config: "go-cli", version: "1.2.3", want: "version-invalid", outcome: "fail"},
		{name: "invalid config", config: "go-cli", version: "v1.2.3", want: "config-invalid", outcome: "fail", setup: func(t *testing.T, d string) { write(t, d, ".stw/release.toml", []byte("schema_version = 2\n")) }},
		{name: "empty readiness", config: "skill-pack", version: "v1.2.3", want: "readiness-missing", outcome: "fail", setup: func(t *testing.T, d string) {
			for _, p := range []string{"readiness/skills-validation.json", "readiness/compatibility.json", "schemas/skill.schema.json"} {
				write(t, d, p, []byte{})
			}
		}},
		{name: "ready skill pack", config: "skill-pack", version: "v1.2.3", want: "readiness-missing", outcome: "pass", setup: func(t *testing.T, d string) {
			for _, p := range []string{"readiness/skills-validation.json", "readiness/compatibility.json", "schemas/skill.schema.json"} {
				write(t, d, p, []byte("evidence"))
			}
		}},
		{name: "symlink readiness", config: "skill-pack", version: "v1.2.3", want: "readiness-missing", outcome: "fail", setup: func(t *testing.T, d string) {
			write(t, d, "readiness/skills-validation.json", []byte("evidence"))
			write(t, d, "readiness/compatibility.json", []byte("evidence"))
			write(t, d, "schemas/real.json", []byte("evidence"))
			if err := os.Symlink("real.json", filepath.Join(d, "schemas/skill.schema.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "version file mismatch", config: "go-cli", version: "v1.2.3", want: "version-file-mismatch", outcome: "fail", setup: func(t *testing.T, d string) {
			raw := strings.Replace(string(exampleBytes(t, "go-cli")), `source = "input"`, `source = "file"`+"\nfile = 'VERSION'\nfile_pattern = '^(.+)$'\nfile_form = 'with-v'", 1)
			write(t, d, ".stw/release.toml", []byte(raw))
			write(t, d, "VERSION", []byte("v9.0.0"))
		}},
		{name: "matching version file", config: "go-cli", version: "v1.2.3", want: "version-file-mismatch", outcome: "pass", setup: func(t *testing.T, d string) {
			raw := strings.Replace(string(exampleBytes(t, "go-cli")), `source = "input"`, `source = "file"`+"\nfile = 'VERSION'\nfile_pattern = '^(.+)$'\nfile_form = 'without-v'", 1)
			write(t, d, ".stw/release.toml", []byte(raw))
			write(t, d, "VERSION", []byte("1.2.3"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, tc.config)})
			if tc.setup != nil {
				tc.setup(t, d)
			}
			commit := commitAll(t, d)
			git(t, d, "remote", "add", "origin", "https://github.com/example/test.git")
			git(t, d, "update-ref", "refs/remotes/origin/main", commit)
			r := Plan(context.Background(), PlanOptions{Path: d, Subject: commit, Version: tc.version})
			if r.Status != "ok" {
				t.Fatalf("%+v", r.Error)
			}
			found := false
			for _, g := range r.Gates {
				if g.Code == tc.want {
					found = true
					if g.Outcome != tc.outcome {
						t.Fatalf("%+v", r.Gates)
					}
				}
			}
			if !found {
				t.Fatalf("missing %s", tc.want)
			}
			if tc.config == "skill-pack" && strings.Contains(r.Text(), " go ") {
				t.Fatal("Go inserted into language-neutral plan")
			}
			write(t, d, ".stw/release.toml", []byte("broken"))
			again := Plan(context.Background(), PlanOptions{Path: d, Subject: commit, Version: tc.version})
			if again.Verdict != r.Verdict {
				t.Fatal("read working tree")
			}
		})
	}
}

func TestVerdictPrecedence(t *testing.T) {
	for _, tc := range []struct {
		gates []Gate
		want  string
	}{{nil, "pass"}, {[]Gate{{Outcome: "not-applicable"}}, "pass"}, {[]Gate{{Outcome: "not-run"}}, "incomplete"}, {[]Gate{{Outcome: "not-run"}, {Outcome: "fail"}}, "fail"}, {[]Gate{{Outcome: "fail"}, {Outcome: "not-run"}}, "fail"}} {
		if got := Verdict(tc.gates); got != tc.want {
			t.Fatalf("%s != %s", got, tc.want)
		}
	}
}

func TestPlanHasNoGitEffects(t *testing.T) {
	d, commit := planRepo(t)
	makeIndexStale(t, d)
	markers := t.TempDir()
	plantSentinels(t, d, markers)
	log := logGit(t)
	count, proxy := listener(t)
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, proxy)
	}
	before := snapshot(t, d)
	r := Plan(context.Background(), PlanOptions{Path: d, Subject: commit, Version: "v1.2.3"})
	if r.Status != "ok" {
		t.Fatalf("%+v", r.Error)
	}
	if diff := differences(before, snapshot(t, d)); len(diff) != 0 {
		t.Fatal(diff)
	}
	if fired, _ := filepath.Glob(filepath.Join(markers, "fired-*")); len(fired) > 0 {
		t.Fatal(fired)
	}
	if count.Load() != 0 {
		t.Fatal("network attempted")
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "ARGS ") {
			continue
		}
		sub := gitSubcommand(strings.TrimPrefix(line, "ARGS "))
		if !detectionGit[sub] && !readGit[sub] && sub != "merge-base" && sub != "config" && sub != "remote" {
			t.Fatalf("unexpected process %s", line)
		}
	}
}

func TestJJReleaseCommitReadEffects(t *testing.T) {
	for lane, binary := range jjLanes(t) {
		t.Run(lane, func(t *testing.T) {
			useJJ(t, binary)
			for _, layout := range jjLayouts() {
				t.Run(layout.name, func(t *testing.T) {
					home := t.TempDir()
					d, ok := layout.setup(t, home)
					if !ok {
						t.Skip("native layout unavailable")
					}
					write(t, d, ".stw/release.toml", exampleBytes(t, "go-cli"))
					if out, err := jjRun(t, d, home, "commit", "-m", "release fixture"); err != nil {
						t.Fatalf("%v %s", err, out)
					}
					commit, err := jjRun(t, d, home, "--ignore-working-copy", "log", "--no-graph", "-r", "@-", "-T", "commit_id")
					if err != nil {
						t.Fatalf("%v %s", err, commit)
					}
					if out, err := jjRun(t, d, home, "git", "remote", "add", "origin", "https://github.com/example/test.git"); err != nil {
						t.Fatalf("%v %s", err, out)
					}
					write(t, d, ".stw/release.toml", []byte("uncommitted broken contract"))
					write(t, d, "unsnapshotted", []byte("dirty"))
					before, state := snapshot(t, d), jjState(t, d, home)
					checked := Check(context.Background(), CheckOptions{Path: d, Commit: strings.TrimSpace(commit)})
					if checked.Status != "ok" {
						t.Fatalf("check %+v %+v", checked, checked.Error)
					}
					planned := Plan(context.Background(), PlanOptions{Path: d, Subject: strings.TrimSpace(commit), Version: "v1.2.3"})
					if planned.Status != "ok" || planned.Verdict != "incomplete" || len(planned.WouldBuild) != 1 {
						t.Fatalf("plan %+v %+v", planned, planned.Error)
					}
					if planned.Gates[0].Reason == nil || *planned.Gates[0].Reason != "main-ref-absent" {
						t.Fatal(planned.Gates)
					}
					if diff := differences(before, snapshot(t, d)); len(diff) != 0 {
						t.Fatal(diff)
					}
					if after := jjState(t, d, home); after != state {
						t.Fatalf("jj operation or snapshot changed: %s", after)
					}
					unsupported := Plan(context.Background(), PlanOptions{Path: d, Subject: "v1.2.3"})
					if unsupported.Status != "refused" || unsupported.Error.Code != "unsupported-backend" {
						t.Fatalf("%+v", unsupported)
					}
				})
			}
		})
	}
}

func TestJJReadinessUsesConstantRetention(t *testing.T) {
	for lane, binary := range jjLanes(t) {
		t.Run(lane, func(t *testing.T) {
			useJJ(t, binary)
			for _, layout := range jjLayouts() {
				t.Run(layout.name, func(t *testing.T) {
					home := t.TempDir()
					d, ok := layout.setup(t, home)
					if !ok {
						t.Skip("native layout unavailable")
					}
					write(t, d, ".stw/release.toml", exampleBytes(t, "skill-pack"))
					for _, p := range []string{"readiness/skills-validation.json", "readiness/compatibility.json", "schemas/skill.schema.json"} {
						write(t, d, p, []byte("evidence"))
					}
					write(t, d, "readiness/skills-validation.json", bytes.Repeat([]byte{0, 255}, 70000))
					if out, err := jjRun(t, d, home, "git", "remote", "add", "origin", "https://github.com/example/skills.git"); err != nil {
						t.Fatalf("%v %s", err, out)
					}
					for _, scenario := range []struct {
						name, outcome string
						change        func()
					}{
						{"large binary evidence", "pass", func() {}},
						{"empty evidence", "fail", func() { write(t, d, "readiness/skills-validation.json", nil) }},
						{"symlink evidence", "fail", func() {
							if err := os.Remove(filepath.Join(d, "readiness/skills-validation.json")); err != nil {
								t.Fatal(err)
							}
							if err := os.Symlink("compatibility.json", filepath.Join(d, "readiness/skills-validation.json")); err != nil {
								t.Fatal(err)
							}
						}},
					} {
						t.Run(scenario.name, func(t *testing.T) {
							scenario.change()
							if out, err := jjRun(t, d, home, "commit", "-m", scenario.name); err != nil {
								t.Fatalf("%v %s", err, out)
							}
							commit, err := jjRun(t, d, home, "--ignore-working-copy", "log", "--no-graph", "-r", "@-", "-T", "commit_id")
							if err != nil {
								t.Fatal(err)
							}
							before, state := snapshot(t, d), jjState(t, d, home)
							r := Plan(context.Background(), PlanOptions{Path: d, Subject: strings.TrimSpace(commit), Version: "v1.2.3"})
							if r.Status != "ok" || len(r.Gates) != 8 || r.Gates[4].Outcome != scenario.outcome {
								t.Fatalf("%+v %+v", r, r.Error)
							}
							if diff := differences(before, snapshot(t, d)); len(diff) != 0 {
								t.Fatal(diff)
							}
							if after := jjState(t, d, home); after != state {
								t.Fatalf("working copy or operation log changed %s", after)
							}
						})
					}
				})
			}
		})
	}
}

func TestJJReleaseRejectsStaleReadObservation(t *testing.T) {
	for lane, binary := range jjLanes(t) {
		t.Run(lane, func(t *testing.T) {
			useJJ(t, binary)
			home := t.TempDir()
			d, ok := jjLayouts()[0].setup(t, home)
			if !ok {
				t.Skip("native layout unavailable")
			}
			write(t, d, "evidence", []byte("present"))
			if out, err := jjRun(t, d, home, "commit", "-m", "fixture"); err != nil {
				t.Fatalf("%v %s", err, out)
			}
			commit, err := jjRun(t, d, home, "--ignore-working-copy", "log", "--no-graph", "-r", "@-", "-T", "commit_id")
			if err != nil {
				t.Fatal(err)
			}
			visited := false
			err = inspection.WithJJReleaseTree(context.Background(), d, strings.TrimSpace(commit), func(reader *inspection.JJReleaseReader) error {
				nonempty, err := reader.Nonempty("evidence")
				if err != nil || !nonempty {
					t.Fatalf("readiness %t %v", nonempty, err)
				}
				visited = true
				if out, err := jjRun(t, d, home, "new", "-m", "concurrent operation"); err != nil {
					t.Fatalf("%v %s", err, out)
				}
				return nil
			})
			if !visited || err == nil {
				t.Fatalf("stale native read passed: visited=%t err=%v", visited, err)
			}
		})
	}
}

func TestJJCheckCommitContractLimitIsFinding(t *testing.T) {
	for lane, binary := range jjLanes(t) {
		t.Run(lane, func(t *testing.T) {
			useJJ(t, binary)
			home := t.TempDir()
			dir, ok := jjLayouts()[1].setup(t, home)
			if !ok {
				t.Skip("native standalone layout unavailable")
			}
			write(t, dir, ".stw/release.toml", append(bytes.Repeat([]byte("\n"), MaxContractBytes), exampleBytes(t, "go-cli")...))
			if out, err := jjRun(t, dir, home, "commit", "-m", "oversized contract"); err != nil {
				t.Fatalf("%v %s", err, out)
			}
			commit, err := jjRun(t, dir, home, "--ignore-working-copy", "log", "--no-graph", "-r", "@-", "-T", "commit_id")
			if err != nil {
				t.Fatal(err)
			}
			if out, err := jjRun(t, dir, home, "git", "remote", "add", "origin", "https://github.com/example/test.git"); err != nil {
				t.Fatalf("%v %s", err, out)
			}
			before, state := snapshot(t, dir), jjState(t, dir, home)
			checked := Check(context.Background(), CheckOptions{Path: dir, Commit: strings.TrimSpace(commit)})
			if checked.Status != "findings" || checked.Error != nil || len(checked.Findings) != 1 || checked.Findings[0].Code != "limit-exceeded" {
				t.Errorf("contract stdout bound must be a finding: status=%s findings=%+v error=%+v", checked.Status, checked.Findings, checked.Error)
			}
			planned := Plan(context.Background(), PlanOptions{Path: dir, Subject: strings.TrimSpace(commit), Version: "v1.2.3"})
			if planned.Status != "refused" || planned.Error == nil || planned.Error.Code != "release-limit" || planned.Boundary.Complete {
				t.Errorf("plan must retain limit refusal: %+v", planned)
			}
			if diff := differences(before, snapshot(t, dir)); len(diff) != 0 {
				t.Fatal(diff)
			}
			if after := jjState(t, dir, home); after != state {
				t.Fatal("limit handling changed native state")
			}
		})
	}
}

func TestGitReleaseAncestry(t *testing.T) {
	dir, commit := planRepo(t)
	source, err := newGitSource(context.Background(), dir, commit)
	if err != nil {
		t.Fatal(err)
	}
	main, gate, err := source.mainAncestry("upstream")
	if err != nil || gate.Outcome != "pass" || main.Commit == nil || *main.Commit != commit {
		t.Fatalf("%+v %+v %v", main, gate, err)
	}
	_, gate, err = source.mainAncestry("origin")
	if err != nil || gate.Reason == nil || *gate.Reason != "main-ref-absent" {
		t.Fatalf("%+v %v", gate, err)
	}
	write(t, dir, ".git/info/grafts", []byte(commit+"\n"))
	main, gate, err = source.mainAncestry("upstream")
	if err != nil || gate.Reason == nil || *gate.Reason != "grafts-present" {
		t.Fatalf("%+v %v", gate, err)
	}
	if main.Commit == nil || *main.Commit != commit {
		t.Fatalf("grafts hid recorded main: %+v", main)
	}
	if err := os.Remove(filepath.Join(dir, ".git/info/grafts")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "side.txt", []byte("unmerged"))
	side := commitAll(t, dir)
	sideSource, err := newGitSource(context.Background(), dir, side)
	if err != nil {
		t.Fatal(err)
	}
	_, gate, err = sideSource.mainAncestry("upstream")
	if err != nil || gate.Outcome != "fail" {
		t.Fatalf("unmerged commit passed %+v %v", gate, err)
	}
	write(t, dir, ".git/shallow", []byte(commit+"\n"))
	main, gate, err = source.mainAncestry("upstream")
	if err != nil || gate.Reason == nil || *gate.Reason != "shallow-clone" {
		t.Fatalf("%+v %v", gate, err)
	}
	if main.Commit == nil || *main.Commit != commit {
		t.Fatalf("shallow state hid recorded main: %+v", main)
	}
}

func TestPlanContractLimitRefuses(t *testing.T) {
	d := repoWith(t, map[string][]byte{".stw/release.toml": append(bytes.Repeat([]byte("\n"), MaxContractBytes), exampleBytes(t, "go-cli")...)})
	commit := commitAll(t, d)
	git(t, d, "remote", "add", "origin", "https://github.com/example/test.git")
	git(t, d, "update-ref", "refs/remotes/origin/main", commit)
	r := Plan(context.Background(), PlanOptions{Path: d, Subject: commit, Version: "v1.2.3"})
	if r.Status != "refused" || r.Error == nil || r.Error.Code != "release-limit" || r.Boundary.Complete {
		t.Fatalf("%+v", r)
	}
}

type boundedPlanSource struct{ *gitSource }

func (s boundedPlanSource) pathEntry(string) (sourceEntry, int64, error) {
	return sourceEntry{}, 0, inspection.ErrJJReleaseLimit
}

func TestPlanNativeReadBoundIsARefusal(t *testing.T) {
	d, commit := planRepo(t)
	source, err := newGitSource(context.Background(), d, commit)
	if err != nil {
		t.Fatal(err)
	}
	r := evaluateCommitPlan(context.Background(), PlanOptions{Subject: commit, Version: "v1.2.3"}, d, boundedPlanSource{source}, PlanResult{Boundary: CheckBoundary{Complete: true}})
	if r.Status != "refused" || r.Error == nil || r.Error.Code != "release-limit" || r.Boundary.Complete {
		t.Fatalf("%+v", r)
	}
}

func TestOfflineCommitPlan(t *testing.T) {
	dir, commit := planRepo(t)
	result := Plan(context.Background(), PlanOptions{Path: dir, Subject: commit, Version: "v1.2.3"})
	if result.Status != "ok" || result.Verdict != "fail" {
		t.Fatalf("%+v", result)
	} // skill pack readiness is absent
	if len(result.Gates) != 8 || result.Gates[4].Outcome != "fail" || result.Gates[5].Outcome != "not-run" {
		t.Fatalf("%+v", result.Gates)
	}
	if result.Subject.MainRef.Remote != "upstream" || len(result.WouldBuild) == 0 {
		t.Fatalf("%+v", result)
	}
}

func TestPlanRefusesUnmatchedRepositoryRemote(t *testing.T) {
	dir, commit := planRepo(t)
	result := Plan(context.Background(), PlanOptions{Path: dir, Subject: commit, Version: "v1.2.3", Repo: "other/project"})
	if result.Status != "refused" || result.Error == nil || result.Error.Code != "remote-unresolved" || result.Boundary.Complete {
		t.Fatalf("%+v", result)
	}
}
