// SPDX-License-Identifier: EUPL-1.2
package release

// ProductContract is one release contract file of a working tree with the result of reading it: the
// contract, or nil when the file is not a valid one (Findings says why).
type ProductContract struct {
	Product  string // the product id of the file name, "" for the default contract
	File     string // repository path, for example .stw/release.zlib.toml
	Contract *Contract
	Findings []Finding
}

// WorktreeContracts is what ReadWorktreeContracts found.
type WorktreeContracts struct {
	Products []ProductContract
	// Findings are the discovery findings about the contract files as a whole (an ambiguous or
	// misnamed file, too many files).
	Findings []Finding
}

// Valid reports whether no finding is an error and every contract file read is a valid contract.
func (w WorktreeContracts) Valid() bool {
	for _, f := range w.Findings {
		if f.Severity == severityError {
			return false
		}
	}
	for _, p := range w.Products {
		if p.Contract == nil {
			return false
		}
	}
	return true
}

// ReadWorktreeContracts reads and validates the release contract files of the working tree at root
// (`.stw/release.toml`, `.stw/release.PRODUCT.toml`, or the JSON form) exactly as `release check`
// does: symlinks are not followed and a file that changes while it is opened is refused. A tree
// without a `.stw` directory has no contracts. The error is set when `.stw` or a file cannot be read.
func ReadWorktreeContracts(root string) (WorktreeContracts, error) {
	var out WorktreeContracts
	source := worktreeSource{root}
	entries, truncated, err := source.StwEntries()
	if err != nil {
		return out, err
	}
	if truncated {
		return out, errReleaseLimit
	}
	names := make([]string, 0, len(entries))
	byName := map[string]sourceEntry{}
	for _, e := range entries {
		names = append(names, e.Name)
		byName[e.Name] = e
	}
	files, findings := DiscoverProducts(names)
	out.Findings = findings
	for _, file := range files {
		contract, _, found, err := lintFile(source, file, byName[file.Path[len(".stw/"):]])
		if err != nil {
			return out, err
		}
		for i := range found {
			found[i].File = file.Path
		}
		out.Products = append(out.Products, ProductContract{Product: file.Product, File: file.Path, Contract: contract, Findings: found})
	}
	return out, nil
}
