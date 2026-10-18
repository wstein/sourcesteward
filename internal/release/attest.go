// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/wstein/sourcesteward/internal/hosting"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

// The JSON envelope below is synthetic until the authorized release rehearsal
// measures GitHub CLI's actual output. Unknown/missing shapes always fail closed.
func attestationDigest(raw []byte, want string) error {
	if len(raw) > MaxReleaseDocumentBytes || !utf8.Valid(raw) {
		return errors.New("attestation output exceeds bounds")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	value, err := decodeJSONValue(dec, "", 0)
	if err != nil {
		return errors.New("invalid attestation JSON")
	}
	if _, err = dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing attestation JSON")
	}
	rows, ok := value.([]any)
	if !ok || len(rows) == 0 {
		return errors.New("missing verified attestation")
	}
	found := false
	for _, row := range rows {
		statement := asMap(asMap(asMap(row)["verificationResult"])["statement"])
		subjects, ok := statement["subject"].([]any)
		if !ok || len(subjects) == 0 {
			return errors.New("missing verified attestation subject")
		}
		for _, subject := range subjects {
			s := asMap(subject)
			digest := asMap(s["digest"])
			if getString(digest, "sha256") == want {
				found = true
			}
		}
	}
	if !found {
		return errors.New("verified subject digest differs from record")
	}
	return nil
}

type attestBuffer struct {
	data     bytes.Buffer
	overflow bool
}

