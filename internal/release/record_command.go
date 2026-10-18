// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/wstein/sourcesteward/internal/inspection"
	"path/filepath"
	"strings"
)

var errRecordWriteUnsupported = errors.New("descriptor-relative no-follow record writing is unavailable on this platform")

type RecordOptions struct {
	RunIDSet, RunAttemptSet                                               bool
	Path, Commit, Version, From, Product, Repo, SignerWorkflowFile, Check string
	RunID, RunAttempt                                                     int64
	Write                                                                 bool
}
type RecordResult struct {
	SchemaVersion   int            `json:"schema_version"`
	Command         string         `json:"command"`
	Status          string         `json:"status"`
	Record          *ReleaseRecord `json:"record"`
	Path            string         `json:"path"`
	Written         bool           `json:"written"`
	CanonicalSHA256 string         `json:"canonical_sha256"`
	Error           *CheckError    `json:"error"`
	Canonical       []byte         `json:"-"`
	Backend         string         `json:"backend"`
	action          string
}

func recordFailure(r RecordResult, code, msg string) RecordResult {
	r.Status = "refused"
	switch code {
	case "artifact-mismatch", "record-invalid", "record-not-canonical":
		r.Status = "failed"
	}
	r.Error = &CheckError{code, Escape(msg)}
	r.Record = nil
	r.Canonical = nil
	return r
}
func recordReadFailure(r RecordResult, ctx context.Context, e error) RecordResult {
	code := "read-failed"
	if errors.Is(e, errReleaseLimit) || errors.Is(e, inspection.ErrJJReleaseLimit) {
		code = "release-limit"
	}
	if errors.Is(e, errJJCommittedSymlinkInput) {
		code = "unsupported-backend"
	}
	if ctx.Err() != nil {
		code = "canceled"
	}
	return recordFailure(r, code, e.Error())
}
func Record(ctx context.Context, o RecordOptions) RecordResult {
	r := RecordResult{SchemaVersion: 1, Command: "release-record", Status: "refused"}
	root, backend, problem := detectOwner(ctx, o.Path, true)
	r.Backend = backend
	if problem != nil {
		return recordFailure(r, problem.code, problem.message)
	}
	if backend != "git" && backend != "jj" {
		return recordFailure(r, "unsupported-backend", "record requires native Git or jj ownership")
	}
	if backend == "git" {
		g, e := newGitSource(ctx, root, o.Commit)
		if errors.Is(e, errCommitUnresolved) {
			return recordFailure(r, "commit-unresolved", e.Error())
		}
		if e != nil {
			return recordReadFailure(r, ctx, e)
		}
		r = assembleRecord(ctx, o, g, r)
	} else {
		e := inspection.WithJJReleaseTree(ctx, root, o.Commit, func(reader *inspection.JJReleaseReader) error {
			r = assembleRecord(ctx, o, jjSource{reader}, r)
			return nil
		})
		if errors.Is(e, inspection.ErrJJReleaseCommitUnresolved) {
			return recordFailure(r, "commit-unresolved", e.Error())
		}
		if e != nil {
			return recordReadFailure(r, ctx, e)
		}
	}
	if r.Status != "ok" {
		return r
	}
	if e := ctx.Err(); e != nil {
		return recordReadFailure(r, ctx, e)
	}
	r.action = "print"
	if o.Check != "" {
		r.action = "check"
		raw, e := readArtifactBytes(o.Check, MaxReleaseDocumentBytes)
		if e != nil {
			return recordReadFailure(r, ctx, e)
		}
		if !bytes.Equal(raw, r.Canonical) {
			_, ff := ParseReleaseDocument(raw)
			code := "record-not-canonical"
			if len(ff) > 0 && ff[0].Code != "record-not-canonical" {
				code = ff[0].Code
			}
			return recordFailure(r, code, "check file differs from the computed canonical record")
		}
	}
	if o.Write {
		r.action = "unchanged"
		written, e := writeRecordExclusive(root, r.Path, r.Canonical)
		r.Written = written
		if written {
			r.action = "write"
		}
		if e != nil {
			code := "output-exists"
			if !errors.Is(e, errOutputExists) {
				code = "write-failed"
			}
			if errors.Is(e, errRecordWriteUnsupported) {
				code = "unsupported-backend"
			}
			return recordFailure(r, code, e.Error())
		}
	}
	return r
}
func assembleRecord(ctx context.Context, o RecordOptions, source planSource, r RecordResult) RecordResult {
	c, ff, code, e := planContract(source, o.Product)
	if e != nil {
		return recordReadFailure(r, ctx, e)
	}
	if code != "" {
		return recordFailure(r, code, "cannot select release contract at C")
	}
	if len(ff) > 0 || c == nil {
		return recordFailure(r, "record-invalid", "contract at C is invalid")
	}
	scheme, e := CompileVersionScheme(c.Version)
	if e != nil || !scheme.Valid(o.Version) {
		return recordFailure(r, "record-invalid", "version does not match contract")
	}
	tag, e := scheme.Tag(o.Version)
	if e != nil {
		return recordFailure(r, "record-invalid", e.Error())
	}
	r.Path = recordPath(c.Project.Product, o.Version)
	configHash, e := contractBlobHash(source, c.Project.Product)
	if e != nil {
		return recordReadFailure(r, ctx, e)
	}
	raw, e := readArtifactBytes(filepath.Join(o.From, "build-manifest.json"), MaxReleaseDocumentBytes)
	if e != nil {
		return recordReadFailure(r, ctx, e)
	}
	doc, ff := ParseReleaseDocument(raw)
	if len(ff) > 0 {
		return recordFailure(r, ff[0].Code, ff[0].Message)
	}
	manifest, ok := doc.(*BuildManifest)
	if !ok {
		return recordFailure(r, "record-invalid", "expected build manifest")
	}
	if manifest.Commit != o.Commit || manifest.Version != o.Version || manifest.ConfigSHA256 != configHash || manifest.Project != c.Project.Name || manifest.Product != c.Project.Product {
		return recordFailure(r, "artifact-mismatch", "manifest commit, version, contract hash or product differs from C")
	}
	if o.Repo != "" && o.Repo != manifest.Repository || (o.RunIDSet || o.RunID != 0) && o.RunID != manifest.Run.RunID || (o.RunAttemptSet || o.RunAttempt != 0) && o.RunAttempt != manifest.Run.RunAttempt {
		return recordFailure(r, "artifact-mismatch", "explicit repository or run identity differs from manifest")
	}
	if ff := validateAssetTargets(manifest.Assets, c); len(ff) > 0 {
		return recordFailure(r, "record-invalid", ff[0].Message)
	}
	signer := c.CI.ReleaseWorkflow
	if o.SignerWorkflowFile != "" {
		signer = o.SignerWorkflowFile
	}
	if !ValidRepoPath(signer) {
		return recordFailure(r, "record-invalid", "signer workflow path is invalid")
	}
	sumsRaw, e := readArtifactBytes(filepath.Join(o.From, c.Outputs.Checksums), 65536)
	if e != nil {
		return recordReadFailure(r, ctx, e)
	}
	sums, e := parseChecksums(sumsRaw)
	if errors.Is(e, errReleaseLimit) {
		return recordReadFailure(r, ctx, e)
	}
	if e != nil {
		return recordFailure(r, "artifact-mismatch", e.Error())
	}
	record := ReleaseRecord{SchemaVersion: 1, Kind: "release-record", Project: c.Project.Name, Product: c.Project.Product, Version: o.Version, Tag: tag, Commit: o.Commit, Repository: manifest.Repository, SourceDateEpoch: manifest.SourceDateEpoch, Build: RecordBuild{manifest.Run.Workflow, manifest.Run.RunID, manifest.Run.RunAttempt, manifest.STWVersion}, Assets: manifest.Assets, Checksums: FileRef{c.Outputs.Checksums, hashBytes(sumsRaw), int64(len(sumsRaw))}, Manifest: FileRef{"build-manifest.json", hashBytes(raw), int64(len(raw))}, Attestation: RecordAttestation{"github-artifact-attestation", manifest.Repository + "/" + signer, "https://slsa.dev/provenance/v1"}}
	for i := range record.Assets {
		if a := record.Assets[i].File; a != nil {
			a.URLTemplate = c.Outputs.URLTemplate
		} else if a := record.Assets[i].Image; a != nil {
			a.Tags = []string{o.Version}
			for _, t := range c.Target {
				if t.ID != a.Target {
					continue
				}
				a.Repository = t.Repository
				for _, p := range []struct {
					path string
					dest **PathSHA
				}{{t.LockFile, &a.Lock}, {t.ToolListFile, &a.ToolList}} {
					if p.path == "" {
						*p.dest = nil
						continue
					}
					hash, e := committedHash(ctx, source, p.path)
					if e != nil {
						return recordReadFailure(r, ctx, e)
					}
					*p.dest = &PathSHA{p.path, hash}
				}
			}
		}
	}
	if !checksumCoverage(record, sums) {
		return recordFailure(r, "artifact-mismatch", "checksum coverage differs from assets and manifest")
	}
	_, g, e := inspectArtifactsContext(ctx, o.From, recordFiles(record))
	if e != nil {
		return recordReadFailure(r, ctx, e)
	}
	for _, gate := range g {
		if gate.Outcome == "fail" {
			return recordFailure(r, "artifact-mismatch", gate.Code)
		}
	}
	if ff := ValidateRecord(record, RecordContext{Contract: c, Repository: manifest.Repository, SignerWorkflowFile: o.SignerWorkflowFile, Checksums: sums}); len(ff) > 0 {
		return recordFailure(r, "record-invalid", ff[0].Message)
	}
	canonical, e := EncodeReleaseDocument(record)
	if e != nil {
		return recordFailure(r, "record-invalid", e.Error())
	}
	r.Status = "ok"
	r.Record = &record
	r.Canonical = canonical
	r.CanonicalSHA256 = hashBytes(canonical)
	return r
}
func hashBytes(raw []byte) string { return fmt.Sprintf("%x", sha256.Sum256(raw)) }
func contractBlobHash(source planSource, product string) (string, error) {
	entries, cut, e := source.StwEntries()
	if e != nil {
		return "", e
	}
	if cut {
		return "", errReleaseLimit
	}
	names := []string{}
	byName := map[string]sourceEntry{}
	for _, entry := range entries {
		names = append(names, entry.Name)
		byName[entry.Name] = entry
	}
	files, _ := DiscoverProducts(names)
	selected, code := SelectProduct(files, product, true)
	if code != "" || len(selected) != 1 {
		return "", fmt.Errorf("contract selection unavailable")
	}
	entry := byName[strings.TrimPrefix(selected[0].Path, ".stw/")]
	raw, cut, e := source.Read(entry, MaxContractBytes)
	if e != nil {
		return "", e
	}
	if cut {
		return "", errReleaseLimit
	}
	return hashBytes(raw), nil
}
func committedHash(ctx context.Context, source planSource, path string) (string, error) {
	if j, ok := source.(jjSource); ok {
		return j.reader.Hash(path)
	}
	e, _, err := source.pathEntry(path)
	if err != nil {
		return "", err
	}
	if e.Kind != "file" {
		return "", fmt.Errorf("lock or tool-list must be a regular committed file")
	}
	if g, ok := source.(*gitSource); ok {
		return gitHashBlob(ctx, g.root, e.id)
	}
	return "", fmt.Errorf("committed-source hash adapter unavailable")
}
func (r RecordResult) Text() string {
	if r.Error != nil {
		return fmt.Sprintf("release record %s: %s: %s\n", r.Status, r.Error.Code, r.Error.Message)
	}
	if r.action == "print" || r.action == "" {
		return string(r.Canonical)
	}
	effects := "read-only; no network or project command"
	result := "checked equal"
	if r.action == "write" {
		effects = "wrote one new file; no staging, commit, tag or network"
		result = "wrote " + r.Path
	}
	if r.action == "unchanged" {
		result = "identical file unchanged: " + r.Path
	}
	if r.action == "write" && r.Backend == "jj" {
		effects += "; the next jj command snapshots this new file into the working-copy commit"
	}
	return fmt.Sprintf("SourceSteward release record: %s %s (%d assets)\neffects: %s\n%s\n", r.Record.Commit, r.Record.Version, len(r.Record.Assets), effects, result)
}
