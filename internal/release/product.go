// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// MaxProducts is the number of contract files a repository may have (spec limits table).
const MaxProducts = 64

// ContractFile is one contract file under .stw/: the default product (Product "") or a named one.
type ContractFile struct {
	Product string
	Path    string // repository path, for example .stw/release.zlib.toml
	Format  string // "toml" or "json"
}

var (
	contractFileName = regexp.MustCompile(`^release(?:\.(.*))?\.(toml|json|yaml|yml)$`)
	productID        = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
)

// DiscoverProducts maps the entry names of the .stw directory to contract files. It reports
// YAML files (never accepted), invalid product ids in file names, a default file mixed with
// named files, two files for one product (config-ambiguous) and more than 64 files.
func DiscoverProducts(names []string) ([]ContractFile, []Finding) {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	var findings []Finding
	at := func(code, path, message string, products ...string) {
		f := errorFinding(code, "", message)
		f.File = path
		for _, p := range products {
			f.scope += "|" + p + "|"
		}
		findings = append(findings, f)
	}
	byProduct := map[string]ContractFile{}
	for _, name := range sorted {
		index := contractFileName.FindStringSubmatchIndex(name)
		if index == nil {
			continue
		}
		path := ".stw/" + name
		format := name[index[4]:index[5]]
		product, named := "", index[2] >= 0
		if named {
			product = name[index[2]:index[3]]
		}
		switch {
		case format == "yaml" || format == "yml":
			at("profile-violation", path, "YAML contract files are not accepted; use TOML (.stw/release.toml) or JSON", product)
			continue
		case named && !productID.MatchString(product):
			at("schema-violation", path, "the product id in the file name must match ^[a-z0-9][a-z0-9-]{0,31}$", product)
			continue
		}
		file := ContractFile{Product: product, Path: path, Format: format}
		if kept, dup := byProduct[product]; dup {
			// TOML is the file kept for review; the other form is the ambiguity.
			if format == "toml" {
				kept, file = file, kept
			}
			at("config-ambiguous", file.Path, fmt.Sprintf("%s and %s both define the same product; keep one", kept.Path, file.Path), product)
			file = kept
		}
		byProduct[product] = file
	}
	products := make([]string, 0, len(byProduct))
	for p := range byProduct {
		products = append(products, p)
	}
	sort.Strings(products)
	files := make([]ContractFile, 0, len(products))
	for _, p := range products {
		files = append(files, byProduct[p])
	}
	if def, ok := byProduct[""]; ok && len(files) > 1 {
		at("config-ambiguous", def.Path, "the default contract "+def.Path+" cannot be mixed with named product files .stw/release.<product>.*", namedProducts(files)...)
	}
	if len(files) > MaxProducts {
		at("limit-exceeded", "", fmt.Sprintf("more than %d contract files; only the first %d are read", MaxProducts, MaxProducts))
		files = files[:MaxProducts]
	}
	sortFindings(findings)
	return files, findings
}

func namedProducts(files []ContractFile) []string {
	var out []string
	for _, f := range files {
		if f.Product != "" {
			out = append(out, f.Product)
		}
	}
	return out
}

// DiscoveryFindingsFor keeps the discovery findings about one product: those about its files and
// those about mixing the default file with named ones when that named product is part of the mix.
func DiscoveryFindingsFor(findings []Finding, product string) []Finding {
	var kept []Finding
	for _, f := range findings {
		if f.scope == "" || strings.Contains(f.scope, "|"+product+"|") {
			kept = append(kept, f)
		}
	}
	return kept
}

// SelectProduct narrows the contract files by --product. An empty name is absent. When a
// product is required and several exist the result is product-required; an unknown name is
// product-unknown (the default product has no name and cannot be selected by one).
func SelectProduct(files []ContractFile, product string, required bool) ([]ContractFile, string) {
	if product != "" {
		for _, f := range files {
			if f.Product == product {
				return []ContractFile{f}, ""
			}
		}
		return nil, "product-unknown"
	}
	if required && len(files) > 1 {
		return nil, "product-required"
	}
	return files, ""
}

const (
	semverBody = `v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})`
	calverBody = `([0-9]{4})\.(0[1-9]|1[0-2])\.(0[1-9]|[12][0-9]|3[01])(?:-b([1-9][0-9]{0,8})(?:\.([1-9][0-9]{0,8}))?)?`
)

// tagGrammar returns the expression a whole tag of the product must match, or nil.
func tagGrammar(c *Contract) *regexp.Regexp {
	var expression string
	switch c.Version.Scheme {
	case "semver":
		expression = `\A` + regexp.QuoteMeta(c.Version.TagPrefix) + semverBody + `\z`
	case "calver":
		expression = `\A` + regexp.QuoteMeta(c.Version.TagPrefix) + calverBody + `\z`
	case "pattern":
		expression = c.Version.TagPattern
	default:
		return nil
	}
	re, err := regexp.Compile(expression)
	if err != nil {
		return nil
	}
	return re
}

// ProductForTag returns the index of the single product whose tag grammar fully matches the
// tag: product-unknown when none does, product-required when several do.
func ProductForTag(contracts []*Contract, tag string) (int, string) {
	found := -1
	for i, c := range contracts {
		re := tagGrammar(c)
		if re == nil || !re.MatchString(tag) {
			continue
		}
		if found >= 0 {
			return -1, "product-required"
		}
		found = i
	}
	if found < 0 {
		return -1, "product-unknown"
	}
	return found, ""
}

// ProductTagOverlaps reports product pairs whose literal tag prefixes are equal or one extends
// the other, so a tag could belong to either. paths names the contract file of each product.
func ProductTagOverlaps(contracts []*Contract, paths []string) []Finding {
	var findings []Finding
	for j := 1; j < len(contracts); j++ {
		pj, okJ := contracts[j].TagPrefixLiteral()
		for i := 0; i < j && okJ; i++ {
			pi, okI := contracts[i].TagPrefixLiteral()
			if okI && (strings.HasPrefix(pi, pj) || strings.HasPrefix(pj, pi)) {
				f := errorFinding("product-tag-overlap", "/version", fmt.Sprintf("the tag prefix %q overlaps %q of the product in %s", clip(pj), clip(pi), clip(paths[i])))
				f.File = paths[j]
				findings = append(findings, f)
				break
			}
		}
	}
	return findings
}
