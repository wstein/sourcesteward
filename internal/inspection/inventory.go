// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

// ValidateInventoryOptions validates usage without accessing repository state.
func ValidateInventoryOptions(o InventoryOptions) error {
	remaining := inventoryRecordLimit - 7
	for _, count := range []int{len(o.Remotes), len(o.Scopes), len(o.Paths)} {
		if count > remaining {
			return errors.New("too many inventory options")
		}
		remaining -= count
	}
	budget := outputLimit
	validate := func(s string) error {
		if len(s) > budget {
			return errors.New("inventory option bytes exceed limit")
		}
		budget -= len(s)
		if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
			return errors.New("invalid inventory option encoding")
		}
		return nil
	}
	for _, s := range []string{o.Query, o.Ref, o.From, o.To, o.Base, o.Revision, o.Anchor} {
		if e := validate(s); e != nil {
			return e
		}
	}
	for _, list := range [][]string{o.Remotes, o.Scopes, o.Paths} {
		for _, s := range list {
			if e := validate(s); e != nil {
				return e
			}
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
		if s == "" || filepath.IsAbs(s) || (o.Anchor != "cwd" && (clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)))) {
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

// Inventory observes selected native repository state without target writes.
func Inventory(ctx context.Context, path string, options InventoryOptions) InventoryResult {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	empty := Result{SchemaVersion: 1, Status: "error", Backend: "unknown", Evidence: []Evidence{}, Diagnostics: []Diagnostic{}}
	r := newInventoryResult(empty)
	if err := ValidateInventoryOptions(options); err != nil {
		return r.fail("invalid-options", err.Error())
	}
	if ctx.Err() != nil {
		return r.fail("canceled", "inventory canceled")
	}
	identity := Inspect(ctx, path)
	r = newInventoryResult(identity)
	if identity.Status != "ok" {
		r.Status = "error"
		r.Boundary.Complete = false
		r.Diagnostics = append(r.Diagnostics, identity.Diagnostics...)
		if len(r.Diagnostics) == 0 {
			return r.fail("identity-failed", "cannot safely identify inventory target")
		}
		return r
	}
	normalized, e := normalizeInventoryOptions(identity, options)
	if e != nil {
		return r.fail("invalid-options", e.Error())
	}
	switch identity.Backend {
	case "git":
		r = inventoryGit(ctx, identity, normalized)
	case "jj":
		r = inventoryJJ(ctx, identity, normalized)
	default:
		return r.fail("unsupported-backend", "inventory backend unsupported")
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return boundedInventoryFailure(r, "inventory-timeout", "inventory exceeded its ten-second deadline")
		}
		return boundedInventoryFailure(r, "canceled", "inventory canceled")
	}
	if r.Status != "error" {
		final := Inspect(ctx, path)
		if ctx.Err() != nil {
			return inventoryContextFailure(ctx, r)
		}
		if final.Status != "ok" || !reflect.DeepEqual(identity, final) {
			return boundedInventoryFailure(r, "changed-repository", "repository identity or ownership changed during inventory")
		}
	}
	if ctx.Err() != nil {
		return inventoryContextFailure(ctx, r)
	}
	if e = checkInventoryLimits(r); e != nil {
		return boundedInventoryFailure(r, "inventory-limit", e.Error())
	}
	return r
}
func boundedInventoryFailure(r InventoryResult, code, message string) InventoryResult {
	// Discard incomplete collections so an oversized/error observation cannot escape its envelope bound.
	r.Workspaces = []InventoryWorkspace{}
	r.References = []InventoryReference{}
	r.Bases = []InventoryBase{}
	r.Files = []InventoryFile{}
	r.Boundary.Limitations = []string{"Incomplete observations were discarded; no consequential action is authorized."}
	r.Diagnostics = []Diagnostic{}
	if r.Query != nil {
		r.Query.Evidence = []string{}
	}
	return r.fail(code, message)
}
func normalizeInventoryOptions(identity Result, o InventoryOptions) (InventoryOptions, error) {
	if len(o.Scopes) == 0 && o.Query != "ref-exists" && o.Query != "ancestor" {
		o.Scopes = []string{"disk", "tracked", "untracked"}
	}
	root := identity.InputPath
	if identity.Repository != nil && identity.Repository.Worktree != nil {
		root = *identity.Repository.Worktree
	}
	if identity.JJRepository != nil {
		root = identity.JJRepository.WorkspaceRoot
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return o, errors.New("cannot resolve selected workspace path")
	}
	root = canonical
	anchor := root
	if o.Anchor == "cwd" {
		cwd, e := os.Getwd()
		if e != nil {
			return o, errors.New("cannot resolve caller directory")
		}
		cwd, e = filepath.EvalSymlinks(cwd)
		if e != nil || !within(root, cwd) {
			return o, errors.New("caller directory is outside selected workspace")
		}
		anchor = cwd
	}
	normalized := []string{}
	for _, p := range o.Paths {
		target := filepath.Join(anchor, p)
		if !within(root, target) {
			return o, errors.New("literal path escapes selected workspace")
		}
		relative, e := filepath.Rel(root, target)
		if e != nil {
			return o, e
		}
		normalized = append(normalized, filepath.ToSlash(relative))
	}
	o.Paths = normalized
	o.Anchor = "workspace"
	return o, nil
}

func inventoryContextFailure(ctx context.Context, r InventoryResult) InventoryResult {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return boundedInventoryFailure(r, "inventory-timeout", "inventory exceeded its ten-second deadline")
	}
	return boundedInventoryFailure(r, "canceled", "inventory canceled")
}
