// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// BuildOptions describes one explicitly selected build operation.
type BuildOptions struct {
	Path, Mode, Commit, Version, Product, Out, Smoke, Tag, Record, STWVersion string
	FilesOnly                                                                 bool
}
type BuildResult struct {
	SchemaVersion int              `json:"schema_version"`
	Command       string           `json:"command"`
	Status        string           `json:"status"`
	Mode          string           `json:"mode"`
	Builds        []BuildExecution `json:"builds"`
	Outputs       []FileRef        `json:"outputs"`
	Unexpected    []string         `json:"unexpected"`
	Error         *CheckError      `json:"error"`
}
type BuildExecution struct {
	ID          string                  `json:"id"`
	Invocations []BuildInvocationResult `json:"invocations"`
}
type buildState struct {
	root, commit, version, configSHA, repository, stwVersion string
	epoch                                                    int64
	contract                                                 *Contract
	run                                                      RecordRun
	readTree                                                 func(string, int) ([]byte, error)
	hashTree                                                 func(string) (string, error)
}

func newBuildResult(mode string) BuildResult {
	return BuildResult{SchemaVersion: 1, Command: "release-build", Status: "ok", Mode: mode, Builds: []BuildExecution{}, Outputs: []FileRef{}, Unexpected: []string{}}
}
func (r *BuildResult) fail(code string, err error) BuildResult {
	if errors.Is(err, errReleaseLimit) {
		code = "release-limit"
	}
	r.Status = "error"
	r.Error = &CheckError{Code: code, Message: Escape(err.Error())}
	return *r
}
func (r BuildResult) Text() string {
	var out strings.Builder
	fmt.Fprintf(&out, "release build %s: %s\n", Escape(r.Mode), r.Status)
	for _, b := range r.Builds {
		for _, i := range b.Invocations {
			fmt.Fprintf(&out, "%s: exit %d\n%s%s", Escape(b.ID), i.Exit, i.Stdout, i.Stderr)
			if i.StdoutTruncated {
				out.WriteString("\nstdout truncated: further output discarded\n")
			}
			if i.StderrTruncated {
				out.WriteString("\nstderr truncated: further output discarded\n")
			}
		}
	}
	for _, n := range r.Unexpected {
		fmt.Fprintf(&out, "unexpected-output: %s\n", n)
	}
	if r.Error != nil {
		fmt.Fprintf(&out, "%s: %s\n", r.Error.Code, r.Error.Message)
	}
	return out.String()
}
func (s buildState) values(target *Target) map[string]string {
	values := map[string]string{"version": s.version, "version_number": strings.TrimPrefix(s.version, "v"), "commit": s.commit, "commit12": s.commit[:min(12, len(s.commit))], "source_date_epoch": strconv.FormatInt(s.epoch, 10), "out_dir": filepath.Join(s.root, "dist/release")}
	if target != nil {
		values["os"], values["arch"], values["target"] = target.OS, target.Arch, target.ID
		values["output"] = buildSubstitute(target.Output, values)
	}
	return values
}
func executeBuild(ctx context.Context, s buildState, o BuildOptions, runner buildRunner, inherited []string) BuildResult {
	r := newBuildResult(o.Mode)
	if err := ctx.Err(); err != nil {
		return r.fail("canceled", err)
	}
	scheme, _ := CompileVersionScheme(s.contract.Version)
	releaseTag := ""
	if scheme != nil {
		releaseTag, _ = scheme.Tag(s.version)
	}
	for _, target := range s.contract.Target {
		if target.Kind == "oci-image" {
			staging := buildSubstitute(target.StagingTag, s.values(&target))
			if staging == s.version || staging == s.values(nil)["version_number"] || (releaseTag != "" && staging == releaseTag) {
				return r.fail("build-failed", errors.New("OCI staging tag equals a release promotion tag"))
			}
		}
	}
	if o.Smoke != "" {
		return executeSmoke(ctx, s, o, runner, inherited)
	}
	if o.FilesOnly {
		kinds := map[string]uint8{}
		for _, target := range s.contract.Target {
			if target.Kind == "file" {
				kinds[target.Build] |= 1
			} else if target.Kind == "oci-image" {
				kinds[target.Build] |= 2
			}
		}
		for _, build := range s.contract.Build {
			if build.Scope == "once" && kinds[build.ID] == 3 {
				return r.fail("unsupported-target", fmt.Errorf("files-only requires separate file/image builds; once build %s is shared", build.ID))
			}
		}
	}
	out, err := filepath.Abs(o.Out)
	if err != nil {
		return r.fail("output-exists", err)
	}
	// Mkdir is deliberately exclusive; an existing empty directory is still a refusal.
	if err := os.Mkdir(out, 0755); err != nil {
		return r.fail("output-exists", err)
	}
	targets := []Target{}
	for _, t := range s.contract.Target {
		if !o.FilesOnly || t.Kind != "oci-image" {
			targets = append(targets, t)
		}
	}
	manifest := BuildManifest{SchemaVersion: 1, Kind: "build-manifest", Project: s.contract.Project.Name, Product: s.contract.Project.Product, Version: s.version, Commit: s.commit, SourceDateEpoch: &s.epoch, ConfigSHA256: s.configSHA, STWVersion: s.stwVersion, Repository: s.repository, Run: s.run, Builds: []ManifestBuild{}, Assets: []RecordAsset{}}
	for _, b := range s.contract.Build {
		refs := []Target{}
		for _, t := range targets {
			if t.Build == b.ID {
				refs = append(refs, t)
			}
		}
		if len(refs) == 0 {
			continue
		}
		disclosure, err := s.manifestBuild(b)
		if err != nil {
			return r.fail("build-failed", err)
		}
		manifest.Builds = append(manifest.Builds, disclosure)
		execution := BuildExecution{ID: b.ID, Invocations: []BuildInvocationResult{}}
		invocations := len(refs)
		if b.Scope == "once" {
			invocations = 1
		}
		for index := 0; index < invocations; index++ {
			var target *Target
			if b.Scope == "per-target" {
				target = &refs[index]
			}
			values := s.values(target)
			argv := make([]string, len(b.Argv))
			for i, a := range b.Argv {
				argv[i] = buildSubstitute(a, values)
			}
			env, err := buildEnvironment(b, values, inherited, s.epoch)
			if err != nil {
				return r.fail("build-failed", err)
			}
			dir, err := buildWorkingDir(s.root, b.WorkingDir)
			if err != nil {
				return r.fail("build-failed", err)
			}
			timeout := b.TimeoutSeconds
			if timeout == 0 {
				timeout = defaultTimeout
			}
			result := runBuildCommand(ctx, runner, BuildCommand{Argv: argv, Env: env, Dir: dir, Timeout: time.Duration(timeout) * time.Second})
			if target != nil {
				id := target.ID
				result.Target = &id
			}
			result.Stdout, result.Stderr = Escape(result.Stdout), Escape(result.Stderr)
			execution.Invocations = append(execution.Invocations, result)
			if result.err != nil || result.Exit != 0 {
				r.Builds = append(r.Builds, execution)
				code := "build-failed"
				if ctx.Err() != nil {
					code = "canceled"
				}
				return r.fail(code, fmt.Errorf("build %s failed (exit %d)", b.ID, result.Exit))
			}
		}
		r.Builds = append(r.Builds, execution)
	}
	names := map[string]bool{}
	declared := map[string]bool{}
	for _, t := range targets {
		values := s.values(&t)
		if t.Kind == "file" {
			p := buildSubstitute(t.Output, values)
			if !strings.HasPrefix(p, "dist/release/") {
				return r.fail("build-failed", errors.New("output outside dist/release"))
			}
			name := path.Base(p)
			if names[name] || name == "build-manifest.json" || name == s.contract.Outputs.Checksums {
				return r.fail("build-failed", errors.New("duplicate or reserved output name"))
			}
			names[name] = true
			declared[p] = true
			ref, err := copyBuildOutput(s.root, p, out, name)
			if err != nil {
				return r.fail("build-failed", err)
			}
			r.Outputs = append(r.Outputs, ref)
			manifest.Assets = append(manifest.Assets, RecordAsset{File: &FileAsset{Kind: "file", Target: t.ID, Role: t.Role, Group: t.Group, Build: t.Build, Name: name, URLTemplate: s.contract.Outputs.URLTemplate, SHA256: ref.SHA256, Size: ref.Size, OS: t.OS, Arch: t.Arch}})
		} else {
			p := buildSubstitute(t.ResultFile, values)
			declared[p] = true
			raw, err := readBuildFile(s.root, p, MaxReleaseDocumentBytes)
			if err != nil {
				return r.fail("build-failed", err)
			}
			document, findings := ParseReleaseDocument(raw)
			result, ok := document.(*OCIResult)
			if !ok || len(findings) > 0 {
				return r.fail("build-failed", errors.New("invalid OCI result"))
			}
			wanted := append([]string{}, t.Platforms...)
			actual := []string{}
			for _, p := range result.Platforms {
				value := p.OS + "/" + p.Arch
				if p.Variant != "" {
					value += "/" + p.Variant
				}
				actual = append(actual, value)
			}
			sort.Strings(wanted)
			sort.Strings(actual)
			if strings.Join(wanted, "\n") != strings.Join(actual, "\n") {
				return r.fail("build-failed", errors.New("OCI platform set differs from contract"))
			}
			image := &OCIImageAsset{Kind: "oci-image", Target: t.ID, Role: t.Role, Group: t.Group, Build: t.Build, Repository: t.Repository, IndexDigest: result.IndexDigest, Platforms: result.Platforms, Tags: []string{s.version}}
			for _, item := range []struct {
				p    string
				dest **PathSHA
			}{{t.LockFile, &image.Lock}, {t.ToolListFile, &image.ToolList}} {
				if item.p != "" {
					hash, err := s.treeHash(item.p)
					if err != nil {
						return r.fail("build-failed", err)
					}
					*item.dest = &PathSHA{Path: item.p, SHA256: hash}
				}
			}
			if t.SourceOffer != "" {
				offer := t.SourceOffer
				image.SourceOffer = &offer
			}
			manifest.Assets = append(manifest.Assets, RecordAsset{Image: image})
		}
	}
	err = filepath.WalkDir(filepath.Join(s.root, "dist/release"), func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !declared[rel] {
			r.Unexpected = append(r.Unexpected, Escape(rel))
		}
		if len(r.Unexpected) > 20000 {
			return errors.New("too many output entries")
		}
		return nil
	})
	if err != nil {
		return r.fail("release-limit", err)
	}
	sort.Strings(r.Unexpected)
	sort.Slice(r.Outputs, func(i, j int) bool { return r.Outputs[i].Name < r.Outputs[j].Name })

	raw, err := EncodeReleaseDocument(manifest)
	if err != nil {
		return r.fail("build-failed", err)
	}
	if _, findings := ParseReleaseDocument(raw); len(findings) > 0 {
		return r.fail("build-failed", fmt.Errorf("manifest invalid: %v", findings))
	}
	sumFiles := append([]FileRef{}, r.Outputs...)
	sumFiles = append(sumFiles, FileRef{Name: "build-manifest.json", SHA256: buildHash(raw), Size: int64(len(raw))})
	sort.Slice(sumFiles, func(i, j int) bool { return sumFiles[i].Name < sumFiles[j].Name })
	var sums strings.Builder
	for _, f := range sumFiles {
		fmt.Fprintf(&sums, "%s  %s\n", f.SHA256, f.Name)
	}
	if err := writeBuildFile(out, s.contract.Outputs.Checksums, []byte(sums.String())); err != nil {
		return r.fail("build-failed", err)
	}
	if err := writeBuildFile(out, "build-manifest.json", raw); err != nil {
		return r.fail("build-failed", err)
	}
	return r
}
func buildHash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func readBuildFile(root, p string, limit int) ([]byte, error) {
	if !fs.ValidPath(p) || strings.Contains(p, "\\") {
		return nil, errors.New("unsafe output path")
	}
	current := root
	for _, part := range strings.Split(p, "/") {
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("symlink output refused")
		}
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	f, err := buildOpenRegular(dir, p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("output must be regular")
	}
	if st.Size() > int64(limit) {
		return nil, fmt.Errorf("%w: output exceeds limit", errReleaseLimit)
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if len(raw) > limit {
		return nil, fmt.Errorf("%w: output exceeds limit", errReleaseLimit)
	}
	return raw, err
}
func writeBuildFile(root, name string, raw []byte) error {
	if !fs.ValidPath(name) || path.Base(name) != name {
		return errors.New("unsafe collected name")
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	f, err := dir.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func copyBuildOutput(root, p, out, name string) (FileRef, error) {
	// Artifacts are bounded by the release's 2 GiB per-file limit; stream rather
	// than retain them in memory. Root.Open prevents traversal outside the root.
	if !fs.ValidPath(p) {
		return FileRef{}, errors.New("unsafe output path")
	}
	current := root
	for _, part := range strings.Split(p, "/") {
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if err != nil {
			return FileRef{}, err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return FileRef{}, errors.New("symlink output")
		}
	}
	srcRoot, err := os.OpenRoot(root)
	if err != nil {
		return FileRef{}, err
	}
	defer srcRoot.Close()
	src, err := buildOpenRegular(srcRoot, p)
	if err != nil {
		return FileRef{}, err
	}
	defer src.Close()
	before, err := src.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return FileRef{}, errors.New("output not regular")
	}
	if before.Size() > 2<<30 {
		return FileRef{}, fmt.Errorf("%w: artifact exceeds limit", errReleaseLimit)
	}
	dstRoot, err := os.OpenRoot(out)
	if err != nil {
		return FileRef{}, err
	}
	defer dstRoot.Close()
	dst, err := dstRoot.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return FileRef{}, err
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(src, (2<<30)+1))
	closeErr := dst.Close()
	if err != nil {
		return FileRef{}, err
	}
	if closeErr != nil {
		return FileRef{}, closeErr
	}
	after, err := src.Stat()
	if size > 2<<30 {
		return FileRef{}, fmt.Errorf("%w: artifact exceeds limit", errReleaseLimit)
	}
	if err != nil || before.Size() != size || after.Size() != size || !before.ModTime().Equal(after.ModTime()) {
		return FileRef{}, errors.New("output changed or exceeded limit")
	}
	return FileRef{Name: name, SHA256: hex.EncodeToString(hash.Sum(nil)), Size: size}, nil
}
func executeSmoke(ctx context.Context, s buildState, o BuildOptions, runner buildRunner, inherited []string) BuildResult {
	r := newBuildResult(o.Mode)
	for _, t := range s.contract.Target {
		if t.ID != o.Smoke {
			continue
		}
		if t.Kind != "file" || len(t.SmokeArgv) == 0 {
			return r.fail("build-failed", errors.New("target has no file smoke command"))
		}
		out, err := filepath.Abs(o.Out)
		if err != nil {
			return r.fail("build-failed", err)
		}
		values := s.values(&t)
		name := path.Base(values["output"])
		if err := checkSmokeArtifact(out, name); err != nil {
			return r.fail("build-failed", err)
		}
		values["artifact"], values["out_dir"] = filepath.Join(out, name), out
		argv := make([]string, len(t.SmokeArgv))
		for i, a := range t.SmokeArgv {
			argv[i] = buildSubstitute(a, values)
		}
		b := Build{EnvInherit: []string{"PATH", "HOME", "TMPDIR"}}
		env, err := buildEnvironment(b, values, inherited, s.epoch)
		if err != nil {
			return r.fail("build-failed", err)
		}
		result := runBuildCommand(ctx, runner, BuildCommand{Argv: argv, Env: env, Dir: s.root, Timeout: defaultTimeout * time.Second})
		id := t.ID
		result.Target = &id
		result.Stdout, result.Stderr = Escape(result.Stdout), Escape(result.Stderr)
		r.Builds = append(r.Builds, BuildExecution{ID: t.Build, Invocations: []BuildInvocationResult{result}})
		if result.Exit != 0 || result.err != nil {
			return r.fail("build-failed", errors.New("smoke failed"))
		}
		return r
	}
	return r.fail("build-failed", errors.New("unknown smoke target"))
}

func checkSmokeArtifact(root, name string) error {
	if !fs.ValidPath(name) || path.Base(name) != name {
		return errors.New("unsafe smoke artifact")
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	f, err := buildOpenRegular(dir, name)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return errors.New("smoke artifact must be regular")
	}
	if st.Size() > 2<<30 {
		return fmt.Errorf("%w: smoke artifact exceeds limit", errReleaseLimit)
	}
	return nil
}

func (s buildState) treeHash(p string) (string, error) {
	if s.hashTree != nil {
		return s.hashTree(p)
	}
	if s.readTree == nil {
		return "", errors.New("tree reader unavailable")
	}
	raw, err := s.readTree(p, MaxReleaseDocumentBytes)
	if err != nil {
		return "", err
	}
	return buildHash(raw), nil
}

func (s buildState) manifestBuild(b Build) (ManifestBuild, error) {
	disclosure := ManifestBuild{ID: b.ID, Network: b.Network, NetworkHosts: append([]string{}, b.NetworkHosts...), LockFiles: []PathSHA{}}
	for _, p := range b.LockFiles {
		hash, err := s.treeHash(p)
		if err != nil {
			return ManifestBuild{}, err
		}
		disclosure.LockFiles = append(disclosure.LockFiles, PathSHA{Path: p, SHA256: hash})
	}
	return disclosure, nil
}
