// SPDX-License-Identifier: EUPL-1.2
// Independent first-round probes, retained as regression tests.
package release

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestReviewerSafeExportBeyondInventedEntryLimit(t *testing.T) {
	entries := make([]buildTreeEntry, 20001)
	for i := range entries {
		entries[i] = buildTreeEntry{Name: fmt.Sprintf("f%05d", i), Mode: "100644"}
	}
	err := exportBuildTree(t.TempDir(), entries, func(string) ([]byte, error) { return nil, nil })
	if err != nil {
		t.Fatalf("safe regular tree refused without a normative export-count limit: %v", err)
	}
}
func TestReviewerOversizedRecordReportsNormativeLimit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "record.json")
	if err := os.WriteFile(p, []byte(strings.Repeat(" ", MaxReleaseDocumentBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	r := buildRelease(context.Background(), BuildOptions{Mode: "local-verify", Record: p}, &fakeBuildRunner{}, nil)
	if r.Error == nil || r.Error.Code != "release-limit" {
		t.Fatalf("oversized record got %+v; want release-limit", r.Error)
	}
}
func TestReviewerLocalVerifyRefusesSemanticallyInvalidRecord(t *testing.T) {
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	commit := commitAll(t, dir)
	raw, err := os.ReadFile("../../specs/026-release-process/examples/go-cli.record.json")
	if err != nil {
		t.Fatal(err)
	}
	d, f := ParseReleaseDocument(raw)
	if len(f) > 0 {
		t.Fatal(f)
	}
	record := d.(*ReleaseRecord)
	record.Commit = commit
	record.Project = "ForgedDifferentProject"
	for _, a := range record.Assets {
		a.File.SHA256 = buildHash([]byte("fake artifact"))
		a.File.Size = int64(len("fake artifact"))
	}
	raw, err = EncodeReleaseDocument(record)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "record.json")
	if err = os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeBuildRunner{run: func(_ context.Context, c BuildCommand, _, _ io.Writer) (int, error) {
		p := filepath.Join(c.Dir, c.Argv[5])
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return -1, err
		}
		return 0, os.WriteFile(p, []byte("fake artifact"), 0644)
	}}
	r := buildRelease(context.Background(), BuildOptions{Mode: "local-verify", Path: dir, Commit: commit, Version: record.Version, Record: p}, fake, nil)
	if r.Error == nil || r.Error.Code != "record-invalid" || len(fake.calls) != 0 {
		t.Fatalf("forged project got status=%s error=%+v calls=%d; want record-invalid before execution", r.Status, r.Error, len(fake.calls))
	}
}

func TestReviewerRejectsReleaseVersionAsStagingTag(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "example/test")
	t.Setenv("GITHUB_RUN_ID", "1")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	version := "2026.10.15-b98.1"
	cfg := strings.Replace(string(exampleBytes(t, "oci-bundle")), `staging_tag = "staging-{commit12}"`, `staging_tag = "`+version+`"`, 1)
	dir := repoWith(t, map[string][]byte{".stw/release.toolbox.toml": []byte(cfg), "bundles/toolbox/tools.lock.json": []byte("{}"), "bundles/toolbox/tools.json": []byte("{}")})
	commit := commitAll(t, dir)
	git(t, dir, "remote", "add", "origin", "https://github.com/example/test.git")
	git(t, dir, "update-ref", "refs/remotes/origin/main", commit)
	raw, err := os.ReadFile("../../specs/026-release-process/examples/oci-bundle.result.json")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeBuildRunner{run: func(_ context.Context, c BuildCommand, _, _ io.Writer) (int, error) {
		p := c.Argv[len(c.Argv)-1]
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return -1, err
		}
		return 0, os.WriteFile(p, raw, 0644)
	}}
	r := buildRelease(context.Background(), BuildOptions{Mode: "ci", Path: dir, Product: "toolbox", Commit: commit, Version: version, Out: filepath.Join(t.TempDir(), "out")}, fake, nil)
	if r.Status == "ok" || len(fake.calls) > 0 {
		t.Fatalf("release version staging accepted: status=%s error=%+v calls=%d", r.Status, r.Error, len(fake.calls))
	}
}

