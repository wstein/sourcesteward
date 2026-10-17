// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"fmt"
	"strings"
)

func plural(n int, singular string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %ss", n, singular)
}

// Text renders the human form of the result. It is derived from the same data as the JSON form;
// every string was escaped when the result was finalized.
func (r CheckResult) Text() string {
	var b strings.Builder
	where := r.Root
	if where == "" {
		where = r.InputPath
	}
	backend := r.Backend
	if r.Commit != "" {
		backend += ", commit " + r.Commit
	}
	fmt.Fprintf(&b, "SourceSteward release check: %s (%s)\n", where, backend)
	b.WriteString("effects: read-only, no network, no project command\n")
	for _, p := range r.Products {
		if p.Contract == nil {
			fmt.Fprintf(&b, "contract: %s (not valid)\n", p.File)
			continue
		}
		c := p.Contract
		product := ""
		if c.Project.Product != "" {
			product = "product " + c.Project.Product + ", "
		}
		fmt.Fprintf(&b, "contract: %s (%sschema %d, scheme %s, %s, %s)\n", p.File, product, c.SchemaVersion, c.Version.Scheme, plural(len(c.Target), "target"), plural(len(c.Build), "build"))
	}
	if r.Error != nil {
		fmt.Fprintf(&b, "%s: %s: %s\n", r.Status, r.Error.Code, r.Error.Message)
		return b.String()
	}
	if len(r.Findings) == 0 {
		b.WriteString("findings: none\n")
		return b.String()
	}
	var counts []string
	if r.Summary.Errors > 0 {
		counts = append(counts, plural(r.Summary.Errors, "error"))
	}
	if r.Summary.Infos > 0 {
		counts = append(counts, fmt.Sprintf("%d info", r.Summary.Infos))
	}
	fmt.Fprintf(&b, "findings: %s\n", strings.Join(counts, ", "))
	for _, f := range r.Findings {
		pointer := f.Pointer
		if pointer == "" {
			pointer = "(document)"
		}
		if f.File != "" && (len(r.Products) != 1 || r.Products[0].File != f.File) {
			pointer = f.File + " " + pointer
		}
		fmt.Fprintf(&b, "  %-5s  %s  %s: %s\n", f.Severity, pointer, f.Code, f.Message)
	}
	return b.String()
}
