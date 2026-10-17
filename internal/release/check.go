// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wstein/sourcesteward/internal/inspection"
)

// CheckOptions are the inputs of `stw release check`.
type CheckOptions struct {
	Path    string // PATH argument; "" means the current directory
	Product string // --product; "" is absent
	Commit  string // --commit, a full commit id; "" lints the working tree
}

// CheckResult is the one result both renderers use (contracts/check.md). Unknown properties are
// additive extensions of the published schema.
type CheckResult struct {
	SchemaVersion int              `json:"schema_version"`
	Command       string           `json:"command"`
	Status        string           `json:"status"` // ok, findings, refused, error
	Backend       string           `json:"backend"`
	InputPath     string           `json:"input_path"`
	Root          string           `json:"root,omitempty"`
	Source        string           `json:"source"` // worktree or commit
	Commit        string           `json:"commit,omitempty"`
	Contract      *Contract        `json:"contract"`
	Products      []CheckedProduct `json:"products"`
	Findings      []Finding        `json:"findings"`
	Summary       CheckSummary     `json:"summary"`
	Boundary      CheckBoundary    `json:"boundary"`
	Error         *CheckError      `json:"error"`
}

// CheckedProduct is the outcome for one contract file.
type CheckedProduct struct {
	File         string    `json:"file"`
	Product      string    `json:"product"`
	ConfigSHA256 string    `json:"config_sha256,omitempty"`
	Contract     *Contract `json:"contract"`
}

type CheckSummary struct {
	Errors int `json:"errors"`
	Infos  int `json:"infos"`
}

type CheckBoundary struct {
	Effects  CheckEffects `json:"effects"`
	Limits   CheckLimits  `json:"limits"`
	Complete bool         `json:"complete"`
}

type CheckEffects struct {
	Network         bool   `json:"network"`
	Writes          bool   `json:"writes"`
	Processes       string `json:"processes"`
	ProjectCommands bool   `json:"project_commands"`
}

type CheckLimits struct {
	MaxBytes    int `json:"max_bytes"`
	MaxFindings int `json:"max_findings"`
}

