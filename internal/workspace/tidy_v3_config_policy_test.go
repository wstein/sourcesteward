// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Spec 031 configuration policy matrix (contracts/configuration-policy.md).
// Every fixture is a temporary repository with its own HOME and XDG directory;
// helpers, hooks and drivers append to a marker file inside the test's own
// scratch directory and the marker must stay absent after planning. Sandboxed
// planning cannot write the marker at all, so each row additionally runs a
// hook-, helper- or attribute-triggering probe through the same session and
// arguments the planner uses, with a control run proving the fixture bites.

type policyFix struct {
	t                           *testing.T
	dir, bin, scratch, marker   string
	helper, home, xdg, hooksDir string
}

func newPolicyFix(t *testing.T) *policyFix {
	t.Helper()
	dir, bin := fixtureTidyV3Git(t)
	f := &policyFix{t: t, dir: dir, bin: bin, scratch: gitTempDir(t), home: gitTempDir(t), xdg: gitTempDir(t)}
	t.Setenv("HOME", f.home)
	t.Setenv("XDG_CONFIG_HOME", f.xdg)
	f.marker = filepath.Join(f.scratch, "marker")
	f.helper = filepath.Join(f.scratch, "helper")
	f.write(f.helper, "#!/bin/sh\necho \"$0 $*\" >> '"+f.marker+"'\nexit 1\n", 0700)
	f.hooksDir = filepath.Join(f.scratch, "hooks")
	if err := os.MkdirAll(f.hooksDir, 0700); err != nil {
		t.Fatal(err)
	}
	f.write(filepath.Join(f.hooksDir, "reference-transaction"), "#!/bin/sh\necho \"hook $*\" >> '"+f.marker+"'\nexit 0\n", 0700)
	gitFixture(t, dir, "branch", "integrated", "HEAD")
	gitFixture(t, dir, "commit", "--allow-empty", "-qm", "advance source")
	return f
}
func (f *policyFix) write(path, data string, mode os.FileMode) {
	f.t.Helper()
	if !strings.HasPrefix(path, f.scratch) && !strings.HasPrefix(path, f.dir) && !strings.HasPrefix(path, f.home) && !strings.HasPrefix(path, f.xdg) {
		f.t.Fatalf("fixture write outside the test's own directories: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		f.t.Fatal(err)
	}
}
func (f *policyFix) cfg(args ...string) {
	f.t.Helper()
	gitFixture(f.t, f.dir, append([]string{"config"}, args...)...)
}
func (f *policyFix) markerPresent() bool {
	_, err := os.Stat(f.marker)
	return err == nil
}

// writable lets a helper reach the marker and Git its temporary files (a
// signature check writes one); the production sandbox grants neither.
func (f *policyFix) writable() []string {
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		f.t.Fatal(err)
	}
	return []string{tmp, filepath.Join(f.dir, ".git")}
}

// dropProbeRefs removes the references a probe created, so that the plan and
// its apply re-evaluation see an unchanged repository.
func (f *policyFix) dropProbeRefs() {
	for _, line := range strings.Split(gitFixture(f.t, f.dir, "for-each-ref", "--format=%(refname)", "refs/probe/"), "\n") {
		if line != "" {
			gitFixture(f.t, f.dir, "-c", "core.hooksPath="+os.DevNull, "update-ref", "-d", line)
		}
	}
	f.clearMarker()
}
func (f *policyFix) clearMarker() { os.Remove(f.marker) }
func (f *policyFix) base() string { return gitFixture(f.t, f.dir, "symbolic-ref", "HEAD") }

func (f *policyFix) plan() V3Result {
	return defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: f.dir, Base: f.base(), StateDir: privateDir(f.t), Refs: []string{"refs/heads/integrated"}, ProtectWorktrees: []string{}})
}

// planNoted requires a successful plan with the exact noted classes.
func (f *policyFix) planNoted(want ...V3ConfigNote) (V3Result, error) {
	r := f.plan()
	if r.Status != "ok" || r.Plan == nil || len(r.Plan.Candidates) != 1 || r.Plan.Candidates[0].Decision != "delete" {
		return r, fmt.Errorf("plan not ok: status=%s diagnostics=%+v", r.Status, r.Diagnostics)
	}
	got := map[string]V3ConfigNote{}
	for _, n := range r.Plan.ConfigNotes {
		got[n.KeyClass] = n
		if v3ConfigClassPolicy[n.KeyClass] == [2]string{} || !v3Hash(n.Digest) {
			return r, fmt.Errorf("note outside the closed set: %+v", n)
		}
	}
	for _, w := range want {
		n, ok := got[w.KeyClass]
		if !ok || n.Disposition != w.Disposition || n.ApplyEffect != w.ApplyEffect {
			return r, fmt.Errorf("missing or wrong note %+v in %+v", w, r.Plan.ConfigNotes)
		}
	}
	if f.markerPresent() {
		return r, fmt.Errorf("helper marker present after planning")
	}
	return r, nil
}