func TestReviewerSyntheticFilterHelper(t *testing.T) {
	if os.Getenv("STW_REVIEWER_FILTER_HELPER") != "1" {
		return
	}
	if err := os.WriteFile(os.Getenv("STW_REVIEWER_FILTER_MARKER"), []byte(os.Getenv("GITHUB_TOKEN")), 0600); err != nil {
		os.Exit(33)
	}
	io.Copy(os.Stdout, os.Stdin)
	os.Exit(0)
}
func TestReviewerCheckoutDoesNotRunUndeclaredFilterWithSecrets(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "example/test")
	t.Setenv("GITHUB_RUN_ID", "1")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli"), ".gitattributes": []byte("tracked filter=reviewer\n"), "tracked": []byte("original\n")})
	commit := commitAll(t, dir)
	git(t, dir, "remote", "add", "origin", "https://github.com/example/test.git")
	git(t, dir, "update-ref", "refs/remotes/origin/main", commit)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	git(t, dir, "config", "filter.reviewer.clean", binary+" -test.run=^TestReviewerSyntheticFilterHelper$")
	marker := filepath.Join(t.TempDir(), "filter-marker")
	t.Setenv("STW_REVIEWER_FILTER_HELPER", "1")
	t.Setenv("STW_REVIEWER_FILTER_MARKER", marker)
	t.Setenv("GITHUB_TOKEN", "synthetic-review-secret")
	// Replace the tracked file without changing its bytes, forcing Git to recheck content.
	if err = os.Remove(filepath.Join(dir, "tracked")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "tracked"), []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fake := &fakeBuildRunner{run: func(_ context.Context, c BuildCommand, _, _ io.Writer) (int, error) {
		p := filepath.Join(c.Dir, c.Argv[5])
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return -1, err
		}
		return 0, os.WriteFile(p, []byte("fake"), 0644)
	}}
	r := buildRelease(context.Background(), BuildOptions{Mode: "ci", Path: dir, Commit: commit, Version: "v1.2.3", Out: filepath.Join(t.TempDir(), "out")}, fake, nil)
	if b, e := os.ReadFile(marker); e == nil {
		t.Fatalf("undeclared clean filter executed with token=%q; status=%s error=%+v fake_calls=%d", b, r.Status, r.Error, len(fake.calls))
	}
	if r.Error == nil || !strings.Contains(r.Error.Message, "checkout conversion helper") || len(fake.calls) != 0 {
		t.Fatalf("configured checkout helper was not refused before project execution: result=%+v fake_calls=%d", r, len(fake.calls))
	}
}

func TestReviewerExportNormalizesModesUnderRestrictiveUmask(t *testing.T) {
	old := syscall.Umask(0077)
	defer syscall.Umask(old)
	root := t.TempDir()
	err := exportBuildTree(root, []buildTreeEntry{{Name: "regular", Mode: "100644"}, {Name: "executable", Mode: "100755"}}, func(string) ([]byte, error) { return []byte("fixture"), nil })
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]os.FileMode{"regular": 0644, "executable": 0755} {
		st, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != want {
			t.Errorf("%s mode %04o; contract requires %04o regardless of local umask", name, st.Mode().Perm(), want)
		}
	}
}

func TestReviewerLocalVerifyRefusesDuplicateAssetWithDifferentDigest(t *testing.T) {
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	commit := commitAll(t, dir)
	raw, err := os.ReadFile("../../specs/026-release-process/examples/go-cli.record.json")
	if err != nil {
		t.Fatal(err)
	}
	d, f := ParseReleaseDocument(raw)
	if len(f) > 0 {
		t.Fatal(f)
	}
	record := d.(*ReleaseRecord)
	record.Commit = commit

	for _, a := range record.Assets {
		a.File.SHA256 = buildHash([]byte("fake artifact"))
		a.File.Size = int64(len("fake artifact"))
	}
	duplicate := *record.Assets[0].File
	duplicate.SHA256 = strings.Repeat("f", 64)
	record.Assets = append([]RecordAsset{{File: &duplicate}}, record.Assets...)
	raw, err = EncodeReleaseDocument(record)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "record.json")
	if err = os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeBuildRunner{run: func(_ context.Context, c BuildCommand, _, _ io.Writer) (int, error) {
		p := filepath.Join(c.Dir, c.Argv[5])
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return -1, err
		}
		return 0, os.WriteFile(p, []byte("fake artifact"), 0644)
	}}
	r := buildRelease(context.Background(), BuildOptions{Mode: "local-verify", Path: dir, Commit: commit, Version: record.Version, Record: p}, fake, nil)
	if r.Error == nil || r.Error.Code != "record-invalid" || len(fake.calls) != 0 {
		t.Fatalf("duplicate mismatching asset got status=%s error=%+v calls=%d; want record-invalid before execution", r.Status, r.Error, len(fake.calls))
	}
}