type CheckError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// IsFullCommitID reports whether s is exactly 40 or 64 lowercase hexadecimal characters.
func IsFullCommitID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Check lints the release contract files of the repository that owns Path. It reads the working
// tree or one commit, writes nothing, contacts no network and starts no project command: the
// only processes are the existing ownership detection and, with a commit, fixed Git reads.
func Check(ctx context.Context, opts CheckOptions) CheckResult {
	input := opts.Path
	if input == "" {
		input = "."
	}
	r := CheckResult{SchemaVersion: 1, Command: "release-check", Status: "error", Backend: "unknown", InputPath: input, Source: "worktree",
		Products: []CheckedProduct{}, Findings: []Finding{},
		Boundary: CheckBoundary{Effects: CheckEffects{Processes: "detection"}, Limits: CheckLimits{MaxBytes: MaxContractBytes, MaxFindings: MaxFindings}}}
	if opts.Commit != "" {
		r.Source, r.Commit = "commit", opts.Commit
		r.Boundary.Effects.Processes = "detection+reads"
	}
	finish := func(r CheckResult) CheckResult { return finalizeCheck(r) }
	refuse := func(code, message string) CheckResult {
		r.Status, r.Error = "refused", &CheckError{code, message}
		return finish(r)
	}
	fail := func(err error) CheckResult {
		if ctx.Err() != nil {
			r.Status, r.Error = "error", &CheckError{"canceled", "the check was canceled"}
		} else {
			r.Status, r.Error = "error", &CheckError{"read-failed", err.Error()}
		}
		return finish(r)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	root, backend, owner := detectOwner(ctx, input)
	r.Backend, r.Root = backend, root
	if owner != nil {
		if owner.refused {
			return refuse(owner.code, owner.message)
		}
		r.Status, r.Error = "error", &CheckError{owner.code, owner.message}
		return finish(r)
	}
	var source contractSource = worktreeSource{root}
	if opts.Commit != "" {
		if backend != "git" {
			return refuse("unsupported-backend", fmt.Sprintf("--commit reads a Git commit and is not supported for a %s-managed repository; lint the working-tree file instead", backend))
		}
		git, err := newGitSource(ctx, root, opts.Commit)
		if errors.Is(err, errCommitUnresolved) {
			return refuse("commit-unresolved", "the commit does not name a commit object of this repository")
		}
		if err != nil {
			return fail(err)
		}
		source = git
	}
	entries, truncated, err := source.Entries()
	if errors.Is(err, errStwNotDirectory) {
		return refuse("config-missing", ".stw is not a directory (symlinks are not followed); no release contract found")
	}
	if err != nil {
		return fail(err)
	}
	names := make([]string, 0, len(entries))
	byName := map[string]sourceEntry{}
	for _, e := range entries {
		names = append(names, e.Name)
		byName[e.Name] = e
	}
	files, findings := DiscoverProducts(names)
	if truncated {
		findings = append(findings, errorFinding("limit-exceeded", "", "the .stw directory listing is too large to read"))
	}
	selected, code := SelectProduct(files, opts.Product, false)
	if code != "" {
		return refuse(code, fmt.Sprintf("no contract file for the product %q", clip(opts.Product)))
	}
	if len(files) == 0 && len(findings) == 0 {
		return refuse("config-missing", "no .stw/release.toml or .stw/release.json")
	}
	if opts.Product != "" {
		// Findings about other products' files are not about this product.
		findings = DiscoveryFindingsFor(findings, opts.Product)
	}
	var contracts []*Contract
	var contractFiles []string
	for _, file := range selected {
		product := CheckedProduct{File: file.Path, Product: file.Product}
		entry := byName[strings.TrimPrefix(file.Path, ".stw/")]
		var fileFindings []Finding
		contract, sum, fileFindings, err := lintFile(source, file, entry)
		if err != nil {
			return fail(err)
		}
		product.ConfigSHA256, product.Contract = sum, contract
		for i := range fileFindings {
			fileFindings[i].File = file.Path
		}
		findings = append(findings, fileFindings...)
		r.Products = append(r.Products, product)
		if contract != nil {
			contracts = append(contracts, contract)
			contractFiles = append(contractFiles, file.Path)
		}
	}
	if opts.Product == "" && len(contracts) > 1 {
		findings = append(findings, ProductTagOverlaps(contracts, contractFiles)...)
	}
	r.Findings = findings
	r.Contract = nil
	if len(r.Products) == 1 {
		r.Contract = r.Products[0].Contract
	}
	r.Boundary.Complete = true
	r.Status = "ok"
	return finish(r)
}

// lintFile reads and validates one contract file of the source.
func lintFile(source contractSource, file ContractFile, entry sourceEntry) (*Contract, string, []Finding, error) {
	if entry.Kind != "file" {
		what := map[string]string{"symlink": "a symlink", "dir": "a directory"}[entry.Kind]
		if what == "" {
			what = "another kind of entry"
		}
		return nil, "", []Finding{errorFinding("profile-violation", "", "the contract file must be a regular file, not "+what+"; symlinks are never followed")}, nil
	}
	data, tooLarge, err := source.Read(entry, MaxContractBytes)
	if err != nil {
		if errors.Is(err, errNotStable) || errors.Is(err, os.ErrNotExist) {
			return nil, "", []Finding{errorFinding("profile-violation", "", "the contract file must be a stable regular file; symlinks are never followed ("+err.Error()+")")}, nil
		}
		return nil, "", nil, err
	}
	if tooLarge {
		return nil, "", []Finding{errorFinding("limit-exceeded", "", fmt.Sprintf("contract file exceeds the %d byte limit", MaxContractBytes))}, nil
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	var doc any
	var findings []Finding
	if file.Format == "json" {
		doc, findings = ParseJSON(data)
	} else {
		var table map[string]any
		table, findings = ParseTOML(data)
		if table != nil {
			doc = table
		}
	}
	if len(findings) != 0 {
		return nil, hash, findings, nil
	}
	contract, findings, err := ValidateContract(doc, ValidateOptions{Product: file.Product, Tree: source})
	if err != nil {
		return nil, hash, nil, err
	}
	return contract, hash, findings, nil
}

// owner describes why the repository could not be selected.
type ownerProblem struct {
	refused bool
	code    string
	message string
}

// detectOwner decides the backend by ownership (never by executable availability) and returns
// the repository root. The order is jj, GitButler, ambiguous, bare, not a repository.
func detectOwner(ctx context.Context, path string) (root, backend string, problem *ownerProblem) {
	res := inspection.Inspect(ctx, path)
	backend = res.Backend
	switch {
	case res.Backend == "ambiguous":
		return "", backend, &ownerProblem{true, "ambiguous-backend", "the repository has both jj and GitButler metadata; the backend cannot be chosen"}
	case res.Backend == "gitbutler":
		root = findGitRoot(res.InputPath)
		if root == "" {
			return "", backend, &ownerProblem{true, "not-repository", "cannot find the root of the GitButler-managed repository"}
		}
		return root, backend, nil
	case res.Status == "ok" && res.JJRepository != nil:
		return res.JJRepository.WorkspaceRoot, "jj", nil
	case res.Status == "ok" && res.Repository != nil:
		if res.Repository.Bare || res.Repository.Worktree == nil {
			return "", backend, &ownerProblem{true, "bare-repository", "a bare repository has no working tree to lint; use a checkout or --commit from one"}
		}
		return *res.Repository.Worktree, "git", nil
	}
	code, message := "inspection-failed", "repository inspection failed"
	if len(res.Diagnostics) > 0 {
		code, message = res.Diagnostics[0].Code, res.Diagnostics[0].Message
	}
	return "", backend, &ownerProblem{code == "not-repository", code, message}
}

// findGitRoot walks up from dir to the directory that holds a .git entry.
func findGitRoot(dir string) string {
	for dir != "" {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

// finalizeCheck derives the status, the summary and the escaped text from the collected data.
func finalizeCheck(r CheckResult) CheckResult {
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
	if r.Products == nil {
		r.Products = []CheckedProduct{}
	}
	sortFindings(r.Findings)
	if len(r.Findings) > MaxFindings {
		dropped := len(r.Findings) - MaxFindings
		r.Findings = append(r.Findings[:MaxFindings:MaxFindings], errorFinding("limit-exceeded", "", fmt.Sprintf("%d further findings are not shown", dropped)))
		r.Boundary.Complete = false
	}
	for _, f := range r.Findings {
		switch f.Severity {
		case severityError:
			r.Summary.Errors++
		case severityInfo:
			r.Summary.Infos++
		}
	}
	if r.Status == "ok" && r.Summary.Errors > 0 {
		r.Status = "findings"
	}
	for i := range r.Findings {
		f := &r.Findings[i]
		f.Pointer, f.Message, f.File = Escape(f.Pointer), Escape(f.Message), Escape(f.File)
	}
	for i := range r.Products {
		r.Products[i].File, r.Products[i].Product = Escape(r.Products[i].File), Escape(r.Products[i].Product)
	}
	r.InputPath, r.Root = Escape(r.InputPath), Escape(r.Root)
	if r.Error != nil {
		r.Error.Message = Escape(r.Error.Message)
	}
	return r
}