// planRefused requires no plan, the named code and the lower-cased rule class.
func (f *policyFix) planRefused(code, rule string) error {
	r := f.plan()
	if r.Status == "ok" || r.Plan != nil || len(r.Diagnostics) == 0 {
		return fmt.Errorf("unsafe configuration planned: %+v", r)
	}
	if r.Diagnostics[0].Code != code || !strings.Contains(r.Diagnostics[0].Message, rule) {
		return fmt.Errorf("wrong refusal %+v, want %s naming %s", r.Diagnostics, code, rule)
	}
	if f.markerPresent() {
		return fmt.Errorf("helper marker present after refusal")
	}
	return nil
}

// applyRefuses requires apply to refuse every effect, naming the class, and to
// leave the branch and the recovery namespace untouched.
func (f *policyFix) applyRefuses(r V3Result, code, rule string) error {
	a := defaultTidyV3RefsApplyCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: r.Plan.StateDir, PlanID: r.Plan.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if a.Status == "ok" || len(a.Diagnostics) == 0 || a.Diagnostics[0].Code != code || !strings.Contains(a.Diagnostics[0].Message, rule) {
		return fmt.Errorf("apply did not refuse with %s naming %s: %+v", code, rule, a)
	}
	if out := gitFixture(f.t, f.dir, "for-each-ref", "refs/heads/integrated", "refs/stw/recovery"); strings.Count(out, "\n")+1 != 1 || !strings.Contains(out, "refs/heads/integrated") {
		return fmt.Errorf("apply changed references: %q", out)
	}
	if f.markerPresent() {
		return fmt.Errorf("helper marker present after apply refusal")
	}
	return nil
}

// probe runs one Git command through a planning session, exactly as planning
// would, with only the scratch directory and the common git directory writable.
func (f *policyFix) probe(args ...string) (string, int, error) {
	ctx, _, closeSession, err := withPlanningSession(context.Background())
	if err != nil {
		return "", -1, err
	}
	defer closeSession()
	return f.probeIn(ctx, args...)
}
func (f *policyFix) probeIn(ctx context.Context, args ...string) (string, int, error) {
	return runSandboxed(ctx, f.bin, args, f.dir, f.writable(), nil)
}

// control proves that the fixture's helper runs when no planning session
// neutralizes it, then resets the marker.
func (f *policyFix) control(args ...string) error {
	f.clearMarker()
	if _, _, err := runSandboxed(context.Background(), f.bin, append([]string{"--no-replace-objects"}, args...), f.dir, f.writable(), nil); err != nil {
		return err
	}
	fired := f.markerPresent()
	f.clearMarker()
	if !fired {
		return fmt.Errorf("control %v did not run the helper: the fixture proves nothing", args)
	}
	return nil
}

// neutralized runs the probe under the session and requires an absent marker.
func (f *policyFix) neutralized(args ...string) error {
	f.clearMarker()
	if _, _, err := f.probe(args...); err != nil {
		return err
	}
	if f.markerPresent() {
		return fmt.Errorf("helper ran for %v under the planning policy", args)
	}
	return nil
}

// audit lists the effective configuration seen under the session: apart from
// the repository's own file, only the constant command-line arguments may
// appear (no global, system, XDG or inherited GIT_CONFIG_* origin).
func (f *policyFix) audit() error {
	out, code, err := f.probe("config", "--list", "--show-origin", "-z")
	if err != nil || code != 0 {
		return fmt.Errorf("audit read failed: %v %d", err, code)
	}
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	if len(fields)%2 != 0 {
		return fmt.Errorf("odd audit record")
	}
	cmdline := []string{}
	local := "file:" + filepath.Join(f.dir, ".git", "config")
	for i := 0; i < len(fields); i += 2 {
		origin, entry := fields[i], fields[i+1]
		key, value, _ := strings.Cut(entry, "\n")
		switch {
		case origin == "command line:":
			cmdline = append(cmdline, strings.ToLower(key)+"="+value)
		case origin == local || origin == "file:.git/config" || origin == "file:config":
		default:
			return fmt.Errorf("configuration from outside the repository applies: %s %s", origin, key)
		}
	}
	want := []string{}
	for _, c := range contractPlanningConstants {
		want = append(want, strings.ToLower(c.key)+"="+c.value)
	}
	if !reflect.DeepEqual(cmdline, want) {
		return fmt.Errorf("command-line configuration differs from planningConfigArgs:\n got %v\nwant %v", cmdline, want)
	}
	return nil
}

