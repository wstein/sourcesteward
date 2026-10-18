// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// BuildRelease is the only production entrypoint for project execution. CI modes
// refuse before repository inspection outside Actions; local-verify is explicit
// verification and the caller remains responsible for authorizing that rebuild.
func BuildRelease(ctx context.Context, o BuildOptions) BuildResult {
	return buildRelease(ctx, o, projectRunner{}, os.Environ())
}
func buildRelease(ctx context.Context, o BuildOptions, runner buildRunner, inherited []string) BuildResult {
	r := newBuildResult(o.Mode)
	if o.Mode != "local-verify" && ((o.Mode != "ci" && o.Mode != "promote") || os.Getenv("GITHUB_ACTIONS") != "true") {
		r.Status = "refused"
		r.Error = &CheckError{Code: "ci-only", Message: "ci and promote builds require GITHUB_ACTIONS=true"}
		return r
	}
	if err := ctx.Err(); err != nil {
		return r.fail("canceled", err)
	}
	var document any
	if o.Mode == "promote" || o.Mode == "local-verify" {
		if o.Record == "" {
			return r.fail("record-invalid", errors.New("record is required"))
		}
		raw, err := readBuildFile(filepath.Dir(o.Record), filepath.Base(o.Record), MaxReleaseDocumentBytes)
		if err != nil {
			return r.fail("record-invalid", err)
		}
		var findings []Finding
		document, findings = ParseReleaseDocument(raw)
		if len(findings) > 0 {
			return r.fail(findings[0].Code, errors.New(findings[0].Message))
		}
	}
	if o.Mode == "promote" {
		record, ok := document.(*ReleaseRecord)
		if !ok {
			return r.fail("record-invalid", errors.New("promotion requires a release record"))
		}
		o.Commit, o.Version = record.Commit, record.Version
		if o.Tag != record.Tag {
			return r.fail("promote-failed", errors.New("tag differs from record"))
		}
	}
	if !IsFullCommitID(o.Commit) {
		return r.fail("commit-unresolved", errors.New("full commit id required"))
	}
	input := o.Path
	if input == "" {
		input = "."
	}
	root, backend, problem := detectOwner(ctx, input, false)
	if problem != nil {
		return r.fail(problem.code, errors.New(problem.message))
	}
	if backend != "git" {
		return r.fail("unsupported-backend", errors.New("build modes require a Git-owned checkout"))
	}
	source, err := newGitSource(ctx, root, o.Commit)
	if err != nil {
		return r.fail("commit-unresolved", err)
	}
	c, findings, code, err := planContract(source, o.Product)
	if err != nil {
		return r.fail("build-failed", err)
	}
	if code != "" {
		return r.fail(code, errors.New("contract selection refused"))
	}
	if len(findings) > 0 || c == nil {
		return r.fail("gate-failed", errors.New("contract has findings"))
	}
	scheme, err := CompileVersionScheme(c.Version)
	if err != nil || !scheme.Valid(o.Version) {
		return r.fail("gate-failed", errors.New("version is invalid"))
	}
	epochString, err := source.commitTime()
	if err != nil {
		return r.fail("build-failed", err)
	}
	epoch, err := strconv.ParseInt(epochString, 10, 64)
	if err != nil {
		return r.fail("build-failed", err)
	}
	s := buildState{root: root, commit: o.Commit, version: o.Version, epoch: epoch, contract: c, repository: os.Getenv("GITHUB_REPOSITORY"), stwVersion: "dev", run: RecordRun{Workflow: c.CI.ReleaseWorkflow}}
	s.run.RunID, _ = strconv.ParseInt(os.Getenv("GITHUB_RUN_ID"), 10, 64)
	s.run.RunAttempt, _ = strconv.ParseInt(os.Getenv("GITHUB_RUN_ATTEMPT"), 10, 64)
	entries, _, err := source.StwEntries()
	if err != nil {
		return r.fail("build-failed", err)
	}
	for _, entry := range entries {
		file := ".stw/" + entry.Name
		if file == ".stw/release.toml" || file == ".stw/release.json" || file == ".stw/release."+o.Product+".toml" || file == ".stw/release."+o.Product+".json" {
			raw, large, err := source.Read(entry, MaxContractBytes)
			if err != nil || large {
				return r.fail("release-limit", errors.New("cannot read bounded config"))
			}
			s.configSHA = buildHash(raw)
		}
	}
	s.hashTree = func(p string) (string, error) {
		entry, _, err := source.pathEntry(p)
		if err != nil {
			return "", err
		}
		if entry.Kind != "file" {
			return "", errors.New("tree input must be regular")
		}
		return hashBuildGitBlob(ctx, source.root, entry.id)
	}

	if o.Mode == "promote" {
		return executePromote(ctx, s, o, document.(*ReleaseRecord), runner, inherited)
	}
	if o.Mode == "local-verify" {
		return executeLocalVerify(ctx, s, o, document, source, runner, inherited)
	}
	// All local CI build gates must be present and passed. Network gates belong to the
	// preceding workflow verification step and never silently count as passed here.
	plan := Plan(ctx, PlanOptions{Path: root, Subject: o.Commit, Product: o.Product, Version: o.Version, Mode: "build"})
	if plan.Error != nil {
		return r.fail(plan.Error.Code, errors.New(plan.Error.Message))
	}
	needed := map[string]bool{"commit-not-on-main": false, "config-invalid": false, "version-invalid": false, "version-file-mismatch": false, "readiness-missing": false}
	for _, g := range plan.Gates {
		if _, ok := needed[g.Code]; ok {
			if g.Outcome != "pass" && g.Outcome != "not-applicable" {
				return r.fail("gate-failed", fmt.Errorf("offline gate %s is %s", g.Code, g.Outcome))
			}
			needed[g.Code] = true
		}
	}
	for gate, present := range needed {
		if !present {
			return r.fail("gate-failed", fmt.Errorf("missing offline gate %s", gate))
		}
	}
	if err := checkBuildCheckout(ctx, root, o.Commit); err != nil {
		return r.fail("checkout-mismatch", err)
	}
	r = executeBuild(ctx, s, o, runner, inherited)
	if err := ctx.Err(); err != nil {
		return r.fail("canceled", err)
	}
	if err := checkBuildCheckout(ctx, root, o.Commit); err != nil {
		return r.fail("checkout-mismatch", err)
	}
	return r
}
func checkBuildCheckout(ctx context.Context, root, commit string) error {
	if err := checkBuildCheckoutHelpers(ctx, root); err != nil {
		return err
	}
	head, truncated, status, err := gitOutput(ctx, root, 128, "", "rev-parse", "--verify", "HEAD")
	if err != nil || truncated || status != 0 || strings.TrimSpace(string(head)) != commit {
		return errors.New("HEAD differs from requested commit")
	}
	_, truncated, status, err = gitOutput(ctx, root, 65536, "", "diff-index", "--quiet", "--no-ext-diff", "--no-textconv", "HEAD", "--")
	if err != nil || truncated || status != 0 {
		return errors.New("tracked checkout differs from HEAD")
	}
	return nil
}
func executePromote(ctx context.Context, s buildState, o BuildOptions, record *ReleaseRecord, runner buildRunner, inherited []string) BuildResult {
	r := newBuildResult(o.Mode)
	sums := map[string]string{}
	for _, a := range record.Assets {
		if a.File != nil {
			sums[a.File.Name] = a.File.SHA256
		}
	}
	if findings := ValidateRecord(*record, RecordContext{Contract: s.contract, Repository: s.repository, Checksums: sums}); len(findings) > 0 {
		return r.fail("record-invalid", errors.New(findings[0].Message))
	}
	for _, a := range record.Assets {
		if a.Image == nil {
			continue
		}
		if s.contract.Promote == nil {
			return r.fail("promote-failed", errors.New("missing promote command"))
		}
		image := a.Image
		var target *Target
		for i := range s.contract.Target {
			if s.contract.Target[i].ID == image.Target {
				target = &s.contract.Target[i]
			}
		}
		if target == nil || target.Kind != "oci-image" || target.Repository != image.Repository || target.Build != image.Build {
			return r.fail("record-invalid", errors.New("image does not match trusted target"))
		}
		values := map[string]string{"repository": image.Repository, "digest": image.IndexDigest, "version": record.Version, "version_number": strings.TrimPrefix(record.Version, "v")}
		argv := make([]string, len(s.contract.Promote.Argv))
		for i, a := range s.contract.Promote.Argv {
			argv[i] = buildSubstitute(a, values)
		}
		env, err := buildEnvironment(Build{EnvInherit: []string{"PATH", "HOME", "TMPDIR"}}, values, inherited, s.epoch)
		if err != nil {
			return r.fail("promote-failed", err)
		}
		timeout := s.contract.Promote.TimeoutSeconds
		if timeout == 0 {
			timeout = defaultTimeout
		}
		result := runBuildCommand(ctx, runner, BuildCommand{Argv: argv, Env: env, Dir: s.root, Timeout: time.Duration(timeout) * time.Second})
		result.Stdout, result.Stderr = Escape(result.Stdout), Escape(result.Stderr)
		r.Builds = append(r.Builds, BuildExecution{ID: image.Build, Invocations: []BuildInvocationResult{result}})
		if result.Exit != 0 || result.err != nil {
			return r.fail("promote-failed", errors.New("promotion command refused or failed"))
		}
	}
	return r
}
func executeLocalVerify(ctx context.Context, s buildState, o BuildOptions, document any, source *gitSource, runner buildRunner, inherited []string) BuildResult {
	r := newBuildResult(o.Mode)
	for _, target := range s.contract.Target {
		if target.Kind == "oci-image" {
			return r.fail("unsupported-target", errors.New("local verification requires an all-file selected product"))
		}
	}
	expected, repository, err := validateLocalComparison(ctx, s, document)
	if err != nil {
		return r.fail("record-invalid", err)
	}
	s.repository = repository
	temp, err := os.MkdirTemp("", "stw-release-verify-")
	if err != nil {
		return r.fail("export-unsafe", err)
	}
	defer os.RemoveAll(temp)
	root := filepath.Join(temp, "source")
	if err := os.Mkdir(root, 0700); err != nil {
		return r.fail("export-unsafe", err)
	}
	if err := exportGitBuildTree(ctx, source, root); err != nil {
		if ctx.Err() != nil {
			return r.fail("canceled", ctx.Err())
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return r.fail("canceled", err)
		}
		return r.fail("export-unsafe", err)
	}
	s.root = root
	s.run = RecordRun{Workflow: s.contract.CI.ReleaseWorkflow, RunID: 1, RunAttempt: 1}
	o.Out = filepath.Join(temp, "outputs")
	o.FilesOnly = true
	r = executeBuild(ctx, s, o, runner, inherited)
	if r.Status != "ok" {
		return r
	}
	if len(expected) != len(r.Outputs) {
		r.Status = "mismatch"
		r.Error = &CheckError{Code: "artifact-mismatch", Message: "artifact set differs"}
		return r
	}
	for _, f := range r.Outputs {
		if expected[f.Name] != f.SHA256 {
			r.Status = "mismatch"
			r.Error = &CheckError{Code: "artifact-mismatch", Message: "artifact hash differs: " + Escape(f.Name)}
			return r
		}
	}
	return r
}
