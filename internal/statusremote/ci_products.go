// SPDX-License-Identifier: EUPL-1.2
package statusremote

import (
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/wstein/sourcesteward/internal/release"
)

// defaultReleaseWorkflow is the release workflow file of a repository whose contract declares none:
// the name the member repositories use.
const defaultReleaseWorkflow = "release.yml"

// defaultProduct is the product of a repository that declares none: the tag grammar `v*.*.*` (semver
// with an optional `v`) and the default release workflow.
var defaultProduct = func() ciProduct {
	scheme, err := release.CompileVersionScheme(release.Version{
		Scheme:      "pattern",
		TagTemplate: "{version}",
		TagPattern:  `\A(?P<version>v?(?P<major>0|[1-9][0-9]{0,8})\.(?P<minor>0|[1-9][0-9]{0,8})\.(?P<patch>0|[1-9][0-9]{0,8}))\z`,
		Order:       []string{"major", "minor", "patch"},
	})
	if err != nil {
		panic("the default tag scheme does not compile: " + err.Error())
	}
	return ciProduct{scheme: scheme, workflow: defaultReleaseWorkflow}
}()

// ciProduct is one product the repository declares: its id (the file name part of
// `.stw/release.PRODUCT.toml`, empty for the default contract), its tag grammar and its release workflow.
type ciProduct struct {
	id       string
	scheme   *release.VersionScheme
	workflow string
}

// ciProducts reads the release contracts of the clone at dir (Spec 026: `.stw/release.toml` or
// `.stw/release.PRODUCT.toml`, regular files only, never followed through a link). A missing directory,
// a file that is not a valid contract and a version scheme that does not compile declare nothing, so a
// repository without a usable declaration gets the default product. The result is in the order of the
// product ids.
func ciProducts(dir string) []ciProduct {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".stw"))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	files, _ := release.DiscoverProducts(names)
	var products []ciProduct
	for _, file := range files {
		contract := readCIContract(filepath.Join(dir, file.Path), file)
		if contract == nil {
			continue
		}
		scheme, err := release.CompileVersionScheme(contract.Version)
		if err != nil || contract.CI.ReleaseWorkflow == "" {
			continue
		}
		products = append(products, ciProduct{id: file.Product, scheme: scheme, workflow: path.Base(contract.CI.ReleaseWorkflow)})
	}
	return products
}

// readCIContract reads and validates one contract file; nil when it cannot be read or is not valid.
func readCIContract(name string, file release.ContractFile) *release.Contract {
	f, err := os.Open(name)
	if err != nil {
		return nil
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, release.MaxContractBytes+1))
	if err != nil {
		return nil
	}
	var doc any
	var findings []release.Finding
	if file.Format == "json" {
		doc, findings = release.ParseJSON(data)
	} else {
		var table map[string]any
		if table, findings = release.ParseTOML(data); table != nil {
			doc = table
		}
	}
	if len(findings) != 0 {
		return nil
	}
	contract, _, err := release.ValidateContract(doc, release.ValidateOptions{Product: file.Product})
	if err != nil {
		return nil
	}
	return contract
}