func neutral(class, apply string) V3ConfigNote {
	return V3ConfigNote{KeyClass: class, Disposition: configNeutralized, ApplyEffect: apply}
}

// contractPlanningConstants is the contract's constant list (configuration-policy.md,
// planning environment and output table), written out independently of the
// production list so that dropping or changing an item fails a row. opposite is
// a different valid value a repository could set.
var contractPlanningConstants = []struct{ key, value, opposite string }{
	{"core.hooksPath", os.DevNull, "/nonexistent-hooks"}, {"core.fsmonitor", "false", "true"}, {"submodule.recurse", "false", "true"},
	{"core.untrackedCache", "false", "true"}, {"merge.renormalize", "false", "true"}, {"merge.renames", "true", "false"},
	{"merge.directoryRenames", "conflict", "true"}, {"merge.renameLimit", "1000", "5"}, {"diff.renames", "true", "false"},
	{"diff.renameLimit", "1000", "5"}, {"core.autocrlf", "false", "true"}, {"gc.auto", "0", "1"}, {"maintenance.auto", "false", "true"},
	{"protocol.ext.allow", "never", "always"}, {"log.showSignature", "false", "true"}, {"color.ui", "never", "always"},
	{"diff.noprefix", "false", "true"}, {"diff.mnemonicPrefix", "false", "true"}, {"diff.algorithm", "myers", "patience"},
	{"diff.indentHeuristic", "true", "false"}, {"diff.context", "3", "9"}, {"diff.interHunkContext", "0", "4"},
	{"diff.colorMoved", "false", "zebra"}, {"diff.wsErrorHighlight", "none", "all"}, {"core.quotePath", "true", "false"},
	{"core.abbrev", "auto", "12"}, {"i18n.logOutputEncoding", "UTF-8", "ISO-8859-1"}, {"log.decorate", "no", "full"},
	{"log.mailmap", "false", "true"}, {"log.follow", "false", "true"}, {"diff.orderFile", os.DevNull, "/nonexistent-order"},
	{"diff.relative", "false", "true"}, {"diff.ignoreSubmodules", "none", "all"},
}

// rowConstants: every contract constant wins over the opposite repository value.
func rowConstants(t *testing.T) error {
	f := newPolicyFix(t)
	for _, c := range contractPlanningConstants {
		f.cfg(c.key, c.opposite)
	}
	for _, c := range contractPlanningConstants {
		out, _, err := f.probe("config", "--get", c.key)
		if err != nil || strings.TrimSpace(out) != c.value {
			return fmt.Errorf("%s is %q under the planning policy, want %s", c.key, out, c.value)
		}
	}
	return f.audit()
}

type configRow struct {
	id  string
	run func(*testing.T) error
}