func (b *attestBuffer) Write(p []byte) (int, error) {
	n := len(p)
	left := MaxReleaseDocumentBytes - b.data.Len()
	if len(p) > left {
		p = p[:left]
		b.overflow = true
	}
	b.data.Write(p)
	return n, nil
}
func verifyAttestation(parent context.Context, target, repo, signer, source, subject, digest, bundle string) error {
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	exe, dir, env, cleanup, err := hosting.PrepareGHProcess(ctx, target, "github.com")
	if err != nil {
		return errors.New("attestation verifier unavailable")
	}
	defer cleanup()
	var subjectFile *os.File
	if !strings.HasPrefix(subject, "oci://") {
		subjectFile, err = openAttestationSubject(ctx, subject, digest)
		if err != nil {
			return err
		}
		defer subjectFile.Close()
		subject = "/dev/fd/3"
	}
	args := []string{"attestation", "verify", subject, "--repo", repo, "--signer-workflow", signer, "--source-ref", source, "--deny-self-hosted-runners", "--format", "json"}
	if bundle != "" {
		args = append(args, "--bundle", bundle)
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	if subjectFile != nil {
		cmd.ExtraFiles = []*os.File{subjectFile}
	}
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = strings.NewReader("")
	cmd.WaitDelay = time.Second
	var out, stderr attestBuffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err = cmd.Run()
	if ctx.Err() != nil {
		return errors.New("attestation verification canceled or exceeded sixty seconds")
	}
	if out.overflow || stderr.overflow {
		return errReleaseLimit
	}
	if err != nil {
		return errors.New("attestation verification failed; missing or invalid evidence")
	}
	if subjectFile != nil {
		if err := validateAttestationDescriptor(ctx, subjectFile, digest); err != nil {
			return err
		}
	}
	return attestationDigest(out.data.Bytes(), digest)
}
func verifyReleaseAttestations(ctx context.Context, o VerifyOptions, r ReleaseRecord, c *Contract) ([]Gate, []AttestationObservation, error) {
	gates := []Gate{gate("attestation-missing", "pass", "", ""), gate("attestation-invalid", "pass", "", "")}
	observations := []AttestationObservation{}
	if c == nil || o.Artifacts == "" {
		for i := range gates {
			gates[i] = gate(gates[i].Code, "not-run", "artifacts-absent", "")
		}
		return gates, observations, nil
	}
	resolved := hosting.Inspect(ctx, o.Path, hosting.Options{Kind: "repo", Repo: o.Repo})
	if resolved.Status != "ok" || resolved.Repository == nil {
		return nil, nil, errors.New("attestation repository resolution failed")
	}
	repo := resolved.Repository.Owner + "/" + resolved.Repository.Name
	signer := c.CI.ReleaseWorkflow
	if o.SignerWorkflowFile != "" {
		signer = o.SignerWorkflowFile
	}
	signer = repo + "/" + signer
	source := "refs/heads/main"
	if o.SourceRef != "" {
		source = o.SourceRef
	}
	subjects := []struct{ name, path, digest string }{}
	for _, f := range recordFiles(r) {
		if !assetName(f.Name) {
			return nil, nil, errors.New("invalid attestation subject name")
		}
		path, err := filepath.Abs(filepath.Join(o.Artifacts, f.Name))
		if err != nil {
			return nil, nil, errors.New("cannot resolve attestation subject")
		}
		subjects = append(subjects, struct{ name, path, digest string }{f.Name, path, f.SHA256})
	}
	for _, asset := range r.Assets {
		if asset.Image != nil {
			a := asset.Image
			subjects = append(subjects, struct{ name, path, digest string }{a.Repository + "@" + a.IndexDigest, "oci://" + a.Repository + "@" + a.IndexDigest, strings.TrimPrefix(a.IndexDigest, "sha256:")})
		}
	}
	manifestOK := false
	for _, s := range subjects {
		var err error
		if !strings.HasPrefix(s.path, "oci://") {
			actual, e := hashArtifact(ctx, s.path)
			if e != nil || actual.SHA256 != s.digest {
				err = errors.New("attestation subject file unavailable or different")
			}
		}
		if err == nil {
			err = verifyAttestation(ctx, o.Path, repo, signer, source, s.path, s.digest, "")
		}
		if errors.Is(err, errReleaseLimit) {
			return gates, observations, err
		}
		outcome := "pass"
		if err != nil {
			outcome = "fail"
			gates[0] = gate("attestation-missing", "fail", "verified-evidence-unavailable", s.name)
			gates[1] = gate("attestation-invalid", "fail", "verification-failed", s.name)
		} else if s.name == r.Manifest.Name {
			manifestOK = true
		}
		observations = append(observations, AttestationObservation{Subject: Escape(s.name), Outcome: outcome, SignerWorkflow: Escape(signer)})
		if ctx.Err() != nil {
			return gates, observations, ctx.Err()
		}
	}
	if manifestOK {
		raw, err := readArtifactBytes(filepath.Join(o.Artifacts, r.Manifest.Name), MaxReleaseDocumentBytes)
		if err != nil {
			gates[1] = gate("attestation-invalid", "fail", "manifest-unavailable", "")
		} else {
			doc, ff := ParseReleaseDocument(raw)
			m, ok := doc.(*BuildManifest)
			if fmt.Sprintf("%x", sha256.Sum256(raw)) != r.Manifest.SHA256 || !ok || len(ff) > 0 || m.Commit != r.Commit || m.Version != r.Version || !reflect.DeepEqual(m.Assets, r.Assets) {
				gates[1] = gate("attestation-invalid", "fail", "manifest-mismatch", "attested manifest must bind the build commit, version and asset digests")
			}
		}
	}
	return gates, observations, nil
}

// Keep the no-follow regular descriptor pinned across gh execution. The child
// reads inherited fd3 through the operating system's /dev/fd interface, avoiding
// reopening a repository-controlled path and creating no artifact copy on disk.
func openAttestationSubject(ctx context.Context, subject, digest string) (*os.File, error) {
	f, err := openRegular(subject)
	if err != nil {
		return nil, errors.New("attestation file must be a regular no-follow subject")
	}
	fail := func(err error) (*os.File, error) { f.Close(); return nil, err }
	if err := validateAttestationDescriptor(ctx, f, digest); err != nil {
		return fail(err)
	}
	return f, nil
}
func validateAttestationDescriptor(ctx context.Context, f *os.File, digest string) error {
	before, err := f.Stat()
	if err != nil {
		return errors.New("cannot inspect attestation subject")
	}
	if !before.Mode().IsRegular() {
		return errors.New("attestation subject descriptor must be a regular file")
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if before.Size() > maxArtifactBytes {
		return errReleaseLimit
	}
	h := sha256.New()
	buf := make([]byte, 64<<10)
	var size int64
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, e := f.Read(buf)
		size += int64(n)
		if size > maxArtifactBytes {
			return errReleaseLimit
		}
		h.Write(buf[:n])
		if e == io.EOF {
			break
		}
		if e != nil {
			return errors.New("cannot read attestation subject")
		}
	}
	after, err := f.Stat()
	if err != nil || before.Size() != size || after.Size() != size || !before.ModTime().Equal(after.ModTime()) || fmt.Sprintf("%x", h.Sum(nil)) != digest {
		return errors.New("attestation subject differs from record or changed while reading")
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return errors.New("cannot rewind attestation subject")
	}
	return nil
}