func TestBuildNativeExportStreamsLargeTreeAndBlob(t *testing.T) {
	dir := repoWith(t, nil)
	for i := 0; i < 20001; i++ {
		write(t, dir, fmt.Sprintf("%05d-%s", i, strings.Repeat("p", 180)), nil)
	}
	large := filepath.Join(dir, "large-source")
	file, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((64 << 20) + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	commit := commitAll(t, dir)
	source, err := newGitSource(context.Background(), dir, commit)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := exportGitBuildTree(context.Background(), source, out); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(out)
	if err != nil || len(entries) != 20002 {
		t.Fatalf("entries%d err%v", len(entries), err)
	}
	st, err := os.Stat(filepath.Join(out, "large-source"))
	if err != nil || st.Size() != (64<<20)+1 {
		t.Fatalf("%v %v", st, err)
	}
	original, err := os.Open(large)
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.New()
	if _, err := io.Copy(expected, original); err != nil {
		t.Fatal(err)
	}
	original.Close()
	write(t, dir, "large-source", []byte("changed working copy"))
	entry, _, err := source.pathEntry("large-source")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hashBuildGitBlob(context.Background(), dir, entry.id)
	if err != nil || hash != hex.EncodeToString(expected.Sum(nil)) {
		t.Fatalf("trusted source hash %s err%v", hash, err)
	}
}

func buildComparisonReviewFixture(t *testing.T) (BuildOptions, *ReleaseRecord, *BuildManifest, func(context.Context, BuildCommand, io.Writer, io.Writer) (int, error)) {
	t.Helper()
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "example/test")
	t.Setenv("GITHUB_RUN_ID", "1")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	dir := repoWith(t, map[string][]byte{".stw/release.toml": exampleBytes(t, "go-cli")})
	commit := commitAll(t, dir)
	git(t, dir, "remote", "add", "origin", "https://github.com/example/test.git")
	git(t, dir, "update-ref", "refs/remotes/origin/main", commit)
	run := func(_ context.Context, c BuildCommand, _, _ io.Writer) (int, error) {
		p := filepath.Join(c.Dir, c.Argv[5])
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return -1, err
		}
		return 0, os.WriteFile(p, []byte("fake artifact"), 0644)
	}
	opts := BuildOptions{Mode: "ci", Path: dir, Commit: commit, Version: "v1.2.3", Out: filepath.Join(t.TempDir(), "ci")}
	result := buildRelease(context.Background(), opts, &fakeBuildRunner{run: run}, nil)
	if result.Status != "ok" {
		t.Fatalf("fixture %+v", result)
	}
	manifestRaw, err := os.ReadFile(filepath.Join(opts.Out, "build-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, findings := ParseReleaseDocument(manifestRaw)
	if len(findings) > 0 {
		t.Fatal(findings)
	}
	manifest := doc.(*BuildManifest)
	raw, err := os.ReadFile("../../specs/026-release-process/examples/go-cli.record.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, findings = ParseReleaseDocument(raw)
	if len(findings) > 0 {
		t.Fatal(findings)
	}
	record := doc.(*ReleaseRecord)
	record.Project, record.Product, record.Version, record.Tag, record.Commit, record.Repository, record.SourceDateEpoch, record.Assets = manifest.Project, manifest.Product, manifest.Version, manifest.Version, manifest.Commit, manifest.Repository, manifest.SourceDateEpoch, manifest.Assets
	record.Build = RecordBuild{Workflow: manifest.Run.Workflow, RunID: manifest.Run.RunID, RunAttempt: manifest.Run.RunAttempt, STWVersion: manifest.STWVersion}
	record.Attestation.SignerWorkflow = manifest.Repository + "/" + manifest.Run.Workflow
	sums, err := os.ReadFile(filepath.Join(opts.Out, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	record.Checksums = FileRef{Name: "SHA256SUMS", SHA256: buildHash(sums), Size: int64(len(sums))}
	record.Manifest = FileRef{Name: "build-manifest.json", SHA256: buildHash(manifestRaw), Size: int64(len(manifestRaw))}
	opts.Mode, opts.Out = "local-verify", ""
	t.Setenv("GITHUB_ACTIONS", "false")
	return opts, record, manifest, run
}
func TestBuildComparisonValidatesTrustedRecordAndManifestBeforeExport(t *testing.T) {
	opts, record, manifest, run := buildComparisonReviewFixture(t)
	for _, kind := range []string{"record", "manifest"} {
		for _, change := range []string{"valid", "project", "product", "repository", "duplicate", "extra-semantics", "provenance"} {
			t.Run(kind+"/"+change, func(t *testing.T) {
				var base any = record
				if kind == "manifest" {
					base = manifest
				}
				raw, err := EncodeReleaseDocument(base)
				if err != nil {
					t.Fatal(err)
				}
				doc, findings := ParseReleaseDocument(raw)
				if len(findings) > 0 {
					t.Fatal(findings)
				}
				var project, product, repository *string
				var assets *[]RecordAsset
				switch d := doc.(type) {
				case *ReleaseRecord:
					project, product, repository, assets = &d.Project, &d.Product, &d.Repository, &d.Assets
				case *BuildManifest:
					project, product, repository, assets = &d.Project, &d.Product, &d.Repository, &d.Assets
				}
				switch change {
				case "project":
					*project = "forged"
				case "product":
					*product = "forged"
				case "repository":
					*repository = "forged/repository"
				case "duplicate":
					duplicate := *(*assets)[0].File
					duplicate.SHA256 = strings.Repeat("f", 64)
					*assets = append([]RecordAsset{{File: &duplicate}}, (*assets)...)
				case "provenance":
					switch d := doc.(type) {
					case *ReleaseRecord:
						d.Attestation.SignerWorkflow = "forged/repository/.github/workflows/release.yml"
					case *BuildManifest:
						d.Builds[0].Network = "fetch"
					}
				case "extra-semantics":
					switch d := doc.(type) {
					case *ReleaseRecord:
						d.Tag = "v9.9.9"
					case *BuildManifest:
						d.ConfigSHA256 = strings.Repeat("f", 64)
					}
				}
				raw, err = EncodeReleaseDocument(doc)
				if err != nil {
					t.Fatal(err)
				}
				local := opts
				local.Record = filepath.Join(t.TempDir(), "comparison.json")
				if err := os.WriteFile(local.Record, raw, 0600); err != nil {
					t.Fatal(err)
				}
				fake := &fakeBuildRunner{run: run}
				result := buildRelease(context.Background(), local, fake, nil)
				if change == "valid" {
					if result.Status != "ok" || len(fake.calls) != 3 {
						t.Fatalf("valid %+v calls%d", result, len(fake.calls))
					}
				} else if result.Error == nil || result.Error.Code != "record-invalid" || len(fake.calls) != 0 {
					t.Fatalf("%+v calls%d", result, len(fake.calls))
				}
			})
		}
	}
}

func TestBuildExportHasNoAggregateVolumeCut(t *testing.T) {
	root := t.TempDir()
	writer, err := newBuildTreeWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	const size = (1 << 30) + 1
	for _, name := range []string{"first", "second"} {
		if err := writer.Write(buildTreeEntry{Name: name, Mode: "100644"}, func(dst io.Writer) error { return dst.(*os.File).Truncate(size) }); err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(filepath.Join(root, name))
		if err != nil || st.Size() != size {
			t.Fatalf("%v %v", st, err)
		}
	}
}
func TestBuildExpandedStagingTagCannotEqualVersion(t *testing.T) {
	s := fixtureBuildState(t)
	s.version = s.commit[:12]
	s.contract.Target = append(s.contract.Target, Target{ID: "image", Kind: "oci-image", Build: "fake", StagingTag: "{commit12}"})
	fake := &fakeBuildRunner{}
	r := executeBuild(context.Background(), s, BuildOptions{Mode: "ci", Out: filepath.Join(t.TempDir(), "new")}, fake, nil)
	if r.Error == nil || len(fake.calls) != 0 {
		t.Fatalf("%+v calls%d", r, len(fake.calls))
	}
}
func TestBuildOCIResultLimitRetainsTypedFailure(t *testing.T) {
	s := fixtureBuildState(t)
	s.contract.Target = []Target{{ID: "image", Kind: "oci-image", Role: "artifact", Build: "fake", StagingTag: "staging-{commit12}", ResultFile: "dist/release/result.json"}}
	fake := &fakeBuildRunner{run: func(context.Context, BuildCommand, io.Writer, io.Writer) (int, error) {
		return 0, os.WriteFile(filepath.Join(s.root, "dist/release/result.json"), []byte(strings.Repeat(" ", MaxReleaseDocumentBytes+1)), 0600)
	}}
	r := executeBuild(context.Background(), s, BuildOptions{Mode: "ci", Out: filepath.Join(t.TempDir(), "new")}, fake, nil)
	if r.Error == nil || r.Error.Code != "release-limit" {
		t.Fatalf("%+v", r)
	}
}
func TestBuildExportBlobProtocolRejectsTruncatedOrMismatchedBodies(t *testing.T) {
	id := strings.Repeat("a", 40)
	for _, raw := range []string{id + " blob 2\nx\n", id + " blob 1\nx!", strings.Repeat("b", 40) + " blob 1\nx\n", id + " tree 1\nx\n", id + " blob -1\n"} {
		if err := copyBuildExportBlob(io.Discard, bufio.NewReader(strings.NewReader(raw)), id); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if err := copyBuildExportBlob(io.Discard, bufio.NewReader(strings.NewReader(id+" blob 3\nabc\n")), id); err != nil {
		t.Fatal(err)
	}
}

func TestBuildOversizedArtifactPreflightRetainsTypedLimit(t *testing.T) {
	root := t.TempDir()
	file, err := os.Create(filepath.Join(root, "artifact"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((2 << 30) + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err := checkSmokeArtifact(root, "artifact"); !errors.Is(err, errReleaseLimit) {
		t.Fatalf("smoke limit: %v", err)
	}
	out := t.TempDir()
	if _, err := copyBuildOutput(root, "artifact", out, "artifact"); !errors.Is(err, errReleaseLimit) {
		t.Fatalf("collection limit: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(out, "artifact")); !os.IsNotExist(err) {
		t.Fatalf("oversized artifact copied: %v", err)
	}
}

func TestBuildStagingRefusesBothPromotionVersionFormsBeforeEarlierFileBuild(t *testing.T) {
	for _, placeholder := range []string{"{version}", "{version_number}"} {
		t.Run(placeholder, func(t *testing.T) {
			raw := loadExample(t, "oci-bundle")
			raw["version"] = map[string]any{"scheme": "semver", "source": "input"}
			staging := buildSubstitute(placeholder, map[string]string{"version": "v1.2.3", "version_number": "1.2.3"})
			raw["target"].([]any)[0].(map[string]any)["staging_tag"] = staging
			raw["promote"].(map[string]any)["argv"] = []any{"crane", "tag", "{repository}@{digest}", placeholder}
			c, findings, err := ValidateContract(raw, ValidateOptions{Product: "toolbox"})
			if err != nil || c == nil || len(findings) != 0 {
				t.Fatalf("invalid fixture: %v %v", err, findings)
			}
			s := fixtureBuildState(t)
			c.Build = append(append([]Build{}, s.contract.Build...), c.Build...)
			c.Target = append(append([]Target{}, s.contract.Target...), c.Target...)
			s.contract = c
			fake := &fakeBuildRunner{run: func(context.Context, BuildCommand, io.Writer, io.Writer) (int, error) { return 0, nil }}
			r := executeBuild(context.Background(), s, BuildOptions{Mode: "ci", Out: filepath.Join(t.TempDir(), "out")}, fake, nil)
			if r.Error == nil || r.Error.Code != "build-failed" || len(fake.calls) != 0 {
				t.Fatalf("staging collision reached project commands: result=%+v calls=%d", r, len(fake.calls))
			}
		})
	}
}

func TestBuildCheckoutRefusesEffectiveConversionHelpersBeforeNativeComparison(t *testing.T) {
	for _, placement := range []string{"local-clean", "local-process", "included-clean", "worktree-clean"} {
		t.Run(placement, func(t *testing.T) {
			dir := repoWith(t, map[string][]byte{".gitattributes": []byte("tracked filter=reviewer\n"), "tracked": []byte("original\n")})
			commit := commitAll(t, dir)
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "marker")
			t.Setenv("STW_REVIEWER_FILTER_HELPER", "1")
			t.Setenv("STW_REVIEWER_FILTER_MARKER", marker)
			t.Setenv("GITHUB_TOKEN", "synthetic-review-secret")
			command := binary + " -test.run=^TestReviewerSyntheticFilterHelper$"
			switch placement {
			case "local-clean":
				git(t, dir, "config", "filter.reviewer.clean", command)
			case "local-process":
				git(t, dir, "config", "filter.reviewer.process", command)
			case "included-clean":
				included := filepath.Join(t.TempDir(), "filter.config")
				git(t, dir, "config", "--file", included, "filter.reviewer.clean", command)
				git(t, dir, "config", "include.path", included)
			case "worktree-clean":
				git(t, dir, "config", "extensions.worktreeConfig", "true")
				git(t, dir, "config", "--worktree", "filter.reviewer.clean", command)
			}
			err = checkBuildCheckout(context.Background(), dir, commit)
			if err == nil || !strings.Contains(err.Error(), "checkout conversion helper") {
				t.Fatalf("helper configuration reached native comparison: %v", err)
			}
			if b, err := os.ReadFile(marker); err == nil {
				t.Fatalf("undeclared helper saw synthetic secret: %q", b)
			}
		})
	}
}