// refsOf the matrix rows; each returns nil only if every assertion holds.
func configMatrixRows() []configRow {
	refused := func(id, rule string, set func(*policyFix)) configRow {
		return configRow{id, func(t *testing.T) error {
			f := newPolicyFix(t)
			set(f)
			return f.planRefused("git-unsafe-configuration", rule)
		}}
	}
	return []configRow{
		{"C1", rowC1}, {"C2", rowC2}, {"C3", rowC3}, {"C4", rowC4}, {"C5", rowC5},
		{"C6", rowC6}, {"C7", rowC7}, {"C8", rowC8},
		refused("C9", "includeIf.*", func(f *policyFix) { f.cfg("includeIf.gitdir:/nowhere/.path", filepath.Join(f.scratch, "absent")) }),
		refused("C10", "extensions.partialclone", func(f *policyFix) { f.cfg("extensions.partialclone", "origin") }),
		{"C11", rowC11},
		refused("C12", "hook.*", func(f *policyFix) { f.cfg("hook.fixture.command", f.helper) }),
		{"C13", rowC13}, {"C14", rowC14},
		refused("C15", "core.attributesfile", func(f *policyFix) { f.cfg("core.attributesFile", filepath.Join(f.scratch, "attributes")) }),
		refused("C16", "extensions.worktreeconfig", func(f *policyFix) { f.cfg("extensions.worktreeConfig", "true") }),
		refused("C17", "core.sparsecheckout", func(f *policyFix) { f.cfg("core.sparseCheckout", "true") }),
		{"C18", func(t *testing.T) error {
			f := newPolicyFix(t)
			f.cfg("core.alternateRefsCommand", f.helper)
			return f.planRefused("git-unsafe-configuration", "core.alternaterefscommand")
		}},
		{"C19", rowC19}, {"C20", rowC20}, {"C21", rowC21}, {"C22", rowC22},
		{"C27", rowC27}, {"C30", rowC30}, {"C31", rowC31}, {"C32", rowC32}, {"C34", rowC34},
		{"renormalize", rowRenormalize}, {"signature", rowSignature}, {"constants", rowConstants}, {"gitlinks", rowGitlinks}, {"filter-attribute-source", rowFilterAttributeSource},
	}
}

