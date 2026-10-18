// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/hosting"
)

// DispatchInputs is the exact workflow input shape, including empty unused
// selectors and the empty product for a single-product contract.
type DispatchInputs struct {
	Mode    string `json:"mode"`
	Product string `json:"product"`
	Commit  string `json:"commit"`
	Tag     string `json:"tag"`
	Version string `json:"version"`
}

// DispatchPlan is the complete stateless dispatch identity. Its main commit is
// the remote value; the caller must first prove that its ancestry gate used the
// same value. Construction and hashing perform no reads, writes or authorization.
type DispatchPlan struct {
	Repository     string         `json:"repository"`
	Host           string         `json:"host"`
	Workflow       string         `json:"workflow"`
	Ref            string         `json:"ref"`
	Inputs         DispatchInputs `json:"inputs"`
	ResolvedCommit string         `json:"resolved_commit"`
	MainCommit     string         `json:"main_commit"`
	ConfigSHA256   string         `json:"config_sha256"`
	Verdict        string         `json:"verdict"`
}

var dispatchWorkflow = regexp.MustCompile(`\A[A-Za-z0-9_.-][A-Za-z0-9_.-]*\.ya?ml\z`)
var dispatchProduct = regexp.MustCompile(`\A[a-z0-9][a-z0-9-]{0,31}\z`)

// CanonicalJSON returns the record canonical form of the complete identity.
// Invalid identities return no bytes. Scheme validation, native resolution,
// annotation, ancestry and fresh online gate evaluation belong to the caller.
func (p DispatchPlan) CanonicalJSON() ([]byte, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	return canonicalValue(p)
}

// ID hashes canonical bytes, including their trailing newline. It neither stores
// a plan nor grants approval to run a build; a later apply must recompute it.
func (p DispatchPlan) ID() (string, error) {
	raw, err := p.CanonicalJSON()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func (p DispatchPlan) validate() error {
	if p.Repository == "" || p.Host == "" {
		return fmt.Errorf("dispatch requires repository and host")
	}
	if err := hosting.ValidateOptions(hosting.Options{Kind: "repo", Repo: p.Repository, Host: p.Host}); err != nil {
		return fmt.Errorf("invalid dispatch destination: %w", err)
	}
	if !dispatchWorkflow.MatchString(p.Workflow) {
		return fmt.Errorf("dispatch workflow must be a YAML file name")
	}
	if p.Ref != "main" {
		return fmt.Errorf("dispatch ref must be main")
	}
	if p.Inputs.Product != "" && !dispatchProduct.MatchString(p.Inputs.Product) {
		return fmt.Errorf("invalid dispatch product")
	}
	if !dispatchText(p.Inputs.Version) {
		return fmt.Errorf("invalid dispatch version")
	}
	switch p.Inputs.Mode {
	case "commit":
		if !IsFullCommitID(p.Inputs.Commit) || p.Inputs.Tag != "" {
			return fmt.Errorf("commit mode requires one full commit and no tag")
		}
	case "tag":
		if !dispatchText(p.Inputs.Tag) || p.Inputs.Commit != "" {
			return fmt.Errorf("tag mode requires one tag and no commit")
		}
	default:
		return fmt.Errorf("dispatch mode must be commit or tag")
	}
	if !IsFullCommitID(p.ResolvedCommit) || !IsFullCommitID(p.MainCommit) {
		return fmt.Errorf("dispatch requires full resolved and remote main commits")
	}
	if len(p.ConfigSHA256) != 64 || !IsFullCommitID(p.ConfigSHA256) {
		return fmt.Errorf("dispatch requires a lowercase SHA-256 contract digest")
	}
	switch p.Verdict {
	case "pass", "fail", "incomplete":
	default:
		return fmt.Errorf("invalid dispatch verdict")
	}
	return nil
}

func dispatchText(s string) bool {
	return s != "" && utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
}
