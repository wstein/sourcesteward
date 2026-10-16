// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"path/filepath"
	"unicode/utf8"
)

const (
	pinsNoEvidenceMessage = "submodule evidence is not implemented in this revision"
	pinsJJMessage         = "jj has no Git submodule model; run this in a Git-owned checkout"
	pinsButlerMessage     = "GitButler-managed workspaces have no supported submodule audit; run this in a Git-owned checkout"
	pinsAmbiguousMessage  = "ownership markers conflict (jj and GitButler); resolve the ownership before auditing pins"
	pinsBareMessage       = "a bare repository has no worktree to compare with its submodule pins"
	pinsNoRepoMessage     = "no Git repository at the path"
)

// Pins audits the submodule pins of a Git superproject without writes, network or fetch.
func Pins(ctx context.Context, options PinsOptions) PinsResult { return pins(ctx, options, runGit) }

// pins decides ownership first and never reads submodule state before that decision:
//  1. jj-owned (standalone, colocated, secondary) and conflicting markers are decided from filesystem
//     markers alone and start no process of any kind;
//  2. every other decision uses the existing Git detection, which starts only Git processes and never jj.
func pins(ctx context.Context, options PinsOptions, run runner) PinsResult {
	shown := options.Path
	if shown == "" {
		shown = "."
	}
	r := NewPinsResult(shown)
	r.Backend = "git"
	fail := func(message string) PinsResult {
		r.Status, r.Error = "error", &PinsError{Code: "pins-failed", Message: message}
		r.Finalize()
		return r
	}
	refuse := func(backend, code, message string) PinsResult {
		r.Status, r.Backend, r.Error = "refused", backend, &PinsError{Code: code, Message: message}
		r.Finalize()
		return r
	}
	if err := ValidatePinsOptions(options); err != nil {
		return fail(err.Error())
	}
	if ctx.Err() != nil {
		return fail("pins canceled")
	}
	abs, err := filepath.Abs(shown)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	if err != nil || !utf8.ValidString(abs) {
		return fail("cannot access the pins path")
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return fail("the pins path must be an accessible directory")
	}
	var markers Result
	if _, err := ancestors(abs, &markers); err != nil {
		return fail("cannot inspect backend metadata")
	}
	if len(markers.Evidence) > 0 {
		if err := detectJJGitButler(ctx, abs, &markers); err != nil {
			if ctx.Err() != nil {
				return fail("pins canceled")
			}
			return fail("cannot safely inspect colocated ownership metadata")
		}
		managed(&markers)
		if markers.Backend == "ambiguous" {
			return refuse("ambiguous", "ambiguous-backend", pinsAmbiguousMessage)
		}
		return refuse("jj", "unsupported-backend", pinsJJMessage)
	}
	identity := inspectOwned(ctx, abs, run, true)
	if ctx.Err() != nil {
		return fail("pins canceled")
	}
	switch identity.Status {
	case "unsupported":
		if identity.Backend == "ambiguous" {
			return refuse("ambiguous", "ambiguous-backend", pinsAmbiguousMessage)
		}
		return refuse(identity.Backend, "unsupported-backend", pinsButlerMessage)
	case "error":
		if len(identity.Diagnostics) > 0 && identity.Diagnostics[0].Code == "not-repository" {
			return refuse("unknown", "not-repository", pinsNoRepoMessage)
		}
		return fail("cannot establish repository identity: " + identity.Error())
	}
	if identity.Repository != nil && identity.Repository.Bare {
		return refuse("git", "bare-repository", pinsBareMessage)
	}
	// Later work items replace this with registry, checkout and publication evidence.
	return fail(pinsNoEvidenceMessage)
}