func TestConfigurationPolicyMatrix(t *testing.T) {
	for _, row := range configMatrixRows() {
		t.Run(row.id, func(t *testing.T) {
			if err := row.run(t); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func rowC1(t *testing.T) error {
	f := newPolicyFix(t)
	f.cfg("core.hooksPath", f.hooksDir)
	if err := f.control("update-ref", "refs/probe/control", "HEAD"); err != nil {
		return err
	}
	if err := f.neutralized("update-ref", "refs/probe/planning", "HEAD"); err != nil {
		return err
	}
	f.dropProbeRefs()
	r, err := f.planNoted(neutral("core.hooksPath", applyRefuses))
	if err != nil {
		return err
	}
	if err = f.audit(); err != nil {
		return err
	}
	return f.applyRefuses(r, "git-unsafe-configuration", "core.hooksPath")
}
func rowC2(t *testing.T) error {
	f := newPolicyFix(t)
	f.write(filepath.Join(f.home, ".gitconfig"), "[core]\n\thooksPath = "+f.hooksDir+"\n", 0600)
	if err := f.control("update-ref", "refs/probe/control", "HEAD"); err != nil {
		return err
	}
	if err := f.neutralized("update-ref", "refs/probe/planning", "HEAD"); err != nil {
		return err
	}
	f.dropProbeRefs()
	r, err := f.planNoted(neutral("core.hooksPath", applyRefuses))
	if err != nil {
		return err
	}
	if err = f.audit(); err != nil {
		return err
	}
	return f.applyRefuses(r, "git-unsafe-configuration", "core.hooksPath")
}
func rowC3(t *testing.T) error {
	f := newPolicyFix(t)
	oid := gitFixture(t, f.dir, "rev-parse", "HEAD")
	gitFixture(t, f.dir, "update-index", "--add", "--cacheinfo", "160000,"+oid+",member")
	gitFixture(t, f.dir, "commit", "-qm", "gitlink")
	f.cfg("submodule.member.url", "https://example.invalid/member.git")
	f.cfg("submodule.member.update", "!"+f.helper)
	r, err := f.planNoted(neutral("submodule.*", applyRefuses), neutral("gitlinks", applyRefuses))
	if err != nil {
		return err
	}
	return f.applyRefuses(r, "git-unsafe-configuration", "submodule.*")
}

// rowGitlinks: gitlinks are data for planning; the strict apply admission
// still refuses them (git-tree-refused), and the note says so.
func rowGitlinks(t *testing.T) error {
	f := newPolicyFix(t)
	oid := gitFixture(t, f.dir, "rev-parse", "HEAD")
	gitFixture(t, f.dir, "update-index", "--add", "--cacheinfo", "160000,"+oid+",member")
	gitFixture(t, f.dir, "commit", "-qm", "gitlink")
	r, err := f.planNoted(neutral("gitlinks", applyRefuses))
	if err != nil {
		return err
	}
	return f.applyRefuses(r, "git-tree-refused", "gitlinks")
}

// rowFilterAttributeSource: an uncommitted worktree attribute naming a
// configured filter never applies once the tip tree is pinned.
func rowFilterAttributeSource(t *testing.T) error {
	f := newPolicyFix(t)
	f.write(filepath.Join(f.dir, ".gitattributes"), "* filter=x\n", 0600)
	f.cfg("filter.x.clean", f.helper)
	_, err := f.planNoted(V3ConfigNote{KeyClass: "merge-driver", Disposition: configReviewReason, ApplyEffect: applyNone})
	return err
}
func rowC4(t *testing.T) error {
	f := newPolicyFix(t)
	f.cfg("submodule.recurse", "true")
	r, err := f.planNoted(neutral("submodule.*", applyRefuses))
	if err != nil {
		return err
	}
	if err = f.neutralized("status", "--porcelain"); err != nil {
		return err
	}
	return f.applyRefuses(r, "git-unsafe-configuration", "submodule.*")
}
func rowC5(t *testing.T) error {
	f := newPolicyFix(t)
	f.cfg("core.fsmonitor", f.helper)
	if err := f.control("status", "--porcelain"); err != nil {
		return err
	}
	r, err := f.planNoted(neutral("core.fsmonitor", applyRefuses))
	if err != nil {
		return err
	}
	if err = f.neutralized("status", "--porcelain"); err != nil {
		return err
	}
	return f.applyRefuses(r, "git-unsafe-configuration", "core.fsmonitor")
}
func rowC6(t *testing.T) error {
	f := newPolicyFix(t)
	f.cfg("filter.x.clean", f.helper)
	if _, err := f.planNoted(V3ConfigNote{KeyClass: "merge-driver", Disposition: configReviewReason, ApplyEffect: applyNone}); err != nil {
		return err
	}
	// A matching committed attribute keeps the existing stage2 refusal.
	f.write(filepath.Join(f.dir, ".gitattributes"), "* filter=x\n", 0600)
	gitFixture(t, f.dir, "add", ".gitattributes")
	gitFixture(t, f.dir, "commit", "-qm", "attribute")
	f.clearMarker() // the fixture's own add ran the clean filter
	return f.planRefused("git-active-filter-refused", "active filter attribute")
}
func rowC7(t *testing.T) error {
	f := newPolicyFix(t)
	f.write(filepath.Join(f.dir, ".gitattributes"), "*.txt merge=x\n", 0600)
	gitFixture(t, f.dir, "add", ".gitattributes")
	gitFixture(t, f.dir, "commit", "-qm", "merge attribute")
	f.cfg("merge.x.driver", f.helper+" %O %A %B")
	_, err := f.planNoted(V3ConfigNote{KeyClass: "merge-driver", Disposition: configReviewReason, ApplyEffect: applyNone})
	return err
}
func rowC8(t *testing.T) error {
	f := newPolicyFix(t)
	f.cfg("diff.external", f.helper)
	f.cfg("diff.x.textconv", f.helper)
	_, err := f.planNoted(neutral("diff.external", applyNone), neutral("diff.*.textconv", applyNone))
	return err
}
func rowC11(t *testing.T) error {
	f := newPolicyFix(t)
	oid := gitFixture(t, f.dir, "rev-parse", "HEAD")
	for _, name := range []string{"bundledock", "crewbook", "workharbor"} {
		gitFixture(t, f.dir, "update-index", "--add", "--cacheinfo", "160000,"+oid+","+name)
		f.cfg("submodule."+name+".url", "https://example.invalid/"+name+".git")
		f.cfg("submodule."+name+".active", "true")
	}
	gitFixture(t, f.dir, "commit", "-qm", "umbrella gitlinks")
	r, err := f.planNoted(neutral("submodule.*", applyRefuses), neutral("gitlinks", applyRefuses))
	if err != nil {
		return err
	}
	return f.applyRefuses(r, "git-unsafe-configuration", "submodule.*")
}
func rowC13(t *testing.T) error {
	f := newPolicyFix(t)
	include := filepath.Join(f.scratch, "include.cfg")
	f.write(include, "[core]\n\thooksPath = "+f.hooksDir+"\n", 0600)
	f.cfg("include.path", include)
	if err := f.control("update-ref", "refs/probe/control", "HEAD"); err != nil {
		return err
	}
	if err := f.neutralized("update-ref", "refs/probe/planning", "HEAD"); err != nil {
		return err
	}
	f.dropProbeRefs()
	r, err := f.planNoted(neutral("core.hooksPath", applyRefuses))
	if err != nil {
		return err
	}
	return f.applyRefuses(r, "git-unsafe-configuration", "core.hooksPath")
}
func rowC14(t *testing.T) error {
	f := newPolicyFix(t)
	for _, key := range []string{"core.pager", "core.editor", "credential.helper", "core.sshCommand", "gpg.program", "core.askPass"} {
		f.cfg(key, f.helper)
	}
	_, err := f.planNoted(neutral("core.pager", applyNone), neutral("core.editor", applyNone), neutral("credential.*", applyNone), neutral("core.sshCommand", applyNone), neutral("gpg.*", applyNone), neutral("core.askPass", applyNone))
	return err
}

// rowSignature is the signed-commit half of C14: a configured gpg.program must
// not run for any `git log` of the planner (log.showSignature constant).
func rowSignature(t *testing.T) error {
	f := newPolicyFix(t)
	raw := gitFixture(t, f.dir, "cat-file", "commit", "HEAD")
	head, body, _ := strings.Cut(raw, "\n\n")
	signed := head + "\ngpgsig -----BEGIN PGP SIGNATURE-----\n \n fixture\n -----END PGP SIGNATURE-----\n\n" + body + "\n"
	cmd := execFixtureHashObject(t, f.dir, signed)
	gitFixture(t, f.dir, "update-ref", "refs/heads/signed", cmd)
	f.cfg("log.showSignature", "true")
	f.cfg("gpg.program", f.helper)
	if err := f.control("log", "-1", "refs/heads/signed"); err != nil {
		return err
	}
	return f.neutralized("log", "-1", "refs/heads/signed")
}
func rowC19(t *testing.T) error {
	for _, name := range []string{"unspecified", "unset", "set"} {
		f := newPolicyFix(t)
		f.cfg("filter."+name+".clean", f.helper)
		if err := f.planRefused("git-unsafe-configuration", "filter.<reserved-sentinel>.*"); err != nil {
			return err
		}
	}
	return nil
}
func rowC20(t *testing.T) error {
	f := newPolicyFix(t)
	for _, name := range []string{"evil", "fixture", "reviewer"} {
		f.cfg("filter."+name+".clean", f.helper)
		f.cfg("filter."+name+".smudge", f.helper)
		f.cfg("filter."+name+".process", f.helper)
	}
	_, err := f.planNoted(V3ConfigNote{KeyClass: "merge-driver", Disposition: configReviewReason, ApplyEffect: applyNone})
	return err
}
func rowC21(t *testing.T) error {
	f := newPolicyFix(t)
	f.write(filepath.Join(f.dir, ".git", "hooks", "reference-transaction"), "#!/bin/sh\necho \"default $*\" >> '"+f.marker+"'\nexit 0\n", 0700)
	r, err := f.planNoted(neutral("default-hooks", applyRefuses))
	if err != nil {
		return err
	}
	return f.applyRefuses(r, "git-hooks-refused", "executable hooks")
}
func rowC22(t *testing.T) error {
	f := newPolicyFix(t)
	head := gitFixture(t, f.dir, "rev-parse", "HEAD")
	other := gitFixture(t, f.dir, "rev-parse", "HEAD~1")
	gitFixture(t, f.dir, "update-ref", "refs/replace/"+head, other)
	_, err := f.planNoted(neutral("replace-refs-ignored", applyNone))
	return err
}

// C27: planning and apply re-evaluation use the identical constant list, and
// that list is the one of the specification.
func rowC27(t *testing.T) error {
	f := newPolicyFix(t)
	seen := [][]string{}
	original := planningPolicyFactory
	planningPolicyFactory = func() planningPolicy {
		p := original()
		seen = append(seen, append([]string{}, p.arguments...))
		return p
	}
	defer func() { planningPolicyFactory = original }()
	r, err := f.planNoted()
	if err != nil {
		return err
	}
	planned := len(seen)
	a := defaultTidyV3RefsApplyCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: r.Plan.StateDir, PlanID: r.Plan.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if a.Status != "ok" || len(seen) <= planned {
		return fmt.Errorf("apply did not re-evaluate under a planning session: %+v", a.Diagnostics)
	}
	spec := "--no-optional-locks --no-replace-objects -c core.hooksPath=/dev/null -c core.fsmonitor=false -c submodule.recurse=false -c core.untrackedCache=false -c merge.renormalize=false -c merge.renames=true -c merge.directoryRenames=conflict -c merge.renameLimit=1000 -c diff.renames=true -c diff.renameLimit=1000 -c core.autocrlf=false -c gc.auto=0 -c maintenance.auto=false -c protocol.ext.allow=never -c log.showSignature=false -c color.ui=never"
	for i, args := range seen {
		if !reflect.DeepEqual(args, seen[0]) || !strings.HasPrefix(strings.Join(args, " "), spec) {
			return fmt.Errorf("session %d argument list differs: %v", i, args)
		}
	}
	return nil
}

// C30: the worktree .gitattributes is ignored once the tip tree is pinned.
func rowC30(t *testing.T) error {
	f := newPolicyFix(t)
	f.write(filepath.Join(f.dir, ".gitattributes"), "* merge=union\n", 0600)
	tree := gitFixture(t, f.dir, "rev-parse", "HEAD^{tree}")
	ctx, session, closeSession, err := withPlanningSession(context.Background())
	if err != nil {
		return err
	}
	defer closeSession()
	args := append(session.attrSource(tree), "check-attr", "merge", "--", "file")
	out, code, err := f.probeIn(ctx, args...)
	if err != nil || code != 0 || !strings.Contains(out, "file: merge: unspecified") {
		return fmt.Errorf("worktree attributes were not ignored: %q %d %v", out, code, err)
	}
	return nil
}

// C32: global and XDG attributes never apply under the private environment.
func rowC32(t *testing.T) error {
	f := newPolicyFix(t)
	f.write(filepath.Join(f.xdg, "git", "attributes"), "* merge=union\n", 0600)
	out, _, err := runSandboxed(context.Background(), f.bin, []string{"check-attr", "merge", "--", "file"}, f.dir, nil, nil)
	if err != nil || !strings.Contains(out, "file: merge: union") {
		return fmt.Errorf("control: XDG attributes do not apply to an unprotected command: %q %v", out, err)
	}
	out, code, err := f.probe("check-attr", "merge", "--", "file")
	if err != nil || code != 0 || !strings.Contains(out, "file: merge: unspecified") {
		return fmt.Errorf("XDG attributes applied: %q %d %v", out, code, err)
	}
	return nil
}

// C31: $GIT_DIR/info/attributes is the one source --attr-source cannot pin.
func rowC31(t *testing.T) error {
	f := newPolicyFix(t)
	path := filepath.Join(f.dir, ".git", "info", "attributes")
	noted := func() (bool, error) {
		r, err := f.planNoted()
		if err != nil {
			return false, err
		}
		return tidyV3HasConfigClass(r.Plan.ConfigNotes, "attribute-source"), nil
	}
	for _, c := range []struct {
		name, content string
		want          bool
	}{{"absent", "", false}, {"comment-only", "# comment\n\n", false}, {"line", "* merge=union\n", true}, {"macro-use", "[attr]m merge=union\n*.txt m\n", true}} {
		os.Remove(path)
		if c.name != "absent" {
			f.write(path, c.content, 0600)
		}
		got, err := noted()
		if err != nil {
			return err
		}
		if got != c.want {
			return fmt.Errorf("info/attributes %s: noted=%v want %v", c.name, got, c.want)
		}
	}
	os.Remove(path)
	if err := os.Symlink(filepath.Join(f.scratch, "elsewhere"), path); err != nil {
		return err
	}
	if got, err := noted(); err != nil || !got {
		return fmt.Errorf("symlinked info/attributes not noted: %v %v", got, err)
	}
	os.Remove(path)
	f.write(path, "# "+strings.Repeat("x", infoAttributesLimit)+"\n", 0600)
	if got, err := noted(); err != nil || !got {
		return fmt.Errorf("oversized info/attributes not noted: %v %v", got, err)
	}
	// A linked worktree shares the common git directory's file.
	os.Remove(path)
	f.write(path, "* merge=union\n", 0600)
	linked := filepath.Join(f.scratch, "linked")
	gitFixture(t, f.dir, "worktree", "add", "-q", "--detach", linked, "HEAD")
	id, err := gitNativeIdentity(context.Background(), linked)
	if err != nil || id.RepositoryDir != filepath.Join(f.dir, ".git") || !infoAttributesActive(id.RepositoryDir) {
		return fmt.Errorf("linked worktree common directory not scanned: %+v %v", id, err)
	}
	return nil
}

// C34: the exact environment the executor builds.
func rowC34(t *testing.T) error {
	f := newPolicyFix(t)
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.hooksPath'='/evil'")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", "/evil")
	t.Setenv("GIT_EXTERNAL_DIFF", "/evil")
	t.Setenv("GIT_ASKPASS", "/evil")
	ctx, session, closeSession, err := withPlanningSession(context.Background())
	if err != nil {
		return err
	}
	defer closeSession()
	_ = ctx
	dir := filepath.Dir(f.bin)
	want := []string{"PATH=" + dir, "GIT_EXEC_PATH=" + dir, "LANG=C", "LC_ALL=C", "TZ=UTC", "TERM=dumb", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat",
		"HOME=" + session.home, "XDG_CONFIG_HOME=" + session.xdg, "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_SYSTEM=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_ATTR_NOSYSTEM=1"}
	if got := session.environment(f.bin); !reflect.DeepEqual(got, want) {
		return fmt.Errorf("planning environment differs:\n got %v\nwant %v", got, want)
	}
	for _, dir := range []string{session.home, session.xdg} {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 || strings.HasPrefix(dir, f.home) || strings.HasPrefix(dir, f.xdg) {
			return fmt.Errorf("private directory %s is not empty, missing or the caller's own", dir)
		}
	}
	return nil
}

// rowRenormalize: a real merge-tree under the policy conflicts where the same
// merge with the repository's merge.renormalize=true is clean (M4).
func rowRenormalize(t *testing.T) error {
	f := newPolicyFix(t)
	f.write(filepath.Join(f.dir, ".gitattributes"), "* text=auto\n", 0600)
	f.write(filepath.Join(f.dir, "c.txt"), "a\nb\n", 0600)
	gitFixture(t, f.dir, "add", ".gitattributes", "c.txt")
	gitFixture(t, f.dir, "commit", "-qm", "renormalize base")
	base := gitFixture(t, f.dir, "rev-parse", "HEAD")
	attrs := gitFixture(t, f.dir, "rev-parse", "HEAD:.gitattributes")
	commit := func(content, name string) string {
		blob := execFixtureHashObjectRaw(t, f.dir, content)
		tree := execFixtureMktree(t, f.dir, "100644 blob "+attrs+"\t.gitattributes\n100644 blob "+blob+"\tc.txt\n")
		return gitFixture(t, f.dir, "commit-tree", tree, "-p", base, "-m", name)
	}
	ours, theirs := commit("a\r\nb\r\n", "ours"), commit("a\nb\nc\n", "theirs")
	f.cfg("merge.renormalize", "true")
	f.cfg("core.autocrlf", "true")
	args := []string{"merge-tree", "--write-tree", "--merge-base=" + base, ours, theirs}
	_, code, err := runSandboxed(context.Background(), f.bin, args, f.dir, f.writable(), nil)
	if err != nil || code != 0 {
		return fmt.Errorf("control: the repository's renormalize merge is not clean: %d %v", code, err)
	}
	_, code, err = f.probe(args...)
	if err != nil || code != 1 {
		return fmt.Errorf("merge under the planning policy exited %d (%v), want the conflict of renormalize=false", code, err)
	}
	return nil
}

func TestPlanningSessionRemovesItsPrivateDirectories(t *testing.T) {
	ctx, session, closeSession, err := withPlanningSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again, same, noop, err := withPlanningSession(ctx); err != nil || same != session || again != ctx {
		t.Fatal("a nested session must join the outer one")
	} else {
		noop()
	}
	if _, err := os.Stat(session.home); err != nil {
		t.Fatal(err)
	}
	closeSession()
	for _, dir := range []string{session.home, session.xdg} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("private directory %s survived", dir)
		}
	}
}

func execFixtureHashObject(t *testing.T, dir, commit string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "hash-object", "-t", "commit", "-w", "--stdin")
	cmd.Stdin = strings.NewReader(commit)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hash-object: %v %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func execFixtureHashObjectRaw(t *testing.T, dir, content string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "hash-object", "--no-filters", "-w", "--stdin")
	cmd.Stdin = strings.NewReader(content)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hash-object: %v %s", err, out)
	}
	return strings.TrimSpace(string(out))
}
func execFixtureMktree(t *testing.T, dir, listing string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "mktree")
	cmd.Stdin = strings.NewReader(listing)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mktree: %v %s", err, out)
	}
	return strings.TrimSpace(string(out))
}
