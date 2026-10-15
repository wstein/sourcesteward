// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ValidateInventoryOptions validates usage without accessing repository state.
func ValidateInventoryOptions(o InventoryOptions) error {
	all := []string{o.Query, o.Ref, o.From, o.To, o.Base, o.Revision, o.Anchor}
	all = append(all, o.Remotes...)
	all = append(all, o.Scopes...)
	all = append(all, o.Paths...)
	if len(all) > inventoryRecordLimit {
		return errors.New("too many inventory options")
	}
	for _, s := range all {
		if !utf8.ValidString(s) || strings.ContainsRune(s, 0) || len(s) > outputLimit {
			return errors.New("invalid inventory option encoding or length")
		}
	}
	if o.Anchor != "" && o.Anchor != "workspace" && o.Anchor != "cwd" {
		return errors.New("anchor must be workspace or cwd")
	}
	scopes := map[string]bool{"tracked": true, "staged": true, "disk": true, "stored": true, "untracked": true, "ignored": true}
	for _, s := range o.Scopes {
		if !scopes[s] {
			return errors.New("unsupported inventory scope")
		}
	}
	for _, s := range o.Remotes {
		if s == "" {
			return errors.New("remote must be nonempty")
		}
	}
	for _, s := range o.Paths {
		clean := filepath.Clean(s)
		if s == "" || filepath.IsAbs(s) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return errors.New("literal paths must remain within the workspace")
		}
	}
	switch o.Query {
	case "":
		if o.Ref != "" || o.From != "" || o.To != "" {
			return errors.New("predicate operands require a query")
		}
	case "clean", "dirty":
		if o.Ref != "" || o.From != "" || o.To != "" {
			return errors.New("unrelated predicate operands")
		}
	case "ref-exists":
		if o.Ref == "" || o.From != "" || o.To != "" {
			return errors.New("ref-exists requires only --ref")
		}
	case "ancestor":
		if o.From == "" || o.To == "" || o.Ref != "" {
			return errors.New("ancestor requires --from and --to")
		}
	default:
		return errors.New("unsupported inventory query")
	}
	if o.Query == "ref-exists" || o.Query == "ancestor" {
		if len(o.Scopes) > 0 || len(o.Paths) > 0 || o.Anchor != "" || o.Base != "" || o.Revision != "" || len(o.Remotes) > 0 {
			return errors.New("reference predicates do not accept unrelated inventory scopes or selectors")
		}
	}
	return nil
}
