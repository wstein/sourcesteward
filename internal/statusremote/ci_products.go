// SPDX-License-Identifier: EUPL-1.2
package statusremote

import (
	"path"
	"strings"

	"github.com/wstein/sourcesteward/internal/release"
)

// defaultReleaseWorkflow is the release workflow file of a pattern that no contract declares: the name
// the member repositories use.
const defaultReleaseWorkflow = "release.yml"

// ciProduct is one product the repository declares: its id (the file name part of
// `.stw/release.PRODUCT.toml`, empty for the default contract), the release tag pattern of its tag
// grammar and its release workflow file.
type ciProduct struct {
	id       string
	pattern  string
	workflow string
}

// ReleaseProducts is what the release contracts of a clone declare. The zero value declares nothing:
// the default pattern and the default workflow apply.
type ReleaseProducts struct {
	products []ciProduct
	invalid  bool
}

// ReadReleaseProducts reads the release contracts of the clone at dir with the reader of
// `release check`. No `.stw` directory (or no clone: dir empty) declares nothing. A contract that
// exists but cannot be read or is not valid makes the result Invalid: the defaults are not a
// substitute for a contract the owner meant. Only semver and pattern tag grammars give a pattern
// (Spec 037 compares semantic versions); a product of another scheme declares none.
func ReadReleaseProducts(dir string) ReleaseProducts {
	if dir == "" {
		return ReleaseProducts{}
	}
	found, err := release.ReadWorktreeContracts(dir)
	if err != nil || !found.Valid() {
		return ReleaseProducts{invalid: true}
	}
	var out ReleaseProducts
	for _, p := range found.Products {
		if pattern := tagPattern(p.Contract.Version); pattern != "" {
			out.products = append(out.products, ciProduct{id: p.Product, pattern: pattern, workflow: path.Base(p.Contract.CI.ReleaseWorkflow)})
		}
	}
	return out
}

// Invalid reports that a contract exists but cannot be used.
func (p ReleaseProducts) Invalid() bool { return p.invalid }

// Patterns lists the release tag patterns of the declared products in the order of their ids; none
// means the default pattern.
func (p ReleaseProducts) Patterns() []string {
	var out []string
	for _, prod := range p.products {
		out = append(out, prod.pattern)
	}
	return out
}

// of returns the product id and the release workflow of a tag pattern: the declaring product's, or no
// product and the default workflow.
func (p ReleaseProducts) of(pattern string) (id, workflow string) {
	for _, prod := range p.products {
		if prod.pattern == pattern {
			return prod.id, prod.workflow
		}
	}
	return "", defaultReleaseWorkflow
}

// tagPattern is the glob of the tags of a version scheme, `*` standing for the version, or "" for a
// scheme that is not a semantic version.
func tagPattern(v release.Version) string {
	switch v.Scheme {
	case "semver":
		return v.TagPrefix + "v*"
	case "pattern":
		return strings.Replace(v.TagTemplate, "{version}", "*", 1)
	}
	return ""
}
