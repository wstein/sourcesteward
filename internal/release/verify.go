// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type VerifyOptions struct {
	PlanOptions
	Artifacts, SignerWorkflowFile, SourceRef string
	Published                                bool
}
type AttestationObservation struct {
	Subject        string `json:"subject"`
	Outcome        string `json:"outcome"`
	SignerWorkflow string `json:"signer_workflow"`
	RunURL         string `json:"run_url"`
}
type VerifyResult struct {
	PlanResult
	Artifacts    []ArtifactObservation    `json:"artifacts"`
	Attestations []AttestationObservation `json:"attestations"`
	TagCommit    *string                  `json:"tag_commit"`
	Diff         DiffResult               `json:"diff"`
}

// OnlineVerifier is the explicit read-only evidence seam. WI5 supplies the bounded online
// implementation; offline verification never invokes it or acquires credentials.
type OnlineVerifier interface {
	VerifyRelease(context.Context, VerifyOptions, ReleaseRecord, *Contract) ([]Gate, []AttestationObservation, error)
}

func Verify(ctx context.Context, o VerifyOptions) VerifyResult { return VerifyWithOnline(ctx, o, nil) }
func VerifyWithOnline(ctx context.Context, o VerifyOptions, online OnlineVerifier) VerifyResult {
	opts := o.PlanOptions
	opts.Mode = "publish"
	opts.signerWorkflowFile = o.SignerWorkflowFile
	opts.Online = false
	p := Plan(ctx, opts)
	r := VerifyResult{PlanResult: p, Artifacts: []ArtifactObservation{}, Attestations: []AttestationObservation{}, TagCommit: p.Subject.TagCommit, Diff: p.diff}
	r.Command = "release-verify"
	if r.Diff.Offending == nil {
		r.Diff.Offending = []string{}
	}
	if r.Status != "ok" {
		if r.Error != nil && r.Error.Code == "unsupported-backend" {
			r.Error.Message = "release verify requires a Git-owned checkout or the authoritative CI run; plan <commit> works in jj"
		}
		return r
	}
	filtered := []Gate{}
	for _, g := range r.Gates {
		if !strings.HasPrefix(g.Code, "artifact-") && !strings.HasPrefix(g.Code, "attestation-") {
			filtered = append(filtered, g)
		}
	}
	r.Gates = filtered
	if o.Published {
		for i := range r.Gates {
			switch r.Gates[i].Code {
			case "already-published", "draft-missing", "version-not-monotonic":
				r.Gates[i] = gate(r.Gates[i].Code, "not-applicable", "published-audit", "")
			}
		}
	}
	if o.Artifacts == "" || p.record == nil {
		reason := "artifacts-absent"
		if p.record == nil {
			reason = "depends-on-record-invalid"
		}
		for _, code := range []string{"artifact-missing", "artifact-unexpected", "artifact-mismatch"} {
			r.Gates = append(r.Gates, gate(code, "not-run", reason, ""))
		}
	} else {
		refs := recordFiles(*p.record)
		observations, gates, e := inspectArtifactsContext(ctx, o.Artifacts, refs)
		if e != nil {
			return verifyReadFailure(r, ctx, e)
		}
		r.Artifacts = observations
		r.Gates = append(r.Gates, gates...)
		data, e := readArtifactBytes(filepath.Join(o.Artifacts, p.record.Checksums.Name), 65536)
		if errors.Is(e, errReleaseLimit) {
			return verifyReadFailure(r, ctx, e)
		}
		sums, parseErr := parseChecksums(data)
		if errors.Is(parseErr, errReleaseLimit) {
			return verifyReadFailure(r, ctx, parseErr)
		}
		if e != nil || parseErr != nil || !checksumCoverage(*p.record, sums) {
			for i := range r.Gates {
				if r.Gates[i].Code == "artifact-mismatch" {
					r.Gates[i] = gate("artifact-mismatch", "fail", "checksums-invalid", "checksum file must cover exactly file assets and manifest")
				}
			}
		}
	}
	if o.Online && online != nil && p.record != nil {
		g, a, e := online.VerifyRelease(ctx, o, *p.record, p.contract)
		if e != nil {
			return verifyReadFailure(r, ctx, e)
		}
		r.Attestations = a
		for _, newGate := range g {
			replaced := false
			for i := range r.Gates {
				if r.Gates[i].Code == newGate.Code {
					r.Gates[i] = newGate
					replaced = true
				}
			}
			if !replaced {
				r.Gates = append(r.Gates, newGate)
			}
		}
	} else {
		for _, code := range []string{"attestation-missing", "attestation-invalid"} {
			r.Gates = append(r.Gates, gate(code, "not-run", "needs-online", ""))
		}
	}
	r.Verdict = Verdict(r.Gates)
	return r
}
func recordFiles(r ReleaseRecord) []FileRef {
	refs := []FileRef{}
	for _, a := range r.Assets {
		if a.File != nil {
			refs = append(refs, FileRef{a.File.Name, a.File.SHA256, a.File.Size})
		}
	}
	return append(refs, r.Checksums, r.Manifest)
}
func checksumCoverage(r ReleaseRecord, sums map[string]string) bool {
	want := map[string]string{r.Manifest.Name: r.Manifest.SHA256}
	for _, a := range r.Assets {
		if a.File != nil {
			want[a.File.Name] = a.File.SHA256
		}
	}
	if len(want) != len(sums) {
		return false
	}
	for n, h := range want {
		if sums[n] != h {
			return false
		}
	}
	return true
}
func (r VerifyResult) Text() string {
	var b strings.Builder
	b.WriteString(strings.Replace(r.PlanResult.Text(), "SourceSteward release plan:", "SourceSteward release verify:", 1))
	for _, a := range r.Artifacts {
		fmt.Fprintf(&b, "artifact %s: %s (%d bytes, %s)\n", Escape(a.Name), a.Outcome, a.Size, a.SHA256)
	}
	return b.String()
}

func verifyReadFailure(r VerifyResult, ctx context.Context, e error) VerifyResult {
	r.PlanResult = planReadFailure(r.PlanResult, ctx, e)
	if r.Error != nil {
		r.Error.Message = Escape(r.Error.Message)
	}
	return r
}
